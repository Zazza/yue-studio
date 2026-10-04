"""Yue Studio worker — API вокруг резидентной YuE2Pipeline (:8091, 0.0.0.0).

Держит модель загруженной между запросами; очередь задач живёт здесь же
(SQLite), так что закрытие desktop-приложения не прерывает генерации.

Эндпоинты:
  GET  /health           — статус, загружена ли модель
  POST /jobs             — постановка в очередь {title, style, lyrics, seed, cot, abc?}
  GET  /jobs             — список последних задач
  GET  /jobs/{id}        — статус задачи
  POST /jobs/{id}/cancel — отмена (только queued)
  POST /jobs/{id}/continue — продолжение с места {from_sec, seed?, abc?, style_add?} → новая джоба-вложение
  POST /jobs/{id}/head   — основная версия песни {head_id} (сам трек или его потомок)
  POST /plan             — только стадия плана: ABC до рендера {style, lyrics, seed, cot}
  GET  /listen/{id}      — страница прослушивания
  GET  /audio/{id}/{f}   — артефакты задачи (audio.flac, score.abc, request.abc, ...)
"""
import json
import shutil
from collections.abc import Iterator
from contextlib import contextmanager
import logging
import os
import random
import re
import sqlite3
import tempfile
import threading
import time
import urllib.request

import arc
import llm
import media
import waveform
from pathlib import Path

from fastapi import FastAPI, HTTPException, Query, Request
from fastapi.responses import FileResponse, HTMLResponse
from pydantic import BaseModel, Field, field_validator

from abcparse import parse_abc
from plancheck import plan_diff
from dsp import analyze_file, beat_grid, vocal_activity
from sheetsage import transcribe as ss_transcribe
from stems import DETAIL_STEMS, MAIN_STEMS, separate as demucs_separate
import voice as voicevc

log = logging.getLogger("yue-worker")
logging.basicConfig(level=logging.INFO)

DATA_DIR = Path(os.environ.get("YUE_DATA_DIR", Path.home() / "yue-studio" / "data"))
JOBS_DIR = DATA_DIR / "jobs"
REFS_DIR = DATA_DIR / "references"
TRANSCRIBES_DIR = DATA_DIR / "transcribes"
CORPUS_DIR = DATA_DIR / "corpus"
VOICES_DIR = DATA_DIR / "voices"
DB_PATH = DATA_DIR / "yue.db"
for d in (JOBS_DIR, REFS_DIR, TRANSCRIBES_DIR, CORPUS_DIR, VOICES_DIR):
    d.mkdir(parents=True, exist_ok=True)

WHISPER_PY = Path(os.environ.get("YUE_WHISPER_PY", Path.home() / "whisper-venv" / "bin" / "python"))

# Бюджет семантических токенов на песню (дефолт протокола 9000 ≈ 4.5–6 мин;
# контекст модели 24576 общий: стиль+лирика+план+песня). 16000 ≈ до ~10 мин,
# пик VRAM растёт с длиной — для 16 ГБ больше не поднимать.
MAX_SEM_TOKENS = int(os.environ.get("YUE2_MAX_TOKENS", "16000"))
# cfg_scale (classifier-free guidance): выше — точнее следует стилю, но суше;
# 1.5 — сбалансированное среднее. 0/отсутствие — дефолт библиотеки.
CFG_SCALE = float(os.environ.get("YUE2_CFG_SCALE", "1.5"))
# сид, с которым yue2 генерирует без явного сида: так шли треки с пустым полем
# seed до того, как воркер стал выбирать случайный (у них в строке 0/NULL)
LIB_DEFAULT_SEED = 831001
# пределы характера исполнения на трек (за ними модель разваливается: шум/повторы)
TEMPERATURE_RANGE = (0.5, 1.5)
CFG_RANGE = (1.0, 4.0)

app = FastAPI(title="yue-worker")

_pipe = None
_pipe_lock = threading.Lock()   # одна операция с моделью одновременно
_load_lock = threading.Lock()
_load_error: str | None = None

db_lock = threading.Lock()


@contextmanager
def db() -> Iterator[sqlite3.Connection]:
    """Соединение с БД на один блок `with`: фиксация при успехе, откат при
    ошибке (как контекст sqlite3) и закрытие. Сам контекст sqlite3 соединение не
    закрывает — каждый запрос оставлял открытым файл БД до сборки мусора."""
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    try:
        with conn:
            yield conn
    finally:
        conn.close()


def init_db():
    with db_lock, db() as conn:
        conn.executescript("""
        CREATE TABLE IF NOT EXISTS jobs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            title TEXT DEFAULT '',
            status TEXT NOT NULL DEFAULT 'queued',
            style TEXT NOT NULL,
            lyrics TEXT NOT NULL,
            seed INTEGER,
            cot TEXT NOT NULL DEFAULT 'full',
            error TEXT DEFAULT '',
            duration_sec REAL DEFAULT 0,
            audio_file TEXT DEFAULT '',
            mp3_file TEXT DEFAULT '',
            wav_file TEXT DEFAULT '',
            abc_file TEXT DEFAULT '',
            created_at TEXT NOT NULL,
            finished_at TEXT DEFAULT ''
        );
        """)


def _migrate():
    with db_lock, db() as conn:
        cols = [r[1] for r in conn.execute("PRAGMA table_info(jobs)")]
        if "mp3_file" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN mp3_file TEXT DEFAULT ''")
        if "wav_file" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN wav_file TEXT DEFAULT ''")
        if "req_abc" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN req_abc TEXT DEFAULT ''")
        if "overdub_of" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN overdub_of INTEGER")
        if "overdub_gain" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN overdub_gain REAL DEFAULT 0.5")
        if "draft" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN draft INTEGER DEFAULT 0")
        if "arc" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN arc TEXT DEFAULT ''")
        if "max_tokens" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN max_tokens INTEGER DEFAULT 0")
        # производный трек (кусок для вклейки, пересборка, проверка куска,
        # вариант-трек): parent_id — от какого трека, role — зачем. В списке
        # треков такие прячутся под родителем («📎 N»), а не сыплются рядом.
        if "parent_id" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN parent_id INTEGER")
        if "role" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN role TEXT DEFAULT ''")
        # «продолжение с места»: с какой секунды родителя модель играет заново
        if "cont_from" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN cont_from REAL DEFAULT 0")
        # источник голоса версии: рендер, чей голос подставлен («перепеть с
        # места» берёт продолжение от него); NULL — голос свой/от родителя
        if "voice_src" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN voice_src INTEGER")
        # основная версия песни (у корня): с какой версией человек работает
        # сейчас — её играет карточка и открывает студия; правки копятся от неё
        if "head_id" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN head_id INTEGER")
        # папка песни («Альбом», «Основы», …): своя у корня, версии следуют
        # за ним; пусто — без папки
        if "folder" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN folder TEXT DEFAULT ''")
        # где в треке «без голоса» звучит дорожка голоса (секунды через запятую)
        if "vocal_leak" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN vocal_leak TEXT DEFAULT ''")
        # характер исполнения: температура (смелость игры) и cfg (точность по
        # стилю/нотам); 0 — по умолчанию (температура библиотеки, CFG_SCALE)
        if "temperature" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN temperature REAL DEFAULT 0")
        if "cfg" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN cfg REAL DEFAULT 0")
        conn.execute("""
        CREATE TABLE IF NOT EXISTS corpus (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            status TEXT NOT NULL DEFAULT 'open',
            created_at TEXT NOT NULL
        );
        """)
        conn.execute("""
        CREATE TABLE IF NOT EXISTS voices (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            job_id INTEGER,
            params TEXT NOT NULL DEFAULT '{}',
            seed INTEGER DEFAULT 0,
            created_at TEXT NOT NULL
        );
        """)


init_db()
_migrate()


def _get_pipe():
    global _pipe, _load_error
    with _load_lock:
        if _pipe is None:
            if _load_error:
                raise HTTPException(503, f"model load failed earlier: {_load_error}")
            from yue2 import YuE2Pipeline
            try:
                t0 = time.time()
                _pipe = YuE2Pipeline.from_pretrained(
                    "m-a-p/YuE2-3B", vae="m-a-p/YuE2-Vae", device="cuda",
                ).__enter__()
                log.info("pipeline loaded in %.1fs", time.time() - t0)
            except Exception as e:  # noqa: BLE001
                _load_error = str(e)
                raise HTTPException(503, f"model load failed: {e}") from e
        return _pipe


def _audio_duration(path: Path) -> float:
    try:
        import soundfile as sf
        return float(sf.info(str(path)).duration)
    except Exception:  # noqa: BLE001
        return 0.0


def _make_formats(job_dir: Path) -> tuple[str, str]:
    """Возвращает (mp3, wav) имена рядом с audio.flac."""
    src = job_dir / "audio.flac"
    if not src.exists():
        return "", ""
    try:
        import soundfile as sf
        data, sr = sf.read(str(src), always_2d=True, dtype="float32")
        sf.write(str(job_dir / "audio.wav"), data, sr, subtype="PCM_16")
        media.encode_mp3(src, job_dir / "audio.mp3")
        return "audio.mp3", "audio.wav"
    except Exception:  # noqa: BLE001
        log.exception("format convert failed")
        return "", ""


# отмена бегущих джоб и их живой прогресс (in-memory: живёт пока воркер)
_cancel_flags: dict[int, threading.Event] = {}
_progress: dict[int, dict] = {}
_state_lock = threading.Lock()


def _on_token_cb(job_id: int):
    """Счётчик токенов/фазы из колбэка пайплайна (plan/generate_semantic).

    plan_end фиксирует последний токен фазы плана: семантические токены =
    tokens - plan_end. Смешивать нельзя — план короткий, семантика длинная,
    и процент от смешанного счётчика доезжал до 99% задолго до конца."""
    counters = {"phase": "load", "tokens": 0, "plan_end": None}

    def on_token(*args):
        for a in args:
            if isinstance(a, str):
                new_phase = "plan" if "abc" in a.lower() else "semantic"
                if new_phase == "semantic" and counters["plan_end"] is None:
                    counters["plan_end"] = counters["tokens"]
                counters["phase"] = new_phase
        counters["tokens"] += 1

    return on_token, counters


def _sem_tokens(counters: dict) -> int:
    if counters.get("plan_end") is None:
        return 0
    return counters["tokens"] - counters["plan_end"]


