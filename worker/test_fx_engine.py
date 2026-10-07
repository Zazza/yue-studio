"""Тесты карточки internal-sound-engine: звуковой движок воркера (fx_engine,
fx_nam.measure_latency) — тест-кейсы 1–10 карточки.

Контракт (из карточки и контракта задачи):
- цепочка — список блоков gate|eq|comp|drive|amp|cab|reverb|delay|gain с параметрами,
  parse_chain проверяет и заполняет умолчания, ошибка — ChainError(ValueError);
- process(audio, sr, chain, resources) — выход той же формы/длины, float32,
  сдвиг 0: импульс на входе → пик |выхода| на том же сэмпле (каждый блок и вся цепочка);
- amp — модель NAM из resources.amp(name): вызываемый объект 1-D → 1-D, атрибуты
  sr и latency; движок передискретизирует в sr модели и обратно и компенсирует latency;
- cab — IR из resources.ir(name) (пик IR выравнивается в 0, частота IR пересчитывается);
  ir "" — «лёгкий кабинет» (срез верха на cutoff_hz);
- reverb/delay — сухой путь без изменения, хвост после; wet=0 → выход = вход;
- fx_nam.measure_latency(fn, sr) — чистый numpy: щелчок через fn, позиция пика
  |выхода| минус позиция щелчка.

Внешняя граница — модель NAM (torch): вместо неё фейк FakeAmp с известной задержкой.
torch, GPU и сеть не нужны.

Запуск: cd worker && python3 -m unittest test_fx_engine -v
Без numpy/scipy классы пропускаются (как соседние тесты); импорт fx_engine/fx_nam
делается внутри тестов — их отсутствие роняет тесты, а не пропускает.
"""
import json
import unittest

try:
    import numpy as np
    import scipy  # noqa: F401
    _HAS_DEPS = True
except ImportError:
    _HAS_DEPS = False

_SKIP = "нужны numpy/scipy (окружение воркера)"

SR = 48000
BLOCKS = ("gate", "eq", "comp", "drive", "amp", "cab", "reverb", "delay", "gain")


def _fx():
    import fx_engine
    return fx_engine


# ---------- фейки внешних границ ----------

class FakeAmp:
    """Фейк модели NAM: сдвигает сигнал на shift сэмплов (отрицательный — вперёд),
    слегка перегружает, атрибуты sr/latency — как у AmpModel по контракту.
    Пишет, с какими длинами её вызывали и была ли она внутри GPU-очереди."""

    def __init__(self, shift=5, sr=48000, latency=None):
        self.shift = shift
        self.sr = sr
        self.latency = shift if latency is None else latency
        self.calls = []

    def __call__(self, x):
        assert x.ndim == 1, f"модель получает 1-D, пришло {x.shape}"
        self.calls.append(len(x))
        y = np.zeros_like(x, dtype=np.float32)
        s = self.shift
        if s > 0:
            y[s:] = x[:-s]
        elif s < 0:
            y[:s] = x[-s:]
        else:
            y[:] = x
        return (0.8 * np.tanh(1.2 * y)).astype(np.float32)


class FakeResources:
    """resources по контракту: ir(name) → (ir, sr), amp(name) → модель; нет — KeyError."""

    def __init__(self, amps=None, irs=None):
        self.amps = dict(amps or {})
        self.irs = dict(irs or {})

    def amp(self, name):
        return self.amps[name]

    def ir(self, name):
        return self.irs[name]


def _res(**kw):
    amps = {"fake": FakeAmp(shift=5)}
    amps.update(kw.pop("amps", {}))
    return FakeResources(amps=amps, **kw)


# ---------- сигналы и замеры ----------

def _impulse(n=2 * SR, pos=30011, ch=None):
    x = np.zeros(n if ch is None else (n, ch), dtype=np.float32)
    x[pos] = 1.0
    return x, pos


def _peak(y):
    a = np.abs(np.asarray(y, dtype=np.float64))
    if a.ndim == 2:
        a = a.max(axis=1)
    return int(np.argmax(a))


