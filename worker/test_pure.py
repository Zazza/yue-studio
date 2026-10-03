"""Тесты чистых функций воркера: llm, abcparse, media.encode_mp3.

Запуск: python3 -m unittest worker.test_pure (из корня репозитория)
или: cd worker && python3 -m unittest test_pure
"""
import time
import contextlib
import unittest
from pathlib import Path

import abcparse
import arc
import llm


class TestAdaptPrompts(unittest.TestCase):
    """Спецификация llm.adapt_prompts: системный промпт фиксирует язык и
    требование сохранения просодии, user — исходный текст без изменений."""

    def test_system_mentions_language_and_prosody(self):
        system, user = llm.adapt_prompts("la la", "Russian")
        self.assertIn("Russian", system)
        self.assertIn("syllable", system)
        self.assertIn("[Verse]", system)  # секционные теги не переводить

    def test_user_is_source_text_trimmed(self):
        system, user = llm.adapt_prompts("  hello \n world \n", "Russian")
        self.assertEqual(user, "hello \n world")
        self.assertNotIn("hello", system)


# CI-джоба worker ставит только ruff: аудио-зависимости (numpy/soundfile/
# lameenc) есть на GPU-машине — там тест и работает, в CI пропускается.
try:
    import lameenc  # noqa: F401
    import numpy  # noqa: F401
    import soundfile  # noqa: F401
    _HAS_MP3_DEPS = True
except ImportError:
    _HAS_MP3_DEPS = False

# numpy отдельно: он есть и в CI (fastapi httpx numpy) — чистая математика
# волн должна гоняться там всегда.
try:
    import numpy  # noqa: F401
    _HAS_NUMPY = True
except ImportError:
    _HAS_NUMPY = False


@unittest.skipUnless(_HAS_MP3_DEPS, "нужны numpy/soundfile/lameenc (GPU-окружение)")
class TestEncodeMp3(unittest.TestCase):
    """Спецификация media.encode_mp3: каналы идут interleaved — левый сигнал
    остаётся слева, правый справа (planar-буфер давал 2x-ускорение каналов)."""

    def test_channels_interleaved(self):
        import numpy as np
        import soundfile as sf
        from media import encode_mp3
        import tempfile

        sr = 24000
        t = np.arange(sr) / sr
        # левый — тон, правый — тишина
        data = np.stack([np.sin(2 * np.pi * 440 * t), np.zeros_like(t)], axis=1).astype("float32")
        with tempfile.TemporaryDirectory() as td:
            wav = Path(td) / "in.wav"
            mp3 = Path(td) / "out.mp3"
            sf.write(str(wav), data, sr)
            encode_mp3(wav, mp3)
            out, _ = sf.read(str(mp3), dtype="float32")
        self.assertEqual(out.shape[1], 2)
        rms_l = float(np.sqrt((out[:, 0] ** 2).mean()))
        rms_r = float(np.sqrt((out[:, 1] ** 2).mean()))
        self.assertGreater(rms_l, 0.2)   # тон в левом
        self.assertLess(rms_r, 0.02)     # справа тишина


ABC = """X:1
T:arc test
M:4/4
L:1/16
Q:1/4=100
V: Vocal clef=treble name="Vocal Melody" snm="Vocal"
V: Ins clef=treble name="Inst." snm="Ins."
K:Dm
% intro
V: Vocal
z16|
V: Ins
d8z8|
% verse
V: Vocal
"Dm"d2a2g2e2|
V: Ins
d4d4d4d4|
% chorus
V: Vocal
"Dm"d2a2g2e2|
V: Ins
e4e4e4e4|
% interlude
V: Ins
f4f4f4f4|
% chorus
V: Vocal
"Dm"d2a2g2e2|
V: Ins
g4g4g4g4|
"""


class TestApplyArc(unittest.TestCase):
    """Спецификация arc.apply_arc: дуги темпа по секциям от базового Q,
    burst поднимает голос на октаву в финальном припеве."""

    def test_no_arc_unchanged(self):
        self.assertEqual(arc.apply_arc(ABC, ""), ABC)
        self.assertEqual(arc.apply_arc(ABC, "nope"), ABC)

    def test_burst_tempos_and_octave(self):
        out = arc.apply_arc(ABC, "burst")
        qs = [ln for ln in out.split("\n") if ln.startswith("Q:1/4=")]
        self.assertIn("Q:1/4=55", qs)   # intro 0.55
        self.assertIn("Q:1/4=72", qs)   # interlude 0.72
        self.assertIn("Q:1/4=117", qs)  # финальный chorus 1.17
        # голос в финальном припеве поднялся: d2 -> d'2
        self.assertIn("d'2a'2g'2e'2", out)
        # ранние секции не транспонированы (куплет + первый припев)
        self.assertEqual(out.count("d2a2g2e2"), 2)

    def test_build_lifts_only_finale(self):
        out = arc.apply_arc(ABC, "build")
        qs = [ln for ln in out.split("\n") if ln.startswith("Q:1/4=")]
        self.assertIn("Q:1/4=85", qs)   # intro 0.85
        self.assertIn("Q:1/4=112", qs)  # финальный chorus 1.12
        self.assertNotIn("d'2", out)    # голос не трогаем

    def test_style_suffix(self):
        s = arc.style_with_arc("post-punk", "burst")
        self.assertTrue(s.startswith("post-punk, "))
        self.assertIn("soaring", s)
        # повторное применение не дублирует
        self.assertEqual(arc.style_with_arc(s, "burst"), s)


class TestStripMd(unittest.TestCase):
    def test_removes_code_fences(self):
        self.assertEqual(llm.strip_md("```text\nкуплет\n```"), "куплет")

    def test_removes_fences_with_lang(self):
        self.assertEqual(llm.strip_md("```python\nprint(1)\n```"), "print(1)")

    def test_plain_text_untouched(self):
        self.assertEqual(llm.strip_md("  обычный текст  "), "обычный текст")

    def test_inner_fences_kept(self):
        # spec: срезаются только заборы в начале/конце ответа модели
        self.assertEqual(llm.strip_md("до ``` внутрь ``` после"), "до ``` внутрь ``` после")

    def test_empty(self):
        self.assertEqual(llm.strip_md(""), "")


SAMPLE_ABC = """\
X:1
T:demo
M:4/4
L:1/8
Q:1/4=120
V:Vocal
% verse
"Am" A2 B c2 | "D" z4 d2 |
V:Ins
E2 F G2 | A4 z2 |
K:G
"""


class TestParseAbc(unittest.TestCase):
    def setUp(self):
        self.r = abcparse.parse_abc(SAMPLE_ABC)

    def test_header_fields(self):
        self.assertEqual(self.r["tempo_bpm"], 120.0)
        self.assertEqual(self.r["key"], "G")
        self.assertEqual(self.r["meter"], "4/4")

    def test_voice_order(self):
        self.assertEqual(self.r["voice_order"], ["Vocal", "Ins"])

    def test_bars_counted_by_pipes(self):
        # spec: каждый | закрывает такт — по 2 такта на голос
        self.assertEqual(len(self.r["bars"]), 4)
        self.assertEqual([b["idx"] for b in self.r["bars"]], [0, 1, 2, 3])

    def test_chords_extracted(self):
        self.assertEqual(self.r["bars"][0]["chords"], ["Am"])
        self.assertEqual(self.r["bars"][1]["chords"], ["D"])

    def test_rests_counted(self):
        # второй такт вокала: z4 → 4 единицы паузы
        self.assertEqual(self.r["bars"][1]["rests"]["Vocal"], 4)

    def test_section_comment(self):
        self.assertEqual(self.r["bars"][0]["section"], "verse")

    def test_timeline_per_voice(self):
        # spec: у каждого голоса свой ход часов — голоса звучат одновременно,
        # хотя в тексте их такты идут последовательными блоками
        bars = self.r["bars"]
        self.assertEqual((bars[0]["start_sec"], bars[0]["end_sec"]), (0.0, 1.25))   # Vocal 1
        self.assertEqual((bars[1]["start_sec"], bars[1]["end_sec"]), (1.25, 2.75))  # Vocal 2
        self.assertEqual((bars[2]["start_sec"], bars[2]["end_sec"]), (0.0, 1.25))   # Ins 1 — параллельно
        self.assertEqual((bars[3]["start_sec"], bars[3]["end_sec"]), (1.25, 2.75))  # Ins 2
        self.assertEqual(self.r["duration_sec"], 2.75)

    def test_chords_not_counted_as_notes(self):
        # spec: "Am" — аннотация, в такте 3 ноты: A2 B c2
        self.assertEqual(self.r["bars"][0]["voices"]["Vocal"], 3)

    def test_timing_math(self):
        # M:4/4, L:1/8, Q=120: единица = 0.25 с; такт A2 B c2 = 5 единиц = 1.25 с
        first = self.r["bars"][0]
        self.assertEqual(first["start_sec"], 0.0)
        self.assertEqual(first["end_sec"], 1.25)

    def test_empty_input(self):
        r = abcparse.parse_abc("")
        self.assertEqual(r["bars"], [])
        self.assertEqual(r["duration_sec"], 0.0)
        self.assertEqual(r["tempo_bpm"], 120.0)  # дефолт


# мультипауза Z<n> = n тактов тишины (карточка internal-studio-insert-sync, п.8):
# 4/4, L:1/16, Q=120 → такт 2 с. Секция A: Vocal 4 такта нот, Ins — Z4;
# секция B: оба голоса по 2 такта нот. Оба голоса заканчиваются на 12 с.
MULTIREST_ABC = """\
X:1
M:4/4
L:1/16
Q:1/4=120
V: Vocal clef=treble name="Vocal Melody" snm="Vocal"
V: Ins clef=treble name="Ins Melody" snm="Inst."
K:Em
% sectionA
V: Vocal
"Em"B4B2B2B4B2B2|"C"c2c2c2c2"D"B2A2A4|"Em"e4e4e4e4|"G"d4d4d4d4|
V: Ins
Z4|
% sectionB
V: Vocal
"Am"a4a4a4a4|"C"g4g4g4g4|
V: Ins
E4E4E4E4|F4F4F4F4|
"""


def _bars_of(result: dict, voice: str) -> list:
    """Такты голоса: голос такта — ключ в voices или rests (как на ролле)."""
    return [b for b in result["bars"]
            if voice in (b.get("voices") or {}) or voice in (b.get("rests") or {})]


def _single(voice: str, body: str) -> dict:
    return abcparse.parse_abc(
        "X:1\nM:4/4\nL:1/16\nQ:1/4=120\nK:C\n% s\nV: " + voice + "\n" + body + "\n")


