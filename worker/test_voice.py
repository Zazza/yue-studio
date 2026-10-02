"""Тесты замены голоса (Seed-VC): чистые функции voice.py и API
POST /jobs/{id}/voice — по спецификации, не по реализации.

Запуск: cd worker && python3 -m unittest test_voice
Без numpy (CI-джоба только с ruff) классы пропускаются, как в test_pure.
"""
import json
import math
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from test_pure import _HAS_WORKER_DEPS, _WorkerApiCase

try:
    import numpy as np
    _HAS_NUMPY = True
except ImportError:
    _HAS_NUMPY = False

SR = 44100


def _sine(amp=0.5, freq=440.0, sec=2.0, sr=SR):
    t = np.arange(int(sec * sr)) / sr
    return (amp * np.sin(2 * np.pi * freq * t)).astype(np.float32)


@unittest.skipUnless(_HAS_NUMPY, "нужен numpy (окружение воркера)")
class TestActiveRms(unittest.TestCase):
    """active_rms: RMS только по громким кадрам; паузы уровень не тянут."""

    def setUp(self):
        import voice
        self.v = voice

    def test_sine_without_pauses_is_amp_over_sqrt2(self):
        self.assertAlmostEqual(self.v.active_rms(_sine(0.5)), 0.5 / math.sqrt(2), delta=0.01)

    def test_half_silence_keeps_level(self):
        x = np.concatenate([_sine(0.5, sec=2.0), np.zeros(2 * SR, dtype=np.float32)])
        full = self.v.active_rms(_sine(0.5, sec=2.0))
        got = self.v.active_rms(x)
        # RMS по всему сигналу дал бы 0.25 (A/2) — тут должно остаться ≈ A/√2
        self.assertAlmostEqual(got, full, delta=full * 0.05)
        self.assertGreater(got, 0.3)

    def test_scales_linearly(self):
        x = _sine(0.2)
        base = self.v.active_rms(x)
        self.assertAlmostEqual(self.v.active_rms(x * 3), base * 3, delta=base * 0.01)

    def test_silence_and_empty_are_zero(self):
        self.assertEqual(self.v.active_rms(np.zeros(SR, dtype=np.float32)), 0)
        self.assertEqual(self.v.active_rms(np.zeros(0, dtype=np.float32)), 0)

    def test_stereo_channels_averaged(self):
        mono = _sine(0.5)
        stereo = np.stack([mono, mono], axis=1)
        self.assertAlmostEqual(self.v.active_rms(stereo), self.v.active_rms(mono), delta=0.005)

    def test_custom_frame(self):
        self.assertAlmostEqual(self.v.active_rms(_sine(0.5), frame=1024),
                               0.5 / math.sqrt(2), delta=0.01)


@unittest.skipUnless(_HAS_NUMPY, "нужен numpy (окружение воркера)")
class TestMatchGain(unittest.TestCase):
    def setUp(self):
        import voice
        self.v = voice

    def test_gain_matches_levels(self):
        src, conv = _sine(0.5), _sine(0.1, freq=330)
        g = self.v.match_gain(src, conv)
        self.assertAlmostEqual(g, 5.0, delta=0.1)
        self.assertAlmostEqual(self.v.active_rms(conv * g), self.v.active_rms(src), delta=0.005)

    def test_quieter_source_gives_gain_below_one(self):
        self.assertLess(self.v.match_gain(_sine(0.1), _sine(0.5)), 1.0)

    def test_zero_level_gives_one(self):
        z = np.zeros(SR, dtype=np.float32)
        self.assertEqual(self.v.match_gain(z, _sine(0.3)), 1.0)
        self.assertEqual(self.v.match_gain(_sine(0.3), z), 1.0)
        self.assertEqual(self.v.match_gain(z, z), 1.0)


