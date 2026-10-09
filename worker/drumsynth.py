"""Наборы драм-машин, которые воркер синтезирует сам (без сети и лицензий): удары частей по слоям силы.

Чистый модуль: numpy/scipy. render(kit, part, layer, sr) — моно float, звук с отсчёта 0, пик ≤ 1. Слоёв по силе —
DRUM_LAYERS; слой k громче слоя k−1 и чуть ярче (как настоящий удар сильнее). Детерминированно: шум — от сида по
(набор, часть, слой), повтор даёт те же отсчёты. Звучание — приближение к машинам, не их копия.
"""
from __future__ import annotations

import zlib

import numpy as np
from scipy import signal

DRUM_LAYERS = 8
PARTS = ("kick", "snare", "hh-closed", "hh-open", "ride", "crash", "tom-small", "tom-medium", "tom-large",
         "clap", "rim", "cowbell")
KITS = {k: PARTS for k in ("tr808", "tr909", "linn", "cr78", "simmons")}
HAT_HZ = (205.3, 304.4, 369.6, 522.7, 540.0, 800.0)   # шесть квадратов хэта/тарелки TR-808
TAIL_FADE_S = 0.005   # последние 5 мс — к нулю: обрыв буфера без щелчка
TAIL_DB = -60.0       # удар обрезается там, где огибающая упала ниже пика на столько
MAX_S = 4.0           # и не длиннее
LINN_SR = 28000       # LinnDrum: сэмплы 28 кГц, 8 бит
MU = 255.0            # компандирование 8 бит


def _rng(kit: str, part: str, layer: int) -> np.random.Generator:
    return np.random.default_rng(zlib.crc32(f"{kit}/{part}/{layer}".encode()))


def _t(sec: float, sr: int) -> np.ndarray:
    return np.arange(int(sec * sr)) / sr


def _env(t: np.ndarray, tau: float, attack: float = 0.0005, sr: int = 48000) -> np.ndarray:
    e = np.exp(-t / tau)
    a = max(1, int(attack * sr))
    e[:a] *= np.linspace(0.0, 1.0, a + 1)[1:]
    return e


def _sweep(t: np.ndarray, f_end: float, f_start: float, tau: float) -> np.ndarray:
    """Синус с частотой, падающей от f_start к f_end с постоянной tau (фаза — интеграл частоты)."""
    f = f_end + (f_start - f_end) * np.exp(-t / tau)
    dt = t[1] - t[0] if len(t) > 1 else 1.0
    return np.sin(2 * np.pi * np.cumsum(f) * dt)


def _bp(x: np.ndarray, sr: int, lo: float, hi: float, order: int = 2) -> np.ndarray:
    hi = min(hi, 0.45 * sr)
    lo = min(lo, hi * 0.9)
    return signal.sosfilt(signal.butter(order, [lo, hi], btype="band", fs=sr, output="sos"), x)


def _hp(x: np.ndarray, sr: int, f: float, order: int = 2) -> np.ndarray:
    return signal.sosfilt(signal.butter(order, min(f, 0.45 * sr), btype="high", fs=sr, output="sos"), x)


def _lp(x: np.ndarray, sr: int, f: float, order: int = 2) -> np.ndarray:
    return signal.sosfilt(signal.butter(order, min(f, 0.45 * sr), btype="low", fs=sr, output="sos"), x)


def _metal(t: np.ndarray, rng, tone: float = 1.0) -> np.ndarray:
    """Металл 808: шесть квадратов несоизмеримых частот."""
    return sum(np.sign(np.sin(2 * np.pi * f * tone * t + rng.random() * 6.28)) for f in HAT_HZ) / 6.0


def _norm(y: np.ndarray) -> np.ndarray:
    pk = float(np.abs(y).max()) if len(y) else 0.0
    return y / pk if pk > 0 else y


# ---------- голоса: (t, sr, rng, b) → звук; b — яркость слоя 0…1 ----------

