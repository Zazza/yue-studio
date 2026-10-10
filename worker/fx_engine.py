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
import re
import zlib
from fractions import Fraction
from pathlib import Path

import numpy as np
from scipy import signal
from scipy.ndimage import maximum_filter1d, minimum_filter1d, uniform_filter1d


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
# версия звучания движка — в ключе кэша превью /fx: поднимать при правке звука блоков, иначе «▶ стало» отдаст
# превью, посчитанное до правки (2 — лента без подъёма тихого сигнала, этап 7б; 3 — уровень synth «на ухо», этап 9)
ENGINE_VERSION = 3
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


def _parse_notes(where: str, notes) -> list[dict]:
    """Ноты блока synth: [{t (с, < 0 — началась до окна), d > 0, midi [0…127], vel 0…1}], не больше
    SYNTH_MAX_NOTES."""
    if not isinstance(notes, list):
        raise ChainError(f"{where}: notes — список нот {{t, d, midi, vel}}")
    if len(notes) > SYNTH_MAX_NOTES:
        raise ChainError(f"{where}: нот не больше {SYNTH_MAX_NOTES} (сейчас {len(notes)})")
    out = []
    for i, nt in enumerate(notes):
        w = f"{where}, нота {i + 1}"
        if not isinstance(nt, dict) or not _num(nt.get("t")) or not _num(nt.get("d")) or nt["d"] <= 0:
            raise ChainError(f"{w}: нужны t (с) и d > 0 (с)")
        midi = nt.get("midi")
        if isinstance(midi, (int, float)) and not isinstance(midi, bool):
            midi = [midi]
        if not isinstance(midi, list) or not midi or not all(_num(m) and 0 <= m <= 127 for m in midi):
            raise ChainError(f"{w}: midi — список номеров нот 0…127")
        if len(midi) > SYNTH_MAX_CHORD or nt["t"] < SYNTH_MIN_T:
            raise ChainError(f"{w}: не больше {SYNTH_MAX_CHORD} нот в аккорде, начало не раньше {SYNTH_MIN_T:g} с")
        vel = nt.get("vel", 0.8)
        if not _num(vel) or not 0 <= vel <= 1:
            raise ChainError(f"{w}: vel — 0…1")
        out.append({"t": float(nt["t"]), "d": float(nt["d"]), "midi": [float(m) for m in midi], "vel": float(vel)})
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
        allowed = set(nums) | set(strs) | {"type"} | ({"bands"} if t == "eq" else set()) | \
            ({"notes", "_until", "_ref_rms", "_syn_rms", "_syn_peak"} if t in ("synth", "perc") else set()) | \
            ({"_t0"} if t in ("perc", "tremolo", "synth") else set())
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
        if t in ("synth", "perc"):
            norm["notes"] = (_parse_notes if t == "synth" else _parse_hits)(where, blk.get("notes", []))
            if _num(blk.get("_until")):   # край окна превью (ставит воркер); зажим — огромное не ломает расчёт
                norm["_until"] = min(max(float(blk["_until"]), 0.0), 86400.0)
            for k in ("_ref_rms", "_syn_rms", "_syn_peak"):   # общий уровень партии (ставит воркер); зажим 0…1e3
                if _num(blk.get(k)):
                    norm[k] = min(max(float(blk[k]), 0.0), 1000.0)
            if t == "perc" and _num(blk.get("_t0")):   # начало окна в треке (ставит воркер): сдвиги «по-живому»
                norm["_t0"] = min(max(float(blk["_t0"]), 0.0), 86400.0)
            if t == "perc" and int(norm["voice"]) == 0 and not norm.get("kit"):
                raise ChainError(f"{where}: голос 0 (сэмплы) — нужен набор kit")
        if t in ("tremolo", "synth") and _num(blk.get("_t0")):   # начало куска в треке (ставит воркер): фазы от трека
            norm["_t0"] = min(max(float(blk["_t0"]), 0.0), 86400.0)
        if t == "sampler" and norm.get("kit_open") and (norm.get("kit_mid") or norm.get("kit_low")):
            raise ChainError(f"{where}: kit_open (открытые удары) нельзя вместе с kit_mid/kit_low (тамы по высоте)")
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


def a_weight(x: np.ndarray, sr: int) -> np.ndarray:
    """Сигнал через кривую A (IEC 61672: ухо глухо к низу и чувствительно к 2–5 кГц) — громкость «на ухо» для уровня
    синт-партии к треку: при равном простом RMS яркий синт слышен громче тёплого на 5–8 дБ (замер «Gone», 2026-10-10).
    Ось 0 — время; форма та же. Фильтр причинный: для RMS сдвиг фазы не важен. Билинейное преобразование занижает
    верх (44,1 кГц: 10 кГц −4,0 дБ при норме −2,5; 16 кГц −15,2 при −6,6) — очень яркая партия чуть громче цели."""
    f1, f2, f3, f4 = 20.598997, 107.65265, 737.86223, 12194.217
    z = [0.0, 0.0, 0.0, 0.0]
    p = [-2 * np.pi * f1, -2 * np.pi * f1, -2 * np.pi * f4, -2 * np.pi * f4, -2 * np.pi * f2, -2 * np.pi * f3]
    k = (2 * np.pi * f4) ** 2 * 10 ** (1.9997 / 20)
    zd, pd, kd = signal.bilinear_zpk(z, p, k, sr)
    sos = signal.zpk2sos(zd, pd, kd)
    return signal.sosfilt(sos, np.asarray(x, dtype=np.float64), axis=0)


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
    pre = int(round(p["predelay_ms"] / 1000 * sr))
    if p.get("gate_ms", 0) > 0:
        # гейт-реверб 80-х: отклик обрывается через gate_ms от удара (спад 5 мс внутри) — уровень входа не важен,
        # в отличие от гейта по порогу после реверба (тихая дорожка не открыла бы его ни разу)
        g = int(round(p["gate_ms"] / 1000 * sr)) - max(pre, 1)
        if g <= 1:   # обрыв раньше начала отклика (gate_ms ≤ predelay_ms) — реверба нет, а не одиночное эхо
            return x
        ir = ir[:g].copy()
        f = min(len(ir), int(0.005 * sr))
        ir[len(ir) - f:] *= np.linspace(1.0, 0.0, f)[:, None]
    ir = _norm_energy(ir)
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


TOM_FMIN, TOM_FMAX = 50.0, 500.0   # sampler: высота удара тамов — максимум спектра в этой полосе
TOM_WIN_S = 0.06                    # … по первым 60 мс после пика
TOM_MERGE = 1.12                    # соседние группы ближе ~2 полутонов — один и тот же там
TOM_MIN_SHARE = 0.05                # группа меньше 5 % ударов — выброс, не отдельный там


