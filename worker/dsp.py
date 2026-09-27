"""DSP-карта трека в JSON: темп, тональность, динамика, шумовое полотно, спектр, жёсткость, стерео.

librosa-анализатор аудио под машинное чтение:
метрики едут в приложение и сравниваются с референсом дельтой.
"""
from __future__ import annotations

import numpy as np


NOTES = ["C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"]


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

    return m
