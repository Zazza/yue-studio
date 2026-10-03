"""Тесты сетки ударов dsp.beat_grid(y, sr) -> {"bpm", "offset", "strength"} —
по спецификации, не по реализации: на синтетическом клик-треке с известным
темпом и первой долей сетка совпадает с ним.

Запуск: cd worker && python3 -m unittest test_grid
Без numpy/librosa (CI-джоба только с ruff) класс пропускается, как в test_pure.
"""
import math
import unittest

try:
    import librosa  # noqa: F401
    import numpy as np
    _HAS_DEPS = True
except ImportError:
    _HAS_DEPS = False

SR = 22050


def click_track(bpm, first, sec=30.0, sr=SR, hats=False, seed=0):
    """Удары «бочкой» (затухающий шум + синус 60 Гц) на first + k·60/bpm;
    hats=True — ещё тихие хэты (~30% амплитуды) ровно посередине между ударами."""
    rng = np.random.default_rng(seed)
    n = int(sec * sr)
    y = np.zeros(n, dtype=np.float64)
    period = 60.0 / bpm
    blen = int(0.08 * sr)
    t = np.arange(blen) / sr
    env = np.exp(-t / 0.015)
    kick = env * (0.6 * rng.uniform(-1, 1, blen) + 0.4 * np.sin(2 * np.pi * 60 * t))
    hlen = int(0.03 * sr)
    th = np.arange(hlen) / sr
    hat = 0.3 * np.exp(-th / 0.005) * rng.uniform(-1, 1, hlen)

    def put(burst, at):
        i = int(round(at * sr))
        if 0 <= i < n:
            m = min(len(burst), n - i)
            y[i:i + m] += burst[:m]

    k = 0
    while first + k * period < sec:
        put(kick, first + k * period)
        if hats:
            put(hat, first + (k + 0.5) * period)
        k += 1
    return (0.8 * y / np.max(np.abs(y))).astype(np.float32)


def phase_err(offset, want, period):
    """Расстояние offset до want по модулю периода (по кругу), с."""
    d = (offset - want) % period
    return min(d, period - d)


@unittest.skipUnless(_HAS_DEPS, "нужны numpy/librosa (окружение воркера)")
class TestBeatGrid(unittest.TestCase):
    def setUp(self):
        import dsp
        self.grid = dsp.beat_grid

    def _check(self, r, bpm, first):
        self.assertAlmostEqual(r["bpm"], bpm, delta=0.3, msg=r)
        period = 60.0 / bpm
        self.assertLessEqual(phase_err(r["offset"], first, period), 0.02, msg=r)
        self.assertGreaterEqual(r["offset"], 0.0, msg=r)
        self.assertLess(r["offset"], 60.0 / r["bpm"], msg=r)

    def test_120_bpm_first_beat_0_2(self):
        r = self.grid(click_track(120, 0.2), SR)
        self._check(r, 120, 0.2)
        self.assertGreater(r["strength"], 0.3, msg=r)

    def test_138_bpm_first_beat_0_065(self):
        r = self.grid(click_track(138, 0.065), SR)
        self._check(r, 138, 0.065)
        self.assertGreater(r["strength"], 0.3, msg=r)

    def test_offbeat_hats_do_not_pull_offset(self):
        # хэты на 0.45, 0.95, … — сетка должна остаться на сильных долях 0.2 + k·0.5
        r = self.grid(click_track(120, 0.2, hats=True), SR)
        self._check(r, 120, 0.2)
        self.assertGreater(phase_err(r["offset"], 0.45, 0.5), 0.1,
                           msg=f"offset на слабой доле: {r}")

    def test_silence_no_exception(self):
        r = self.grid(np.zeros(10 * SR, dtype=np.float32), SR)
        self.assertTrue(r["bpm"] == 0 or r["strength"] == 0, msg=r)
        for key in ("bpm", "offset", "strength"):
            self.assertTrue(math.isfinite(r[key]), msg=f"{key}: {r}")

    def test_offset_in_one_period(self):
        # первая доля позже периода (1.3 с при периоде 0.5) — offset всё равно приведён в [0, 60/bpm)
        for bpm, first in ((120, 1.3), (138, 0.0), (100, 0.59)):
            with self.subTest(bpm=bpm, first=first):
                r = self.grid(click_track(bpm, first, seed=bpm), SR)
                self.assertGreater(r["bpm"], 0, msg=r)
                self.assertGreaterEqual(r["offset"], 0.0, msg=r)
                self.assertLess(r["offset"], 60.0 / r["bpm"], msg=r)
                self.assertLessEqual(phase_err(r["offset"], first, 60.0 / bpm), 0.02, msg=r)


if __name__ == "__main__":
    unittest.main()