def _hit_pitch(mono: np.ndarray, t: int, sr: int) -> float:
    """Высота удара: частота максимума спектра 50–500 Гц на TOM_WIN_S после пика (окно Ханна,
    дополнение нулями до ~1 Гц на бин)."""
    seg = mono[t:t + int(TOM_WIN_S * sr)]
    if len(seg) < 16:
        return 0.0
    nfft = max(len(seg), sr)
    spec = np.abs(np.fft.rfft(seg * np.hanning(len(seg)), nfft))
    f = np.fft.rfftfreq(nfft, 1.0 / sr)
    band = (f >= TOM_FMIN) & (f <= TOM_FMAX)
    return float(f[band][np.argmax(spec[band])]) if band.any() else 0.0


def _best_split(v: np.ndarray, k: int) -> list[int]:
    """Точное разбиение отсортированных значений v на k подряд идущих групп с наименьшей суммой квадратов
    отклонений (одномерные k-средних, динамика по префиксным суммам) → индексы начала групп 1…k−1."""
    n = len(v)
    s1 = np.concatenate([[0.0], np.cumsum(v)])
    s2 = np.concatenate([[0.0], np.cumsum(v * v)])

    best = np.full((k + 1, n + 1), np.inf)
    cut = np.zeros((k + 1, n + 1), dtype=int)
    best[0][0] = 0.0
    for g in range(1, k + 1):
        for j in range(g, n + 1):
            i = np.arange(g - 1, j)                      # начало последней группы v[i:j]
            c = best[g - 1][i] + s2[j] - s2[i] - (s1[j] - s1[i]) ** 2 / (j - i)
            m = int(np.argmin(c))
            best[g][j], cut[g][j] = c[m], i[m]
    starts, j = [], n
    for g in range(k, 0, -1):
        j = cut[g][j]
        starts.append(j)
    return sorted(starts)[1:]


def _pitch_groups(pitches: list[float], nkits: int) -> tuple[list[int], int]:
    """Номер группы каждого удара по высоте (0 — самая низкая) и число групп: лучшее разбиение по логарифму
    высоты (одномерные k-средних, точно — без выбора стартовых центров: от него зависели и пустые группы, и
    захват центра выбросом), k — от числа наборов вниз, пока центры соседних групп не дальше TOM_MERGE или в
    группе меньше TOM_MIN_SHARE ударов. Не «цепочкой»: на живой дорожке (протечка, #488) цепочка склеивала
    всё в одну группу (314 из 315)."""
    lp = np.log(np.maximum(np.asarray(pitches, dtype=np.float64), 1e-6))
    n = len(lp)
    order = np.argsort(lp, kind="stable")
    v = lp[order]
    for k in range(min(nkits, len(np.unique(v))), 1, -1):
        bounds = [0] + _best_split(v, k) + [n]
        sizes = np.diff(bounds)
        centres = [float(v[bounds[i]:bounds[i + 1]].mean()) for i in range(k)]
        if np.all(np.diff(centres) >= np.log(TOM_MERGE)) and sizes.min() >= max(1, TOM_MIN_SHARE * n):
            out = [0] * n
            for g in range(k):
                for idx in order[bounds[g]:bounds[g + 1]]:
                    out[int(idx)] = g
            return out, k
    return [0] * n, 1


def _sampler(x, sr, p, res):
    """Замена ударов сэмплами набора: сила удара → слой по рангу (соседние удары — соседние
    слои по кругу), пик сэмпла — на пик удара (без сдвига); громкость — как у входа.
    kit_open — второй набор для долго звучащих ударов (открытый хэт): удар звучит во входе (до −20 дБ,
    не дальше следующего удара) дольше, чем среднее геометрическое типичных времён звучания сэмплов
    обоих наборов, — берётся kit_open. choke — новый удар глушит предыдущий (педаль хэта)."""
    n, ch = x.shape
    pitched = bool(p.get("kit_mid") or p.get("kit_low"))
    if pitched and p.get("kit_open"):
        raise ChainError("sampler: kit_open (открытые удары) нельзя вместе с kit_mid/kit_low (тамы по высоте)")
    sets = [_sampler_layers(res, p["kit"], sr, ch)]
    if pitched:
        # тамы по высоте: наборы от высокого к низкому (kit — малый, kit_mid — средний, kit_low — большой)
        names = [p["kit"]] + [p[k] for k in ("kit_mid", "kit_low") if p.get(k)]
        sets = [_sampler_layers(res, nm, sr, ch) for nm in names]
    elif p.get("kit_open"):
        sets.append(_sampler_layers(res, p["kit_open"], sr, ch))
    y = np.zeros_like(x)
    hits = _hits(x, sr, p["floor_db"])
    if not hits:
        return y
    kind = [0] * len(hits)
    if pitched:
        mono = x.mean(axis=1)
        grp, ng = _pitch_groups([_hit_pitch(mono, t, sr) for t, _ in hits], len(sets))
        low = len(sets) - 1                              # индекс набора kit_low (или kit_mid, если low нет)
        mid = 1 if p.get("kit_mid") else 0
        for i, g in enumerate(grp):
            top = ng - 1 - g                             # 0 — самая высокая группа
            if ng == 1:
                kind[i] = mid
            elif top == 0:
                kind[i] = 0
            elif g == 0:
                kind[i] = low
            else:
                kind[i] = mid                            # средняя группа — бывает только при трёх наборах
    elif len(sets) > 1:
        typ = [float(np.median([_ring(_env5(np.abs(s).mean(axis=1), sr), at, len(s)) for _, at, s, _ in L]))
               for L in sets]
        edge = np.sqrt(max(typ[0], 1.0) * max(typ[1], 1.0))
        env = _env5(np.abs(x).mean(axis=1), sr)
        for i, (t, _) in enumerate(hits):
            end = hits[i + 1][0] if i + 1 < len(hits) else n
            kind[i] = int(_ring(env, t, end) >= edge)
    ps = np.array([h[1] for h in hits])
    ranks = np.argsort(np.argsort(ps)) / max(len(ps) - 1, 1)
    # динамика: сила удара вокруг медианы в степени dynamics (1 — как сыграно, 0 — машина, 2 — призрачные
    # тише, акценты громче); слой — по рангу, как раньше
    dyn = float(p.get("dynamics", 1.0))
    if dyn != 1.0:
        med = float(np.median(ps))
        hits = [(t, med * (pk / med) ** dyn if med > EPS and pk > EPS else pk) for t, pk in hits]
    fade = max(1, int(0.01 * sr))
    turn = [0] * len(sets)                              # чередование слоёв — своё у каждого набора
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


KIT_MIDI_NAME = re.compile(r"^m(\d{1,3})$")   # синтезированные наборы: m<MIDI>.wav — точная высота