@unittest.skipUnless(_HAS_NUMPY, "нужен numpy (окружение воркера)")
class TestMix(unittest.TestCase):
    def setUp(self):
        import voice
        self.v = voice

    def test_sum_of_tracks_and_vocal_with_gain(self):
        a = np.full((100, 2), 0.1, dtype=np.float32)
        b = np.full((100, 2), 0.2, dtype=np.float32)
        voc = np.full((100, 2), 0.1, dtype=np.float32)
        out = self.v.mix([a, b], voc, gain=2.0)
        self.assertEqual(out.dtype, np.float32)
        self.assertEqual(out.shape, (100, 2))
        np.testing.assert_allclose(out, 0.5, atol=1e-6)

    def test_default_gain_is_one(self):
        a = np.full((50, 2), 0.1, dtype=np.float32)
        voc = np.full((50, 2), 0.2, dtype=np.float32)
        np.testing.assert_allclose(self.v.mix([a], voc), 0.3, atol=1e-6)

    def test_length_is_longest_instrumental(self):
        a = np.full((100, 2), 0.1, dtype=np.float32)
        b = np.full((80, 2), 0.1, dtype=np.float32)
        out = self.v.mix([a, b], np.zeros((60, 2), dtype=np.float32))
        self.assertEqual(out.shape, (100, 2))
        np.testing.assert_allclose(out[:80], 0.2, atol=1e-6)
        np.testing.assert_allclose(out[80:], 0.1, atol=1e-6)

    def test_longer_vocal_trimmed(self):
        a = np.zeros((100, 2), dtype=np.float32)
        voc = np.full((150, 2), 0.3, dtype=np.float32)
        out = self.v.mix([a], voc)
        self.assertEqual(out.shape, (100, 2))
        np.testing.assert_allclose(out, 0.3, atol=1e-6)

    def test_shorter_vocal_padded_with_silence(self):
        a = np.full((100, 2), 0.1, dtype=np.float32)
        voc = np.full((40, 2), 0.3, dtype=np.float32)
        out = self.v.mix([a], voc)
        self.assertEqual(out.shape, (100, 2))
        np.testing.assert_allclose(out[:40], 0.4, atol=1e-6)
        np.testing.assert_allclose(out[40:], 0.1, atol=1e-6)

    def test_mono_vocal_goes_to_both_channels(self):
        a = np.zeros((SR // 10, 2), dtype=np.float32)
        voc = _sine(0.3, sec=0.1)
        out = self.v.mix([a], voc, gain=1.5)
        self.assertEqual(out.shape, (SR // 10, 2))
        np.testing.assert_allclose(out[:, 0], out[:, 1], atol=1e-7)
        np.testing.assert_allclose(out[:, 0], voc * 1.5, atol=1e-6)

    def test_peak_above_limit_scaled_to_099(self):
        a = np.zeros((1000, 2), dtype=np.float32)
        a[:, 0] = 0.8
        a[:, 1] = 0.4
        voc = np.full((1000, 2), 0.8, dtype=np.float32)
        out = self.v.mix([a], voc)
        self.assertAlmostEqual(float(np.max(np.abs(out))), 0.99, delta=1e-4)
        # масштаб общий, а не клип: соотношение каналов сохраняется (1.6 : 1.2)
        self.assertAlmostEqual(float(out[0, 1] / out[0, 0]), 1.2 / 1.6, delta=1e-4)

    def test_negative_peak_also_limited(self):
        a = np.full((100, 2), -0.9, dtype=np.float32)
        voc = np.full((100, 2), -0.9, dtype=np.float32)
        out = self.v.mix([a], voc)
        self.assertAlmostEqual(float(np.max(np.abs(out))), 0.99, delta=1e-4)

    def test_peak_within_limit_unchanged(self):
        a = np.full((100, 2), 0.5, dtype=np.float32)
        voc = np.full((100, 2), 0.49, dtype=np.float32)
        out = self.v.mix([a], voc)
        np.testing.assert_allclose(out, 0.99, atol=1e-6)


@unittest.skipUnless(_HAS_NUMPY, "нужен numpy (окружение воркера)")
class TestResample(unittest.TestCase):
    def setUp(self):
        import voice
        self.v = voice

    @staticmethod
    def _peak_hz(x, sr):
        spec = np.abs(np.fft.rfft(x))
        return float(np.argmax(spec)) * sr / len(x)

    def test_same_rate_returns_unchanged(self):
        x = _sine(0.5, sec=0.5)
        out = self.v.resample(x, SR, SR)
        self.assertEqual(out.shape, x.shape)
        np.testing.assert_array_equal(out, x)

    def test_length_scales(self):
        x = _sine(0.5, sec=1.0)
        for sr_to in (22050, 48000, 16000):
            out = self.v.resample(x, SR, sr_to)
            self.assertLessEqual(abs(len(out) - len(x) * sr_to / SR), 1, sr_to)

    def test_frequency_preserved(self):
        x = _sine(0.5, freq=1000, sec=1.0)
        for sr_to in (22050, 48000):
            out = np.asarray(self.v.resample(x, SR, sr_to))
            self.assertAlmostEqual(self._peak_hz(out, sr_to), 1000, delta=5, msg=sr_to)

    def test_stereo(self):
        mono = _sine(0.5, freq=1000, sec=1.0)
        x = np.stack([mono, mono * 0.5], axis=1)
        out = np.asarray(self.v.resample(x, SR, 48000))
        self.assertEqual(out.ndim, 2)
        self.assertEqual(out.shape[1], 2)
        self.assertLessEqual(abs(out.shape[0] - SR * 48000 / SR), 1)
        self.assertAlmostEqual(self._peak_hz(out[:, 0], 48000), 1000, delta=5)
        self.assertAlmostEqual(self._peak_hz(out[:, 1], 48000), 1000, delta=5)


@unittest.skipUnless(_HAS_NUMPY, "нужен numpy (окружение воркера)")
class TestAvailableAndLimits(unittest.TestCase):
    def setUp(self):
        import voice
        self.v = voice
        td = tempfile.TemporaryDirectory()
        self.addCleanup(td.cleanup)
        self.dir = Path(td.name)
        self.py = self.dir / "venv" / "bin" / "python"
        self.inf = self.dir / "code" / "inference.py"
        for name, val in (("SEEDVC_DIR", self.dir), ("SEEDVC_PY", self.py)):
            p = mock.patch.object(voice, name, val)
            p.start()
            self.addCleanup(p.stop)

    @staticmethod
    def _touch(p):
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text("")

    def test_empty_dir_unavailable(self):
        self.assertFalse(self.v.available())

    def test_only_python_unavailable(self):
        self._touch(self.py)
        self.assertFalse(self.v.available())

    def test_only_inference_unavailable(self):
        self._touch(self.inf)
        self.assertFalse(self.v.available())

    def test_both_present_available(self):
        self._touch(self.py)
        self._touch(self.inf)
        self.assertTrue(self.v.available())

    def test_limits(self):
        self.assertEqual((self.v.REF_MIN_SEC, self.v.REF_MAX_SEC), (3, 30))
        self.assertEqual((self.v.STEPS_MIN, self.v.STEPS_MAX), (10, 100))


@unittest.skipUnless(_HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestVoiceJobApi(_WorkerApiCase):
    """POST /jobs/{id}/voice: джоба замены голоса трека тембром образца."""

    def _avail(self, value):
        p = mock.patch.object(self.w.voicevc, "available", return_value=value)
        p.start()
        self.addCleanup(p.stop)

    def _done(self, style="dark rock", audio=True, status="done"):
        jid = self._job(style=style, status=status, semantic=False)
        if audio:
            d = self.jobs_dir / str(jid)
            d.mkdir(parents=True, exist_ok=True)
            (d / "audio.flac").write_bytes(b"x")
            with self._conn() as c:
                c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid

    def _count(self):
        with self._conn() as c:
            return c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0]

    def test_not_installed_503(self):
        self._avail(False)
        track, ref = self._done(), self._done()
        before = self._count()
        r = self.client.post(f"/jobs/{track}/voice", json={"ref_job_id": ref})
        self.assertEqual(r.status_code, 503, r.text)
        self.assertEqual(self._count(), before)

    def test_missing_track_or_ref_404(self):
        self._avail(True)
        good = self._done()
        cases = {
            "track missing": (9999, good),
            "ref missing": (good, 9999),
            "track not done": (self._done(status="running"), good),
            "ref not done": (good, self._done(status="queued")),
            "track no audio": (self._done(audio=False), good),
            "ref no audio": (good, self._done(audio=False)),
        }
        before = self._count()
        for name, (track, ref) in cases.items():
            r = self.client.post(f"/jobs/{track}/voice", json={"ref_job_id": ref})
            self.assertEqual(r.status_code, 404, (name, r.status_code, r.text))
        self.assertEqual(self._count(), before)

    def test_validation_422(self):
        self._avail(True)
        track, ref = self._done(), self._done()
        before = self._count()
        for extra in ({"ref_dur": 2.9}, {"ref_dur": 30.5}, {"steps": 9}, {"steps": 101},
                      {"ref_from": -1}):
            r = self.client.post(f"/jobs/{track}/voice", json={"ref_job_id": ref, **extra})
            self.assertEqual(r.status_code, 422, (extra, r.status_code))
        self.assertEqual(self._count(), before)

    def test_boundaries_accepted(self):
        self._avail(True)
        track, ref = self._done(), self._done()
        for extra in ({"ref_dur": 3, "steps": 10, "ref_from": 0},
                      {"ref_dur": 30, "steps": 100}):
            r = self.client.post(f"/jobs/{track}/voice", json={"ref_job_id": ref, **extra})
            self.assertEqual(r.status_code, 200, (extra, r.text))

    def test_success_creates_queued_voice_job(self):
        self._avail(True)
        track = self._done(style="dark rock, male baritone")
        ref = self._done()
        r = self.client.post(f"/jobs/{track}/voice", json={"ref_job_id": ref})
        self.assertEqual(r.status_code, 200, r.text)
        new_id = r.json()["id"]
        self.assertNotIn(new_id, (track, ref))
        row, parent = self._row(new_id), self._row(track)
        self.assertEqual(row["status"], "queued")
        self.assertEqual(row["role"], "voice")
        self.assertEqual(row["parent_id"], track)
        self.assertIn(f"голос #{ref}", row["title"])
        self.assertEqual(row["style"], parent["style"])
        self.assertEqual(row["lyrics"], parent["lyrics"])
        p = json.loads((self.jobs_dir / str(new_id) / "voice.json").read_text())
        self.assertEqual(p["ref_job_id"], ref)
        self.assertEqual(float(p["ref_from"]), 0)
        self.assertEqual(float(p["ref_dur"]), 25)
        self.assertEqual(int(p["steps"]), 50)

    def test_custom_params_and_title(self):
        self._avail(True)
        track, ref = self._done(), self._done()
        r = self.client.post(f"/jobs/{track}/voice", json={
            "ref_job_id": ref, "ref_from": 12.5, "ref_dur": 10, "steps": 30,
            "title": "Мой баритон"})
        self.assertEqual(r.status_code, 200, r.text)
        new_id = r.json()["id"]
        self.assertEqual(self._row(new_id)["title"], "Мой баритон")
        p = json.loads((self.jobs_dir / str(new_id) / "voice.json").read_text())
        self.assertEqual(p["ref_job_id"], ref)
        self.assertAlmostEqual(float(p["ref_from"]), 12.5)
        self.assertAlmostEqual(float(p["ref_dur"]), 10)
        self.assertEqual(int(p["steps"]), 30)

    def test_config_reports_seedvc(self):
        r = self.client.get("/config")
        self.assertEqual(r.status_code, 200, r.text)
        cfg = r.json()
        self.assertIn("seedvc_dir", cfg)
        self.assertIn("seedvc_available", cfg)
        self.assertIsInstance(cfg["seedvc_available"], bool)


@unittest.skipUnless(_HAS_NUMPY, "нужен numpy (окружение воркера)")
class TestActivityMask(unittest.TestCase):
    """activity_mask: 1 где голос (с запасом по краям), 0 в длинной тишине,
    плавный переход; абсолютный пол -55 dBFS и относительный порог по треку."""

    HOLD = 0.15   # GATE_HOLD_SEC
    FADE = 0.03   # GATE_FADE_SEC
    FRAME = 0.02  # GATE_FRAME_SEC

    def setUp(self):
        import voice
        self.v = voice

    def _silence(self, sec):
        return np.zeros(int(sec * SR), dtype=np.float32)

    def _voiced(self, pre=1.0, voice_sec=1.0, post=1.0, amp=0.5):
        """тишина pre, громкий синус voice_sec, тишина post."""
        return np.concatenate([self._silence(pre), _sine(amp, sec=voice_sec), self._silence(post)])

    def _at(self, sec):
        return int(sec * SR)

    def test_length_dtype_and_range(self):
        x = np.concatenate([self._voiced(), np.zeros(7, dtype=np.float32)])
        m = self.v.activity_mask(x, SR)
        self.assertEqual(len(m), len(x))
        self.assertEqual(m.dtype, np.float32)
        self.assertGreaterEqual(float(m.min()), 0.0)
        self.assertLessEqual(float(m.max()), 1.0)

    def test_stereo_input_one_value_per_frame(self):
        mono = self._voiced()
        x = np.stack([mono, mono], axis=1)
        m = self.v.activity_mask(x, SR)
        self.assertEqual(m.shape, (len(mono),))
        np.testing.assert_allclose(m, self.v.activity_mask(mono, SR), atol=1e-6)

    def test_stereo_channels_are_averaged(self):
        # противофазные каналы в среднем дают тишину — голоса нет
        mono = self._voiced()
        m = self.v.activity_mask(np.stack([mono, -mono], axis=1), SR)
        self.assertEqual(len(m), len(mono))
        self.assertEqual(float(m.max()), 0.0)

    def test_voice_is_one_long_silence_is_zero(self):
        m = self.v.activity_mask(self._voiced(pre=1.0, voice_sec=1.0, post=1.0), SR)
        # громкий участок 1.0..2.0 с
        self.assertTrue(np.all(m[self._at(1.05):self._at(1.95)] == 1.0))
        # дальше HOLD+FADE от голоса (с запасом на кадр) — ноль
        far = self.HOLD + self.FADE + 3 * self.FRAME
        self.assertTrue(np.all(m[:self._at(1.0 - far)] == 0.0))
        self.assertTrue(np.all(m[self._at(2.0 + far):] == 0.0))

    def test_hold_keeps_mask_open_around_voice(self):
        m = self.v.activity_mask(self._voiced(pre=1.0, voice_sec=1.0, post=1.0), SR)
        margin = self.HOLD - 2 * self.FRAME  # ~0.11 с — внутри запаса
        # до начала голоса (вдох) и после конца (окончание) — ещё 1.0
        self.assertTrue(np.all(m[self._at(1.0 - margin):self._at(1.0)] == 1.0))
        self.assertTrue(np.all(m[self._at(2.0):self._at(2.0 + margin)] == 1.0))

    def test_transition_is_smooth(self):
        m = self.v.activity_mask(self._voiced(), SR)
        # нет скачка 1→0 между соседними сэмплами
        self.assertLess(float(np.max(np.abs(np.diff(m)))), 0.5)
        # есть промежуточные значения на обоих краях
        mid = (m > 0.0) & (m < 1.0)
        self.assertTrue(mid[:self._at(1.0)].any(), "нет плавного входа")
        self.assertTrue(mid[self._at(2.0):].any(), "нет плавного выхода")

    def test_absolute_floor_quiet_signal_is_zero(self):
        # синус 1e-4 ≈ -83 dBFS, ниже пола -55 — голоса нет, хотя он «громкий» относительно себя
        m = self.v.activity_mask(_sine(1e-4, sec=2.0), SR)
        self.assertEqual(len(m), self._at(2.0))
        self.assertEqual(float(m.max()), 0.0)

    def test_relative_threshold_leak_is_zero(self):
        # громко 0.5 → тишина → «утечка» на 40 дБ тише (0.005 ≈ -49 dBFS, выше пола)
        x = np.concatenate([
            _sine(0.5, sec=1.0), self._silence(1.0),
            _sine(0.005, sec=1.0), self._silence(1.0)])
        m = self.v.activity_mask(x, SR)
        self.assertTrue(np.all(m[self._at(0.05):self._at(0.95)] == 1.0))
        far = self.HOLD + self.FADE + 3 * self.FRAME
        # вся утечка 2.0..3.0 с — дальше запаса от громкого места
        self.assertTrue(np.all(m[self._at(1.0 + far):] == 0.0))

    def test_empty_input(self):
        m = self.v.activity_mask(np.zeros(0, dtype=np.float32), SR)
        self.assertEqual(len(m), 0)

    def test_full_silence_all_zeros(self):
        x = self._silence(2.0)
        m = self.v.activity_mask(x, SR)
        self.assertEqual(len(m), len(x))
        self.assertEqual(float(m.max()), 0.0)


if __name__ == "__main__":
    unittest.main()
