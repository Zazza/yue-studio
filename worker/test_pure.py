"""Тесты чистых функций воркера: llm, abcparse, media.encode_mp3.

Запуск: python3 -m unittest worker.test_pure (из корня репозитория)
или: cd worker && python3 -m unittest test_pure
"""
import time
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

    def _conn(self):
        import sqlite3
        c = sqlite3.connect(self.db_path)
        c.row_factory = sqlite3.Row
        return c

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