def _kit_notes(raw, ksr: int, names=None) -> list[tuple[float, float, int, np.ndarray]]:
    """Сэмплы набора баса: (высота MIDI, пик, начало атаки, моно-сэмпл, высота из имени?). Высота — из имени
    m<MIDI> (синтезированные наборы), иначе по самому звуку (yin). Длинные сэмплы режутся до BASS_SAMPLE_S."""
    import librosa
    out = []
    names = list(names) if names is not None and len(names) == len(raw) else [None] * len(raw)
    for s, nm in zip(raw, names, strict=True):
        s = np.asarray(s, dtype=np.float32)   # 224 сэмпла по 6 с: float64 — лишние 240 МБ
        s = (s.mean(axis=1) if s.ndim > 1 else s)[:int(BASS_SAMPLE_S * ksr)]
        peak = float(np.abs(s).max()) if len(s) else 0.0
        if peak <= 0:
            continue
        a = int(np.argmax(np.abs(s) > 0.1 * peak))          # начало атаки
        m = KIT_MIDI_NAME.match(nm or "")
        if m:
            out.append((float(m.group(1)), peak, a, s, True))
            continue
        body = s[a + int(0.05 * ksr):a + int(0.6 * ksr)]    # после щелчка струны — тон
        if len(body) < 2048:
            continue
        # librosa предупреждает, что 25 Гц не влезает в кадр дважды; на Growlybass замер сверен с pyin
        # (C#1/E1 — те же отклонения: это расстройка самих сэмплов, её и компенсирует сдвиг высоты)
        with np.errstate(invalid="ignore"):   # librosa/numba: «invalid value in cast» при первой компиляции
            f0 = librosa.yin(body, fmin=25, fmax=500, sr=ksr, frame_length=2048)
        out.append((float(librosa.hz_to_midi(np.median(f0))), peak, a, s, False))
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

    def env(v):
        return np.sqrt(uniform_filter1d(v * v, w) + 1e-12)

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
    # имена файлов — у хранилища воркера (синтезированные наборы m<MIDI>: высота точная); нет — замер по звуку
    names = res.kit_names(p["kit"]) if hasattr(res, "kit_names") else None
    kit = _kit_notes(raw, int(ksr), names)
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
    # высоты всех сэмплов из имён — ноты сворачиваются по диапазону набора; иначе (замер yin) — как раньше: выше
    # двух октав над нижним сэмплом — вниз (замер баса ошибается октавой вверх), ниже нижнего — вверх
    # граница сверху: по именам — верхний сэмпл (+0,5: целая нота на краю не сворачивается); по замеру — прежнее
    # «нижний + 24» без допуска (lo дробный: growlybass 24,97 — допуск сдвинул бы границу на полтона)
    lim = max(k[0] for k in kit) + 0.5 if all(k[4] for k in kit) else lo + 24
    for c in cells:
        if c[2] is not None:
            q = int(round(c[2] - tun))
            while q > lim:
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
        midi_s, _, att, s, _named = g[j]
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


# ---------- синтезатор и эффекты для синтов (этап 4) ----------
SYNTH_MAX_NOTES = 5000   # нот в блоке synth не больше: партия на весь трек — сотни
SYNTH_MAX_CHORD = 16     # нот в одном аккорде не больше
SYNTH_MIN_T = -600.0     # нота не раньше 10 мин до окна: считается целиком от начала — длинная съела бы память
SYNTH_EDGE_S = 0.01      # нарастание/спад на краях окна: нота, идущая через край, без щелчка


def _polyblep(ph, dt):
    """Поправка PolyBLEP для разрыва в фазе 0 (ph — фаза 0..1, dt — шаг фазы)."""
    out = np.zeros_like(ph)
    a = ph < dt
    t = ph[a] / dt[a]
    out[a] = t + t - t * t - 1.0
    b = ph > 1.0 - dt
    t = (ph[b] - 1.0) / dt[b]
    out[b] = t * t + t + t + 1.0
    return out


def _osc(wave: int, f: np.ndarray, sr: int, pwm: float = 0.5, phase0: float = 0.0, fm=None) -> np.ndarray:
    """Генератор с переменной частотой f (Гц, по сэмплам): 0 пила, 1 квадрат, 2 пульс (ширина pwm),
    3 треугольник, 4 синус, 5 FM (синус, фаза которого качается синусом в fm[0] раз выше с глубиной
    fm[1]·e^(−t/fm[2]) от начала ноты — электропиано, колокольчики); разрывы — с поправкой PolyBLEP."""
    if wave == 5:
        ratio, index, decay = fm if fm is not None else (1.0, 0.0, 1.0)
        ph = phase0 + np.cumsum(f / sr)        # без свёртки по модулю: модулятор — от той же фазы
        idx = index * np.exp(-np.arange(len(f)) / sr / max(decay, 1e-3))
        return np.sin(2 * np.pi * ph + idx * np.sin(2 * np.pi * ratio * ph))
    dt = np.clip(f / sr, 1e-6, 0.49)
    ph = (phase0 + np.cumsum(dt)) % 1.0
    if wave == 4:
        return np.sin(2 * np.pi * ph)
    if wave == 0:
        return 2.0 * ph - 1.0 - _polyblep(ph, dt)
    width = 0.5 if wave in (1, 3) else float(np.clip(pwm, 0.05, 0.95))
    sq = np.where(ph < width, 1.0, -1.0) + _polyblep(ph, dt) - _polyblep((ph - width) % 1.0, dt)
    if wave == 3:   # треугольник — интеграл квадрата (с утечкой)
        tri = np.cumsum(sq * 4 * dt)
        tri = signal.lfilter([1, -1], [1, -0.999], tri)
        return tri / (np.abs(tri).max() + EPS)
    return sq


def _adsr(n_on: int, n_total: int, sr: int, a, d, s, r) -> np.ndarray:
    """Огибающая длиной n_total: атака/спад/удержание до n_on, затем затухание r."""
    env = np.zeros(n_total)
    na, nd = max(1, int(a * sr)), max(1, int(d * sr))
    k = np.arange(n_on)
    on = np.where(k < na, k / na, np.where(k < na + nd, 1 - (1 - s) * (k - na) / nd, s))
    env[:n_on] = on
    last = on[-1] if n_on else 0.0
    nr = n_total - n_on
    if nr > 0:
        env[n_on:] = last * np.exp(-np.arange(nr) / max(1.0, r * sr / 6.9))   # −60 дБ за r
    return env


def _lowpass_sweep(x, sr, cut_lo, cut_hi, mod, res):
    """НЧ-фильтр с изменяемой во времени частотой среза: смесь двух неподвижных фильтров (закрытого и
    открытого) по огибающей mod 0..1 — дёшево и без пошагового пересчёта коэффициентов."""
    q = 0.707 + res * 8
    def lp(c):
        c = float(np.clip(c, 20, sr * 0.45))
        b, a = signal.iirfilter(2, c, btype="low", ftype="butter", fs=sr) if res <= 0 else _rbj_lp(c, q, sr)
        return signal.lfilter(b, a, x)
    if abs(cut_hi - cut_lo) < 1 or np.ptp(mod) < 1e-3:
        return lp(cut_lo + (cut_hi - cut_lo) * float(np.mean(mod)))
    lo, hi = lp(cut_lo), lp(cut_hi)
    return lo + (hi - lo) * mod


