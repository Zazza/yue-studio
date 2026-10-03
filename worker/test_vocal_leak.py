"""Тесты dsp.vocal_activity — по спецификации, не по реализации:
начала участков, где уровень кадров 0.1 с ≥ thresh_db dBFS; короче min_len —
выброс; паузы короче gap склеиваются; моно или (кадры, каналы).

Запуск: cd worker && python3 -m unittest test_vocal_leak
Без numpy (CI-джоба только с ruff) класс пропускается, как в test_pure.
"""
import unittest

try:
    import numpy as np
    _HAS_DEPS = True
except ImportError:
    _HAS_DEPS = False

SR = 44100


def _db(db):
    return 10 ** (db / 20)


def _track(sec=40.0, sr=SR, seed=0):
    """Почти тишина: белый шум на −80 dBFS (RMS)."""
    rng = np.random.default_rng(seed)
    return (rng.standard_normal(int(sec * sr)) * _db(-80)).astype(np.float32)


def _add_tone(x, start, dur, rms_db, sr=SR, freq=440.0):
    """Синус с RMS = rms_db dBFS (амплитуда = RMS·√2) на [start, start+dur)."""
    i0 = int(round(start * sr))
    n = int(round(dur * sr))
    t = np.arange(n) / sr
    x[i0:i0 + n] += (_db(rms_db) * np.sqrt(2) * np.sin(2 * np.pi * freq * t)).astype(x.dtype)
    return x


@unittest.skipUnless(_HAS_DEPS, "нужен numpy (окружение воркера)")
class TestVocalActivity(unittest.TestCase):
    def setUp(self):
        import dsp
        self.va = dsp.vocal_activity

    def test_digital_silence_empty(self):
        self.assertEqual(self.va(np.zeros(SR * 10, dtype=np.float32), SR), [])

    def test_near_silent_track_empty(self):
        self.assertEqual(self.va(_track(), SR), [])

    def test_single_tone_start(self):
        x = _add_tone(_track(), 25.4, 0.5, -20)
        got = self.va(x, SR)
        self.assertEqual(len(got), 1, got)
        self.assertAlmostEqual(got[0], 25.4, delta=0.1 + 1e-9)

    def test_starts_rounded_to_tenth(self):
        x = _add_tone(_track(), 25.4, 0.5, -20)
        for s in self.va(x, SR):
            self.assertAlmostEqual(s, round(s, 1), places=9)

    def test_stereo_input(self):
        mono = _add_tone(_track(), 25.4, 0.5, -20)
        x = np.stack([mono, mono], axis=1)
        got = self.va(x, SR)
        self.assertEqual(len(got), 1, got)
        self.assertAlmostEqual(got[0], 25.4, delta=0.1 + 1e-9)

    def test_two_separate_tones(self):
        x = _add_tone(_track(), 5.0, 0.5, -20)
        x = _add_tone(x, 20.0, 0.5, -20)
        got = self.va(x, SR)
        self.assertEqual(len(got), 2, got)
        self.assertAlmostEqual(got[0], 5.0, delta=0.1 + 1e-9)
        self.assertAlmostEqual(got[1], 20.0, delta=0.1 + 1e-9)

    def test_short_gap_merged_into_one_span(self):
        # 10.0–10.5, пауза 0.5 с (< gap=1 с), 11.0–11.5 → один участок от 10.0
        x = _add_tone(_track(), 10.0, 0.5, -20)
        x = _add_tone(x, 11.0, 0.5, -20)
        got = self.va(x, SR)
        self.assertEqual(len(got), 1, got)
        self.assertAlmostEqual(got[0], 10.0, delta=0.1 + 1e-9)

    def test_short_blip_dropped(self):
        x = _add_tone(_track(), 12.0, 0.15, -20)
        self.assertEqual(self.va(x, SR), [])

    def test_tone_below_threshold_ignored(self):
        x = _add_tone(_track(), 25.4, 0.5, -40)
        self.assertEqual(self.va(x, SR), [])

    def test_threshold_parameter_respected(self):
        # тот же −40 dBFS тон проходит при пороге −50
        x = _add_tone(_track(), 25.4, 0.5, -40)
        got = self.va(x, SR, thresh_db=-50)
        self.assertEqual(len(got), 1, got)
        self.assertAlmostEqual(got[0], 25.4, delta=0.1 + 1e-9)


if __name__ == "__main__":
    unittest.main()
