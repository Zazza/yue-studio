"""Тесты карточки internal-own-track, этап 7а, условие 50 (тест-кейс ТК85): блок tremolo
движка fx_engine — громкость качается по LFO.

Контракт из карточки:
- параметры rate_hz (0,5…15, умолч. 5), depth (0…1, 0,5), shape (0…1, 0), stereo (0…1, 0);
  описание — worker/fx_blocks.json, вне диапазона — ChainError (API отвечает 422);
- множитель левого канала g(t) = 1 − depth·m(t), m ∈ [0, 1]; при shape 0
  m = (1 + sin 2πft)/2 точно; shape 1 — почти квадрат «вкл-выкл»;
- stereo 1: правый — 1 − depth·(1 − m(t)) (противофаза); на моно stereo — как 0;
- длина = входу, без сдвига (умножение); depth 0 → выход = вход.

Написаны по карточке, без чтения реализации. Внешних границ нет — моков нет.
Запуск: cd worker && python3 -m unittest test_fx_tremolo -v
"""
import json
import unittest
from pathlib import Path

try:
    import numpy as np
    import scipy  # noqa: F401
    _HAS_DEPS = True
except ImportError:
    _HAS_DEPS = False

_SKIP = "нужны numpy/scipy (окружение воркера)"

SR = 48000
CARRIER = 440.0


def _fx():
    import fx_engine
    return fx_engine


def _sine(seconds=4.0, ch=2, amp=0.5):
    """Синус 440 Гц; float32, чтобы «выход = вход» сравнивался без потерь на float32-выходе."""
    t = np.arange(int(SR * seconds)) / SR
    s = (amp * np.sin(2 * np.pi * CARRIER * t)).astype(np.float32)
    return s if ch == 0 else np.repeat(s[:, None], ch, axis=1)


def _trem(**p):
    return [{"type": "tremolo", **p}]


def _win():
    """Окно сглаживания — период несущей в отсчётах, нечётное (центрированное, без сдвига)."""
    return int(round(SR / CARRIER)) | 1


def _gain_env(y, x):
    """Огибающая усиления: |выход| и |вход|, сглаженные по периоду несущей, их отношение.
    Края (полокна) отрезаются."""
    w = _win()
    k = np.ones(w) / w
    ey = np.convolve(np.abs(y.astype(np.float64)), k, mode="same")
    ex = np.convolve(np.abs(x.astype(np.float64)), k, mode="same")
    return (ey / ex)[w:-w]


def _expected_g(n, rate, depth, right=False):
    t = np.arange(n) / SR
    m = (1 + np.sin(2 * np.pi * rate * t)) / 2
    return 1 - depth * ((1 - m) if right else m)


