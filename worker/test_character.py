"""Спецификация «характер исполнения»: temperature (смелость) и cfg (точность)
на трек. 0 = «по умолчанию»: температура 1.0 библиотеки, cfg = CFG_SCALE модуля
(YUE2_CFG_SCALE, по умолчанию 1.5). Допустимо: temperature 0 или [0.5, 1.5];
cfg 0 или [1.0, 4.0]; иначе 422. Производные джобы (parent_id, continue)
наследуют значения родителя, явные значения важнее.

Запуск из каталога worker: python -m unittest test_character -v
"""
import sys
import types
import unittest

from test_pure import _HAS_WORKER_DEPS, _WorkerApiCase


def _post_job(case, **extra):
    body = {"style": "dark rock", "lyrics": "[verse] la la"}
    body.update(extra)
    return case.client.post("/jobs", json=body)


def _job_id(case, r):
    case.assertEqual(r.status_code, 200, r.text)
    data = r.json()
    jid = data.get("id", data.get("job_id"))
    case.assertTrue(jid, data)
    return int(jid)


def _get(case, jid):
    r = case.client.get(f"/jobs/{jid}")
    case.assertEqual(r.status_code, 200, r.text)
    return r.json()


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestCharacterSubmit(_WorkerApiCase):
    """W1–W3: POST /jobs принимает и хранит temperature/cfg, проверяет диапазон."""

    def test_w1_explicit_values_stored_and_returned(self):
        jid = _job_id(self, _post_job(self, temperature=1.15, cfg=2.5))
        got = _get(self, jid)
        self.assertAlmostEqual(got["temperature"], 1.15)
        self.assertAlmostEqual(got["cfg"], 2.5)

    def test_w2_absent_means_zero(self):
        jid = _job_id(self, _post_job(self))
        got = _get(self, jid)
        self.assertEqual(got["temperature"], 0)
        self.assertEqual(got["cfg"], 0)

    def test_w2_explicit_zero_allowed(self):
        jid = _job_id(self, _post_job(self, temperature=0, cfg=0))
        got = _get(self, jid)
        self.assertEqual(got["temperature"], 0)
        self.assertEqual(got["cfg"], 0)

    def test_w3_bounds_inclusive(self):
        for t, c in ((0.5, 1.0), (1.5, 4.0)):
            jid = _job_id(self, _post_job(self, temperature=t, cfg=c))
            got = _get(self, jid)
            self.assertAlmostEqual(got["temperature"], t)
            self.assertAlmostEqual(got["cfg"], c)

    def test_w3_bad_temperature_422(self):
        for t in (2.0, -0.1, 0.3):
            with self._conn() as c:
                before = c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0]
            r = _post_job(self, temperature=t)
            self.assertEqual(r.status_code, 422, (t, r.text))
            with self._conn() as c:
                after = c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0]
            self.assertEqual(before, after, t)

    def test_w3_bad_cfg_422(self):
        for v in (5, -1, 0.5):
            with self._conn() as c:
                before = c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0]
            r = _post_job(self, cfg=v)
            self.assertEqual(r.status_code, 422, (v, r.text))
            with self._conn() as c:
                after = c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0]
            self.assertEqual(before, after, v)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestCharacterInherit(_WorkerApiCase):
    """W4–W5: производные джобы наследуют характер родителя."""

    def _parent(self):
        jid = _job_id(self, _post_job(self, temperature=1.15, cfg=2.5))
        with self._conn() as c:
            c.execute("UPDATE jobs SET status='done', duration_sec=60 WHERE id=?", (jid,))
        return jid

    def test_w4_child_without_values_inherits(self):
        parent = self._parent()
        jid = _job_id(self, _post_job(self, parent_id=parent, role="section"))
        got = _get(self, jid)
        self.assertAlmostEqual(got["temperature"], 1.15)
        self.assertAlmostEqual(got["cfg"], 2.5)

    def test_w4_child_explicit_values_win(self):
        parent = self._parent()
        jid = _job_id(self, _post_job(self, parent_id=parent, role="section",
                                      temperature=0.8, cfg=3.0))
        got = _get(self, jid)
        self.assertAlmostEqual(got["temperature"], 0.8)
        self.assertAlmostEqual(got["cfg"], 3.0)

    def test_w4_child_of_default_parent_stays_default(self):
        parent = _job_id(self, _post_job(self))
        jid = _job_id(self, _post_job(self, parent_id=parent, role="section"))
        got = _get(self, jid)
        self.assertEqual(got["temperature"], 0)
        self.assertEqual(got["cfg"], 0)

    def test_w5_continue_inherits(self):
        import numpy as np
        parent = self._parent()
        d = self.jobs_dir / str(parent)
        d.mkdir(parents=True, exist_ok=True)
        np.save(d / "semantic.npy", np.arange(60 * 25, dtype=np.int64))
        r = self.client.post(f"/jobs/{parent}/continue", json={"from_sec": 12.5})
        self.assertLess(r.status_code, 300, r.text)
        kids = self._children(parent)
        self.assertEqual(len(kids), 1)
        got = _get(self, kids[0]["id"])
        self.assertAlmostEqual(got["temperature"], 1.15)
        self.assertAlmostEqual(got["cfg"], 2.5)


