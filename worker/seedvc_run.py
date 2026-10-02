"""Замена тембра голоса Seed-VC. Запускается интерпретатором venv Seed-VC
(см. seedvc_install.sh); в основном venv воркера Seed-VC нет — отдельные
зависимости и лицензия (GPL-3.0).

usage:
  seedvc_run.py --dir D --source src.wav --target ref.wav --out out.wav [--steps 50]
  seedvc_run.py --dir D --warmup        # скачать веса и проверить запуск
→ JSON в stdout {out, seconds}
"""
import argparse
import glob
import json
import os
import shutil
import sys
import tempfile
import time


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", required=True)
    ap.add_argument("--source")
    ap.add_argument("--target")
    ap.add_argument("--out")
    ap.add_argument("--steps", type=int, default=50)
    ap.add_argument("--warmup", action="store_true")
    a = ap.parse_args()

    code = os.path.join(os.path.abspath(a.dir), "code")
    # веса Hugging Face — внутри каталога Seed-VC, не в домашнем кэше
    os.environ.setdefault("HF_HOME", os.path.join(os.path.abspath(a.dir), "hf"))
    # Seed-VC кладёт свои веса в ./checkpoints относительно рабочего каталога
    os.chdir(code)
    sys.path.insert(0, code)

    import soundfile as sf
    import torchaudio

    # torchaudio ≥ 2.9 пишет файлы через torchcodec — его в venv нет и он не нужен
    def _save(path, wav, sr, **_):
        sf.write(path, wav.detach().cpu().numpy().T, sr)

    torchaudio.save = _save
    import inference  # noqa: E402 — после chdir и подмены save

    if a.warmup:
        src = sorted(glob.glob("examples/source/*.wav"))[0]
        tgt = sorted(glob.glob("examples/reference/*.wav"))[0]
        steps = 2
    else:
        src, tgt, steps = a.source, a.target, a.steps
    out_dir = tempfile.mkdtemp(prefix="seedvc-")
    t0 = time.time()
    inference.main(argparse.Namespace(
        source=src, target=tgt, output=out_dir, diffusion_steps=steps, length_adjust=1.0,
        inference_cfg_rate=0.7, f0_condition=True, auto_f0_adjust=False, semi_tone_shift=0,
        checkpoint=None, config=None, fp16=True))
    produced = glob.glob(os.path.join(out_dir, "*.wav"))
    if not produced:
        raise SystemExit("seed-vc не записал результат")
    out = a.out or os.path.join(os.path.abspath(a.dir), "warmup.wav")
    shutil.move(produced[0], out)
    shutil.rmtree(out_dir, ignore_errors=True)
    print(json.dumps({"out": out, "seconds": round(time.time() - t0, 1)}))


if __name__ == "__main__":
    main()
