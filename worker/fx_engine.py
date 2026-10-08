"""Звуковой движок: цепочка обработки дорожки «педали → усилитель → кабинет →
пространство» на numpy/scipy (+ NAM через resources).

Контракт «без задержки»: выход каждого блока той же длины, что вход, и не сдвинут —
импульс на входе даёт пик на том же сэмпле. Отсюда устройство блоков:
- линейные фильтры — нулевой фазы (sosfiltfilt): обработка офлайн, будущее известно;
  каузальный срез верха сдвигал бы пик на 2–3 сэмпла;
- гейт/компрессор меняют только громкость: огибающая считается на управляющей
  частоте (блоки по CTRL сэмплов), сам сигнал не задерживается;
- перегруз — с передискретизацией ×4 (resample_poly не сдвигает);
- усилитель (NAM) — сдвиг модели известен (latency) и снимается;
- кабинет и реверб — свёртка; у IR кабинета пик выравнивается в 0, у реверба сухой
  путь на месте, хвост начинается после предзадержки.
"""
from __future__ import annotations

import json
from fractions import Fraction
from pathlib import Path

import numpy as np
from scipy import signal
from scipy.ndimage import maximum_filter1d


class ChainError(ValueError):
    """Неверная цепочка: текст — понятная причина (уходит в ответ 422)."""


CTRL = 16            # шаг управляющего сигнала гейта/компрессора, сэмплов (~0,36 мс при 44,1 кГц)
OVERSAMPLE = 4       # передискретизация перегруза
MAX_TAIL_S = 10.0    # предел длины хвоста реверба/дилея
EPS = 1e-12
SAMPLER_HOP = 128    # кадр уточнения ударов баса, сэмплов (2,7 мс при 48 кГц)
HITS_HOP = 256       # кадр поиска ударов sampler: место удара — по пику в сэмплах; кадр 128 стоил ~1,4 ГБ на трек
BASS_SAMPLE_S = 6.0  # bass: сэмпл набора дольше не тянется (Growlybass записан по 6 с — целиком)
BASS_RELEASE_S = 0.03  # bass: глушение струны в конце ноты
BASS_FOLLOW_DB = 15.0  # bass: предел подгонки громкости по времени к входу, ± дБ
BASS_RISE_DB = 2.0   # bass: удар — рост громкости 25 мс после начала против 25 мс до, не меньше
AMP_INPUT_RMS_DB = -20.0  # уровень входа захвата NAM (так шли опыты на гитаре YuE)
MAX_BLOCKS = 16      # блоков в цепочке не больше: длинная цепочка надолго заняла бы воркер и очередь GPU
MAX_BANDS = 12       # полос эквалайзера в блоке не больше

# описание блоков — один источник worker/fx_blocks.json (копии во фронте и MCP — make mcp-data)
BLOCKS_JSON = Path(__file__).with_name("fx_blocks.json")
_BLOCKS = json.loads(BLOCKS_JSON.read_text(encoding="utf-8"))
# параметр: (умолчание, мин, макс); zero_off — 0 тоже допустим и значит «выкл»
SPEC: dict[str, dict[str, tuple]] = {
    t: {p["id"]: (float(p["default"]), float(p["min"]), float(p["max"])) for p in b.get("params", [])}
    for t, b in _BLOCKS.items()}
ZERO_OFF: dict[str, set[str]] = {t: {p["id"] for p in b.get("params", []) if p.get("zero_off")}
                                 for t, b in _BLOCKS.items()}
# целые параметры (число нот на долю): 2.5 — ошибка, а не округление
INTEGER: dict[str, set[str]] = {t: {p["id"] for p in b.get("params", []) if p.get("integer")}
                                for t, b in _BLOCKS.items()}
# строковые параметры: (умолчание или None — обязателен)
STR_SPEC: dict[str, dict[str, str | None]] = {
    t: {s["id"]: (None if s.get("required") else s.get("default", "")) for s in b["strings"]}
    for t, b in _BLOCKS.items() if b.get("strings")}
BAND_SPEC = {k: (float(v["default"]), float(v["min"]), float(v["max"]))
             for k, v in _BLOCKS["eq"]["bands"].items()}


def _num(v) -> bool:
    return isinstance(v, (int, float)) and not isinstance(v, bool) and np.isfinite(v)


def _check_num(where: str, key: str, v, spec: tuple) -> float:
    _, lo, hi = spec
    if not _num(v):
        raise ChainError(f"{where}: {key} должен быть числом")
    if not lo <= v <= hi:
        raise ChainError(f"{where}: {key}={v} вне диапазона [{lo:g}, {hi:g}]")
    return float(v)


def _parse_bands(where: str, bands) -> list[dict]:
    if not isinstance(bands, list):
        raise ChainError(f"{where}: bands должен быть списком полос")
    if len(bands) > MAX_BANDS:
        raise ChainError(f"{where}: не больше {MAX_BANDS} полос (сейчас {len(bands)})")
    out = []
    for j, b in enumerate(bands):
        w = f"{where}, полоса {j + 1}"
        if not isinstance(b, dict):
            raise ChainError(f"{w}: полоса — объект {{freq_hz, gain_db, q}}")
        extra = set(b) - set(BAND_SPEC)
        if extra:
            raise ChainError(f"{w}: неизвестный параметр {sorted(extra)[0]}")
        out.append({k: _check_num(w, k, b.get(k, s[0]), s) for k, s in BAND_SPEC.items()})
    return out