def _corr(a, b):
    return float(np.corrcoef(a, b)[0, 1])


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TremoloSpecTest(unittest.TestCase):
    def test_block_described_in_fx_blocks_json(self):
        blocks = json.loads(Path(__file__).with_name("fx_blocks.json").read_text(encoding="utf-8"))
        self.assertIn("tremolo", sorted(blocks))
        b = blocks["tremolo"]
        for lang in ("ru", "en"):
            self.assertTrue(b["label"][lang].strip())
        params = {p["id"]: p for p in b["params"]}
        want = {"rate_hz": (0.5, 15, 5), "depth": (0, 1, 0.5), "shape": (0, 1, 0), "stereo": (0, 1, 0)}
        self.assertEqual(set(params), set(want))
        for k, (lo, hi, d) in want.items():
            self.assertAlmostEqual(params[k]["min"], lo, msg=k)
            self.assertAlmostEqual(params[k]["max"], hi, msg=k)
            self.assertAlmostEqual(params[k]["default"], d, msg=k)

    def test_defaults_filled(self):
        norm = _fx().parse_chain(_trem())[0]
        self.assertEqual(norm["type"], "tremolo")
        self.assertAlmostEqual(norm["rate_hz"], 5)
        self.assertAlmostEqual(norm["depth"], 0.5)
        self.assertAlmostEqual(norm["shape"], 0)
        self.assertAlmostEqual(norm["stereo"], 0)

    def test_rate_20_is_chain_error(self):
        fx = _fx()
        with self.assertRaises(fx.ChainError):
            fx.parse_chain(_trem(rate_hz=20))
        with self.assertRaises(fx.ChainError):
            fx.process(_sine(0.5), SR, _trem(rate_hz=20))

    def test_out_of_range_params_are_chain_error(self):
        fx = _fx()
        for bad in ({"rate_hz": 0.4}, {"depth": 1.5}, {"depth": -0.1}, {"shape": 1.2},
                    {"stereo": -1}, {"stereo": 2}, {"rate_hz": "fast"}, {"speed": 3}):
            with self.subTest(bad=bad), self.assertRaises(fx.ChainError):
                fx.parse_chain(_trem(**bad))


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TremoloSoundTest(unittest.TestCase):
    def test_tc85_sine_envelope_follows_lfo(self):
        x = _sine()
        y = _fx().process(x, SR, _trem(rate_hz=4, depth=0.6, shape=0, stereo=0))
        env = _gain_env(y[:, 0], x[:, 0])
        w = _win()
        exp = _expected_g(len(x), 4, 0.6)[w:-w]
        self.assertGreaterEqual(_corr(env, exp), 0.99)
        self.assertAlmostEqual(env.min() / env.max(), 0.4, delta=0.03)

    def test_shape0_gain_is_exact_formula_without_shift(self):
        # g(t) = 1 − depth·(1 + sin 2πft)/2 «точно», умножение без сдвига: y = x·g по отсчётам
        x = _sine(2.0)
        y = _fx().process(x, SR, _trem(rate_hz=4, depth=0.6, shape=0, stereo=0))
        g = _expected_g(len(x), 4, 0.6)
        np.testing.assert_allclose(y[:, 0], x[:, 0] * g, atol=1e-3)

    def test_stereo0_left_equals_right(self):
        x = _sine()
        y = _fx().process(x, SR, _trem(rate_hz=4, depth=0.6, shape=0, stereo=0))
        np.testing.assert_array_equal(y[:, 0], y[:, 1])

    def test_depth0_output_equals_input(self):
        x = _sine()
        for shape in (0, 1):
            for stereo in (0, 1):
                with self.subTest(shape=shape, stereo=stereo):
                    y = _fx().process(x, SR, _trem(rate_hz=7, depth=0, shape=shape, stereo=stereo))
                    self.assertEqual(y.shape, x.shape)
                    np.testing.assert_allclose(y, x, rtol=0, atol=1e-9)

    def test_stereo1_channels_in_antiphase_sum_constant(self):
        x = _sine()
        y = _fx().process(x, SR, _trem(rate_hz=4, depth=1, shape=0, stereo=1))
        el = _gain_env(y[:, 0], x[:, 0])
        er = _gain_env(y[:, 1], x[:, 1])
        self.assertLessEqual(_corr(el, er), -0.9)
        s = el + er
        self.assertLessEqual((s.max() - s.min()) / s.mean(), 0.05)

    def test_stereo1_right_is_complement_formula(self):
        x = _sine(2.0)
        y = _fx().process(x, SR, _trem(rate_hz=4, depth=0.6, shape=0, stereo=1))
        np.testing.assert_allclose(y[:, 0], x[:, 0] * _expected_g(len(x), 4, 0.6), atol=1e-3)
        np.testing.assert_allclose(y[:, 1], x[:, 1] * _expected_g(len(x), 4, 0.6, right=True), atol=1e-3)

    def test_shape1_is_almost_square(self):
        # «почти квадрат»: ≥ 70 % огибающей у одного из двух уровней — 1 или 1 − depth (допуск 0,1)
        depth = 0.6
        x = _sine()
        y = _fx().process(x, SR, _trem(rate_hz=4, depth=depth, shape=1, stereo=0))
        env = _gain_env(y[:, 0], x[:, 0])
        near = (np.abs(env - 1) <= 0.1) | (np.abs(env - (1 - depth)) <= 0.1)
        self.assertGreaterEqual(near.mean(), 0.7)
        # и это не константа: оба уровня встречаются
        self.assertGreater((np.abs(env - 1) <= 0.1).mean(), 0.2)
        self.assertGreater((np.abs(env - (1 - depth)) <= 0.1).mean(), 0.2)

    def test_mono_input_stereo1_same_as_stereo0(self):
        fx = _fx()
        for x in (_sine(1.0, ch=0), _sine(1.0, ch=1)):
            with self.subTest(shape=x.shape):
                a = fx.process(x, SR, _trem(rate_hz=4, depth=0.8, shape=0, stereo=1))
                b = fx.process(x, SR, _trem(rate_hz=4, depth=0.8, shape=0, stereo=0))
                self.assertEqual(a.shape, x.shape)
                np.testing.assert_allclose(a, b, atol=1e-6)
                self.assertFalse(np.allclose(a, x, atol=1e-3))

    def test_length_equals_input(self):
        fx = _fx()
        for n in (1, 777, SR + 13):
            x = np.random.default_rng(n).standard_normal((n, 2)).astype(np.float32) * 0.1
            for p in ({}, {"shape": 1, "stereo": 1, "depth": 1, "rate_hz": 15}):
                with self.subTest(n=n, p=p):
                    y = fx.process(x, SR, _trem(**p))
                    self.assertEqual(y.shape, x.shape)
                    self.assertTrue(np.all(np.isfinite(y)))



