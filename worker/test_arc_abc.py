"""Спецификация «драматургия поверх прикреплённого ABC».

Раньше arc вместе с abc был отказом 422. Теперь:
A1. POST /jobs с непустым abc и arc ∈ {build, wave, burst} → 200: джоба создана,
    arc сохранён в строке, req_abc='request.abc', jobs/<id>/request.abc —
    ровно присланный abc.
A2. Генерация такой джобы: пайплайн получает abc == arc.apply_arc(abc, arc) и
    style == arc.style_with_arc(style, arc); request.abc на диске не меняется
    (файл пользователя не трогаем).
A3. abc без arc → пайплайн получает исходный abc и исходный style.
A4. arc с cot="off" — по-прежнему 422 (драматургии нужен план), с abc и без.

Запуск из каталога worker: python -m unittest test_arc_abc -v
"""
import sys
import types
import unittest

import arc as arcmod
from test_character import _job_id, _post_job, _RecordingModel, _Sampling
from test_pure import ABC, _HAS_WORKER_DEPS, _WorkerApiCase

ARCS = ("build", "wave", "burst")
STYLE = "dark rock"  # стиль по умолчанию в _post_job


def _count_jobs(case):
    with case._conn() as c:
        return c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0]


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestArcAbcSubmit(_WorkerApiCase):
    """A1, A4: приём POST /jobs."""

    def test_a1_abc_with_arc_accepted_and_stored(self):
        for a in ARCS:
            with self.subTest(arc=a):
                jid = _job_id(self, _post_job(self, abc=ABC, arc=a))
                row = self._row(jid)
                self.assertEqual(row["arc"], a)
                self.assertEqual(row["req_abc"], "request.abc")
                f = self.jobs_dir / str(jid) / "request.abc"
                self.assertTrue(f.exists(), f)
                self.assertEqual(f.read_text(), ABC)

    def test_a4_arc_with_cot_off_422_without_abc(self):
        for a in ARCS:
            with self.subTest(arc=a):
                before = _count_jobs(self)
                r = _post_job(self, arc=a, cot="off")
                self.assertEqual(r.status_code, 422, r.text)
                self.assertEqual(_count_jobs(self), before)

    def test_a4_control_cot_off_without_arc_accepted(self):
        # контроль: 422 выше — из-за arc, а не из-за самого cot="off"
        _job_id(self, _post_job(self, cot="off"))

    def test_a4_arc_with_cot_off_422_with_abc(self):
        for a in ARCS:
            with self.subTest(arc=a):
                before = _count_jobs(self)
                r = _post_job(self, abc=ABC, arc=a, cot="off")
                self.assertEqual(r.status_code, 422, r.text)
                self.assertEqual(_count_jobs(self), before)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestArcAbcGeneration(_WorkerApiCase):
    """A2, A3: что получает пайплайн при генерации джобы с abc."""

    def setUp(self):
        super().setUp()
        from unittest import mock
        protocol = types.ModuleType("yue2.protocol")
        protocol.Sampling = _Sampling
        mods = {"yue2": mock.MagicMock(), "yue2.protocol": protocol,
                "yue2.pipeline": mock.MagicMock(), "torch": mock.MagicMock()}
        p = mock.patch.dict(sys.modules, mods)
        p.start()
        self.addCleanup(p.stop)
        self.model = _RecordingModel()
        for name, val in (("_pipe", self.model), ("_load_error", None)):
            p = mock.patch.object(self.w, name, val)
            p.start()
            self.addCleanup(p.stop)

    def _run(self, **extra):
        jid = _job_id(self, _post_job(self, **extra))
        self.w._run_job(jid)  # исключение наружу = провал теста
        self.assertTrue(self.model.calls, "пайплайн не вызван")
        return jid

    def _passed_values(self):
        """Все строковые значения, переданные пайплайну (имя аргумента —
        деталь реализации, проверяем, что значение до модели дошло)."""
        vals = []
        for _, kw in self.model.calls:
            for v in kw.values():
                if isinstance(v, str):
                    vals.append(v)
        return vals

    def _abc_passed(self):
        return [v for v in self._passed_values() if v.lstrip().startswith("X:")]

    def test_a2_pipeline_gets_arc_applied_abc_and_style(self):
        for a in ARCS:
            with self.subTest(arc=a):
                self.model.calls.clear()
                want_abc = arcmod.apply_arc(ABC, a)
                self.assertNotEqual(want_abc, ABC, "пример ABC не чувствителен к дуге")
                want_style = arcmod.style_with_arc(STYLE, a)
                self.assertNotEqual(want_style, STYLE)
                self._run(abc=ABC, arc=a)
                abcs = self._abc_passed()
                self.assertTrue(abcs, f"abc не дошёл до пайплайна: {self.model.calls}")
                self.assertEqual(set(abcs), {want_abc})
                self.assertIn(want_style, self._passed_values(), self.model.calls)
                self.assertNotIn(STYLE, self._passed_values(),
                                 "в пайплайн ушёл стиль без дуги")

    def test_a2_request_abc_file_unchanged(self):
        for a in ARCS:
            with self.subTest(arc=a):
                jid = self._run(abc=ABC, arc=a)
                f = self.jobs_dir / str(jid) / "request.abc"
                self.assertEqual(f.read_text(), ABC)

    def test_a3_abc_without_arc_passed_as_is(self):
        for extra in ({}, {"arc": ""}):
            with self.subTest(**extra):
                self.model.calls.clear()
                self._run(abc=ABC, **extra)
                abcs = self._abc_passed()
                self.assertTrue(abcs, f"abc не дошёл до пайплайна: {self.model.calls}")
                self.assertEqual(set(abcs), {ABC})
                self.assertIn(STYLE, self._passed_values(), self.model.calls)


if __name__ == "__main__":
    unittest.main()
