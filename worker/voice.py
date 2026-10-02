"""«Голос альбома»: тембр голоса трека заменяется на тембр образца (Seed-VC).

Seed-VC живёт в своём каталоге и venv (см. seedvc_install.sh) и вызывается
отдельной программой (seedvc_run.py) — как whisper: зависимости и лицензия
(GPL-3.0) не смешиваются с воркером. Здесь — подготовка дорожек, вызов и
сведение результата с музыкой трека. ffmpeg не нужен: soundfile + numpy.
"""
from __future__ import annotations

import json
import os
import subprocess
import tempfile
from pathlib import Path

import numpy as np

SEEDVC_DIR = Path(os.environ.get("YUE_SEEDVC_DIR", Path.home() / "yue-studio" / "seedvc"))
SEEDVC_PY = SEEDVC_DIR / "venv" / "bin" / "python"
RUNNER = Path(__file__).parent / "seedvc_run.py"

REF_MIN_SEC, REF_MAX_SEC = 3.0, 30.0   # Seed-VC берёт образец до 30 с
STEPS_MIN, STEPS_MAX = 10, 100


def available() -> bool:
    """Seed-VC установлен: есть его venv и код."""
    return SEEDVC_PY.is_file() and (SEEDVC_DIR / "code" / "inference.py").is_file()


def active_rms(x: np.ndarray, frame: int = 2048) -> float:
    """RMS по кадрам, где звучит голос (громче четверти 60-го процентиля): паузы
    между фразами не тянут уровень вниз. Пусто/тишина — 0."""
    x = np.asarray(x, dtype=np.float64)
    if x.ndim > 1:
        x = x.mean(axis=1)
    n = len(x) // frame
    if n == 0:
        return float(np.sqrt(np.mean(x ** 2))) if len(x) else 0.0
    rms = np.sqrt((x[: n * frame].reshape(n, frame) ** 2).mean(axis=1))
    loud = rms > np.percentile(rms, 60) * 0.25
    return float(np.sqrt((rms[loud] ** 2).mean())) if loud.any() else 0.0


GATE_FRAME_SEC = 0.02    # шаг огибающей голоса
GATE_HOLD_SEC = 0.15     # запас по краям фраз: вдохи и окончания не обрубаются
GATE_FADE_SEC = 0.03     # мягкий край ворот, без щелчка
GATE_FLOOR_DB = -55.0    # тише этого (dBFS) — голоса нет при любом треке


