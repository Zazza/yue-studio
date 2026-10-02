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
INSTR_STEMS = ("drums", "bass", "other")


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


def match_gain(source: np.ndarray, converted: np.ndarray) -> float:
    """Множитель для converted, чтобы его активный уровень совпал с source."""
    a, b = active_rms(source), active_rms(converted)
    return a / b if a > 0 and b > 0 else 1.0


def mix(instrumental: list[np.ndarray], vocal: np.ndarray, gain: float = 1.0) -> np.ndarray:
    """Сумма дорожек (frames, ch) и голоса: длина — по самой длинной дорожке
    музыки (голос обрезается/дополняется тишиной), моно-голос — в оба канала;
    пики выше 0.99 мягко прижимаются, чтобы не клиповать."""
    length = max(len(s) for s in instrumental)
    ch = max(s.shape[1] if s.ndim > 1 else 1 for s in instrumental)
    out = np.zeros((length, ch), dtype=np.float64)
    for s in instrumental:
        s2 = s if s.ndim > 1 else s[:, None]
        out[: len(s2)] += s2[:, :ch] if s2.shape[1] >= ch else np.repeat(s2, ch, axis=1)
    v = vocal if vocal.ndim > 1 else vocal[:, None]
    if v.shape[1] < ch:
        v = np.repeat(v[:, :1], ch, axis=1)
    n = min(length, len(v))
    out[:n] += v[:n, :ch] * gain
    peak = np.abs(out).max() if out.size else 0.0
    if peak > 0.99:
        out *= 0.99 / peak
    return out.astype(np.float32)


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


def run(src_dir: Path, ref_dir: Path, out_dir: Path, ref_from: float, ref_dur: float, steps: int) -> Path:
    """Голос трека src_dir (дорожки stem-*.flac уже есть) — тембром голоса из
    ref_dir в окне [ref_from, ref_from + ref_dur) → out_dir/audio.flac."""
    import soundfile as sf

    vocals, sr = sf.read(str(src_dir / "stem-vocals.flac"), dtype="float32", always_2d=True)
    ref, rsr = sf.read(str(ref_dir / "stem-vocals.flac"), dtype="float32", always_2d=True)
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
    # «озвучивает» утечки гитар в паузах дорожки голоса
    mask = activity_mask(vocals, sr)
    k = min(len(vc), len(mask))
    vc = vc[:k] * (mask[:k, None] if vc.ndim > 1 else mask[:k])
    music = [sf.read(str(src_dir / f"stem-{s}.flac"), dtype="float32", always_2d=True)[0] for s in INSTR_STEMS]
    out = mix(music, vc, match_gain(vocals, vc))
    out_dir.mkdir(parents=True, exist_ok=True)
    dst = out_dir / "audio.flac"
    sf.write(str(dst), out, sr)
    return dst
