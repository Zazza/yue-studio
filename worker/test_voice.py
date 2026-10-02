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
        self.assertIsNone(p["ref_from"])   # не задано — окно подберёт воркер (best_window)
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


@unittest.skipUnless(_HAS_NUMPY, "нужен numpy (окружение воркера)")
class TestReplaceVocal(unittest.TestCase):
    """replace_vocal: в готовом миксе старый голос вычитается и добавляется новый
    по маске (mix − mask·vocals + mask·gain·new); вне маски микс не трогается."""

    N = 1000

    def setUp(self):
        import voice
        self.v = voice

    def _const(self, val, n=None, ch=None):
        n = self.N if n is None else n
        shape = (n,) if ch is None else (n, ch)
        return np.full(shape, val, dtype=np.float32)

    def _rand_mix(self, n=None, ch=2, seed=1):
        rng = np.random.default_rng(seed)
        n = self.N if n is None else n
        return (rng.uniform(-0.4, 0.4, (n, ch))).astype(np.float32)

    def test_returns_float32_frames_channels(self):
        out = self.v.replace_vocal(self._rand_mix(), self._const(0.1), self._const(0.2),
                                   self._const(1.0))
        self.assertEqual(out.dtype, np.float32)
        self.assertEqual(out.shape, (self.N, 2))

    # 1. mask = 0 → бит в бит mix
    def test_zero_mask_is_bit_exact_mix(self):
        mix = self._rand_mix()
        rng = np.random.default_rng(7)
        vocals = rng.uniform(-0.3, 0.3, self.N).astype(np.float32)
        new = rng.uniform(-0.3, 0.3, self.N).astype(np.float32)
        mask = np.zeros(self.N, dtype=np.float32)
        out = self.v.replace_vocal(mix, vocals, new, mask, gain=2.0)
        np.testing.assert_array_equal(out, mix.astype(np.float32))

    def test_zero_mask_region_bit_exact_inside_partial_mask(self):
        mix = self._rand_mix()
        rng = np.random.default_rng(3)
        vocals = rng.uniform(-0.3, 0.3, self.N).astype(np.float32)
        new = rng.uniform(-0.3, 0.3, self.N).astype(np.float32)
        mask = np.zeros(self.N, dtype=np.float32)
        mask[300:600] = 1.0
        out = self.v.replace_vocal(mix, vocals, new, mask)
        np.testing.assert_array_equal(out[:300], mix[:300])
        np.testing.assert_array_equal(out[600:], mix[600:])

    # 2. mask = 1, new = vocals, gain = 1 → mix
    def test_replace_with_itself_is_mix(self):
        mix = self._rand_mix()
        rng = np.random.default_rng(5)
        vocals = rng.uniform(-0.3, 0.3, self.N).astype(np.float32)
        out = self.v.replace_vocal(mix, vocals, vocals.copy(), self._const(1.0), gain=1.0)
        np.testing.assert_allclose(out, mix, atol=1e-6)

    # 3. mask = 1 → старый вычтен, новый добавлен с gain
    def test_full_mask_subtracts_old_adds_new_with_gain(self):
        out = self.v.replace_vocal(self._const(0.5, ch=2), self._const(0.3), self._const(0.2),
                                   self._const(1.0), gain=0.5)
        np.testing.assert_allclose(out, 0.5 - 0.3 + 0.5 * 0.2, atol=1e-6)

    def test_default_gain_is_one(self):
        out = self.v.replace_vocal(self._const(0.5, ch=2), self._const(0.3), self._const(0.2),
                                   self._const(1.0))
        np.testing.assert_allclose(out, 0.5 - 0.3 + 0.2, atol=1e-6)

    def test_fractional_mask_blends(self):
        out = self.v.replace_vocal(self._const(0.5, ch=2), self._const(0.3), self._const(0.2),
                                   self._const(0.5), gain=1.0)
        np.testing.assert_allclose(out, 0.5 - 0.5 * 0.3 + 0.5 * 0.2, atol=1e-6)

    # 4. форма: моно в оба канала, длина = mix, паддинг/обрезка
    def test_mono_1d_goes_to_both_channels(self):
        mix = self._rand_mix()
        rng = np.random.default_rng(9)
        vocals = rng.uniform(-0.2, 0.2, self.N).astype(np.float32)
        new = rng.uniform(-0.2, 0.2, self.N).astype(np.float32)
        out = self.v.replace_vocal(mix, vocals, new, self._const(1.0), gain=0.7)
        for c in range(2):
            np.testing.assert_allclose(out[:, c], mix[:, c] - vocals + 0.7 * new, atol=1e-6)

    def test_mono_column_same_as_1d(self):
        mix = self._rand_mix()
        rng = np.random.default_rng(11)
        vocals = rng.uniform(-0.2, 0.2, self.N).astype(np.float32)
        new = rng.uniform(-0.2, 0.2, self.N).astype(np.float32)
        mask = self._const(1.0)
        flat = self.v.replace_vocal(mix, vocals, new, mask)
        col = self.v.replace_vocal(mix, vocals[:, None], new[:, None], mask)
        self.assertEqual(col.shape, (self.N, 2))
        np.testing.assert_allclose(col, flat, atol=1e-7)

    def test_short_inputs_padded_with_zeros(self):
        mix = self._rand_mix()
        half = self.N // 2
        out = self.v.replace_vocal(mix, self._const(0.3, n=half), self._const(0.2, n=half),
                                   self._const(1.0, n=half))
        self.assertEqual(out.shape, (self.N, 2))
        np.testing.assert_allclose(out[:half], mix[:half] - 0.3 + 0.2, atol=1e-6)
        # за концом маски маска = 0 → чистый mix
        np.testing.assert_array_equal(out[half:], mix[half:])

    def test_short_vocals_and_new_under_full_mask(self):
        mix = self._rand_mix()
        half = self.N // 2
        out = self.v.replace_vocal(mix, self._const(0.3, n=half), self._const(0.2, n=half),
                                   self._const(1.0))
        np.testing.assert_allclose(out[half:], mix[half:], atol=1e-6)

    def test_long_inputs_truncated(self):
        mix = self._rand_mix()
        n2 = self.N * 2
        out = self.v.replace_vocal(mix, self._const(0.3, n=n2), self._const(0.2, n=n2),
                                   self._const(1.0, n=n2))
        self.assertEqual(out.shape, (self.N, 2))
        np.testing.assert_allclose(out, mix - 0.3 + 0.2, atol=1e-6)

    # 5. mix 1D → (frames, 1)
    def test_mono_mix_gives_column(self):
        mix = self._const(0.5)
        out = self.v.replace_vocal(mix, self._const(0.3), self._const(0.2), self._const(1.0))
        self.assertEqual(out.shape, (self.N, 1))
        np.testing.assert_allclose(out[:, 0], 0.4, atol=1e-6)

    # 6. пик > 0.99 → локальный ограничитель: |out| ≤ 0.99, трек целиком не масштабируется
    def _far_from(self, over, n, dist):
        """Булева маска сэмплов дальше dist от любого индекса из over."""
        far = np.ones(n, dtype=bool)
        for i in np.flatnonzero(over):
            far[max(0, i - dist):i + dist + 1] = False
        return far

    def test_peak_over_limit_clamped_locally(self):
        n = 6000
        # значения точно представимы в float32: 0.25 − 0.125 + 0.5 = 0.625 в любом порядке
        mix = np.full((n, 2), 0.25, dtype=np.float32)
        vocals = np.full(n, 0.125, dtype=np.float32)
        new = np.full(n, 0.5, dtype=np.float32)
        new[3000:3050] = 1.5          # под маской: 0.25 − 0.125 + 1.5 = 1.625 > 0.99
        mask = np.ones(n, dtype=np.float32)
        out = self.v.replace_vocal(mix, vocals, new, mask)
        self.assertLessEqual(float(np.max(np.abs(out))), 0.99 + 1e-7)  # float32: 0.99 = 0.9900000095
        over = np.zeros(n, dtype=bool)
        over[3000:3050] = True
        far = self._far_from(over, n, 3 * self.v.LIMIT_WIN)
        self.assertTrue(far.any())
        # вдали от пика — формула бит в бит, без общего приглушения
        np.testing.assert_array_equal(out[far], np.full((int(far.sum()), 2), 0.625,
                                                        dtype=np.float32))

    def test_peak_outside_mask_far_region_is_bit_exact_mix(self):
        n = 6000
        mix = self._rand_mix(n=n)
        mask = np.zeros(n, dtype=np.float32)
        mask[3000:3100] = 1.0
        new = np.full(n, 1.5, dtype=np.float32)
        out = self.v.replace_vocal(mix, np.zeros(n, dtype=np.float32), new, mask)
        self.assertLessEqual(float(np.max(np.abs(out))), 0.99 + 1e-7)  # float32: 0.99 = 0.9900000095
        w3 = 3 * self.v.LIMIT_WIN
        np.testing.assert_array_equal(out[:3000 - w3 - 1], mix[:3000 - w3 - 1])
        np.testing.assert_array_equal(out[3100 + w3 + 1:], mix[3100 + w3 + 1:])

    def test_negative_peak_over_limit_clamped(self):
        mix = self._const(-0.5, ch=2)
        mix[:, 1] = 0.25
        out = self.v.replace_vocal(mix, self._const(0.5), self._const(-0.5),
                                   self._const(1.0), gain=2.0)
        # без ограничителя: канал 0 = −2.0, канал 1 = −1.25 на всей длине
        self.assertLessEqual(float(np.max(np.abs(out))), 0.99 + 1e-7)  # float32: 0.99 = 0.9900000095
        self.assertTrue(np.all(out[:, 0] < 0))
        # усиление общее для каналов — отношение сохраняется
        np.testing.assert_allclose(out[:, 1] / out[:, 0], 1.25 / 2.0, rtol=1e-5)

    def test_peak_within_limit_not_scaled(self):
        out = self.v.replace_vocal(self._const(0.5, ch=2), self._const(0.1), self._const(0.5),
                                   self._const(1.0))
        np.testing.assert_allclose(out, 0.9, atol=1e-6)

    def test_peak_exactly_099_not_scaled(self):
        mix = self._const(0.49, ch=2)
        out = self.v.replace_vocal(mix, self._const(0.0), self._const(0.5), self._const(1.0))
        np.testing.assert_allclose(out, np.float32(0.49) + np.float32(0.5), atol=1e-6)

    # 7. вход не мутируется
    def test_inputs_not_mutated(self):
        mix = self._rand_mix()
        rng = np.random.default_rng(13)
        vocals = rng.uniform(-0.3, 0.3, self.N).astype(np.float32)
        new = rng.uniform(-0.9, 0.9, self.N).astype(np.float32)
        mask = rng.uniform(0, 1, self.N).astype(np.float32)
        copies = [a.copy() for a in (mix, vocals, new, mask)]
        self.v.replace_vocal(mix, vocals, new, mask, gain=3.0)
        for a, c in zip((mix, vocals, new, mask), copies, strict=True):
            np.testing.assert_array_equal(a, c)