class TestParseAbcMultiRest(unittest.TestCase):
    """Спецификация: Z<n> — n целых тактов паузы, Z — один такт; время голоса
    идёт по всем n тактам (иначе квадратики ролла уезжают раньше звука)."""

    def setUp(self):
        self.r = abcparse.parse_abc(MULTIREST_ABC)

    def test_ins_has_six_bars(self):
        self.assertEqual(len(_bars_of(self.r, "Ins")), 6)
        self.assertEqual(len(_bars_of(self.r, "Vocal")), 6)

    def test_ins_ends_with_vocal(self):
        ins = _bars_of(self.r, "Ins")
        voc = _bars_of(self.r, "Vocal")
        self.assertAlmostEqual(ins[-1]["end_sec"], 12.0, places=6)
        self.assertAlmostEqual(voc[-1]["end_sec"], 12.0, places=6)
        self.assertAlmostEqual(self.r["duration_sec"], 12.0, places=6)

    def test_multirest_bars_are_whole_bars(self):
        ins = _bars_of(self.r, "Ins")
        spans = [(round(b["start_sec"], 6), round(b["end_sec"], 6)) for b in ins]
        self.assertEqual(spans, [(0.0, 2.0), (2.0, 4.0), (4.0, 6.0), (6.0, 8.0),
                                 (8.0, 10.0), (10.0, 12.0)])

    def test_multirest_bars_are_rests(self):
        ins = _bars_of(self.r, "Ins")
        for b in ins[:4]:
            self.assertEqual((b.get("voices") or {}).get("Ins", 0), 0)
            self.assertEqual(b["section"], "sectionA")
        # такты с нотами после мультипаузы — ноты считаются
        self.assertEqual(ins[4]["voices"]["Ins"], 4)
        self.assertEqual(ins[4]["section"], "sectionB")

    def test_bare_z_is_one_bar(self):
        r = _single("Ins", "Z|")
        bars = _bars_of(r, "Ins")
        self.assertEqual(len(bars), 1)
        self.assertAlmostEqual(bars[0]["end_sec"], 2.0, places=6)

    def test_z2_is_two_bars(self):
        r = _single("Ins", "Z2|")
        bars = _bars_of(r, "Ins")
        self.assertEqual(len(bars), 2)
        self.assertAlmostEqual(bars[-1]["end_sec"], 4.0, places=6)


if __name__ == "__main__":
    unittest.main()


# --- Спецификация 1.7: продолжение с места, версии песни (head) -------------
# Воркер импортируется целиком: нужны fastapi/pydantic/numpy (+httpx для
# TestClient). В CI их нет — классы пропускаются, как TestEncodeMp3.
try:
    import fastapi  # noqa: F401
    import httpx  # noqa: F401
    import numpy  # noqa: F401
    _HAS_WORKER_DEPS = True
except ImportError:
    _HAS_WORKER_DEPS = False

_SEM_TOK_PER_SEC_SPEC = 25  # спецификация: 25 семантических токенов в секунду


def _import_worker(tmp: str):
    """Импорт воркера с данными во временном каталоге: при импорте он создаёт
    БД в YUE_DATA_DIR — домашний каталог трогать нельзя."""
    import os
    os.environ["YUE_DATA_DIR"] = tmp
    import yue_worker
    return yue_worker


class _WorkerDbCase(unittest.TestCase):
    """Каждый тест — своя свежая БД и каталог джоб (DB_PATH/JOBS_DIR
    подменяются на временные), модель не грузится: startup-события не
    запускаются, TestClient создаётся без контекст-менеджера."""

    def setUp(self):
        import tempfile
        from unittest import mock
        self._td = tempfile.TemporaryDirectory()
        self.addCleanup(self._td.cleanup)
        tmp = Path(self._td.name)
        self.w = _import_worker(str(tmp))
        self.db_path = tmp / "yue.db"
        self.jobs_dir = tmp / "jobs"
        self.jobs_dir.mkdir(exist_ok=True)  # импорт воркера мог уже создать
        for name, val in (("DB_PATH", self.db_path), ("JOBS_DIR", self.jobs_dir)):
            p = mock.patch.object(self.w, name, val)
            p.start()
            self.addCleanup(p.stop)

    @contextlib.contextmanager
    def _conn(self):
        """Соединение на блок `with`: фиксация/откат как у sqlite3 и закрытие
        (контекст sqlite3 сам не закрывает — ResourceWarning в каждом тесте)."""
        import sqlite3
        c = sqlite3.connect(self.db_path)
        c.row_factory = sqlite3.Row
        try:
            with c:
                yield c
        finally:
            c.close()

    def _cols(self):
        with self._conn() as c:
            return {r[1] for r in c.execute("PRAGMA table_info(jobs)")}


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestMigrateContinueColumns(_WorkerDbCase):
    """_migrate(): добавляет parent_id, role, head_id, cont_from; повторный
    запуск не падает; старые строки сохраняются."""

    NEW_COLS = {"parent_id", "role", "head_id", "cont_from"}

    def _legacy_db(self):
        # БД «до 1.7»: таблица jobs без новых колонок, одна готовая песня
        with self._conn() as c:
            c.execute("""CREATE TABLE jobs (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                title TEXT DEFAULT '', status TEXT NOT NULL DEFAULT 'queued',
                style TEXT NOT NULL, lyrics TEXT NOT NULL, seed INTEGER,
                cot TEXT NOT NULL DEFAULT 'full', error TEXT DEFAULT '',
                duration_sec REAL DEFAULT 0, audio_file TEXT DEFAULT '',
                abc_file TEXT DEFAULT '', created_at TEXT NOT NULL,
                finished_at TEXT DEFAULT '')""")
            c.execute("INSERT INTO jobs (title, status, style, lyrics, seed, created_at) "
                      "VALUES ('old song', 'done', 'dark rock', '[verse] la', 7, '2026-01-01T00:00:00')")

    def test_adds_new_columns(self):
        self._legacy_db()
        self.assertFalse(self.NEW_COLS & self._cols())
        self.w._migrate()
        self.assertTrue(self.NEW_COLS <= self._cols(), self.NEW_COLS - self._cols())

    def test_idempotent(self):
        self._legacy_db()
        self.w._migrate()
        cols_once = self._cols()
        self.w._migrate()  # не должно бросить «duplicate column»
        self.assertEqual(self._cols(), cols_once)

    def test_old_rows_preserved(self):
        self._legacy_db()
        self.w._migrate()
        self.w._migrate()
        with self._conn() as c:
            rows = c.execute("SELECT * FROM jobs").fetchall()
        self.assertEqual(len(rows), 1)
        r = rows[0]
        self.assertEqual((r["title"], r["status"], r["style"], r["lyrics"], r["seed"]),
                         ("old song", "done", "dark rock", "[verse] la", 7))
        self.assertIsNone(r["parent_id"])  # старая песня — корень, а не производная


