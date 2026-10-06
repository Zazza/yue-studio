"""Разделение трека BS-Roformer-SW (+ барабаны по частям, DrumSep) — запускается
воркером подпроцессом в ОТДЕЛЬНОМ окружении (`YUE_SEP_PY`, по умолчанию
~/sep-venv): python-audio-separator тянет свой torch/onnxruntime, в окружении
YuE2 они бы конфликтовали. Подпроцесс к тому же сам освобождает видеопамять.

    python sep_run.py <аудио> <каталог_выхода> <каталог_весов>

Пишет в каталог выхода сырые дорожки модели как есть (<имя>.wav, частота
модели) и печатает последней строкой JSON:
    {"stems": {"vocals": путь, ...}, "drum_parts": {"kick": путь, ...},
     "drum_error": "" | текст}
Сбой DrumSep не роняет основное разделение (drum_parts пусто, drum_error).
Веса (некоммерческие, CC BY-NC-SA) качаются при первом вызове в каталог весов.
"""
import json
import os
import sys
from pathlib import Path

# vocals/drums/bass/guitar/piano/other; в сумме ≈ трек (точно — после stems._distribute_residual)
MAIN_MODEL = "BS-Roformer-SW.ckpt"
DRUM_MODEL = "MDX23C-DrumSep-aufr33-jarredou.ckpt"  # kick/snare/toms/hh/ride/crash


def _run(model: str, src: Path, out_dir: Path, models_dir: Path, prefix: str) -> dict:
    from audio_separator.separator import Separator
    # куски пачкой по 4: на свободной видеокарте ~в 1,4 раза быстрее, чем по одному
    sep = Separator(output_dir=str(out_dir), model_file_dir=str(models_dir),
                    output_format="WAV", log_level=40,
                    mdxc_params={"segment_size": 256, "override_model_segment_size": False,
                                 "batch_size": 4, "overlap": None, "pitch_shift": 0})
    sep.load_model(model_filename=model)
    out = {}
    for f in sep.separate(str(src)):
        p = Path(f) if os.path.isabs(f) else out_dir / f
        # имя дорожки — в скобках: «трек_(Guitar)_BS-Roformer-SW.wav»
        name = p.name.rsplit("(", 1)[-1].split(")")[0].lower()
        dst = out_dir / f"{prefix}{name}.wav"
        os.replace(p, dst)
        out[name] = str(dst)
    return out


def main(argv: list[str]) -> int:
    src, out_dir, models_dir = Path(argv[1]), Path(argv[2]), Path(argv[3])
    out_dir.mkdir(parents=True, exist_ok=True)
    models_dir.mkdir(parents=True, exist_ok=True)
    stems = _run(MAIN_MODEL, src, out_dir, models_dir, "")
    parts, err = {}, ""
    if "drums" in stems:
        try:
            parts = _run(DRUM_MODEL, Path(stems["drums"]), out_dir, models_dir, "drums-")
        except Exception as e:  # noqa: BLE001 — части барабанов необязательны
            err = f"{type(e).__name__}: {e}"
    print(json.dumps({"stems": stems, "drum_parts": parts, "drum_error": err}))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