def parse_chain(chain) -> list[dict]:
    """Проверка и нормализация цепочки: умолчания дописываются, ошибки — ChainError."""
    if not isinstance(chain, list):
        raise ChainError("цепочка — список блоков")
    if not chain:
        raise ChainError("цепочка пуста")
    if len(chain) > MAX_BLOCKS:
        raise ChainError(f"цепочка: не больше {MAX_BLOCKS} блоков (сейчас {len(chain)})")
    out = []
    for i, blk in enumerate(chain):
        where = f"блок {i + 1}"
        if not isinstance(blk, dict):
            raise ChainError(f"{where}: блок — объект с полем type")
        t = blk.get("type")
        if not isinstance(t, str) or t not in SPEC:
            raise ChainError(f"{where}: неизвестный тип {t!r} (есть: {', '.join(SPEC)})")
        where = f"{where} ({t})"
        nums, strs = SPEC[t], STR_SPEC.get(t, {})
        allowed = set(nums) | set(strs) | {"type"} | ({"bands"} if t == "eq" else set())
        extra = set(blk) - allowed
        if extra:
            raise ChainError(f"{where}: неизвестный параметр {sorted(extra)[0]}")
        norm: dict = {"type": t}
        for k, s in nums.items():
            v = blk.get(k, s[0])
            norm[k] = 0.0 if k in ZERO_OFF.get(t, ()) and _num(v) and v == 0 else _check_num(where, k, v, s)
            if k in INTEGER.get(t, ()) and norm[k] != int(norm[k]):
                raise ChainError(f"{where}: {k} должен быть целым")
        for k, default in strs.items():
            v = blk.get(k, default)
            if v is None:
                raise ChainError(f"{where}: нужен параметр {k}")
            if not isinstance(v, str):
                raise ChainError(f"{where}: {k} должен быть строкой")
            norm[k] = v
        if t == "eq":
            norm["bands"] = _parse_bands(where, blk.get("bands", []))
        out.append(norm)
    return out


def needs_gpu(chain) -> bool:
    """Есть ли в цепочке усилитель (NAM — видеокарта, общая очередь)."""
    return any(isinstance(b, dict) and b.get("type") == "amp" for b in (chain or []))


# ---------- общие помощники ----------

def _db(v: float) -> float:
    return float(10 ** (v / 20))


def _rms(x: np.ndarray) -> float:
    return float(np.sqrt(np.mean(np.square(x, dtype=np.float64)))) if x.size else 0.0


def _zero_phase(sos, x: np.ndarray) -> np.ndarray:
    """Фильтр нулевой фазы по времени (ось 0). Короткий вход — без отражённого края."""
    padlen = min(3 * (2 * len(sos) + 1), x.shape[0] - 1)
    if padlen < 1:
        return x.copy()
    return signal.sosfiltfilt(sos, x, axis=0, padlen=padlen)


def _nyq_ok(hz: float, sr: int) -> float:
    return min(hz, 0.45 * sr)


def _peaking_sos(freq: float, gain_db: float, q: float, sr: int) -> np.ndarray:
    """Пиковый биквад RBJ (cookbook)."""
    a = 10 ** (gain_db / 40)
    w0 = 2 * np.pi * _nyq_ok(freq, sr) / sr
    alpha = np.sin(w0) / (2 * q)
    b = [1 + alpha * a, -2 * np.cos(w0), 1 - alpha * a]
    den = [1 + alpha / a, -2 * np.cos(w0), 1 - alpha / a]
    return signal.tf2sos(b, den)


def _resample(x: np.ndarray, sr_from: int, sr_to: int) -> np.ndarray:
    if sr_from == sr_to:
        return x
    f = Fraction(sr_to, sr_from)
    return signal.resample_poly(x, f.numerator, f.denominator, axis=0)


def _fit(x: np.ndarray, n: int) -> np.ndarray:
    """Обрезать/дополнить нулями до n сэмплов по оси 0."""
    if x.shape[0] >= n:
        return x[:n]
    pad = [(0, n - x.shape[0])] + [(0, 0)] * (x.ndim - 1)
    return np.pad(x, pad)


