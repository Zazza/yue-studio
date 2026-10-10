"""Yue Studio worker — API вокруг резидентной YuE2Pipeline (:8091, 0.0.0.0).

Держит модель загруженной между запросами; очередь задач живёт здесь же
(SQLite), так что закрытие desktop-приложения не прерывает генерации.

Эндпоинты:
  GET  /health           — статус, загружена ли модель
  GET  /stats            — сводка для статус-бара: VRAM, очередь, текущая джоба, очередь к GPU
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
import contextlib
import json
import shutil
from collections.abc import Iterator
from contextlib import contextmanager
import logging
import os
import random
import re
import sqlite3
import sys
import tempfile
import threading
import time
import urllib.parse
import urllib.request

import arc
import llm
import media
import instruments as fx_instruments
import presets as sound_presets
import waveform
from pathlib import Path

from fastapi import FastAPI, HTTPException, Query, Request
from fastapi.responses import FileResponse, HTMLResponse
from starlette.concurrency import run_in_threadpool
from pydantic import BaseModel, Field, field_validator

from abcparse import parse_abc
from plancheck import plan_diff
from dsp import analyze_file, beat_grid, vocal_activity
from sheetsage import transcribe as ss_transcribe
from stems import (DETAIL_STEMS, DRUM_PARTS, MAIN_STEMS, MODEL_DEMUCS, MODEL_ROFORMER, roformer_available,
                   roformer_enabled, separate as demucs_separate)
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
_gpu_lock = threading.RLock()   # очередь к GPU: рендер/транскрипция/стемы/whisper не работают параллельно
_gpu_waiters = 0                # сколько потоков стоит в очереди к GPU (для /stats)
_gpu_waiters_lock = threading.Lock()
_load_lock = threading.Lock()
_load_error: str | None = None


@contextmanager
def gpu_queue(section: str = "gpu"):
    """Очередь к единственному GPU: всё тяжёлое (YuE2, demucs, SheetSage,
    whisper-сабпроцесс) проходит через один лок, иначе параллельные CUDA-контексты
    ломают друг другу graph capture (cudaErrorStreamCaptureInvalidated).
    Порядок захвата везде _pipe_lock → _gpu_lock, обратного нет — дедлока не будет."""
    global _gpu_waiters
    t0 = time.monotonic()
    with _gpu_waiters_lock:
        _gpu_waiters += 1
    try:
        _gpu_lock.acquire()
    finally:
        with _gpu_waiters_lock:
            _gpu_waiters -= 1
    try:
        wait = time.monotonic() - t0
        if wait > 1.0:
            log.info("gpu queue: %s ждал(а) лок %.1f с", section, wait)
        yield
    finally:
        _gpu_lock.release()

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
        # пресеты звука у трека: [{id, status, child_id, error}] — что применить после готовности
        if "sound_presets" not in cols:
            conn.execute("ALTER TABLE jobs ADD COLUMN sound_presets TEXT DEFAULT ''")
        conn.execute("""
        CREATE TABLE IF NOT EXISTS sound_presets (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            slug TEXT UNIQUE,
            name TEXT NOT NULL,
            note TEXT NOT NULL DEFAULT '',
            specs TEXT NOT NULL DEFAULT '[]',
            final TEXT NOT NULL DEFAULT '[]',
            reference_job_id INTEGER,
            builtin INTEGER NOT NULL DEFAULT 0,
            created_at TEXT NOT NULL
        );
        """)
        cols = [r[1] for r in conn.execute("PRAGMA table_info(sound_presets)")]
        if "target_lufs" not in cols:
            conn.execute("ALTER TABLE sound_presets ADD COLUMN target_lufs REAL")
        if "master" not in cols:   # мастер на воркере (этап 6): цепочка движка на весь микс
            conn.execute("ALTER TABLE sound_presets ADD COLUMN master TEXT NOT NULL DEFAULT '[]'")
        if "parts" not in cols:    # партии-рецепты (этап 8): синт по аккордам, перкуссия по сетке
            conn.execute("ALTER TABLE sound_presets ADD COLUMN parts TEXT NOT NULL DEFAULT '[]'")
        if "family" not in cols:   # семья жанровых пресетов (этап 8б): Рок, Тяжёлое, Электроника, Поп и другое
            conn.execute("ALTER TABLE sound_presets ADD COLUMN family TEXT NOT NULL DEFAULT ''")
        # встроенные — upsert по slug: рецепт из кода (правка встроенного доходит до старой базы), id
        # прежний; пользователь их не меняет и не удаляет, свои пресеты (slug NULL) не трогаются
        for b in sound_presets.BUILTIN:
            conn.execute(
                "INSERT INTO sound_presets(slug,name,note,specs,final,reference_job_id,target_lufs,master,parts,"
                "family,builtin,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,1,?) ON CONFLICT(slug) DO UPDATE SET "
                "name=excluded.name, note=excluded.note, specs=excluded.specs, final=excluded.final, "
                "reference_job_id=excluded.reference_job_id, target_lufs=excluded.target_lufs, "
                "master=excluded.master, parts=excluded.parts, family=excluded.family, builtin=1",
                (b["slug"], b["name"], b["note"], json.dumps(b["specs"]), json.dumps(b["final"]),
                 b["reference_job_id"], b.get("target_lufs"), json.dumps(b.get("master", [])),
                 json.dumps(b.get("parts", [])), b.get("family", ""), "2026-10-08T00:00:00"))
        # свои инструменты страницы «Инструменты» (этап 13в): цепочка движка под своим именем
        conn.execute("""
        CREATE TABLE IF NOT EXISTS fx_instruments (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            base TEXT NOT NULL DEFAULT '',
            grp TEXT NOT NULL DEFAULT '',
            stems TEXT NOT NULL DEFAULT '[]',
            chain TEXT NOT NULL DEFAULT '[]',
            extra TEXT NOT NULL DEFAULT '{}',
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
        with _pipe_lock, gpu_queue(f"render {job_id}"):
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
                    if row["arc"]:
                        # драматургия поверх прикреплённого плана: темп по секциям,
                        # burst — ещё и октава вокала в финале; файл пользователя не трогаем
                        request["style"] = arc.style_with_arc(request["style"], row["arc"])
                        request["abc"] = arc.apply_arc(request["abc"], row["arc"])
                        log.info("job %s: arc=%s applied to custom abc", job_id, row["arc"])
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


def _separate(job_id: int, audio: Path, jdir: Path, model: str | None = None) -> dict:
    """Дорожки (модель — model или настройка stems_model) + проверка голоса в
    треке «без голоса» (vocal_leak)."""
    pref = model or stems_pref()
    with gpu_queue(f"stems {job_id}"):
        # по умолчанию — прежний вызов separate(audio, dir): контракт не меняется
        result = demucs_separate(audio, jdir) if pref == MODEL_DEMUCS else demucs_separate(audio, jdir, prefer=pref)
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
        with gpu_queue(f"voice {job_id}"):
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
    # пресеты звука: приложение применит их после готовности трека (до 3, не у черновика)
    sound_preset_ids: list[int] = Field(default_factory=list)

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
    stems_model: str | None = None   # разделение: «htdemucs» (быстро) | «roformer» (чище)
    fx_engine: bool | None = None    # звуковой движок (POST /jobs/{id}/fx); YUE_FX_ENGINE=0 — выкл всегда


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


STEMS_CHOICES = (MODEL_DEMUCS, "roformer")


def stems_pref() -> str:
    """Чем делать дорожки по умолчанию: htdemucs (быстро) или roformer (чище, ~4,5× дольше)."""
    v = _load_settings().get("stems_model", MODEL_DEMUCS)
    return v if v in STEMS_CHOICES else MODEL_DEMUCS


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
        # выбор пользователя и чем дорожки будут делаться на деле (RoFormer — если выбран и установлен)
        "stems_pref": stems_pref(),
        "stems_model": MODEL_ROFORMER if roformer_enabled(stems_pref()) else MODEL_DEMUCS,
        "roformer_available": roformer_available(),
        "fx_engine": fx_engine_enabled(),
        "fx_preview": True,   # воркер умеет превью движка (страница «Инструменты»); старый — нет поля
        "fx_phrases": True,   # фразы по кругу для страницы «Инструменты» (/fx/phrases)
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
    if req.stems_model is not None:
        if req.stems_model not in STEMS_CHOICES:
            raise HTTPException(422, f"stems_model must be one of {', '.join(STEMS_CHOICES)}")
        st = _load_settings()
        st["stems_model"] = req.stems_model
        _save_settings(st)
    if req.fx_engine is not None:
        st = _load_settings()
        st["fx_engine"] = bool(req.fx_engine)
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


@app.get("/stats")
def stats():
    """Сводка для статус-бара приложения: VRAM, очередь джоб, текущая джоба,
    ожидающие в очереди к GPU. Лёгкий — опрашивается раз в несколько секунд."""
    vram_total = vram_used = None
    try:
        import torch
        if torch.cuda.is_available():
            free, total = torch.cuda.mem_get_info()
            vram_total = round(total / 1024 / 1024)
            vram_used = round((total - free) / 1024 / 1024)
    except Exception:  # noqa: BLE001 - CPU-воркер или CUDA недоступна: VRAM просто нет
        pass
    with db_lock, db() as conn:
        counts = dict(conn.execute(
            "SELECT status, COUNT(*) FROM jobs GROUP BY status").fetchall())
    running = None
    with _state_lock:
        progress_snapshot = dict(_progress)
    for job_id, p in progress_snapshot.items():
        row = _job_row(job_id)
        if row is None or row["status"] != "running":
            continue
        running = {"job_id": job_id, "title": row["title"],
                   "stage": p.get("stage", ""),
                   "progress_pct": p.get("progress_pct"),
                   "tok_per_s": p.get("tok_per_s")}
        break
    return {"model_loaded": _pipe is not None,
            "vram_total": vram_total, "vram_used": vram_used,
            "queue": {"queued": counts.get("queued", 0), "running": counts.get("running", 0)},
            "running": running,
            "gpu_waiting": _gpu_waiters}


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
        existing = {r["id"] for r in conn.execute("SELECT id FROM sound_presets")}
        try:
            preset_ids = sound_presets.check_job_ids(req.sound_preset_ids, existing, req.draft)
        except sound_presets.PresetError as e:
            raise HTTPException(422, str(e)) from e
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
        if preset_ids:
            conn.execute("UPDATE jobs SET sound_presets=? WHERE id=?", (json.dumps(
                [{"id": i, "status": "pending", "child_id": 0, "error": ""} for i in preset_ids]), job_id))
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


# ---------- пресеты звука ----------

def _preset_dict(row) -> dict:
    return {"id": row["id"], "slug": row["slug"] or "", "name": row["name"], "note": row["note"],
            "specs": json.loads(row["specs"] or "[]"), "final": json.loads(row["final"] or "[]"),
            "reference_job_id": row["reference_job_id"], "target_lufs": row["target_lufs"],
            "master": json.loads(row["master"] or "[]"), "parts": json.loads(row["parts"] or "[]"),
            "family": row["family"] or "", "builtin": bool(row["builtin"])}


def _parse_engine(chain):
    import fx_engine
    return fx_engine.parse_chain(chain)


def _preset_body(body) -> dict:
    if not isinstance(body, dict):
        raise HTTPException(422, "пресет — объект {name, note, specs, final, reference_job_id, target_lufs, master}")
    try:
        return sound_presets.validate(body, _parse_engine)
    except sound_presets.PresetError as e:
        raise HTTPException(422, str(e)) from e


@app.get("/sound-presets")
def list_sound_presets():
    """Пресеты звука: встроенные первыми, затем свои по порядку создания."""
    with db_lock, db() as conn:
        rows = conn.execute("SELECT * FROM sound_presets ORDER BY builtin DESC, id").fetchall()
    return [_preset_dict(r) for r in rows]


@app.post("/sound-presets")
async def create_sound_preset(request: Request):
    p = _preset_body(await request.json())
    with db_lock, db() as conn:
        cur = conn.execute(
            "INSERT INTO sound_presets(name,note,specs,final,reference_job_id,target_lufs,master,parts,created_at) "
            "VALUES(?,?,?,?,?,?,?,?,?)",
            (p["name"], p["note"], json.dumps(p["specs"]), json.dumps(p["final"]), p["reference_job_id"],
             p["target_lufs"], json.dumps(p["master"]), json.dumps(p["parts"]), time.strftime("%Y-%m-%dT%H:%M:%S")))
        row = conn.execute("SELECT * FROM sound_presets WHERE id=?", (cur.lastrowid,)).fetchone()
    return _preset_dict(row)


def _own_preset(conn, preset_id: int):
    row = conn.execute("SELECT * FROM sound_presets WHERE id=?", (preset_id,)).fetchone()
    if row is None:
        raise HTTPException(404, "preset not found")
    if row["builtin"]:
        raise HTTPException(409, "встроенный пресет не меняется и не удаляется — сохраните свой")
    return row


@app.put("/sound-presets/{preset_id}")
async def update_sound_preset(preset_id: int, request: Request):
    body = await request.json()
    with db_lock, db() as conn:
        _own_preset(conn, preset_id)
    p = _preset_body(body)
    with db_lock, db() as conn:
        _own_preset(conn, preset_id)
        conn.execute("UPDATE sound_presets SET name=?, note=?, specs=?, final=?, reference_job_id=?, target_lufs=?, "
                     "master=?, parts=? WHERE id=?",
                     (p["name"], p["note"], json.dumps(p["specs"]), json.dumps(p["final"]),
                      p["reference_job_id"], p["target_lufs"], json.dumps(p["master"]), json.dumps(p["parts"]),
                      preset_id))
        row = conn.execute("SELECT * FROM sound_presets WHERE id=?", (preset_id,)).fetchone()
    return _preset_dict(row)


@app.delete("/sound-presets/{preset_id}")
def delete_sound_preset(preset_id: int):
    with db_lock, db() as conn:
        _own_preset(conn, preset_id)
        conn.execute("DELETE FROM sound_presets WHERE id=?", (preset_id,))
    return {"deleted": True}


class PresetStateIn(BaseModel):
    status: str = Field(pattern="^(pending|running|done|error)$")
    child_id: int | None = None
    error: str | None = None


@app.post("/jobs/{job_id}/sound-presets/{preset_id}/state")
def job_preset_state(job_id: int, preset_id: int, req: PresetStateIn):
    """Статус пресета у трека: pending→running — захват (уже не pending → 409), running→done|error,
    error|running→pending — повтор. Атомарно под db_lock: два приложения не применят пресет дважды."""
    with db_lock, db() as conn:
        row = conn.execute("SELECT sound_presets FROM jobs WHERE id=?", (job_id,)).fetchone()
        if row is None:
            raise HTTPException(404, "job not found")
        try:
            items = sound_presets.apply_state(_job_presets(row["sound_presets"]), preset_id,
                                              req.status, req.child_id, req.error)
        except KeyError as e:
            raise HTTPException(404, "у трека нет этого пресета") from e
        except sound_presets.TransitionError as e:
            raise HTTPException(409, str(e)) from e
        except sound_presets.PresetError as e:
            raise HTTPException(422, str(e)) from e
        conn.execute("UPDATE jobs SET sound_presets=? WHERE id=?", (json.dumps(items), job_id))
    return items


# ---------- сетка аккордов (синт по аккордам) ----------

CHORD_HOP = 2048   # шаг хромы, сэмплов


@app.get("/jobs/{job_id}/chord_grid")
def job_chord_grid(job_id: int):
    """Аккорды и секции плана трека → такты звука: доли по барабанам (нет — по миксу), сдвиг плана — по хроме
    гармонии (бас+прочее, нет — микс). Синт по аккордам ложится в такт звука, а не во время плана."""
    import librosa
    import numpy as np
    import soundfile as sf
    import chordgrid
    row = _job_row(job_id)
    if row is None or not row["audio_file"]:
        raise HTTPException(404, "job or audio not found")
    jdir = JOBS_DIR / str(job_id)
    abc = jdir / (row["abc_file"] or "score.abc")
    if not abc.is_file():
        raise HTTPException(404, "у трека нет плана (score.abc) — сделайте транскрипцию")
    plan = chordgrid.plan_bars(abc.read_text(encoding="utf-8"))
    if not plan:
        raise HTTPException(404, "в плане нет тактов")

    def mono(path):
        y, s_r = sf.read(str(path), always_2d=True, dtype="float32")
        return y.mean(axis=1), s_r
    mix, sr = mono(jdir / row["audio_file"])
    n = len(mix)
    drums = jdir / "stem-drums.flac"
    beat_src = mono(drums)[0][:n] if drums.is_file() else mix
    harm = mix
    if (jdir / "stem-bass.flac").is_file() and (jdir / "stem-other.flac").is_file():
        b, o = mono(jdir / "stem-bass.flac")[0], mono(jdir / "stem-other.flac")[0]
        m = min(len(b), len(o), n)
        harm = b[:m] + o[:m]
    _, beats = librosa.beat.beat_track(y=beat_src, sr=sr, units="samples", tightness=200)
    if len(beats) < 4:
        raise HTTPException(422, "в звуке не найдено долей — сетку тактов не построить")
    bl = float(np.median(np.diff(beats)))
    grid = np.concatenate([np.arange(beats[0] - bl, -1, -bl)[::-1], beats,
                           np.arange(beats[-1] + bl, n, bl)]).astype(int)
    chroma = librosa.feature.chroma_cqt(y=harm, sr=sr, hop_length=CHORD_HOP)
    ends = list(grid[1:]) + [n]
    bc = np.array([chroma[:, a // CHORD_HOP:max(a // CHORD_HOP + 1, e // CHORD_HOP)].mean(axis=1)
                   for a, e in zip(grid, ends, strict=True)])
    shift, phase = chordgrid.best_shift([b["chord"] for b in plan], bc)
    starts = list(grid[phase::4])
    bars = []
    for j, a in enumerate(starts):
        e = starts[j + 1] if j + 1 < len(starts) else a + 4 * bl   # последний — полный такт (за концом не звучит)
        k = j - shift
        src = plan[k] if 0 <= k < len(plan) else {"chord": None, "section": ""}
        bars.append({"start": round(a / sr, 4), "end": round(e / sr, 4), "chord": src["chord"],
                     "section": src["section"]})
    return {"bpm": round(60.0 * sr / bl, 2), "bars": bars}


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
    with _pipe_lock, gpu_queue("plan"):
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
    d["sound_presets"] = _job_presets(d.get("sound_presets"))
    # готовые миксы трека (вклейки, эффекты на дорожки): по ним список треков
    # показывает «версии» и у трека без дочерних треков
    d["mixes"] = sum(1 for _ in (JOBS_DIR / str(row["id"])).glob("overdub-inst-*.flac"))
    if d.get("status") == "running":
        with _state_lock:
            d.update(_progress.get(row["id"], {}))
    return d


def _job_presets(raw) -> list:
    """Пресеты звука трека из колонки: пусто или битое — нет пресетов."""
    try:
        v = json.loads(raw or "[]")
    except ValueError:
        return []
    return v if isinstance(v, list) else []


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
# вход движка-варианта (/fx file): эффекты, пресеты и миксы студии — не превью и не дорожки
FX_FILE_RE = re.compile(r"^(dsp|overdub)-[a-z0-9.-]+\.flac$")


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
        # whisper-сабпроцесс тоже занимает GPU — через общую очередь, иначе
        # ломает CUDA-контекст идущего рендера
        with gpu_queue("whisper"):
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
        # whisper занимает GPU — в тредпул: ждёт очередь к GPU, не блокируя event loop
        text, err = await run_in_threadpool(_whisper_lyrics, src, language)
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

def _ss_transcribe_queued(src: Path, dst: Path) -> dict:
    """SheetSage-транскрипция через общую очередь к GPU."""
    with gpu_queue(f"transcribe {src.name}"):
        return ss_transcribe(src, dst)


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
        # GPU-транскрипция — в тредпуле через общую очередь к GPU:
        # ждёт рендер, а не блокирует event loop воркера
        result = await run_in_threadpool(_ss_transcribe_queued, src, tdir)
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
    with _pipe_lock, gpu_queue(f"preview {job_id}"):
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
            result = await run_in_threadpool(_ss_transcribe_queued, audio, jdir)
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


@app.post("/jobs/{job_id}/transcribe")
async def job_transcribe(job_id: int):
    """Партитура для готового трека без неё (импорт без транскрипции или с упавшей,
    DSP-вариант): SheetSage2 по audio_file → score.abc, abc_file джобы.
    Готовый план не перезаписывается — 409."""
    row = _job_row(job_id)
    if row is None:
        raise HTTPException(404, "job not found")
    if row["status"] != "done" or not row["audio_file"]:
        raise HTTPException(409, "job not done (no audio)")
    if row["abc_file"]:
        raise HTTPException(409, "job already has a score")
    jdir = JOBS_DIR / str(job_id)
    try:
        result = await run_in_threadpool(_ss_transcribe_queued, jdir / row["audio_file"], jdir)
    except Exception as e:  # noqa: BLE001
        log.exception("job transcribe failed")
        raise HTTPException(500, f"transcribe failed: {e}") from e
    abc = result.get("abc", "")
    if not abc.strip():
        raise HTTPException(500, "transcribe returned an empty score")
    (jdir / "score.abc").write_text(abc, encoding="utf-8")
    (jdir / "score.json").unlink(missing_ok=True)   # кэш ролла от прошлой попытки
    with db_lock, db() as conn:
        conn.execute("UPDATE jobs SET abc_file=? WHERE id=?", ("score.abc", job_id))
    return {"id": job_id, "abc_file": "score.abc"}


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
def job_stems(job_id: int, model: str | None = None):
    """Разделить трек на дорожки. ?model=roformer|htdemucs — разово этой моделью,
    без параметра — по настройке stems_model."""
    if model is not None and model not in STEMS_CHOICES:
        raise HTTPException(422, f"model must be one of {', '.join(STEMS_CHOICES)}")
    row = _job_row(job_id)
    if row is None or not row["audio_file"]:
        raise HTTPException(404, "job or audio not found")
    job_dir = JOBS_DIR / str(job_id)
    try:
        result = _separate(job_id, job_dir / row["audio_file"], job_dir, model)
    except Exception as e:  # noqa: BLE001
        log.exception("stems failed")
        raise HTTPException(500, f"separation failed: {friendly_error(e)}") from e
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
            raise HTTPException(500, f"separation failed: {friendly_error(e)}") from e
        stems = main_stems()
        if not stems:
            raise HTTPException(500, "no stems after separation")
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
        t = await run_in_threadpool(_ss_transcribe_queued, src, track_dir / "ss2")
        info["abc"] = t["abc"]
        info["stats"] = t["stats"]
    except Exception as e:  # noqa: BLE001
        info["abc_error"] = str(e)
    lyrics_text, lyrics_err = await run_in_threadpool(_whisper_lyrics, src)
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


# ---------- Звуковой движок: цепочка обработки дорожки (fx_engine) ----------

FX_KINDS = {"amp": ("amps", ".nam"), "ir": ("irs", ".wav")}
FX_UPLOAD_MAX = 50 * 1024 * 1024
FX_XFADE_S = 0.01        # кроссфейд на границах окна
FX_PREVIEW_TAIL_S = 3.0  # превью: хвост реверба/дилея после окна
FX_SAMPLER_TAIL_S = 1.0  # превью: хвост сэмплов sampler/bass после окна — не короче (бочка/малый ~0,3–0,8 с)
FX_SAMPLER_TAIL_MAX_S = 4.0  # …и не длиннее: по самому длинному сэмплу цепочки (тарелки звенят 8–12 с)
FX_PREVIEW_KEEP = 8      # превью движка на джобу (старые — временные файлы, удаляются)
FX_FADE_MAX_S = 0.5      # край окна превью — не длиннее
FX_CLIP_FULL_SCALE = 0.9999  # превью из кэша: пик на полной шкале = при записи был перегруз
FX_SOURCES = ("mix",) + MAIN_STEMS + DETAIL_STEMS + DRUM_PARTS
# имя захвата/IR: буквы (в т.ч. кириллица), цифры, пробел и « .,()+-_»; без путей
FX_NAME_RE = re.compile(r"^[\w][\w .,()+\-]{0,150}$")
# сторонние пакеты NAM (neural-amp-modeler 0.12.2 + зависимости) — отдельной папкой
NAM_DEPS = os.environ.get("YUE_NAM_DEPS", "")


def _fx_dir() -> Path:
    """Захваты и IR: <data>/fx/{amps,irs} (от DATA_DIR в момент вызова)."""
    return DATA_DIR / "fx"


def fx_engine_enabled() -> bool:
    """Движок включён: YUE_FX_ENGINE=0 выключает принудительно (откат), иначе — настройка."""
    if os.environ.get("YUE_FX_ENGINE", "").strip() == "0":
        return False
    return bool(_load_settings().get("fx_engine", True))


def _fx_asset_path(kind: str, name: str) -> Path:
    """Файл захвата/IR по имени (расширение можно не писать). Неверное имя — KeyError."""
    sub, ext = FX_KINDS[kind]
    if name.lower().endswith(ext):
        name = name[:-len(ext)] + ext  # Room.WAV → Room.wav: список ищет по строчному расширению
    else:
        name += ext
    if "/" in name or "\\" in name or ".." in name or not FX_NAME_RE.match(name):
        raise KeyError(name)
    return _fx_dir() / sub / name


def _nam_latency_path(p: Path) -> Path:
    return p.with_name(p.name + ".latency.json")


def _nam_version(p: Path) -> list:
    st = p.stat()
    return [st.st_mtime_ns, st.st_size]


def _nam_cached_latency(p: Path) -> int | None:
    """Замер задержки захвата — только если он сделан для этой версии файла (mtime_ns, размер):
    файл заменили на диске мимо загрузки — замер заново."""
    try:
        d = json.loads(_nam_latency_path(p).read_text())
        if d.get("version") != _nam_version(p):
            return None
        return int(d["latency"])
    except (OSError, ValueError, KeyError, TypeError, AttributeError):
        return None  # нет замера или старый формат — load_nam замерит щелчком


class _FxResources:
    """Захваты NAM и IR из <data>/fx для fx_engine.process. Модели, загруженные за проход,
    выгружаются в close() — видеопамять делится с YuE2."""

    def __init__(self):
        self._amps: dict = {}

    def ir(self, name: str):
        p = _fx_asset_path("ir", name)
        if not p.is_file():
            raise KeyError(name)
        import soundfile as sf
        data, sr = sf.read(str(p), dtype="float32")
        return data, sr

    def amp(self, name: str):
        p = _fx_asset_path("amp", name)
        if not p.is_file():
            raise KeyError(name)
        if p.name not in self._amps:
            if NAM_DEPS and NAM_DEPS not in sys.path:
                sys.path.insert(0, NAM_DEPS)
            import fx_nam
            cached = _nam_cached_latency(p)
            m = fx_nam.load_nam(p, cached)
            if cached is None:
                _nam_latency_path(p).write_text(json.dumps({"latency": m.latency, "version": _nam_version(p)}))
            self._amps[p.name] = m
        return self._amps[p.name]

    def kit(self, name: str):
        """Сэмплы набора `<набор>/<часть>` и их частота (sampler, bass). Предел длины — max_s из каталога
        FX_KITS (у баса); у прочих наборов — целиком (тарелки звучат дольше 6 с)."""
        import soundfile as sf
        files = _kit_files(_kit_path(name))
        if not files:
            raise KeyError(name)
        max_s = FX_KITS.get(name.partition("/")[0], {}).get("max_s")
        out, sr = [], None
        for f in files:
            x, s = sf.read(str(f), dtype="float32",
                           frames=int(sf.info(str(f)).samplerate * max_s) if max_s else -1)
            if sr is None:
                sr = s
            elif s != sr:                # разная частота в одном наборе — пересчитать к первой
                from fractions import Fraction
                from scipy import signal
                fr = Fraction(sr, s)
                x = signal.resample_poly(x, fr.numerator, fr.denominator, axis=0).astype("float32")
            out.append(x)
        return out, sr

    def kit_names(self, name: str) -> list:
        """Имена сэмплов набора без расширения — в том же порядке, что kit() (высота из имени у синтезированных)."""
        return [f.stem for f in _kit_files(_kit_path(name))]

    def close(self) -> None:
        for m in self._amps.values():
            m.close()
        self._amps.clear()


def fx_resources() -> _FxResources:
    return _FxResources()


class FxIn(BaseModel):
    model_config = {"populate_by_name": True}
    source: str = "mix"
    chain: list
    from_: float | None = Field(None, alias="from")
    to: float | None = None
    output: str = "mix"
    label: str = ""
    preview: bool = False   # прослушать кусок (страница «Инструменты»): preview-fx-*.flac, не вариант
    # превью: вход окна с линейными краями, с (нарастание с from, спад после to) — пересборка
    # студии кладёт кусок в трек вместо исходной дорожки с теми же фейдами, без щелчка
    fade: float = 0.0
    # превью: файл от начала трека (до from — тишина) — пересборка студии кладёт его без
    # задержки (adelay ffmpeg ошибается на сэмпл), «на место» тем же отсчётом, что дорожку
    pad: bool = False
    # добавление (синт-партия): в превью «в миксе» — трек + обработанное, а не замена дорожки
    add: bool = False
    # вход — вариант трека (микс студии, файл пресета), а не исходный звук; только source mix
    file: str = ""
    # результат — в тот же вариант file (мастер на миксе студии: имя микса не меняется); не превью
    in_place: bool = False


def _fx_window_weights(n: int, sr: int, frm: float | None, to: float | None):
    """Вес обработанного звука по сэмплам: 1 в окне, 0 вне, линейный кроссфейд FX_XFADE_S
    внутри окна на краях. Без окна — None (везде 1)."""
    import numpy as np
    if frm is None and to is None:
        return None
    a = int(round((frm or 0.0) * sr))
    b = int(round(to * sr)) if to is not None else n
    w = np.zeros(n)
    w[a:b] = 1.0
    xf = min(int(FX_XFADE_S * sr), (b - a) // 2)
    if xf > 0:
        ramp = np.linspace(0.0, 1.0, xf + 2)[1:-1]
        if a > 0:
            w[a:a + xf] = ramp
        if b < n:
            w[b - xf:b] = ramp[::-1]
    return w


def _fx_render(job_id: int, req: FxIn) -> dict:
    import hashlib
    import uuid
    import numpy as np
    import soundfile as sf
    import fx_engine
    try:
        chain = fx_engine.parse_chain(req.chain)
    except fx_engine.ChainError as e:
        raise HTTPException(422, str(e)) from e
    if req.source not in FX_SOURCES:
        raise HTTPException(422, f"source must be one of {', '.join(FX_SOURCES)}")
    if req.output not in ("mix", "solo"):
        raise HTTPException(422, "output must be mix or solo")
    if len(req.label) > DSP_LABEL_MAX:
        raise HTTPException(422, f"label longer than {DSP_LABEL_MAX} chars")
    if req.file and (not FX_FILE_RE.match(req.file) or req.source != "mix"):
        raise HTTPException(422, "file — вариант трека dsp-*.flac или overdub-*.flac, только с source mix")
    if req.in_place and (not req.file or req.preview):
        raise HTTPException(422, "in_place — только с file и без preview")
    row = _job_row(job_id)
    if row is None or not row["audio_file"]:
        raise HTTPException(404, "job or audio not found")
    jdir = JOBS_DIR / str(job_id)
    src = jdir / (req.file or row["audio_file"])
    if req.file and not src.is_file():
        raise HTTPException(404, f"у трека нет варианта {req.file}")
    info = sf.info(str(src))
    sr, n = info.samplerate, info.frames
    dur = n / sr
    frm, to = req.from_, req.to
    if frm is not None and not 0 <= frm < dur:
        raise HTTPException(422, f"from must be within 0…{dur:.2f} s")
    if to is not None and not 0 < to <= dur + 1e-6:
        raise HTTPException(422, f"to must be within 0…{dur:.2f} s")
    if frm is not None and to is not None and frm >= to:
        raise HTTPException(422, "from must be less than to")

    if req.preview and (frm is None or to is None):
        raise HTTPException(422, "preview needs from and to")
    if req.add and not req.preview:
        # вариант на весь трек «добавлением» не делается: без превью трек в окне заменился бы обработанным
        raise HTTPException(422, "add — только у превью (preview: true)")
    if not 0 <= req.fade <= FX_FADE_MAX_S:
        raise HTTPException(422, f"fade must be within 0…{FX_FADE_MAX_S} s")

    track, _ = sf.read(str(src), always_2d=True, dtype="float64")
    if req.source == "mix":
        part = track
    else:
        try:
            _ensure_stems(job_id)
        except Exception as e:  # noqa: BLE001
            raise HTTPException(500, f"separation failed: {friendly_error(e)}") from e
        sp = jdir / f"stem-{req.source}.flac"
        if not sp.is_file():
            hint = " (части барабанов — только при разделении RoFormer)" if req.source in DRUM_PARTS else ""
            raise HTTPException(422, f"у трека нет дорожки {req.source}{hint}")
        part, _ = sf.read(str(sp), always_2d=True, dtype="float64")
        part = part[:n] if len(part) >= n else np.pad(part, ((0, n - len(part)), (0, 0)))
        if part.shape[1] != track.shape[1]:
            part = np.repeat(part.mean(axis=1, keepdims=True), track.shape[1], axis=1)

    if req.preview:
        used = [src] if req.source == "mix" else [src, jdir / f"stem-{req.source}.flac"]
        # ноты синта в запросе — от начала трека, превью считает кусок с from: сдвинуть на окно
        full = None
        if req.source == "mix" and any(b["type"] == "limiter" for b in chain):
            # только на весь микс (мастер): на дорожке в окне кусок вклеивается с краями — там прежний путь
            # громкость к цели меряется по всему входу: превью — отрезок полного рендера, а не окно отдельно;
            # считается только при промахе кэша превью (полный трек — секунды)
            full = lambda: _fx_process(job_id, part.astype(np.float32), sr,  # noqa: E731
                                       _fx_window(synth_window, chain, 0.0, n / sr, part, sr)).astype(np.float64)
        # цепочка окна (служебные поля, уровень партии по всему треку — секунды) — только при промахе кэша превью;
        # ключ кэша — от присланной цепочки и окна; опора уровня партии — сам вход (трек при source mix)
        windowed = lambda: _fx_window(synth_window, chain, frm, to - frm + req.fade, part, sr)  # noqa: E731
        return _fx_preview(job_id, jdir, req, chain, windowed, track, part, sr, used, full)

    # синт на весь трек — тот же общий уровень партии, что у превью (служебные значения — только от воркера)
    chain = _fx_window(synth_window, chain, 0.0, n / sr, part, sr)
    wet = _fx_process(job_id, part.astype(np.float32), sr, chain).astype(np.float64)

    # в треке меняется только разница «обработанная − исходная»: вне окна трек побитно тот же
    w = _fx_window_weights(n, sr, frm, to)
    delta = wet - part if w is None else (wet - part) * w[:, None]
    if req.output == "solo" and req.source != "mix":
        out = part + delta
    else:
        out = track + delta

    key = {"chain": chain, "from": frm, "to": to, "output": req.output}
    if req.file:
        key["file"] = req.file   # та же цепочка на треке и на варианте — разные результаты
    key = json.dumps(key, sort_keys=True)
    fname = f"dsp-fx-{req.source}-{hashlib.sha1(key.encode()).hexdigest()[:8]}.flac"
    label = req.label or f"Движок: {' → '.join(b['type'] for b in chain)} · {req.source}"
    if req.in_place:
        # мастер на миксе: тот же файл, подпись прежняя + « · мастер»
        fname = req.file
        try:
            label = json.loads((jdir / f"{fname}.metrics.json").read_text()).get("label") or fname
        except (OSError, ValueError):
            label = fname
        if req.label:
            label = req.label
        elif not label.endswith(" · мастер"):
            label += " · мастер"
    target = jdir / fname
    created = time.strftime("%Y-%m-%dT%H:%M:%S")
    # на месте — через временный файл: оборванная запись не портит микс; своё имя на запрос и не *.flac —
    # список вариантов его не видит, параллельные записи не сталкиваются
    tmp = target.with_name(f".{target.name}.{uuid.uuid4().hex[:8]}.part") if req.in_place else target
    try:
        sf.write(str(tmp), out, sr, format="FLAC",   # временное имя .part — формат не по расширению
                 subtype=info.subtype if info.subtype in ("PCM_16", "PCM_24") else "PCM_24")
        if tmp != target:
            tmp.replace(target)
        metrics = analyze_file(target)
        (jdir / f"{fname}.metrics.json").write_text(json.dumps(
            {"file": fname, "created_at": created, "metrics": metrics, "label": label}, ensure_ascii=False))
    except Exception as e:  # noqa: BLE001
        if req.in_place:
            if tmp != target:
                tmp.unlink(missing_ok=True)   # микс не трогаем
        else:
            target.unlink(missing_ok=True)
            (jdir / f"{fname}.metrics.json").unlink(missing_ok=True)
        log.exception("fx render failed for job %s", job_id)
        raise HTTPException(500, f"fx render failed: {friendly_error(e)}") from e
    peak = float(np.max(np.abs(out))) if out.size else 0.0
    log.info("job %s: fx %s (%s) → %s, пик %.2f", job_id, req.source, label, fname, peak)
    return {"file": fname, "label": label, "created_at": created, "metrics": metrics,
            "clipped": peak > 1.0}


SYNTH_LEAD_S = 1.0   # нота, начатая задолго до окна, считается не раньше чем за столько (и за атаку+спад) до окна


SYNTH_LEVEL_SPAN_S = 24.0   # уровень партии на выходе цепочки меряется по стольким секундам от первой ноты (+ хвост)
# предел поправки уровня на блоки после synth, дБ: нелинейный блок (гейт, компрессор) не разгонит партию на +80 дБ
SYNTH_POST_CORR_DB = (-24.0, 12.0)


def _notes_mask(n: int, sr: int, notes: list):
    import numpy as np
    m = np.zeros(n, dtype=bool)
    for nt in notes:
        a, b = max(0, int(nt["t"] * sr)), min(n, int((nt["t"] + nt["d"]) * sr))
        if b > a:
            m[a:b] = True
    return m


def synth_ref_rms(track, sr: int, notes: list) -> float:
    """Громкость трека «на ухо» (A-уровень, по всем каналам) там, где звучат ноты партии (секунды трека), — опора
    уровня синта: одна и та же для превью любого окна и для пересборки (иначе 15 с тихого куплета и весь трек давали
    партию разной громкости). Нот нет — 0."""
    import numpy as np
    import fx_engine
    m = _notes_mask(len(track), sr, notes)
    if not m.any():
        return 0.0
    a = fx_engine.a_weight(np.asarray(track, dtype=np.float64), sr)
    return float(np.sqrt(np.mean(a[m] ** 2)))


def synth_part_level(blk: dict, n: int, sr: int, post: list | None = None, ch: int = 2,
                     target: float = 0.1) -> tuple[float, float]:
    """Громкость партии синта «на ухо» (A-уровень там, где звучит) на единицу сырого усиления и пик сырой партии —
    по всем нотам на всём треке, один раз на запрос (пэд на всю песню ~2 с): общий множитель уровня для любого окна.
    post — блоки цепочки после synth (до следующего synth/perc): уровень меряется на их выходе (eq +8 дБ после synth
    не делает партию громче цели) — по отрезку SYNTH_LEVEL_SPAN_S от первой ноты, сырой синт в нём заранее выставлен
    к целевому A-уровню target (гейт, перегруз и лента зависят от уровня входа — замер на рабочем уровне); поправка —
    в пределах SYNTH_POST_CORR_DB."""
    import numpy as np
    import fx_engine
    full = fx_engine.parse_chain([blk])[0]          # умолчания блока (в эндпоинте цепочка уже разобрана)
    res = fx_resources() if full.get("kit") else None   # синт на сэмплах (фортепиано) — набор из хранилища
    try:
        y = fx_engine._synth_notes(n, sr, 1, dict(full, output_db=0.0, _raw=True), res)[:, 0]
    finally:
        if res is not None:
            res.close()
    peak = float(np.abs(y).max()) if len(y) else 0.0
    m = np.abs(y) > 1e-6
    if not m.any():
        return 0.0, peak
    level = float(np.sqrt(np.mean(fx_engine.a_weight(y, sr)[m] ** 2)))
    # усилитель NAM выравнивает выход по входу (и идёт через очередь видеокарты) — в замер не входит
    post = [b for b in (post or []) if b.get("type") not in ("synth", "perc", "amp")]
    if not post or level <= 0.0:
        return level, peak
    s0 = int(np.argmax(m))
    tail = int(fx_engine.MAX_TAIL_S * sr) if any(b.get("type") in ("reverb", "delay", "spring") for b in post) else 0
    s1 = min(n, s0 + int(SYNTH_LEVEL_SPAN_S * sr))
    seg = np.zeros(s1 - s0 + tail)
    seg[: s1 - s0] = y[s0:s1] * (max(target, 1e-6) / level)
    sm = np.zeros(len(seg), dtype=bool)
    sm[: s1 - s0] = m[s0:s1]
    x = np.repeat(seg[:, None], max(1, ch), axis=1)
    post = [dict({k: v for k, v in b.items() if not k.startswith("_")}, _t0=s0 / sr) if b.get("type") == "tremolo"
            else b for b in post]
    # IR реверба/кабинета и наборы sampler/bass после synth — из хранилища
    res = fx_resources() if any(b.get("ir") or b.get("kit") for b in post) else None
    try:
        out = np.asarray(fx_engine.process(x, sr, post, res), dtype=np.float64)
    finally:
        if res is not None:
            res.close()
    before = float(np.sqrt(np.mean(fx_engine.a_weight(x, sr)[sm] ** 2)))
    after = float(np.sqrt(np.mean(fx_engine.a_weight(out, sr)[sm] ** 2)))
    if before <= 0:
        return level, peak
    lo, hi = SYNTH_POST_CORR_DB
    corr = min(max(after / before, 10 ** (-hi / 20)), 10 ** (-lo / 20))   # громче цели — не больше чем на hi дБ
    return level * corr, peak


def synth_window(chain: list, frm: float, win: float, track=None, sr: int = 0) -> list:
    """Ноты synth (секунды трека) → для куска окна [frm, frm + win): сдвиг на frm; отзвучавшие до окна и
    начатые после — отброшены; начатые до окна — не раньше SYNTH_LEAD_S до окна (иначе синт считал бы минуты
    сэмплов, а давняя нота упиралась бы в лимит начала); длинные — до края окна; _until — глушить синт на краю
    окна (затухание и сам синт не заходят в хвост реверба/дилея)."""
    out = []
    for i, b in enumerate(chain):
        if b["type"] == "perc":
            out.append(perc_window(b, frm, win, track, sr))
            continue
        if b["type"] == "tremolo":
            # фаза качания — от времени в треке: кусок окна начинается с frm (служебное поле — только от воркера)
            out.append(dict({k: v for k, v in b.items() if not k.startswith("_")}, _t0=frm))
            continue
        if b["type"] != "synth":
            out.append(b)
            continue
        # служебные поля (_until, _ref_rms, …) ставит только воркер: присланные в запросе — выбросить до расчётов
        b = {k: v for k, v in b.items() if not k.startswith("_")}
        notes = []
        # сэмплы (kit) и FM звучат от начала ноты (позиция в сэмпле, спад глубины): переносить начало к окну нельзя —
        # превью звучало бы иначе, чем тот же кусок в треке (перезапуск за 1 с до окна — громче на 5,8 дБ, ревью s7c)
        whole = bool(b.get("kit")) or 5 in (b.get("osc1"), b.get("osc2"))
        for nt in b["notes"]:
            t, end = nt["t"] - frm, nt["t"] - frm + nt["d"]
            if end <= 0 or t >= win:
                continue
            if whole:
                # не дальше WHOLE_LEAD_S до окна: сэмпл дольше 8 с не звучит, FM-глубина к 60 с погасла, а нота в 10 мин
                # упёрлась бы в предел начала (SYNTH_MIN_T → 422)
                t = max(t, -WHOLE_LEAD_S)
                notes.append(dict(nt, t=t, d=min(end, win) - t))
                continue
            # запас до окна — не меньше атаки и спада огибающих: дальше нота в удержании, уровень как в полном рендере
            lead = max(SYNTH_LEAD_S, b.get("attack_s", 0) + b.get("decay_s", 0) + 0.1,
                       b.get("f_attack_s", 0) + b.get("f_decay_s", 0) + 0.1)
            t = max(t, -lead)
            notes.append(dict(nt, t=t, d=min(end, win) - t))
        blk = dict(b, notes=notes, _until=win, _t0=frm)   # _t0 — фазы нот от их места в треке
        if track is not None:
            import numpy as np
            import fx_engine
            # общий уровень партии: громкость трека и синта по ВСЕМ нотам (синт — сырой, без нормировки окна)
            blk["_ref_rms"] = synth_ref_rms(track, sr, b["notes"])
            # блоки после synth до следующей партии: уровень партии — на их выходе
            post = []
            for nb in chain[i + 1:]:
                if nb["type"] in ("synth", "perc"):
                    break
                post.append(nb)
            ch = np.asarray(track).shape[1] if np.asarray(track).ndim > 1 else 1
            ref = blk["_ref_rms"]
            target = ref * fx_engine._db(b.get("rel_db", fx_engine.SYNTH_REL_DB)) \
                if ref > fx_engine._db(fx_engine.SYNTH_QUIET_DB) else 0.1
            blk["_syn_rms"], blk["_syn_peak"] = synth_part_level(b, len(track), sr, post, ch, target)
        out.append(blk)
    return out


WHOLE_LEAD_S = 60.0  # synth kit/FM: нота, начатая раньше окна, — не дальше этого (сэмпл и FM-спад уже отзвучали)
PERC_LEAD_S = 12.0   # perc: удар, начатый раньше окна, ещё звучит (тарелка набора — до 8–12 с, голоса — до 2 с)


def _fx_window(fn, *args):
    """synth_window с ошибкой цепочки как 422 (набора perc нет — понятный текст, а не 500)."""
    import fx_engine
    try:
        return fn(*args)
    except fx_engine.ChainError as e:
        raise HTTPException(422, str(e)) from e


def perc_window(b: dict, frm: float, win: float, track=None, sr: int = 0) -> dict:
    """Удары perc (секунды трека) → для куска окна [frm, frm + win): сдвиг на frm, место удара НЕ меняется
    (в отличие от ноты синта удар нельзя «подтянуть» к окну); начатые раньше PERC_LEAD_S до окна (отзвучали) и
    начатые после — отброшены; _until — край окна + FX_SAMPLER_TAIL_MAX_S (удар у края звучит в хвост превью, как
    у sampler; дальше буфер всё равно кончается); _t0 — начало окна в треке (сдвиги «по-живому» от места удара
    в треке). Уровень — общий для партии, как у synth: громкость трека и сырых ударов на ячейках ВСЕХ ударов."""
    import numpy as np
    import fx_engine
    b = {k: v for k, v in b.items() if not k.startswith("_")}   # служебные поля ставит только воркер
    hits = []
    for h in b.get("notes") or []:
        t = h["t"] - frm
        if t < -PERC_LEAD_S or t >= win:
            continue
        hits.append(dict(h, t=t, d=max(min(t + h["d"], win) - t, 1e-3)))
    blk = dict(b, notes=hits, _until=win + FX_SAMPLER_TAIL_MAX_S, _t0=frm)
    if track is not None:
        full = fx_engine.parse_chain([b])[0]
        n = len(track)
        res = fx_resources()
        try:
            y = fx_engine._perc_notes(n, sr, np.asarray(track).shape[1], full, res)   # каналы — как у окна
        finally:
            close = getattr(res, "close", None)
            if close:
                close()
        m = fx_engine.perc_cells(full["notes"], n, sr)
        blk["_ref_rms"] = float(np.sqrt(np.mean(np.asarray(track)[m] ** 2))) if m.any() else 0.0
        blk["_syn_rms"] = float(np.sqrt(np.mean(y[m] ** 2))) if m.any() else 0.0
        blk["_syn_peak"] = float(np.abs(y).max()) if len(y) else 0.0
    return blk


def _fx_process(job_id: int, part, sr: int, chain: list):
    """Цепочка движка; с amp — в очереди GPU (выгрузка захватов — тоже внутри неё)."""
    import fx_engine
    res = fx_resources()
    try:
        with gpu_queue(f"fx {job_id}") if fx_engine.needs_gpu(chain) else contextlib.nullcontext():
            try:
                return fx_engine.process(part, sr, chain, res)
            finally:
                close = getattr(res, "close", None)
                if close:
                    close()
    except fx_engine.ChainError as e:
        raise HTTPException(422, str(e)) from e


def _fx_stamp(paths: list[Path], chain: list) -> list:
    """Версии файлов, от которых зависит звук превью: источник и захваты/IR цепочки (mtime_ns).
    Пересобрали дорожки или перезалили захват под тем же именем — превью новое, не из кэша."""
    files = list(paths)
    for blk in chain:
        for key in ("kit", "kit_open", "kit_mid", "kit_low"):
            if blk.get("type") in ("sampler", "bass", "perc", "synth") and blk.get(key):
                try:
                    files += _kit_files(_kit_path(blk[key]))
                except KeyError:
                    pass  # неверное имя — process отвергнет
        for kind, key in (("amp", "model"), ("ir", "ir")):
            name = blk.get(key)
            if name and (blk["type"] == "amp") == (kind == "amp"):
                try:
                    files.append(_fx_asset_path(kind, name))
                except KeyError:
                    pass  # неверное имя уже отвергнуто бы обработкой; в ключ не идёт
    out = []
    for f in files:
        try:
            st = f.stat()
            out.append([f.name, st.st_mtime_ns, st.st_size])   # размер — как в версии замера захвата
        except OSError:
            out.append([f.name, 0, 0])
    return out


def _fx_kit_tail(chain: list) -> float:
    """Хвост превью для цепочки с наборами: по самому длинному сэмплу sampler, от FX_SAMPLER_TAIL_S до
    FX_SAMPLER_TAIL_MAX_S (тарелка у конца окна не обрывается, бочка не тянет лишние секунды). У bass —
    FX_SAMPLER_TAIL_S: нота глохнет вместе со входом, длинный сэмпл после окна не звучит."""
    import soundfile as sf
    longest = 0.0
    for blk in chain:
        if blk.get("type") == "perc" and int(blk.get("voice", 1)) != 0:
            longest = max(longest, 0.7 * float(blk.get("decay", 1.0)))   # голос драм-машины — до 0,7·decay с
        for key in ("kit", "kit_open", "kit_mid", "kit_low"):
            if blk.get("type") in ("sampler", "perc") and blk.get(key):
                try:
                    files = _kit_path(blk[key]).glob("*.wav")
                except KeyError:
                    continue
                # perc tone < 1 — сэмпл медленнее и длиннее (кросс-ревью: tone 0,5 обрывал хвост на 2 с раньше)
                speed = float(blk.get("tone", 1.0)) if blk.get("type") == "perc" else 1.0
                for f in files:
                    try:
                        longest = max(longest, sf.info(str(f)).duration / max(speed, 0.5))
                    except RuntimeError:
                        pass  # битый файл — его отвергнет чтение набора
    return min(max(FX_SAMPLER_TAIL_S, longest), FX_SAMPLER_TAIL_MAX_S)


def _mtime_or_zero(p: Path) -> float:
    """mtime для сортировки превью: файл могли удалить параллельно — тогда он «самый старый»."""
    try:
        return p.stat().st_mtime
    except OSError:
        return 0.0


def _fx_preview(job_id: int, jdir: Path, req: FxIn, chain: list, windowed, track, part, sr: int,
                used: list[Path], full=None) -> dict:
    """Превью куска [from, to) (+ хвост реверба/дилея): вход после to — тишина, в миксе
    к треку добавляется разница «обработанное − исходное». Тот же запрос — тот же файл."""
    import hashlib
    import uuid
    import numpy as np
    import soundfile as sf
    n = len(track)
    a, b = int(round(req.from_ * sr)), int(round(req.to * sr))
    tail = FX_PREVIEW_TAIL_S if any(blk["type"] in ("reverb", "delay") for blk in chain) else 0.0
    if any(blk["type"] in ("sampler", "bass", "perc") for blk in chain):
        # удар у конца окна: сэмпл звучит дальше, без обрыва; с эхом/ревербом — больший из хвостов
        tail = max(tail, _fx_kit_tail(chain))
    fade = int(round(req.fade * sr))
    stop = min(b + fade, n)              # вход: до to и спад края после него
    end = min(stop + int(tail * sr), n)
    import fx_engine
    key = {"source": req.source, "chain": chain, "from": req.from_, "to": req.to,
           "output": req.output, "files": _fx_stamp(used, chain), "engine": fx_engine.ENGINE_VERSION}
    if req.file:
        key["file"] = req.file           # версия файла — в files (mtime)
    if fade:
        key["fade"] = req.fade           # без края — поля нет (ключ меняют только версия движка и сам запрос)
    if req.pad:
        key["pad"] = True
    if req.add:
        key["add"] = True
    key = json.dumps(key, sort_keys=True)
    fname = f"preview-fx-{hashlib.sha1(key.encode()).hexdigest()[:8]}.flac"
    target = jdir / fname
    if target.is_file():
        try:
            os.utime(target)             # попадание — свежее: вытесняются давно не слушанные
            data, fsr = sf.read(str(target), dtype="float32")
            # перегруз в файле уже срезан записью — признак: пик на полной шкале
            return {"file": fname, "duration_sec": round(len(data) / fsr, 3),
                    "clipped": bool(data.size and float(np.max(np.abs(data))) >= FX_CLIP_FULL_SCALE)}
        except (OSError, RuntimeError):
            pass  # файл вытеснили параллельно — считаем заново
    seg = np.zeros((end - a, part.shape[1]))
    seg[:stop - a] = part[a:stop]
    if fade:
        # как dsp.WindowGraph: рост 0→1 за fade с начала окна, спад 1→0 от to до to+fade
        k = np.arange(stop - a, dtype=np.float64)
        w = np.minimum(1.0, k / fade) * np.clip((b + fade - (a + k)) / fade, 0.0, 1.0)
        seg[:stop - a] *= w[:, None]
    if full is not None:
        # full — полный рендер (ограничитель к цели LUFS); вход окна без краёв — тот же отрезок
        wet, seg = full()[a:end], part[a:end]
    else:
        wet = _fx_process(job_id, seg.astype(np.float32), sr, windowed()).astype(np.float64)
    if req.add:
        out = wet if req.output == "solo" else track[a:end] + wet     # поверх трека, ничего не вычитая
    else:
        out = wet if (req.output == "solo" and req.source != "mix") else track[a:end] + (wet - seg)
    lead = a if req.pad and a > 0 else 0
    # своё временное имя на запрос (одинаковые превью параллельно не делят файл) и не *.flac —
    # вытеснение ниже его не видит. Тишина до окна (pad) пишется блоками по секунде: массив длиной
    # from в памяти не нужен (окно на 4-й минуте — сотни МБ)
    tmp = target.with_name(f"{target.name}.{uuid.uuid4().hex[:8]}.part")
    with sf.SoundFile(str(tmp), "w", samplerate=sr, channels=out.shape[1], format="FLAC", subtype="PCM_24") as fh:
        silence = np.zeros((min(lead, sr), out.shape[1]))
        left = lead
        while left > 0:
            k = min(left, len(silence))
            fh.write(silence[:k])
            left -= k
        fh.write(out)
    tmp.replace(target)
    olds = sorted(jdir.glob("preview-fx-*.flac"), key=_mtime_or_zero, reverse=True)
    for old in olds[FX_PREVIEW_KEEP:]:
        old.unlink(missing_ok=True)
    peak = float(np.max(np.abs(out))) if out.size else 0.0
    return {"file": fname, "duration_sec": round((lead + len(out)) / sr, 3), "clipped": peak > 1.0}


@app.post("/jobs/{job_id}/fx")
def job_fx(job_id: int, req: FxIn):
    """Звуковой движок: цепочка блоков (fx_engine) на весь трек или дорожку, в окне
    from/to или целиком. Результат — вариант dsp-fx-*.flac (как варианты эффектов ПК)."""
    if not fx_engine_enabled():
        raise HTTPException(503, "звуковой движок выключен (настройка fx_engine / YUE_FX_ENGINE=0)")
    return _fx_render(job_id, req)


@app.get("/fx/assets")
def fx_assets():
    """Загруженные захваты NAM (с замеренной задержкой, если уже была) и IR."""
    import soundfile as sf
    amps, irs = [], []
    for p in sorted((_fx_dir() / "amps").glob("*.nam")):
        amps.append({"name": p.name, "latency": _nam_cached_latency(p)})  # None — замер при применении
    for p in sorted((_fx_dir() / "irs").glob("*.wav")):
        try:
            i = sf.info(str(p))
            irs.append({"name": p.name, "sr": i.samplerate, "seconds": round(i.frames / i.samplerate, 3)})
        except Exception:  # noqa: BLE001 - битый файл в списке не показываем, остальные — да
            log.warning("fx: unreadable IR %s", p.name)
    return {"amps": amps, "irs": irs, "kits": _kit_list()}


# ---------- Наборы сэмплов (блоки движка sampler — барабаны, bass — бас) ----------

# каталог наборов, которые воркер качает сам по требованию: части → (каталог в репозитории, отбор
# файлов). В поставку не входят; лицензия — у автора набора (docs/deployment.md)
VSCO_REF = "440300901dfe9275fd84e0b7763af1f8443ae62e"   # VSCO-2-CE, версия закреплена
FX_KITS = {
    "osdk": {  # The Open Source Drum Kit (Real Music Media) — public domain
        "repo": "crabacus/the-open-source-drumkit",
        # версия закреплена: файлы набора не подменятся под тем же именем
        "ref": "c58808b2ff5a6cd77c2f47cf45f1a892ce6a1e2c",
        "parts": {"kick": ("kick", r"kick\d+\.wav"), "snare": ("snare", r"snare-top\d+\.wav"),
                  # хэт: закрытый / полузакрытый / полуоткрытый (полностью открытого в наборе нет)
                  "hh-closed": ("hihat/closed-hihat", r"chh\d+\.wav"),
                  "hh-half": ("hihat/half-closed-hihat", r"hchh\d+\.wav"),
                  "hh-open": ("hihat/half-open-hihat", r"hohh\d+\.wav"),
                  "ride": ("ride", r"ride-mid-in\d+\.wav"), "crash": ("crash", r"crash\d+\.wav"),
                  # тамы по размеру — верхние микрофоны (small-tom<N>), без «-under» (нижний микрофон)
                  "tom-small": ("toms", r"small-tom\d+\.wav"), "tom-medium": ("toms", r"medium-tom\d+\.wav"),
                  "tom-large": ("toms", r"large-tom\d+\.wav")},
    },
    "salamander": {  # Salamander Grand Piano V3 (Alexander Holm), CC-BY 3.0 — блок synth с kit (фортепиано)
        "repo": "sfzinstruments/SalamanderGrandPiano",
        "ref": "3382bf9496bba2486f5ab0de55a264d1dfc38404",
        # один слой силы удара на часть: 30 нот через 3 полутона A0…C8 (~47 МБ); высота — из имени файла
        "parts": {"piano": ("Samples", r"[A-G]#?\d+v10\.flac"), "piano-soft": ("Samples", r"[A-G]#?\d+v4\.flac")},
        "max_s": 8.0,  # нота длиннее не тянется: память (30 сэмплов × 8 с стерео)
    },
    "growlybass": {  # Growlybass (Karoryfer Lecolds): Squier Jazz Bass, CC0 — блок bass
        "repo": "sfzinstruments/karoryfer.growlybass",
        "ref": "4f483268fc66b5a6d5781d421c0d11b8d08d3fc6",
        "parts": {"bass": ("sustain", r"[\w-]+\.wav")},
        "max_s": 6.0,  # сэмпл читается не дольше: 224 файла по 6 с (долгая нота целиком, память — 240 МБ)
    },
    # ещё бас-гитары и контрабас Karoryfer (CC0, github sfzinstruments): один слой силы, первый повтор — ноты
    # по полутонам/терциям, высоту блок bass меряет по звуку; ~7…28 МБ на часть
    "swagbass": {  # Ibanez BTB-400 со старыми плоскими струнами — глухой «мотаун»
        "repo": "sfzinstruments/karoryfer.swagbass",
        "ref": "9d10fcae71af1975988ddecd5af1c95d372c7355",
        "parts": {"bass": ("notes", r"[a-g]b?\d_f_rr1\.wav")},
        "max_s": 6.0,
    },
    "blackblue": {  # Black And Blue Basses: 5-струнные — darkblack пальцами (тёплый), babyblue медиатором (яркий)
        "repo": "sfzinstruments/karoryfer.black-and-blue-basses",
        "ref": "6e7d674cdb41be7a54dbccb15472401ad01099b9",
        "parts": {"darkblack": ("Samples/darkblack/reg", r"darkblack_[a-g]b?\d_f_rr1\.wav"),
                  "babyblue": ("Samples/babyblue/reg", r"babyblue_[a-g]b?\d_f_rr1\.wav")},
        "max_s": 6.0,
    },
    "meatbass": {  # контрабас Otto Rubner 1958, щипок (pizz)
        "repo": "sfzinstruments/karoryfer.meatbass",
        "ref": "ac9e859564bda286ab5ec672d00ff1aa2fef2895",
        "parts": {"pizz": ("Samples/pizz", r"[a-g]b?\d_vl3_rr1\.wav")},
        "max_s": 6.0,
    },
    "pastabass": {  # Squier Bass VI: плоские струны медиатором — щёлкающий баритон-бас
        "repo": "sfzinstruments/karoryfer.pastabass",
        "ref": "90135cd026db5d4fa0fe538240b4203f085f5244",
        "parts": {"linguine": ("samples/linguine", r"[a-g]b?\d_vl3_rr1\.wav")},
        "max_s": 6.0,
    },
    # оркестр VSCO-2 Community Edition (Sam Gossner и др., CC0): один слой силы, первый повтор; набор на инструмент —
    # качается, когда инструмент выбран впервые (~5…100 МБ). Третий элемент части — поправка октавы к ноте из имени:
    # VSCO почти везде считает C3 = 60 (+12), скрипка соло и арфа — C4 = 60 (0); замер pyin по каждому сэмплу
    # 2026-10-10. Файлы ставятся под точной высотой m<MIDI>.wav — synth и bass берут её из имени
    "vsco-violin": {  # скрипка соло и группа скрипок: смычок с вибрато, щипок, коротко
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "solo": ("Strings/Solo Violin/Arco Vib", r"LLVln_ArcoVib_[A-G]#?\d_f\.wav", 0),
            "ens": ("Strings/Violin Section/susVib", r"VlnEns_susVib_[A-G]#?\d_v2\.wav", 12),
            "pizz": ("Strings/Violin Section/Pizz", r"VlnEns_Pizz_[A-G]#?\d_v2_rr1\.wav", 12),
            "spic": ("Strings/Violin Section/Spic", r"VlnEns_Spic_[A-G]#?\d_v2_rr1\.wav", 12),
        },
    },
    "vsco-viola": {  # альты: смычок, щипок, коротко
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "ens": ("Strings/Viola Section/susvib", r"ViolaEns_susvib_[A-G]#?\d_v2_1\.wav", 12),
            "pizz": ("Strings/Viola Section/pizz", r"ViolaEns_pizz_[A-G]#?\d_v2_rr1\.wav", 12),
            "spic": ("Strings/Viola Section/spic", r"Violas_spic_[A-G]#?\d_v2_rr1\.wav", 12),
        },
    },
    "vsco-cello": {  # виолончели: смычок, щипок, коротко
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "ens": ("Strings/Cello Section/susvib", r"susvib_[A-G]#?\d_v3_1\.wav", 12),
            "pizz": ("Strings/Cello Section/pizzT", r"pizzT_[A-G]#?\d_v2_RR1\.wav", 12),
            "spic": ("Strings/Cello Section/spic", r"spic_[A-G]#?\d_v2_RR1\.wav", 12),
        },
    },
    "vsco-contrabass": {  # контрабас: смычок, щипок, коротко
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "sus": ("Strings/Solo Contrabass/SusVib", r"BKCtbss_SusVib_[A-G]#?\d_v3_rr1\.wav", 12),
            "pizz": ("Strings/Solo Contrabass/Pizz", r"BKCtbss_Pizz_[A-G]#?\d_v3_rr1\.wav", 12),
            "spic": ("Strings/Solo Contrabass/Spic", r"BKCtbss_Spic_[A-G]#?\d_v3_rr1\.wav", 12),
        },
    },
    "vsco-harp": {  # арфа
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "harp": ("Strings/Harp", r"KSHarp_[A-G]#?\d_mf\.wav", 0),
        },
    },
    "vsco-flute": {  # флейта: протяжно, коротко
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "sus": ("Woodwinds/Flute/susNV", r"LDFlute_susNV_[A-G]#?\d_v3_1\.wav", 12),
            "stac": ("Woodwinds/Flute/stac", r"LDFlute_stac_[A-G]#?\d_v2_rr1\.wav", 12),
        },
    },
    "vsco-oboe": {  # гобой
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "sus": ("Woodwinds/Oboe/Sus", r"Oboe_Sus_[A-G]#?\d_v3_Main\.wav", 12),
            "stac": ("Woodwinds/Oboe/Stacc", r"Oboe_Stacc_[A-G]#?\d_v2_rr1_Main\.wav", 12),
        },
    },
    "vsco-clarinet": {  # кларнет
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "sus": ("Woodwinds/Clarinet/susLong", r"DCClar_susLong_[A-G]#?\d_v2_rr1_sum\.wav", 12),
            "stac": ("Woodwinds/Clarinet/stac", r"DCClar_stac_[A-G]#?\d_v2_rr1_sum\.wav", 12),
        },
    },
    "vsco-bassoon": {  # фагот
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "sus": ("Woodwinds/Bassoon/sus", r"PSBassoon_[A-G]#?\d_v2_1\.wav", 12),
            "stac": ("Woodwinds/Bassoon/stac", r"PSBassoon_[A-G]#?\d_v2_rr1\.wav", 12),
        },
    },
    "vsco-trumpet": {  # труба
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "sus": ("Brass/Trumpet/sus", r"Sum_SHTrumpet_sus_[A-G]#?\d_v3_rr1\.wav", 12),
            "stac": ("Brass/Trumpet/stac", r"Sum_SHTrumpet_stac_[A-G]#?\d_v2_rr1\.wav", 12),
        },
    },
    "vsco-horn": {  # валторна
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "sus": ("Brass/F Horn/sus", r"MOHorn_sus_[A-G]#?\d_v2_1\.wav", 12),
            "stac": ("Brass/F Horn/stac", r"MOHorn_stac_[A-G]#?\d_v2_rr1\.wav", 12),
        },
    },
    "vsco-trombone": {  # тромбон
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "sus": ("Brass/Tenor Trombone/sus", r"tenortbn_sus_[A-G]#?\d_v2_1\.wav", 12),
            "stac": ("Brass/Tenor Trombone/stac", r"tenortbn_stac_[A-G]#?\d_v2_rr1\.wav", 12),
        },
    },
    "vsco-tuba": {  # туба
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "sus": ("Brass/Tuba/sus", r"Tuba3_sus_[A-G]#?\d_v2_rr1_Mid\.wav", 12),
            "stac": ("Brass/Tuba/stac", r"Tuba3_stac_[A-G]#?\d_v2_rr1_Sum\.wav", 12),
        },
    },
    "vsco-mallets": {  # колокольчики, маримба, ксилофон
        "repo": "sgossner/VSCO-2-CE", "ref": VSCO_REF, "max_s": 8.0,
        "parts": {
            "glock": ("Percussion/Glock", r"glock_medium_[A-G]#?\d\.wav", 12),
            "marimba": ("Percussion/Marimba", r"Marimba_hit_Outrigger_[A-G]#?\d_loud_01\.wav", 12),
            "xylo": ("Percussion/Xylo", r"Xylo_Medium_[A-G]#?\d_ff_01_far\.wav", 12),
        },
    },
    # драм-машины и синт-басы — воркер синтезирует сам (drumsynth.py): без сети и чужих лицензий
    **{k: {"synth": "drums"} for k in ("tr808", "tr909", "linn", "cr78", "simmons")},
    "synthbass": {"synth": "bass"},
}
FX_KIT_SR = 48000   # частота синтезированных наборов
FX_KIT_PART_RE = re.compile(r"^[a-z0-9-]{1,40}$")
_fx_kit_locks: dict = {}               # установка одного набора — по одной (общий каталог .part)
_fx_kit_locks_guard = threading.Lock()