def _kick(kit, t, sr, rng, b):
    if kit == "tr808":
        y = _sweep(t, 50.0, 130.0 + 40 * b, 0.025) * _env(t, 0.45, sr=sr)
        y += 0.15 * b * _hp(rng.uniform(-1, 1, len(t)), sr, 2000) * _env(t, 0.002, sr=sr)
    elif kit == "tr909":
        y = np.tanh(2.5 * _sweep(t, 55.0, 260.0 + 60 * b, 0.018) * _env(t, 0.22, sr=sr))
        y += 0.35 * (0.5 + b) * _hp(rng.uniform(-1, 1, len(t)), sr, 3000) * _env(t, 0.004, sr=sr)
    elif kit == "linn":
        y = _sweep(t, 62.0, 140.0, 0.02) * _env(t, 0.16, sr=sr)
        y += 0.3 * _lp(rng.uniform(-1, 1, len(t)), sr, 900) * _env(t, 0.015, sr=sr)
    elif kit == "cr78":
        y = np.sin(2 * np.pi * 62.0 * t) * _env(t, 0.1, attack=0.002, sr=sr)
        y += 0.1 * np.sin(2 * np.pi * 124.0 * t) * _env(t, 0.03, sr=sr)
    else:  # simmons: короткий «бум» с глубоким спадом высоты и шумом
        y = _sweep(t, 48.0, 180.0, 0.05) * _env(t, 0.2, sr=sr)
        y += 0.25 * _lp(rng.uniform(-1, 1, len(t)), sr, 1500) * _env(t, 0.03, sr=sr)
    return y


def _snare(kit, t, sr, rng, b):
    n = rng.uniform(-1, 1, len(t))
    if kit == "tr808":
        tone = (np.sin(2 * np.pi * 180 * t) + 0.6 * np.sin(2 * np.pi * 330 * t)) * _env(t, 0.06, sr=sr)
        y = 0.8 * tone + (0.6 + 0.4 * b) * _hp(n, sr, 1800) * _env(t, 0.09, sr=sr)
    elif kit == "tr909":
        tone = _sweep(t, 190, 260, 0.01) * _env(t, 0.05, sr=sr)
        y = 0.7 * tone + (0.9 + 0.3 * b) * _bp(n, sr, 1500, 12000) * _env(t, 0.13, sr=sr)
    elif kit == "linn":
        tone = np.sin(2 * np.pi * 200 * t) * _env(t, 0.04, sr=sr)
        y = 0.6 * tone + _bp(n, sr, 300, 8000) * _env(t, 0.12, sr=sr)
    elif kit == "cr78":
        tone = np.sin(2 * np.pi * 240 * t) * _env(t, 0.02, sr=sr)
        y = 0.6 * tone + 0.7 * _bp(n, sr, 2000, 8000) * _env(t, 0.04, sr=sr)
    else:  # simmons: тон с быстрым спадом и много шума
        tone = _sweep(t, 220, 600, 0.03) * _env(t, 0.12, sr=sr)
        y = 0.7 * tone + 0.8 * _bp(n, sr, 1000, 9000) * _env(t, 0.15, sr=sr)
    return y


def _hat(kit, t, sr, rng, b, open_):
    tau = (0.25 if open_ else 0.035) * (1.0 if kit != "cr78" else 0.7)
    if kit in ("tr808", "cr78"):
        v = _metal(t, rng, 1.0 if kit == "tr808" else 1.25)
        v = _hp(v, sr, 7000, 4)
        if kit == "cr78":
            v = 0.6 * v + 0.4 * _hp(rng.uniform(-1, 1, len(t)), sr, 8000, 4)
    elif kit == "tr909":
        v = 0.5 * _metal(t, rng, 1.6) + 0.5 * rng.uniform(-1, 1, len(t))
        v = _hp(v, sr, 7500, 4)
    else:  # linn и simmons: шум верха (у simmons — фильтрованный, «синтетический»)
        v = _hp(rng.uniform(-1, 1, len(t)), sr, 6500 if kit == "linn" else 5000, 4)
    return v * _env(t, tau * (0.9 + 0.2 * b), sr=sr)


def _cymbal(kit, t, sr, rng, b, ride):
    tau = 0.45 if ride else 0.55
    if kit in ("tr808", "cr78"):
        v = _bp(_metal(t, rng, 0.85), sr, 3000, 12000, 4)
    elif kit == "tr909":
        v = _hp(0.4 * _metal(t, rng, 1.3) + 0.6 * rng.uniform(-1, 1, len(t)), sr, 4000, 4)
    else:
        v = _hp(rng.uniform(-1, 1, len(t)), sr, 4500, 4)
    if ride:   # райд: «динь» — тон колокола поверх шума
        v = 0.6 * v + 0.4 * np.sin(2 * np.pi * 3200 * t) * _env(t, 0.3, sr=sr)
    return v * _env(t, tau * (0.9 + 0.2 * b), attack=0.001, sr=sr)


TOM_HZ = {"tom-small": 220.0, "tom-medium": 160.0, "tom-large": 110.0}