# ---------- Кросс-ревью s7a r1: условие 50а, ТК85а — фаза LFO от времени в треке ----------
# Контракт: LFO считается от t = _t0 + отсчёт/sr; _t0 (начало куска в треке, с) ставит только
# воркер: превью окна [from, to) — _t0 = from, полный рендер — 0; присланный в запросе _t0
# выбрасывается. Служебный _t0 разрешён только tremolo и perc, у прочих блоков — ChainError.

T0, T1 = 20.1, 21.1
LONG = 30.0
TREM_FULL = {"rate_hz": 5, "depth": 1, "stereo": 1}


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TremoloPhaseFromTrackTimeTest(unittest.TestCase):
    """ТК85а (движок): кусок с _t0 = тот же отрезок обработки целого сигнала."""

    def test_tc85a_chunk_with_t0_equals_slice_of_full(self):
        fx = _fx()
        x = _sine(LONG)
        a, b = int(round(T0 * SR)), int(round(T1 * SR))
        full = fx.process(x, SR, _trem(**TREM_FULL))
        chunk = fx.process(x[a:b].copy(), SR, _trem(**TREM_FULL, _t0=T0))
        self.assertEqual(chunk.shape, (b - a, 2))
        np.testing.assert_allclose(chunk, full[a:b], rtol=0, atol=1e-6)

    def test_tc85a_without_t0_chunk_starts_from_zero_phase(self):
        # контроль осмысленности: без _t0 фаза куска — от нуля, и он с отрезком не совпадает
        fx = _fx()
        x = _sine(LONG)
        a, b = int(round(T0 * SR)), int(round(T1 * SR))
        full = fx.process(x, SR, _trem(**TREM_FULL))
        chunk0 = fx.process(x[a:b].copy(), SR, _trem(**TREM_FULL))
        explicit0 = fx.process(x[a:b].copy(), SR, _trem(**TREM_FULL, _t0=0))
        np.testing.assert_allclose(explicit0, chunk0, rtol=0, atol=1e-9)
        self.assertGreater(float(np.abs(chunk0 - full[a:b]).max()), 1e-2)

    def test_t0_allowed_for_perc(self):
        fx = _fx()
        y = fx.process(np.zeros((SR, 2), dtype=np.float32), SR,
                       [{"type": "perc", "notes": [{"t": 0.2, "d": 0.2, "vel": 1.0}],
                         "humanize_ms": 0, "_t0": 3.0}])
        self.assertEqual(y.shape, (SR, 2))

    def test_t0_on_other_blocks_is_chain_error(self):
        fx = _fx()
        x = _sine(0.5)
        for blk in ({"type": "gain", "_t0": 1.0}, {"type": "delay", "wet": 0, "_t0": 1.0},
                    {"type": "eq", "bands": [], "_t0": 0}):
            with self.subTest(blk=blk):
                with self.assertRaises(fx.ChainError):
                    fx.parse_chain([blk])
                with self.assertRaises(fx.ChainError):
                    fx.process(x, SR, [blk])