KIT_EXTS = (".wav", ".flac")   # сэмплы наборов: wav (osdk, growlybass, синтезированные) и flac (Salamander)


def _kit_files(d: Path) -> list:
    """Файлы сэмплов части набора — wav и flac вместе, по имени (порядок один для kit() и kit_names())."""
    return sorted(f for f in d.iterdir() if f.suffix.lower() in KIT_EXTS) if d.is_dir() else []


def _kits_dir() -> Path:
    return _fx_dir() / "kits"


def _kit_path(name: str) -> Path:
    """`<набор>/<часть>` → каталог сэмплов; неверное имя — KeyError."""
    kit, _, part = name.partition("/")
    if not (FX_KIT_PART_RE.match(kit) and FX_KIT_PART_RE.match(part)):
        raise KeyError(name)
    return _kits_dir() / kit / part


def _kit_list() -> list:
    out = []
    root = _kits_dir()
    if root.is_dir():
        for d in sorted(p for p in root.glob("*/*") if p.is_dir() and FX_KIT_PART_RE.match(p.name)):
            n = len(_kit_files(d))
            if n:
                out.append({"name": f"{d.parent.name}/{d.name}", "samples": n})
    return out


def _http_get(url: str, timeout: float = 60) -> bytes:
    """GET без прокси окружения (воркер в LAN; прокси ПК сюда не относится)."""
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(url, timeout=timeout) as r:
        return r.read()