def _sine(hz, amp=0.5, dur=3.0, sr=SR):
    t = np.arange(int(dur * sr)) / sr
    return (amp * np.sin(2 * np.pi * hz * t)).astype(np.float32)


def _tone_amp(x, hz, sr=SR, t0=1.0, t1=2.0):
    """Амплитуда синуса hz на отрезке [t0, t1) (одна точка ДПФ; отрезок —
    целое число периодов)."""
    x = np.asarray(x, dtype=np.float64)
    if x.ndim > 1:
        x = x.mean(axis=1)
    a, b = int(t0 * sr), int(t1 * sr)
    seg = x[a:b]
    t = np.arange(len(seg)) / sr
    return 2 * abs(np.sum(seg * np.exp(-2j * np.pi * hz * t))) / len(seg)


def _db(r):
    return 20 * np.log10(max(r, 1e-12))


def _block(t, **params):
    return {"type": t, **params}


def _default_block(t):
    # у amp нет умолчания модели — обязательна, берём фейк
    return _block("amp", model="fake") if t == "amp" else _block(t)


# ---------- ТК1, ТК2: без задержки ----------

@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestNoLatency(unittest.TestCase):
    """Условие 1: длина та же, импульс → пик на том же сэмпле."""

    def _check(self, chain, x, pos, sr=SR, res=None):
        out = _fx().process(x, sr, chain, res if res is not None else _res())
        self.assertEqual(out.shape, x.shape)
        self.assertEqual(out.dtype, np.float32)
        self.assertTrue(np.all(np.isfinite(out)))
        self.assertEqual(_peak(out), pos)
        return out

    # ТК1
    def test_tc1_each_block_default_mono(self):
        for t in BLOCKS:
            with self.subTest(block=t):
                x, pos = _impulse()
                self._check([_default_block(t)], x, pos)

    def test_tc1_each_block_default_stereo(self):
        for t in BLOCKS:
            with self.subTest(block=t):
                x, pos = _impulse(ch=2)
                self._check([_default_block(t)], x, pos)

    def test_tc1_each_block_odd_length(self):
        # нечётная длина и импульс у края — длина не теряется на передискретизации/фильтрах
        for t in BLOCKS:
            with self.subTest(block=t):
                x, pos = _impulse(n=SR + 7, pos=1234)
                self._check([_default_block(t)], x, pos)

    def test_tc1_eq_with_filters_and_band_zero_phase(self):
        x, pos = _impulse()
        chain = [_block("eq", highpass_hz=100, lowpass_hz=8000,
                        bands=[{"freq_hz": 1000, "gain_db": 6, "q": 1}])]
        self._check(chain, x, pos)

    # ТК2
    def test_tc2_chain_of_all_blocks_fake_amp_latency_5(self):
        x, pos = _impulse()
        chain = [_default_block(t) for t in BLOCKS]
        self.assertEqual([b["type"] for b in chain], list(BLOCKS))
        self._check(chain, x, pos)

    def test_tc2_chain_of_all_blocks_stereo(self):
        x, pos = _impulse(ch=2)
        self._check([_default_block(t) for t in BLOCKS], x, pos)

    def test_tc2_amp_at_other_sample_rate(self):
        # условие 9: дорожка 44,1 кГц, модель 48 кГц — передискретизация без сдвига
        amp = FakeAmp(shift=5, sr=48000)
        sr = 44100
        x, pos = _impulse(n=2 * sr, pos=20011)
        self._check([_block("amp", model="m")], x, pos, sr=sr, res=FakeResources(amps={"m": amp}))
        # модель получила сигнал на своей частоте (длина ≈ n·48000/44100)
        self.assertTrue(amp.calls)
        want = 2 * sr * 48000 / sr
        for n in amp.calls:
            self.assertLess(abs(n - want), 64, f"длина на входе модели {n}, ждали ≈{want:.0f}")

    def test_tc2_amp_channels_one_by_one(self):
        # контракт: каналы — по отдельности (модель получает 1-D)
        amp = FakeAmp(shift=5)
        x, pos = _impulse(ch=2)
        self._check([_block("amp", model="m")], x, pos, res=FakeResources(amps={"m": amp}))
        self.assertEqual(len(amp.calls), 2)