class _WorkerApiCase(_WorkerDbCase):
    def setUp(self):
        super().setUp()
        from fastapi.testclient import TestClient
        self.w.init_db()
        self.w._migrate()
        self.client = TestClient(self.w.app, raise_server_exceptions=False)

    def _job(self, style="dark rock", duration=60.0, parent_id=None, role="",
             semantic=True, status="done"):
        with self._conn() as c:
            cur = c.execute(
                "INSERT INTO jobs (title, status, style, lyrics, duration_sec, parent_id, role, created_at) "
                "VALUES ('t', ?, ?, '[verse] la la', ?, ?, ?, '2026-01-01T00:00:00')",
                (status, style, duration, parent_id, role))
            jid = cur.lastrowid
        if semantic:
            import numpy as np
            d = self.jobs_dir / str(jid)
            d.mkdir(parents=True, exist_ok=True)
            np.save(d / "semantic.npy",
                    np.arange(int(duration * _SEM_TOK_PER_SEC_SPEC), dtype=np.int64))
        return jid

    def _children(self, parent):
        with self._conn() as c:
            return c.execute("SELECT * FROM jobs WHERE parent_id=?", (parent,)).fetchall()

    def _row(self, jid):
        with self._conn() as c:
            return c.execute("SELECT * FROM jobs WHERE id=?", (jid,)).fetchone()


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestContinueJob(_WorkerApiCase):
    """POST /jobs/{id}/continue: новая джоба parent_id=id, role='continue',
    cont_from=отметка; стиль = стиль родителя + приписка."""

    def test_creates_continuation(self):
        parent = self._job()
        r = self.client.post(f"/jobs/{parent}/continue", json={"from_sec": 12.5})
        self.assertLess(r.status_code, 300, r.text)
        kids = self._children(parent)
        self.assertEqual(len(kids), 1)
        k = kids[0]
        self.assertEqual(k["role"], "continue")
        self.assertAlmostEqual(k["cont_from"], 12.5)
        self.assertNotEqual(k["id"], parent)

    def test_style_add_reaches_new_job_style(self):
        parent = self._job(style="dark rock, male baritone")
        r = self.client.post(f"/jobs/{parent}/continue",
                             json={"from_sec": 10, "style_add": "electric guitar enters and builds"})
        self.assertLess(r.status_code, 300, r.text)
        style = self._children(parent)[0]["style"]
        self.assertIn("dark rock, male baritone", style)
        self.assertIn("electric guitar enters and builds", style)

    def test_without_style_add_style_is_parents(self):
        parent = self._job(style="dark rock")
        self.client.post(f"/jobs/{parent}/continue", json={"from_sec": 10})
        self.assertEqual(self._children(parent)[0]["style"].strip(), "dark rock")

    def test_parent_not_found_404(self):
        r = self.client.post("/jobs/9999/continue", json={"from_sec": 10})
        self.assertEqual(r.status_code, 404)
        self.assertEqual(self._children(9999), [])

    def test_parent_without_semantic_4xx(self):
        parent = self._job(semantic=False, status="running")
        r = self.client.post(f"/jobs/{parent}/continue", json={"from_sec": 10})
        self.assertTrue(400 <= r.status_code < 500, r.status_code)
        self.assertEqual(self._children(parent), [])

    def test_mark_zero_or_negative_4xx(self):
        parent = self._job()
        for mark in (0, -5):
            r = self.client.post(f"/jobs/{parent}/continue", json={"from_sec": mark})
            self.assertTrue(400 <= r.status_code < 500, (mark, r.status_code))
        self.assertEqual(self._children(parent), [])

    def test_mark_at_or_beyond_length_not_500(self):
        # спецификация допускает 4xx или обрезку; обрезка — отметка < длины
        parent = self._job(duration=60.0)
        for mark in (60.0, 500.0):
            before = {k["id"] for k in self._children(parent)}
            r = self.client.post(f"/jobs/{parent}/continue", json={"from_sec": mark})
            self.assertLess(r.status_code, 500, (mark, r.text))
            if r.status_code < 300:
                new = [k for k in self._children(parent) if k["id"] not in before]
                self.assertEqual(len(new), 1)
                self.assertGreater(new[0]["cont_from"], 0)
                self.assertLess(new[0]["cont_from"], 60.0, mark)
            else:
                self.assertTrue(400 <= r.status_code < 500, (mark, r.status_code))


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestContSteps(unittest.TestCase):
    """_cont_steps(cont_from, n_tokens): сыгранных шагов модели =
    round(cont_from × 25), в пределах [1, n_tokens]. Функцию ещё предстоит
    выделить из _continue_song — до этого тест красный."""

    def setUp(self):
        import tempfile
        self._td = tempfile.TemporaryDirectory()
        self.addCleanup(self._td.cleanup)
        self.w = _import_worker(self._td.name)

    def test_rate_constant(self):
        self.assertEqual(self.w.SEM_TOK_PER_SEC, _SEM_TOK_PER_SEC_SPEC)

    def test_exact_seconds(self):
        self.assertEqual(self.w._cont_steps(10, 1000), 250)

    def test_rounding(self):
        self.assertEqual(self.w._cont_steps(2.03, 1000), 51)   # 50.75 → 51
        self.assertEqual(self.w._cont_steps(2.01, 1000), 50)   # 50.25 → 50

    def test_clamped_to_token_count(self):
        self.assertEqual(self.w._cont_steps(100, 1000), 1000)
        self.assertEqual(self.w._cont_steps(40, 1000), 1000)   # ровно 1000

    def test_at_least_one(self):
        self.assertEqual(self.w._cont_steps(0.001, 1000), 1)
        self.assertEqual(self.w._cont_steps(0, 1000), 1)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestSetHead(_WorkerApiCase):
    """POST /jobs/{id}/head: у корня head_id = выбранная версия; версия
    обязана быть производной этого корня; сброс на сам корень допустим."""

    def test_sets_head_to_derived_version(self):
        root = self._job()
        v = self._job(parent_id=root, role="continue")
        r = self.client.post(f"/jobs/{root}/head", json={"head_id": v})
        self.assertLess(r.status_code, 300, r.text)
        self.assertEqual(self._row(root)["head_id"], v)

    def test_foreign_version_rejected(self):
        root = self._job()
        other_root = self._job()
        foreign = self._job(parent_id=other_root, role="continue")
        for bad in (other_root, foreign):
            r = self.client.post(f"/jobs/{root}/head", json={"head_id": bad})
            self.assertTrue(400 <= r.status_code < 500, (bad, r.status_code))
        self.assertIn(self._row(root)["head_id"], (None, 0, root))

    def test_missing_version_rejected(self):
        root = self._job()
        r = self.client.post(f"/jobs/{root}/head", json={"head_id": 9999})
        self.assertTrue(400 <= r.status_code < 500, r.status_code)

    def test_reset_to_root(self):
        root = self._job()
        v = self._job(parent_id=root, role="continue")
        self.client.post(f"/jobs/{root}/head", json={"head_id": v})
        for reset in ({"head_id": root}, {"head_id": None}, {"head_id": 0}):
            self.client.post(f"/jobs/{root}/head", json={"head_id": v})
            r = self.client.post(f"/jobs/{root}/head", json=reset)
            self.assertLess(r.status_code, 300, (reset, r.text))
            self.assertIn(self._row(root)["head_id"], (None, 0, root), reset)

    def test_root_not_found_404(self):
        r = self.client.post("/jobs/9999/head", json={"head_id": None})
        self.assertEqual(r.status_code, 404)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestContinueOnlyViaEndpoint(_WorkerApiCase):
    """role=continue создаёт только POST /jobs/{id}/continue: обычный POST /jobs
    с такой ролью обошёл бы проверки semantic.npy и отметки."""

    def test_post_jobs_rejects_continue_role(self):
        parent = self._job()
        r = self.client.post("/jobs", json={"style": "rock", "lyrics": "[verse] la",
                                            "parent_id": parent, "role": "continue"})
        self.assertEqual(r.status_code, 422)
        self.assertEqual(self._children(parent), [])


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestCopyFailureRollsBack(_WorkerApiCase):
    """Сбой копирования файла не оставляет запись без аудио."""

    def setUp(self):
        super().setUp()
        from unittest import mock
        p = mock.patch.object(self.w, "VOICES_DIR", Path(self._td.name) / "voices")
        p.start()
        self.addCleanup(p.stop)

    def _fail_copy(self):
        from unittest import mock
        return mock.patch("shutil.copy2", side_effect=OSError("disk full"))

    def test_variant_track(self):
        parent = self._job()
        (self.jobs_dir / str(parent) / "dsp-wall.flac").write_bytes(b"x")
        with self._fail_copy():
            r = self.client.post(f"/jobs/{parent}/variant_track", json={"file": "dsp-wall.flac"})
        self.assertGreaterEqual(r.status_code, 500)
        self.assertEqual(self._children(parent), [])

    def test_voice_create(self):
        job = self._job()
        (self.jobs_dir / str(job) / "audio.flac").write_bytes(b"x")
        with self._fail_copy():
            r = self.client.post("/voices", json={"name": "v", "job_id": job})
        self.assertGreaterEqual(r.status_code, 500)
        with self._conn() as c:
            self.assertEqual(c.execute("SELECT COUNT(*) FROM voices").fetchone()[0], 0)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestProgressFinalize(_WorkerDbCase):
    """Стадия «сохранение…» (finalize) не затирается сторожем прогресса: он
    берёт стадию из counters, а не держит «semantic»/«decode»."""

    def test_watcher_keeps_finalize(self):
        counters = {"phase": "finalize", "tokens": 500, "plan_end": 100}
        stop = self.w._progress_watcher(777, counters, 0.0)
        try:
            for _ in range(50):
                if 777 in self.w._progress:
                    break
                time.sleep(0.02)
            self.assertEqual(self.w._progress[777]["stage"], "finalize")
        finally:
            stop.set()


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestMigrateVoiceSrc(TestMigrateContinueColumns):
    """_migrate(): добавляет в jobs колонку voice_src (INTEGER, по умолчанию
    NULL); повторный запуск не падает; старые строки сохраняются."""

    NEW_COLS = {"voice_src"}

    def test_voice_src_integer_default_null(self):
        self._legacy_db()
        self.w._migrate()
        with self._conn() as c:
            info = {r[1]: r for r in c.execute("PRAGMA table_info(jobs)")}
            old = c.execute("SELECT voice_src FROM jobs").fetchone()
        self.assertIn("voice_src", info)
        self.assertEqual(info["voice_src"][2].upper(), "INTEGER")
        self.assertIsNone(info["voice_src"][4])  # dflt_value: без DEFAULT → NULL
        self.assertIsNone(old["voice_src"])      # у старой строки — NULL


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestVariantTrackVoiceSrc(_WorkerApiCase):
    """POST /jobs/{id}/variant_track: необязательное voice_src (int) попадает
    в новую джобу; без поля — NULL; ссылка на несуществующую джобу → 422."""

    def _parent_with_variant(self):
        parent = self._job()
        (self.jobs_dir / str(parent) / "dsp-wall.flac").write_bytes(b"x")
        return parent

    def test_voice_src_stored(self):
        parent = self._parent_with_variant()
        src = self._job()
        r = self.client.post(f"/jobs/{parent}/variant_track",
                             json={"file": "dsp-wall.flac", "voice_src": src})
        self.assertLess(r.status_code, 300, r.text)
        kids = self._children(parent)
        self.assertEqual(len(kids), 1)
        self.assertEqual(kids[0]["voice_src"], src)

    def test_without_voice_src_null(self):
        parent = self._parent_with_variant()
        r = self.client.post(f"/jobs/{parent}/variant_track", json={"file": "dsp-wall.flac"})
        self.assertLess(r.status_code, 300, r.text)
        kids = self._children(parent)
        self.assertEqual(len(kids), 1)
        self.assertIsNone(kids[0]["voice_src"])

    def test_missing_voice_src_job_422(self):
        parent = self._parent_with_variant()
        with self._conn() as c:
            before = c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0]
        r = self.client.post(f"/jobs/{parent}/variant_track",
                             json={"file": "dsp-wall.flac", "voice_src": 9999})
        self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(self._children(parent), [])
        with self._conn() as c:
            self.assertEqual(c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0], before)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestListJobsVoiceSrc(_WorkerApiCase):
    """GET /jobs отдаёт voice_src у каждой джобы; у старых — null."""

    def test_old_jobs_have_null_voice_src(self):
        a, b = self._job(), self._job()
        r = self.client.get("/jobs")
        self.assertEqual(r.status_code, 200, r.text)
        jobs = {j["id"]: j for j in r.json()}
        for jid in (a, b):
            self.assertIn("voice_src", jobs[jid])
            self.assertIsNone(jobs[jid]["voice_src"])

    def test_variant_voice_src_in_list(self):
        parent = self._job()
        (self.jobs_dir / str(parent) / "dsp-wall.flac").write_bytes(b"x")
        src = self._job()
        r = self.client.post(f"/jobs/{parent}/variant_track",
                             json={"file": "dsp-wall.flac", "voice_src": src})
        self.assertLess(r.status_code, 300, r.text)
        kid = self._children(parent)[0]["id"]
        jobs = {j["id"]: j for j in self.client.get("/jobs").json()}
        self.assertEqual(jobs[kid]["voice_src"], src)
        for jid in (parent, src):
            self.assertIn("voice_src", jobs[jid])
            self.assertIsNone(jobs[jid]["voice_src"])


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestListJobsWholeLibrary(_WorkerApiCase):
    """/jobs отдаёт всю библиотеку, а не 100 последних: иначе старый корень
    пропадает из списка (версии без родителя, «перепеть» теряет дубли)."""

    def test_more_than_100(self):
        ids = [self._job(semantic=False) for _ in range(130)]
        got = {j["id"] for j in self.client.get("/jobs").json()}
        self.assertIn(ids[0], got)
        self.assertEqual(len(got), 130)


# --- Спецификация: высота голоса по тактам (vocal_contour) -----------------
# Чистые функции живут в воркере, а его импорт требует fastapi/numpy —
# поэтому тот же skipUnless, что у соседних классов. librosa не нужна.