def _progress_watcher(job_id: int, counters: dict, t0: float, budget: int = 0):
    """Раз в 2 с пишет прогресс в _progress (для /jobs)."""
    stop = threading.Event()
    last = 0
    last_sem_move = time.time()

    def watch():
        nonlocal last, last_sem_move   # иначе первый тик: referenced before assignment
        while not stop.is_set():
            elapsed = time.time() - t0
            sem = _sem_tokens(counters)
            tps = (sem - last) / 2 if elapsed > 2 and sem >= last else None
            if sem != last:
                last_sem_move = time.time()
            last = sem
            stage = counters["phase"]
            # семантика замерла, но пайплайн жив — это synthesize/decode:
            # на длинном треке минуты тишины, UI висел «на 99%» без объяснения
            if stage == "semantic" and sem > 0 and time.time() - last_sem_move > 15:
                stage = "decode"
            # честный процент есть только у семантики (самая длинная фаза):
            # токены / бюджет; загрузка модели и план — неопределённая длительность
            pct = None
            if stage == "semantic":
                pct = min(99, sem * 100 // max(1, budget or MAX_SEM_TOKENS))
            with _state_lock:
                _progress[job_id] = {
                    "stage": stage,
                    "tokens": counters["tokens"],
                    "tok_per_s": round(tps, 1) if tps else None,
                    "elapsed_s": round(elapsed, 1),
                    "progress_pct": pct,
                }
            stop.wait(2)

    t = threading.Thread(target=watch, daemon=True, name=f"watch-{job_id}")
    t.start()
    return stop


# шагов модели (семантических токенов) на секунду звука: у #174 6904 шага на
# 274.9 с → 25.1. Гипотеза «ровно 25 Гц» не сверена с кодеком — эксперимент.
SEM_TOK_PER_SEC = 25


def _cont_steps(cont_from, n_tokens: int) -> int:
    """Сколько шагов модели родителя подать как сыгранные: отметка × 25/с,
    не меньше 1 и не больше, чем есть."""
    return max(1, min(n_tokens, round(float(cont_from or 0) * SEM_TOK_PER_SEC)))


def _continue_song(pipe, row, request):
    """«Продолжение с места»: шаги модели родителя до cont_from подаются как
    уже сыгранные, дальше модель продолжает сама (другой сид; при req_abc —
    изменённый план; стиль — строки трека, с припиской). До отметки — то же
    исполнение, склейки нет (проверено: #195, #196)."""
    import dataclasses

    import numpy as np
    from yue2.pipeline import SemanticResult, SongResult, SymbolicPlan
    from yue2.protocol import CODEC_OFFSET, CONTEXT, Sampling, negative_prefix, token_prefixes

    src = JOBS_DIR / str(row["parent_id"])
    saved = SymbolicPlan.load(src)
    tokens = [int(t) for t in np.load(src / "semantic.npy")]
    k = _cont_steps(row["cont_from"], len(tokens))
    # стиль — из строки нового трека (стиль родителя + приписка «что изменить в
    # звучании»); раньше брался из плана родителя, и приписка до модели не
    # доходила (request.json #200 — без неё)
    req = dataclasses.replace(saved.request, style=request.get("style") or saved.request.style,
                              seed=int(request.get("seed") or saved.request.seed),
                              cfg_scale=request.get("cfg_scale", saved.request.cfg_scale))
    if request.get("abc"):
        plan = pipe.plan(request=dataclasses.replace(req, abc=request["abc"]))
    else:
        plan = SymbolicPlan(req, saved.abc, saved.abc_ids,
                            token_prefixes(req, pipe.tokenizer, saved.abc_ids), saved.timing, saved.truncated)
    forced = [t + CODEC_OFFSET for t in tokens[:k]]
    prefix = plan.prefix + forced
    negative = None
    if plan.request.guidance != 1:
        negative = negative_prefix(plan.request, pipe.tokenizer, plan.abc_ids) + forced
    budget = request["semantic_sampling"].max_tokens if request.get("semantic_sampling") else MAX_SEM_TOKENS
    room = CONTEXT - max(len(prefix), len(negative or []))
    temperature = request["semantic_sampling"].temperature if request.get("semantic_sampling") else 1.0
    sampling = Sampling(max_tokens=max(200, min(budget - k, room)), temperature=temperature)
    ids, timing, truncated = pipe._generate(
        prefix, sampling, plan.request.seed, "semantic", negative=negative,
        cfg_scale=plan.request.guidance, legacy_off=plan.request.cot == "off",
        cancelled=request.get("cancelled"), on_token=request.get("on_token"))
    semantic = SemanticResult(plan, tokens[:k] + [int(t) - CODEC_OFFSET for t in ids], timing, truncated)
    latents = pipe.synthesize(semantic, cancelled=request.get("cancelled"))
    audio = pipe.decode(latents)
    config = pipe.effective_config(plan.request, None, sampling)
    return SongResult(audio, 48000, semantic, latents, config, pipe.weights,
                      {"semantic": timing}, f"continue-{row['parent_id']}-{k}")


def _is_cuda_oom(e: BaseException) -> bool:
    low = str(e).lower()
    return "out of memory" in low and "cuda" in low


def _unload_pipe() -> None:
    """Выгрузить модель и вернуть видеопамять (вызывать под _pipe_lock).
    После нехватки памяти упавшая генерация оставляла в модели ~1,4 ГБ, и
    следующие джобы падали даже на свободном GPU; модель загрузится заново
    при следующей джобе (~12 с)."""
    global _pipe
    if _pipe is None:
        return
    try:
        _pipe.__exit__(None, None, None)
    except Exception:  # noqa: BLE001 - выгрузка всё равно продолжается
        log.exception("pipeline close failed")
    _pipe = None
    import gc
    gc.collect()
    try:
        import torch
        torch.cuda.empty_cache()
    except Exception:  # noqa: BLE001 - без torch/CUDA освобождать нечего
        pass


def friendly_error(e: BaseException) -> str:
    """Текст ошибки для человека. Нехватка видеопамяти (частое на общем GPU:
    рядом Ollama, Stable Diffusion) — вместо стека CUDA «GPU занят» со
    свободной/общей памятью и подсказкой, что делать; остальное — как есть."""
    s = str(e)
    if not _is_cuda_oom(e):
        return s
    mem = ""
    try:
        import torch
        free, total = torch.cuda.mem_get_info()
        mem = f" (свободно {free / 2**30:.1f} из {total / 2**30:.1f} ГБ)"
    except Exception:  # noqa: BLE001 - без torch/CUDA — сообщение без цифр
        pass
    return (f"GPU занят: не хватило видеопамяти{mem}. Освободите память от других программ "
            "(модель в Ollama, Stable Diffusion и т. п.) и нажмите «повторить».")


def _run_job(job_id: int):
    with db_lock, db() as conn:
        row = conn.execute("SELECT * FROM jobs WHERE id=?", (job_id,)).fetchone()
    job_dir = JOBS_DIR / str(job_id)
    job_dir.mkdir(parents=True, exist_ok=True)
    cancel_ev = threading.Event()
    with _state_lock:
        _cancel_flags[job_id] = cancel_ev
    on_token, counters = _on_token_cb(job_id)
    budget = 450 if row["draft"] else MAX_SEM_TOKENS   # ~18 с превью
    if not row["draft"] and row["max_tokens"]:
        budget = max(500, min(int(row["max_tokens"]), 30000))
    if row["role"] == "voice":
        _run_voice_job(job_id, row, job_dir)
        return
    watch_stop = _progress_watcher(job_id, counters, time.time(), budget)
    try:  # noqa: SIM105 - очистка состояния после любого исхода
        with _pipe_lock:
            pipe = _get_pipe()
            counters["phase"] = "plan"
            request = {"style": row["style"], "lyrics": row["lyrics"], "cot": row["cot"],
                       "on_token": on_token,
                       "cancelled": lambda: cancel_ev.is_set()}
            cfg = row["cfg"] or CFG_SCALE
            if cfg > 0:
                request["cfg_scale"] = cfg
            # бюджет длины и смелость игры — параметры Sampling, не запроса
            try:
                from yue2.protocol import Sampling
                request["semantic_sampling"] = Sampling(max_tokens=budget,
                                                        temperature=row["temperature"] or 1.0)
            except ImportError:
                pass
            if row["seed"]:
                request["seed"] = row["seed"]
            if row["req_abc"]:
                abc_path = job_dir / row["req_abc"]
                if abc_path.is_file():
                    request["abc"] = abc_path.read_text()
            elif row["arc"]:
                # драматургия: строим план сами, правим темп/вокал по дуге —
                # и рендерим по своему ABC (механизм req_abc)
                counters["phase"] = "arc-plan"
                plan_res = pipe.plan(style=request["style"], lyrics=request["lyrics"],
                                     cot=row["cot"],
                                     **({"seed": row["seed"]} if row["seed"] else {}))
                request["style"] = arc.style_with_arc(request["style"], row["arc"])
                request["abc"] = arc.apply_arc(plan_res.abc, row["arc"])
                (job_dir / "request.abc").write_text(request["abc"], encoding="utf-8")
                log.info("job %s: arc=%s applied to plan", job_id, row["arc"])
            t0 = time.time()
            failure = ""
            try:
                try:
                    song = (_continue_song(pipe, row, request) if row["role"] == "continue"
                            else pipe(**request))
                    # через counters: сторож прогресса раз в 2 с пишет стадию оттуда
                    # и затёр бы прямую запись в _progress
                    counters["phase"] = "finalize"
                    with _state_lock:
                        _progress.setdefault(job_id, {})["stage"] = "finalize"
                except TypeError as te:
                    # старые сборки yue2 могут не знать cfg_scale — без него
                    if "cfg_scale" in str(te):
                        log.warning("yue2 pipeline не поддерживает cfg_scale (%s); дефолт библиотеки", te)
                        request.pop("cfg_scale", None)
                        song = pipe(**request)
                    else:
                        raise
                song.save_artifacts(job_dir)
            except (InterruptedError, KeyboardInterrupt):
                log.info("job %s canceled by user", job_id)
                with db_lock, db() as conn:
                    conn.execute("UPDATE jobs SET status='canceled', finished_at=? WHERE id=?",
                                 (time.strftime("%Y-%m-%dT%H:%M:%S"), job_id))
                return
            except Exception as e:  # noqa: BLE001
                log.exception("job %s failed", job_id)
                _mark_failed(job_id, e)
                failure = "oom" if _is_cuda_oom(e) else "error"
            if failure:
                # выгрузка — после except: там стек ошибки ещё держит тензоры генерации
                if failure == "oom":
                    _unload_pipe()
                return
            truncated = song.truncated
            if isinstance(truncated, dict):
                truncated = any(truncated.values())
            audio = job_dir / "audio.flac"
            mp3, wav = _make_formats(job_dir)
            if row["overdub_of"]:
                _mix_overdub(int(row["overdub_of"]), job_id,
                             float(row["overdub_gain"] or 0.5))
            with db_lock, db() as conn:
                conn.execute(
                    "UPDATE jobs SET status='done', duration_sec=?, audio_file=?, mp3_file=?, wav_file=?, abc_file=?, finished_at=? WHERE id=?",  # noqa: E501
                    (_audio_duration(audio),
                     "audio.flac" if audio.exists() else "",
                     mp3,
                     wav,
                     "score.abc" if (job_dir / "score.abc").exists() else "",
                     time.strftime("%Y-%m-%dT%H:%M:%S"), job_id))
        log.info("job %s done in %.1fs (truncated=%s)", job_id, time.time() - t0, truncated)
        if row["role"] != "voice" and is_instrumental(row["style"]) and auto_stems_instrumental():
            try:
                _ensure_stems(job_id)   # дорожки + проверка голоса
            except Exception:  # noqa: BLE001 - трек готов; метка — подсказка
                log.exception("auto stems failed for job %s", job_id)
    finally:
        watch_stop.set()
        with _state_lock:
            _cancel_flags.pop(job_id, None)
            _progress.pop(job_id, None)


def _mark_failed(job_id: int, e: BaseException) -> None:
    """Джоба упала: статус error и понятная пользователю причина."""
    with db_lock, db() as conn:
        conn.execute("UPDATE jobs SET status='error', error=?, finished_at=? WHERE id=?",
                     (friendly_error(e), time.strftime("%Y-%m-%dT%H:%M:%S"), job_id))


def _separate(job_id: int, audio: Path, jdir: Path) -> dict:
    """Дорожки demucs + проверка голоса в треке «без голоса» (vocal_leak)."""
    result = demucs_separate(audio, jdir)
    check_vocal_leak(job_id)
    return result


def _ensure_stems(job_id: int) -> Path:
    """Дорожки demucs трека (делаются, если их ещё нет) → каталог трека."""
    row = _job_row(job_id)
    jdir = JOBS_DIR / str(job_id)
    if not (jdir / "stem-vocals.flac").is_file():
        if row is None or not row["audio_file"]:
            raise RuntimeError(f"у трека #{job_id} нет звука")
        _separate(job_id, jdir / row["audio_file"], jdir)
    return jdir


def _run_voice_job(job_id: int, row, job_dir: Path):
    """«Голос альбома»: голос родителя — тембром образца (Seed-VC), музыка та же."""
    t0 = time.time()
    try:
        p = json.loads((job_dir / "voice.json").read_text())
        src = _ensure_stems(int(row["parent_id"]))
        ref = _ensure_stems(int(p["ref_job_id"]))
        parent = _job_row(int(row["parent_id"]))
        audio = voicevc.run(src, parent["audio_file"], ref, job_dir,
                            None if p.get("ref_from") is None else float(p["ref_from"]),
                            float(p["ref_dur"]), int(p["steps"]))
        # ноты те же — ролл и превью работают по плану родителя
        for f in ("score.abc", "score.json"):
            if (src / f).is_file():
                shutil.copy2(src / f, job_dir / f)
        mp3, wav = _make_formats(job_dir)
        with db_lock, db() as conn:
            conn.execute(
                "UPDATE jobs SET status='done', duration_sec=?, audio_file=?, mp3_file=?, wav_file=?, abc_file=?, finished_at=? WHERE id=?",  # noqa: E501
                (_audio_duration(audio), audio.name, mp3, wav,
                 "score.abc" if (job_dir / "score.abc").exists() else "",
                 time.strftime("%Y-%m-%dT%H:%M:%S"), job_id))
        log.info("voice job %s done in %.1fs", job_id, time.time() - t0)
    except Exception as e:  # noqa: BLE001
        log.exception("voice job %s failed", job_id)
        _mark_failed(job_id, e)


class VoiceIn(BaseModel):
    ref_job_id: int                                  # трек-образец: чей голос
    # окно образца в его дорожке голоса, с; не задано — самый плотный кусок пения
    ref_from: float | None = Field(default=None, ge=0)
    ref_dur: float = Field(default=25, ge=voicevc.REF_MIN_SEC, le=voicevc.REF_MAX_SEC)
    steps: int = Field(default=50, ge=voicevc.STEPS_MIN, le=voicevc.STEPS_MAX)
    title: str = ""


@app.post("/jobs/{job_id}/voice")
def voice_job(job_id: int, req: VoiceIn):
    """«Голос альбома» (ЭКСПЕРИМЕНТ: голос узнаётся, но дрожит — свойство
    Seed-VC): новая версия трека — голос спет тембром образца (отдельная
    установка), музыка и мелодия прежние. Джоба идёт в общую очередь."""
    if not voicevc.available():
        raise HTTPException(503, f"Seed-VC не установлен ({voicevc.SEEDVC_DIR}): worker/seedvc_install.sh")
    with db_lock, db() as conn:
        rows = {r["id"]: r for r in conn.execute(
            "SELECT * FROM jobs WHERE id IN (?, ?)", (job_id, req.ref_job_id))}
        for jid in (job_id, req.ref_job_id):
            r = rows.get(jid)
            if r is None or r["status"] != "done" or not r["audio_file"]:
                raise HTTPException(404, f"job {jid} not found or not done")
        src = rows[job_id]
        cur = conn.execute(
            "INSERT INTO jobs(title,status,style,lyrics,seed,cot,parent_id,role,created_at)"
            " VALUES(?,?,?,?,?,?,?,?,?)",
            (req.title or f"{src['title']} · голос #{req.ref_job_id}", "queued", src["style"],
             src["lyrics"], src["seed"], src["cot"], job_id, "voice", time.strftime("%Y-%m-%dT%H:%M:%S")))
        new_id = cur.lastrowid
        # параметры — под тем же замком: очередь забирает джобы только через него
        # и не увидит «голос» без voice.json
        jdir = JOBS_DIR / str(new_id)
        jdir.mkdir(parents=True, exist_ok=True)
        (jdir / "voice.json").write_text(json.dumps(
            {"ref_job_id": req.ref_job_id, "ref_from": req.ref_from, "ref_dur": req.ref_dur, "steps": req.steps}))
    return {"id": new_id}


def queue_loop():
    while True:
        with db_lock, db() as conn:
            row = conn.execute(
                "SELECT id FROM jobs WHERE status='queued' ORDER BY id LIMIT 1").fetchone()
        if row is None:
            time.sleep(2)
            continue
        with db_lock, db() as conn:
            cur = conn.execute(
                "UPDATE jobs SET status='running' WHERE id=? AND status='queued'", (row["id"],))
            claimed = cur.rowcount > 0
        if claimed:
            _run_job(row["id"])


@app.on_event("startup")
def _startup():
    # джобы, «повисшие» в running с прошлого запуска: генератор мёртв — честная ошибка
    with db_lock, db() as conn:
        conn.execute(
            "UPDATE jobs SET status='error', error='воркер перезапущен во время генерации', finished_at=? "
            "WHERE status='running'",
            (time.strftime("%Y-%m-%dT%H:%M:%S"),))
    threading.Thread(target=queue_loop, daemon=True).start()


@app.on_event("shutdown")
def _shutdown():
    with _pipe_lock:
        _unload_pipe()


# роли производных треков: section — рендер куска для вклейки, rebuild —
# пересборка с приёмами, fragment — «проверить кусок», variant — вариант
# эффекта, ставший треком. continue сюда не входит: продолжение создаёт только
# POST /jobs/{id}/continue (проверки semantic.npy и отметки)
JOB_ROLE_PATTERN = "^(|section|rebuild|fragment|variant)$"


class JobIn(BaseModel):
    title: str = ""
    style: str
    lyrics: str
    seed: int | None = None
    # жёсткий потолок семантических токенов (~25 т/с): селектор длительности
    # формы; 0 = бюджет воркера. Модель может закончить раньше, но не позже
    max_tokens: int = 0
    cot: str = Field(default="full", pattern="^(full|melody|off)$")
    abc: str | None = None
    # черновик ~15-20 с вместо полного трека: быстро послушать, стоит ли рендерить целиком
    draft: bool = False
    # драматургия: "" | build (нарастание) | wave (волна) | burst (взрыв)
    arc: str = Field(default="", pattern="^(|build|wave|burst)$")
    # производный трек: от какого трека и зачем (см. _migrate)
    parent_id: int | None = None
    role: str = Field(default="", pattern=JOB_ROLE_PATTERN)
    # характер исполнения: 0 — по умолчанию; у производного трека 0 — как у родителя
    temperature: float = 0
    cfg: float = 0

    @field_validator("temperature")
    @classmethod
    def _temperature_range(cls, v: float) -> float:
        if v != 0 and not TEMPERATURE_RANGE[0] <= v <= TEMPERATURE_RANGE[1]:
            raise ValueError(f"temperature: 0 or {TEMPERATURE_RANGE[0]}..{TEMPERATURE_RANGE[1]}")
        return v

    @field_validator("cfg")
    @classmethod
    def _cfg_range(cls, v: float) -> float:
        if v != 0 and not CFG_RANGE[0] <= v <= CFG_RANGE[1]:
            raise ValueError(f"cfg: 0 or {CFG_RANGE[0]}..{CFG_RANGE[1]}")
        return v


class PlanIn(BaseModel):
    style: str
    lyrics: str
    seed: int | None = None
    cot: str = Field(default="full", pattern="^(full|melody|off)$")


class CopilotIn(BaseModel):
    theme: str
    style: str = ""
    example: str = ""
    lang: str = "Russian"
    instruction: str = ""  # свой системный промпт (заготовка из жанра/голоса формы)


class TranslateIn(BaseModel):
    text: str
    to: str = "English"


OLLAMA_URL = os.environ.get("YUE_OLLAMA_URL", "http://127.0.0.1:11434/api/chat")
OLLAMA_MODEL = os.environ.get("YUE_OLLAMA_MODEL", "qwen2.5-chat-ru:latest")


class ConfigIn(BaseModel):
    ollama_url: str | None = None
    ollama_model: str | None = None
    auto_stems_instrumental: bool | None = None


# настройки воркера, переживающие перезапуск (в отличие от Ollama — те из env)
SETTINGS_PATH = DATA_DIR / "settings.json"


def _load_settings() -> dict:
    try:
        return json.loads(SETTINGS_PATH.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}


def _save_settings(st: dict) -> None:
    SETTINGS_PATH.parent.mkdir(parents=True, exist_ok=True)
    SETTINGS_PATH.write_text(json.dumps(st, ensure_ascii=False, indent=1), encoding="utf-8")


def auto_stems_instrumental() -> bool:
    """У треков «без голоса» сразу делать дорожки и проверять голос (по умолчанию да)."""
    return bool(_load_settings().get("auto_stems_instrumental", True))


def is_instrumental(style: str) -> bool:
    """Трек заказан без голоса: в стиле «instrumental» или «no vocals»."""
    low = (style or "").lower()
    return "instrumental" in low or "no vocals" in low


def check_vocal_leak(job_id: int) -> None:
    """У трека «без голоса» с готовыми дорожками — где звучит дорожка голоса
    (vocal_leak, секунды через запятую). Другие треки и треки без дорожек —
    не трогаем; сбой проверки не должен ронять разделение на дорожки."""
    row = _job_row(job_id)
    stem = JOBS_DIR / str(job_id) / "stem-vocals.flac"
    if row is None or not is_instrumental(row["style"]) or not stem.is_file():
        return
    try:
        import soundfile as sf
        x, sr = sf.read(str(stem), dtype="float32", always_2d=True)
        leak = ",".join(str(t) for t in vocal_activity(x, sr))
    except Exception:  # noqa: BLE001 - метка — подсказка, не повод ронять дорожки
        log.exception("vocal check failed for job %s", job_id)
        return
    with db_lock, db() as conn:
        conn.execute("UPDATE jobs SET vocal_leak=? WHERE id=?", (leak, job_id))


@app.get("/config")
def get_config():
    """Текущие настройки: Ollama (копайтер/перевод), пути. Меняется через POST."""
    return {
        "ollama_url": OLLAMA_URL,
        "ollama_model": OLLAMA_MODEL,
        "data_dir": str(DATA_DIR),
        "whisper_py": str(WHISPER_PY),
        "whisper_available": WHISPER_PY.is_file(),
        "seedvc_dir": str(voicevc.SEEDVC_DIR),
        "seedvc_available": voicevc.available(),
        "auto_stems_instrumental": auto_stems_instrumental(),
    }


@app.post("/config")
def set_config(req: ConfigIn):
    """Смена Ollama URL/модели на лету (настройки из GUI). До перезапуска:
    env-переменные при старте имеют приоритет, это переопределяет на сессию."""
    global OLLAMA_URL, OLLAMA_MODEL
    if req.ollama_url is not None:
        if not req.ollama_url.startswith(("http://", "https://")):
            raise HTTPException(422, "ollama_url must be an http(s) URL")
        OLLAMA_URL = req.ollama_url.rstrip("/")
    if req.ollama_model is not None:
        if not req.ollama_model.strip():
            raise HTTPException(422, "ollama_model must not be empty")
        OLLAMA_MODEL = req.ollama_model.strip()
    if req.auto_stems_instrumental is not None:
        st = _load_settings()
        st["auto_stems_instrumental"] = bool(req.auto_stems_instrumental)
        _save_settings(st)
    log.info("config updated: ollama=%s model=%s", OLLAMA_URL, OLLAMA_MODEL)
    return get_config()


@app.get("/ollama_models")
def ollama_models(url: str | None = None):
    """Список установленных моделей Ollama (для выпадающего списка в настройках).
    ?url= — проверить кандидат до сохранения; без параметра — текущий OLLAMA_URL."""
    chat_url = url or OLLAMA_URL
    base = chat_url.split("/api/")[0] if "/api/" in chat_url else chat_url.rstrip("/")
    try:
        r = urllib.request.urlopen(f"{base}/api/tags", timeout=10)
        tags = json.load(r).get("models", [])
    except Exception as e:  # noqa: BLE001
        raise HTTPException(502, f"ollama unreachable: {e}") from e
    return {"base_url": base, "models": sorted(str(m.get("name", "")) for m in tags if m.get("name"))}


@app.post("/copilot")
def copilot(req: CopilotIn):
    """Копайтер стихов через Ollama. keep_alive=0: модель выгружается из VRAM
    сразу после ответа — 9 ГБ рядом с YuE (7.7 ГБ) иначе роняют генерацию OOM."""
    if not req.theme.strip():
        raise HTTPException(422, "theme is required")
    system = req.instruction.strip() or (
        "Ты — поэт-песенник. Пишешь тексты песен с секционной разметкой "
        "[Verse], [Chorus] (можно [Bridge], [Outro]). В ответе — только текст "
        "песни: без пояснений, без markdown-разметки, без названия. Строки "
        "короткие и поёмые, с рифмой. Два-три куплета и припев."
    )
    user = f"Тема: {req.theme.strip()}\nЯзык текста: {req.lang}"
    if req.style.strip():
        user += f"\nСтиль музыки (для манеры и образов): {req.style.strip()}"
    if req.example.strip():
        user += ("\nВот пример текста в похожей манере — подражай манере, "
                 f"но не повторяй содержание:\n{req.example.strip()}")
    user += "\nНапиши текст песни."
    t0 = time.time()
    try:
        text = llm.strip_md(llm.ollama_chat(OLLAMA_URL, OLLAMA_MODEL, system, user,
                                            temperature=0.9, timeout=280))
    except Exception as e:  # noqa: BLE001
        raise HTTPException(502, f"ollama failed: {e}") from e
    if not text:
        raise HTTPException(502, "ollama returned empty text")
    log.info("copilot: %d chars in %.1fs", len(text), time.time() - t0)
    return {"text": text, "seconds": round(time.time() - t0, 1)}


@app.get("/health")
def health():
    return {"status": "ok", "model_loaded": _pipe is not None, "load_error": _load_error}


@app.post("/translate")
def translate(req: TranslateIn):
    """Перевод строки стиля (рус → анг) через Ollama — слоты можно писать по-русски.
    keep_alive=0: модель выгружается из VRAM сразу (см. /copilot)."""
    if not req.text.strip():
        raise HTTPException(422, "text is required")
    system = (f"Translate the user's music style description to {req.to}. "
              "Keep it a single comma-separated tag line. Output ONLY the translation.")
    t0 = time.time()
    try:
        out = llm.strip_md(llm.ollama_chat(OLLAMA_URL, OLLAMA_MODEL, system,
                                           req.text.strip(), temperature=0.2))
    except Exception as e:  # noqa: BLE001
        raise HTTPException(502, f"ollama failed: {e}") from e
    if not out:
        raise HTTPException(502, "ollama returned empty translation")
    return {"text": out, "seconds": round(time.time() - t0, 1)}


@app.post("/jobs")
def submit(req: JobIn):
    if req.abc is not None and req.cot == "off":
        raise HTTPException(422, "abc requires cot=full or cot=melody")
    if req.arc and req.abc is not None and req.abc.strip():
        raise HTTPException(422, "arc is applied to the generated plan; explicit abc wins")
    if req.arc and req.cot == "off":
        raise HTTPException(422, "arc requires cot=full or cot=melody (needs the plan)")
    with db_lock, db() as conn:
        temperature, cfg, seed = req.temperature, req.cfg, req.seed
        par = None
        if req.parent_id is not None:
            par = conn.execute("SELECT seed, temperature, cfg FROM jobs WHERE id=?", (req.parent_id,)).fetchone()
        if par is not None:
            # кусок для вклейки — в характере родителя, иначе шов слышен
            temperature = temperature or par["temperature"] or 0
            cfg = cfg or par["cfg"] or 0
            # без сида — сид родителя (тот же голос); старый родитель без
            # сида генерировался с сидом библиотеки
            seed = seed or par["seed"] or LIB_DEFAULT_SEED
        if not seed:
            # пусто — случайный, и он остаётся в треке: видно и повторяемо
            # (раньше пустое поле давало всегда сид библиотеки — один и тот же трек)
            seed = random.randrange(1, 2**31)
        cur = conn.execute(
            "INSERT INTO jobs(title,status,style,lyrics,seed,cot,draft,"
            "arc,max_tokens,parent_id,role,temperature,cfg,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
            (req.title or f"Untitled {time.strftime('%H:%M')}", "queued", req.style,
             req.lyrics, seed, req.cot, int(req.draft), req.arc,
             int(req.max_tokens or 0), req.parent_id, req.role, temperature, cfg,
             time.strftime("%Y-%m-%dT%H:%M:%S")))
        job_id = cur.lastrowid
        if req.abc is not None and req.abc.strip():
            (JOBS_DIR / str(job_id)).mkdir(parents=True, exist_ok=True)
            (JOBS_DIR / str(job_id) / "request.abc").write_text(req.abc, encoding="utf-8")
            conn.execute("UPDATE jobs SET req_abc='request.abc' WHERE id=?", (job_id,))
    return {"id": job_id}


class ContinueIn(BaseModel):
    from_sec: float = Field(gt=0)        # с какой секунды родителя играть заново
    seed: int | None = None              # None — случайный: другой вариант продолжения
    abc: str | None = None               # изменённый план (приёмы) — необязательно
    title: str = ""
    # что изменить в звучании с этого места: приписка к стилю родителя
    # («electric guitar enters and builds») — инструменты в плане не записать
    style_add: str = ""


@app.post("/jobs/{job_id}/continue")
def continue_job(job_id: int, req: ContinueIn):
    """ЭКСПЕРИМЕНТ: новый трек = родитель до from_sec + продолжение моделью."""
    import random
    with db_lock, db() as conn:
        row = conn.execute("SELECT * FROM jobs WHERE id=?", (job_id,)).fetchone()
        if row is None or row["status"] != "done":
            raise HTTPException(404, "job not found or not done")
        if not (JOBS_DIR / str(job_id) / "semantic.npy").is_file():
            raise HTTPException(422, "no semantic.npy — job cannot be continued: an imported track "
                                "(or an old one) has no model tokens; "
                                "regenerate it with the same style, lyrics and seed")
        # отметка за концом: модель переиграла бы весь трек без нового куска
        if row["duration_sec"] and req.from_sec >= row["duration_sec"]:
            raise HTTPException(422, "from_sec must be inside the track")
        seed = req.seed if req.seed is not None else random.randrange(1, 2**31)
        cur = conn.execute(
            "INSERT INTO jobs(title,status,style,lyrics,seed,cot,parent_id,role,cont_from,"
            "temperature,cfg,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)",
            (req.title or f"{row['title']} · с {req.from_sec:.0f} с", "queued",
             row["style"].strip().rstrip(",") + (", " + req.style_add.strip() if req.style_add.strip() else ""),
             row["lyrics"], seed, row["cot"], job_id, "continue", req.from_sec,
             row["temperature"] or 0, row["cfg"] or 0,
             time.strftime("%Y-%m-%dT%H:%M:%S")))
        new_id = cur.lastrowid
        if req.abc is not None and req.abc.strip():
            (JOBS_DIR / str(new_id)).mkdir(parents=True, exist_ok=True)
            (JOBS_DIR / str(new_id) / "request.abc").write_text(req.abc, encoding="utf-8")
            conn.execute("UPDATE jobs SET req_abc='request.abc' WHERE id=?", (new_id,))
    return {"id": new_id}


class HeadIn(BaseModel):
    head_id: int | None = None   # None/0 — основной снова сам трек


@app.post("/jobs/{job_id}/head")
def set_head(job_id: int, req: HeadIn):
    """Основная версия песни: job_id — корень, head_id — он сам или его потомок."""
    with db_lock, db() as conn:
        rows = {r["id"]: r["parent_id"] for r in conn.execute("SELECT id, parent_id FROM jobs")}
        if job_id not in rows:
            raise HTTPException(404, "job not found")
        head = req.head_id or None
        if head is not None:
            cur, seen = head, set()
            while cur is not None and cur != job_id and cur not in seen:
                seen.add(cur)
                cur = rows.get(cur)
            if cur != job_id:
                raise HTTPException(422, "head must be the job itself or its descendant")
        conn.execute("UPDATE jobs SET head_id=? WHERE id=?", (head, job_id))
    return {"id": job_id, "head_id": head}


JOB_TITLE_MAX = 200
JOB_FOLDER_MAX = 60


class JobPatchIn(BaseModel):
    title: str | None = None    # None — не менять; пустое после strip — ошибка
    folder: str | None = None   # None — не менять; "" — убрать из папки


@app.patch("/jobs/{job_id}")
def patch_job(job_id: int, req: JobPatchIn):
    """Подпись трека и папка: человек ориентируется по названию, а не по номеру
    (номер при переносе на другую машину меняется)."""
    sets, vals = [], []
    if req.title is not None:
        title = req.title.strip()
        if not title:
            raise HTTPException(422, "title must not be empty")
        if len(title) > JOB_TITLE_MAX:
            raise HTTPException(422, f"title longer than {JOB_TITLE_MAX}")
        sets.append("title=?")
        vals.append(title)
    if req.folder is not None:
        folder = req.folder.strip()
        if len(folder) > JOB_FOLDER_MAX:
            raise HTTPException(422, f"folder longer than {JOB_FOLDER_MAX}")
        sets.append("folder=?")
        vals.append(folder)
    with db_lock, db() as conn:
        if conn.execute("SELECT 1 FROM jobs WHERE id=?", (job_id,)).fetchone() is None:
            raise HTTPException(404, "job not found")
        if sets:
            conn.execute(f"UPDATE jobs SET {', '.join(sets)} WHERE id=?", (*vals, job_id))
        row = conn.execute("SELECT * FROM jobs WHERE id=?", (job_id,)).fetchone()
    return _job_dict(row)


@app.post("/plan")
def plan(req: PlanIn):
    """Только стадия плана: возвращает ABC до рендера (GPU, но без синтеза звука).

    Синхронный: ждёт освобождения GPU, если идёт рендер. При холодном старте
    включает загрузку модели, первый вызов может занять пару минут.
    """
    if req.cot == "off":
        raise HTTPException(422, "plan requires cot=full or cot=melody (off does not produce ABC)")
    # пусто — случайный (раньше — всегда сид библиотеки); сид в ответе:
    # трек по этому плану с тем же сидом споёт так же
    seed = req.seed or random.randrange(1, 2**31)
    with _pipe_lock:
        pipe = _get_pipe()
        t0 = time.time()
        result = pipe.plan(style=req.style, lyrics=req.lyrics, cot=req.cot, seed=seed)
    truncated = result.truncated
    if isinstance(truncated, dict):
        truncated = any(truncated.values())
    return {"abc": result.abc, "truncated": bool(truncated), "seconds": round(time.time() - t0, 1),
            "seed": seed}


def _job_dict(row) -> dict:
    d = {k: row[k] for k in row.keys()}
    d["draft"] = bool(d.get("draft"))  # sqlite даёт 0/1, клиент ждёт bool
    # готовые миксы трека (вклейки, эффекты на дорожки): по ним список треков
    # показывает «версии» и у трека без дочерних треков
    d["mixes"] = sum(1 for _ in (JOBS_DIR / str(row["id"])).glob("overdub-inst-*.flac"))
    if d.get("status") == "running":
        with _state_lock:
            d.update(_progress.get(row["id"], {}))
    return d


JOBS_LIST_LIMIT = 2000


@app.get("/jobs")
def list_jobs():
    with db_lock, db() as conn:
        # вся библиотека (с запасом): при LIMIT 100 старые корни пропадали из
        # списка — версии песни теряли родителя, а «перепеть» — свои дубли
        rows = conn.execute("SELECT * FROM jobs ORDER BY id DESC LIMIT ?", (JOBS_LIST_LIMIT,)).fetchall()
    return [_job_dict(r) for r in rows]


@app.get("/jobs/{job_id}")
def get_job(job_id: int):
    with db_lock, db() as conn:
        row = conn.execute("SELECT * FROM jobs WHERE id=?", (job_id,)).fetchone()
    if row is None:
        raise HTTPException(404, "job not found")
    return _job_dict(row)


@app.post("/jobs/{job_id}/cancel")
def cancel(job_id: int):
    """Отмена: queued — снять из очереди; running — остановить генерацию
    (пайплайн проверяет флаг по колбэку, завершится штатно на ближайшем шаге)."""
    with db_lock, db() as conn:
        cur = conn.execute(
            "UPDATE jobs SET status='canceled', finished_at=? WHERE id=? AND status='queued'",
            (time.strftime("%Y-%m-%dT%H:%M:%S"), job_id))
    if cur.rowcount > 0:
        return {"canceled": True}
    with db_lock, db() as conn:
        row = conn.execute("SELECT status FROM jobs WHERE id=?", (job_id,)).fetchone()
    if row and row["status"] == "running":
        with _state_lock:
            ev = _cancel_flags.get(job_id)
        if ev:
            ev.set()
            return {"canceled": True, "running": True}
    return {"canceled": False}


@app.post("/jobs/{job_id}/retry")
def retry_job(job_id: int):
    """«Повторить»: упавшая или отменённая джоба снова в очередь с теми же
    параметрами — стиль, сид, план, роль лежат в её строке и папке, очередь
    подберёт её как новую. Готовую или идущую — нельзя (409)."""
    with db_lock, db() as conn:
        row = conn.execute("SELECT status FROM jobs WHERE id=?", (job_id,)).fetchone()
        if row is None:
            raise HTTPException(404, "job not found")
        if row["status"] not in ("error", "canceled"):
            raise HTTPException(409, f"job is {row['status']}: retry only failed or canceled jobs")
        conn.execute("UPDATE jobs SET status='queued', error='', finished_at='' WHERE id=?", (job_id,))
    log.info("job %s retried", job_id)
    return {"retried": True}


@app.delete("/jobs/{job_id}")
def delete_job(job_id: int):
    """Удаление результата: строка в БД + папка с артефактами. Идущий рендер не трогаем."""
    with db_lock, db() as conn:
        row = conn.execute("SELECT status FROM jobs WHERE id=?", (job_id,)).fetchone()
        if row is None:
            raise HTTPException(404, "job not found")
        if row["status"] == "running":
            raise HTTPException(409, "job is running, cancel first")
        conn.execute("DELETE FROM jobs WHERE id=?", (job_id,))
    import shutil
    shutil.rmtree(JOBS_DIR / str(job_id), ignore_errors=True)
    log.info("job %s deleted", job_id)
    return {"deleted": True}


MIN_GRID_SEC = 4  # короче — по атакам темп не уточнить


class GridIn(BaseModel):
    from_sec: float = Field(default=0, ge=0)
    to_sec: float = Field(default=0, ge=0)   # 0 — до конца трека


@app.post("/jobs/{job_id}/grid")
def job_grid(job_id: int, body: GridIn | None = None):
    """Сетка долей для эффектов в такт («Ритм-гейт»): темп и время сильной доли.
    Источник — дорожка барабанов (есть после demucs), иначе микс. Окно
    from_sec–to_sec (0 — до конца) — лучше то место, где эффект будет: доля
    берётся первая в окне, и погрешность темпа не накапливается по треку."""
    body = body or GridIn()
    if 0 < body.to_sec <= body.from_sec:
        raise HTTPException(422, "to_sec must be after from_sec (0 — to the end)")
    row = _job_row(job_id)
    if row is None or not row["audio_file"]:
        raise HTTPException(404, "job or audio not found")
    jdir = JOBS_DIR / str(job_id)
    drums = jdir / "stem-drums.flac"
    src, source = (drums, "drums") if drums.is_file() else (jdir / row["audio_file"], "mix")
    import librosa
    dur = (body.to_sec - body.from_sec) if body.to_sec > 0 else None
    y, sr = librosa.load(str(src), sr=22050, mono=True, offset=body.from_sec, duration=dur)
    if len(y) < sr * MIN_GRID_SEC:
        raise HTTPException(422, f"window too short: need at least {MIN_GRID_SEC} s of audio")
    g = beat_grid(y, sr)
    if not g["bpm"]:
        raise HTTPException(422, "no beat found in the window")
    return {"bpm": g["bpm"], "offset": round(body.from_sec + g["offset"], 3),
            "strength": g["strength"], "source": source}


@app.post("/jobs/{job_id}/analyze")
def analyze_job(job_id: int, fresh: bool = False):
    """DSP-замер готового трека (librosa): метрики кэшируются в metrics.json."""
    with db_lock, db() as conn:
        row = conn.execute("SELECT audio_file FROM jobs WHERE id=?", (job_id,)).fetchone()
    if row is None or not row["audio_file"]:
        raise HTTPException(404, "job or audio not found")
    metrics_path = JOBS_DIR / str(job_id) / "metrics.json"
    if not fresh and metrics_path.is_file():
        return json.loads(metrics_path.read_text())
    try:
        metrics = analyze_file(JOBS_DIR / str(job_id) / row["audio_file"])
    except Exception as e:  # noqa: BLE001
        raise HTTPException(500, f"analysis failed: {e}") from e
    metrics_path.write_text(json.dumps(metrics))
    return metrics


@app.post("/references")
async def add_reference(request: Request):
    """Референс для дельты метрик: тело — сырые байты аудио, X-Filename — имя."""
    fname = re.sub(r"[^A-Za-z0-9_.-]", "_", request.headers.get("x-filename", "")) or "reference.flac"
    data = await request.body()
    if not data:
        raise HTTPException(422, "empty body")
    rid = f"{int(time.time())}-{fname}"
    try:
        metrics = None
        (REFS_DIR / rid).write_bytes(data)
        metrics = analyze_file(REFS_DIR / rid)
        (REFS_DIR / f"{rid}.json").write_text(json.dumps(
            {"id": rid, "created_at": time.strftime("%Y-%m-%dT%H:%M:%S"), "metrics": metrics}))
    except Exception as e:  # noqa: BLE001
        for p in (REFS_DIR / rid, REFS_DIR / f"{rid}.json"):
            p.unlink(missing_ok=True)
        raise HTTPException(422, f"cannot analyze reference: {e}") from e
    return {"id": rid, "metrics": metrics}


@app.get("/references")
def list_references():
    out = []
    for mp in sorted(REFS_DIR.glob("*.json"), key=lambda p: p.stat().st_mtime, reverse=True):
        try:
            item = json.loads(mp.read_text())
        except Exception:  # noqa: BLE001
            continue
        audio = mp.with_suffix("")
        item["size"] = audio.stat().st_size if audio.is_file() else 0
        out.append(item)
    return out


DSP_NAME_RE = re.compile(r"^(dsp|overdub|preview|stem)-[a-z0-9.-]+\.flac$")


# ---------- Лирика: whisper-распознавание и адаптация-перевод ----------

# язык пения по первому слову стиля YuE («English, post-punk, …») → код whisper
STYLE_LANGS = {"english": "en", "russian": "ru", "chinese": "zh", "mandarin": "zh", "cantonese": "yue",
               "japanese": "ja", "korean": "ko", "spanish": "es", "french": "fr", "german": "de",
               "italian": "it", "portuguese": "pt"}


def lyrics_language(style: str, explicit: str | None = None) -> str | None:
    """Язык для распознавания: явный (код вроде «en»; «auto» — определить
    самому) или по первому тегу стиля трека; не понять — None (whisper сам)."""
    if explicit:
        e = explicit.strip().lower()
        return None if e == "auto" else e
    first = (style or "").split(",")[0].strip().lower()
    return STYLE_LANGS.get(first)


def _whisper_lyrics(src: Path, language: str | None = None) -> tuple[str, str]:
    """Текст трека через faster-whisper (отдельный venv, см. whisper_run.py).
    language — код языка пения (None — whisper определяет сам).
    Возвращает (текст, ошибка): при неудаче текст пустой, ошибка — пояснение."""
    if not WHISPER_PY.is_file():
        return "", f"whisper venv not found: {WHISPER_PY}"
    try:
        # ctranslate2 в whisper-venv не видит libcublas из pip-wheel'ов nvidia
        import subprocess
        import sys
        env = dict(os.environ)
        lib = Path(sys.prefix) / "lib"
        cublas = next(iter(lib.glob("python*/site-packages/nvidia/cublas/lib")), None)
        cudnn = next(iter(lib.glob("python*/site-packages/nvidia/cudnn/lib")), None)
        extra = ":".join(str(p) for p in (cublas, cudnn) if p)
        if extra:
            env["LD_LIBRARY_PATH"] = extra + ":" + env.get("LD_LIBRARY_PATH", "")
        cmd = [str(WHISPER_PY), str(Path(__file__).parent / "whisper_run.py"), str(src)]
        if language:
            cmd += ["--language", language]
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=600, env=env)
        if r.returncode != 0:
            return "", (r.stderr or "whisper failed")[-500:]
        return json.loads(r.stdout).get("text", ""), ""
    except Exception as e:  # noqa: BLE001
        return "", str(e)


