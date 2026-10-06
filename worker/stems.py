"""Разделение трека на стемы: demucs (htdemucs, по умолчанию — быстро) или по выбору
BS-Roformer-SW (чище, ~4,5× дольше; отдельное окружение, см. sep_run.py); demucs — и
запасной путь при сбое RoFormer, и откат (YUE_STEMS_MODEL=demucs).

Модель грузится на вызов и выгружается: не держим VRAM рядом с YuE2.
"""
import json
import logging
import os
import shutil
import subprocess
import time
from pathlib import Path

log = logging.getLogger("yue-worker.stems")

# основные дорожки (htdemucs): в сумме дают трек — по ним минус и замены
MAIN_STEMS = ("drums", "bass", "other", "vocals")
# подробные дорожки (htdemucs_6s): гитара и клавиши — уточнение внутри
# «прочего», в сумму трека не входят (иначе гитара удвоится)
DETAIL_STEMS = ("guitar", "piano")
# части барабанов (DrumSep, только при RoFormer): уточнение внутри «барабанов»,
# в сумму трека не входят — минус и вклейки их не видят
DRUM_PARTS = ("kick", "snare", "toms", "hh", "ride", "crash")

MODEL_ROFORMER = "bs-roformer-sw"
MODEL_DEMUCS = "htdemucs"
SEP_TIMEOUT = 1800  # с: 4-минутный трек — около минуты, запас на первую загрузку весов


def _sep_python() -> Path:
    """python отдельного окружения разделения (YUE_SEP_PY, по умолчанию ~/sep-venv)."""
    return Path(os.environ.get("YUE_SEP_PY") or Path.home() / "sep-venv" / "bin" / "python")


def _sep_models() -> Path:
    """Каталог весов RoFormer/DrumSep (YUE_SEP_MODELS, по умолчанию ~/sep-models)."""
    return Path(os.environ.get("YUE_SEP_MODELS") or Path.home() / "sep-models")


def roformer_available() -> bool:
    """Окружение RoFormer установлено (YUE_SEP_PY есть)."""
    return _sep_python().is_file()


def roformer_enabled(prefer: str = MODEL_DEMUCS) -> bool:
    """RoFormer — только если его выбрали (prefer), окружение есть и не включён
    принудительный откат YUE_STEMS_MODEL=demucs."""
    if os.environ.get("YUE_STEMS_MODEL", "").strip().lower() == "demucs":
        return False
    return prefer in ("roformer", MODEL_ROFORMER) and roformer_available()


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


def _read_as(path: Path, sr: int, n: int):
    """Дорожка модели → стерео float32 в частоте трека sr и длиной n сэмплов."""
    import numpy as np
    import soundfile as sf
    x, xsr = sf.read(str(path), dtype="float32", always_2d=True)
    if x.shape[1] == 1:
        x = np.concatenate([x, x], axis=1)
    if xsr != sr:  # audio-separator пишет в частоте модели (44,1 кГц)
        import librosa
        x = librosa.resample(x.T, orig_sr=xsr, target_sr=sr).T.astype(np.float32)
    if len(x) < n:
        x = np.pad(x, ((0, n - len(x)), (0, 0)))
    return x[:n]


def _distribute_residual(main: dict, mix, sr: int) -> dict:
    """Остаток «трек − сумма основных» раздаётся основным дорожкам пропорционально
    их амплитуде на каждой частоте и в каждый момент (кусками по 20 с). Сумма
    после этого равна треку точно — на этом держатся минус и вклейки; у RoFormer
    без этого в низах терялось до −25 дБ трека."""
    import numpy as np
    import librosa
    names = list(main)
    out = {k: v.copy() for k, v in main.items()}
    n_fft, hop, step = 2048, 512, 20 * sr
    for a in range(0, len(mix), step):
        b = min(len(mix), a + step)
        for ch in range(mix.shape[1]):
            res = mix[a:b, ch] - sum(main[k][a:b, ch] for k in names)
            if not np.any(res):
                continue
            R = librosa.stft(res, n_fft=n_fft, hop_length=hop)
            mags = [np.abs(librosa.stft(main[k][a:b, ch], n_fft=n_fft, hop_length=hop)) for k in names]
            tot = sum(mags) + 1e-9
            parts = [librosa.istft(R * (m / tot), hop_length=hop, length=b - a) for m in mags]
            # что не раздалось (тишина во всех дорожках, края) — в «прочее»
            left = res - sum(parts)
            for k, p in zip(names, parts, strict=True):
                out[k][a:b, ch] += p
            out["other"][a:b, ch] += left
    return out


