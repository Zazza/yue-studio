"""Тесты замера громкости dsp.loudness по EBU R128 / ITU-R BS.1770 —
по спецификации (эталонные случаи EBU Tech 3341/3342), не по реализации.

Запуск: cd worker && python3 -m unittest test_loudness
Без numpy/scipy (CI-джоба только с ruff) класс пропускается, как в test_pure.
"""
import json
import unittest

try:
    import numpy as np
    import scipy  # noqa: F401
    _HAS_DEPS = True
except ImportError:
    _HAS_DEPS = False


def _stereo_sine(peak, sec, sr, freq=1000.0):
    t = np.arange(int(sec * sr)) / sr
    mono = peak * np.sin(2 * np.pi * freq * t)
    return np.stack([mono, mono], axis=1).astype(np.float64)


def _db(db):
    return 10 ** (db / 20)


@unittest.skipUnless(_HAS_DEPS, "нужны numpy/scipy (окружение воркера)")
class TestLoudness(unittest.TestCase):
    def setUp(self):
        import dsp
        self.loudness = dsp.loudness

    def test_reference_sine_48k_is_minus_23_lufs(self):
        r = self.loudness(_stereo_sine(_db(-23), 20, 48000), 48000)
        self.assertAlmostEqual(r["lufs"], -23.0, delta=0.1)

    def test_reference_sine_44100_is_minus_23_lufs(self):
        r = self.loudness(_stereo_sine(_db(-23), 20, 44100), 44100)
        self.assertAlmostEqual(r["lufs"], -23.0, delta=0.2)

    def test_lufs_and_lra_rounded_to_tenth(self):
        r = self.loudness(_stereo_sine(_db(-17.33), 10, 48000), 48000)
        for key in ("lufs", "lra", "true_peak_db"):
            self.assertAlmostEqual(r[key], round(r[key], 1), places=9, msg=key)

    def test_lra_of_two_levels_10db_apart_is_10(self):
        sr = 48000
        x = np.concatenate([_stereo_sine(_db(-20), 20, sr), _stereo_sine(_db(-30), 20, sr)])
        r = self.loudness(x, sr)
        self.assertAlmostEqual(r["lra"], 10.0, delta=1.0)

    def test_steady_sine_has_near_zero_lra(self):
        r = self.loudness(_stereo_sine(_db(-23), 20, 48000), 48000)
        self.assertLess(r["lra"], 1.0)

    def test_full_scale_sine_true_peak_near_0_dbtp(self):
        r = self.loudness(_stereo_sine(1.0, 5, 48000), 48000)
        self.assertGreaterEqual(r["true_peak_db"], -0.1)
        self.assertLessEqual(r["true_peak_db"], 0.6)

    def test_true_peak_is_plain_float_json_serializable(self):
        r = self.loudness(_stereo_sine(0.5, 5, 48000), 48000)
        self.assertIs(type(r["true_peak_db"]), float)
        json.dumps(r)  # весь результат уходит в API — не должен падать

    def test_mono_1d_input_gives_numeric_lufs(self):
        sr = 48000
        t = np.arange(10 * sr) / sr
        x = (_db(-23) * np.sin(2 * np.pi * 1000 * t)).astype(np.float64)
        r = self.loudness(x, sr)
        self.assertIsInstance(r["lufs"], (int, float))
        self.assertTrue(np.isfinite(r["lufs"]))

    def test_silence_has_no_lufs_and_very_low_plain_float_peak(self):
        r = self.loudness(np.zeros((10 * 48000, 2)), 48000)
        self.assertNotIn("lufs", r)
        self.assertNotIn("lra", r)
        self.assertIs(type(r["true_peak_db"]), float)
        self.assertLess(r["true_peak_db"], -100)
        json.dumps(r)

    def test_doubling_amplitude_adds_6_db(self):
        sr = 48000
        a = self.loudness(_stereo_sine(_db(-30), 10, sr), sr)["lufs"]
        b = self.loudness(_stereo_sine(2 * _db(-30), 10, sr), sr)["lufs"]
        self.assertAlmostEqual(b - a, 6.0, delta=0.1)


if __name__ == "__main__":
    unittest.main()