def _rbj_lp(f0, q, sr):
    w = 2 * np.pi * f0 / sr
    al = np.sin(w) / (2 * q)
    c = np.cos(w)
    b = np.array([(1 - c) / 2, 1 - c, (1 - c) / 2])
    a = np.array([1 + al, -2 * c, 1 - al])
    return b / a[0], a / a[0]


NOTE_NAME = re.compile(r"^([A-Ga-g])([#b]?)(-?\d)(?:v\d+)?$")   # «A0v10», «C#4v4», «Eb3» → MIDI
SEMITONE = {"c": 0, "d": 2, "e": 4, "f": 5, "g": 7, "a": 9, "b": 11}


def sample_midi(name: str) -> int | None:
    """Высота сэмпла по имени файла: m<MIDI> (синтезированные наборы) или нота с октавой («A0v10» = 21)."""
    m = KIT_MIDI_NAME.match(name or "")
    if m:
        return int(m.group(1))
    m = NOTE_NAME.match(name or "")
    if not m:
        return None
    return 12 * (int(m.group(3)) + 1) + SEMITONE[m.group(1).lower()] + {"#": 1, "b": -1, "": 0}[m.group(2)]


KIT_TAIL_FADE_S = 0.05   # спад в конце сэмпла набора synth


def _note_phase(t_abs: float, midi: float, voice: int) -> float:
    """Начальная фаза генератора ноты — от её места в треке (мс), высоты и голоса унисона, а не от порядка нот:
    превью окна, где ранние ноты отброшены, звучит той же формой, что трек."""
    key = f"{int(round(t_abs * 1000.0))}/{float(midi):g}/{voice}".encode()
    return float(np.random.default_rng(zlib.crc32(key)).random())


def _synth_bank(res, kit: str, sr: int) -> list[tuple[int, np.ndarray]]:
    """Сэмплы набора для synth: [(MIDI из имени, моно-сэмпл на частоте sr)], по высоте; без нот в именах — ошибка."""
    raw, ksr = _resource(res, "kit", kit)
    names = res.kit_names(kit) if hasattr(res, "kit_names") else []
    bank = []
    for s, nm in zip(raw, names if len(names) == len(raw) else [None] * len(raw), strict=True):
        midi = sample_midi(nm)
        if midi is None:
            continue
        s = np.asarray(s, dtype=np.float64)
        s = s.mean(axis=1) if s.ndim > 1 else s
        s = _resample(s[:, None], int(ksr), sr)[:, 0].copy()
        f = min(len(s), int(KIT_TAIL_FADE_S * sr))   # обрезанный по max_s сэмпл гаснет, а не обрывается щелчком
        if f:
            s[len(s) - f:] *= np.linspace(1.0, 0.0, f)
        bank.append((midi, s))
    if not bank:
        raise ChainError(f"набор {kit!r}: нет сэмплов с нотой в имени (A0v10, C#4, m28…)")
    bank.sort(key=lambda b: b[0])
    return bank


def _kit_voice(bank, midi: int, m: int) -> np.ndarray:
    """Нота сэмплом: ближайший по высоте, пересчёт частоты (линейная интерполяция), длина m, дальше — тишина."""
    ms, s = min(bank, key=lambda b: (abs(b[0] - midi), b[0]))
    pos = np.arange(m) * 2 ** ((midi - ms) / 12)
    return np.interp(pos, np.arange(len(s)), s, right=0.0)