# ---------- фразы страницы «Инструменты»: круг через цепочку движка ----------

FX_PHRASE_KEEP = 40       # кругов в кэше (каждый — секунды звука; старые вытесняются)
FX_PHRASE_RE = re.compile(r"phrase-[0-9a-f]{12}\.wav")


def _phrase_dir() -> Path:
    d = DATA_DIR / "fx-phrases"
    d.mkdir(parents=True, exist_ok=True)
    return d


class PhraseIn(BaseModel):
    phrase: str
    tempo: float = 1.0
    chain: list = []
    stems: list[str] = []
    bypass: bool = False


@app.get("/fx/phrases")
def fx_phrases():
    """Каталог фраз: id, семья (guitar/bass/drums/synth), название, темп, долей в круге, аккорды по тактам (ноты синта
    и перкуссии кладёт страница), стиль синта, части и длина круга при темпе 1."""
    import phrases
    out = []
    for pid, p in phrases.PHRASES.items():
        _, cycle = phrases.render_dry(pid, 1.0)
        out.append({"id": pid, "family": p["family"], "name": p["name"], "bpm": p["bpm"], "beats": p["beats"],
                    "chords": p["chords"], "style": p.get("style", ""), "parts": list(phrases.parts_of(pid)),
                    "cycle_sec": round(cycle, 4)})
    return out


