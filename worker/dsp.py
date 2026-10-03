"""DSP-карта трека в JSON: темп, тональность, динамика, шумовое полотно, спектр, жёсткость, стерео.

librosa-анализатор аудио под машинное чтение:
метрики едут в приложение и сравниваются с референсом дельтой.
"""
from __future__ import annotations

import logging

import numpy as np

log = logging.getLogger(__name__)


NOTES = ["C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"]


# Громкость по ITU-R BS.1770 / EBU R128. Фильтры K-взвешивания заданы для 48 кГц:
# полка верхов + срез низа.
_K_SR = 48000
_K_SHELF = ([1.53512485958697, -2.69169618940638, 1.19839281085285], [1.0, -1.69065929318241, 0.73248077421585])
_K_HP = ([1.0, -2.0, 1.0], [1.0, -1.99004745483398, 0.99007225036621])
_K_OFFSET = -0.691          # поправка BS.1770 к громкости блока
_ABS_GATE = -70.0           # блоки тише — тишина, не учитываются
_LUFS_BLOCK = (0.4, 0.1)    # интегральная: блоки 400 мс с шагом 100 мс...
_LUFS_REL_GATE = -10.0      # ...и относительный гейт −10 LU
_LRA_BLOCK = (3.0, 1.0)     # LRA: блоки 3 с с шагом 1 с...
_LRA_REL_GATE = -20.0       # ...относительный гейт −20 LU, диапазон p10..p95
_TRUE_PEAK_OVERSAMPLE = 4


def _block_loudness(z2: np.ndarray, sr: int, win: float, hop: float) -> np.ndarray:
    """Громкость блоков (LUFS) по сумме средних квадратов K-взвешенных каналов."""
    n, h = int(win * sr), int(hop * sr)
    if z2.shape[0] < n:
        return np.array([])
    starts = np.arange(0, z2.shape[0] - n + 1, h)
    c = np.vstack([np.zeros((1, z2.shape[1])), np.cumsum(z2, axis=0)])
    ms = (c[starts + n] - c[starts]) / n           # средний квадрат блока по каналам
    return _K_OFFSET + 10 * np.log10(ms.sum(axis=1) + 1e-12)


def _power_mean_db(db: np.ndarray) -> float:
    """Среднее блоков по мощности (не по децибелам), в дБ."""
    return float(10 * np.log10(np.mean(10 ** (db / 10))))


def _gated(blocks: np.ndarray, rel_gate: float) -> np.ndarray:
    """Блоки после двух гейтов: абсолютного (тишина) и относительного (rel_gate
    дБ от средней громкости оставшихся)."""
    b = blocks[blocks > _ABS_GATE]
    if not b.size:
        return b
    return b[b > _power_mean_db(b) + rel_gate]