try:
    import test_fx_api as _fa
    _API_OK, _API_SKIP = _fa._OK, _fa._SKIP
    _ApiBase = _fa._FxApiCase
except Exception as e:  # окружение без fastapi/httpx — пропуск, как у test_fx_api
    _API_OK, _API_SKIP = False, f"нет окружения воркера для API: {e}"
    _ApiBase = unittest.TestCase


@unittest.skipUnless(_API_OK, _API_SKIP)
class TremoloPreviewMatchesRenderTest(_ApiBase):
    """ТК85а (воркер): превью окна [20,1; 21,1) (source mix, output mix) = тот же отрезок
    полного /fx-рендера ±1e-4; присланный в запросе _t0 игнорируется."""

    def _long_job(self):
        import soundfile as sf
        sr = _fa.SR
        jid = self._job(duration=LONG, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        t = np.arange(int(LONG * sr)) / sr
        left = 0.2 * np.sin(2 * np.pi * 440 * t)
        right = 0.2 * np.sin(2 * np.pi * 660 * t)
        sf.write(str(d / "audio.flac"), np.stack([left, right], axis=1).astype(np.float32), sr)
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid, d

    def _preview(self, jid, chain):
        r = self._fx(jid, source="mix", output="mix", chain=chain, preview=True,
                     **{"from": T0, "to": T1})
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def _render_slice(self, jid, d):
        out = self._ok(jid, source="mix", output="mix", chain=_trem(**TREM_FULL))
        y, sr = _fa._read(d / out["file"])
        a, b = int(round(T0 * sr)), int(round(T1 * sr))
        return y[a:b]

    def test_tc85a_preview_window_equals_full_render_slice(self):
        jid, d = self._long_job()
        want = self._render_slice(jid, d)
        out = self._preview(jid, _trem(**TREM_FULL))
        got, _ = _fa._read(d / out["file"])
        self.assertEqual(got.shape, want.shape)
        np.testing.assert_allclose(got, want, rtol=0, atol=1e-4)

    def test_tc85a_t0_from_request_is_ignored(self):
        jid, d = self._long_job()
        want = self._render_slice(jid, d)
        base, _ = _fa._read(d / self._preview(jid, _trem(**TREM_FULL))["file"])
        for t0 in (0, 5.0, 123.4):
            with self.subTest(t0=t0):
                out = self._preview(jid, _trem(**TREM_FULL, _t0=t0))
                got, _ = _fa._read(d / out["file"])
                self.assertEqual(got.shape, base.shape)
                np.testing.assert_allclose(got, base, rtol=0, atol=1e-6)
                np.testing.assert_allclose(got, want, rtol=0, atol=1e-4)

    def test_tc85a_t0_in_full_render_request_is_ignored(self):
        # полный рендер — _t0 = 0, присланный в запросе не сдвигает фазу
        jid, d = self._long_job()
        plain = self._ok(jid, source="mix", output="mix", chain=_trem(**TREM_FULL))
        y0, _ = _fa._read(d / plain["file"])
        r = self._fx(jid, source="mix", output="mix", chain=_trem(**TREM_FULL, _t0=7.3))
        self.assertEqual(r.status_code, 200, r.text)
        y1, _ = _fa._read(d / r.json()["file"])
        np.testing.assert_allclose(y1, y0, rtol=0, atol=1e-6)


if __name__ == "__main__":
    unittest.main()