def _tom(kit, part, t, sr, rng, b):
    f = TOM_HZ[part]
    if kit == "simmons":   # «пью»: спад высоты на октаву с лишним
        y = _sweep(t, f, f * 2.6, 0.08) * _env(t, 0.35, sr=sr)
        y += 0.2 * _lp(rng.uniform(-1, 1, len(t)), sr, 3000) * _env(t, 0.03, sr=sr)
    elif kit == "tr909":
        y = _sweep(t, f, f * 1.5, 0.03) * _env(t, 0.25, sr=sr)
        y += 0.2 * _bp(rng.uniform(-1, 1, len(t)), sr, 200, 2000) * _env(t, 0.02, sr=sr)
    elif kit == "cr78":
        y = np.sin(2 * np.pi * f * 1.2 * t) * _env(t, 0.12, attack=0.002, sr=sr)
    else:  # 808, linn
        y = _sweep(t, f, f * 1.15, 0.05) * _env(t, 0.3 if kit == "tr808" else 0.22, sr=sr)
    return y


def _clap(kit, t, sr, rng, b):
    # три всплеска и хвост; первый — самый громкий (пик удара — в начале)
    e = sum(w * np.where(t >= k, np.exp(-(t - k) / 0.003), 0.0) for k, w in ((0.0, 1.0), (0.010, 0.6), (0.020, 0.45)))
    e = e + 0.3 * np.where(t >= 0.030, np.exp(-(t - 0.030) / 0.08), 0.0)
    a = max(1, int(0.0005 * sr))
    e[:a] *= np.linspace(0.0, 1.0, a + 1)[1:]
    return _bp(rng.uniform(-1, 1, len(t)), sr, 800, 2500 if kit != "tr909" else 4000) * e


def _rim(kit, t, sr, rng, b):
    f = 1700.0 if kit != "tr909" else 1900.0
    return (np.sin(2 * np.pi * f * t) + 0.5 * rng.uniform(-1, 1, len(t)) * np.exp(-t / 0.002)) * _env(t, 0.008, sr=sr)


def _cowbell(kit, t, sr, rng, b):
    e = _env(t, 0.09, sr=sr) * (0.6 + 0.4 * np.exp(-t / 0.01))
    v = sum(np.sign(np.sin(2 * np.pi * f * t)) for f in (540.0, 800.0))
    return _bp(v, sr, 350, 1600) * e


LEN_S = {"kick": 3.5, "snare": 1.2, "hh-closed": 0.5, "hh-open": 2.2, "ride": 4.0, "crash": 4.0, "tom-small": 2.5,
         "tom-medium": 2.5, "tom-large": 3.0, "clap": 1.0, "rim": 0.3, "cowbell": 1.0}   # запас; режется по TAIL_DB
NOISY = ("hh-open", "ride", "crash", "clap")   # шумовым — подчёркнутая атака: пик удара — в его начале
# тональным (бочка, тамы) — щелчок атаки: иначе пик удара гуляет между первыми периодами синуса, и сэмплер ставит
# удар на 7–13 мс позже (пик сэмпла ставится на пик удара)
PUNCH = ("kick", "tom-small", "tom-medium", "tom-large")