# ---------- ТК3: компенсация задержки NAM ----------

def _shift_fn(n, gain=-0.5):
    """Функция 1-D → 1-D, задерживающая на n сэмплов (отрицательный — вперёд);
    знак минус — пик ищется по модулю."""
    def fn(x):
        x = np.asarray(x, dtype=np.float32)
        y = np.zeros_like(x)
        if n > 0:
            y[n:] = x[:-n]
        elif n < 0:
            y[:n] = x[-n:]
        else:
            y[:] = x
        return gain * y
    return fn


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestNamLatency(unittest.TestCase):

    def test_tc3_fx_nam_imports_without_torch(self):
        # контракт: measure_latency — чистый numpy; модуль импортируется без torch
        import fx_nam
        self.assertTrue(callable(fx_nam.measure_latency))

    def test_tc3_measure_latency(self):
        import fx_nam
        for n in (-2, 0, 8):
            with self.subTest(n=n):
                self.assertEqual(fx_nam.measure_latency(_shift_fn(n), SR), n)

    def test_tc3_measure_latency_other_sr(self):
        import fx_nam
        self.assertEqual(fx_nam.measure_latency(_shift_fn(8, gain=0.3), 44100), 8)

    def test_tc3_engine_compensates_latency(self):
        for n in (-2, 0, 8):
            with self.subTest(n=n):
                x, pos = _impulse()
                res = FakeResources(amps={"m": FakeAmp(shift=n)})
                out = _fx().process(x, SR, [_block("amp", model="m")], res)
                self.assertEqual(out.shape, x.shape)
                self.assertEqual(_peak(out), pos)

    def test_tc3_impulse_at_tail_not_lost(self):
        # регрессия кросс-ревью: задержка модели 8 сэмплов, импульс в последних
        # сэмплах короткого входа — компенсация не теряет хвост
        n = 2000
        for pos in (1999, 1995):
            with self.subTest(pos=pos):
                x, _ = _impulse(n=n, pos=pos)
                res = FakeResources(amps={"m": FakeAmp(shift=8, sr=SR)})
                out = _fx().process(x, SR, [_block("amp", model="m")], res)
                self.assertEqual(out.shape, x.shape)
                self.assertGreater(float(np.max(np.abs(out))), 0.1, "выход нулевой")
                self.assertEqual(_peak(out), pos)

    def test_tc3_measured_latency_then_compensated(self):
        # связка как у воркера: замер на фейке → latency модели → сдвиг 0
        import fx_nam
        amp = FakeAmp(shift=8)
        amp.latency = fx_nam.measure_latency(amp, amp.sr)
        x, pos = _impulse()
        out = _fx().process(x, SR, [_block("amp", model="m")], FakeResources(amps={"m": amp}))
        self.assertEqual(_peak(out), pos)


# ---------- ТК4: EQ ----------

@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestEq(unittest.TestCase):

    def _gain_db(self, chain, hz):
        x = _sine(hz)
        y = _fx().process(x, SR, chain)
        return _db(_tone_amp(y, hz) / _tone_amp(x, hz))

    def test_tc4_highpass_100_kills_30hz(self):
        self.assertLessEqual(self._gain_db([_block("eq", highpass_hz=100)], 30), -20)

    def test_tc4_highpass_100_passes_1k(self):
        self.assertAlmostEqual(self._gain_db([_block("eq", highpass_hz=100)], 1000), 0, delta=1)

    def test_tc4_band_plus6_at_1k(self):
        chain = [_block("eq", bands=[{"freq_hz": 1000, "gain_db": 6, "q": 1}])]
        self.assertAlmostEqual(self._gain_db(chain, 1000), 6, delta=1)

    def test_eq_defaults_are_off(self):
        # контракт: highpass 0 / lowpass 0 — выкл, полос нет → вход не меняется
        x = _sine(440)
        y = _fx().process(x, SR, [_block("eq")])
        np.testing.assert_allclose(y, x, atol=1e-4)