def _synth_notes(n: int, sr: int, ch: int, p: dict, res=None) -> np.ndarray:
    notes = p.get("notes") or []
    bank = _synth_bank(res, p["kit"], sr) if p.get("kit") else None
    y = np.zeros(n)
    rel_n = int(p["release_s"] * sr)
    rng = np.random.default_rng(12345)
    uni = int(p["unison"])
    dets = np.linspace(-1, 1, uni) * p["detune_cents"] if uni > 1 else np.zeros(1)
    for nt in notes:
        a = int(round(nt["t"] * sr))
        # нота кончилась до окна — не играется: иначе её затухание (одно в окне) нормировка ниже вытянула бы
        # до −6 дБFS; нота, начатая до окна и звучащая в нём, играется с середины
        if a >= n or nt["t"] + nt["d"] <= 0:
            continue
        n_on = max(1, int(round(nt["d"] * sr)))
        b = min(n, a + n_on + rel_n)
        if b <= max(a, 0):
            continue
        m = b - a
        t = np.arange(m) / sr
        vib = 2 ** (p["vib_cents"] / 1200 * np.sin(2 * np.pi * p["vib_rate"] * t)) if p["vib_cents"] > 0 else 1.0
        v = np.zeros(m)
        for midi in (nt["midi"] if bank is None else ()):
            f0 = 440.0 * 2 ** ((midi - 69) / 12)
            for vi, dc in enumerate(dets):
                f = f0 * 2 ** (dc / 1200) * vib * np.ones(m)
                ph0 = _note_phase(nt["t"] + float(p.get("_t0", 0.0)), midi, vi)
                fm = (p["fm_ratio"], p["fm_index"], p["fm_decay_s"])
                o = _osc(int(p["osc1"]), f, sr, p["pwm"], ph0, fm) * (1 - p["osc_mix"])
                if p["osc_mix"] > 0:
                    o += _osc(int(p["osc2"]), f * 2 ** (p["osc2_semi"] / 12), sr, p["pwm"], ph0, fm) * p["osc_mix"]
                v += o / len(dets)
            if p["sub"] > 0:
                v += p["sub"] * _osc(1, f0 / 2 * np.ones(m), sr)
        if bank is not None:   # сэмплы набора вместо генераторов: суб и шум не звучат
            for midi in nt["midi"]:
                v += _kit_voice(bank, int(midi), m)
        elif p["noise"] > 0:
            v += p["noise"] * rng.uniform(-1, 1, m)
        n_on_c = min(n_on, m)
        env = _adsr(n_on_c, m, sr, p["attack_s"], p["decay_s"], p["sustain"], p["release_s"])
        fenv = _adsr(n_on_c, m, sr, p["f_attack_s"], p["f_decay_s"], 0.0, p["release_s"])
        lfo = 0.5 * (1 + np.sin(2 * np.pi * max(p["vib_rate"], 0.1) * t))
        mod = np.clip(p["env_amount"] * fenv + p["lfo_cutoff"] * lfo, 0, 1)
        cut = p["cutoff_hz"]
        v = _lowpass_sweep(v, sr, cut, min(16000.0, cut * 8), mod, p["resonance"])
        vel = float(nt.get("vel", 0.8))
        seg = v * env * vel
        lo = max(0, a)
        y[lo:b] += seg[lo - a:]
    pk = np.abs(y).max()
    if pk > EPS and not p.get("_raw"):   # _raw — без нормировки по пику окна (уровень задаёт общий множитель партии)
        y *= 10 ** (-6 / 20) / pk * 10 ** (p["output_db"] / 20)
    # край окна: _until (секунды; ставит воркер для превью) — синт молчит дальше, затухание не заходит в хвост
    # эффектов; без него — конец буфера
    stop = min(n, int(round(p.get("_until", n / sr) * sr)))
    y[stop:] = 0.0
    e = min(int(SYNTH_EDGE_S * sr), max(stop, 1) // 2)
    if e > 0:
        ramp = np.linspace(0.0, 1.0, e, endpoint=False)
        y[:e] *= ramp
        y[stop - e:stop] *= ramp[::-1]
    return np.repeat(y[:, None], ch, axis=1)


def _mod_delay(x, sr, base_ms, depth_ms, rate, phase):
    n = len(x)
    t = np.arange(n) / sr
    d = (base_ms + depth_ms * 0.5 * (1 + np.sin(2 * np.pi * rate * t + phase))) * sr / 1000
    idx = np.arange(n) - d
    return np.interp(idx, np.arange(n), x, left=0.0)


def _chorus(x, sr, p, _res=None):
    """Ансамбль: несколько копий через задержки 7–20 мс, качаемые LFO с разной фазой; L и R — разные фазы."""
    n, ch = x.shape
    out = np.zeros_like(x)
    vcs = int(p["voices"])
    for c in range(ch):
        src = x.mean(axis=1) if ch > 1 else x[:, 0]
        wet = np.zeros(n)
        for v in range(vcs):
            ph = 2 * np.pi * (v / vcs + (0.25 if c == 1 else 0.0))
            wet += _mod_delay(src, sr, 7.0, p["depth_ms"], p["rate_hz"] * (1 + 0.07 * v), ph)
        wet /= max(vcs, 1)
        out[:, c] = x[:, c] * (1 - p["mix"] * 0.5) + wet * p["mix"]
    return out


def _flanger(x, sr, p, _res=None):
    n, ch = x.shape
    out = np.zeros_like(x)
    for c in range(ch):
        src = x[:, c]
        wet = _mod_delay(src, sr, p["delay_ms"], p["depth_ms"], p["rate_hz"], 0.5 * np.pi * c)
        if p["feedback"] > 0:   # простая обратная связь: ещё один проход через ту же задержку
            wet = wet + p["feedback"] * _mod_delay(wet, sr, p["delay_ms"], p["depth_ms"], p["rate_hz"], 0.5 * np.pi * c)
        out[:, c] = src * (1 - p["mix"] * 0.5) + wet * p["mix"]
    return out


def _phaser(x, sr, p, _res=None, block=256):
    """Цепочка all-pass первого порядка с частотой, качаемой LFO (пересчёт по блокам, состояние сохраняется)."""
    n, ch = x.shape
    out = np.zeros_like(x)
    stages = int(p["stages"])
    for c in range(ch):
        src = x[:, c]
        y = np.zeros(n)
        z = np.zeros(stages)
        fb = 0.0
        for s0 in range(0, n, block):
            s1 = min(n, s0 + block)
            tt = (s0 / sr)
            lfo = 0.5 * (1 + np.sin(2 * np.pi * p["rate_hz"] * tt + 0.5 * np.pi * c))
            fc = 200 * (2 ** (lfo * p["depth"] * 5))       # 200 Гц … 6,4 кГц
            k = np.tan(np.pi * min(fc, sr * 0.45) / sr)
            a1 = (k - 1) / (k + 1)
            seg = src[s0:s1] + fb * p["feedback"]
            for st in range(stages):
                seg, zf = signal.lfilter([a1, 1.0], [1.0, a1], seg, zi=[z[st]])
                z[st] = zf[0]
            y[s0:s1] = seg
            fb = float(seg[-1]) if len(seg) else 0.0
        out[:, c] = src * (1 - p["mix"] * 0.5) + y * p["mix"] * 0.5 + src * p["mix"] * 0.5
    return out


def _tape(x, sr, p, _res=None):
    """Лента: wow (медленное плавание высоты, ~0,6 Гц) и flutter (быстрое, ~7 Гц) — переменная задержка;
    насыщение tanh; срез верха; тихое шипение."""
    n, ch = x.shape
    t = np.arange(n) / sr
    wow = p["wow"] * 6.0 * (1 + np.sin(2 * np.pi * 0.6 * t))           # мс: медленное плавание
    flutter = p["flutter"] * 0.6 * (1 + np.sin(2 * np.pi * 7.0 * t))   # мс: быстрое дрожание
    d = (wow + flutter) * sr / 1000
    idx = np.arange(n) - d
    out = np.zeros_like(x)
    sos = signal.butter(2, min(p["lowpass_hz"], sr * 0.45), "low", fs=sr, output="sos")
    rng = np.random.default_rng(7)
    for c in range(ch):
        y = np.interp(idx, np.arange(n), x[:, c], left=0.0)
        if p["saturation"] > 0:
            # тихий сигнал — без изменения уровня (наклон tanh(g·y)/g в нуле — 1), громкие пики сжимаются;
            # было tanh(g·y)/tanh(g) — тихое поднималось до +9,6 дБ (лоуфай-гитара громче всего трека, #695)
            g = 1 + 4 * p["saturation"]
            y = np.tanh(y * g) / g
        y = signal.sosfiltfilt(sos, y)
        if p["hiss"] > 0:
            y += p["hiss"] * 0.01 * rng.standard_normal(n)
        out[:, c] = y
    return out


def _spring(x, sr, p, _res=None):
    """Пружина: дисперсия (цепочка all-pass — «чирп» пружины) + затухающий шум с тоном; сухой звук не трогается."""
    n, ch = x.shape
    L = int(min(p["decay_s"] * 1.5, 6.0) * sr)
    rng = np.random.default_rng(3)
    ir = rng.standard_normal(L) * np.exp(-np.arange(L) / (p["decay_s"] * sr / 6.9))
    # «пружинистость»: отражения с шагом ~ 30 мс и дисперсия
    for k in range(1, 8):
        i = int(k * 0.031 * sr)
        if i < L:
            ir[i] += 0.6 ** k * 3
    for _ in range(12):
        ir = signal.lfilter([-0.6, 1.0], [1.0, -0.6], ir)
    cut = 1500 + 6000 * p["tone"]
    ir = signal.sosfilt(signal.butter(2, [120, min(cut, sr * 0.45)], "band", fs=sr, output="sos"), ir)
    ir /= np.sqrt(np.sum(ir ** 2)) + EPS
    out = np.zeros_like(x)
    for c in range(ch):
        wet = signal.fftconvolve(x[:, c], ir)[:n]
        out[:, c] = x[:, c] + p["wet"] * wet
    return out


TREMOLO_SQUARE_K = 12.0   # tremolo shape 1: tanh(k·sin) — «почти квадрат» с мягкими краями без щелчков


def _tremolo(x, sr, p, _res=None):
    """Тремоло: громкость качается по LFO, g = 1 − depth·m(t), m ∈ [0, 1]; shape 0 — m = (1 + sin)/2, к 1 —
    «вкл/выкл» (tanh от синуса); stereo — правый канал к противофазе 1 − m (звук ходит лево-право). Умножение —
    звук не сдвигается. Фаза — от времени в треке (_t0 — начало куска, ставит воркер): превью окна звучит так же,
    как этот отрезок в треке."""
    n, ch = x.shape
    s = np.sin(2 * np.pi * p["rate_hz"] * (float(p.get("_t0", 0.0)) + np.arange(n) / sr))
    k = TREMOLO_SQUARE_K * p["shape"]
    if k > 0:
        s = np.tanh(k * s) / np.tanh(k)
    m = 0.5 * (1.0 + s)
    out = x * (1.0 - p["depth"] * m)[:, None]
    if ch > 1 and p["stereo"] > 0:
        mr = (1.0 - p["stereo"]) * m + p["stereo"] * (1.0 - m)
        out[:, 1:] = x[:, 1:] * (1.0 - p["depth"] * mr)[:, None]
    return out


SYNTH_QUIET_DB = -60.0   # вход тише (A-уровень там, где звучит синт) — громкость синта по пику, а не от трека
SYNTH_REL_DB = -8.0      # умолчание rel_db: «на ухо» на 8 дБ тише трека (#707–#709: −6 по простому RMS — «писк»)


def _synth(x, sr, p, _res):
    """Синт по нотам партии notes [{t, d, midi[], vel}] (t — от начала окна). Громкость «на ухо» — от входа (трека
    при source mix): A-уровень (a_weight) выхода там, где синт звучит, = A-уровень входа на тех же отсчётах · rel_db
    (простой RMS давал ярким синтам +5…+8 дБ на слух — «писк», #707–#709); тихий вход — пик −6 дБFS. Сверху output_db.
    _syn_rms воркер меряет на выходе всей цепочки партии (eq/реверб после synth входят в уровень)."""
    if _num(p.get("_ref_rms")) and _num(p.get("_syn_rms")):
        # превью/пересборка: один множитель на всю партию (воркер посчитал громкость трека и синта по всем нотам) —
        # любое окно звучит так же, как этот кусок в треке; тихий трек — по пику всей партии (−6 дБFS)
        y = _synth_notes(x.shape[0], sr, x.shape[1], dict(p, output_db=0.0, _raw=True), _res)
        if p["_syn_rms"] <= EPS:
            return y * 0.0
        if p["_ref_rms"] > _db(SYNTH_QUIET_DB):
            g = p["_ref_rms"] * _db(p.get("rel_db", SYNTH_REL_DB)) / p["_syn_rms"]
        else:
            g = _db(-6.0) / max(float(p.get("_syn_peak", 0.0)), EPS)
        return y * g * _db(p["output_db"])
    y = _synth_notes(x.shape[0], sr, x.shape[1], dict(p, output_db=0.0), _res)
    mask = np.abs(y).max(axis=1) > 1e-6
    if mask.any():
        # опора — громкость трека по всем нотам партии (_ref_rms ставит воркер для превью и пересборки), иначе —
        # вход окна на звучащих отсчётах; обе — «на ухо»
        rin = float(p["_ref_rms"]) if _num(p.get("_ref_rms")) else _rms(a_weight(x, sr)[mask])
        if rin > _db(SYNTH_QUIET_DB):
            y = y * (rin * _db(p.get("rel_db", SYNTH_REL_DB)) / max(_rms(a_weight(y, sr)[mask]), EPS))
    return y * _db(p["output_db"])


# ---------- перкуссия по сетке (этап 5) ----------
PERC_MAX_HITS = 8000     # ударов в блоке perc не больше: шестнадцатые на 6 мин при 180 BPM — 4320
PERC_VOICES = 6          # голоса драм-машины: 1 хэт, 2 шейкер, 3 хлопок, 4 ковбелл, 5 римшот, 6 бубен (0 — сэмплы kit)
PERC_HAT_HZ = (205.3, 304.4, 369.6, 522.7, 540.0, 800.0)   # шесть квадратов хэта/тарелки TR-808


def _parse_hits(where: str, notes) -> list[dict]:
    """Удары блока perc: [{t (с, < 0 — до окна), d > 0 (ячейка удара), vel 0…1}], не больше PERC_MAX_HITS."""
    if not isinstance(notes, list):
        raise ChainError(f"{where}: notes — список ударов {{t, d, vel}}")
    if len(notes) > PERC_MAX_HITS:
        raise ChainError(f"{where}: ударов не больше {PERC_MAX_HITS} (сейчас {len(notes)})")
    out = []
    for i, nt in enumerate(notes):
        w = f"{where}, удар {i + 1}"
        if not isinstance(nt, dict) or not _num(nt.get("t")) or not _num(nt.get("d")) or nt["d"] <= 0:
            raise ChainError(f"{w}: нужны t (с) и d > 0 (с)")
        if nt["t"] < SYNTH_MIN_T:
            raise ChainError(f"{w}: начало не раньше {SYNTH_MIN_T:g} с")
        vel = nt.get("vel", 0.8)
        if not _num(vel) or not 0 <= vel <= 1:
            raise ChainError(f"{w}: vel — 0…1")
        out.append({"t": float(nt["t"]), "d": float(nt["d"]), "vel": float(vel)})
    return out


def _bp(x, sr, lo, hi, order=2):
    """Полоса lo…hi (каузально: звук голоса начинается в 0 — фильтр не тянет его назад)."""
    hi = min(hi, 0.45 * sr)
    lo = min(lo, hi * 0.9)
    return signal.sosfilt(signal.butter(order, [lo, hi], btype="band", fs=sr, output="sos"), x)


def _perc_voice(voice: int, sr: int, tone: float, decay: float, rng) -> np.ndarray:
    """Один удар голоса драм-машины (моно, звук с отсчёта 0, пик ≈ 1)."""
    def env(tau, length, attack=0.0005):
        t = np.arange(int(length * sr)) / sr
        e = np.exp(-t / tau)
        a = max(1, int(attack * sr))
        e[:a] *= np.linspace(0.0, 1.0, a + 1)[1:]
        return t, e
    if voice == 1:    # хэт: шесть квадратов 808 через верх
        t, e = env(0.04 * decay, 0.4 * decay)
        v = sum(np.sign(np.sin(2 * np.pi * f * tone * t + rng.random() * 6.28)) for f in PERC_HAT_HZ)
        v = signal.sosfilt(signal.butter(4, min(7000.0 * tone, 0.4 * sr), btype="high", fs=sr, output="sos"), v)
    elif voice == 2:  # шейкер: шум в верхней полосе, мягкая атака 8 мс
        t, e = env(0.05 * decay, 0.45 * decay, attack=0.008)
        v = _bp(rng.uniform(-1, 1, len(t)), sr, 5000.0 * tone, 12000.0 * tone)
    elif voice == 3:  # хлопок: три всплеска через 10 мс и хвост
        t = np.arange(int(0.5 * decay * sr)) / sr
        e = sum(np.where(t >= k, np.exp(-(t - k) / 0.003), 0.0) for k in (0.0, 0.010, 0.020))
        e = e + 0.6 * np.where(t >= 0.030, np.exp(-(t - 0.030) / (0.07 * decay)), 0.0)
        a = max(1, int(0.0005 * sr))
        e[:a] *= np.linspace(0.0, 1.0, a + 1)[1:]
        v = _bp(rng.uniform(-1, 1, len(t)), sr, 800.0 * tone, 2500.0 * tone)
    elif voice == 4:  # ковбелл 808: два квадрата 540/800 Гц
        t, e = env(0.09 * decay, 0.7 * decay)
        e = e * (0.6 + 0.4 * np.exp(-t / 0.01))
        v = sum(np.sign(np.sin(2 * np.pi * f * tone * t)) for f in (540.0, 800.0))
        v = _bp(v, sr, 350.0 * tone, 1600.0 * tone)
    elif voice == 5:  # римшот: короткий тон 1,7 кГц и щелчок
        t, e = env(0.008 * decay, 0.12 * decay)
        v = np.sin(2 * np.pi * 1700.0 * tone * t) + 0.5 * rng.uniform(-1, 1, len(t)) * np.exp(-t / 0.002)
    else:             # бубен: шум верха, тарелочки — четыре всплеска
        t = np.arange(int(0.6 * decay * sr)) / sr
        e = sum(w * np.where(t >= k, np.exp(-(t - k) / (0.05 * decay)), 0.0)
                for k, w in ((0.0, 1.0), (0.007, 0.7), (0.016, 0.5), (0.027, 0.35)))
        a = max(1, int(0.0005 * sr))
        e[:a] *= np.linspace(0.0, 1.0, a + 1)[1:]
        v = _bp(rng.uniform(-1, 1, len(t)), sr, 6000.0 * tone, 15000.0 * tone, order=4)
    y = v * e
    pk = float(np.abs(y).max()) if len(y) else 0.0
    return y / pk if pk > EPS else y


def _perc_jitter(t_abs: float, ms: float) -> float:
    """Сдвиг удара «по-живому» (с): от его места в треке, а не от порядка в окне — превью любого окна
    и пересборка сдвигают один и тот же удар одинаково."""
    if ms <= 0:
        return 0.0
    seed = int(round(t_abs * 1000.0)) & 0xFFFFFFFF
    return float(np.random.default_rng(seed).uniform(-1.0, 1.0)) * ms / 1000.0


def _perc_notes(n: int, sr: int, ch: int, p: dict, res) -> np.ndarray:
    """Удары партии без нормировки: удар vel 1 — пик сэмпла/голоса ≈ 1, громкость ∝ vel."""
    hits = p.get("notes") or []
    y = np.zeros((n, ch))
    if not hits:
        return y
    voice = int(p["voice"])
    tone, decay = float(p["tone"]), float(p["decay"])
    if voice == 0:
        layers = _sampler_layers(res, p["kit"], sr, ch)
        if abs(tone - 1.0) > 1e-3:   # скорость сэмпла: tone 2 — вдвое выше и короче
            up, down = 100, max(1, int(round(100 * tone)))
            layers = [_sampler_layers_one(signal.resample_poly(s, up, down, axis=0)) for _, _, s, _ in layers]
        bank = [(s / max(pk, EPS), att) for pk, _, s, att in layers]
    else:
        rng = np.random.default_rng(int(voice))
        # у шумовых голосов — несколько вариантов звука по кругу (один и тот же шум на каждом ударе звучал бы машинно)
        bank = [(np.repeat(_perc_voice(voice, sr, tone, decay, rng)[:, None], ch, axis=1), 0) for _ in range(4)]
    last = len(bank) - 1
    t0 = float(p.get("_t0", 0.0))
    for h in hits:
        # слой/вариант — от места удара в треке (мс), а не от номера в окне: превью любого окна и пересборка
        # играют один и тот же удар одним сэмплом
        turn = int(round(abs(h["t"] + t0) * 1000.0))
        if voice == 0:
            k = int(round(h["vel"] * last))
            if 0 < k < last:     # слой по силе, средние — ±1 по кругу; самый тихий и самый громкий — точно
                k += (0, -1, 1)[turn % 3]
            s, att = bank[k]
        else:
            s, att = bank[turn % len(bank)]
        g = h["vel"]
        at = int(round((h["t"] + _perc_jitter(h["t"] + t0, float(p["humanize_ms"]))) * sr)) - att
        a, b = max(at, 0), min(at + len(s), n)
        if b > a:
            y[a:b] += s[a - at:b - at] * g
    stop = min(n, int(round(p.get("_until", n / sr) * sr)))
    y[stop:] = 0.0
    e = min(int(SYNTH_EDGE_S * sr), max(stop, 1) // 2)
    if e > 0 and stop < n:
        y[stop - e:stop] *= np.linspace(1.0, 0.0, e, endpoint=False)[:, None]
    return y


def _sampler_layers_one(s: np.ndarray) -> tuple[float, int, np.ndarray, int]:
    mono = np.abs(s).sum(axis=1)
    return float(np.abs(s).max()), int(np.argmax(mono)), s, int(np.argmax(mono >= 0.1 * mono.max()))


def perc_cells(notes: list, n: int, sr: int) -> np.ndarray:
    """Отсчёты ячеек ударов [t, t + d) — где мерить громкость партии и трека."""
    m = np.zeros(n, dtype=bool)
    for h in notes:
        a, b = max(0, int(h["t"] * sr)), min(n, int((h["t"] + h["d"]) * sr))
        if b > a:
            m[a:b] = True
    return m


def _perc(x, sr, p, res):
    """Перкуссия поверх трека по ударам notes [{t, d, vel}] (t — от начала окна): голос драм-машины или сэмплы
    набора. Громкость — как у synth: RMS выхода на ячейках ударов = RMS входа (трека) на них · rel_db; тихий
    вход — пик −6 дБFS; сверху output_db. _ref_rms/_syn_rms/_syn_peak — общий уровень партии (ставит воркер)."""
    n, ch = x.shape
    y = _perc_notes(n, sr, ch, p, res)
    if _num(p.get("_ref_rms")) and _num(p.get("_syn_rms")):
        if p["_syn_rms"] <= EPS:
            return y * 0.0
        if p["_ref_rms"] > _db(SYNTH_QUIET_DB):
            g = p["_ref_rms"] * _db(p["rel_db"]) / p["_syn_rms"]
        else:
            g = _db(-6.0) / max(float(p.get("_syn_peak", 0.0)), EPS)
        return y * g * _db(p["output_db"])
    mask = perc_cells(p.get("notes") or [], n, sr)
    rin = _rms(x[mask]) if mask.any() else 0.0
    if rin > _db(SYNTH_QUIET_DB) and mask.any():
        y = y * (rin * _db(p["rel_db"]) / max(_rms(y[mask]), EPS))
    else:
        y = y * (_db(-6.0) / max(float(np.abs(y).max()) if y.size else 0.0, EPS))
    return y * _db(p["output_db"])


# ---------- мастер: склейка шины и ограничитель по истинному пику ----------

GLUE_KNEE_DB = 6.0       # мягкое колено склейки
LIMIT_OVERSAMPLE = 4     # истинный пик — по передискретизации ×4 (как замер dsp.loudness)
LIMIT_AHEAD_S = 0.0015   # упреждение ограничителя: громкость опускается к пику заранее, звук не сдвигается
LIMIT_GAIN_DB = 12.0     # подстройка к цели LUFS — не больше ±12 дБ
LIMIT_LUFS_TOL = 0.3     # цель LUFS достигнута с точностью ±0,3
LIMIT_PASSES = 4         # проходов подстройки к цели не больше
LIMIT_TP_TOL_DB = 0.02   # истинный пик выше потолка на столько — ещё один проход ограничителя
LIMIT_MARGIN_DB = 0.15   # ограничитель целится ниже потолка на столько (сетка замера другая)


def _glue(x, sr, p, _res):
    """Склейка шины: стерео-связанный детектор RMS (каналы вместе — образ не «гуляет»), сглаживание
    атакой/восстановлением, мягкое колено; mix — параллельная компрессия (сухой + сжатый). Уровень
    RMS калиброван по синусу (AES17): синус с пиком −6 дБFS читается как −6 — порог как у пика."""
    n = x.shape[0]
    # RMS в окне атаки (центр — без сдвига: обработка офлайн), затем по блокам CTRL
    pw = uniform_filter1d(np.mean(np.square(x), axis=1), size=max(1, int(p["attack_ms"] / 1000 * sr)), mode="nearest")
    nb = -(-n // CTRL)
    pw = np.pad(pw, (0, nb * CTRL - n)).reshape(nb, CTRL).mean(axis=1)
    lvl = _smooth(pw, _coef(p["attack_ms"], sr), _coef(p["release_ms"], sr), float(pw[0]) if nb else 0.0)
    lvl_db = 10 * np.log10(np.maximum(lvl * 2, EPS))
    over = lvl_db - p["threshold_db"]
    slope = 1 / p["ratio"] - 1
    w = GLUE_KNEE_DB
    gr = np.where(2 * over < -w, 0.0,
                  np.where(2 * over > w, slope * over, slope * (over + w / 2) ** 2 / (2 * w)))
    g = _ctrl_to_samples(10 ** ((gr + p["makeup_db"]) / 20), n)[:, None]
    return x * (p["mix"] * g + (1 - p["mix"]))


def _true_peaks(x: np.ndarray) -> np.ndarray:
    """Истинный пик на каждый сэмпл: максимум |x| по каналам и по точкам между сэмплами (×4)."""
    n = x.shape[0]
    out = np.zeros(n)
    for c in range(x.shape[1]):   # по каналу и по фазам: max по узкой оси у numpy в разы медленнее
        up = _fit(np.abs(signal.resample_poly(x[:, c], LIMIT_OVERSAMPLE, 1)), n * LIMIT_OVERSAMPLE)
        up = up.reshape(n, LIMIT_OVERSAMPLE)
        for k in range(LIMIT_OVERSAMPLE):
            np.maximum(out, up[:, k], out=out)
        np.maximum(out, np.abs(x[:, c]), out=out)
    return out


def _tp_limit(x: np.ndarray, sr: int, ceiling_db: float, release_ms: float) -> np.ndarray:
    """Ограничитель по истинному пику без сдвига звука: нужная громкость на блок CTRL — потолок/пик;
    минимум вперёд на упреждение (+ блок с каждой стороны: интерполяция между блоками не превышает
    нужного), сглаживание окном упреждения — громкость опускается к пику заранее; восстановление —
    плавно, спад — сразу. Пики выше потолка после фильтров — ещё проходы."""
    n = x.shape[0]
    # запас под потолком: замер (dsp.loudness, стриминги) ищет пик на своей сетке 48 кГц ×4 — точки между
    # сэмплами другие, и пик «ровно на потолке» там читается на 0,1–0,2 дБ выше
    c = _db(ceiling_db - LIMIT_MARGIN_DB)
    y = x
    for _ in range(3):
        tp = _true_peaks(y)
        if tp.max() <= c * _db(LIMIT_TP_TOL_DB):
            return y
        nb = -(-n // CTRL)
        need = np.minimum(1.0, c / np.maximum(np.pad(tp, (0, nb * CTRL - n)).reshape(nb, CTRL).max(axis=1), EPS))
        ahead = max(1, int(np.ceil(LIMIT_AHEAD_S * sr / CTRL)))
        h = minimum_filter1d(need, size=2 * ahead + 3, mode="nearest")
        h = np.minimum(h, uniform_filter1d(h, size=2 * ahead + 1, mode="nearest"))
        g = _smooth(h, _coef(release_ms, sr), 1.0, float(h[0]) if nb else 1.0)
        y = y * _ctrl_to_samples(g, n)[:, None]
    # остаток после проходов (редкие межсэмпловые всплески) — общий сдвиг громкости, а не перегруз
    tp = float(_true_peaks(y).max()) if n else 0.0
    return y * (c / tp) if tp > c else y


def _lufs(x: np.ndarray, sr: int) -> float | None:
    import dsp
    return dsp.loudness(x, sr, true_peak=False).get("lufs")


def _limiter(x, sr, p, _res):
    """Мастер-ограничитель: с target_lufs громкость доводится до цели (по всему входу блока, ±12 дБ,
    подстройка до ±0,3 LU — ограничитель сам немного снижает громкость), пики — не выше потолка по
    истинному пику. Без цели громкость не меняется, срезаются только пики выше потолка."""
    if not x.size or not np.any(x):
        return x
    target = p["target_lufs"]
    if not target:
        return _tp_limit(x, sr, p["ceiling_db"], p["release_ms"])
    lin = _lufs(x, sr)
    if lin is None:
        return _tp_limit(x, sr, p["ceiling_db"], p["release_ms"])
    gain = float(np.clip(target - lin, -LIMIT_GAIN_DB, LIMIT_GAIN_DB))
    y = x
    for _ in range(LIMIT_PASSES):
        y = _tp_limit(x * _db(gain), sr, p["ceiling_db"], p["release_ms"])
        got = _lufs(y, sr)
        if got is None or abs(got - target) <= LIMIT_LUFS_TOL:
            break
        nxt = float(np.clip(gain + target - got, -LIMIT_GAIN_DB, LIMIT_GAIN_DB))
        if nxt == gain:
            break   # упёрлись в предел подстройки
        gain = nxt
    return y


BLOCKS = {"gate": _gate, "eq": _eq, "comp": _comp, "drive": _drive, "amp": _amp,
          "cab": _cab, "reverb": _reverb, "delay": _delay, "gain": _gain, "sampler": _sampler,
          "bass": _bass, "synth": _synth, "chorus": _chorus, "phaser": _phaser, "flanger": _flanger,
          "tape": _tape, "spring": _spring, "tremolo": _tremolo, "perc": _perc, "glue": _glue, "limiter": _limiter}


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


__all__ = ["ChainError", "SPEC", "a_weight", "parse_chain", "needs_gpu", "process"]