@app.post("/fx/phrase")
def fx_phrase(req: PhraseIn):
    """Круг фразы через цепочку (WAV 16 бит стерео): играет по кругу, пока крутятся регуляторы. Тот же запрос —
    тот же файл без пересчёта."""
    import hashlib
    import uuid
    import numpy as np
    import soundfile as sf
    import fx_engine
    import phrases
    if not fx_engine_enabled():
        raise HTTPException(503, "звуковой движок выключен (настройка fx_engine / YUE_FX_ENGINE=0)")
    if req.phrase not in phrases.PHRASES:
        raise HTTPException(404, f"нет фразы {req.phrase}")
    bad = [s for s in req.stems if s not in phrases.parts_of(req.phrase)]
    if bad:
        raise HTTPException(422, f"у фразы нет частей: {', '.join(bad)}")
    if not (phrases.TEMPO_MIN <= req.tempo <= phrases.TEMPO_MAX):
        raise HTTPException(422, f"темп {phrases.TEMPO_MIN}…{phrases.TEMPO_MAX}")
    # без эффектов: остаются блоки, которые сами играют ноты (синт, перкуссия), — иначе синт-фраза — тишина
    chain = [b for b in req.chain if isinstance(b, dict) and b.get("type") in phrases.NOTE_BLOCKS] if req.bypass \
        else req.chain
    try:
        if chain:                        # пустая — сухая фраза (как bypass)
            fx_engine.parse_chain(chain)
    except fx_engine.ChainError as e:
        raise HTTPException(422, str(e)) from e
    src = phrases.DIR / f"{req.phrase}.flac"
    # бас фразы — набором бас-гитары (если есть): его файлы тоже в ключе
    src_chain = [{"type": "bass", "kit": phrases.BASS_KIT}] if phrases.PHRASES[req.phrase]["family"] == "bass" else []
    key = json.dumps({"phrase": req.phrase, "tempo": round(req.tempo, 4), "chain": chain,
                      "stems": sorted(req.stems), "bypass": req.bypass, "engine": fx_engine.ENGINE_VERSION,
                      "files": _fx_stamp([src] if src.is_file() else [], chain + src_chain)}, sort_keys=True)
    fname = f"phrase-{hashlib.sha1(key.encode()).hexdigest()[:12]}.wav"
    d = _phrase_dir()
    target = d / fname
    if target.is_file():
        try:
            info = sf.info(str(target))  # вытесняются самые старые по времени расчёта
            # «срезано» — метка рядом с кругом: повтор тех же настроек не теряет подсказку «громко»
            return {"file": fname, "cycle_sec": round(info.frames / info.samplerate, 4),
                    "clipped": target.with_name(target.name + ".clipped").is_file()}
        except (OSError, RuntimeError):
            pass  # вытеснили параллельно — считаем заново
    res = fx_resources()
    try:
        dry, cycle = phrases.render_dry(req.phrase, req.tempo, res=res)
    except ValueError as e:
        raise HTTPException(422, str(e)) from e
    try:
        with gpu_queue("fx phrase") if fx_engine.needs_gpu(chain) else contextlib.nullcontext():
            try:
                out, clipped = phrases.loop_render(dry, req.stems, chain, phrases.SR, res, bypass=req.bypass)
            finally:
                close = getattr(res, "close", None)
                if close:
                    close()
    except fx_engine.ChainError as e:
        raise HTTPException(422, str(e)) from e
    except ValueError as e:
        raise HTTPException(422, str(e)) from e
    tmp = target.with_name(f"{target.name}.{uuid.uuid4().hex[:8]}.part")
    sf.write(str(tmp), np.clip(out, -1.0, 1.0), phrases.SR, format="WAV", subtype="PCM_16")
    mark = target.with_name(target.name + ".clipped")
    if clipped:
        mark.touch()                  # метка — до публикации круга: попадание в кэш видит её сразу
    else:
        mark.unlink(missing_ok=True)
    tmp.replace(target)
    olds = sorted(d.glob("phrase-*.wav"), key=_mtime_or_zero, reverse=True)
    for old in olds[FX_PHRASE_KEEP:]:
        old.unlink(missing_ok=True)
        old.with_name(old.name + ".clipped").unlink(missing_ok=True)
    return {"file": fname, "cycle_sec": round(len(out) / phrases.SR, 4), "clipped": bool(clipped)}


