"""demucs (htdemucs) — разделение трека на стемы.

Модель грузится на вызов и выгружается: не держим VRAM рядом с YuE2.
"""
import logging
import time
from pathlib import Path

log = logging.getLogger("yue-worker.stems")


def separate(audio_path: Path, out_dir: Path) -> dict:
    """→ out_dir/stem-{drums,bass,other,vocals}.flac. Возвращает {stems, seconds}."""
    import soundfile as sf
    import torch
    from demucs.pretrained import get_model
    from demucs.apply import apply_model

    out_dir.mkdir(parents=True, exist_ok=True)
    data, sr = sf.read(str(audio_path), dtype="float32", always_2d=True)
    if data.shape[1] == 1:
        data = __import__("numpy").concatenate([data, data], axis=1)
    t0 = time.time()
    model = get_model("htdemucs")
    try:
        mix = torch.from_numpy(data.T).unsqueeze(0)
        if model.samplerate != sr:
            import torchaudio.functional as AF
            mix = AF.resample(mix, sr, model.samplerate)
        with torch.no_grad():
            stems = apply_model(model, mix.to("cuda"), split=True, overlap=0.25)[0]
        names = []
        for i, name in enumerate(model.sources):
            stem = stems[i].T.cpu().numpy()
            if model.samplerate != sr:
                import torch as _t
                stem = AF.resample(_t.from_numpy(stem.T), model.samplerate, sr).T.numpy()
            sf.write(str(out_dir / f"stem-{name}.flac"), stem, sr)
            names.append(f"stem-{name}.flac")
    finally:
        del model
        torch.cuda.empty_cache()
    log.info("demucs: %s stems from %s in %.1fs", len(names), audio_path.name, time.time() - t0)
    return {"stems": names, "seconds": round(time.time() - t0, 1)}
