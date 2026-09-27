"""SheetSage2 (m-a-p/SheetSage2, 57M) — транскрипция аудио в лид-лист ABC.

Модель ленивая и маленькая — держим загруженной после первого вызова;
с YuE2 не пересекается по VRAM (сотни МБ).
"""
import json
import logging
import threading
import time
from pathlib import Path

log = logging.getLogger("yue-worker.sheetsage")

_model = None
_lock = threading.Lock()
_load_error: str | None = None


def get_model():
    global _model, _load_error
    with _lock:
        if _model is None:
            if _load_error:
                raise RuntimeError(f"SheetSage2 load failed earlier: {_load_error}")
            from transformers import AutoModel
            try:
                t0 = time.time()
                _model = AutoModel.from_pretrained(
                    "m-a-p/SheetSage2", trust_remote_code=True).eval().to("cuda")
                log.info("SheetSage2 loaded in %.1fs", time.time() - t0)
            except Exception as e:  # noqa: BLE001
                _load_error = str(e)
                raise
        return _model


def transcribe(audio_path: Path, out_dir: Path) -> dict:
    """Транскрипция файла → артефакты в out_dir (score.abc, *.lab, midi...).

    Возвращает {abc, seconds, warnings, stats}.
    """
    import soundfile as sf
    import torch

    out_dir.mkdir(parents=True, exist_ok=True)
    data, sr = sf.read(str(audio_path), dtype="float32", always_2d=True)
    mono = data.mean(axis=1)
    model = get_model()
    t0 = time.time()
    with torch.no_grad():
        result = model.transcribe(torch.from_numpy(mono), sampling_rate=sr,
                                  output_dir=str(out_dir))
    seconds = round(time.time() - t0, 1)
    return {
        "abc": result.get("abc") or "",
        "seconds": seconds,
        "warnings": [str(w) for w in (result.get("warnings") or [])],
        "stats": summarize(out_dir),
    }


def _read_lab(path: Path) -> list[list[str]]:
    rows = []
    if not path.is_file():
        return rows
    for line in path.read_text().splitlines():
        parts = line.split("\t")
        if len(parts) >= 3:
            rows.append(parts)
    return rows


def summarize(out_dir: Path) -> dict:
    """Статистика по артефактам транскрипции: тональности, аккорды, структура."""
    keys: dict[str, float] = {}
    for start, end, label in _read_lab(out_dir / "key.lab"):
        keys[label.strip()] = keys.get(label.strip(), 0.0) + float(end) - float(start)
    chords: dict[str, int] = {}
    chord_seq: list[str] = []
    for _, _, label in _read_lab(out_dir / "chord.lab"):
        lab = label.strip()
        chord_seq.append(lab)
        chords[lab] = chords.get(lab, 0) + 1
    structure = [r[2].strip() for r in _read_lab(out_dir / "structure.lab")]
    # прогрессии: пары соседних аккордов
    pairs: dict[str, int] = {}
    for a, b in zip(chord_seq, chord_seq[1:]):
        if a != b:
            pairs[f"{a}->{b}"] = pairs.get(f"{a}->{b}", 0) + 1
    return {
        "keys": dict(sorted(keys.items(), key=lambda kv: -kv[1])),
        "top_chords": dict(sorted(chords.items(), key=lambda kv: -kv[1])[:12]),
        "top_progressions": dict(sorted(pairs.items(), key=lambda kv: -kv[1])[:12]),
        "structure": structure,
    }