@app.get("/fx/phrase/files/{file}")
def fx_phrase_file(file: str):
    """Круг из кэша фраз (имя — из ответа /fx/phrase)."""
    path = _phrase_dir() / file
    if not FX_PHRASE_RE.fullmatch(file) or not path.is_file():
        raise HTTPException(404, "нет такого круга")
    return FileResponse(path, media_type="audio/wav")


# ---------- свои инструменты (страница «Инструменты», выбор в студии) ----------

def _instrument_row(r) -> dict:
    return {"id": r["id"], "name": r["name"], "base": r["base"], "group": r["grp"], "stems": json.loads(r["stems"]),
            "chain": json.loads(r["chain"]), "extra": json.loads(r["extra"]), "created_at": r["created_at"]}


def _instrument_body(body, partial: bool) -> dict:
    import fx_engine
    try:
        return fx_instruments.validate(body, fx_engine.parse_chain, partial=partial)
    except fx_instruments.InstrumentError as e:
        raise HTTPException(422, str(e)) from e


@app.get("/fx/instruments")
def fx_instruments_list():
    with db() as conn:
        return [_instrument_row(r) for r in conn.execute("SELECT * FROM fx_instruments ORDER BY id")]


@app.post("/fx/instruments")
def fx_instrument_create(body: dict):
    p = _instrument_body(body, partial=False)
    with db() as conn:
        cur = conn.execute(
            "INSERT INTO fx_instruments(name,base,grp,stems,chain,extra,created_at) VALUES(?,?,?,?,?,?,?)",
            (p["name"], p["base"], p["group"], json.dumps(p["stems"]), json.dumps(p["chain"]),
             json.dumps(p["extra"]), time.strftime("%Y-%m-%dT%H:%M:%S")))
        r = conn.execute("SELECT * FROM fx_instruments WHERE id=?", (cur.lastrowid,)).fetchone()
    return _instrument_row(r)


