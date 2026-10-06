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

from fractions import Fraction

import numpy as np
from scipy import signal
from scipy.ndimage import maximum_filter1d


class ChainError(ValueError):
    """Неверная цепочка: текст — понятная причина (уходит в ответ 422)."""


CTRL = 16            # шаг управляющего сигнала гейта/компрессора, сэмплов (~0,36 мс при 44,1 кГц)
OVERSAMPLE = 4       # передискретизация перегруза
MAX_TAIL_S = 10.0    # предел длины хвоста реверба/дилея
EPS = 1e-12
AMP_INPUT_RMS_DB = -20.0  # уровень входа захвата NAM (так шли опыты на гитаре YuE)

# параметр: (умолчание, мин, макс)
SPEC: dict[str, dict[str, tuple]] = {
    "gate": {"threshold_db": (-50.0, -90.0, 0.0), "range_db": (-40.0, -90.0, 0.0),
             "attack_ms": (1.0, 0.1, 50.0), "release_ms": (100.0, 5.0, 1000.0)},
    "eq": {"highpass_hz": (0.0, 0.0, 1000.0), "lowpass_hz": (0.0, 0.0, 22000.0)},
    "comp": {"threshold_db": (-20.0, -60.0, 0.0), "ratio": (4.0, 1.0, 20.0),
             "attack_ms": (10.0, 0.1, 200.0), "release_ms": (100.0, 5.0, 2000.0),
             "makeup_db": (0.0, -12.0, 24.0)},
    "drive": {"gain_db": (12.0, 0.0, 48.0), "mix": (1.0, 0.0, 1.0), "output_db": (0.0, -24.0, 12.0)},
    "amp": {"input_db": (0.0, -24.0, 24.0), "output_db": (0.0, -24.0, 24.0)},
    "cab": {"cutoff_hz": (7000.0, 2000.0, 12000.0), "mix": (1.0, 0.0, 1.0)},
    "reverb": {"decay_s": (1.5, 0.1, 10.0), "predelay_ms": (10.0, 0.0, 200.0),
               "lowpass_hz": (8000.0, 1000.0, 20000.0), "wet": (0.3, 0.0, 1.0)},
    "delay": {"time_ms": (375.0, 1.0, 2000.0), "feedback": (0.35, 0.0, 0.95),
              "lowpass_hz": (6000.0, 1000.0, 20000.0), "wet": (0.3, 0.0, 1.0)},
}
# строковые параметры: (умолчание или None — обязателен)
STR_SPEC: dict[str, dict[str, str | None]] = {"amp": {"model": None}, "cab": {"ir": ""}, "reverb": {"ir": ""}}
BAND_SPEC = {"freq_hz": (1000.0, 20.0, 20000.0), "gain_db": (0.0, -24.0, 24.0), "q": (1.0, 0.1, 10.0)}
LOWPASS_MIN = 1000.0  # lowpass_hz эквалайзера: 0 — выкл, иначе не ниже


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
            norm[k] = _check_num(where, k, blk.get(k, s[0]), s)
        for k, default in strs.items():
            v = blk.get(k, default)
            if v is None:
                raise ChainError(f"{where}: нужен параметр {k}")
            if not isinstance(v, str):
                raise ChainError(f"{where}: {k} должен быть строкой")
            norm[k] = v
        if t == "eq":
            lp = norm["lowpass_hz"]
            if 0 < lp < LOWPASS_MIN:
                raise ChainError(f"{where}: lowpass_hz — 0 (выкл) или от {LOWPASS_MIN:g}")
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
        raise ChainError(f"{'захват' if kind == 'amp' else 'IR'} {name!r} не найден") from None


BLOCKS = {"gate": _gate, "eq": _eq, "comp": _comp, "drive": _drive, "amp": _amp,
          "cab": _cab, "reverb": _reverb, "delay": _delay}


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
