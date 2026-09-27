"""Yue Studio worker — API вокруг резидентной YuE2Pipeline (:8091, 0.0.0.0).

Держит модель загруженной между запросами; очередь задач живёт здесь же
(SQLite), так что закрытие desktop-приложения не прерывает генерации.

Эндпоинты:
  GET  /health           — статус, загружена ли модель
  POST /jobs             — постановка в очередь {title, style, lyrics, seed, cot, abc?}
  GET  /jobs             — список последних задач
  GET  /jobs/{id}        — статус задачи
  POST /jobs/{id}/cancel — отмена (только queued)
  POST /plan             — только стадия плана: ABC до рендера {style, lyrics, seed, cot}
  GET  /listen/{id}      — страница прослушивания
  GET  /audio/{id}/{f}   — артефакты задачи (audio.flac, score.abc, request.abc, ...)
"""
import json
import logging
import os
import re
import sqlite3
import tempfile
import threading
import time
import urllib.request

import llm
import media
from pathlib import Path

from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import FileResponse, HTMLResponse
from pydantic import BaseModel, Field

from abcparse import parse_abc
from dsp import analyze_file
from sheetsage import transcribe as ss_transcribe
from stems import separate as demucs_separate

log = logging.getLogger("yue-worker")
logging.basicConfig(level=logging.INFO)

DATA_DIR = Path(os.environ.get("YUE_DATA_DIR", Path.home() / "yue-studio" / "data"))
JOBS_DIR = DATA_DIR / "jobs"
REFS_DIR = DATA_DIR / "references"
TRANSCRIBES_DIR = DATA_DIR / "transcribes"
CORPUS_DIR = DATA_DIR / "corpus"
DB_PATH = DATA_DIR / "yue.db"
for d in (JOBS_DIR, REFS_DIR, TRANSCRIBES_DIR, CORPUS_DIR):
    d.mkdir(parents=True, exist_ok=True)

WHISPER_PY = Path(os.environ.get("YUE_WHISPER_PY", Path.home() / "whisper-venv" / "bin" / "python"))

# Бюджет семантических токенов на песню (дефолт протокола 9000 ≈ 4.5–6 мин;
# контекст модели 24576 общий: стиль+лирика+план+песня). 16000 ≈ до ~10 мин,
# пик VRAM растёт с длиной — для 16 ГБ больше не поднимать.
MAX_SEM_TOKENS = int(os.environ.get("YUE2_MAX_TOKENS", "16000"))
# cfg_scale (classifier-free guidance): выше — точнее следует стилю, но суше;
# 1.5 — сбалансированное среднее. 0/отсутствие — дефолт библиотеки.
CFG_SCALE = float(os.environ.get("YUE2_CFG_SCALE", "1.5"))

app = FastAPI(title="yue-worker")

_pipe = None
_pipe_lock = threading.Lock()   # одна операция с моделью одновременно
_load_lock = threading.Lock()
_load_error: str | None = None

db_lock = threading.Lock()