@app.put("/fx/instruments/{iid}")
def fx_instrument_update(iid: int, body: dict):
    p = _instrument_body(body, partial=True)
    cols = {"name": "name", "base": "base", "group": "grp", "stems": "stems", "chain": "chain", "extra": "extra"}
    with db() as conn:
        if not conn.execute("SELECT 1 FROM fx_instruments WHERE id=?", (iid,)).fetchone():
            raise HTTPException(404, f"нет инструмента {iid}")
        for k, v in p.items():
            conn.execute(f"UPDATE fx_instruments SET {cols[k]}=? WHERE id=?",
                         (json.dumps(v) if k in ("stems", "chain", "extra") else v, iid))
        r = conn.execute("SELECT * FROM fx_instruments WHERE id=?", (iid,)).fetchone()
    return _instrument_row(r)


@app.delete("/fx/instruments/{iid}")
def fx_instrument_delete(iid: int):
    with db() as conn:
        if conn.execute("DELETE FROM fx_instruments WHERE id=?", (iid,)).rowcount == 0:
            raise HTTPException(404, f"нет инструмента {iid}")
    return {"ok": True}


@app.post("/fx/kits/install")
def fx_kit_install(name: str = ""):
    """Скачать набор сэмплов из каталога FX_KITS (повтор — без сети). Одновременные установки
    одного набора идут по очереди: иначе вторая удаляла бы каталог, опубликованный первой."""
    spec = FX_KITS.get(name)
    if spec is None:
        raise HTTPException(422, f"unknown kit {name!r} (есть: {', '.join(FX_KITS)})")
    with _fx_kits_guard_for(name), _kit_file_lock(name):
        if spec.get("synth"):
            return _fx_kit_generate(name, spec["synth"])
        return _fx_kit_install(name, spec)


