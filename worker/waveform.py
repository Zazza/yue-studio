"""Волна громкости и спектрограмма артефактов джобы (вид «как в плеере»).

Пики (огибающая) — soundfile+numpy, ffmpeg не нужен; спектрограмма — готовой
картинкой ffmpeg showspectrumpic (ffmpeg на GPU-машине опционален: нет
бинарника — эндпоинт спектра отказывает, волна громкости работает без него).
"""
from pathlib import Path

# Версии формата кэшей (как SCORE_V у score.json): смена расчёта без смены
# версии отдавала бы старые сайдикары. v2 — разрешение огибающей 60 окон/с
# (v1 = 10 рисовала контур «квадратными» ступеньками по 100 мс).
PEAKS_V = 2
SPECTRUM_V = 1

# Пики: каноническое разрешение — 60 окон/с (окно ~17 мс — различимы удары
# бочки); канва студии агрегирует окна в пиксели. Канонический размер
# стабилен для файла → стабильный кэш.
PEAKS_PER_SEC = 60
DEFAULT_BINS_MIN, DEFAULT_BINS_MAX = 500, 20000
MIN_BINS, MAX_BINS = 100, 20000

# Размер картинки спектрограммы; ось X линейна по всей длительности трека.
SPEC_W, SPEC_H = 1600, 320


def default_bins(duration_sec: float) -> int:
    """Каноническое число окон по длительности (для кэша)."""
    n = round(duration_sec * PEAKS_PER_SEC)
    return max(DEFAULT_BINS_MIN, min(DEFAULT_BINS_MAX, n))


def clamp_bins(bins: int) -> int:
    return max(MIN_BINS, min(MAX_BINS, int(bins)))


def peaks_from_samples(y, bins: int) -> list[list[float]]:
    """Огибающая моно-сигнала: [min, max] по окнам, амплитуды как есть
    (без нормализации — тихый трек выглядит тихим, файлы сравнимы).
    Хвост паддится нулями до кратного окнам размера."""
    import numpy as np

    n = int(bins)   # кламп bins — политика эндпоинта: здесь ровно столько, сколько попросили
    if n <= 0 or len(y) == 0:
        return []
    w = -(-len(y) // n)                       # ceil: окно может быть шире
    pad = n * w - len(y)
    if pad:
        y = np.concatenate([y, np.zeros(pad, dtype=y.dtype)])
    m = y.reshape(n, w)
    lo, hi = m.min(axis=1), m.max(axis=1)
    return [[round(float(a), 4), round(float(b), 4)] for a, b in zip(lo, hi, strict=True)]


def compute_peaks(path: Path, bins: int) -> dict:
    """Огибающая аудиофайла: mono = среднее каналов; ответ для /jobs/{id}/peaks."""
    import soundfile as sf

    data, sr = sf.read(str(path), always_2d=True, dtype="float32")
    y = data.mean(axis=1) if data.shape[1] > 1 else data[:, 0]
    n = clamp_bins(bins)
    return {
        "_v": PEAKS_V,
        "file": path.name,
        "bins": n,
        "duration_sec": round(len(y) / sr, 3),
        "peaks": peaks_from_samples(y, n),
    }


def render_spectrum_png(src: Path, dst: Path) -> dict:
    """Спектрограмма showspectrumpic → PNG по пути dst; метаданные для кэша."""
    import shutil
    import subprocess

    ffmpeg = shutil.which("ffmpeg")
    if not ffmpeg:
        raise FileNotFoundError("ffmpeg not available on worker (spectrum only; peaks work)")
    cmd = [ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-i", str(src),
           "-lavfi", f"showspectrumpic=s={SPEC_W}x{SPEC_H}:legend=0",
           "-frames:v", "1", str(dst)]
    proc = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
    if proc.returncode != 0 or not dst.is_file():
        raise RuntimeError(f"ffmpeg failed: {(proc.stderr or '').strip()[:300]}")
    return {"_v": SPECTRUM_V, "w": SPEC_W, "h": SPEC_H}