def activity_mask(x: np.ndarray, sr: int) -> np.ndarray:
    """Где в дорожке голоса действительно поют: 1.0 — голос, 0.0 — пауза, по
    сэмплу. Порог — выше и абсолютного пола GATE_FLOOR_DB, и уровня пауз
    трека (−30 дБ от громких мест); голос расширяется на GATE_HOLD_SEC в обе
    стороны, края сглажены на GATE_FADE_SEC. Тишина/пустой вход — нули."""
    x = np.asarray(x, dtype=np.float64)
    if x.ndim > 1:
        x = x.mean(axis=1)
    n = len(x)
    if n == 0:
        return np.zeros(0, dtype=np.float32)
    hop = max(1, int(GATE_FRAME_SEC * sr))
    frames = -(-n // hop)
    pad = np.zeros(frames * hop)
    pad[:n] = x
    rms = np.sqrt((pad.reshape(frames, hop) ** 2).mean(axis=1))
    db = 20 * np.log10(rms + 1e-12)
    thr = max(GATE_FLOOR_DB, np.percentile(db, 95) - 30.0)
    on = db > thr
    hold = int(round(GATE_HOLD_SEC / GATE_FRAME_SEC))
    if hold and on.any():
        on = np.convolve(on.astype(float), np.ones(2 * hold + 1), mode="same") > 0
    m = np.repeat(on.astype(np.float64), hop)[:n]
    fade = max(1, int(GATE_FADE_SEC * sr))
    m = np.convolve(m, np.ones(fade) / fade, mode="same")
    return np.clip(m, 0.0, 1.0).astype(np.float32)


def best_window(x: np.ndarray, sr: int, dur: float) -> float:
    """Начало окна длиной dur (с), где голос звучит большую часть времени
    (по кадрам 0.1 с; «звучит» — порог как у activity_mask: −30 дБ от громких
    мест, не тише GATE_FLOOR_DB, — утечки в паузах не считаются, сколько бы
    их ни было) — образец без пауз и проигрышей. Дорожка короче окна — 0."""
    x = np.asarray(x, dtype=np.float64)
    if x.ndim > 1:
        x = x.mean(axis=1)
    hop = max(1, int(sr * 0.1))
    n = len(x) // hop
    w = int(round(dur / 0.1))
    if n <= w:
        return 0.0
    rms = np.sqrt((x[: n * hop].reshape(n, hop) ** 2).mean(axis=1))
    db = 20 * np.log10(rms + 1e-12)
    act = (db > max(GATE_FLOOR_DB, np.percentile(db, 95) - 30.0)).astype(np.float64)
    sums = np.convolve(act, np.ones(w), mode="valid")   # голос в каждом окне
    return float(np.argmax(sums)) * 0.1


def match_gain(source: np.ndarray, converted: np.ndarray) -> float:
    """Множитель для converted, чтобы его активный уровень совпал с source."""
    a, b = active_rms(source), active_rms(converted)
    return a / b if a > 0 and b > 0 else 1.0


def replace_vocal(mix: np.ndarray, vocals: np.ndarray, new: np.ndarray, mask: np.ndarray,
                  gain: float = 1.0) -> np.ndarray:
    """Заменить голос в готовом миксе: mix − mask·vocals + mask·gain·new.
    Микс берётся целиком, а не собирается из дорожек demucs (сумма дорожек ≠
    микс — музыка портилась и там, где голоса нет): вне маски результат бит в
    бит равен миксу. Все массивы — (frames[, ch]) одной частоты; моно-голос
    идёт в оба канала; длина — как у микса (короткие дополняются тишиной).
    Пики выше 0.99 прижимаются локально (limit_peaks), остальное не меняется."""
    mix = np.asarray(mix, dtype=np.float64)
    if mix.ndim == 1:
        mix = mix[:, None]
    n, ch = mix.shape

    def fit(x):
        x = np.asarray(x, dtype=np.float64)
        if x.ndim == 1:
            x = x[:, None]
        if x.shape[1] < ch:
            x = np.repeat(x[:, :1], ch, axis=1)
        out = np.zeros((n, ch))
        k = min(n, len(x))
        out[:k] = x[:k, :ch]
        return out

    m = np.zeros(n)
    k = min(n, len(mask))
    m[:k] = np.asarray(mask, dtype=np.float64)[:k]
    out = mix + m[:, None] * (gain * fit(new) - fit(vocals))
    return limit_peaks(out).astype(np.float32)


LIMIT = 0.99
LIMIT_WIN = 256   # сэмплов (~5 мс на 48 кГц): ширина плавного края ограничителя


def limit_peaks(x: np.ndarray, limit: float = LIMIT, win: int = LIMIT_WIN) -> np.ndarray:
    """Прижать только пики выше limit — локальным усилением с плавным краем
    (минимум по окну 2·win + сглаживание по win), не приглушая весь трек: дальше
    win сэмплов от пиков сигнал не меняется бит в бит."""
    x = np.asarray(x, dtype=np.float64)
    if x.size == 0:
        return x
    mag = np.abs(x).max(axis=1) if x.ndim > 1 else np.abs(x)
    if mag.max() <= limit:
        return x
    from scipy.ndimage import minimum_filter1d, uniform_filter1d
    need = np.minimum(1.0, limit / np.maximum(mag, 1e-12))
    g = uniform_filter1d(minimum_filter1d(need, size=2 * win + 1, mode="nearest"), size=win + 1, mode="nearest")
    g = np.minimum(g, need)        # сглаживание не должно поднять усиление над нужным
    return x * (g[:, None] if x.ndim > 1 else g)


def resample(x: np.ndarray, sr_from: int, sr_to: int) -> np.ndarray:
    """Пересчёт частоты (frames[, ch]) полифазным фильтром; та же частота — без изменений."""
    if sr_from == sr_to:
        return x
    from math import gcd

    from scipy.signal import resample_poly
    g = gcd(int(sr_from), int(sr_to))
    return resample_poly(x, int(sr_to) // g, int(sr_from) // g, axis=0).astype(np.float32)


def convert(source_wav: Path, ref_wav: Path, out_wav: Path, steps: int, timeout: int = 3600) -> dict:
    """Вызов seedvc_run.py интерпретатором Seed-VC → {out, seconds}."""
    r = subprocess.run(
        [str(SEEDVC_PY), str(RUNNER), "--dir", str(SEEDVC_DIR), "--source", str(source_wav),
         "--target", str(ref_wav), "--out", str(out_wav), "--steps", str(steps)],
        capture_output=True, text=True, timeout=timeout)
    if r.returncode != 0:
        tail = (r.stderr or r.stdout).strip().splitlines()[-5:]
        raise RuntimeError("seed-vc: " + " | ".join(tail))
    return json.loads(r.stdout.strip().splitlines()[-1])


def run(src_dir: Path, audio_name: str, ref_dir: Path, out_dir: Path, ref_from: float | None, ref_dur: float,
        steps: int) -> Path:
    """Голос трека src_dir (микс audio_name и дорожка stem-vocals.flac уже есть) —
    тембром голоса из ref_dir в окне [ref_from, ref_from + ref_dur) → out_dir/audio.flac.
    ref_from None — окно подбирается само (best_window)."""
    import soundfile as sf

    vocals, sr = sf.read(str(src_dir / "stem-vocals.flac"), dtype="float32", always_2d=True)
    mix, msr = sf.read(str(src_dir / audio_name), dtype="float32", always_2d=True)
    if msr != sr:
        mix = resample(mix, msr, sr)
    ref, rsr = sf.read(str(ref_dir / "stem-vocals.flac"), dtype="float32", always_2d=True)
    if ref_from is None:
        ref_from = best_window(ref, rsr, ref_dur)
    ref = ref[int(ref_from * rsr): int((ref_from + ref_dur) * rsr)].mean(axis=1)
    if len(ref) < REF_MIN_SEC * rsr:
        raise ValueError("образец голоса короче 3 с — сдвинь начало окна")
    with tempfile.TemporaryDirectory(prefix="yue-voice-") as tmp:
        t = Path(tmp)
        sf.write(str(t / "src.wav"), vocals.mean(axis=1), sr)
        sf.write(str(t / "ref.wav"), ref, rsr)
        convert(t / "src.wav", t / "ref.wav", t / "vc.wav", steps)
        vc, vsr = sf.read(str(t / "vc.wav"), dtype="float32", always_2d=True)
    vc = resample(vc, vsr, sr)   # Seed-VC поёт на 44.1 кГц, дорожки YuE — 48 кГц
    # новый голос — только там, где пел исходный: Seed-VC шумит на старте и
    # «озвучивает» утечки гитар в паузах дорожки голоса; вне фраз — исходный микс
    out = replace_vocal(mix, vocals, vc, activity_mask(vocals, sr), match_gain(vocals, vc))
    out_dir.mkdir(parents=True, exist_ok=True)
    dst = out_dir / "audio.flac"
    sf.write(str(dst), out, sr)
    return dst