@contextmanager
def _kit_file_lock(name: str):
    """Блокировка набора между процессами (воркер и kits_install.py при make worker): иначе оба писали бы в одну
    <часть>.part, и replace одного ронял бы установку другого."""
    import fcntl
    _kits_dir().mkdir(parents=True, exist_ok=True)
    with open(_kits_dir() / f".{name}.lock", "w") as fh:
        fcntl.flock(fh, fcntl.LOCK_EX)
        try:
            yield
        finally:
            fcntl.flock(fh, fcntl.LOCK_UN)


def _fx_kits_guard_for(name: str) -> threading.Lock:
    with _fx_kit_locks_guard:
        return _fx_kit_locks.setdefault(name, threading.Lock())


def _fx_kit_generate(name: str, kind: str) -> dict:
    """Синтезированный набор: части drumsynth (слои силы удара или ноты баса) → wav PCM 16 в каталог набора.
    Есть часть — не пересчитывается; недописанная — во временном каталоге .part и в список не попадёт."""
    import numpy as np
    import soundfile as sf
    import drumsynth
    if kind == "drums":
        parts = {p: [(f"v{k}", lambda p=p, k=k: drumsynth.render(name, p, k, FX_KIT_SR))
                     for k in range(drumsynth.DRUM_LAYERS)] for p in drumsynth.KITS[name]}
    else:
        parts = {p: [(f"m{m}", lambda p=p, m=m: drumsynth.render_bass(p, m, FX_KIT_SR))
                     for m in drumsynth.BASS_RANGE[p]] for p in drumsynth.BASS_KITS[name]}
    out, generated = {}, False
    for part, items in parts.items():
        dst = _kits_dir() / name / part
        have = list(dst.glob("*.wav")) if dst.is_dir() else []
        if have:
            out[part] = len(have)
            continue
        tmp = dst.with_name(dst.name + ".part")
        shutil.rmtree(tmp, ignore_errors=True)
        tmp.mkdir(parents=True)
        # синтез — тот же прогресс, что скачивание (kits_install печатает, приложение показывает)
        _kit_progress_set(name, part=part, done=0, total=len(items), bytes=0)
        try:
            for i, (fname, fn) in enumerate(items):
                y = np.clip(fn(), -1.0, 1.0)
                sf.write(str(tmp / f"{fname}.wav"), y, FX_KIT_SR, subtype="PCM_16")
                _kit_progress_add(name, done=i + 1, size=(tmp / f"{fname}.wav").stat().st_size)
        finally:
            _kit_progress_set(name)
        shutil.rmtree(dst, ignore_errors=True)
        tmp.replace(dst)
        out[part] = len(items)
        generated = True
        log.info("fx kit %s/%s: %d samples synthesized", name, part, len(items))
    return {"name": name, "parts": out, "downloaded": False, "generated": generated}