class _Sampling:
    """Заглушка yue2.protocol.Sampling: как в библиотеке, temperature по
    умолчанию 1.0; хранит все переданные параметры."""

    def __init__(self, **kw):
        self.temperature = 1.0
        self.max_tokens = None
        for k, v in kw.items():
            setattr(self, k, v)


class _RecordingModel:
    """Пайплайн-заглушка: записывает каждый вызов (имя, kwargs) и прерывает
    генерацию ошибкой — дальше шага генерации тесту идти не нужно."""

    def __init__(self):
        self.calls = []

    def __enter__(self):
        return self

    def __exit__(self, *args):
        pass

    def _rec(self, name, kw):
        self.calls.append((name, kw))
        raise RuntimeError("stop after recording")

    def __call__(self, *a, **kw):
        self._rec("__call__", kw)

    def __getattr__(self, name):
        if name.startswith("__"):
            raise AttributeError(name)
        return lambda *a, **kw: self._rec(name, kw)


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestCharacterGeneration(_WorkerApiCase):
    """W6: _run_job передаёт пайплайну cfg_scale и semantic_sampling.temperature
    по джобе; 0/0 → CFG_SCALE модуля и температура 1.0."""

    def setUp(self):
        super().setUp()
        from unittest import mock
        protocol = types.ModuleType("yue2.protocol")
        protocol.Sampling = _Sampling
        yue2 = mock.MagicMock()
        mods = {"yue2": yue2, "yue2.protocol": protocol,
                "yue2.pipeline": mock.MagicMock(), "torch": mock.MagicMock()}
        p = mock.patch.dict(sys.modules, mods)
        p.start()
        self.addCleanup(p.stop)
        self.model = _RecordingModel()
        for name, val in (("_pipe", self.model), ("_load_error", None)):
            p = mock.patch.object(self.w, name, val)
            p.start()
            self.addCleanup(p.stop)

    def _gen_kwargs(self, **extra):
        jid = _job_id(self, _post_job(self, **extra))
        self.w._run_job(jid)  # исключение наружу = провал теста
        calls = [kw for _, kw in self.model.calls
                 if "cfg_scale" in kw or "semantic_sampling" in kw]
        self.assertTrue(calls, f"генерация не вызвана: {self.model.calls}")
        return calls[0]

    def test_w6_job_values_reach_pipeline(self):
        kw = self._gen_kwargs(temperature=1.15, cfg=2.5)
        self.assertAlmostEqual(kw.get("cfg_scale"), 2.5)
        s = kw.get("semantic_sampling")
        self.assertIsNotNone(s, kw)
        self.assertAlmostEqual(s.temperature, 1.15)
        self.assertTrue(s.max_tokens, "бюджет длины (max_tokens) потерян")

    def test_w6_defaults_when_zero(self):
        kw = self._gen_kwargs()
        self.assertAlmostEqual(kw.get("cfg_scale"), self.w.CFG_SCALE)
        s = kw.get("semantic_sampling")
        self.assertIsNotNone(s, kw)
        self.assertAlmostEqual(s.temperature, 1.0)
        self.assertTrue(s.max_tokens, "бюджет длины (max_tokens) потерян")

    def test_w6_only_temperature_keeps_default_cfg(self):
        kw = self._gen_kwargs(temperature=0.8)
        self.assertAlmostEqual(kw.get("cfg_scale"), self.w.CFG_SCALE)
        self.assertAlmostEqual(kw["semantic_sampling"].temperature, 0.8)


if __name__ == "__main__":
    unittest.main()