@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestNoteName(unittest.TestCase):
    """_note_name(hz): ближайшая нота равномерного строя, A4 = 440 Гц,
    научная запись с диезами; нет высоты → «·»."""

    def setUp(self):
        import tempfile
        self._td = tempfile.TemporaryDirectory()
        self.addCleanup(self._td.cleanup)
        self.w = _import_worker(self._td.name)

    def test_reference_notes(self):
        for hz, name in ((440.0, "A4"), (261.63, "C4"), (293.66, "D4"),
                         (246.94, "B3"), (466.16, "A#4")):
            with self.subTest(hz=hz):
                self.assertEqual(self.w._note_name(hz), name)

    def test_octave_boundary_b3_c4(self):
        # B3 и C4 — соседние ноты в разных октавах: номер октавы меняется на C
        self.assertEqual(self.w._note_name(246.94), "B3")
        self.assertEqual(self.w._note_name(261.63), "C4")

    def test_nearest_note_not_floor(self):
        # 450 Гц ближе к A4 (440), чем к A#4 (466.16); 460 — ближе к A#4
        self.assertEqual(self.w._note_name(450.0), "A4")
        self.assertEqual(self.w._note_name(460.0), "A#4")

    def test_other_octaves(self):
        self.assertEqual(self.w._note_name(110.0), "A2")
        self.assertEqual(self.w._note_name(880.0), "A5")

    def test_no_pitch(self):
        for hz in (0, 0.0, float("nan"), -440.0):
            with self.subTest(hz=hz):
                self.assertEqual(self.w._note_name(hz), "·")


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestContourBars(unittest.TestCase):
    """_contour_bars(f0, hop_sec, bars, from_s, to_s): такты, пересекающиеся
    с [from_s, to_s); в каждом — 4 равные четверти, медиана f0 по голосным
    кадрам → нота; меньше 3 голосных кадров → «·»."""

    HOP = 0.01  # кадр i — момент i*0.01 с; четверть такта 1 с = 25 кадров
    A4, C4, D4, B3 = 440.0, 261.63, 293.66, 246.94

    def setUp(self):
        import tempfile
        self._td = tempfile.TemporaryDirectory()
        self.addCleanup(self._td.cleanup)
        self.w = _import_worker(self._td.name)

    @staticmethod
    def _bars(n, length=1.0, start=0.0):
        return [{"start_sec": start + i * length, "end_sec": start + (i + 1) * length}
                for i in range(n)]

    def _f0(self, seconds, fill=float("nan")):
        import numpy as np
        return np.full(int(round(seconds / self.HOP)), fill, dtype=float)

    def _set(self, f0, t0, t1, hz):
        """Заполнить кадры с моментом в [t0, t1) частотой hz."""
        i0, i1 = int(round(t0 / self.HOP)), int(round(t1 / self.HOP))
        f0[i0:i1] = hz

    def test_four_quarters_notes(self):
        f0 = self._f0(1.0)
        self._set(f0, 0.00, 0.25, self.A4)
        self._set(f0, 0.25, 0.50, self.C4)
        self._set(f0, 0.50, 0.75, self.D4)
        self._set(f0, 0.75, 1.00, self.B3)
        res = self.w._contour_bars(f0, self.HOP, self._bars(1), 0.0, 1.0)
        self.assertEqual(len(res), 1)
        self.assertEqual(res[0]["index"], 0)
        self.assertAlmostEqual(res[0]["start"], 0.0)
        self.assertAlmostEqual(res[0]["end"], 1.0)
        self.assertEqual(res[0]["notes"], ["A4", "C4", "D4", "B3"])

    def test_median_not_mean(self):
        # 20 кадров A4 + 5 кадров A5: медиана → A4, среднее (528 Гц) дало бы C5
        f0 = self._f0(1.0)
        self._set(f0, 0.00, 0.20, self.A4)
        self._set(f0, 0.20, 0.25, 880.0)
        res = self.w._contour_bars(f0, self.HOP, self._bars(1), 0.0, 1.0)
        self.assertEqual(res[0]["notes"][0], "A4")

    def test_unvoiced_frames_ignored_in_median(self):
        # 5 голосных кадров A4 посреди четверти, остальное NaN → A4, а не «·»
        f0 = self._f0(1.0)
        self._set(f0, 0.10, 0.15, self.A4)
        res = self.w._contour_bars(f0, self.HOP, self._bars(1), 0.0, 1.0)
        self.assertEqual(res[0]["notes"], ["A4", "·", "·", "·"])

    def test_fewer_than_three_voiced_is_dot(self):
        f0 = self._f0(1.0)
        self._set(f0, 0.10, 0.12, self.A4)   # 2 кадра → «·»
        self._set(f0, 0.35, 0.38, self.C4)   # 3 кадра → нота
        res = self.w._contour_bars(f0, self.HOP, self._bars(1), 0.0, 1.0)
        self.assertEqual(res[0]["notes"][:2], ["·", "C4"])

    def test_quarters_follow_bar_length(self):
        # такт 2 с, начинается с 1 с: четверти по 0.5 с
        f0 = self._f0(3.0)
        self._set(f0, 1.0, 1.5, self.A4)
        self._set(f0, 1.5, 2.0, self.C4)
        self._set(f0, 2.0, 2.5, self.D4)
        self._set(f0, 2.5, 3.0, self.B3)
        bars = [{"start_sec": 0.0, "end_sec": 1.0}, {"start_sec": 1.0, "end_sec": 3.0}]
        res = self.w._contour_bars(f0, self.HOP, bars, 1.0, 3.0)
        self.assertEqual([b["index"] for b in res], [1])
        self.assertEqual(res[0]["notes"], ["A4", "C4", "D4", "B3"])

    def test_range_selects_overlapping_bars_with_indexes(self):
        f0 = self._f0(5.0, self.A4)
        res = self.w._contour_bars(f0, self.HOP, self._bars(5), 1.5, 3.2)
        # пересекают [1.5, 3.2): такты 1 (1–2), 2 (2–3), 3 (3–4)
        self.assertEqual([b["index"] for b in res], [1, 2, 3])
        self.assertEqual([(b["start"], b["end"]) for b in res],
                         [(1.0, 2.0), (2.0, 3.0), (3.0, 4.0)])

    def test_range_half_open(self):
        # такт, кончающийся ровно на from, и такт, начинающийся ровно на to,
        # с полуинтервалом [from, to) не пересекаются
        f0 = self._f0(5.0, self.A4)
        res = self.w._contour_bars(f0, self.HOP, self._bars(5), 1.0, 3.0)
        self.assertEqual([b["index"] for b in res], [1, 2])

    def test_index_is_position_in_bars_not_in_result(self):
        f0 = self._f0(10.0, self.A4)
        bars = self._bars(10)
        res = self.w._contour_bars(f0, self.HOP, bars, 7.0, 8.0)
        self.assertEqual(len(res), 1)
        self.assertEqual(res[0]["index"], 7)
        self.assertEqual(res[0]["notes"], ["A4"] * 4)

    def test_no_bars_in_range_empty(self):
        f0 = self._f0(3.0, self.A4)
        self.assertEqual(self.w._contour_bars(f0, self.HOP, self._bars(3), 10.0, 20.0), [])
        self.assertEqual(self.w._contour_bars(f0, self.HOP, [], 0.0, 3.0), [])

    def test_bar_beyond_f0_is_all_dots(self):
        # голос анализирован на 1 с, а такт 2–3 с — кадров нет → «·», не падение
        f0 = self._f0(1.0, self.A4)
        res = self.w._contour_bars(f0, self.HOP, self._bars(3), 2.0, 3.0)
        self.assertEqual(len(res), 1)
        self.assertEqual(res[0]["notes"], ["·"] * 4)

    def test_plain_list_f0(self):
        f0 = [self.A4] * 100
        res = self.w._contour_bars(f0, self.HOP, self._bars(1), 0.0, 1.0)
        self.assertEqual(res[0]["notes"], ["A4"] * 4)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestContourSummary(unittest.TestCase):
    """_contour_summary(f0): медиана, 5-й и 95-й перцентили по голосным
    кадрам, округление до 1 знака; голоса нет → все None."""

    def setUp(self):
        import tempfile
        self._td = tempfile.TemporaryDirectory()
        self.addCleanup(self._td.cleanup)
        self.w = _import_worker(self._td.name)

    def test_percentiles_ignore_unvoiced(self):
        import numpy as np
        voiced = np.arange(100.0, 201.0)          # 101 значение: 100..200
        f0 = np.empty(voiced.size * 2)
        f0[0::2] = voiced
        f0[1::2] = np.nan                           # NaN между голосными кадрами
        s = self.w._contour_summary(f0)
        self.assertEqual(s, {"median_hz": 150.0, "low_hz": 105.0, "high_hz": 195.0})

    def test_rounded_to_one_decimal(self):
        s = self.w._contour_summary([123.456] * 10)
        self.assertEqual(s, {"median_hz": 123.5, "low_hz": 123.5, "high_hz": 123.5})

    def test_no_voice_all_none(self):
        for f0 in ([float("nan")] * 50, []):
            with self.subTest(n=len(f0)):
                self.assertEqual(self.w._contour_summary(f0),
                                 {"median_hz": None, "low_hz": None, "high_hz": None})


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestVocalContourEndpoint(_WorkerApiCase):
    """GET /jobs/{id}/vocal_contour?from=&to=: нет джобы → 404; нет стема
    вокала jobs/<id>/stem-vocals.flac → 409 с подсказкой make_stems.
    Анализ аудио здесь не вызывается."""

    def test_missing_job_404(self):
        r = self.client.get("/jobs/9999/vocal_contour", params={"from": 0, "to": 10})
        self.assertEqual(r.status_code, 404, r.text)

    def test_no_vocal_stem_409(self):
        jid = self._job()
        # другой стем есть, вокального — нет
        (self.jobs_dir / str(jid) / "stem-drums.flac").write_bytes(b"x")
        r = self.client.get(f"/jobs/{jid}/vocal_contour", params={"from": 0, "to": 10})
        self.assertEqual(r.status_code, 409, r.text)
        self.assertIn("make_stems", r.text)

    def test_no_vocal_stem_409_without_range(self):
        jid = self._job(semantic=False)
        r = self.client.get(f"/jobs/{jid}/vocal_contour")
        self.assertEqual(r.status_code, 409, r.text)
        self.assertIn("make_stems", r.text)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestVocalContourRange(_WorkerApiCase):
    """Отметка вне трека — понятная 422, а не 500 из анализа."""

    def _with_stem(self):
        jid = self._job(duration=60.0, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        (d / "stem-vocals.flac").write_bytes(b"x")
        (d / "score.abc").write_text("X:1\nM:4/4\nL:1/16\nK:C\nV: Vocal\nC16|\n")
        return jid

    def test_bad_ranges_422(self):
        jid = self._with_stem()
        for q in ("from=60", "from=500", "from=-1", "from=10&to=5", "from=nan", "to=inf"):
            r = self.client.get(f"/jobs/{jid}/vocal_contour?{q}")
            self.assertEqual(r.status_code, 422, q)


# ---------- Волна громкости и спектрограмма ----------

@unittest.skipUnless(_HAS_NUMPY, "нужен numpy (в CI ставится всегда)")
class TestWaveformPeaks(unittest.TestCase):
    """Спецификация waveform.peaks_from_samples: [min, max] по окнам без
    нормализации (тихое окно тихим и остаётся, файлы сравнимы между собой);
    хвост короче окна паддится, а не теряется."""

    def test_windows_min_max(self):
        import numpy as np
        from waveform import peaks_from_samples
        sr = 1000
        t = np.arange(sr) / sr
        y = np.concatenate([
            0.5 * np.sin(2 * np.pi * 10 * t),    # окно 1: тон ±0.5
            np.zeros(sr),                         # окно 2: тишина
            1.0 * np.sin(2 * np.pi * 10 * t),     # окно 3: тон ±1.0
        ])
        peaks = peaks_from_samples(y, 3)
        self.assertEqual(len(peaks), 3)
        self.assertAlmostEqual(peaks[0][0], -0.5, delta=0.01)
        self.assertAlmostEqual(peaks[0][1], 0.5, delta=0.01)
        self.assertEqual(peaks[1], [0.0, 0.0])
        self.assertAlmostEqual(peaks[2][0], -1.0, delta=0.01)
        self.assertAlmostEqual(peaks[2][1], 1.0, delta=0.01)

    def test_tail_padded_not_lost(self):
        import numpy as np
        from waveform import peaks_from_samples
        y = np.concatenate([np.zeros(100), np.ones(51)])   # 151 сэмпл
        peaks = peaks_from_samples(y, 2)                    # окна по 76 + паддинг
        self.assertEqual(len(peaks), 2)
        self.assertEqual(peaks[0], [0.0, 0.0])
        self.assertEqual(peaks[1], [0.0, 1.0])

    def test_bins_bounds(self):
        from waveform import clamp_bins, default_bins
        self.assertEqual(clamp_bins(5), 100)
        self.assertEqual(clamp_bins(999999), 20000)
        self.assertEqual(default_bins(10), 600)      # 60 окон/с с первых секунд
        self.assertEqual(default_bins(240), 14400)   # 4 минуты → 60 окон/с
        self.assertEqual(default_bins(2400), 20000)  # длинный → максимум


def _ffmpeg_available() -> bool:
    import shutil
    return shutil.which("ffmpeg") is not None


@unittest.skipUnless(_HAS_MP3_DEPS, "нужны numpy/soundfile (GPU-окружение)")
class TestWaveApi(_WorkerApiCase):
    """Спецификация GET /jobs/{id}/peaks: канонический bins кэшируется в
    <файл>.peaks.json (mtime-гейт), явный другой — считается мимо кэша;
    плохое имя файла → 400, нет джобы/файла → 404."""

    def _audio_job(self, seconds=3.0):
        import numpy as np
        import soundfile as sf
        jid = self._job(duration=seconds, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        sr = 24000
        t = np.arange(int(sr * seconds)) / sr
        sf.write(str(d / "audio.flac"), (0.5 * np.sin(2 * np.pi * 440 * t)).astype("float32"), sr)
        return jid

    def test_peaks_canonical(self):
        jid = self._audio_job()
        r = self.client.get(f"/jobs/{jid}/peaks", params={"file": "audio.flac"})
        self.assertEqual(r.status_code, 200, r.text)
        body = r.json()
        self.assertEqual(body["_v"], 2)             # v2 — 60 окон/с
        self.assertEqual(body["file"], "audio.flac")
        self.assertEqual(body["bins"], 500)             # 3 с → минимум 500
        self.assertAlmostEqual(body["duration_sec"], 3.0, places=2)
        self.assertEqual(len(body["peaks"]), 500)
        lo, hi = body["peaks"][0]                       # синус ±0.5 без нормировки
        self.assertGreater(lo, -0.6)
        self.assertLess(lo, -0.4)
        self.assertGreater(hi, 0.4)
        self.assertLess(hi, 0.6)

    def test_canonical_cached_and_reused(self):
        from unittest import mock
        jid = self._audio_job()
        r = self.client.get(f"/jobs/{jid}/peaks", params={"file": "audio.flac"})
        self.assertEqual(r.status_code, 200, r.text)
        cache = self.jobs_dir / str(jid) / "audio.flac.peaks.json"
        self.assertTrue(cache.is_file())
        with mock.patch.object(self.w.waveform, "compute_peaks",
                               side_effect=AssertionError("кэш не используется")):
            r2 = self.client.get(f"/jobs/{jid}/peaks", params={"file": "audio.flac"})
        self.assertEqual(r2.status_code, 200, r2.text)
        self.assertEqual(r2.json(), r.json())

    def test_explicit_bins_not_cached(self):
        jid = self._audio_job()
        r = self.client.get(f"/jobs/{jid}/peaks", params={"file": "audio.flac", "bins": 300})
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json()["bins"], 300)
        self.assertEqual(len(r.json()["peaks"]), 300)
        self.assertFalse((self.jobs_dir / str(jid) / "audio.flac.peaks.json").is_file())

    def test_errors(self):
        jid = self._audio_job()
        for query, code in (
            ({"file": "../x"}, 400),
            ({"file": "a/b.flac"}, 400),
            ({"file": "missing.flac"}, 404),
            ({}, 400),                                   # audio_file в БД пуст
        ):
            with self.subTest(query=query):
                r = self.client.get(f"/jobs/{jid}/peaks", params=query)
                self.assertEqual(r.status_code, code, r.text)
        r = self.client.get("/jobs/9999/peaks")
        self.assertEqual(r.status_code, 404, r.text)


@unittest.skipUnless(_HAS_MP3_DEPS, "нужны numpy/soundfile (GPU-окружение)")
class TestSpectrumApi(_WorkerApiCase):
    """Спецификация GET /jobs/{id}/spectrum.png: PNG с линейной осью времени,
    кэш-сайдикары <файл>.spectrum.png/.json; нет ffmpeg → 503 (волна громкости
    при этом работает — ffmpeg на воркере опционален)."""

    def _job_with_file(self, real_audio: bool):
        jid = self._job(duration=3.0, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        if real_audio:
            import numpy as np
            import soundfile as sf
            sr = 24000
            t = np.arange(sr * 3) / sr
            sf.write(str(d / "audio.flac"), (0.5 * np.sin(2 * np.pi * 440 * t)).astype("float32"), sr)
        else:
            (d / "audio.flac").write_bytes(b"x")
        return jid

    def test_no_ffmpeg_503(self):
        from unittest import mock
        jid = self._job_with_file(real_audio=False)
        with mock.patch("shutil.which", return_value=None):
            r = self.client.get(f"/jobs/{jid}/spectrum.png", params={"file": "audio.flac"})
        self.assertEqual(r.status_code, 503, r.text)

    def test_errors(self):
        jid = self._job_with_file(real_audio=False)
        r = self.client.get(f"/jobs/{jid}/spectrum.png", params={"file": "../x"})
        self.assertEqual(r.status_code, 400, r.text)
        r = self.client.get(f"/jobs/{jid}/spectrum.png", params={"file": "missing.flac"})
        self.assertEqual(r.status_code, 404, r.text)
        r = self.client.get("/jobs/9999/spectrum.png")
        self.assertEqual(r.status_code, 404, r.text)

    @unittest.skipUnless(_ffmpeg_available(), "нужен ffmpeg в PATH")
    def test_png_and_cache(self):
        from unittest import mock
        jid = self._job_with_file(real_audio=True)
        r = self.client.get(f"/jobs/{jid}/spectrum.png", params={"file": "audio.flac"})
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.headers["content-type"], "image/png")
        self.assertTrue(r.content.startswith(b"\x89PNG"), "не PNG-байты")
        d = self.jobs_dir / str(jid)
        self.assertTrue((d / "audio.flac.spectrum.png").is_file())
        self.assertTrue((d / "audio.flac.spectrum.json").is_file())
        with mock.patch.object(self.w.waveform, "render_spectrum_png",
                               side_effect=AssertionError("кэш не используется")):
            r2 = self.client.get(f"/jobs/{jid}/spectrum.png", params={"file": "audio.flac"})
        self.assertEqual(r2.status_code, 200, r2.text)
        self.assertEqual(r2.content, r.content)


# --- Спецификация: «найти свист» — узкие тональные пики в спектре ----------
# Чистая функция живёт в воркере (импорт требует fastapi/numpy), librosa не
# нужна: спектр мощности собирается прямо в numpy.

@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestTonalPeaks(unittest.TestCase):
    """_tonal_peaks(power, freqs, min_hz=1000, max_hz=18000, min_prom_db=12,
    top=5): узкие пики — бин выше медианы окрестности ±~300 Гц (без самого
    пика ±~2 бина) на prominence_db ≥ min_prom_db, в [min_hz, max_hz];
    соседние бины одного пика — один результат; [{"hz", "prominence_db"}]
    по убыванию prominence, не больше top."""

    SR = 48000
    N_BINS = 4097  # шаг ≈ 5.86 Гц: окрестность ±300 Гц ≈ ±51 бин

    def setUp(self):
        import tempfile
        import numpy as np
        self._td = tempfile.TemporaryDirectory()
        self.addCleanup(self._td.cleanup)
        self.w = _import_worker(self._td.name)
        self.freqs = np.linspace(0, self.SR / 2, self.N_BINS)
        self.rng = np.random.default_rng(12345)

    def _flat_db(self):
        # ровный спектр 0 дБ с шумом ±1 дБ
        return self.rng.uniform(-1.0, 1.0, self.N_BINS)

    def _add_peak(self, db, hz, gain_db):
        # узкий пик: центральный бин +gain, соседи −6 дБ от него (один тон
        # размазывается на пару бинов — это всё ещё ОДИН результат)
        import numpy as np
        i = int(np.argmin(np.abs(self.freqs - hz)))
        db[i] += gain_db
        db[i - 1] += gain_db - 6
        db[i + 1] += gain_db - 6
        return db

    @staticmethod
    def _power(db):
        return 10.0 ** (db / 10.0)

    def _run(self, db, **kw):
        return self.w._tonal_peaks(self._power(db), self.freqs, **kw)

    def test_flat_noise_no_peaks(self):
        self.assertEqual(self._run(self._flat_db()), [])

    def test_single_peak_found_once(self):
        res = self._run(self._add_peak(self._flat_db(), 5265, 30))
        self.assertEqual(len(res), 1, res)
        self.assertAlmostEqual(res[0]["hz"], 5265, delta=15)
        self.assertGreaterEqual(res[0]["prominence_db"], 26)
        self.assertLessEqual(res[0]["prominence_db"], 34)

    def test_result_shape_and_rounding(self):
        res = self._run(self._add_peak(self._flat_db(), 5265, 30))
        self.assertEqual(set(res[0].keys()), {"hz", "prominence_db"})
        for k in ("hz", "prominence_db"):
            v = res[0][k]
            self.assertEqual(round(float(v), 1), float(v), f"{k}={v} не округлено")

    def test_two_peaks_sorted_by_prominence(self):
        db = self._add_peak(self._flat_db(), 9000, 20)
        db = self._add_peak(db, 5265, 30)
        res = self._run(db)
        self.assertEqual(len(res), 2, res)
        self.assertAlmostEqual(res[0]["hz"], 5265, delta=15)
        self.assertAlmostEqual(res[1]["hz"], 9000, delta=15)
        self.assertGreater(res[0]["prominence_db"], res[1]["prominence_db"])

    def test_weak_peak_below_threshold(self):
        self.assertEqual(self._run(self._add_peak(self._flat_db(), 5265, 8)), [])

    def test_peak_below_min_hz_ignored(self):
        self.assertEqual(self._run(self._add_peak(self._flat_db(), 500, 30)), [])

    def test_peak_above_max_hz_ignored(self):
        self.assertEqual(self._run(self._add_peak(self._flat_db(), 20000, 30)), [])

    def test_seven_peaks_top_five_strongest(self):
        db = self._flat_db()
        gains = (14, 16, 18, 20, 22, 24, 26)
        hzs = (2000, 4000, 6000, 8000, 10000, 12000, 14000)
        for hz, g in zip(hzs, gains, strict=True):
            db = self._add_peak(db, hz, g)
        res = self._run(db)
        self.assertEqual(len(res), 5, res)
        proms = [r["prominence_db"] for r in res]
        self.assertEqual(proms, sorted(proms, reverse=True))
        # пять сильнейших: 14000..6000; 2000 (+14) и 4000 (+16) отсечены
        for r, hz in zip(res, (14000, 12000, 10000, 8000, 6000), strict=True):
            self.assertAlmostEqual(r["hz"], hz, delta=15)

    def test_top_and_threshold_params(self):
        db = self._add_peak(self._flat_db(), 9000, 20)
        db = self._add_peak(db, 5265, 30)
        res = self._run(db, top=1)
        self.assertEqual(len(res), 1, res)
        self.assertAlmostEqual(res[0]["hz"], 5265, delta=15)
        res = self._run(db, min_prom_db=25)
        self.assertEqual(len(res), 1, res)
        self.assertAlmostEqual(res[0]["hz"], 5265, delta=15)

    def test_wide_hump_is_not_tone(self):
        import numpy as np
        db = self._flat_db()
        # плавный горб +15 дБ шириной 2 кГц вокруг 8 кГц (форма Ханна в дБ)
        x = (self.freqs - 8000) / 1000.0
        hump = np.where(np.abs(x) <= 1, 15 * 0.5 * (1 + np.cos(np.pi * x)), 0.0)
        self.assertEqual(self._run(db + hump), [])


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestTonesEndpoint(_WorkerApiCase):
    """GET /jobs/{id}/tones?from=&to=: нет джобы → 404 («not found»);
    отметки вне трека → 422. Анализ аудио здесь не вызывается."""

    def _audio_job(self, duration=60.0):
        jid = self._job(duration=duration, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        (d / "audio.flac").write_bytes(b"x")
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid

    def _assert_job_404(self, r):
        detail = r.json().get("detail", "")
        self.assertIn("not found", detail.lower())
        # «Not Found» — ответ FastAPI на несуществующий маршрут: 404 должен
        # идти от обработчика tones («нет джобы»), а не от отсутствия роута
        self.assertNotEqual(detail, "Not Found", "маршрута /tones нет")

    def test_missing_job_404(self):
        r = self.client.get("/jobs/9999/tones", params={"from": 0, "to": 10})
        self.assertEqual(r.status_code, 404, r.text)
        self._assert_job_404(r)

    def test_missing_job_404_without_range(self):
        r = self.client.get("/jobs/9999/tones")
        self.assertEqual(r.status_code, 404, r.text)
        self._assert_job_404(r)

    def test_bad_ranges_422(self):
        jid = self._audio_job(duration=60.0)
        for q in ("from=60", "from=500", "from=-1", "from=10&to=5", "from=10&to=10",
                  "from=nan", "from=inf", "to=nan", "to=inf"):
            with self.subTest(q=q):
                r = self.client.get(f"/jobs/{jid}/tones?{q}")
                self.assertEqual(r.status_code, 422, f"{q}: {r.text}")


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestTonesStemParam(TestTonesEndpoint):
    """GET /jobs/{id}/tones?stem=vocals|drums|bass|other — анализ стема
    jobs/<id>/stem-<stem>.flac: нет файла → 409 с подсказкой make_stems;
    неизвестный stem → 422; без stem — как раньше (микс). Анализ аудио здесь
    не вызывается: проверяются только отказы до него."""

    STEMS = ("vocals", "drums", "bass", "other")

    def test_each_known_stem_without_file_409_make_stems(self):
        jid = self._audio_job(duration=60.0)
        for stem in self.STEMS:
            with self.subTest(stem=stem):
                r = self.client.get(f"/jobs/{jid}/tones", params={"stem": stem})
                self.assertEqual(r.status_code, 409, r.text)
                self.assertIn("make_stems", r.json().get("detail", ""))

    def test_other_stem_present_does_not_count(self):
        # есть стем барабанов, просят голос → всё равно 409
        jid = self._audio_job(duration=60.0)
        (self.jobs_dir / str(jid) / "stem-drums.flac").write_bytes(b"x")
        r = self.client.get(f"/jobs/{jid}/tones", params={"stem": "vocals"})
        self.assertEqual(r.status_code, 409, r.text)
        self.assertIn("make_stems", r.json().get("detail", ""))

    def test_stem_with_range_without_file_409(self):
        jid = self._audio_job(duration=60.0)
        r = self.client.get(f"/jobs/{jid}/tones",
                            params={"stem": "bass", "from": 5, "to": 20})
        self.assertEqual(r.status_code, 409, r.text)
        self.assertIn("make_stems", r.json().get("detail", ""))

    def test_unknown_stem_422(self):
        jid = self._audio_job(duration=60.0)
        for stem in ("guitar", "../x", "../../audio", "VOCALS", "vocals/../x", "mix"):
            with self.subTest(stem=stem):
                r = self.client.get(f"/jobs/{jid}/tones", params={"stem": stem})
                self.assertEqual(r.status_code, 422, f"{stem}: {r.text}")

    def test_unknown_stem_422_even_if_such_file_exists(self):
        # файл с «чужим» именем не делает стем допустимым
        jid = self._audio_job(duration=60.0)
        (self.jobs_dir / str(jid) / "stem-guitar.flac").write_bytes(b"x")
        r = self.client.get(f"/jobs/{jid}/tones", params={"stem": "guitar"})
        self.assertEqual(r.status_code, 422, r.text)

    def test_stem_on_missing_job_404(self):
        r = self.client.get("/jobs/9999/tones", params={"stem": "vocals"})
        self.assertEqual(r.status_code, 404, r.text)
        self._assert_job_404(r)

    def test_without_stem_no_audio_as_before(self):
        # без stem — прежнее поведение микса: нет audio → 404 «no audio»,
        # а не 409 про стемы
        jid = self._job(duration=60.0, semantic=False)
        r = self.client.get(f"/jobs/{jid}/tones")
        self.assertEqual(r.status_code, 404, r.text)
        self.assertIn("no audio", r.json().get("detail", "").lower())
        self.assertNotIn("make_stems", r.text)

    def test_without_stem_bad_range_still_422(self):
        jid = self._audio_job(duration=60.0)
        r = self.client.get(f"/jobs/{jid}/tones?from=10&to=5")
        self.assertEqual(r.status_code, 422, r.text)


# ---------- Проверка изменённого плана (карточка internal-plan-check) ----------

_PLAN_OLD = ("X:1\nM:4/4\nL:1/16\nQ:1/4=120\nK:C\n% verse\n"
             "V: Vocal\n\"C\"c4B4A4G4|\"G\"B4B4G4G4|\nV: Ins\nC16|G16|\n")
_PLAN_NEW = ("X:1\nM:4/4\nL:1/16\nQ:1/4=120\nK:C\n% verse\n"
             "V: Vocal\n\"C\"c4B4A4G4|\"G\"c'4B4G4G4|\nV: Ins\nC16|G16|\n")


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestPlanCheckEndpoint(_WorkerApiCase):
    """POST /jobs/{id}/plan_check {abc, from_sec?}: нет джобы / нет плана → 404,
    пустой abc → 422, иначе ответ = plancheck.plan_diff(план джобы, abc, from_sec)."""

    def _job_with_plan(self, abc=_PLAN_OLD):
        jid = self._job(semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        (d / "score.abc").write_text(abc)
        return jid

    def test_missing_job_404(self):
        r = self.client.post("/jobs/9999/plan_check", json={"abc": _PLAN_NEW})
        self.assertEqual(r.status_code, 404, r.text)

    def test_no_plan_404(self):
        jid = self._job(semantic=False)
        r = self.client.post(f"/jobs/{jid}/plan_check", json={"abc": _PLAN_NEW})
        self.assertEqual(r.status_code, 404, r.text)

    def test_empty_abc_422(self):
        jid = self._job_with_plan()
        for abc in ("", "   \n"):
            r = self.client.post(f"/jobs/{jid}/plan_check", json={"abc": abc})
            self.assertEqual(r.status_code, 422, repr(abc))

    def test_missing_abc_422(self):
        jid = self._job_with_plan()
        r = self.client.post(f"/jobs/{jid}/plan_check", json={})
        self.assertEqual(r.status_code, 422, r.text)

    def test_response_is_plan_diff(self):
        import plancheck
        jid = self._job_with_plan()
        r = self.client.post(f"/jobs/{jid}/plan_check", json={"abc": _PLAN_NEW})
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json(), plancheck.plan_diff(_PLAN_OLD, _PLAN_NEW, None))
        self.assertEqual(r.json()["changed_total"], 1)

    def test_from_sec_passed_through(self):
        import plancheck
        jid = self._job_with_plan()
        r = self.client.post(f"/jobs/{jid}/plan_check",
                             json={"abc": _PLAN_NEW, "from_sec": 5.0})
        self.assertEqual(r.status_code, 200, r.text)
        expected = plancheck.plan_diff(_PLAN_OLD, _PLAN_NEW, 5.0)
        self.assertEqual(r.json(), expected)
        self.assertTrue(any("до отметки" in w for w in r.json()["warnings"]))


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestPatchJob(_WorkerApiCase):
    """PATCH /jobs/{id} {title?, folder?}: правка названия и папки трека.
    Пробелы по краям обрезаются; пустое title / title > 200 / folder > 60 → 422;
    folder "" — убрать из папки; не переданное (или null) поле не меняется;
    ответ 200 — трек целиком (как GET /jobs/{id}); нет трека → 404;
    неуспешный запрос не меняет ничего."""

    def _get(self, jid):
        r = self.client.get(f"/jobs/{jid}")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def _listed(self, jid):
        r = self.client.get("/jobs")
        self.assertEqual(r.status_code, 200, r.text)
        return {j["id"]: j for j in r.json()}[jid]

    def _set(self, jid, **body):
        r = self.client.patch(f"/jobs/{jid}", json=body)
        self.assertEqual(r.status_code, 200, (body, r.text))
        return r.json()

    # 1. title
    def test_title_renamed_and_trimmed(self):
        jid = self._job(semantic=False)
        body = self._set(jid, title="  Новое имя  ")
        self.assertEqual(body["title"], "Новое имя")
        self.assertEqual(self._get(jid)["title"], "Новое имя")

    def test_empty_title_422(self):
        jid = self._job(semantic=False)
        for bad in ("", "   ", " \t\n "):
            r = self.client.patch(f"/jobs/{jid}", json={"title": bad})
            self.assertEqual(r.status_code, 422, repr(bad))
        self.assertEqual(self._get(jid)["title"], "t")

    def test_title_length_limit(self):
        jid = self._job(semantic=False)
        self.assertEqual(self._set(jid, title="x" * 200)["title"], "x" * 200)
        r = self.client.patch(f"/jobs/{jid}", json={"title": "y" * 201})
        self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(self._get(jid)["title"], "x" * 200)

    # 2. folder
    def test_folder_set_and_trimmed(self):
        jid = self._job(semantic=False)
        body = self._set(jid, folder="  Альбом  ")
        self.assertEqual(body["folder"], "Альбом")
        self.assertEqual(self._get(jid)["folder"], "Альбом")

    def test_empty_folder_removes_from_folder(self):
        jid = self._job(semantic=False)
        self._set(jid, folder="Альбом")
        for empty in ("", "   "):
            self._set(jid, folder="Альбом")
            body = self._set(jid, folder=empty)
            self.assertEqual(body["folder"], "", repr(empty))
            self.assertEqual(self._get(jid)["folder"], "", repr(empty))

    def test_folder_length_limit(self):
        jid = self._job(semantic=False)
        self.assertEqual(self._set(jid, folder="f" * 60)["folder"], "f" * 60)
        r = self.client.patch(f"/jobs/{jid}", json={"folder": "g" * 61})
        self.assertEqual(r.status_code, 422, r.text)
        self.assertEqual(self._get(jid)["folder"], "f" * 60)

    # 3. не переданное / null поле не меняется
    def test_omitted_field_unchanged(self):
        jid = self._job(semantic=False)
        self._set(jid, title="Имя", folder="Папка")
        self.assertEqual(self._set(jid, title="Другое")["folder"], "Папка")
        self.assertEqual(self._set(jid, folder="Другая")["title"], "Другое")
        got = self._get(jid)
        self.assertEqual((got["title"], got["folder"]), ("Другое", "Другая"))

    def test_null_field_unchanged(self):
        jid = self._job(semantic=False)
        self._set(jid, title="Имя", folder="Папка")
        body = self._set(jid, title=None, folder=None)
        self.assertEqual((body["title"], body["folder"]), ("Имя", "Папка"))
        got = self._get(jid)
        self.assertEqual((got["title"], got["folder"]), ("Имя", "Папка"))

    def test_empty_body_changes_nothing(self):
        jid = self._job(semantic=False)
        self._set(jid, title="Имя", folder="Папка")
        before = self._get(jid)
        body = self._set(jid)
        self.assertEqual((body["title"], body["folder"]), ("Имя", "Папка"))
        self.assertEqual(self._get(jid), before)

    # 4. ответ — трек целиком, как GET /jobs/{id}
    def test_response_is_whole_track(self):
        jid = self._job(semantic=False)
        body = self._set(jid, title="Имя", folder="Папка")
        for key in ("id", "title", "folder", "status"):
            self.assertIn(key, body)
        self.assertEqual(body["id"], jid)
        self.assertEqual(body["status"], "done")
        self.assertEqual(body, self._get(jid))

    # 5. нет трека
    def test_missing_job_404(self):
        r = self.client.patch("/jobs/9999", json={"title": "Имя"})
        self.assertEqual(r.status_code, 404, r.text)
        r = self.client.patch("/jobs/9999", json={})
        self.assertEqual(r.status_code, 404, r.text)

    # 6. новое значение видно в GET /jobs/{id} и GET /jobs
    def test_visible_in_get_and_list(self):
        jid = self._job(semantic=False)
        other = self._job(semantic=False)
        self._set(jid, title="Имя", folder="Папка")
        got, listed = self._get(jid), self._listed(jid)
        self.assertEqual((got["title"], got["folder"]), ("Имя", "Папка"))
        self.assertEqual((listed["title"], listed["folder"]), ("Имя", "Папка"))
        # соседний трек не задет
        o = self._listed(other)
        self.assertEqual((o["title"], o["folder"]), ("t", ""))

    # 7. у новых треков folder — "" (не null)
    def test_new_track_folder_empty_string(self):
        jid = self._job(semantic=False)
        self.assertEqual(self._get(jid)["folder"], "")
        self.assertEqual(self._listed(jid)["folder"], "")

    # 8. 422 не меняет ничего, даже валидное второе поле
    def test_failed_request_changes_nothing(self):
        jid = self._job(semantic=False)
        self._set(jid, title="Имя", folder="Папка")
        for bad in ({"title": "", "folder": "Новая"},
                    {"title": "x" * 201, "folder": "Новая"},
                    {"title": "Новое", "folder": "g" * 61}):
            r = self.client.patch(f"/jobs/{jid}", json=bad)
            self.assertEqual(r.status_code, 422, bad)
            got = self._get(jid)
            self.assertEqual((got["title"], got["folder"]), ("Имя", "Папка"), bad)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestRetryJob(_WorkerApiCase):
    """POST /jobs/{id}/retry: упавшая (error) или отменённая (canceled) джоба
    возвращается в очередь — 200 {"retried": true}, status=queued, error пуст.
    done/running/queued → 409, статус не меняется; нет джобы → 404."""

    def _failed(self, status, error="CUDA out of memory"):
        jid = self._job(semantic=False, status=status)
        with self._conn() as c:
            c.execute("UPDATE jobs SET error=? WHERE id=?", (error, jid))
        return jid

    def _get(self, jid):
        r = self.client.get(f"/jobs/{jid}")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def test_error_job_requeued(self):
        jid = self._failed("error")
        r = self.client.post(f"/jobs/{jid}/retry")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json(), {"retried": True})
        got = self._get(jid)
        self.assertEqual(got["status"], "queued")
        self.assertFalse(got.get("error"), got)

    def test_canceled_job_requeued(self):
        jid = self._failed("canceled", error="")
        r = self.client.post(f"/jobs/{jid}/retry")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json(), {"retried": True})
        got = self._get(jid)
        self.assertEqual(got["status"], "queued")
        self.assertFalse(got.get("error"), got)

    def test_not_failed_job_409_unchanged(self):
        for status in ("done", "running", "queued"):
            jid = self._job(semantic=False, status=status)
            r = self.client.post(f"/jobs/{jid}/retry")
            self.assertEqual(r.status_code, 409, (status, r.text))
            self.assertEqual(self._row(jid)["status"], status)

    def test_missing_job_404(self):
        r = self.client.post("/jobs/9999/retry")
        self.assertEqual(r.status_code, 404, r.text)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestFriendlyError(_WorkerDbCase):
    """friendly_error(e): «CUDA out of memory» (любой регистр) → человеческое
    сообщение с «GPU занят» и «повторить», без исключений даже без torch или
    при сбое запроса к CUDA; любое другое исключение → str(e) как есть."""

    OOM_TEXTS = ("CUDA out of memory. Tried to allocate 2.00 GiB",
                 "cuda OUT OF MEMORY",
                 "RuntimeError: Cuda Out Of Memory while sampling")

    def _assert_friendly(self, msg):
        self.assertIsInstance(msg, str)
        self.assertIn("GPU занят", msg)
        self.assertIn("повторить", msg)

    def test_oom_any_case(self):
        for text in self.OOM_TEXTS:
            self._assert_friendly(self.w.friendly_error(RuntimeError(text)))

    def _broken_torch(self):
        """torch, у которого любой запрос к CUDA падает."""
        import types

        def boom(*a, **kw):
            raise RuntimeError("CUDA driver failure")

        cuda = types.SimpleNamespace(
            is_available=boom, mem_get_info=boom, memory_allocated=boom,
            memory_reserved=boom, max_memory_allocated=boom,
            get_device_properties=boom, device_count=boom, empty_cache=boom,
            memory_summary=boom)
        return types.SimpleNamespace(cuda=cuda)

    def _with_torch(self, fake):
        """Подменить torch и в sys.modules, и в модуле воркера (если он там)."""
        import sys
        from unittest import mock
        stack = contextlib.ExitStack()
        stack.enter_context(mock.patch.dict(sys.modules, {"torch": fake}))
        if hasattr(self.w, "torch"):
            stack.enter_context(mock.patch.object(self.w, "torch", fake))
        return stack

    def test_oom_without_torch(self):
        with self._with_torch(None):  # import torch → ImportError
            for text in self.OOM_TEXTS:
                self._assert_friendly(self.w.friendly_error(RuntimeError(text)))

    def test_oom_when_cuda_query_fails(self):
        with self._with_torch(self._broken_torch()):
            for text in self.OOM_TEXTS:
                self._assert_friendly(self.w.friendly_error(RuntimeError(text)))

    def test_other_errors_unchanged(self):
        for e in (RuntimeError("ffmpeg failed: exit 1"), ValueError("bad seed"),
                  KeyError("x"), RuntimeError("out of memory"), Exception("")):
            self.assertEqual(self.w.friendly_error(e), str(e), repr(e))


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestLyricsLanguage(_WorkerDbCase):
    """lyrics_language(style, explicit=None): явный язык — в нижнем регистре
    без пробелов, «auto» → None; без явного — по ПЕРВОМУ тегу стиля
    (English → en, Russian → ru), неизвестный первый тег или пустой стиль → None."""

    def test_explicit_normalized(self):
        self.assertEqual(self.w.lyrics_language("Russian, rock", "EN"), "en")
        self.assertEqual(self.w.lyrics_language("", "  De "), "de")

    def test_explicit_auto_is_none(self):
        for v in ("auto", "AUTO", " Auto "):
            self.assertIsNone(self.w.lyrics_language("English, rock", v), v)

    def test_first_tag(self):
        for style, want in (("English, post-punk, 120 BPM", "en"),
                            ("Russian, rock", "ru"),
                            ("russian", "ru")):
            self.assertEqual(self.w.lyrics_language(style), want, style)

    def test_unknown_first_tag_none(self):
        self.assertIsNone(self.w.lyrics_language("post-punk, English"))

    def test_empty_style_none(self):
        self.assertIsNone(self.w.lyrics_language(""))
        self.assertIsNone(self.w.lyrics_language(None))


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestJobLyricsEndpoint(_WorkerApiCase):
    """POST /jobs/{id}/lyrics[?language=]: распознаёт stem-vocals.flac, если он
    есть в папке джобы, иначе аудио джобы; язык — из query («auto» → None),
    иначе по первому тегу стиля; пустой текст → 422; не готова/нет аудио → 404."""

    def setUp(self):
        super().setUp()
        from unittest import mock
        self.calls = []
        self.text = "hello world"

        def rec(*args, **kwargs):
            src = kwargs.get("src_path", args[0] if args else None)
            lang = kwargs.get("language", args[1] if len(args) > 1 else None)
            self.calls.append((Path(src), lang))
            return (self.text, "")

        p = mock.patch.object(self.w, "_whisper_lyrics", rec)
        p.start()
        self.addCleanup(p.stop)

    def _audio_job(self, style="English, post-punk, 120 BPM", status="done", vocals=False):
        jid = self._job(style=style, semantic=False, status=status)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        (d / "audio.flac").write_bytes(b"x")
        if vocals:
            (d / "stem-vocals.flac").write_bytes(b"v")
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid

    def test_uses_audio_without_stem(self):
        jid = self._audio_job()
        r = self.client.post(f"/jobs/{jid}/lyrics")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json()["text"], "hello world")
        self.assertEqual(len(self.calls), 1)
        self.assertEqual(self.calls[0][0].name, "audio.flac")

    def test_prefers_vocal_stem(self):
        jid = self._audio_job(vocals=True)
        r = self.client.post(f"/jobs/{jid}/lyrics")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(len(self.calls), 1)
        self.assertEqual(self.calls[0][0].name, "stem-vocals.flac")
        self.assertEqual(self.calls[0][0].parent.name, str(jid))

    def test_language_from_style(self):
        jid = self._audio_job(style="English, post-punk")
        self.client.post(f"/jobs/{jid}/lyrics")
        self.assertEqual(self.calls[0][1], "en")

    def test_unknown_style_language_none(self):
        jid = self._audio_job(style="post-punk, English")
        r = self.client.post(f"/jobs/{jid}/lyrics")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertIsNone(self.calls[0][1])

    def test_query_language_overrides_style(self):
        jid = self._audio_job(style="English, rock")
        r = self.client.post(f"/jobs/{jid}/lyrics", params={"language": "RU"})
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(self.calls[0][1], "ru")

    def test_query_auto_is_none(self):
        jid = self._audio_job(style="English, rock")
        r = self.client.post(f"/jobs/{jid}/lyrics", params={"language": "auto"})
        self.assertEqual(r.status_code, 200, r.text)
        self.assertIsNone(self.calls[0][1])

    def test_empty_text_422(self):
        self.text = ""
        jid = self._audio_job()
        r = self.client.post(f"/jobs/{jid}/lyrics")
        self.assertEqual(r.status_code, 422, r.text)

    def test_not_done_or_no_audio_404(self):
        running = self._audio_job(status="running")
        no_audio = self._job(semantic=False, status="done")
        for jid in (running, no_audio, 9999):
            r = self.client.post(f"/jobs/{jid}/lyrics")
            self.assertEqual(r.status_code, 404, (jid, r.text))
        self.assertEqual(self.calls, [])