FX_KIT_FILES = Path(__file__).with_name("fx_kit_files.json")
FX_KIT_RETRIES = 3            # попыток на файл набора
FX_KIT_RETRY_PAUSE_S = 1.0    # пауза перед повтором: 1, 2 с

# идущие установки по наборам: {набор: {name, part, done, total, bytes}} — несколько наборов разом (пульт студии и
# страница «Инструменты») не затирают и не очищают прогресс друг друга
_kit_progress: dict = {}
_kit_progress_lock = threading.Lock()


def _kit_progress_set(name: str, **kw) -> None:
    """Прогресс набора name; без полей — установка закончена (запись убирается)."""
    with _kit_progress_lock:
        if kw and name in _kit_progress:
            _kit_progress[name].update(kw)       # следующая часть того же набора — место в порядке прежнее
        elif kw:
            _kit_progress[name] = {"name": name, **kw}
        else:
            _kit_progress.pop(name, None)


def _kit_progress_add(name: str, done: int, size: int) -> None:
    with _kit_progress_lock:
        p = _kit_progress.get(name)
        if p:
            p["done"] = done
            p["bytes"] = p.get("bytes", 0) + size


def _http_get_retry(url: str) -> bytes:
    """Файл набора с повторами: сеть рвётся, GitHub иногда отвечает 5xx — без повтора набор не ставился целиком."""
    import http.client
    for k in range(FX_KIT_RETRIES):
        try:
            return _http_get(url)
        except (OSError, http.client.HTTPException):
            if k == FX_KIT_RETRIES - 1:
                raise
            time.sleep(FX_KIT_RETRY_PAUSE_S * (k + 1))
    raise OSError(url)   # недостижимо: цикл либо вернул, либо поднял


@app.get("/fx/kits/progress")
def fx_kits_progress():
    """Идущая установка набора {name, part, done, total, bytes} (нет — {}): прогресс для приложения."""
    with _kit_progress_lock:
        return dict(list(_kit_progress.values())[-1]) if _kit_progress else {}   # последняя начатая


def _kit_pinned_files() -> dict:
    """Закреплённые списки файлов наборов {набор: {часть: [имена]}} (fx_kit_files.json рядом с воркером; нет — {})."""
    try:
        return json.loads(FX_KIT_FILES.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}


def _fx_kit_install(name: str, spec: dict) -> dict:
    import http.client
    parts, downloaded = {}, False
    for part, (src, pattern, *shift) in spec["parts"].items():
        dst = _kits_dir() / name / part
        have = _kit_files(dst)   # wav и flac: установленный Salamander (flac) не качается заново
        if have:
            parts[part] = len(have)
            continue
        tmp = dst.with_name(dst.name + ".part")   # недокачанное — не набор: в список не попадёт
        try:
            pinned = _kit_pinned_files().get(name, {}).get(part)
            if pinned:
                # список файлов закреплён (fx_kit_files.json): без API GitHub — у него 60 запросов в час на адрес
                # без ключа, а у оркестра 31 часть; сами файлы — прямыми ссылками raw (лимит API на них не действует)
                bad = [n for n in pinned if not re.fullmatch(pattern, n)]
                if bad:   # список и шаблон части разошлись — не ставить чужие файлы молча
                    raise ValueError(f"закреплённый список не по шаблону части: {', '.join(bad[:3])}")
                base = f"https://raw.githubusercontent.com/{spec['repo']}/{spec['ref']}/{urllib.parse.quote(src)}"
                files = [{"name": n, "download_url": f"{base}/{urllib.parse.quote(n)}"} for n in pinned]
            else:
                listing = json.loads(_http_get(
                    # каталоги с пробелами («Solo Violin/Arco Vib») — экранируются; «/» остаётся
                    f"https://api.github.com/repos/{spec['repo']}/contents/{urllib.parse.quote(src)}?ref={spec['ref']}"))
                if not isinstance(listing, list):
                    raise ValueError("GitHub ответил не списком файлов")
                files = [f for f in listing if isinstance(f, dict) and re.fullmatch(pattern, str(f.get("name", "")))]
            if not files:
                raise ValueError("в источнике нет файлов")
            # .part не стирается: файлы, скачанные до обрыва, не качаются заново (докачка)
            tmp.mkdir(parents=True, exist_ok=True)
            _kit_progress_set(name, part=part, done=0, total=len(files), bytes=0)
            for i, f in enumerate(files):
                fname = str(f["name"])
                if shift:   # нота из имени + поправка октавы → точная высота в имени (m<MIDI>.wav)
                    import fx_engine
                    midi = fx_engine.sample_midi(Path(fname).stem)
                    if midi is None:
                        raise ValueError(f"{fname}: нет ноты в имени")
                    fname = f"m{midi + shift[0]}{Path(fname).suffix}"
                out = tmp / fname
                if not (out.is_file() and out.stat().st_size > 0):
                    try:
                        data = _http_get_retry(str(f["download_url"]))
                    except (OSError, http.client.HTTPException) as e:
                        raise OSError(f"{f['name']}: {e}") from e   # причина — с именем файла
                    part_file = out.with_name(out.name + ".dl")   # недокачанный файл не выдаёт себя за целый
                    part_file.write_bytes(data)
                    part_file.replace(out)
                _kit_progress_add(name, done=i + 1, size=out.stat().st_size)
        except (OSError, ValueError, KeyError, http.client.HTTPException) as e:
            # сеть, обрыв ответа, разбор — причина в ответ; скачанное остаётся в .part до повтора
            _kit_progress_set(name)
            raise HTTPException(502, f"kit {name}/{part}: не скачать — {e}") from e
        shutil.rmtree(dst, ignore_errors=True)
        tmp.replace(dst)
        parts[part] = len(files)
        downloaded = True
        log.info("fx kit %s/%s: %d samples", name, part, len(files))
    _kit_progress_set(name)   # установка набора закончена — прогресса нет (между частями запись не удаляется)
    return {"name": name, "parts": parts, "downloaded": downloaded}


@app.post("/fx/assets")
async def fx_asset_upload(request: Request, kind: str = "", name: str = ""):
    """Загрузить захват NAM (.nam) или IR (.wav): тело — байты файла. В поставку не
    входят — пользователь кладёт свои (лицензии захватов — у их авторов)."""
    if kind not in FX_KINDS:
        raise HTTPException(422, "kind must be amp or ir")
    ext = FX_KINDS[kind][1]
    if not name.lower().endswith(ext):
        raise HTTPException(422, f"name must end with {ext}")
    try:
        target = _fx_asset_path(kind, name)
    except KeyError:
        raise HTTPException(422, "bad name: allowed letters, digits, space and . , ( ) + - _ "
                                 "(no / \\ or ..), up to 150 chars") from None
    try:
        declared = int(request.headers.get("content-length") or 0)
    except ValueError:
        declared = 0
    if declared > FX_UPLOAD_MAX:
        raise HTTPException(413, f"file larger than {FX_UPLOAD_MAX // (1024 * 1024)} MB")
    data = await request.body()
    if len(data) > FX_UPLOAD_MAX:
        raise HTTPException(413, f"file larger than {FX_UPLOAD_MAX // (1024 * 1024)} MB")
    if not data:
        raise HTTPException(422, "empty body")
    if kind == "amp":
        try:
            cfg = json.loads(data)
            ok = isinstance(cfg, dict) and "architecture" in cfg and "weights" in cfg
        except ValueError:
            ok = False
        if not ok:
            raise HTTPException(422, "not a .nam file (JSON with architecture and weights)")
    else:
        import io
        import soundfile as sf
        try:
            ir, _ = sf.read(io.BytesIO(data), dtype="float32")
            ok = len(ir) > 0
        except Exception:  # noqa: BLE001 - любой сбой разбора = не wav
            ok = False
        if not ok:
            raise HTTPException(422, "not a readable .wav file")
    target.parent.mkdir(parents=True, exist_ok=True)
    tmp = target.with_name(target.name + ".part")
    tmp.write_bytes(data)
    tmp.replace(target)
    _nam_latency_path(target).unlink(missing_ok=True)  # новый файл — старый замер не годится
    log.info("fx asset uploaded: %s %s (%d bytes)", kind, target.name, len(data))
    return {"name": target.name, "kind": kind}


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
