"""demucs (htdemucs) — разделение трека на стемы.

Модель грузится на вызов и выгружается: не держим VRAM рядом с YuE2.
"""
import logging
import time
from pathlib import Path

log = logging.getLogger("yue-worker.stems")

# основные дорожки (htdemucs): в сумме дают трек — по ним минус и замены
MAIN_STEMS = ("drums", "bass", "other", "vocals")
# подробные дорожки (htdemucs_6s): гитара и клавиши — уточнение внутри
# «прочего», в сумму трека не входят (иначе гитара удвоится)
DETAIL_STEMS = ("guitar", "piano")


def _run_model(name: str, data, sr: int) -> dict:
    """Модель demucs name на звук data (сэмплы × каналы) → {источник: дорожка}
    в частоте sr. Модель выгружается сразу после прохода."""
    import torch
    from demucs.pretrained import get_model
    from demucs.apply import apply_model
    import torchaudio.functional as AF

    model = get_model(name)
    try:
        mix = torch.from_numpy(data.T).unsqueeze(0)
        if model.samplerate != sr:
            mix = AF.resample(mix, sr, model.samplerate)
        with torch.no_grad():
            # без видеокарты — на CPU (медленнее: ~4 с на секунду звука)
            device = "cuda" if torch.cuda.is_available() else "cpu"
            stems = apply_model(model, mix.to(device), split=True, overlap=0.25)[0]
        out = {}
        for i, src in enumerate(model.sources):
            stem = stems[i].cpu()
            if model.samplerate != sr:
                stem = AF.resample(stem, model.samplerate, sr)
            out[src] = stem.T.numpy()
        return out
    finally:
        del model
        if torch.cuda.is_available():
            torch.cuda.empty_cache()


def separate(audio_path: Path, out_dir: Path) -> dict:
    """→ out_dir/stem-{drums,bass,other,vocals}.flac (+ stem-{guitar,piano}.flac).
    Возвращает {stems, seconds}. Подробные дорожки — по возможности: сбой
    6-стемной модели (нет весов, сети) основные дорожки не роняет."""
    import numpy as np
    import soundfile as sf

    audio_path, out_dir = Path(audio_path), Path(out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)
    data, sr = sf.read(str(audio_path), dtype="float32", always_2d=True)
    if data.shape[1] == 1:
        data = np.concatenate([data, data], axis=1)
    t0 = time.time()
    names = []
    main = _run_model("htdemucs", data, sr)
    for name in MAIN_STEMS:
        sf.write(str(out_dir / f"stem-{name}.flac"), main[name], sr)
        names.append(f"stem-{name}.flac")
    try:
        detail = _run_model("htdemucs_6s", data, sr)
        for name in DETAIL_STEMS:
            sf.write(str(out_dir / f"stem-{name}.flac"), detail[name], sr)
            names.append(f"stem-{name}.flac")
    except Exception:  # noqa: BLE001 — основные дорожки уже есть, подробные необязательны
        log.exception("demucs 6s: гитара/клавиши не выделены (%s)", audio_path.name)
    log.info("demucs: %s stems from %s in %.1fs", len(names), audio_path.name, time.time() - t0)
    return {"stems": names, "seconds": round(time.time() - t0, 1)}