@unittest.skipUnless(_HAS_NUMPY, "нужен numpy (окружение воркера)")
class TestLimitPeaks(unittest.TestCase):
    """limit_peaks: прижать только пики выше limit плавным локальным усилением;
    вдали от пиков (> 3·win) сигнал не меняется, ниже limit — не меняется вовсе."""

    def setUp(self):
        import voice
        self.v = voice

    @staticmethod
    def _far(x, limit, dist):
        """Маска кадров дальше dist от любого кадра, где |x| > limit."""
        a = np.abs(x) if x.ndim == 1 else np.max(np.abs(x), axis=1)
        far = np.ones(len(a), dtype=bool)
        for i in np.flatnonzero(a > limit):
            far[max(0, i - dist):i + dist + 1] = False
        return far

    @staticmethod
    def _smooth(n=20000, base=0.8, amp=0.5, period=8000.0):
        # гладкий, положительный (0.3…1.3): усиление out/x определено везде
        t = np.arange(n)
        return (base + amp * np.sin(2 * np.pi * t / period)).astype(np.float32)

    # ниже/на пороге — без изменений
    def test_within_limit_unchanged_stereo(self):
        x = np.random.default_rng(1).uniform(-0.9, 0.9, (5000, 2)).astype(np.float32)
        np.testing.assert_array_equal(self.v.limit_peaks(x), x)

    def test_within_limit_unchanged_1d(self):
        x = _sine(amp=0.9, sec=0.2)
        np.testing.assert_array_equal(self.v.limit_peaks(x), x)

    def test_peak_exactly_at_limit_unchanged(self):
        x = np.zeros(3000, dtype=np.float64)
        x[1500] = 0.99
        x[1600] = -0.99
        np.testing.assert_array_equal(self.v.limit_peaks(x), x)

    def test_custom_limit_within_unchanged(self):
        x = _sine(amp=0.4, sec=0.2)
        np.testing.assert_array_equal(self.v.limit_peaks(x, limit=0.5), x)

    # над порогом — |out| ≤ limit
    def test_over_limit_clamped_1d(self):
        x = self._smooth()
        out = self.v.limit_peaks(x)
        self.assertLessEqual(float(np.max(np.abs(out))), 0.99 + 1e-9)

    def test_negative_peaks_clamped(self):
        x = -self._smooth()
        out = self.v.limit_peaks(x)
        self.assertLessEqual(float(np.max(np.abs(out))), 0.99 + 1e-9)

    def test_single_sample_spike_clamped(self):
        x = np.zeros(4000, dtype=np.float32)
        x[2000] = 2.0
        out = self.v.limit_peaks(x)
        self.assertLessEqual(float(np.max(np.abs(out))), 0.99 + 1e-9)

    def test_all_samples_over_limit(self):
        x = np.full((3000, 2), 1.5, dtype=np.float32)
        out = self.v.limit_peaks(x)
        self.assertLessEqual(float(np.max(np.abs(out))), 0.99 + 1e-9)

    def test_custom_limit_respected(self):
        x = _sine(amp=0.9, sec=0.5)
        out = self.v.limit_peaks(x, limit=0.5)
        self.assertLessEqual(float(np.max(np.abs(out))), 0.5 + 1e-9)

    # вдали от пиков — точное равенство
    def test_far_from_peak_bit_exact_1d(self):
        x = _sine(amp=0.5, sec=0.5)
        x[10000:10050] *= 3.0
        out = self.v.limit_peaks(x)
        far = self._far(x, 0.99, 3 * self.v.LIMIT_WIN)
        self.assertGreater(int(far.sum()), 0)
        self.assertLessEqual(float(np.max(np.abs(out))), 0.99 + 1e-9)
        np.testing.assert_array_equal(out[far], x[far])

    def test_far_from_peak_bit_exact_stereo(self):
        x = np.random.default_rng(2).uniform(-0.6, 0.6, (12000, 2)).astype(np.float32)
        x[3000, 0] = 1.4
        x[8000, 1] = -1.7
        out = self.v.limit_peaks(x)
        far = self._far(x, 0.99, 3 * self.v.LIMIT_WIN)
        self.assertGreater(int(far.sum()), 0)
        self.assertLessEqual(float(np.max(np.abs(out))), 0.99 + 1e-9)
        np.testing.assert_array_equal(out[far], x[far])

    def test_far_region_scales_with_custom_win(self):
        x = _sine(amp=0.5, sec=0.5)
        x[10000:10010] = 1.5
        win = 64
        out = self.v.limit_peaks(x, win=win)
        far = self._far(x, 0.99, 3 * win)
        np.testing.assert_array_equal(out[far], x[far])
        self.assertLessEqual(float(np.max(np.abs(out))), 0.99 + 1e-9)

    # плавность усиления
    def test_gain_changes_smoothly(self):
        x = self._smooth()
        out = self.v.limit_peaks(x)
        g = out.astype(np.float64) / x.astype(np.float64)
        self.assertLess(float(np.max(np.abs(np.diff(g)))), 0.01)

    def test_gain_smooth_around_short_burst(self):
        # гладкий сигнал с резким, но коротким превышением: усиление всё равно без ступенек
        x = np.full(8000, 0.6, dtype=np.float64)
        x[4000:4100] = 1.2
        out = self.v.limit_peaks(x)
        g = out / x
        self.assertLessEqual(float(np.max(np.abs(out))), 0.99 + 1e-9)
        self.assertLess(float(np.max(np.abs(np.diff(g)))), 0.01)

    # стерео: общее усиление
    def test_stereo_gain_shared_between_channels(self):
        left = self._smooth()
        x = np.stack([left, 0.5 * left], axis=1).astype(np.float32)
        out = self.v.limit_peaks(x)
        self.assertLessEqual(float(np.max(np.abs(out))), 0.99 + 1e-9)
        np.testing.assert_allclose(out[:, 1] / out[:, 0], 0.5, rtol=1e-5)

    # форма
    def test_shape_preserved(self):
        for shape in ((5000,), (5000, 1), (5000, 2)):
            x = np.full(shape, 1.3, dtype=np.float32)
            x[:2500] = 0.2
            with self.subTest(shape=shape):
                self.assertEqual(self.v.limit_peaks(x).shape, shape)

    def test_empty_input(self):
        for shape in ((0,), (0, 2)):
            with self.subTest(shape=shape):
                out = self.v.limit_peaks(np.zeros(shape, dtype=np.float32))
                self.assertEqual(out.size, 0)
                self.assertEqual(out.shape, shape)