def loudness(x: np.ndarray, sr: int) -> dict:
    """Громкость трека: интегральная (lufs), диапазон громкости (lra, LU) и
    истинный пик (true_peak_db, dBTP). x — (кадры, каналы) или моно."""
    from math import gcd

    from scipy.signal import lfilter, resample_poly

    x = np.asarray(x, dtype=np.float64)
    if x.ndim == 1:
        x = x[:, None]
    if sr != _K_SR:
        g = gcd(int(sr), _K_SR)
        x = resample_poly(x, _K_SR // g, int(sr) // g, axis=0)
    z2 = lfilter(*_K_HP, lfilter(*_K_SHELF, x, axis=0), axis=0) ** 2
    out: dict = {}
    g = _gated(_block_loudness(z2, _K_SR, *_LUFS_BLOCK), _LUFS_REL_GATE)
    if g.size:
        out["lufs"] = round(_power_mean_db(g), 1)
    g = _gated(_block_loudness(z2, _K_SR, *_LRA_BLOCK), _LRA_REL_GATE)
    if g.size:
        lo, hi = np.percentile(g, [10, 95])
        out["lra"] = round(float(hi - lo), 1)
    tp = float(np.abs(resample_poly(x, _TRUE_PEAK_OVERSAMPLE, 1, axis=0)).max())
    out["true_peak_db"] = round(float(20 * np.log10(tp + 1e-12)), 1)
    return out


def _refine_bpm(env: np.ndarray, t: np.ndarray, bpm0: float) -> tuple[float, float]:
    """Уточнение темпа ±3% шагом 0,05 BPM: огибающая атак сворачивается с
    комплексной синусоидой частоты долей; модуль суммы — насколько удары
    ложатся на сетку. Возвращает (bpm, strength 0…1)."""
    best_bpm, best = bpm0, 0.0
    for bpm in np.arange(bpm0 * 0.97, bpm0 * 1.03, 0.05):
        z = abs(np.sum(env * np.exp(2j * np.pi * t * bpm / 60.0)))
        if z > best:
            best_bpm, best = float(bpm), z
    return best_bpm, best / env.sum()


def _downbeat_offset(on_t: np.ndarray, w: np.ndarray, period: float) -> float:
    """Сдвиг сильной доли по атакам (время on_t, сила w). Не по средней фазе
    огибающей: хай-хэт на слабых долях тянул её (по барабанам и по миксу
    выходило по-разному). Сначала фаза атак на сетке шестнадцатых, затем
    сильная доля — та из четырёх шестнадцатых, где больше энергии атак."""
    p16 = period / 4
    off16 = (np.angle(np.sum(w * np.exp(2j * np.pi * on_t / p16))) / (2 * np.pi) * p16) % p16

    def slot_energy(k: int) -> float:
        dist = np.abs(((on_t - off16 - k * p16 + period / 2) % period) - period / 2)
        return float(w[dist < p16 / 2].sum())

    best = max(range(4), key=slot_energy)
    return float((off16 + best * p16) % period)


def beat_grid(y: np.ndarray, sr: int, lo: float = 60.0, hi: float = 200.0) -> dict:
    """Сетка долей трека для эффектов в такт («Ритм-гейт»): темп (bpm), сдвиг
    сильной доли (offset, с, 0 ≤ offset < длины доли) и strength (0…1) — доля
    «попадающей» в сетку энергии атак: < 0,1 — сетки по сути нет (рубато,
    тишина). y — моно; лучше дорожка барабанов. Темп — оценка librosa,
    приведённая в lo..hi и уточнённая _refine_bpm."""
    import librosa

    hop = 256
    env = librosa.onset.onset_strength(y=np.asarray(y, dtype=np.float32), sr=sr, hop_length=hop)
    env = np.maximum(env - np.median(env), 0)
    if env.sum() <= 0:
        return {"bpm": 0.0, "offset": 0.0, "strength": 0.0}
    bpm0 = float(np.atleast_1d(librosa.feature.tempo(onset_envelope=env, sr=sr, hop_length=hop))[0])
    while bpm0 < lo:
        bpm0 *= 2
    while bpm0 > hi:
        bpm0 /= 2
    bpm, strength = _refine_bpm(env, np.arange(len(env)) * hop / sr, bpm0)
    frames = librosa.onset.onset_detect(onset_envelope=env, sr=sr, hop_length=hop, backtrack=False)
    offset = 0.0
    if len(frames) >= 4:  # меньше — фазу не по чему мерить
        offset = _downbeat_offset(frames * hop / sr, env[frames], 60.0 / bpm)
    return {"bpm": round(bpm, 2), "offset": round(offset, 3), "strength": round(float(strength), 3)}


_VA_FRAME = 0.1  # шаг кадров громкости в vocal_activity, с


def vocal_activity(x: np.ndarray, sr: int, thresh_db: float = -35.0, min_len: float = 0.3,
                   gap: float = 1.0) -> list[float]:
    """Где звучит дорожка голоса: начала участков (с), где громкость кадров по
    0,1 с не ниже thresh_db (dBFS), участок не короче min_len, паузы короче gap
    склеиваются. Для треков «без голоса»: непустой список — нейросеть подсунула
    голос («кул» в интро) или в дорожку голоса попал инструмент — послушать."""
    x = np.asarray(x, dtype=np.float64)
    if x.ndim > 1:
        x = x.mean(axis=1)
    hop = max(1, int(sr * _VA_FRAME))
    n = len(x) // hop
    if n == 0:
        return []
    db = 20 * np.log10(np.sqrt((x[: n * hop].reshape(n, hop) ** 2).mean(axis=1)) + 1e-12)
    t = np.flatnonzero(db >= thresh_db) * _VA_FRAME  # время громких кадров
    if not t.size:
        return []
    breaks = np.flatnonzero(np.diff(t) > gap)         # паузы длиннее gap делят участки
    starts = t[np.r_[0, breaks + 1]]
    ends = t[np.r_[breaks, t.size - 1]] + _VA_FRAME
    return [round(float(a), 1) for a, b in zip(starts, ends, strict=True) if b - a >= min_len]


def analyze_file(path) -> dict:
    import librosa

    y, sr = librosa.load(str(path), sr=22050, mono=True)

    m: dict = {"duration_sec": round(len(y) / sr, 1)}

    # Темп
    onset_env = librosa.onset.onset_strength(y=y, sr=sr)
    tempo = float(np.atleast_1d(librosa.beat.beat_track(onset_envelope=onset_env, sr=sr)[0])[0])
    m["tempo_bpm"] = round(tempo, 1)

    # Тональность: доминирующая ступень по хроме
    chroma_mean = librosa.feature.chroma_cqt(y=y, sr=sr).mean(axis=1)
    m["key"] = NOTES[int(np.argmax(chroma_mean))]

    # Динамика
    rms_db = 20 * np.log10(librosa.feature.rms(y=y)[0] + 1e-9)
    p95, p50, p10 = (float(v) for v in np.percentile(rms_db, [95, 50, 10]))
    m["rms_p95_db"], m["rms_median_db"], m["rms_p10_db"] = round(p95, 1), round(p50, 1), round(p10, 1)
    m["dyn_range_db"] = round(p95 - p10, 1)
    m["crest_db"] = round(p95 - p50, 1)

    # Клиппинг/перегруз
    m["peak"] = round(float(np.abs(y).max()), 3)
    m["clip_pct"] = round(float(np.mean(np.abs(y) > 0.98)) * 100, 3)

    # Шумовое полотно: медиана самых тихих 5% RMS-окон
    quiet = np.argsort(rms_db)[: len(rms_db) // 20]
    m["noise_floor_db"] = round(float(np.median(rms_db[quiet])), 1)
    m["signal_noise_db"] = round(p95 - m["noise_floor_db"], 1)

    # Спектральный профиль: доля энергии по полосам + ширина полосы
    S = np.abs(librosa.stft(y, n_fft=2048))
    freqs = librosa.fft_frequencies(sr=sr)
    S_mean = S.mean(axis=1)
    total = float(S_mean.sum())
    bands = {}
    for name, lo, hi in [("bass", 0, 150), ("low_mid", 150, 500), ("mid", 500, 2000),
                         ("high", 2000, 8000), ("air", 8000, 11025)]:
        sel = (freqs >= lo) & (freqs < hi)
        bands[name] = round(100 * float(S_mean[sel].sum()) / total, 1)
    m["bands"] = bands
    cum = np.cumsum(S_mean) / total
    m["centroid_hz"] = round(float(librosa.feature.spectral_centroid(y=y)[0].mean()), 0)
    m["f95_hz"] = round(float(freqs[np.searchsorted(cum, 0.95)]), 0)
    m["f99_hz"] = round(float(freqs[np.searchsorted(cum, 0.99)]), 0)

    # Гармоничность/жёсткость: спектральный флэтнес в громких местах
    flat = librosa.feature.spectral_flatness(y=y)[0]
    m["flatness_median"] = round(float(np.median(flat)), 3)
    m["flatness_loud"] = round(float(np.median(flat[rms_db > np.percentile(rms_db, 75)])), 3)

    # Стерео
    try:
        y_full, _ = librosa.load(str(path), sr=44100, mono=False)
    except Exception:  # noqa: BLE001
        y_full = y
    if y_full.ndim == 2 and y_full.shape[0] == 2:
        L, R = y_full
        m["stereo_corr"] = round(float(np.corrcoef(L, R)[0, 1]), 2)
        m["mid_db"] = round(float(20 * np.log10(np.std(L + R) + 1e-9)), 1)
        m["side_db"] = round(float(20 * np.log10(np.std(L - R) + 1e-9)), 1)
    else:
        m["stereo_corr"] = None

    # Громкость по стандарту стримингов (EBU R128): LUFS, LRA, true peak
    try:
        import soundfile as sf
        x, xsr = sf.read(str(path), dtype="float64", always_2d=True)
        m.update(loudness(x, xsr))
    except ImportError:
        pass  # без soundfile/scipy громкости нет, остальные метрики всё равно нужны
    except Exception:  # noqa: BLE001 - карта трека нужна и без громкости
        log.exception("громкость (LUFS) не посчитана: %s", path)

    return m