def render(kit: str, part: str, layer: int, sr: int = 48000) -> np.ndarray:
    """Удар части `part` набора `kit`, слой силы layer (0 — самый тихий … DRUM_LAYERS−1)."""
    if kit not in KITS or part not in KITS[kit]:
        raise KeyError(f"{kit}/{part}")
    if not 0 <= layer < DRUM_LAYERS:
        raise ValueError(f"слой 0…{DRUM_LAYERS - 1}")
    rng = _rng(kit, part, layer)
    b = layer / (DRUM_LAYERS - 1)
    t = _t(LEN_S[part], sr)
    if part == "kick":
        y = _kick(kit, t, sr, rng, b)
    elif part == "snare":
        y = _snare(kit, t, sr, rng, b)
    elif part in ("hh-closed", "hh-open"):
        y = _hat(kit, t, sr, rng, b, part == "hh-open")
    elif part in ("ride", "crash"):
        y = _cymbal(kit, t, sr, rng, b, part == "ride")
    elif part.startswith("tom-"):
        y = _tom(kit, part, t, sr, rng, b)
    elif part == "clap":
        y = _clap(kit, t, sr, rng, b)
    elif part == "rim":
        y = _rim(kit, t, sr, rng, b)
    else:
        y = _cowbell(kit, t, sr, rng, b)
    if part in NOISY:
        y = y * (1.0 + 1.5 * np.exp(-t / 0.004))
    elif part in PUNCH:
        y = y * (1.0 + 1.2 * np.exp(-t / 0.003))
    if kit == "linn":   # 28 кГц и 8 бит: верх выше 14 кГц уходит, лёгкое зерно
        y = _norm(y)
        y = signal.resample_poly(y, LINN_SR, sr)
        # 8 бит с компандированием (μ-закон, как ЦАП машин тех лет): тихий хвост не рассыпается в шум
        c = np.log1p(MU * np.abs(y)) / np.log1p(MU)
        y = np.sign(y) * np.expm1(np.round(c * 127) / 127 * np.log1p(MU)) / MU
        y = signal.resample_poly(y, sr, LINN_SR)[: len(t)]
        y = _lp(y, sr, 11000, 8)
    y = _norm(y)
    env = signal.sosfiltfilt(signal.butter(2, 30, fs=sr, output="sos"), np.abs(y))
    loud = np.nonzero(env > 10 ** (TAIL_DB / 20) * env.max())[0]
    end = min(len(y), int(MAX_S * sr), (int(loud[-1]) + 1) if len(loud) else len(y))
    y = y[:max(end, int(0.03 * sr))].copy()
    f = min(len(y), int(TAIL_FADE_S * sr))
    if f:
        y[-f:] *= np.linspace(1.0, 0.0, f)
    # сила удара: громкость от −24 дБ (слой 0) до 0 дБ (последний)
    return y * 10 ** ((-24.0 + 24.0 * b) / 20)


# ---------- синт-басы набором (блок bass): ноты по полутонам ----------

BASS_KITS = {"synthbass": ("moog", "sub808", "acid")}
# ноты сэмплов, MIDI: E1 … G3 — диапазон баса трека; файлы m<MIDI>.wav — блок bass берёт высоту из имени (замер
# yin на почти чистом низком синусе ошибался бы до +26 центов)
BASS_RANGE = {"moog": range(28, 56), "sub808": range(28, 56), "acid": range(28, 56)}
BASS_NOTE_S = 2.5


def _rbj_lp(x: np.ndarray, sr: int, f0: float, q: float) -> np.ndarray:
    w = 2 * np.pi * min(f0, 0.45 * sr) / sr
    al = np.sin(w) / (2 * q)
    c = np.cos(w)
    b = np.array([(1 - c) / 2, 1 - c, (1 - c) / 2])
    a = np.array([1 + al, -2 * c, 1 - al])
    return signal.lfilter(b / a[0], a / a[0], x)


def render_bass(part: str, midi: int, sr: int = 48000) -> np.ndarray:
    """Нота синт-баса: moog (пила + квадрат через фильтр со щелчком), sub808 (синус с лёгкой
    грязью, медленно гаснет), acid (пила через резонансный фильтр — 303). Пик 1, звук с отсчёта 0."""
    if part not in BASS_KITS["synthbass"]:
        raise KeyError(f"synthbass/{part}")
    f = 440.0 * 2 ** ((midi - 69) / 12)
    t = _t(BASS_NOTE_S, sr)
    ph = 2 * np.pi * f * t
    if part == "sub808":
        # чистый синус низких нот блок bass меряет с ошибкой до +40 центов (кадр yin) — грязь даёт гармоники
        y = np.tanh(3.0 * (np.sin(ph) + 0.4 * np.sin(2 * ph))) * _env(t, 1.6, attack=0.003, sr=sr)
    else:
        saw = signal.sawtooth(ph)
        if part == "moog":
            src = 0.65 * saw - 0.35 * signal.square(ph)   # знак: у пилы scipy основной тон в противофазе квадрату
            lo, hi = _lp(src, sr, 350, 4), _lp(src, sr, 2500, 4)
            m = np.exp(-t / 0.12)
            y = (lo + (hi - lo) * m) * _env(t, 3.0, attack=0.004, sr=sr)
        else:
            lo, hi = _rbj_lp(saw, sr, 280, 5.0), _rbj_lp(saw, sr, 1800, 5.0)
            m = np.exp(-t / 0.08)
            y = np.tanh(0.8 * (lo + (hi - lo) * m)) * _env(t, 2.0, attack=0.002, sr=sr)
    y = _norm(y)
    r = int(0.05 * sr)   # отпускание в конце сэмпла
    y[-r:] *= np.linspace(1.0, 0.0, r)
    return y