@unittest.skipUnless(_HAS_NUMPY, "нужен numpy (окружение воркера)")
class TestBestWindow(unittest.TestCase):
    """best_window: начало окна dur с наибольшей долей звучащего голоса, шаг 0.1 с."""

    BSR = 16000

    def setUp(self):
        import voice
        self.v = voice
        self.rng = np.random.default_rng(7)

    def _track(self, total, sung):
        """Тихая утечка 1e-4 на всём треке + шумовое «пение» 0.3 в интервалах sung."""
        n = int(total * self.BSR)
        x = (1e-4 * self.rng.standard_normal(n)).astype(np.float32)
        for a, b in sung:
            i, j = int(a * self.BSR), int(b * self.BSR)
            x[i:j] = (0.3 * self.rng.standard_normal(j - i)).astype(np.float32)
        return x

    def _assert_grid(self, got, x, dur):
        self.assertAlmostEqual(got * 10, round(got * 10), delta=1e-9 * 10)
        self.assertGreaterEqual(got, 0.0)
        self.assertLessEqual(got, len(x) / self.BSR - dur + 1e-9)

    def test_single_sung_region_window_inside(self):
        x = self._track(60, [(30, 45)])
        got = self.v.best_window(x, self.BSR, 10)
        self.assertGreaterEqual(got, 30 - 1e-9)
        self.assertLessEqual(got, 35 + 1e-9)

    def test_prefers_longer_sung_region(self):
        x = self._track(60, [(5, 10), (30, 50)])
        got = self.v.best_window(x, self.BSR, 15)
        self.assertGreaterEqual(got, 30 - 0.2)
        self.assertLessEqual(got, 35 + 0.2)

    def test_track_shorter_than_window_is_zero(self):
        x = self._track(5, [(1, 4)])
        self.assertEqual(self.v.best_window(x, self.BSR, 10), 0.0)

    def test_track_equal_to_window_is_zero(self):
        x = self._track(10, [(6, 9)])
        self.assertEqual(self.v.best_window(x, self.BSR, 10), 0.0)

    def test_empty_track_is_zero(self):
        self.assertEqual(self.v.best_window(np.zeros(0, np.float32), self.BSR, 10), 0.0)

    def test_stereo_same_as_mono_mix(self):
        left = self._track(60, [(30, 45)])
        right = self._track(60, [(30, 45)])
        stereo = np.stack([left, right], axis=1)
        self.assertAlmostEqual(self.v.best_window(stereo, self.BSR, 10),
                               self.v.best_window(stereo.mean(axis=1), self.BSR, 10),
                               delta=1e-9)

    def test_stereo_singing_in_one_channel_found(self):
        sung = self._track(60, [(30, 45)])
        quiet = self._track(60, [])
        got = self.v.best_window(np.stack([quiet, sung], axis=1), self.BSR, 10)
        self.assertGreaterEqual(got, 30 - 1e-9)
        self.assertLessEqual(got, 35 + 1e-9)

    def test_result_on_grid_and_in_bounds(self):
        cases = [(self._track(60, [(30, 45)]), 10),
                 (self._track(60, [(5, 10), (30, 50)]), 15),
                 (self._track(23.37, [(18.33, 23.37)]), 4.55),
                 (self._track(20, [(0, 3)]), 7)]
        for x, dur in cases:
            with self.subTest(dur=dur):
                self._assert_grid(self.v.best_window(x, self.BSR, dur), x, dur)

    def test_singing_at_track_end_reaches_upper_bound(self):
        x = self._track(30, [(22, 30)])
        got = self.v.best_window(x, self.BSR, 10)
        self.assertGreaterEqual(got, 20 - 0.2)
        self.assertLessEqual(got, 20 + 1e-9)


if __name__ == "__main__":
    unittest.main()