def _adapt_prompts(text: str, to: str) -> tuple[str, str]:
    """Делегирует llm.adapt_prompts (чистая функция, покрыта тестами)."""
    return llm.adapt_prompts(text, to)


class LyricsAdaptIn(BaseModel):
    text: str
    to: str = "Russian"


@app.post("/lyrics")
async def recognize_lyrics(request: Request):
    """Трек (тело — байты аудио, X-Filename) → текст (faster-whisper).
    Для каверов: текст оригинала → адаптация → поле лирики."""
    fname = re.sub(r"[^A-Za-z0-9_.-]", "_", request.headers.get("x-filename", "")) or "input.flac"
    language = lyrics_language("", request.headers.get("x-language"))
    data = await request.body()
    if not data:
        raise HTTPException(422, "empty body")
    with tempfile.TemporaryDirectory() as td:
        src = Path(td) / fname
        src.write_bytes(data)
        t0 = time.time()
        text, err = _whisper_lyrics(src, language)
    if err:
        raise HTTPException(500, f"lyrics recognition failed: {err}")
    if not text.strip():
        raise HTTPException(422, "no speech recognized")
    return {"text": text.strip(), "seconds": round(time.time() - t0, 1)}


@app.post("/lyrics/adapt")
def adapt_lyrics(req: LyricsAdaptIn):
    """Адаптация-перевод лирики под пение (Ollama, сохранение слогов).
    keep_alive=0: модель выгружается из VRAM сразу (см. /copilot)."""
    if not req.text.strip():
        raise HTTPException(422, "text is required")
    system, user = _adapt_prompts(req.text, req.to)
    t0 = time.time()
    try:
        out = llm.strip_md(llm.ollama_chat(OLLAMA_URL, OLLAMA_MODEL, system, user,
                                           temperature=0.4, timeout=280))
    except Exception as e:  # noqa: BLE001
        raise HTTPException(502, f"ollama failed: {e}") from e
    if not out.strip():
        raise HTTPException(502, "ollama returned empty adaptation")
    return {"text": out.strip(), "seconds": round(time.time() - t0, 1)}