# ---------- ТК5: gate ----------

@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestGate(unittest.TestCase):

    def test_tc5_noise_below_threshold_suppressed(self):
        rng = np.random.default_rng(1)
        x = np.zeros(2 * SR, dtype=np.float32)
        # тишина, затем шум с пиком −60 дБFS — ниже порога −50
        x[SR // 2:] = rng.uniform(-0.001, 0.001, size=len(x) - SR // 2).astype(np.float32)
        y = _fx().process(x, SR, [_block("gate", threshold_db=-50, range_db=-40)])
        seg = slice(SR, 2 * SR)
        rin = np.sqrt(np.mean(x[seg].astype(np.float64) ** 2))
        rout = np.sqrt(np.mean(y[seg].astype(np.float64) ** 2))
        self.assertLessEqual(_db(rout / rin), -30, "шум ниже порога не подавлен")

    def test_tc5_signal_above_threshold_keeps_attack(self):
        x = np.zeros(2 * SR, dtype=np.float32)
        onset = SR // 2
        a = 0.5  # −6 дБFS, выше порога
        burst = _sine(1000, amp=a, dur=0.5)
        x[onset:onset + len(burst)] = burst
        y = _fx().process(x, SR, [_block("gate", threshold_db=-50, range_db=-40, attack_ms=1)])
        sl = slice(onset, onset + len(burst))
        # первая 10 мс атаки и всё тело — без изменения формы
        attack = slice(onset, onset + SR // 100)
        self.assertLessEqual(float(np.max(np.abs(y[attack] - x[attack]))), 0.05 * a,
                             "атака сигнала выше порога искажена")
        self.assertLessEqual(float(np.max(np.abs(y[sl] - x[sl]))), 0.05 * a)


# ---------- ТК6: comp ----------

@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestComp(unittest.TestCase):

    def _out_peak_db(self, in_db, **params):
        a = 10 ** (in_db / 20)
        x = _sine(1000, amp=a, dur=2.0)
        y = _fx().process(x, SR, [_block("comp", **params)])
        steady = np.abs(y[SR:].astype(np.float64))  # установившийся режим — вторая секунда
        return _db(float(steady.max()))

    def test_tc6_formula_threshold_minus20_ratio4(self):
        # T + (A − T)/ratio = −20 + (−6 + 20)/4 = −16,5 дБ
        got = self._out_peak_db(-6, threshold_db=-20, ratio=4)
        self.assertAlmostEqual(got, -16.5, delta=1.5)

    def test_tc6_makeup_added(self):
        got = self._out_peak_db(-6, threshold_db=-20, ratio=4, makeup_db=6)
        self.assertAlmostEqual(got, -10.5, delta=1.5)

    def test_tc6_below_threshold_untouched(self):
        got = self._out_peak_db(-30, threshold_db=-20, ratio=4)
        self.assertAlmostEqual(got, -30, delta=1.0)


# ---------- ТК7: drive ----------

def _thd(y, f0, sr=SR, harmonics=20):
    a1 = _tone_amp(y, f0, sr)
    hs = [_tone_amp(y, f0 * k, sr) for k in range(2, harmonics + 1) if f0 * k < sr / 2]
    return float(np.sqrt(np.sum(np.square(hs))) / a1)


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestDrive(unittest.TestCase):

    def _thd_at(self, gain_db):
        x = _sine(480, amp=0.5)
        y = _fx().process(x, SR, [_block("drive", gain_db=gain_db, mix=1)])
        return _thd(y, 480)

    def test_tc7_harmonics_grow_with_gain(self):
        t6, t20, t40 = self._thd_at(6), self._thd_at(20), self._thd_at(40)
        self.assertLess(t6, t20)
        self.assertLess(t20, t40)

    def test_tc7_oversampling_keeps_distortion(self):
        self.assertGreaterEqual(self._thd_at(20), 0.10, "перегруз при gain 20 дБ «съеден»")


# ---------- ТК8: cab ----------

def _lowpass_ir(cut_hz, sr, n=257, delay=None):
    """FIR низких частот (окно Хэмминга) с пиком в середине — не в нуле."""
    m = np.arange(n) - (n - 1) / 2
    h = np.sinc(2 * cut_hz / sr * m) * np.hamming(n)
    h /= h.sum()
    if delay:
        h = np.concatenate([np.zeros(delay), h])
    return h.astype(np.float32)


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestCab(unittest.TestCase):

    def test_tc8_ir_peak_not_at_zero_is_aligned(self):
        ir = np.zeros(512, dtype=np.float32)
        ir[40], ir[41], ir[90] = 1.0, 0.5, -0.3
        x, pos = _impulse()
        out = _fx().process(x, SR, [_block("cab", ir="box")], FakeResources(irs={"box": (ir, SR)}))
        self.assertEqual(out.shape, x.shape)
        self.assertEqual(_peak(out), pos)

    def test_tc8_ir_other_rate_aligned(self):
        ir = _lowpass_ir(4000, 96000, delay=30)
        x, pos = _impulse()
        out = _fx().process(x, SR, [_block("cab", ir="hi")], FakeResources(irs={"hi": (ir, 96000)}))
        self.assertEqual(out.shape, x.shape)
        self.assertEqual(_peak(out), pos)

    def test_tc8_ir_other_rate_is_resampled(self):
        # IR — срез на 4 кГц при 96 кГц. Пересчитан → 3 кГц проходит, 6 кГц режется;
        # если сэмплы взять как 48 кГц, срез уедет на 2 кГц и 3 кГц срежется.
        ir = _lowpass_ir(4000, 96000)
        res = FakeResources(irs={"hi": (ir, 96000)})
        chain = [_block("cab", ir="hi")]
        g = {}
        for hz in (500, 3000, 6000):
            x = _sine(hz)
            g[hz] = _tone_amp(_fx().process(x, SR, chain, res), hz) / _tone_amp(x, hz)
        self.assertGreater(_db(g[3000] / g[500]), -3, f"3 кГц срезан: IR не пересчитан ({g})")
        self.assertLess(_db(g[6000] / g[500]), -20, f"6 кГц не срезан ({g})")

    def test_cab_stereo_ir_peaks_on_different_samples(self):
        # регрессия кросс-ревью: пики каналов IR на разных сэмплах — каждый канал
        # выхода с пиком на месте импульса (выравнивание не по одному каналу)
        ir = np.zeros((256, 2), dtype=np.float32)
        ir[10, 0], ir[40, 0] = 1.0, 0.75
        ir[20, 1], ir[40, 1] = 1.0, 0.75
        x, pos = _impulse(ch=2)
        out = _fx().process(x, SR, [_block("cab", ir="st")], FakeResources(irs={"st": (ir, SR)}))
        self.assertEqual(out.shape, x.shape)
        for c in range(2):
            with self.subTest(channel=c):
                self.assertEqual(_peak(out[:, c]), pos)

    def test_cab_builtin_light_cuts_top(self):
        # ir "" — «лёгкий кабинет»: срез верха на cutoff_hz, ресурсы не нужны
        chain = [_block("cab", cutoff_hz=4000)]
        lo = _sine(500)
        hi = _sine(12000)
        g_lo = _tone_amp(_fx().process(lo, SR, chain, None), 500) / _tone_amp(lo, 500)
        g_hi = _tone_amp(_fx().process(hi, SR, chain, None), 12000) / _tone_amp(hi, 12000)
        self.assertAlmostEqual(_db(g_lo), 0, delta=1)
        self.assertLess(_db(g_hi), -6)

    def test_cab_missing_ir_is_chain_error(self):
        x, _ = _impulse()
        fx = _fx()
        with self.assertRaises(fx.ChainError):
            fx.process(x, SR, [_block("cab", ir="nope")], FakeResources())
        with self.assertRaises(fx.ChainError):
            fx.process(x, SR, [_block("cab", ir="nope")], None)


# ---------- ТК9: reverb / delay ----------

@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestReverbDelay(unittest.TestCase):

    def _run(self, block):
        x, pos = _impulse(n=3 * SR, pos=SR // 2)
        return x, pos, _fx().process(x, SR, [block])

    def _dry_in_place(self, block):
        x, pos, out = self._run(block)
        self.assertEqual(out.shape, x.shape)
        self.assertEqual(_peak(out), pos)
        self.assertAlmostEqual(float(out[pos]), 1.0, delta=0.02, msg="сухой путь изменён")
        self.assertLessEqual(float(np.max(np.abs(out[:pos]))), 1e-4, "хвост раньше сигнала")
        return pos, out

    def test_tc9_reverb_dry_in_place_tail_after(self):
        pos, out = self._dry_in_place(_block("reverb"))
        early = out[pos + SR // 50: pos + SR // 2].astype(np.float64)   # 20–500 мс
        late = out[pos + SR: pos + 3 * SR // 2].astype(np.float64)      # 1,0–1,5 с
        self.assertGreater(np.sqrt(np.mean(early ** 2)), 1e-4, "нет хвоста")
        self.assertGreater(np.sum(early ** 2), np.sum(late ** 2), "хвост не затухает")

    def test_tc9_delay_dry_in_place_echo_after(self):
        pos, out = self._dry_in_place(_block("delay", time_ms=375))
        a = np.abs(out.astype(np.float64))
        lo = pos + SR // 200  # после 5 мс
        echo = lo + int(np.argmax(a[lo: pos + SR // 2]))
        self.assertLessEqual(abs(echo - (pos + int(0.375 * SR))), SR // 1000,
                             f"повтор на {(echo - pos) / SR * 1000:.1f} мс, ждали 375")
        self.assertGreater(a[echo], 1e-3)

    def test_tc9_wet_zero_is_identity(self):
        rng = np.random.default_rng(2)
        x = (0.3 * rng.standard_normal((SR, 2))).astype(np.float32)
        for t in ("reverb", "delay"):
            with self.subTest(block=t):
                y = _fx().process(x, SR, [_block(t, wet=0)])
                np.testing.assert_allclose(y, x, atol=1e-6)


# ---------- Условие 12: блок «громкость» (gain) ----------

def _rms(y, t0=0.5, t1=2.5, sr=SR):
    y = np.asarray(y, dtype=np.float64)
    return float(np.sqrt(np.mean(np.square(y[int(t0 * sr):int(t1 * sr)]))))


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestGain(unittest.TestCase):
    """gain_db −24…+24, по умолчанию 0; без сдвига (умножение), длина та же."""

    def _noise(self, ch=None, amp=0.05):
        rng = np.random.default_rng(12)
        shape = (2 * SR,) if ch is None else (2 * SR, ch)
        return (amp * rng.standard_normal(shape)).astype(np.float32)

    def test_default_0db_is_identity(self):
        for ch in (None, 2):
            with self.subTest(ch=ch):
                x = self._noise(ch)
                y = _fx().process(x, SR, [_block("gain")], None)
                self.assertEqual(y.shape, x.shape)
                np.testing.assert_allclose(y, x, atol=1e-6)

    def test_explicit_0db_is_identity(self):
        x = self._noise()
        y = _fx().process(x, SR, [_block("gain", gain_db=0)], None)
        np.testing.assert_allclose(y, x, atol=1e-6)

    def test_plus6db_doubles(self):
        x = _sine(1000, amp=0.2)
        y = _fx().process(x, SR, [_block("gain", gain_db=6)], None)
        self.assertAlmostEqual(_db(_tone_amp(y, 1000) / _tone_amp(x, 1000)), 6, delta=0.05)
        # ×2 по сэмплам (6 дБ ≈ ×1,995)
        self.assertAlmostEqual(_db(_rms(y) / _rms(x)), 6, delta=0.05)

    def test_bounds_minus24_plus24(self):
        x = _sine(1000, amp=0.01)
        for g in (-24, 24):
            with self.subTest(gain_db=g):
                self.assertEqual(_fx().parse_chain([_block("gain", gain_db=g)])[0]["gain_db"], g)
                y = _fx().process(x, SR, [_block("gain", gain_db=g)], None)
                self.assertAlmostEqual(_db(_rms(y) / _rms(x)), g, delta=0.05)

    def test_out_of_range_is_chain_error(self):
        fx = _fx()
        for g in (-24.5, 24.5, -100, 100):
            with self.subTest(gain_db=g):
                with self.assertRaises(fx.ChainError):
                    fx.parse_chain([_block("gain", gain_db=g)])
                with self.assertRaises(fx.ChainError):
                    fx.process(_sine(1000, dur=0.1), SR, [_block("gain", gain_db=g)], None)

    def test_no_shift_same_length(self):
        for ch in (None, 2):
            for g in (-24, -6, 6, 24):
                with self.subTest(ch=ch, gain_db=g):
                    x, pos = _impulse(n=SR + 7, pos=1234, ch=ch)
                    x *= 0.01
                    y = _fx().process(x, SR, [_block("gain", gain_db=g)], None)
                    self.assertEqual(y.shape, x.shape)
                    self.assertEqual(y.dtype, np.float32)
                    self.assertEqual(_peak(y), pos)

    def test_after_narrow_eq_and_drive_raises_by_gain(self):
        # опыт #617: после узкой полосы и перегруза звук тихий — gain поднимает на заданные дБ
        x = self._noise(amp=0.1)
        base = [_block("eq", highpass_hz=800, lowpass_hz=1500), _block("drive")]
        quiet = _fx().process(x, SR, base, None)
        for g in (6, 12):
            with self.subTest(gain_db=g):
                loud = _fx().process(x, SR, base + [_block("gain", gain_db=g)], None)
                self.assertEqual(loud.shape, x.shape)
                self.assertAlmostEqual(_db(_rms(loud) / _rms(quiet)), g, delta=0.05)


# ---------- ТК10: проверка цепочки ----------

DEFAULTS = {
    "gate": {"threshold_db": -50, "range_db": -40, "attack_ms": 1, "release_ms": 100},
    "eq": {"highpass_hz": 0, "lowpass_hz": 0, "bands": []},
    "comp": {"threshold_db": -20, "ratio": 4, "attack_ms": 10, "release_ms": 100, "makeup_db": 0},
    "drive": {"gain_db": 12, "mix": 1, "output_db": 0},
    "amp": {"model": "fake", "input_db": 0, "output_db": 0},
    "cab": {"ir": "", "cutoff_hz": 7000, "mix": 1},
    "reverb": {"ir": "", "decay_s": 1.5, "predelay_ms": 10, "lowpass_hz": 8000, "wet": 0.3},
    "delay": {"time_ms": 375, "feedback": 0.35, "lowpass_hz": 6000, "wet": 0.3},
    "gain": {"gain_db": 0},
}


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestParseChain(unittest.TestCase):

    def _bad(self, chain):
        fx = _fx()
        with self.assertRaises(fx.ChainError) as cm:
            fx.parse_chain(chain)
        self.assertTrue(str(cm.exception).strip(), "у ошибки нет причины")
        return str(cm.exception)

    def test_chain_error_is_value_error(self):
        self.assertTrue(issubclass(_fx().ChainError, ValueError))

    def test_tc10_unknown_block(self):
        msg = self._bad([{"type": "fuzz"}])
        self.assertIn("fuzz", msg)

    def test_tc10_missing_type(self):
        self._bad([{"gain_db": 6}])

    def test_tc10_empty_list(self):
        self._bad([])

    def test_tc10_not_a_list(self):
        for bad in ("gate", {"type": "gate"}, None, 5):
            with self.subTest(chain=bad):
                self._bad(bad)

    def test_tc10_param_out_of_range(self):
        cases = [
            [{"type": "gate", "threshold_db": 5}],
            [{"type": "gate", "range_db": -91}],
            [{"type": "comp", "ratio": 0.5}],
            [{"type": "comp", "ratio": 21}],
            [{"type": "drive", "gain_db": 49}],
            [{"type": "drive", "mix": 1.5}],
            [{"type": "eq", "highpass_hz": 1001}],
            [{"type": "eq", "lowpass_hz": 500}],
            [{"type": "eq", "bands": [{"freq_hz": 10}]}],
            [{"type": "eq", "bands": [{"freq_hz": 1000, "gain_db": 25}]}],
            [{"type": "eq", "bands": [{"freq_hz": 1000, "q": 0}]}],
            [{"type": "cab", "cutoff_hz": 1000}],
            [{"type": "reverb", "decay_s": 11}],
            [{"type": "reverb", "wet": -0.1}],
            [{"type": "delay", "feedback": 0.96}],
            [{"type": "delay", "time_ms": 0}],
            [{"type": "amp", "model": "x", "input_db": 30}],
        ]
        for chain in cases:
            with self.subTest(chain=chain):
                self._bad(chain)

    def test_tc10_unknown_param(self):
        self._bad([{"type": "gate", "foo": 1}])

    def test_tc10_wrong_type(self):
        for chain in ([{"type": "drive", "gain_db": "loud"}],
                      [{"type": "eq", "bands": "1k+6"}],
                      [{"type": "gate"}, "comp"]):
            with self.subTest(chain=chain):
                self._bad(chain)

    def test_tc10_type_not_a_string(self):
        # регрессия кросс-ревью: нехэшируемый/нестроковый type — ChainError, не TypeError
        for t in ([], {}, 5):
            with self.subTest(type=t):
                self._bad([{"type": t}])

    def test_tc10_amp_requires_model(self):
        self._bad([{"type": "amp"}])

    def test_bounds_inclusive(self):
        fx = _fx()
        ok = [{"type": "gate", "threshold_db": 0, "range_db": -90},
              {"type": "comp", "ratio": 20, "threshold_db": -60},
              {"type": "eq", "highpass_hz": 1000},
              {"type": "eq", "lowpass_hz": 1000},
              {"type": "eq", "lowpass_hz": 22000},
              {"type": "delay", "feedback": 0.95, "time_ms": 2000}]
        self.assertEqual(len(fx.parse_chain(ok)), len(ok))

    def test_defaults_filled(self):
        fx = _fx()
        for t, want in DEFAULTS.items():
            with self.subTest(block=t):
                blk = {"type": t, "model": "fake"} if t == "amp" else {"type": t}
                got = fx.parse_chain([blk])
                self.assertEqual(len(got), 1)
                self.assertEqual(got[0]["type"], t)
                for k, v in want.items():
                    self.assertIn(k, got[0])
                    if isinstance(v, (int, float)):
                        self.assertAlmostEqual(float(got[0][k]), float(v), places=6, msg=k)
                    else:
                        self.assertEqual(got[0][k], v, k)

    def test_order_preserved_and_json_roundtrip(self):
        fx = _fx()
        chain = [{"type": "delay"}, {"type": "eq", "highpass_hz": 80}, {"type": "gate"}]
        got = fx.parse_chain(json.loads(json.dumps(chain)))
        self.assertEqual([b["type"] for b in got], ["delay", "eq", "gate"])
        self.assertAlmostEqual(float(got[1]["highpass_hz"]), 80)
        json.dumps(got)  # нормализованная цепочка — снова данные (JSON)

    def test_needs_gpu(self):
        fx = _fx()
        self.assertTrue(fx.needs_gpu([{"type": "eq"}, {"type": "amp", "model": "x"}]))
        self.assertFalse(fx.needs_gpu([{"type": t} for t in BLOCKS if t != "amp"]))

    def test_process_rejects_bad_chain(self):
        fx = _fx()
        x, _ = _impulse()
        for chain in ([], [{"type": "fuzz"}]):
            with self.subTest(chain=chain), self.assertRaises(fx.ChainError):
                fx.process(x, SR, chain, _res())

    def test_process_missing_amp_is_chain_error(self):
        fx = _fx()
        x, _ = _impulse()
        with self.assertRaises(fx.ChainError):
            fx.process(x, SR, [{"type": "amp", "model": "nope"}], FakeResources())
        with self.assertRaises(fx.ChainError):
            fx.process(x, SR, [{"type": "amp", "model": "nope"}], None)


if __name__ == "__main__":
    unittest.main()