def db() -> sqlite3.Connection:
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    return conn


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
        conn.execute("""
        CREATE TABLE IF NOT EXISTS corpus (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            status TEXT NOT NULL DEFAULT 'open',
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
    """Счётчик токенов/фазы из колбэка пайплайна (plan/generate_semantic)."""
    counters = {"phase": "load", "tokens": 0}

    def on_token(*args):
        for a in args:
            if isinstance(a, str):
                counters["phase"] = "plan" if "abc" in a.lower() else "semantic"
        counters["tokens"] += 1

    return on_token, counters


def _progress_watcher(job_id: int, counters: dict, t0: float, budget: int = 0):
    """Раз в 2 с пишет прогресс в _progress (для /jobs)."""
    stop = threading.Event()
    last = 0

    def watch():
        nonlocal last   # иначе первый тик: referenced before assignment
        while not stop.is_set():
            elapsed = time.time() - t0
            tps = (counters["tokens"] - last) / 2 if elapsed > 2 else None
            last = counters["tokens"]
            # честный процент есть только у семантики (самая длинная фаза):
            # токены / бюджет; загрузка модели и план — неопределённая длительность
            pct = None
            if counters["phase"] == "semantic":
                pct = min(99, counters["tokens"] * 100 // max(1, budget or MAX_SEM_TOKENS))
            with _state_lock:
                _progress[job_id] = {
                    "stage": counters["phase"],
                    "tokens": counters["tokens"],
                    "tok_per_s": round(tps, 1) if tps else None,
                    "elapsed_s": round(elapsed, 1),
                    "progress_pct": pct,
                }
            stop.wait(2)

    t = threading.Thread(target=watch, daemon=True, name=f"watch-{job_id}")
    t.start()
    return stop


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
    watch_stop = _progress_watcher(job_id, counters, time.time(), budget)
    try:  # noqa: SIM105 - очистка состояния после любого исхода
        with _pipe_lock:
            pipe = _get_pipe()
            counters["phase"] = "plan"
            request = {"style": row["style"], "lyrics": row["lyrics"], "cot": row["cot"],
                       "on_token": on_token,
                       "cancelled": lambda: cancel_ev.is_set()}
            if CFG_SCALE > 0:
                request["cfg_scale"] = CFG_SCALE
            # бюджет длины — параметр Sampling, не запроса
            try:
                from yue2.protocol import Sampling
                request["semantic_sampling"] = Sampling(max_tokens=budget)
            except ImportError:
                pass
            if row["seed"]:
                request["seed"] = row["seed"]
            if row["req_abc"]:
                abc_path = job_dir / row["req_abc"]
                if abc_path.is_file():
                    request["abc"] = abc_path.read_text()
            t0 = time.time()
            try:
                try:
                    song = pipe(**request)
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
                with db_lock, db() as conn:
                    conn.execute("UPDATE jobs SET status='error', error=?, finished_at=? WHERE id=?",
                                 (str(e), time.strftime("%Y-%m-%dT%H:%M:%S"), job_id))
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
    finally:
        watch_stop.set()
        with _state_lock:
            _cancel_flags.pop(job_id, None)
            _progress.pop(job_id, None)


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
    global _pipe
    with _pipe_lock:
        if _pipe is not None:
            try:
                _pipe.__exit__(None, None, None)
            except Exception:  # noqa: BLE001
                log.exception("pipeline close failed")
            _pipe = None


class JobIn(BaseModel):
    title: str = ""
    style: str
    lyrics: str
    seed: int | None = None
    cot: str = Field(default="full", pattern="^(full|melody|off)$")
    abc: str | None = None
    # черновик ~15-20 с вместо полного трека: быстро послушать, стоит ли рендерить целиком
    draft: bool = False


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


@app.get("/config")
def get_config():
    """Текущие настройки: Ollama (копайтер/перевод), пути. Меняется через POST."""
    return {
        "ollama_url": OLLAMA_URL,
        "ollama_model": OLLAMA_MODEL,
        "data_dir": str(DATA_DIR),
        "whisper_py": str(WHISPER_PY),
        "whisper_available": WHISPER_PY.is_file(),
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
    with db_lock, db() as conn:
        cur = conn.execute(
            "INSERT INTO jobs(title,status,style,lyrics,seed,cot,draft,created_at) VALUES(?,?,?,?,?,?,?,?)",
            (req.title or f"Untitled {time.strftime('%H:%M')}", "queued", req.style,
             req.lyrics, req.seed, req.cot, int(req.draft), time.strftime("%Y-%m-%dT%H:%M:%S")))
        job_id = cur.lastrowid
        if req.abc is not None and req.abc.strip():
            (JOBS_DIR / str(job_id)).mkdir(parents=True, exist_ok=True)
            (JOBS_DIR / str(job_id) / "request.abc").write_text(req.abc, encoding="utf-8")
            conn.execute("UPDATE jobs SET req_abc='request.abc' WHERE id=?", (job_id,))
    return {"id": job_id}


@app.post("/plan")
def plan(req: PlanIn):
    """Только стадия плана: возвращает ABC до рендера (GPU, но без синтеза звука).

    Синхронный: ждёт освобождения GPU, если идёт рендер. При холодном старте
    включает загрузку модели, первый вызов может занять пару минут.
    """
    if req.cot == "off":
        raise HTTPException(422, "plan requires cot=full or cot=melody (off does not produce ABC)")
    with _pipe_lock:
        pipe = _get_pipe()
        t0 = time.time()
        result = pipe.plan(style=req.style, lyrics=req.lyrics, cot=req.cot,
                           **({"seed": req.seed} if req.seed else {}))
    truncated = result.truncated
    if isinstance(truncated, dict):
        truncated = any(truncated.values())
    return {"abc": result.abc, "truncated": bool(truncated), "seconds": round(time.time() - t0, 1)}


def _job_dict(row) -> dict:
    d = {k: row[k] for k in row.keys()}
    if d.get("status") == "running":
        with _state_lock:
            d.update(_progress.get(row["id"], {}))
    return d


@app.get("/jobs")
def list_jobs():
    with db_lock, db() as conn:
        rows = conn.execute("SELECT * FROM jobs ORDER BY id DESC LIMIT 100").fetchall()
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

def _whisper_lyrics(src: Path) -> tuple[str, str]:
    """Текст трека через faster-whisper (отдельный venv, см. whisper_run.py).
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
        r = subprocess.run([str(WHISPER_PY), str(Path(__file__).parent / "whisper_run.py"), str(src)],
                           capture_output=True, text=True, timeout=600, env=env)
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
    data = await request.body()
    if not data:
        raise HTTPException(422, "empty body")
    with tempfile.TemporaryDirectory() as td:
        src = Path(td) / fname
        src.write_bytes(data)
        t0 = time.time()
        text, err = _whisper_lyrics(src)
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
def job_lyrics(job_id: int):
    """Текст из готового аудио джобы (faster-whisper) — без повторной загрузки
    файла: аудио уже у воркера. Для овердаба/кавера этой же джобы."""
    row = _job_row(job_id)
    if row is None or row["status"] != "done" or not row["audio_file"]:
        raise HTTPException(404, "job not done (no audio)")
    src = JOBS_DIR / str(job_id) / row["audio_file"]
    t0 = time.time()
    text, err = _whisper_lyrics(src)
    if err:
        raise HTTPException(500, f"lyrics recognition failed: {err}")
    if not text.strip():
        raise HTTPException(422, "no speech recognized")
    return {"text": text.strip(), "seconds": round(time.time() - t0, 1)}


@app.get("/jobs/{job_id}/score")
def job_score(job_id: int):
    """Таймлайн из score.abc: такты × голоса, аккорды, секции + RMS по секциям.
    Кэшируется в score.json."""
    row = _job_row(job_id)
    if row is None:
        raise HTTPException(404, "job not found")
    job_dir = JOBS_DIR / str(job_id)
    abc_path = job_dir / (row["abc_file"] or "score.abc")
    cache = job_dir / "score.json"
    if not abc_path.is_file():
        raise HTTPException(404, "no score.abc for this job")
    if cache.is_file() and cache.stat().st_mtime >= abc_path.stat().st_mtime:
        return json.loads(cache.read_text())
    parsed = parse_abc(abc_path.read_text())
    parsed["rms_sections"] = _rms_sections(job_dir / row["audio_file"], parsed["bars"]) \
        if row["audio_file"] and (job_dir / row["audio_file"]).is_file() else []
    cache.write_text(json.dumps(parsed, ensure_ascii=False))
    return parsed


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
            (title or fname.rsplit(".", 1)[0], "done", "(импорт внешнего трека)", "",
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
    with db_lock, db() as conn:
        cur = conn.execute(
            "INSERT INTO jobs(title,status,style,lyrics,seed,cot,req_abc,overdub_of,created_at)"
            " VALUES(?,?,?,?,?,?,?,?,?)",
            (f"overdub of #{job_id}", "queued", req.style, req.lyrics, None, "full",
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
        result = demucs_separate(job_dir / row["audio_file"], job_dir)
    except Exception as e:  # noqa: BLE001
        log.exception("stems failed")
        raise HTTPException(500, f"demucs failed: {e}") from e
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
    stems = sorted(job_dir.glob("stem-*.flac"))
    if not stems:
        try:
            demucs_separate(job_dir / row["audio_file"], job_dir)
        except Exception as e:  # noqa: BLE001
            raise HTTPException(500, f"demucs failed: {e}") from e
        stems = sorted(job_dir.glob("stem-*.flac"))
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


@app.post("/jobs/{job_id}/dsp")
async def add_dsp_variant(job_id: int, request: Request):
    """Вариант пост-обработки от приложения (ffmpeg на ПК): тело — flac,
    X-Filename — dsp-<цепочка>.flac. Замеряется и кэшируется рядом."""
    fname = request.headers.get("x-filename", "")
    if not DSP_NAME_RE.match(fname):
        raise HTTPException(422, "filename must be dsp-<chain>.flac")
    with db_lock, db() as conn:
        if conn.execute("SELECT 1 FROM jobs WHERE id=?", (job_id,)).fetchone() is None:
            raise HTTPException(404, "job not found")
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
            {"file": fname, "created_at": time.strftime("%Y-%m-%dT%H:%M:%S"), "metrics": metrics}))
    except Exception as e:  # noqa: BLE001
        target.unlink(missing_ok=True)
        (job_dir / f"{fname}.metrics.json").unlink(missing_ok=True)
        raise HTTPException(422, f"cannot analyze dsp variant: {e}") from e
    return {"file": fname, "created_at": time.strftime("%Y-%m-%dT%H:%M:%S"), "metrics": metrics}


@app.get("/jobs/{job_id}/dsp")
def dsp_variants(job_id: int):
    job_dir = JOBS_DIR / str(job_id)
    if not job_dir.is_dir():
        return []
    out = []
    for f in sorted(job_dir.glob("dsp-*.flac"), key=lambda p: p.stat().st_mtime, reverse=True):
        item = {"file": f.name,
                "created_at": time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime(f.stat().st_mtime)),
                "metrics": None}
        mp = job_dir / f"{f.name}.metrics.json"
        if mp.is_file():
            try:
                item["metrics"] = json.loads(mp.read_text()).get("metrics")
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