# ---------- Remix: транскрипция трека (SheetSage2) ----------

@app.post("/transcribe")
async def transcribe(request: Request):
    """Трек (тело — байты аудио, X-Filename) → ABC лид-лист. Для каверов:
    полученный ABC правится и рендерится с новым стилем/текстом."""
    fname = re.sub(r"[^A-Za-z0-9_.-]", "_", request.headers.get("x-filename", "")) or "input.flac"
    data = await request.body()
    if not data:
        raise HTTPException(422, "empty body")
    tid = f"{int(time.time())}-{fname}"
    tdir = TRANSCRIBES_DIR / tid
    tdir.mkdir(parents=True, exist_ok=True)
    src = tdir / fname
    src.write_bytes(data)
    try:
        result = ss_transcribe(src, tdir)
    except Exception as e:  # noqa: BLE001
        log.exception("transcribe failed")
        raise HTTPException(500, f"transcribe failed: {e}") from e
    (tdir / "meta.json").write_text(json.dumps({
        "id": tid, "filename": fname,
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%S"), **result}))
    return {"id": tid, **result}


@app.get("/transcribes")
def list_transcribes():
    out = []
    for mp in sorted(TRANSCRIBES_DIR.glob("*/meta.json"),
                     key=lambda p: p.stat().st_mtime, reverse=True)[:50]:
        try:
            out.append(json.loads(mp.read_text()))
        except Exception:  # noqa: BLE001
            continue
    return out


@app.get("/transcribes/{tid}/{fname}")
def transcribe_file(tid: str, fname: str):
    if "/" in tid or ".." in tid or "/" in fname or ".." in fname:
        raise HTTPException(400, "bad path")
    path = TRANSCRIBES_DIR / tid / fname
    if not path.is_file():
        raise HTTPException(404, "not found")
    return FileResponse(path)


# ---------- Пиано-ролл: расклад score.abc по тактам ----------

def _job_row(job_id: int):
    with db_lock, db() as conn:
        return conn.execute("SELECT * FROM jobs WHERE id=?", (job_id,)).fetchone()


@app.post("/jobs/{job_id}/lyrics")
def job_lyrics(job_id: int, language: str | None = None):
    """Текст из готового аудио джобы (faster-whisper) — без повторной загрузки
    файла: аудио уже у воркера. Для овердаба/кавера этой же джобы.
    Есть дорожка голоса (demucs) — распознаётся она, а не микс: гитары сбивали
    whisper. Язык — ?language=en|ru|…|auto, без него — по стилю трека."""
    row = _job_row(job_id)
    if row is None or row["status"] != "done" or not row["audio_file"]:
        raise HTTPException(404, "job not done (no audio)")
    jdir = JOBS_DIR / str(job_id)
    vocals = jdir / "stem-vocals.flac"
    src = vocals if vocals.is_file() else jdir / row["audio_file"]
    t0 = time.time()
    text, err = _whisper_lyrics(src, lyrics_language(row["style"], language))
    if err:
        raise HTTPException(500, f"lyrics recognition failed: {err}")
    if not text.strip():
        raise HTTPException(422, "no speech recognized")
    return {"text": text.strip(), "seconds": round(time.time() - t0, 1)}


# версия формата/расчёта score.json: 2 — per-voice таймлайн тактов; 3 — мультипауза
# Z<n> = n тактов и время по темпу заголовка (без смены версии кэш отдавал старое)
SCORE_V = 3


class PlanCheckIn(BaseModel):
    abc: str = ""
    from_sec: float | None = None


@app.post("/jobs/{job_id}/plan_check")
def plan_check(job_id: int, req: PlanCheckIn):
    """Что изменилось в плане abc относительно плана джобы и где проблемы
    (потолок голоса, правки до отметки, сдвиг тактов) — для MCP."""
    row = _job_row(job_id)
    if row is None:
        raise HTTPException(404, "job not found")
    abc_path = JOBS_DIR / str(job_id) / (row["abc_file"] or "score.abc")
    if not abc_path.is_file():
        raise HTTPException(404, "no score.abc for this job")
    if not req.abc.strip():
        raise HTTPException(422, "abc is empty")
    try:
        return plan_diff(abc_path.read_text(), req.abc, req.from_sec)
    except ValueError as e:   # нот/тактов не разобрать
        raise HTTPException(422, f"abc: {e}") from None


@app.get("/jobs/{job_id}/score")
def job_score(job_id: int):
    """Таймлайн из score.abc: такты × голоса, аккорды, секции + RMS по секциям.
    Кэшируется в score.json; _v — версия формата (смена разметки сбрасывает кэш)."""
    row = _job_row(job_id)
    if row is None:
        raise HTTPException(404, "job not found")
    job_dir = JOBS_DIR / str(job_id)
    abc_path = job_dir / (row["abc_file"] or "score.abc")
    cache = job_dir / "score.json"
    if not abc_path.is_file():
        raise HTTPException(404, "no score.abc for this job")
    if cache.is_file() and cache.stat().st_mtime >= abc_path.stat().st_mtime:
        try:
            cached = json.loads(cache.read_text())
            if cached.get("_v") == SCORE_V:
                return cached
        except Exception:  # noqa: BLE001 — битый кэш просто перегенерим
            pass
    parsed = parse_abc(abc_path.read_text())
    parsed["_v"] = SCORE_V
    parsed["rms_sections"] = _rms_sections(job_dir / row["audio_file"], parsed["bars"]) \
        if row["audio_file"] and (job_dir / row["audio_file"]).is_file() else []
    cache.write_text(json.dumps(parsed, ensure_ascii=False))
    return parsed


# ---------- Высота голоса по тактам (проверка «спето ли по плану») ----------
# Замер 2026-09-30: модель поёт мелодию Vocal плана нота в ноту — сверка контура
# с планом показывает, дошла ли правка мелодии и не ушёл ли голос в писк.
_NOTE_NAMES = ["C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"]
_CONTOUR_MIN_FRAMES = 3   # меньше голосных кадров в четверти — «·» (шум, согласные)


def _note_name(hz) -> str:
    """Ближайшая нота равномерного строя (A4 = 440 Гц); нет высоты — «·»."""
    import math
    try:
        hz = float(hz)
    except (TypeError, ValueError):
        return "·"
    if not hz > 0:   # 0, отрицательное, NaN
        return "·"
    midi = round(69 + 12 * math.log2(hz / 440.0))
    return f"{_NOTE_NAMES[midi % 12]}{midi // 12 - 1}"


def _contour_bars(f0, hop_sec: float, bars: list[dict], from_s: float, to_s: float) -> list[dict]:
    """Кадры F0 (NaN — нет голоса) → такты [from, to): по четвертям медиана
    высоты голосных кадров → имя ноты."""
    import numpy as np
    f0 = np.asarray(f0, dtype=float)
    out = []
    for i, b in enumerate(bars):
        start, end = float(b["start_sec"]), float(b["end_sec"])
        if end <= from_s or start >= to_s:
            continue
        q = (end - start) / 4
        notes = []
        for k in range(4):
            a = int(round((start + k * q) / hop_sec))
            z = int(round((start + (k + 1) * q) / hop_sec))
            seg = f0[a:z]
            seg = seg[np.isfinite(seg) & (seg > 0)]
            notes.append(_note_name(float(np.median(seg))) if len(seg) >= _CONTOUR_MIN_FRAMES else "·")
        out.append({"index": i, "start": start, "end": end, "notes": notes})
    return out


def _contour_summary(f0) -> dict:
    """Медиана и диапазон (5–95-й перцентиль) высоты голоса, Гц."""
    import numpy as np
    f = np.asarray(f0, dtype=float)
    f = f[np.isfinite(f) & (f > 0)]
    if not len(f):
        return {"median_hz": None, "low_hz": None, "high_hz": None}
    return {"median_hz": round(float(np.median(f)), 1),
            "low_hz": round(float(np.percentile(f, 5)), 1),
            "high_hz": round(float(np.percentile(f, 95)), 1)}


@app.get("/jobs/{job_id}/vocal_contour")
def vocal_contour(job_id: int, from_: float = Query(0.0, alias="from"), to: float = 0.0):
    """Высота голоса по тактам плана (стем vocals, pyin) в окне [from, to);
    to = 0 — до конца трека. Нужен стем вокала (make_stems)."""
    row = _job_row(job_id)
    if row is None:
        raise HTTPException(404, "job not found")
    job_dir = JOBS_DIR / str(job_id)
    stem = job_dir / "stem-vocals.flac"
    if not stem.is_file():
        raise HTTPException(409, "no vocals stem — run make_stems first")
    abc_path = job_dir / (row["abc_file"] or "score.abc")
    if not abc_path.is_file():
        raise HTTPException(404, "no score.abc for this job")
    import math
    dur = float(row["duration_sec"] or 0)
    if not (math.isfinite(from_) and math.isfinite(to)) or from_ < 0 or (dur and from_ >= dur) \
            or (to > 0 and to <= from_):
        raise HTTPException(422, "from/to outside the track")
    parsed = parse_abc(abc_path.read_text())
    bars = [b for b in parsed["bars"] if any("vocal" in v.lower() for v in (b.get("voices") or {}))]
    import librosa
    import numpy as np
    to_s = to if to > 0 else float(row["duration_sec"] or parsed["duration_sec"] or 0)
    sr, hop = 22050, 512
    y, _ = librosa.load(str(stem), sr=sr, mono=True, offset=max(0.0, from_), duration=max(0.1, to_s - from_))
    f0, _, _ = librosa.pyin(y, fmin=70, fmax=900, sr=sr, hop_length=hop)
    # кадры считаются от from — сдвигаем таймлайн тактов в ту же шкалу
    pad = int(round(max(0.0, from_) / (hop / sr)))
    f0 = np.concatenate([np.full(pad, np.nan), f0])
    return {"bars": _contour_bars(f0, hop / sr, bars, from_, to_s), **_contour_summary(f0)}


# ---------- «Найти свист»: узкие устойчивые тона в миксе ----------
_TONE_NEIGH_HZ = 300    # окрестность пика для медианы фона, Гц
_TONE_GUARD_BINS = 2    # сам пик (± бины) в медиану фона не входит


def _tonal_peaks(power, freqs, min_hz=1000, max_hz=18000, min_prom_db=12, top=5) -> list[dict]:
    """Узкие пики среднего спектра мощности: на сколько дБ бин выше медианы
    окрестности (±300 Гц без самого пика). Локальные максимумы в [min_hz, max_hz]
    с prominence ≥ min_prom_db, самые заметные первыми. Широкий горб — не тон."""
    import numpy as np
    power = np.asarray(power, dtype=float)
    freqs = np.asarray(freqs, dtype=float)
    if len(freqs) < 3:
        return []
    step = float(freqs[1] - freqs[0]) or 1.0
    half = max(_TONE_GUARD_BINS + 2, int(round(_TONE_NEIGH_HZ / step)))
    out = []
    for i in range(1, len(power) - 1):
        if not (min_hz <= freqs[i] <= max_hz):
            continue
        if not (power[i] >= power[i - 1] and power[i] > power[i + 1]):
            continue
        lo, hi = max(0, i - half), min(len(power), i + half + 1)
        neigh = np.concatenate([power[lo:max(lo, i - _TONE_GUARD_BINS)], power[i + _TONE_GUARD_BINS + 1:hi]])
        if not len(neigh):
            continue
        prom = 10 * np.log10(power[i] / max(float(np.median(neigh)), 1e-30))
        if prom >= min_prom_db:
            out.append({"hz": round(float(freqs[i]), 1), "prominence_db": round(float(prom), 1)})
    out.sort(key=lambda x: -x["prominence_db"])
    return out[:top]


def _check_window(from_: float, to: float, dur: float):
    """Окно анализа внутри трека, иначе 422 (а не 500 из librosa)."""
    import math
    if not (math.isfinite(from_) and math.isfinite(to)) or from_ < 0 or (dur and from_ >= dur) \
            or (to > 0 and to <= from_):
        raise HTTPException(422, "from/to outside the track")


_TONE_STEMS = MAIN_STEMS + DETAIL_STEMS


@app.get("/jobs/{job_id}/tones")
def job_tones(job_id: int, from_: float = Query(0.0, alias="from"), to: float = 0.0, stem: str = ""):
    """Узкие тона («свист») в окне [from, to); to = 0 — до конца. stem —
    дорожка (vocals/drums/bass/other/guitar/piano; нужен make_stems), пусто — весь микс."""
    row = _job_row(job_id)
    if row is None:
        raise HTTPException(404, "job not found")
    if stem:
        if stem not in _TONE_STEMS:   # только имена из списка — не путь
            raise HTTPException(422, f"stem must be one of {', '.join(_TONE_STEMS)}")
        audio = JOBS_DIR / str(job_id) / f"stem-{stem}.flac"
        if not audio.is_file():
            raise HTTPException(409, f"no {stem} stem — run make_stems first")
    else:
        audio = JOBS_DIR / str(job_id) / (row["audio_file"] or "audio.flac")
        if not audio.is_file():
            raise HTTPException(404, "no audio for this job")
    dur = float(row["duration_sec"] or 0)
    _check_window(from_, to, dur)
    import librosa
    import numpy as np
    to_s = to if to > 0 else (dur or 1e9)
    y, sr = librosa.load(str(audio), sr=44100, mono=True, offset=from_, duration=max(0.1, to_s - from_))
    S = np.abs(librosa.stft(y, n_fft=8192, hop_length=2048)) ** 2
    return _tonal_peaks(S.mean(axis=1), librosa.fft_frequencies(sr=sr, n_fft=8192))


def _rms_sections(audio_path: Path, bars: list[dict]) -> list[dict]:
    """Средняя RMS-громкость (дБ) по секциям, границы берём из тактов."""
    try:
        import librosa
        import numpy as np
        y, sr = librosa.load(str(audio_path), sr=22050, mono=True)
    except Exception:  # noqa: BLE001
        return []
    total = len(y) / sr
    sections: list[dict] = []
    for bar in bars:
        if sections and sections[-1]["section"] == bar["section"]:
            sections[-1]["end_sec"] = bar["end_sec"]
        else:
            sections.append({"section": bar["section"],
                             "start_sec": min(bar["start_sec"], total),
                             "end_sec": min(bar["end_sec"], total)})
    out = []
    for s in sections:
        if s["end_sec"] <= s["start_sec"]:
            continue
        seg = y[int(s["start_sec"] * sr):int(s["end_sec"] * sr)]
        if not len(seg):
            continue
        out.append({**s, "rms_db": round(float(20 * np.log10(max(np.sqrt(np.mean(seg ** 2)), 1e-9))), 1)})
    return out


# ---------- Волна громкости и спектрограмма (выбор места правки по звуку) ----------

@app.get("/jobs/{job_id}/peaks")
def job_peaks(job_id: int, file: str | None = None, bins: int | None = None):
    """Огибающая громкости артефакта: [min, max] по окнам (амплитуды [-1,1]).
    Кэш-сайдикар <файл>.peaks.json с mtime-гейтом и _v (как score.json);
    кэшируется только канонический bins (по длительности), явный другой
    считается мимо кэша, чтобы селектор разрешений не затирал кэш."""
    row = _job_row(job_id)
    if row is None:
        raise HTTPException(404, "job not found")
    fname = file or row["audio_file"]
    if not fname or "/" in fname or ".." in fname:
        raise HTTPException(400, "bad filename")
    src = JOBS_DIR / str(job_id) / fname
    if not src.is_file():
        raise HTTPException(404, "file not found")
    duration = _audio_duration(src)
    if duration <= 0:
        raise HTTPException(422, "cannot read audio")
    canonical = bins is None
    n = waveform.default_bins(duration) if canonical else waveform.clamp_bins(bins)
    cache = src.parent / (src.name + ".peaks.json")
    if canonical and cache.is_file() and cache.stat().st_mtime >= src.stat().st_mtime:
        try:
            cached = json.loads(cache.read_text())
            if cached.get("_v") == waveform.PEAKS_V and cached.get("bins") == n \
                    and cached.get("file") == fname:
                return cached
        except Exception:  # noqa: BLE001 — битый кэш просто перегенерим
            pass
    try:
        out = waveform.compute_peaks(src, n)
    except Exception as e:  # noqa: BLE001 — битый/нечитаемый аудио
        raise HTTPException(422, f"cannot compute peaks: {e}") from e
    if canonical:
        cache.write_text(json.dumps(out))
    return out


@app.get("/jobs/{job_id}/spectrum.png")
def job_spectrum(job_id: int, file: str | None = None):
    """Спектрограмма артефакта готовой картинкой ffmpeg showspectrumpic:
    ось X линейна 0..длительность — та же шкала времени, что у волны.
    Кэш <файл>.spectrum.png + метаданные <файл>.spectrum.json (mtime-гейт).
    ffmpeg на воркере опционален: нет бинарника — 503, волна работает."""
    row = _job_row(job_id)
    if row is None:
        raise HTTPException(404, "job not found")
    fname = file or row["audio_file"]
    if not fname or "/" in fname or ".." in fname:
        raise HTTPException(400, "bad filename")
    src = JOBS_DIR / str(job_id) / fname
    if not src.is_file():
        raise HTTPException(404, "file not found")
    png = src.parent / (src.name + ".spectrum.png")
    meta = src.parent / (src.name + ".spectrum.json")
    if png.is_file() and png.stat().st_mtime >= src.stat().st_mtime and meta.is_file():
        try:
            if json.loads(meta.read_text()).get("_v") == waveform.SPECTRUM_V:
                return FileResponse(png, media_type="image/png")
        except Exception:  # noqa: BLE001 — битые метаданные перегенерим
            pass
    try:
        m = waveform.render_spectrum_png(src, png)
    except FileNotFoundError as e:
        raise HTTPException(503, str(e)) from e
    except Exception as e:  # noqa: BLE001 — ffmpeg отказал/битый аудио
        raise HTTPException(422, f"cannot render spectrum: {e}") from e
    meta.write_text(json.dumps(m))
    return FileResponse(png, media_type="image/png")


# ---------- Фрагментное превью: VAE-decode куска латентов ----------

LATENT_HZ = 25.0  # латенты YuE2 — 25 кадров/сек (замерено: 1556 кадров = 62.2 с)


@app.post("/jobs/{job_id}/preview")
def job_preview(job_id: int, body: dict):
    """Превью фрагмента без AR-генерации: срез latent.npy → VAE decode.
    Тело: {from_sec, to_sec}. → preview-<f>-<t>.flac в папке джобы."""
    row = _job_row(job_id)
    if row is None or row["status"] != "done":
        raise HTTPException(404, "job not done")
    try:
        from_s = max(0.0, float(body.get("from_sec", 0)))
        to_s = float(body.get("to_sec", 10))
    except (TypeError, ValueError) as e:
        raise HTTPException(422, f"bad range: {e}") from e
    if to_s - from_s < 1.0:
        raise HTTPException(422, "range must be >= 1 second")
    latents_path = JOBS_DIR / str(job_id) / "latent.npy"
    if not latents_path.is_file():
        raise HTTPException(404, "no latent.npy for this job")
    import numpy as np
    import soundfile as sf
    z = np.load(latents_path)
    total_sec = z.shape[0] / LATENT_HZ
    # расклад ABC бывает длиннее реального звука (декод обрезан) — клэмпим оба конца
    from_s = min(max(0.0, from_s), max(0.0, total_sec - 1.0))
    to_s = min(max(to_s, from_s + 1.0), total_sec)
    a, b = int(from_s * LATENT_HZ), max(int(to_s * LATENT_HZ), int(from_s * LATENT_HZ) + 1)
    with _pipe_lock:
        pipe = _get_pipe()
        try:
            audio = pipe.decode(z[a:b, :])
        except Exception as e:  # noqa: BLE001
            raise HTTPException(500, f"decode failed: {e}") from e
    fname = f"preview-{from_s:.0f}-{to_s:.0f}.flac"
    sf.write(str(JOBS_DIR / str(job_id) / fname), audio, 48000)
    return {"file": fname, "from_sec": from_s, "to_s": to_s,
            "duration_sec": round(to_s - from_s, 1)}


@app.get("/jobs/{job_id}/previews")
def job_previews(job_id: int):
    job_dir = JOBS_DIR / str(job_id)
    if not job_dir.is_dir():
        return []
    return [f.name for f in sorted(job_dir.glob("preview-*.flac"),
                                   key=lambda p: p.stat().st_mtime, reverse=True)]


# ---------- Овердаб: партия поверх готового трека ----------

class OverdubIn(BaseModel):
    style: str
    gain: float = 0.5
    lyrics: str = ""
    # сид родителя = «та же интерпретация» (ближе к оригиналу); None — новая
    seed: int | None = None
    # свой план партии (иначе — score.abc исходника); так «+ инструмент»
    # рендерит план, где партия молчит вне нужного куска — локализация звука
    abc: str | None = None


def _mix_overdub(parent_id: int, child_id: int, gain: float = 0.5):
    """Микс: дочерний рендер (по ABC родителя) поверх родительского аудио."""
    import numpy as np
    import soundfile as sf
    parent = JOBS_DIR / str(parent_id) / "audio.flac"
    child = JOBS_DIR / str(child_id) / "audio.flac"
    if not parent.is_file() or not child.is_file():
        log.warning("overdub mix skipped: missing audio for %s/%s", parent_id, child_id)
        return
    a, sr = sf.read(str(parent), always_2d=True, dtype="float32")
    b, b_sr = sf.read(str(child), always_2d=True, dtype="float32")
    if b_sr != sr:  # не должно случаться (оба 48k), но не молчим
        log.warning("overdub sr mismatch %s/%s", sr, b_sr)
    n = max(len(a), len(b))
    mix = np.zeros((n, a.shape[1]), dtype="float32")
    mix[:len(a)] += a
    mb = np.zeros((n, a.shape[1]), dtype="float32")
    mb[:min(len(b), n)] = b[:n]
    mix[:len(mb)] += mb * gain
    # короткий кроссфейд в конце, чтобы не щёлкало на обрезе
    fade = min(int(0.05 * sr), n)
    if fade:
        mix[-fade:] *= np.linspace(1, 0, fade)[:, None]
    out = JOBS_DIR / str(parent_id) / f"overdub-{child_id}.flac"
    peak = float(np.abs(mix).max())
    if peak > 1.0:
        mix /= peak
    sf.write(str(out), mix, sr)
    try:
        analyze_file(out)
    except Exception:  # noqa: BLE001
        log.exception("overdub analyze failed")
    log.info("overdub: %s mixed over %s (gain %.2f)", child_id, parent_id, gain)


class ImportIn(BaseModel):
    title: str = ""
    transcribe: bool = True   # сразу делать транскрипцию (SheetSage2) для ролла/овердаба


class VariantTrackIn(BaseModel):
    """Вариант DSP-эффекта → отдельный трек: тот же звук с обработкой,
    с подписью эффекта; в студии работают стемы/минус/эффекты."""
    file: str
    title: str = ""
    voice_src: int | None = None   # рендер, чей голос подставлен в версию


@app.post("/jobs/{job_id}/variant_track")
def job_variant_track(job_id: int, req: VariantTrackIn):
    import shutil
    if "/" in req.file or ".." in req.file or not DSP_NAME_RE.match(req.file):
        raise HTTPException(422, "file must be dsp-<chain>.flac")
    with db_lock, db() as conn:
        row = conn.execute("SELECT title, abc_file FROM jobs WHERE id=?", (job_id,)).fetchone()
        if row is None:
            raise HTTPException(404, "job not found")
        src = JOBS_DIR / str(job_id) / req.file
        if not src.is_file():
            raise HTTPException(404, f"variant {req.file} not found")
        if req.voice_src is not None and conn.execute(
                "SELECT 1 FROM jobs WHERE id=?", (req.voice_src,)).fetchone() is None:
            raise HTTPException(422, "voice_src job not found")
        cur = conn.execute(
            "INSERT INTO jobs(title,status,style,lyrics,seed,cot,parent_id,role,voice_src,created_at,finished_at)"
            " VALUES(?,?,?,?,?,?,?,?,?,?,?)",
            (req.title or row["title"], "done", "(вариант DSP-эффекта)", "", None, "full",
             job_id, "variant", req.voice_src,
             time.strftime("%Y-%m-%dT%H:%M:%S"), time.strftime("%Y-%m-%dT%H:%M:%S")))
        jid = cur.lastrowid
    jdir = JOBS_DIR / str(jid)
    try:
        jdir.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, jdir / "audio.flac")
        if row["abc_file"] and (JOBS_DIR / str(job_id) / row["abc_file"]).is_file():
            shutil.copy2(JOBS_DIR / str(job_id) / row["abc_file"], jdir / "score.abc")
            abc = "score.abc"
        else:
            abc = ""
    except OSError:
        # иначе в списке остаётся готовый трек без аудио
        log.exception("variant %s of job %s: copy failed, job %s rolled back", req.file, job_id, jid)
        with db_lock, db() as conn:
            conn.execute("DELETE FROM jobs WHERE id=?", (jid,))
        shutil.rmtree(jdir, ignore_errors=True)
        raise
    with db_lock, db() as conn:
        conn.execute(
            "UPDATE jobs SET duration_sec=?, audio_file=?, abc_file=? WHERE id=?",
            (_audio_duration(jdir / "audio.flac"), "audio.flac", abc, jid))
    log.info("variant %s of job %s promoted to job %s", req.file, job_id, jid)
    return {"id": jid}


# стиль импортированного трека; фронт по нему узнаёт импорт (frontend: StudioPage.vue)
IMPORT_STYLE = "(импорт внешнего трека)"


@app.post("/tracks/import")
async def tracks_import(request: Request, transcribe: bool = True, title: str = ""):
    """Импорт внешнего трека как джобы-статуса done: дальше работают стемы,
    минус, эффекты, ролл (по транскрипции) и овердаб — вся студия."""
    fname = re.sub(r"[^A-Za-z0-9_.-]", "_", request.headers.get("x-filename", "")) or "track.flac"
    data = await request.body()
    if not data:
        raise HTTPException(422, "empty body")
    with db_lock, db() as conn:
        cur = conn.execute(
            "INSERT INTO jobs(title,status,style,lyrics,seed,cot,created_at,finished_at)"
            " VALUES(?,?,?,?,?,?,?,?)",
            (title or fname.rsplit(".", 1)[0], "done", IMPORT_STYLE, "",
             None, "full", time.strftime("%Y-%m-%dT%H:%M:%S"), time.strftime("%Y-%m-%dT%H:%M:%S")))
        jid = cur.lastrowid
    jdir = JOBS_DIR / str(jid)
    jdir.mkdir(parents=True, exist_ok=True)
    ext = "".join(Path(fname).suffixes)[-5:] or ".flac"
    audio = jdir / ("audio" + ext)
    audio.write_bytes(data)
    abc_file = ""
    transcribe_error = ""
    if transcribe:
        try:
            result = ss_transcribe(audio, jdir)
            (jdir / "score.abc").write_text(result.get("abc", ""), encoding="utf-8")
            abc_file = "score.abc" if result.get("abc", "").strip() else ""
        except Exception as e:  # noqa: BLE001
            log.exception("import transcribe failed")
            transcribe_error = str(e)
    with db_lock, db() as conn:
        conn.execute(
            "UPDATE jobs SET duration_sec=?, audio_file=?, abc_file=? WHERE id=?",
            (_audio_duration(audio), audio.name, abc_file, jid))
    return {"id": jid, "duration_sec": _audio_duration(audio), "abc_file": abc_file,
            "transcribe_error": transcribe_error}


@app.post("/jobs/{job_id}/overdub")
def submit_overdub(job_id: int, req: OverdubIn):
    """Рендер партии по score.abc джобы (механизм req_abc) с её стилем;
    по завершении воркер микширует результат поверх исходника (overdub-<id>.flac).

    Честная граница: YuE2 не даёт стемов — «партия» = новый рендер того же ABC
    под новым стилем, подмешанный к оригиналу."""
    row = _job_row(job_id)
    if row is None or row["status"] != "done" or not row["abc_file"]:
        raise HTTPException(404, "job not done (no score.abc)")
    abc = (JOBS_DIR / str(job_id) / row["abc_file"]).read_text()
    if req.abc and req.abc.strip():
        abc = req.abc   # свой план партии (напр., инструмент только в выделении)
    with db_lock, db() as conn:
        cur = conn.execute(
            "INSERT INTO jobs(title,status,style,lyrics,seed,cot,req_abc,overdub_of,created_at)"
            " VALUES(?,?,?,?,?,?,?,?,?)",
            (f"overdub of #{job_id}", "queued", req.style, req.lyrics, req.seed, "full",
             "", job_id, time.strftime("%Y-%m-%dT%H:%M:%S")))
        child_id = cur.lastrowid
        cdir = JOBS_DIR / str(child_id)
        cdir.mkdir(parents=True, exist_ok=True)
        (cdir / "request.abc").write_text(abc, encoding="utf-8")
        conn.execute("UPDATE jobs SET req_abc='request.abc', overdub_gain=? WHERE id=?",
                     (req.gain, child_id))
    return {"id": child_id}


# ---------- Стемы (demucs) ----------

@app.post("/jobs/{job_id}/stems")
def job_stems(job_id: int):
    row = _job_row(job_id)
    if row is None or not row["audio_file"]:
        raise HTTPException(404, "job or audio not found")
    job_dir = JOBS_DIR / str(job_id)
    try:
        result = _separate(job_id, job_dir / row["audio_file"], job_dir)
    except Exception as e:  # noqa: BLE001
        log.exception("stems failed")
        raise HTTPException(500, f"demucs failed: {friendly_error(e)}") from e
    for f in result["stems"]:
        try:
            m = analyze_file(job_dir / f)
            (job_dir / f"{f}.metrics.json").write_text(
                json.dumps({"file": f, "metrics": m}))
        except Exception:  # noqa: BLE001
            pass
    return result


@app.post("/jobs/{job_id}/mp3")
def job_mp3(job_id: int):
    """Ленивая конвертация в mp3 320 (для импортированных треков)."""
    row = _job_row(job_id)
    if row is None or not row["audio_file"]:
        raise HTTPException(404, "job or audio not found")
    job_dir = JOBS_DIR / str(job_id)
    out = job_dir / "audio.mp3"
    if not out.is_file():
        try:
            media.encode_mp3(job_dir / row["audio_file"], out)
        except Exception as e:  # noqa: BLE001
            raise HTTPException(500, f"mp3 encode failed: {e}") from e
        with db_lock, db() as conn:
            conn.execute("UPDATE jobs SET mp3_file=? WHERE id=?", ("audio.mp3", job_id))
    return {"file": "audio.mp3"}


class MinusIn(BaseModel):
    exclude: list[str] = Field(default_factory=list)  # drums/bass/other/vocals


@app.post("/jobs/{job_id}/minus")
def job_minus(job_id: int, req: MinusIn):
    """Минус-трек: микс стемов без исключённых групп (честное смешивание, без
    перегенерации). Стемы разделяются при необходимости. → minus.flac."""
    row = _job_row(job_id)
    if row is None or not row["audio_file"]:
        raise HTTPException(404, "job or audio not found")
    job_dir = JOBS_DIR / str(job_id)
    # только основные дорожки: гитара/клавиши уже внутри «прочего»
    def main_stems():
        return [p for n in MAIN_STEMS if (p := job_dir / f"stem-{n}.flac").is_file()]
    stems = main_stems()
    if not stems:
        try:
            _separate(job_id, job_dir / row["audio_file"], job_dir)
        except Exception as e:  # noqa: BLE001
            raise HTTPException(500, f"demucs failed: {friendly_error(e)}") from e
        stems = main_stems()
        if not stems:
            raise HTTPException(500, "no stems after demucs")
    keep = [p for p in stems if p.stem.replace("stem-", "") not in req.exclude]
    if not keep:
        raise HTTPException(422, "cannot exclude every stem")
    import numpy as np
    import soundfile as sf
    mix = None
    sr = None
    for p in keep:
        data, sr = sf.read(str(p), always_2d=True, dtype="float32")
        mix = data if mix is None else mix[:len(data)] + data[:len(mix)]
    if mix is None:
        raise HTTPException(500, "no stems to mix")
    peak = float(np.max(np.abs(mix))) if mix.size else 0.0
    if peak > 0.99:
        mix = mix / peak * 0.99
    sf.write(str(job_dir / "minus.flac"), mix, sr)
    return {"file": "minus.flac", "excluded": req.exclude,
            "kept": [p.stem.replace("stem-", "") for p in keep]}


@app.get("/jobs/{job_id}/stems")
def job_stems_list(job_id: int):
    job_dir = JOBS_DIR / str(job_id)
    if not job_dir.is_dir():
        return []
    out = []
    for f in sorted(job_dir.glob("stem-*.flac")):
        mp = job_dir / f"{f.name}.metrics.json"
        out.append({"file": f.name, "name": f.stem.replace("stem-", ""),
                    "created_at": time.strftime("%Y-%m-%dT%H:%M:%S",
                                                time.localtime(f.stat().st_mtime)),
                    "metrics": json.loads(mp.read_text()) if mp.is_file() else None})
    return out


# ---------- Профиль из корпуса ----------

class CorpusIn(BaseModel):
    name: str


def _corpus_dir(cid: int) -> Path:
    return CORPUS_DIR / str(cid)


@app.post("/corpus")
def corpus_create(req: CorpusIn):
    if not req.name.strip():
        raise HTTPException(422, "name is required")
    with db_lock, db() as conn:
        cur = conn.execute("INSERT INTO corpus(name,status,created_at) VALUES(?,?,?)",
                           (req.name.strip(), "open", time.strftime("%Y-%m-%dT%H:%M:%S")))
        cid = cur.lastrowid
    _corpus_dir(cid).mkdir(parents=True, exist_ok=True)
    return {"id": cid}


@app.post("/corpus/{cid}/track")
async def corpus_add_track(cid: int, request: Request):
    """Трек корпуса (байты): DSP-паспорт + транскрипция SheetSage2 + текст Whisper.
    Обрабатывается синхронно — минута-другая на трек."""
    fname = re.sub(r"[^A-Za-z0-9_.-]", "_", request.headers.get("x-filename", "")) or "track.flac"
    with db_lock, db() as conn:
        if conn.execute("SELECT 1 FROM corpus WHERE id=?", (cid,)).fetchone() is None:
            raise HTTPException(404, "corpus not found")
    data = await request.body()
    if not data:
        raise HTTPException(422, "empty body")
    tdir = _corpus_dir(cid) / "tracks"
    tdir.mkdir(parents=True, exist_ok=True)
    n = len(list(tdir.glob("track-*")))
    track_dir = tdir / f"track-{n:02d}-{fname}"
    track_dir.mkdir(parents=True, exist_ok=True)
    src = track_dir / fname
    src.write_bytes(data)
    info: dict = {"filename": fname, "created_at": time.strftime("%Y-%m-%dT%H:%M:%S")}
    try:
        info["metrics"] = analyze_file(src)
    except Exception as e:  # noqa: BLE001
        info["metrics_error"] = str(e)
    try:
        t = ss_transcribe(src, track_dir / "ss2")
        info["abc"] = t["abc"]
        info["stats"] = t["stats"]
    except Exception as e:  # noqa: BLE001
        info["abc_error"] = str(e)
    lyrics_text, lyrics_err = _whisper_lyrics(src)
    info["lyrics"] = lyrics_text
    info["lyrics_error"] = lyrics_err
    (track_dir / "info.json").write_text(json.dumps(info, ensure_ascii=False))
    return info


@app.post("/corpus/{cid}/build")
def corpus_build(cid: int):
    """Агрегация треков в профиль: темп/тональности/прогрессии/структура,
    усреднённый DSP-паспорт, ABC-шаблон, стилевое описание (Ollama)."""
    cdir = _corpus_dir(cid)
    tracks = []
    for ip in sorted((cdir / "tracks").glob("*/info.json")) if (cdir / "tracks").is_dir() else []:
        try:
            tracks.append(json.loads(ip.read_text()))
        except Exception:  # noqa: BLE001
            continue
    if not tracks:
        raise HTTPException(422, "no analyzed tracks: add at least one")

    import numpy as np
    tempos = [t["metrics"]["tempo_bpm"] for t in tracks if t.get("metrics")]
    keys: dict[str, float] = {}
    chords: dict[str, int] = {}
    progs: dict[str, int] = {}
    structures: list[str] = []
    for t in tracks:
        st = t.get("stats") or {}
        for k, v in (st.get("keys") or {}).items():
            keys[k] = keys.get(k, 0.0) + v
        for c, v in (st.get("top_chords") or {}).items():
            chords[c] = chords.get(c, 0) + v
        for p, v in (st.get("top_progressions") or {}).items():
            progs[p] = progs.get(p, 0) + v
        structures.append(" → ".join(st.get("structure") or []))
    passport: dict[str, float] = {}
    for t in tracks:
        for k, v in (t.get("metrics") or {}).items():
            if isinstance(v, (int, float)) and not isinstance(v, bool):
                passport.setdefault(k, []).append(float(v))
    passport = {k: round(float(np.median(v)), 2) for k, v in passport.items() if len(v) >= max(1, len(tracks) // 2)}
    # ABC-шаблон: транскрипция трека, ближайшего к медианному темпу
    tpl_i = min(range(len(tracks)),
                key=lambda i: abs(tracks[i]["metrics"]["tempo_bpm"] - float(np.median(tempos)))
                ) if tempos else 0
    profile = {
        "id": cid,
        "tracks": len(tracks),
        "tempo_median": round(float(np.median(tempos)), 1) if tempos else None,
        "tempo_range": [round(min(tempos), 1), round(max(tempos), 1)] if tempos else None,
        "keys": dict(sorted(keys.items(), key=lambda kv: -kv[1])[:5]),
        "top_chords": dict(sorted(chords.items(), key=lambda kv: -kv[1])[:10]),
        "top_progressions": dict(sorted(progs.items(), key=lambda kv: -kv[1])[:10]),
        "structures": structures,
        "dsp_passport": passport,
        "abc_template": tracks[tpl_i].get("abc", ""),
        "lyrics_samples": [t.get("lyrics", "")[:600] for t in tracks if t.get("lyrics")],
    }
    profile["style"] = _corpus_style(profile)
    (cdir / "profile.json").write_text(json.dumps(profile, ensure_ascii=False))
    with db_lock, db() as conn:
        conn.execute("UPDATE corpus SET status='done' WHERE id=?", (cid,))
    return profile


def _corpus_style(profile: dict) -> str:
    """Стилевая строка профиля через Ollama-копайтера (qwen2.5)."""
    facts = json.dumps({k: profile[k] for k in
                        ("tempo_median", "keys", "top_chords", "top_progressions",
                         "dsp_passport")}, ensure_ascii=False, default=str)
    system = ("Ты — звукорежиссёр и продюсер. По статистике корпуса треков одного "
              "исполнителя составь ОДНУ строку описания стиля для генератора музыки "
              "(жанр, инструментовка, характер звука, продакшн). В ответе — только "
              "эта строка, без пояснений, на английском, как тег-строка.")
    try:
        return llm.strip_md(llm.ollama_chat(OLLAMA_URL, OLLAMA_MODEL, system,
                                            f"Статистика корпуса:\n{facts}",
                                            temperature=0.4))
    except Exception as e:  # noqa: BLE001
        log.warning("corpus style via ollama failed: %s", e)
        return ""


@app.get("/corpus")
def corpus_list():
    with db_lock, db() as conn:
        rows = conn.execute("SELECT * FROM corpus ORDER BY id DESC").fetchall()
    out = []
    for r in rows:
        item = {k: r[k] for k in r.keys()}
        p = _corpus_dir(r["id"]) / "profile.json"
        item["has_profile"] = p.is_file()
        tdir = _corpus_dir(r["id"]) / "tracks"
        item["tracks"] = len(list(tdir.glob("track-*"))) if tdir.is_dir() else 0
        out.append(item)
    return out


@app.get("/corpus/{cid}")
def corpus_get(cid: int):
    p = _corpus_dir(cid) / "profile.json"
    if not p.is_file():
        raise HTTPException(404, "profile not built yet")
    return json.loads(p.read_text())


@app.get("/corpus/{cid}/tracks")
def corpus_tracks(cid: int):
    """Разбор по трекам корпуса: что увидели в каждом (темп, тональность,
    аккорды, фрагмент текста, ошибки)."""
    tdir = _corpus_dir(cid) / "tracks"
    out = []
    if not tdir.is_dir():
        return out
    for ip in sorted(tdir.glob("track-*/info.json")):
        try:
            info = json.loads(ip.read_text())
        except Exception:  # noqa: BLE001
            continue
        m = info.get("metrics") or {}
        st = info.get("stats") or {}
        out.append({
            "filename": info.get("filename"),
            "created_at": info.get("created_at"),
            "tempo_bpm": m.get("tempo_bpm"),
            "key": next(iter(st.get("keys") or {}), None),
            "top_chords": list(st.get("top_chords") or {})[:6],
            "structure": st.get("structure") or [],
            "lyrics_head": (info.get("lyrics") or "")[:200],
            "abc_error": info.get("abc_error"),
            "lyrics_error": bool(info.get("lyrics_error")),
        })
    return out


# ---------- Карточки голосов (примерочная) ----------

def _voice_dir(vid: int) -> Path:
    return VOICES_DIR / str(vid)


class VoiceIn(BaseModel):
    """Карточка голоса из джобы-прослушивания: ручки примерочной (params,
    JSON-строка) + использованный seed — дескриптор пересчитывается на клиенте."""
    name: str
    job_id: int
    params: str = "{}"
    seed: int = 0


@app.post("/voices")
def voice_create(req: VoiceIn):
    import shutil
    if not req.name.strip():
        raise HTTPException(422, "name is required")
    try:
        parsed = json.loads(req.params or "{}")
        if not isinstance(parsed, dict):
            raise ValueError("not an object")
    except ValueError as e:
        raise HTTPException(422, f"params must be a JSON object: {e}") from None
    with db_lock, db() as conn:
        row = conn.execute("SELECT status FROM jobs WHERE id=?", (req.job_id,)).fetchone()
        if row is None:
            raise HTTPException(404, "job not found")
        if row["status"] != "done":
            raise HTTPException(422, "job is not done")
        job_dir = JOBS_DIR / str(req.job_id)
        if not (job_dir / "audio.flac").is_file():
            raise HTTPException(422, "job has no audio")
        cur = conn.execute(
            "INSERT INTO voices(name,job_id,params,seed,created_at) VALUES(?,?,?,?,?)",
            (req.name.strip(), req.job_id, req.params, req.seed,
             time.strftime("%Y-%m-%dT%H:%M:%S")))
        vid = cur.lastrowid
    d = _voice_dir(vid)
    try:
        d.mkdir(parents=True, exist_ok=True)
        shutil.copy2(job_dir / "audio.flac", d / "audio.flac")
        if (job_dir / "audio.mp3").is_file():
            shutil.copy2(job_dir / "audio.mp3", d / "audio.mp3")
    except OSError:
        # иначе в примерочной остаётся голос без образца
        log.exception("voice #%s from job %s: copy failed, rolled back", vid, req.job_id)
        with db_lock, db() as conn:
            conn.execute("DELETE FROM voices WHERE id=?", (vid,))
        shutil.rmtree(d, ignore_errors=True)
        raise
    log.info("voice #%s saved from job %s", vid, req.job_id)
    return {"id": vid}


@app.get("/voices")
def voices_list():
    with db_lock, db() as conn:
        rows = conn.execute("""
            SELECT v.*, EXISTS(SELECT 1 FROM jobs j WHERE j.id = v.job_id) AS job_alive
            FROM voices v ORDER BY v.id DESC""").fetchall()
    out = []
    for r in rows:
        item = {k: r[k] for k in r.keys()}
        item["job_alive"] = bool(r["job_alive"])  # sqlite EXISTS даёт 0/1, клиент ждёт bool
        item["has_audio"] = (_voice_dir(r["id"]) / "audio.flac").is_file()
        out.append(item)
    return out


@app.delete("/voices/{vid}")
def voice_delete(vid: int):
    import shutil
    with db_lock, db() as conn:
        if conn.execute("SELECT 1 FROM voices WHERE id=?", (vid,)).fetchone() is None:
            raise HTTPException(404, "voice not found")
        conn.execute("DELETE FROM voices WHERE id=?", (vid,))
    shutil.rmtree(_voice_dir(vid), ignore_errors=True)
    log.info("voice #%s deleted", vid)
    return {"deleted": True}


DSP_LABEL_MAX = 300


@app.post("/jobs/{job_id}/dsp")
async def add_dsp_variant(job_id: int, request: Request, label: str = ""):
    """Вариант пост-обработки от приложения (ffmpeg на ПК): тело — flac,
    X-Filename — dsp-<цепочка>.flac. Замеряется и кэшируется рядом.
    label — что сделано («Перегруз голоса · голос»): по имени файла микса
    (overdub-inst-0) этого не понять."""
    fname = request.headers.get("x-filename", "")
    if not DSP_NAME_RE.match(fname):
        raise HTTPException(422, "filename must be dsp-<chain>.flac")
    with db_lock, db() as conn:
        if conn.execute("SELECT 1 FROM jobs WHERE id=?", (job_id,)).fetchone() is None:
            raise HTTPException(404, "job not found")
    if len(label) > DSP_LABEL_MAX:
        raise HTTPException(422, f"label longer than {DSP_LABEL_MAX} chars")
    data = await request.body()
    if not data:
        raise HTTPException(422, "empty body")
    job_dir = JOBS_DIR / str(job_id)
    job_dir.mkdir(parents=True, exist_ok=True)
    target = job_dir / fname
    try:
        metrics = None
        target.write_bytes(data)
        metrics = analyze_file(target)
        (job_dir / f"{fname}.metrics.json").write_text(json.dumps(
            {"file": fname, "created_at": time.strftime("%Y-%m-%dT%H:%M:%S"), "metrics": metrics,
             "label": label}, ensure_ascii=False))
    except Exception as e:  # noqa: BLE001
        target.unlink(missing_ok=True)
        (job_dir / f"{fname}.metrics.json").unlink(missing_ok=True)
        raise HTTPException(422, f"cannot analyze dsp variant: {e}") from e
    return {"file": fname, "created_at": time.strftime("%Y-%m-%dT%H:%M:%S"), "metrics": metrics,
            "label": label}


@app.delete("/jobs/{job_id}/dsp/{fname}")
def dsp_variant_delete(job_id: int, fname: str):
    """Удалить вариант (dsp-*/overdub-*): сам файл и его метрики."""
    if "/" in fname or ".." in fname or not DSP_NAME_RE.match(fname):
        raise HTTPException(422, "bad variant name")
    d = JOBS_DIR / str(job_id)
    f = d / fname
    if not f.is_file():
        raise HTTPException(404, "variant not found")
    f.unlink()
    for side in (f"{fname}.metrics.json", f"{fname}.peaks.json",
                 f"{fname}.spectrum.json", f"{fname}.spectrum.png"):
        p = d / side
        if p.is_file():
            p.unlink()
    log.info("job %s: variant %s deleted", job_id, fname)
    return {"deleted": True}


@app.get("/jobs/{job_id}/dsp")
def dsp_variants(job_id: int):
    job_dir = JOBS_DIR / str(job_id)
    if not job_dir.is_dir():
        return []
    out = []
    # dsp-*.flac — варианты эффектов; overdub-*.flac — вклейки партий
    # (в т.ч. инструменты из приёмов) — единый список «вариантов трека»
    files = sorted(list(job_dir.glob("dsp-*.flac")) + list(job_dir.glob("overdub-*.flac")),
                   key=lambda p: p.stat().st_mtime, reverse=True)
    for f in files:
        item = {"file": f.name,
                "created_at": time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime(f.stat().st_mtime)),
                "metrics": None, "label": ""}
        mp = job_dir / f"{f.name}.metrics.json"
        if mp.is_file():
            try:
                side = json.loads(mp.read_text())
                item["metrics"], item["label"] = side.get("metrics"), side.get("label", "")
            except Exception:  # noqa: BLE001
                pass
        out.append(item)
    return out


@app.get("/listen/{job_id}")
def listen(job_id: int, f: str | None = None):
    with db_lock, db() as conn:
        row = conn.execute("SELECT id,title,status,audio_file FROM jobs WHERE id=?", (job_id,)).fetchone()
    if row is None or row["status"] != "done":
        raise HTTPException(404, "job not found")
    fname = row["audio_file"]
    if f is not None:
        if "/" in f or ".." in f or not DSP_NAME_RE.match(f):
            raise HTTPException(400, "bad filename")
        fname = f
    src = f"/audio/{job_id}/{fname}"
    title = row["title"] or f"Job #{job_id}"
    if fname != row["audio_file"]:
        title = f"{title} · {fname}"
    return HTMLResponse(f"""<!doctype html>
<html><head><meta charset="utf-8"><title>{title} — Yue Studio</title>
<style>
body {{ background:#101014;color:#e8e8ee;font:15px system-ui,sans-serif;
       display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0 }}
.card {{ background:#18181f;border:1px solid #2a2a35;border-radius:12px;padding:28px 32px;max-width:640px;width:92% }}
h1 {{ font-size:18px;margin:0 0 4px;font-weight:600 }}
small {{ color:#8b8b9a }}
audio {{ width:100%;margin-top:18px }}
</style></head>
<body><div class="card">
<h1>{title}</h1><small>Yue Studio — job #{job_id}</small>
<audio controls autoplay src="{src}"></audio>
</div></body></html>""")


@app.get("/audio/{job_id}/{fname}")
def audio(job_id: int, fname: str):
    if "/" in fname or ".." in fname:
        raise HTTPException(400, "bad filename")
    path = JOBS_DIR / str(job_id) / fname
    if not path.is_file():
        raise HTTPException(404, "not found")
    return FileResponse(path)
