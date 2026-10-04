"""Спецификация «сид по умолчанию».

Раньше пустой seed означал фиксированный сид библиотеки yue2 (831001): в форме
это выглядело как «случайный», а выходил один и тот же трек. Теперь:
S1. POST /jobs без seed (нет / 0 / null) и без parent_id → у джобы seed —
    целое в [1, 2**31), у разных джоб разный, не 831001.
S2. Явный seed сохраняется как есть.
S3. С parent_id и без seed — seed родителя; у родителя 0/NULL (старый трек,
    реально генерировался с 831001) → 831001. Явный seed ребёнка важнее.
S4. _run_job передаёт пайплайну seed, сохранённый в строке джобы.

Запуск из каталога worker: python -m unittest test_seed -v
"""
import sys
import types
import unittest

from test_character import _get, _job_id, _post_job, _RecordingModel, _Sampling
from test_pure import _HAS_WORKER_DEPS, _WorkerApiCase

LIB_DEFAULT_SEED = 831001


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestSeedSubmit(_WorkerApiCase):
    """S1–S2: POST /jobs без родителя."""

    def _assert_random_seed(self, seed):
        self.assertIsInstance(seed, int, seed)
        self.assertGreaterEqual(seed, 1)
        self.assertLess(seed, 2**31)
        self.assertNotEqual(seed, LIB_DEFAULT_SEED)

    def test_s1_absent_seed_gets_random(self):
        jid = _job_id(self, _post_job(self))
        self._assert_random_seed(_get(self, jid)["seed"])

    def test_s1_zero_seed_gets_random(self):
        jid = _job_id(self, _post_job(self, seed=0))
        self._assert_random_seed(_get(self, jid)["seed"])

    def test_s1_null_seed_gets_random(self):
        jid = _job_id(self, _post_job(self, seed=None))
        self._assert_random_seed(_get(self, jid)["seed"])

    def test_s1_seeds_differ_between_jobs(self):
        seeds = []
        for extra in ({}, {"seed": 0}, {"seed": None}, {}, {}):
            jid = _job_id(self, _post_job(self, **extra))
            seed = _get(self, jid)["seed"]
            self._assert_random_seed(seed)
            seeds.append(seed)
        self.assertGreater(len(set(seeds)), 1, f"все сиды одинаковые: {seeds}")

    def test_s1_seed_persisted_in_row(self):
        # сид, показанный API, — тот же, что лежит в строке (по нему пойдёт генерация)
        jid = _job_id(self, _post_job(self))
        self.assertEqual(self._row(jid)["seed"], _get(self, jid)["seed"])

    def test_s2_explicit_seed_kept(self):
        jid = _job_id(self, _post_job(self, seed=42))
        self.assertEqual(_get(self, jid)["seed"], 42)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestSeedInherit(_WorkerApiCase):
    """S3: производная джоба (parent_id) без seed наследует сид родителя."""

    def _parent_with_seed(self, seed):
        jid = self._job()  # строка без seed (NULL), status=done
        if seed is not None:
            with self._conn() as c:
                c.execute("UPDATE jobs SET seed=? WHERE id=?", (seed, jid))
        return jid

    def _child_seed(self, parent, **extra):
        jid = _job_id(self, _post_job(self, parent_id=parent, role="section", **extra))
        return _get(self, jid)["seed"]

    def test_s3_inherits_parent_seed(self):
        self.assertEqual(self._child_seed(self._parent_with_seed(777)), 777)

    def test_s3_inherits_with_zero_seed_in_body(self):
        self.assertEqual(self._child_seed(self._parent_with_seed(777), seed=0), 777)

    def test_s3_parent_zero_means_library_default(self):
        self.assertEqual(self._child_seed(self._parent_with_seed(0)), LIB_DEFAULT_SEED)

    def test_s3_parent_null_means_library_default(self):
        self.assertEqual(self._child_seed(self._parent_with_seed(None)), LIB_DEFAULT_SEED)

    def test_s3_explicit_child_seed_wins(self):
        self.assertEqual(self._child_seed(self._parent_with_seed(777), seed=42), 42)

    def test_s3_explicit_child_seed_wins_over_old_parent(self):
        self.assertEqual(self._child_seed(self._parent_with_seed(None), seed=42), 42)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestSeedGeneration(_WorkerApiCase):
    """S4: генерация идёт с сидом из строки джобы, а не с дефолтом библиотеки."""

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

    def _gen_kwargs(self, jid):
        self.w._run_job(jid)  # исключение наружу = провал теста
        calls = [kw for _, kw in self.model.calls
                 if "cfg_scale" in kw or "semantic_sampling" in kw]
        self.assertTrue(calls, f"генерация не вызвана: {self.model.calls}")
        return calls[0]

    def test_s4_job_without_seed_generates_with_stored_seed(self):
        jid = _job_id(self, _post_job(self))
        stored = self._row(jid)["seed"]
        self.assertTrue(stored, "сид не сохранён в строке джобы")
        kw = self._gen_kwargs(jid)
        self.assertEqual(kw.get("seed"), stored, kw)
        self.assertNotEqual(kw.get("seed"), LIB_DEFAULT_SEED)

    def test_s4_explicit_seed_reaches_pipeline(self):
        jid = _job_id(self, _post_job(self, seed=42))
        self.assertEqual(self._gen_kwargs(jid).get("seed"), 42)


if __name__ == "__main__":
    unittest.main()