def _separate_roformer(audio_path: Path, out_dir: Path, sr: int, n: int, mix) -> list:
    """RoFormer-SW подпроцессом → контракт файлов приложения. Исключение — при
    любом сбое основного разделения (вызывающий уходит на demucs)."""
    import numpy as np
    import soundfile as sf
    tmp = out_dir / ".sep-tmp"
    shutil.rmtree(tmp, ignore_errors=True)
    tmp.mkdir(parents=True)
    try:
        cmd = [str(_sep_python()), str(Path(__file__).parent / "sep_run.py"),
               str(audio_path), str(tmp), str(_sep_models())]
        p = subprocess.run(cmd, capture_output=True, text=True, timeout=SEP_TIMEOUT)
        if p.returncode != 0:
            raise RuntimeError(f"sep_run exit {p.returncode}: {p.stderr.strip()[-500:]}")
        res = json.loads(p.stdout.strip().splitlines()[-1])
        got = res.get("stems") or {}
        need = {"vocals", "drums", "bass", "other"}
        if not need <= set(got):
            raise RuntimeError(f"sep_run: нет дорожек {sorted(need - set(got))}")
        st = {k: _read_as(Path(v), sr, n) for k, v in got.items()}
        zero = np.zeros((n, 2), dtype=np.float32)
        # у SW шесть дорожек в сумме дают трек; у приложения «прочее» включает гитару и клавиши
        main = {"drums": st["drums"], "bass": st["bass"], "vocals": st["vocals"],
                "other": st["other"] + st.get("guitar", zero) + st.get("piano", zero)}
        main = _distribute_residual(main, mix, sr)
        names = []
        for name in MAIN_STEMS:
            sf.write(str(out_dir / f"stem-{name}.flac"), main[name], sr)
            names.append(f"stem-{name}.flac")
        for name in DETAIL_STEMS:
            if name in st:
                sf.write(str(out_dir / f"stem-{name}.flac"), st[name], sr)
                names.append(f"stem-{name}.flac")
        if res.get("drum_error"):
            log.warning("DrumSep: части барабанов не выделены (%s): %s", audio_path.name, res["drum_error"])
        for name, f in (res.get("drum_parts") or {}).items():
            if name in DRUM_PARTS:
                sf.write(str(out_dir / f"stem-{name}.flac"), _read_as(Path(f), sr, n), sr)
                names.append(f"stem-{name}.flac")
        return names
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def separate(audio_path: Path, out_dir: Path, prefer: str = MODEL_DEMUCS) -> dict:
    """→ out_dir/stem-{drums,bass,other,vocals}.flac (+ stem-{guitar,piano}.flac,
    при RoFormer ещё stem-{kick,snare,toms,hh,ride,crash}.flac).
    Возвращает {stems, seconds, model}. prefer — «roformer» (чище, ~4,5× дольше)
    или «htdemucs» (быстро, по умолчанию). RoFormer — если выбран и доступен; его
    сбой уводит на demucs. Подробные дорожки — по возможности."""
    import numpy as np
    import soundfile as sf

    audio_path, out_dir = Path(audio_path), Path(out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)
    data, sr = sf.read(str(audio_path), dtype="float32", always_2d=True)
    if not len(data):
        raise ValueError(f"в файле нет звука: {audio_path.name}")
    if data.shape[1] == 1:
        data = np.concatenate([data, data], axis=1)
    t0 = time.time()
    # части барабанов от прошлого разделения не должны пережить новое (demucs их не делает)
    for name in DRUM_PARTS:
        (out_dir / f"stem-{name}.flac").unlink(missing_ok=True)
    if roformer_enabled(prefer):
        try:
            names = _separate_roformer(audio_path, out_dir, sr, len(data), data)
            log.info("roformer: %s stems from %s in %.1fs", len(names), audio_path.name, time.time() - t0)
            return {"stems": names, "seconds": round(time.time() - t0, 1), "model": MODEL_ROFORMER}
        except Exception:  # noqa: BLE001 — запасной путь: прежний demucs
            log.exception("roformer: разделение не удалось, запасной путь demucs (%s)", audio_path.name)
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
    return {"stems": names, "seconds": round(time.time() - t0, 1), "model": MODEL_DEMUCS}