def _ctrl_env(x: np.ndarray) -> np.ndarray:
    """Пик |x| (по всем каналам) на блоках по CTRL сэмплов."""
    a = np.abs(x).max(axis=1)
    nb = -(-len(a) // CTRL)
    a = np.pad(a, (0, nb * CTRL - len(a)))
    return a.reshape(nb, CTRL).max(axis=1)


def _ctrl_to_samples(g: np.ndarray, n: int) -> np.ndarray:
    """Управляющий сигнал (по центрам блоков) → на каждый сэмпл."""
    centers = np.arange(len(g)) * CTRL + (CTRL - 1) / 2
    return np.interp(np.arange(n), centers, g)


def _coef(ms: float, sr: int) -> float:
    """Коэффициент однополюсного сглаживания на управляющей частоте."""
    steps = max(ms / 1000 * sr / CTRL, 1e-3)
    return float(1 - np.exp(-1 / steps))


def _smooth(target: np.ndarray, up: float, down: float, start: float) -> np.ndarray:
    """Асимметричное сглаживание: up — когда растёт, down — когда падает."""
    out = np.empty_like(target)
    g = start
    for i, t in enumerate(target):
        g += (up if t > g else down) * (t - g)
        out[i] = g
    return out


# ---------- блоки (x: (n, ch) float64) ----------

def _gate(x, sr, p, _res):
    env = _ctrl_env(x)
    # «заглядывание вперёд» на время атаки: гейт открыт к началу удара, сигнал не задерживается
    # (5 постоянных времени — к удару гейт открыт на 99 %)
    ahead = int(np.ceil(5 * p["attack_ms"] / 1000 * sr / CTRL)) + 1
    env = maximum_filter1d(env, size=2 * ahead + 1, origin=0)
    thr = _db(p["threshold_db"])
    floor = _db(p["range_db"])
    target = np.where(env >= thr, 1.0, floor)
    g = _smooth(target, _coef(p["attack_ms"], sr), _coef(p["release_ms"], sr), float(target[0]) if len(target) else 1.0)
    return x * _ctrl_to_samples(g, x.shape[0])[:, None]


def _comp(x, sr, p, _res):
    env = _ctrl_env(x)
    lvl = _smooth(env, _coef(p["attack_ms"], sr), _coef(p["release_ms"], sr), 0.0)
    lvl_db = 20 * np.log10(np.maximum(lvl, EPS))
    over = np.maximum(lvl_db - p["threshold_db"], 0.0)
    gain_db = -over * (1 - 1 / p["ratio"]) + p["makeup_db"]
    return x * _ctrl_to_samples(10 ** (gain_db / 20), x.shape[0])[:, None]


def _eq(x, sr, p, _res):
    y = x
    if p["highpass_hz"] > 0:
        y = _zero_phase(signal.butter(2, _nyq_ok(p["highpass_hz"], sr), "highpass", fs=sr, output="sos"), y)
    if p["lowpass_hz"] > 0:
        y = _zero_phase(signal.butter(2, _nyq_ok(p["lowpass_hz"], sr), "lowpass", fs=sr, output="sos"), y)
    for b in p["bands"]:
        if b["gain_db"]:
            # два прохода (туда и обратно) — половина усиления на проход
            y = _zero_phase(_peaking_sos(b["freq_hz"], b["gain_db"] / 2, b["q"], sr), y)
    return y


def _drive(x, sr, p, _res):
    n = x.shape[0]
    up = signal.resample_poly(x, OVERSAMPLE, 1, axis=0)
    g = _db(p["gain_db"])
    wet = np.tanh(g * up)
    wet = _fit(signal.resample_poly(wet, 1, OVERSAMPLE, axis=0), n)
    # громкость перегруза — как у входа: крутилка gain меняет характер, не уровень
    wet *= _rms(x) / max(_rms(wet), EPS)
    return (p["mix"] * wet + (1 - p["mix"]) * x) * _db(p["output_db"])


def _shift_left(y: np.ndarray, k: int) -> np.ndarray:
    """Сдвинуть к началу на k сэмплов (k < 0 — к концу), длина та же, края — нули."""
    if k == 0:
        return y
    out = np.zeros_like(y)
    if k > 0:
        out[:len(y) - k] = y[k:]
    else:
        out[-k:] = y[:len(y) + k]
    return out


def _amp(x, sr, p, res):
    model = _resource(res, "amp", p["model"])
    n = x.shape[0]
    msr = int(getattr(model, "sr", 48000))
    lat = int(getattr(model, "latency", 0))
    # захваты сняты с гитарным уровнем: вход приводится к AMP_INPUT_RMS_DB, input_db — от него;
    # выход — к громкости входа (output_db — от неё), иначе дорожка в миксе прыгает
    rin = _rms(x)
    gin = (_db(AMP_INPUT_RMS_DB) / rin if rin > EPS else 1.0) * _db(p["input_db"])
    chans = []
    for c in range(x.shape[1]):
        xm = (_resample(x[:, c], sr, msr) * gin).astype(np.float32)
        m = len(xm)
        if abs(lat) >= m:            # задержка не меньше куска — захват неисправен (отрицательная падала с 500)
            raise ChainError(f"захват {p['model']!r}: задержка {lat} сэмплов не меньше длины куска ({m}) — "
                             "захват неисправен или кусок слишком короткий")
        # тишина в конце под задержку модели: иначе её последние lat сэмплов не успевают выйти
        xm = np.pad(xm, (0, max(lat, 0)))
        ym = np.asarray(model(xm), dtype=np.float64)
        ym = _shift_left(_fit(ym, len(xm)), lat)[:m]
        chans.append(_fit(_resample(ym, msr, sr), n))
    y = np.stack(chans, axis=1)
    return y * (rin / max(_rms(y), EPS) if rin > EPS else 1.0) * _db(p["output_db"])


def _ir_peaks(ir: np.ndarray) -> list[int]:
    """Позиция главного пика IR по каждому каналу: на столько свёртка канала сдвигается обратно
    (у стерео-IR пики каналов могут не совпадать)."""
    return [int(np.argmax(np.abs(ir[:, c]))) for c in range(ir.shape[1])]


def _load_ir(res, name: str, sr: int) -> np.ndarray:
    ir, ir_sr = _resource(res, "ir", name)
    ir = np.asarray(ir, dtype=np.float64)
    if ir.ndim == 1:
        ir = ir[:, None]
    if ir.shape[0] == 0 or not np.any(ir):
        raise ChainError(f"IR {name}: пустой файл")
    return _resample(ir, int(ir_sr), sr)


def _match_channels(ir: np.ndarray, ch: int) -> np.ndarray:
    """IR под число каналов входа: моно IR — на все каналы, иначе — среднее каналов IR."""
    return ir if ir.shape[1] == ch else np.repeat(ir.mean(axis=1, keepdims=True), ch, axis=1)


def _conv(x: np.ndarray, ir: np.ndarray, at: list[int] | None = None) -> np.ndarray:
    """Свёртка (n, ch) с IR (m, ch) → (n, ch); канал c отсчитывается с сэмпла at[c] (пик кабинета —
    в ноль, весь IR при этом работает, и до пика тоже)."""
    n, ch = x.shape
    ir = _match_channels(ir, ch)
    y = signal.oaconvolve(x, ir, axes=0)
    if not at:
        return _fit(y, n)
    return np.stack([_fit(y[at[c]:, c], n) for c in range(ch)], axis=1)


def _cab(x, sr, p, res):
    if p["ir"]:
        ir = _load_ir(res, p["ir"], sr)
        # громкость: 0 дБ на самой громкой частоте IR (постоянная, от сигнала не зависит)
        ir = ir / max(float(np.abs(np.fft.rfft(ir, axis=0)).max()), EPS)
        ir = _match_channels(ir, x.shape[1])
        wet = _conv(x, ir, _ir_peaks(ir))
    else:
        # «лёгкий кабинет»: мягкий срез верха без своей окраски
        wet = _zero_phase(signal.butter(2, _nyq_ok(p["cutoff_hz"], sr), "lowpass", fs=sr, output="sos"), x)
    return p["mix"] * wet + (1 - p["mix"]) * x


def _norm_energy(ir: np.ndarray) -> np.ndarray:
    e = np.sqrt(np.sum(np.square(ir), axis=0, keepdims=True))
    return ir / np.maximum(e, EPS)


def _gen_reverb_ir(sr: int, ch: int, decay_s: float, lowpass_hz: float) -> np.ndarray:
    """Затухающий шум: RT60 = decay_s, верх хвоста — lowpass_hz; каналы раскоррелированы."""
    n = int(min(decay_s * 1.5, MAX_TAIL_S) * sr)
    rng = np.random.default_rng(20261006)  # детерминированно: один и тот же звук на повторе
    t = np.arange(n) / sr
    ir = rng.standard_normal((n, ch)) * np.exp(-6.91 * t / decay_s)[:, None]
    ir = _zero_phase(signal.butter(2, _nyq_ok(lowpass_hz, sr), "lowpass", fs=sr, output="sos"), ir)
    fade = min(n, int(0.005 * sr))  # мягкое начало хвоста
    ir[:fade] *= np.linspace(0, 1, fade)[:, None]
    return ir


def _reverb(x, sr, p, res):
    if p["wet"] == 0:
        return x
    ch = x.shape[1]
    if p["ir"]:
        ir = _load_ir(res, p["ir"], sr)
        ir = _zero_phase(signal.butter(2, _nyq_ok(p["lowpass_hz"], sr), "lowpass", fs=sr, output="sos"), ir) \
            if ir.shape[0] > 12 else ir
    else:
        ir = _gen_reverb_ir(sr, ch, p["decay_s"], p["lowpass_hz"])
    ir = _norm_energy(ir)
    pre = int(round(p["predelay_ms"] / 1000 * sr))
    # хвост — строго после сухого звука (не раньше 1 сэмпла), сухой путь не трогаем
    ir = np.concatenate([np.zeros((max(pre, 1), ir.shape[1])), ir])
    return x + p["wet"] * _conv(x, ir)


def _delay(x, sr, p, _res):
    if p["wet"] == 0:
        return x
    d = max(1, int(round(p["time_ms"] / 1000 * sr)))
    limit = int(MAX_TAIL_S * sr)
    sos = signal.butter(2, _nyq_ok(p["lowpass_hz"], sr), "lowpass", fs=sr, output="sos")
    # отклик повторов: k-й повтор на k·d, громкость fb^(k-1), каждый следующий темнее
    taps = []
    echo = np.zeros(1 + 64)
    echo[32] = 1.0
    amp, k = 1.0, 1
    while k * d < limit and amp > 1e-3:
        echo = _zero_phase(sos, echo)
        taps.append((k * d, amp, echo.copy()))
        amp *= p["feedback"]
        k += 1
        if p["feedback"] == 0:
            break
    m = taps[-1][0] + 33 if taps else 1
    ir = np.zeros(m)
    for pos, a, e in taps:
        lo = pos - 32
        seg = e * a
        if lo < 1:  # не раньше сухого звука
            seg = seg[1 - lo:]
            lo = 1
        ir[lo:lo + len(seg)] += seg[:m - lo]
    return x + p["wet"] * _conv(x, ir[:, None])


def _resource(res, kind: str, name: str):
    if res is None:
        raise ChainError(f"нет хранилища для {kind} {name!r}")
    try:
        return getattr(res, kind)(name)
    except KeyError:
        what = {"amp": "захват", "ir": "IR", "kit": "набор"}.get(kind, kind)
        raise ChainError(f"{what} {name!r} не найден") from None


def _hits(x: np.ndarray, sr: int, floor_db: float) -> list[tuple[int, float]]:
    """Удары во входе (n, ch): начала нот (librosa), пик — максимум |моно| до 40 мс после начала;
    тише p95·10^(floor_db/20) — отбрасываются (протечка других барабанов в дорожке-части)."""
    import librosa
    mono = np.abs(x).mean(axis=1)
    hop = HITS_HOP
    # тишина спереди: удар в первом кадре librosa иначе не видит (не с чем сравнить)
    pad = max(4 * hop, int(0.25 * sr))   # выбор пиков librosa усредняет ~0,1 с истории
    lead = np.concatenate([np.zeros(pad, dtype=np.float32), x.mean(axis=1).astype(np.float32),
                           np.zeros(pad, dtype=np.float32)])   # и сзади: удар в последнем кадре
    on = librosa.onset.onset_detect(y=lead, sr=sr, units="samples", hop_length=hop, backtrack=False, delta=0.08)
    on = np.clip(on - pad, 0, len(mono) - 1)
    win = max(1, int(0.015 * sr))
    hits = []
    hop = SAMPLER_HOP                    # запасы вокруг удара — прежние: кадр поиска крупнее, место — в сэмплах
    back = max(2 * hop, int(0.02 * sr))
    for o in on:
        # начало ноты librosa даёт с точностью до кадра и бывает позже самого удара (у края —
        # на сотни сэмплов): пик ищем до 20 мс назад, не заходя за предыдущий удар, и до 40 мс вперёд,
        # не заходя за следующее начало. 15 мс вперёд было мало: у бочки несколько почти равных пиков
        # подряд, и выбор зависел от кадра начала — на #279 каждый пятый удар гулял на 1–10 мс
        # между окнами, начатыми в разных местах
        a = max(0, o - back, (hits[-1][0] + hop) if hits else 0)
        nxt = on[on > o + win]
        seg = mono[a:min(o + int(0.04 * sr), int(nxt[0]) - hop if len(nxt) else len(mono), len(mono))]
        if len(seg):
            t = int(a + np.argmax(seg))
            if not hits or t - hits[-1][0] > hop:   # тот же удар, найденный с двух кадров, — один раз
                hits.append((t, float(seg.max())))
    if not hits:
        return []
    top = float(np.percentile([p for _, p in hits], 95))
    return [(t, p) for t, p in hits if p >= top * _db(floor_db)]


def _sampler_layers(res, name: str, sr: int, ch: int) -> list[tuple[float, int, np.ndarray, int]]:
    """Сэмплы набора как слои (пик, место пика, сэмпл, начало атаки — первый отсчёт ≥ 10 % пика),
    по возрастанию пика."""
    raw, ksr = _resource(res, "kit", name)
    layers = []
    for s in raw:
        s = np.asarray(s, dtype=np.float64)
        s = s[:, None] if s.ndim == 1 else s
        s = _match_channels(_resample(s, int(ksr), sr), ch)
        mono = np.abs(s).sum(axis=1)
        if mono.max() > 0:
            layers.append((float(np.abs(s).max()), int(np.argmax(mono)), s,
                           int(np.argmax(mono >= 0.1 * mono.max()))))
    if not layers:
        raise ChainError(f"набор {name!r}: нет сэмплов")
    layers.sort(key=lambda t: t[0])
    return layers


def _ring(env: np.ndarray, t: int, end: int) -> int:
    """Сколько звучит удар с пиком на t: до спада огибающей на −20 дБ, не дальше end (следующий удар)."""
    seg = env[t:max(t + 1, end)]
    k = np.flatnonzero(seg < seg[0] * 0.1)
    return int(k[0]) if len(k) else len(seg)


def _env5(mono: np.ndarray, sr: int) -> np.ndarray:
    from scipy.ndimage import uniform_filter1d
    # сглаживание даёт в тишине крошечные отрицательные числа — корень из них NaN
    return np.sqrt(np.maximum(uniform_filter1d(mono * mono, max(1, int(0.005 * sr))), 0.0))


def _sampler(x, sr, p, res):
    """Замена ударов сэмплами набора: сила удара → слой по рангу (соседние удары — соседние
    слои по кругу), пик сэмпла — на пик удара (без сдвига); громкость — как у входа.
    kit_open — второй набор для долго звучащих ударов (открытый хэт): удар звучит во входе (до −20 дБ,
    не дальше следующего удара) дольше, чем среднее геометрическое типичных времён звучания сэмплов
    обоих наборов, — берётся kit_open. choke — новый удар глушит предыдущий (педаль хэта)."""
    n, ch = x.shape
    sets = [_sampler_layers(res, p["kit"], sr, ch)]
    if p.get("kit_open"):
        sets.append(_sampler_layers(res, p["kit_open"], sr, ch))
    y = np.zeros_like(x)
    hits = _hits(x, sr, p["floor_db"])
    if not hits:
        return y
    kind = [0] * len(hits)
    if len(sets) > 1:
        typ = [float(np.median([_ring(_env5(np.abs(s).mean(axis=1), sr), at, len(s)) for _, at, s, _ in L]))
               for L in sets]
        edge = np.sqrt(max(typ[0], 1.0) * max(typ[1], 1.0))
        env = _env5(np.abs(x).mean(axis=1), sr)
        for i, (t, _) in enumerate(hits):
            end = hits[i + 1][0] if i + 1 < len(hits) else n
            kind[i] = int(_ring(env, t, end) >= edge)
    ps = np.array([h[1] for h in hits])
    ranks = np.argsort(np.argsort(ps)) / max(len(ps) - 1, 1)
    fade = max(1, int(0.01 * sr))
    turn = [0, 0]                                       # чередование слоёв — своё у каждого набора
    chosen = []
    for i, r in enumerate(ranks):
        layers = sets[kind[i]]
        last = len(layers) - 1
        chosen.append(layers[min(last, max(0, int(round(r * last)) + (turn[kind[i]] % 3) - 1))])  # −1/0/+1 слой
        turn[kind[i]] += 1
    for i, (t, pk) in enumerate(hits):
        peak, at, s, _ = chosen[i]
        s0 = t - at
        a, b = max(s0, 0), min(s0 + len(s), n)
        if b <= a:
            continue
        seg = s[a - s0:b - s0] * (pk / peak)
        if p.get("choke") and i + 1 < len(hits):
            # к началу атаки следующего сэмпла — тишина, спад 10 мс; но не раньше собственного пика
            # (удары ближе 10 мс — глушение не задевает пик текущего)
            t1, (_, at1, _, att1) = hits[i + 1][0], chosen[i + 1]
            cut = max(t1 - at1 + att1, t + 1)
            if cut < b:
                g = np.ones(b - a)
                lo = max(a, t, cut - fade)
                g[lo - a:cut - a] = np.linspace(1, 0, cut - lo, endpoint=False)
                g[cut - a:] = 0
                seg = seg * g[:, None]
        y[a:b] += seg
    rin = _rms(x)
    return y * (rin / max(_rms(y), EPS) if rin > EPS else 1.0) * _db(p["output_db"])


def _kit_notes(raw, ksr: int) -> list[tuple[float, float, int, np.ndarray]]:
    """Сэмплы набора баса: (высота MIDI по самому звуку, пик, начало атаки, моно-сэмпл) — имена
    файлов не нужны. Длинные сэмплы режутся до BASS_SAMPLE_S: щипок дольше не тянется."""
    import librosa
    out = []
    for s in raw:
        s = np.asarray(s, dtype=np.float32)   # 224 сэмпла по 6 с: float64 — лишние 240 МБ
        s = (s.mean(axis=1) if s.ndim > 1 else s)[:int(BASS_SAMPLE_S * ksr)]
        peak = float(np.abs(s).max()) if len(s) else 0.0
        if peak <= 0:
            continue
        a = int(np.argmax(np.abs(s) > 0.1 * peak))          # начало атаки
        body = s[a + int(0.05 * ksr):a + int(0.6 * ksr)]    # после щелчка струны — тон
        if len(body) < 2048:
            continue
        # librosa предупреждает, что 25 Гц не влезает в кадр дважды; на Growlybass замер сверен с pyin
        # (C#1/E1 — те же отклонения: это расстройка самих сэмплов, её и компенсирует сдвиг высоты)
        with np.errstate(invalid="ignore"):   # librosa/numba: «invalid value in cast» при первой компиляции
            f0 = librosa.yin(body, fmin=25, fmax=500, sr=ksr, frame_length=2048)
        out.append((float(librosa.hz_to_midi(np.median(f0))), peak, a, s))
    return out


def _attack_at(m: np.ndarray, hf: np.ndarray, c: np.ndarray, o: int, sr: int) -> tuple[int, float] | None:
    """Удар около начала ноты librosa: (сэмпл начала, рост громкости в дБ) или None, если удара нет.
    Начало — точка от −30 до +60 мс, где громкость 25 мс после неё больше всего превышает громкость 25 мс до
    неё (окно короче периода E1 — 24 мс — гуляло бы с фазой волны); максимум на краю поиска — удар
    дальше, этот кандидат не он. Есть щелчок струны (верх — разность соседних сэмплов) рядом —
    начало уточняется по нему до сэмпла: первый сэмпл, где щелчок доходит до 30 % своего пика."""
    w = max(8, int(0.025 * sr))
    # вперёд дальше: на паузе между нотами librosa отмечает её начало, а удар — после паузы. У начала
    # окна «до» короче 25 мс (не меньше 2 мс): иначе нота через 10 мс после начала окна не находилась,
    # и щипок вставал на начало окна — раньше ноты
    # Короткое «до» — только из тишины: на хвосте низкой ноты громкость за пару мс зависит от фазы
    # волны, и рост «находился» на 3–8 мс от начала окна — щипок на 7–22 мс раньше ноты; на звуке
    # «до» не короче 12 мс (полпериода E1)
    t = np.arange(max(int(0.002 * sr), o - int(0.03 * sr)), min(len(m) - w, o + int(0.06 * sr)) + 1)
    if len(t) < 3:
        return None
    lo_t = np.maximum(t - w, 0)
    after_e = (c[t + w] - c[t]) / w
    before_e = (c[t] - c[lo_t]) / (t - lo_t)
    ratio = after_e / (before_e + EPS)
    short = (t - lo_t) < int(0.012 * sr)
    ratio[short & (before_e > after_e * _db(-40) ** 2)] = 0.0      # короткое «до» и не тишина — не удар
    k = int(np.argmax(ratio))
    if k == 0 or k == len(t) - 1:
        return None
    at = int(t[k])
    # рост — с отступом 10 мс по обе стороны: провал на стыке легато (спад и атака по 5 мс) сам
    # по себе не удар, а в окна вплотную он попадал и давал «рост» больше 2 дБ у повтора той же ноты
    g = int(0.01 * sr)
    a0, a1 = min(len(m), at + g), min(len(m), at + g + w)
    b0, b1 = max(0, at - g - w), max(0, at - g)
    if b1 - b0 < int(0.002 * sr):          # у начала окна отступу нет места — «до» = всё, что до точки
        b0, b1 = 0, at
    before = (c[b1] - c[b0]) / max(b1 - b0, 1)
    rise = 10 * np.log10(max((c[a1] - c[a0]) / max(a1 - a0, 1) / (before + EPS), EPS))
    # окна по 25 мс ставят точку позже начала плавной атаки (5–10 мс): начало — последняя точка за
    # 20 мс до неё, где огибающая (максимум |звука| на 2 мс вперёд — назад не заглядывает) ещё не
    # выше уровня до удара + 20 % подъёма к пику следующих 20 мс
    from scipy.ndimage import maximum_filter1d
    sounding = False                       # перед ударом звучит хвост ноты (не пауза)
    b0, b1 = max(0, at - int(0.045 * sr)), min(len(m), at + int(0.02 * sr))
    q = int(0.002 * sr)
    if at - b0 > int(0.02 * sr) + q and b1 > at:
        ef = maximum_filter1d(np.abs(m[b0:b1 + q]), 2 * q + 1, origin=-q)[:b1 - b0]
        q0, q1 = at - int(0.02 * sr) - b0, at - b0 + 1
        base, peak = float(np.median(ef[:q0])), float(ef[q1 - 1:].max())
        seg = ef[q0:q1]
        if peak < base * _db(BASS_RISE_DB) and seg.min() <= 0.2 * base:
            # роста нет (смена ноты легато), но есть глубокий провал стыка — нота начинается на его дне:
            # по «20 % подъёма» точка уезжала на 15 мс позже. Дно — по RMS 8 мс: огибающая «2 мс вперёд»
            # проваливается и на нуле волны низкой ноты (дно на 6 мс раньше стыка)
            from scipy.ndimage import uniform_filter1d
            rms = np.sqrt(np.maximum(uniform_filter1d(m[b0:b1] ** 2, max(1, int(0.008 * sr))), 0.0))
            at = b0 + q0 + int(np.argmin(rms[q0:q1]))
        elif peak > base:
            low = np.flatnonzero(ef[q0:q1] <= base + 0.2 * (peak - base))
            if len(low):
                at = b0 + q0 + int(low[-1])
        # хвост — если за 10 мс до точки громкость (RMS 8 мс) не падала к нулю (пауза). Огибающая «2 мс»
        # для этого не годится: проваливается на нуле низкой волны, и хвост принимался за паузу (−7 мс)
        from scipy.ndimage import uniform_filter1d
        k5 = at - b0
        tail = np.sqrt(np.maximum(uniform_filter1d(m[b0:b1] ** 2, max(1, int(0.008 * sr))), 0))
        sounding = k5 > 0 and float(tail[max(0, k5 - int(0.01 * sr)):k5].min()) > 0.1 * peak
    hop = SAMPLER_HOP
    # на хвосте ноты щелчок ищется и до 12 мс вперёд: уточнение по огибающей бывает там на полпериода
    # раньше удара (впадина волны), а щелчок струны стоит ровно на ударе. После паузы — нет: вперёд
    # нашёлся бы излом конца плавной атаки (+7,6 мс при атаке 10 мс)
    lo, hi = max(1, at - 2 * hop), min(len(m), at + (int(0.012 * sr) if sounding else 2 * hop))
    seg = np.abs(hf[lo:hi])
    base = np.median(np.abs(hf[max(0, lo - int(0.05 * sr)):lo])) if lo > 1 else 0.0
    if len(seg) and seg.max() > 8 * max(base, EPS):
        at = int(lo + np.argmax(seg >= 0.3 * seg.max()))
    return at, rise


def _bass_cells(m: np.ndarray, sr: int, division: int, changes=()) -> tuple[np.ndarray, set]:
    """Границы ячеек (сэмплы) и множество тех, что начинаются ударом. Сетка — доли дорожки,
    поделённые на division; удары (начала нот) встают в сетку сами и заменяют соседнюю границу
    ближе 40 мс. Долей не нашлось (одна нота, рубато) — ячейки только по ударам."""
    import librosa
    hop = SAMPLER_HOP
    # края — зеркальным продолжением, не тишиной: окно превью/пересборки начинается посреди звучащего
    # баса, и скачок из тишины давал огромный ложный удар — librosa нормирует силу ударов по самому
    # сильному, и настоящие уходили под порог (отрывок RoFormer: 9 начал нот вместо 140)
    pad = min(max(4 * hop, int(0.25 * sr)), len(m) - 1)
    lead = np.pad(m, pad, mode="reflect").astype(np.float32) if pad > 0 else m.astype(np.float32)
    # кадр 256: начало всё равно уточняется до сэмпла в _attack_at, а кадр 128 стоил 1,4 ГБ на трек
    on = librosa.onset.onset_detect(y=lead, sr=sr, units="samples", hop_length=2 * hop, backtrack=True)
    on = np.unique(np.clip(on - pad, 0, len(m) - 1))
    hf = np.diff(m, prepend=m[:1])     # верх без фильтра: нулевая фаза размазала бы щелчок назад на ~3 мс
    c = np.cumsum(np.concatenate([[0.0], m * m]))
    near = int(0.04 * sr)
    # удар — только с ростом громкости (иначе librosa ловит и конец ноты, и переливы тянущейся);
    # кандидаты ближе 40 мс — один удар: остаётся тот, где рост больше
    cand = sorted(h for h in (_attack_at(m, hf, c, int(o), sr) for o in on) if h and h[1] >= BASS_RISE_DB)
    soft = np.array(sorted(h[0] for h in (_attack_at(m, hf, c, int(o), sr) for o in on) if h), dtype=int)
    hits: list = []
    for h in cand:
        if hits and h[0] - hits[-1][0] < near:
            if h[1] > hits[-1][1]:
                hits[-1] = h
        else:
            hits.append(h)
    on = np.array([h[0] for h in hits], dtype=int)
    # доли — кадром 512: сетка нужна лишь там, где ударов нет; кадр 128 стоил 14,5 ГБ на трек 279 с
    _, beats = librosa.beat.beat_track(y=m.astype(np.float32), sr=sr, hop_length=512, units="samples")
    grid = []
    if len(beats) >= 2:
        step = float(np.median(np.diff(beats))) / division
        g = float(beats[0])
        while g - step >= 0:
            g -= step
        grid = list(np.arange(g, len(m), step).astype(int))
    bounds = set(int(o) for o in on)
    # смена высоты без удара — тоже граница: доли по басу бывают найдены вдвое реже (#466: 59 вместо
    # 129 BPM), и нота тянулась бы через смену. Кадр yin — 186 мс: смена рядом с ударом (±120 мс) —
    # это сам удар, иначе граница встала бы перед ним и дала щипок раньше удара. Без удара — на начало
    # ноты librosa без роста громкости рядом (±120 мс): смена ноты слышна там, а не в середине кадра yin.
    # Начала нет — смену не берём: на мутной дорожке (Demucs, гитара в басе) высота дрожит, и щипки
    # по ней шли бы мимо такта — хуже, чем нота, дотянутая до следующего удара
    lim = int(0.12 * sr)
    for g in changes:
        if (len(on) and np.min(np.abs(on - g)) <= lim) or not len(soft) or np.min(np.abs(soft - g)) > lim:
            continue
        g = int(soft[np.argmin(np.abs(soft - g))])
        if not bounds or min(abs(g - b) for b in bounds) > near:
            bounds.add(int(g))
    if not len(on) or on[0] > near:   # удар у самого начала окна — ячейка [0, удар) дала бы второй щипок
        bounds.add(0)
    # сетка — лишь в промежутках: не ближе 100 мс к любой границе (удару, смене ноты, началу окна) —
    # граница чуть раньше другой давала короткий щипок перед ней (#466: пары через 40–55 мс)
    # (не дальше 0,6 шага сетки: при division 4 и быстром темпе шаг сам ~90 мс). Сравнение — с границами
    # до сетки: с только что добавленной точкой сетки выпадала бы каждая вторая
    far = int(min(0.1 * sr, 0.6 * step)) if grid else 0
    fixed = set(bounds) | {-far - 1}
    for g in grid:
        if min(abs(g - b) for b in fixed) > far:
            bounds.add(int(g))
    return np.array(sorted(bounds) + [len(m)]), set(int(o) for o in on)


def _pitch_changes(f0: np.ndarray, fr: np.ndarray, step: float) -> list[int]:
    """Сэмплы, где нота баса сменилась и держится ≥ 5 кадров (~116 мс): кадры yin (шаг step сэмплов),
    тихие (< 5 % самого громкого) не считаются. Октава — тоже смена: дрожь yin отсекает устойчивость
    5 кадров, а октавные ходы у баса обычны."""
    from scipy.ndimage import median_filter
    voiced = fr > 0.05 * max(float(fr.max()), EPS)
    if voiced.sum() < 3:
        return []
    midi = 12 * np.log2(np.maximum(f0, EPS) / 440.0) + 69
    tun = float(np.median((midi[voiced] - np.round(midi[voiced]) + 0.5) % 1 - 0.5))
    q = median_filter(np.round(midi - tun), 3)
    run = 5                      # короче — дрожь yin на мутной дорожке (#466: 175 «смен» за минуту)
    out, last = [], None
    for k in range(len(q) - run + 1):
        if not voiced[k:k + run].all() or not (q[k:k + run] == q[k]).all():
            continue
        if last is not None and q[k] != last:
            out.append(int(k * step))
        last = q[k]
    return out


def _band_follow(y: np.ndarray, ref: np.ndarray, sr: int) -> np.ndarray:
    """Громкость y по времени — как у ref, отдельно до 200 Гц и выше (окно 0,5 с, ±15 дБ):
    у сэмплов другой баланс низа и середины, а одна общая громкость давала «бас мешает под голосом»."""
    from scipy import signal
    from scipy.ndimage import uniform_filter1d
    sos = signal.butter(4, 200, "lowpass", fs=sr, output="sos")
    w = max(1, int(0.5 * sr))
    env = lambda v: np.sqrt(uniform_filter1d(v * v, w) + 1e-12)
    out = np.zeros_like(y)
    lo_y, lo_r = _zero_phase(sos, y[:, None])[:, 0], _zero_phase(sos, ref[:, None])[:, 0]
    for a, b in ((lo_y, lo_r), (y - lo_y, ref - lo_r)):     # верх — остаток: полосы в сумме — целое
        g0 = _rms(b) / max(_rms(a), EPS)                        # общий уровень полосы, а по времени —
        out += a * g0 * np.clip(env(b) / (env(a) * g0), _db(-BASS_FOLLOW_DB), _db(BASS_FOLLOW_DB))  # ±предел
    # где y молчит, полосы — только предзвон фильтра нулевой фазы, а подгонка в тишине его усиливает:
    # выход — лишь там, где звучит y (запас 1 мс: предзвон перед атакой — тоже «звук раньше удара»)
    k = max(1, int(0.001 * sr))
    live = np.abs(y) > 1e-6 * max(float(np.abs(y).max()), EPS)
    return out * (uniform_filter1d(live.astype(float), 2 * k + 1) > 0)


def _bass(x, sr, p, res):
    """Замена баса сэмплами бас-гитары: ячейки — доли дорожки (division на долю) и её удары; в ячейке —
    нота дорожки (медиана частоты, строй учтён); новый щипок — на ударе, смене ноты или после тишины,
    иначе нота тянется. Атака сэмпла — на начале удара (без сдвига); громкость и баланс низ/верх
    следуют за входом во времени (+ output_db)."""
    import librosa
    raw, ksr = _resource(res, "kit", p["kit"])
    kit = _kit_notes(raw, int(ksr))
    if not kit:
        raise ChainError(f"набор {p['kit']!r}: нет сэмплов с высотой")
    n = x.shape[0]
    m = x.mean(axis=1)
    y = np.zeros(n)
    if _rms(m) <= EPS:
        return np.zeros_like(x)
    # высота: yin на пониженной частоте (быстро); кадры тише порога не голосуют
    dsr = 11025
    md = _resample(m[:, None], sr, dsr)[:, 0]
    hop = 256
    with np.errstate(invalid="ignore"):       # то же предупреждение librosa/numba, см. _kit_notes
        f0 = librosa.yin(md, fmin=30, fmax=400, sr=dsr, frame_length=2048, hop_length=hop, center=True)
    fr = librosa.feature.rms(y=md, frame_length=2048, hop_length=hop, center=True)[0]
    bounds, onsets = _bass_cells(m, sr, int(round(p["division"])), _pitch_changes(f0, fr, hop * sr / dsr))
    cells = []
    for c0, c1 in zip(bounds[:-1], bounds[1:], strict=True):
        a, b = c0 * dsr // sr // hop, max(c0 * dsr // sr // hop + 1, c1 * dsr // sr // hop)
        k0, k1 = a + (b - a) // 5, b - (b - a) // 5             # края ячейки — переходы, не голосуют
        rf = fr[k0:max(k1, k0 + 1)]
        sel = f0[k0:max(k1, k0 + 1)][rf > 0.3 * max(rf.max(), EPS)]   # по своей громкости: тихая нота — тоже
        cells.append([int(c0), int(c1), float(librosa.hz_to_midi(np.median(sel))) if len(sel) else None,
                      _rms(m[c0:c1])])
    loud = np.percentile([c[3] for c in cells], 90)
    for c in cells:
        if c[3] < loud * _db(p["floor_db"]):
            c[2] = None
    voiced = [c[2] for c in cells if c[2] is not None]
    if not voiced:
        return np.zeros_like(x)
    tun = float(np.median([(v - round(v) + 0.5) % 1 - 0.5 for v in voiced]))   # строй дорожки, полутона
    lo = min(k[0] for k in kit)
    for c in cells:
        if c[2] is not None:
            q = int(round(c[2] - tun))
            while q > lo + 24:
                q -= 12
            while q < lo - 0.5:
                q += 12
            c[2] = q
    for i in range(1, len(cells) - 1):                          # одиночный выброс — к соседям
        a, b, c = cells[i - 1][2], cells[i][2], cells[i + 1][2]
        if a is not None and a == c and b is not None and b != a:
            cells[i][2] = a
    # ноты: щипок на ударе, смене ноты или после тишины; иначе ячейка продлевает ноту
    notes = []
    for c0, c1, q, e in cells:
        if q is None:
            continue
        if notes and notes[-1][1] == c0 and notes[-1][2] == q and c0 not in onsets:
            notes[-1][1] = c1
        else:
            notes.append([c0, c1, q, e])
    es = np.array([nt[3] for nt in notes])
    ranks = np.argsort(np.argsort(es)) / max(len(es) - 1, 1)
    groups = {}
    for k in kit:
        groups.setdefault(int(round(k[0])), []).append(k)
    for g in groups.values():
        g.sort(key=lambda k: k[1])
    rel = int(BASS_RELEASE_S * sr)
    # конец ноты — где вход замолк: ячейка сетки бывает длиннее звука, и нота тянулась бы в тишину
    from scipy.ndimage import uniform_filter1d
    # без maximum крохи < 0 после сглаживания давали NaN, NaN-процентиль выключал обрезку — бас звучал в паузах
    env = np.sqrt(np.maximum(uniform_filter1d(m * m, max(1, int(0.02 * sr))), 0.0))
    quiet = env < np.percentile(env, 90) * _db(p["floor_db"] - 10)
    for nt in notes:
        after = np.flatnonzero(quiet[nt[0] + int(0.02 * sr):nt[1]])
        if len(after):
            nt[1] = max(nt[0] + 1, nt[0] + int(0.02 * sr) + int(after[0]) - rel)
    prev: dict = {}                                             # группа → дубль предыдущего щипка
    for i, ((t0, t1, q, _), r) in enumerate(zip(notes, ranks, strict=True)):
        gk = min(groups, key=lambda kk: abs(kk - q))
        g = groups[gk]
        j = min(len(g) - 1, max(0, int(round(r * (len(g) - 1))) + (i % 3) - 1))   # слой + чередование дублей
        if len(g) > 1 and prev.get(gk) == j:                   # ранг съел чередование — соседний дубль
            j = j - 1 if j > 0 else j + 1
        prev[gk] = j
        midi_s, _, att, s = g[j]
        ratio = 2 ** ((q + tun - midi_s) / 12) * ksr / sr      # шаг по сэмплу на сэмпл выхода
        length = min(t1 - t0 + rel, n - t0)
        idx = att + np.arange(length) * ratio
        idx = idx[idx < len(s) - 1]
        if not len(idx):
            continue
        w = np.interp(idx, np.arange(len(s)), s)
        f = min(rel, len(w))
        w[-f:] *= np.linspace(1, 0, f)                          # глушение струны к следующей ноте
        y[t0:t0 + len(w)] += w
    y = _band_follow(y, m, sr) * _db(p["output_db"])
    return np.repeat(y[:, None], x.shape[1], axis=1)


def _gain(x, sr, p, _res):
    """Громкость: перегруз и усилитель выравнивают выход по своему входу — после узкой
    полосы эквалайзера звук тихий, поднять его в цепочке нечем было, кроме компрессора."""
    return x * _db(p["gain_db"])


BLOCKS = {"gate": _gate, "eq": _eq, "comp": _comp, "drive": _drive, "amp": _amp,
          "cab": _cab, "reverb": _reverb, "delay": _delay, "gain": _gain, "sampler": _sampler,
          "bass": _bass}


def process(audio, sr: int, chain, resources=None) -> np.ndarray:
    """Цепочка по порядку. audio (n,) или (n, ch) → та же форма и длина, float32, без сдвига."""
    blocks = parse_chain(chain)
    a = np.asarray(audio)
    mono = a.ndim == 1
    x = (a[:, None] if mono else a).astype(np.float64)
    n = x.shape[0]
    for b in blocks:
        if n == 0:
            break
        x = _fit(BLOCKS[b["type"]](x, int(sr), b, resources), n)
    out = x[:, 0] if mono else x
    return out.astype(np.float32)


__all__ = ["ChainError", "SPEC", "parse_chain", "needs_gpu", "process"]