# --- Спецификация: POST /jobs/{id}/grid — сетка ударов окна трека ----------
# Нужны soundfile (запись аудио в каталог джобы) и librosa (анализ).
try:
    import librosa  # noqa: F401
    import soundfile  # noqa: F401
    _HAS_GRID_DEPS = _HAS_WORKER_DEPS
except ImportError:
    _HAS_GRID_DEPS = False


def _click_audio(bpm, first, sec, sr=22050):
    """Клик-трек: затухающие шумовые удары на first + k·60/bpm, моно float32."""
    import numpy as np
    rng = np.random.default_rng(int(bpm))
    n = int(sec * sr)
    y = np.zeros(n, dtype=np.float64)
    blen = int(0.06 * sr)
    burst = np.exp(-np.arange(blen) / sr / 0.015) * rng.uniform(-1, 1, blen)
    k = 0
    while first + k * 60.0 / bpm < sec:
        i = int(round((first + k * 60.0 / bpm) * sr))
        m = min(blen, n - i)
        y[i:i + m] += burst[:m]
        k += 1
    return (0.8 * y / np.max(np.abs(y))).astype("float32"), sr


@unittest.skipUnless(_HAS_GRID_DEPS, "нужны fastapi/httpx/numpy/soundfile/librosa (окружение воркера)")
class TestGridEndpoint(_WorkerApiCase):
    """POST /jobs/{id}/grid {from_sec?, to_sec?}: сетка по stem-drums.flac, если он
    есть (source=drums), иначе по аудио джобы (source=mix); offset — абсолютное
    время трека внутри окна; окно < 4 с → 422; нет аудио → 404."""

    def _audio_job(self, bpm=120, first=0.2, sec=30.0, drums=None):
        import soundfile as sf
        jid = self._job(duration=sec, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        y, sr = _click_audio(bpm, first, sec)
        sf.write(str(d / "audio.flac"), y, sr)
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        if drums is not None:
            y, sr = _click_audio(drums[0], drums[1], sec)
            sf.write(str(d / "stem-drums.flac"), y, sr)
        return jid

    def test_mix_when_no_drums_stem(self):
        jid = self._audio_job(bpm=120, first=0.2)
        r = self.client.post(f"/jobs/{jid}/grid", json={})
        self.assertEqual(r.status_code, 200, r.text)
        body = r.json()
        self.assertEqual(body["source"], "mix")
        self.assertAlmostEqual(body["bpm"], 120, delta=0.3, msg=body)

    def test_drums_stem_preferred(self):
        # микс — 100 BPM, стем барабанов — 120 BPM: ответ должен быть по стему
        jid = self._audio_job(bpm=100, first=0.1, drums=(120, 0.2))
        r = self.client.post(f"/jobs/{jid}/grid", json={})
        self.assertEqual(r.status_code, 200, r.text)
        body = r.json()
        self.assertEqual(body["source"], "drums")
        self.assertAlmostEqual(body["bpm"], 120, delta=0.3, msg=body)

    def test_offset_is_absolute_track_time(self):
        jid = self._audio_job(bpm=120, first=0.2)
        r = self.client.post(f"/jobs/{jid}/grid", json={"from_sec": 10.0})
        self.assertEqual(r.status_code, 200, r.text)
        body = r.json()
        self.assertGreaterEqual(body["offset"], 10.0, msg=body)
        self.assertLess(body["offset"], 10.5, msg=body)
        self.assertAlmostEqual(body["offset"], 10.2, delta=0.02, msg=body)

    def test_offset_absolute_with_both_bounds(self):
        jid = self._audio_job(bpm=120, first=0.2)
        r = self.client.post(f"/jobs/{jid}/grid", json={"from_sec": 10.0, "to_sec": 25.0})
        self.assertEqual(r.status_code, 200, r.text)
        self.assertAlmostEqual(r.json()["offset"], 10.2, delta=0.02, msg=r.json())

    def test_window_shorter_than_4s_422(self):
        jid = self._audio_job()
        for body in ({"from_sec": 10.0, "to_sec": 13.0},
                     {"from_sec": 10.0, "to_sec": 10.0},
                     {"from_sec": 27.0}):  # до конца 30-секундного трека — 3 с
            with self.subTest(body=body):
                r = self.client.post(f"/jobs/{jid}/grid", json=body)
                self.assertEqual(r.status_code, 422, f"{body}: {r.text}")

    def test_no_audio_404(self):
        no_audio = self._job(semantic=False, status="done")
        for jid in (no_audio, 9999):
            with self.subTest(jid=jid):
                r = self.client.post(f"/jobs/{jid}/grid", json={})
                self.assertEqual(r.status_code, 404, r.text)
                self.assertNotEqual(r.json().get("detail"), "Not Found", "маршрута /grid нет")


# --- «Голос в инструментале»: is_instrumental, vocal_leak, авто-стемы -------
def _leak_stem(path, tone_at=25.4, sec=40.0, sr=44100):
    """stem-vocals.flac: шум −80 dBFS, тон RMS −20 dBFS 0.5 с с tone_at (None — без тона)."""
    import numpy as np
    import soundfile as sf
    rng = np.random.default_rng(1)
    x = rng.standard_normal(int(sec * sr)) * 10 ** (-80 / 20)
    if tone_at is not None:
        i0, n = int(round(tone_at * sr)), int(round(0.5 * sr))
        t = np.arange(n) / sr
        x[i0:i0 + n] += 10 ** (-20 / 20) * np.sqrt(2) * np.sin(2 * np.pi * 440 * t)
    sf.write(str(path), np.stack([x, x], axis=1).astype(np.float32), sr)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestIsInstrumental(unittest.TestCase):
    """is_instrumental(style): «instrumental» или «no vocals» без учёта регистра."""

    def setUp(self):
        import tempfile
        self._td = tempfile.TemporaryDirectory()
        self.addCleanup(self._td.cleanup)
        self.f = _import_worker(self._td.name).is_instrumental

    def test_true_cases(self):
        for s in ("instrumental", "dark rock, Instrumental", "INSTRUMENTAL synthwave",
                  "ambient, no vocals", "No Vocals, drone"):
            with self.subTest(style=s):
                self.assertIs(self.f(s), True)

    def test_false_cases(self):
        for s in ("dark rock, male baritone", "vocals upfront", "no voice", ""):
            with self.subTest(style=s):
                self.assertIs(self.f(s), False)

    def test_none_false(self):
        self.assertIs(self.f(None), False)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestVocalLeak(_WorkerApiCase):
    """vocal_leak: строка начал голоса в треке «без голоса» (по stem-vocals.flac),
    по умолчанию ""; видна в GET /jobs и GET /jobs/{id}; стемы на инструментале
    заполняют её."""

    def _leak(self, jid):
        return self._row(jid)["vocal_leak"]

    def test_default_empty_in_db_and_api(self):
        jid = self._job(semantic=False)
        self.assertEqual(self._leak(jid) or "", "")
        r = self.client.get(f"/jobs/{jid}")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json()["vocal_leak"], "")
        jobs = {j["id"]: j for j in self.client.get("/jobs").json()}
        self.assertEqual(jobs[jid]["vocal_leak"], "")

    def test_instrumental_with_vocal_tone_sets_leak(self):
        jid = self._job(style="ambient drone, instrumental", semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        _leak_stem(d / "stem-vocals.flac")
        self.w.check_vocal_leak(jid)
        self.assertEqual(self._leak(jid), "25.4")
        self.assertEqual(self.client.get(f"/jobs/{jid}").json()["vocal_leak"], "25.4")
        jobs = {j["id"]: j for j in self.client.get("/jobs").json()}
        self.assertEqual(jobs[jid]["vocal_leak"], "25.4")

    def test_instrumental_clean_stem_stays_empty(self):
        jid = self._job(style="instrumental", semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        _leak_stem(d / "stem-vocals.flac", tone_at=None)
        self.w.check_vocal_leak(jid)
        self.assertEqual(self._leak(jid) or "", "")

    def test_non_instrumental_left_empty(self):
        jid = self._job(style="dark rock, male baritone", semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        _leak_stem(d / "stem-vocals.flac")
        self.w.check_vocal_leak(jid)
        self.assertEqual(self._leak(jid) or "", "")

    def test_no_stem_unchanged_no_exception(self):
        jid = self._job(style="instrumental", semantic=False)
        (self.jobs_dir / str(jid)).mkdir(parents=True, exist_ok=True)
        self.w.check_vocal_leak(jid)  # не должно бросать
        self.assertEqual(self._leak(jid) or "", "")
        with self._conn() as c:
            c.execute("UPDATE jobs SET vocal_leak='7.0' WHERE id=?", (jid,))
        self.w.check_vocal_leak(jid)
        self.assertEqual(self._leak(jid), "7.0")

    def test_missing_job_no_exception(self):
        self.w.check_vocal_leak(9999)

    def test_stems_endpoint_on_instrumental_sets_leak(self):
        import numpy as np
        import soundfile as sf
        from unittest import mock
        paths = {(m, getattr(r, "path", "")) for r in self.w.app.routes
                 for m in getattr(r, "methods", ()) or ()}
        self.assertIn(("POST", "/jobs/{job_id}/stems"), paths)

        jid = self._job(style="Instrumental, post-rock", duration=40.0, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        sf.write(str(d / "audio.flac"), np.zeros((44100 * 40, 2), dtype=np.float32), 44100)
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))

        def fake_separate(audio_path, out_dir):
            out_dir = Path(out_dir)
            out_dir.mkdir(parents=True, exist_ok=True)
            names = []
            for name in ("drums", "bass", "other"):
                _leak_stem(out_dir / f"stem-{name}.flac", tone_at=None)
                names.append(f"stem-{name}.flac")
            _leak_stem(out_dir / "stem-vocals.flac")
            names.append("stem-vocals.flac")
            return {"stems": names, "seconds": 0.0}

        with mock.patch.object(self.w, "demucs_separate", side_effect=fake_separate):
            r = self.client.post(f"/jobs/{jid}/stems")
            self.assertLess(r.status_code, 300, r.text)
            deadline = time.time() + 15
            while time.time() < deadline and (self._leak(jid) or "") == "":
                time.sleep(0.1)
        self.assertEqual(self._leak(jid), "25.4")


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestAutoStemsInstrumentalConfig(_WorkerApiCase):
    """GET/POST /config: auto_stems_instrumental (bool, по умолчанию true),
    сохраняется в DATA_DIR/settings.json."""

    def setUp(self):
        super().setUp()
        from unittest import mock
        data = Path(self._td.name) / "cfg"
        data.mkdir()
        self.settings = data / "settings.json"
        for name, val in (("DATA_DIR", data), ("SETTINGS_PATH", self.settings)):
            p = mock.patch.object(self.w, name, val)
            p.start()
            self.addCleanup(p.stop)

    def test_default_true_without_settings_file(self):
        self.assertFalse(self.settings.exists())
        r = self.client.get("/config")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertIs(r.json()["auto_stems_instrumental"], True)

    def test_post_false_persists(self):
        import json
        r = self.client.post("/config", json={"auto_stems_instrumental": False})
        self.assertLess(r.status_code, 300, r.text)
        self.assertIs(self.client.get("/config").json()["auto_stems_instrumental"], False)
        self.assertTrue(self.settings.exists())
        self.assertIs(json.loads(self.settings.read_text())["auto_stems_instrumental"], False)

    def test_post_true_after_false(self):
        self.client.post("/config", json={"auto_stems_instrumental": False})
        self.client.post("/config", json={"auto_stems_instrumental": True})
        self.assertIs(self.client.get("/config").json()["auto_stems_instrumental"], True)
