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
    """resources по контракту: ir(name) → (ir, sr), amp(name) → модель,
    kit(name) → (список сэмплов, sr); нет — KeyError."""

    def __init__(self, amps=None, irs=None, kits=None):
        self.amps = dict(amps or {})
        self.irs = dict(irs or {})
        self.kits = dict(kits or {})

    def amp(self, name):
        return self.amps[name]

    def ir(self, name):
        return self.irs[name]

    def kit(self, name):
        return self.kits[name]


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


# ---------- Условие 14 (internal-studio-engine, этап 5а): блок «сэмплер» (sampler) ----------
#
# Контракт: вход — дорожка части барабанов; удары — начала нот во входе (моно-сумма),
# пик удара — максимум |x| в 15 мс после начала; удары тише p95·10^(floor_db/20) (от
# громких) отбрасываются; сила удара → слой набора по рангу (громкий удар — громкий
# сэмпл, соседние удары сдвигаются на −1/0/+1 слой); сэмпл (в частоте входа) ставится
# пиком на пик удара, сила — пик удара / пик сэмпла; выход — только новые удары,
# RMS как у входа, затем × output_db; длина и форма — как у входа; нет ударов — тишина.
# Набор — resources.kit(name) → (список сэмплов, sr); нет набора — ChainError.
#
# Слои фейкового набора различимы по частоте: слой i — затухающий тон HZ_i с пиком
# ровно на отсчёте ATT (не на нуле — так видно, что сэмплер ставит пик, а не начало).
# Удар во входе — затухающий тон 90 Гц с пиком |x| ровно на первом отсчёте.

ATT = 40                                   # отсчёт пика сэмпла
LAYER_HZ = (400, 700, 1250, 2200, 4000)   # частоты слоёв, по возрастанию пика
LAYER_PEAK = (0.2, 0.4, 0.6, 0.8, 1.0)
HIT_HZ = 90


def _layer(hz, peak, sr=SR, dur=0.1, ch=None):
    k = np.arange(int(dur * sr))
    att = ATT * sr // SR if sr != SR else ATT
    env = np.where(k <= att, k / att, np.exp(-(k - att) / (0.02 * sr)))
    x = (peak * env * np.cos(2 * np.pi * hz * (k - att) / sr)).astype(np.float32)
    if ch:
        x = np.stack([x] * ch, axis=1)
    return x


def _kit(sr=SR, ch=None, hz=LAYER_HZ, peaks=LAYER_PEAK):
    # слои в перемешанном порядке: сортирует по пику сам движок
    order = [3, 0, 4, 1, 2][:len(hz)] if len(hz) == 5 else list(range(len(hz)))
    return [_layer(hz[i], peaks[i], sr=sr, ch=ch) for i in order], sr


def _kit_res(name="test/kick", **kw):
    return FakeResources(kits={name: _kit(**kw)})


def _hits(positions, amps, n, ch=None, sr=SR):
    """Удары: затухающий тон HIT_HZ длиной 80 мс, пик |x| = amp ровно на отсчёте p."""
    x = np.zeros(n, dtype=np.float64)
    k = np.arange(int(0.08 * sr))
    shape = np.exp(-k / (0.01 * sr)) * np.cos(2 * np.pi * HIT_HZ * k / sr)
    for p, a in zip(positions, amps, strict=True):
        m = min(len(k), n - p)
        x[p:p + m] += a * shape[:m]
    x = x.astype(np.float32)
    if ch:
        x = np.stack([x] * ch, axis=1)
    return x


def _sampler(**params):
    return _block("sampler", kit=params.pop("kit", "test/kick"), **params)


def _mono(y):
    y = np.abs(np.asarray(y, dtype=np.float64))
    return y.max(axis=1) if y.ndim == 2 else y


def _peak_near(y, p, sr=SR, before=0.05, after=0.1):
    a = _mono(y)
    lo, hi = max(0, p - int(before * sr)), min(len(a), p + int(after * sr))
    return lo + int(np.argmax(a[lo:hi])), float(a[lo:hi].max())


def _layer_of(y, p, sr=SR, hz=LAYER_HZ):
    """Индекс слоя (по частоте) в выходе у удара p: тон с наибольшей амплитудой на [p, p+50 мс)."""
    amps = [_tone_amp(y, f, sr=sr, t0=p / sr, t1=p / sr + 0.05) for f in hz]
    return int(np.argmax(amps))


def _positions(count, start=0.2, step=0.3, sr=SR, jitter=(0, 7, 13, 3, 11, 5, 2, 9)):
    # некруглые отсчёты: не на сетке блоков/окон
    return [int((start + i * step) * sr) + 17 + jitter[i % len(jitter)] for i in range(count)]


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestSampler(unittest.TestCase):

    def _run(self, x, chain, res=None, sr=SR):
        out = _fx().process(x, sr, chain, res if res is not None else _kit_res())
        self.assertEqual(out.shape, x.shape)
        self.assertEqual(out.dtype, np.float32)
        self.assertTrue(np.all(np.isfinite(out)))
        return out

    # --- без сдвига ---

    def test_no_shift_mono(self):
        pos = _positions(8)
        n = pos[-1] + SR
        x = _hits(pos, [0.8] * len(pos), n)
        y = self._run(x, [_sampler()])
        for p in pos:
            with self.subTest(hit=p):
                at, _ = _peak_near(y, p)
                self.assertLessEqual(abs(at - p), 1, f"пик выхода {at}, удар {p}")

    def test_no_shift_stereo_input_mono_kit(self):
        pos = _positions(6)
        n = pos[-1] + SR
        x = _hits(pos, [0.8] * len(pos), n, ch=2)
        y = self._run(x, [_sampler()])
        self.assertEqual(y.shape, (n, 2))
        for p in pos:
            with self.subTest(hit=p):
                at, _ = _peak_near(y, p)
                self.assertLessEqual(abs(at - p), 1, f"пик выхода {at}, удар {p}")

    def test_no_shift_mono_input_stereo_kit(self):
        pos = _positions(6)
        n = pos[-1] + SR
        x = _hits(pos, [0.8] * len(pos), n)
        y = self._run(x, [_sampler()], res=_kit_res(ch=2))
        self.assertEqual(y.shape, (n,))
        for p in pos:
            with self.subTest(hit=p):
                at, _ = _peak_near(y, p)
                self.assertLessEqual(abs(at - p), 1, f"пик выхода {at}, удар {p}")

    def test_length_shape_odd(self):
        n = SR + 7
        pos = [SR // 4 + 3, SR // 2 + 11]
        for ch in (None, 2):
            with self.subTest(ch=ch):
                x = _hits(pos, [0.8] * 2, n, ch=ch)
                y = self._run(x, [_sampler()])
                self.assertEqual(y.shape, x.shape)

    def test_hit_before_sample_attack(self):
        # удар раньше ATT: начало сэмпла за левым краем — обрезается, пик на месте
        n = SR + 7
        pos = [ATT // 2, SR // 2 + 3]
        y = self._run(_hits(pos, [0.8] * 2, n), [_sampler()])
        at, _ = _peak_near(y, pos[0], before=0.001, after=0.004)
        self.assertLessEqual(abs(at - pos[0]), 1, f"пик выхода {at}, удар {pos[0]}")

    def test_hit_at_end_tail_cut(self):
        # удар у конца: хвост сэмпла обрезается, длина та же, пик на месте
        n = SR + 7
        pos = [SR // 3, n - 200]
        y = self._run(_hits(pos, [0.8] * 2, n), [_sampler()])
        self.assertEqual(len(y), n)
        at, _ = _peak_near(y, pos[1], before=0.001, after=0.004)
        self.assertLessEqual(abs(at - pos[1]), 1, f"пик выхода {at}, удар {pos[1]}")

    def test_click_at_input_edges_peak_in_place(self):
        # регрессия кросс-ревью: удар у самых краёв входа (сэмпл 0, 20, последний,
        # предпоследний) находится и ставится пиком на место (±1) — при 22,05 и 48 кГц
        for sr in (22050, 48000):
            n = sr + 7
            res = FakeResources(kits={"test/kick": _kit(sr=sr)})
            for p in (0, 20, n - 1, n - 2):
                with self.subTest(sr=sr, click=p):
                    x = np.zeros(n, dtype=np.float32)
                    for k, a in enumerate((0.8, -0.4, 0.2)):  # короткий щелчок, пик |x| на p
                        if p + k < n:
                            x[p + k] = a
                    y = self._run(x, [_sampler()], res=res, sr=sr)
                    self.assertGreater(float(_mono(y).max()), 0, "удар у края не найден — выход тишина")
                    at = _peak(y)
                    self.assertLessEqual(abs(at - p), 1, f"пик выхода {at}, щелчок {p}")

    # --- сила → слой ---

    def test_loud_hit_loud_layer_quiet_hit_quiet_layer(self):
        # самый громкий удар — из громких слоёв, самый тихий — из тихих (с учётом сдвига ±1)
        amps = [0.5, 0.2, 0.7, 1.0, 0.35, 0.6, 0.9, 0.45, 0.8, 0.3]
        pos = _positions(len(amps))
        x = _hits(pos, amps, pos[-1] + SR)
        y = self._run(x, [_sampler(floor_db=-40)])
        top = len(LAYER_HZ) - 1
        loud, quiet = pos[amps.index(1.0)], pos[amps.index(0.2)]
        self.assertGreaterEqual(_layer_of(y, loud), top - 1, "громкий удар — не громкий слой")
        self.assertLessEqual(_layer_of(y, quiet), 1, "тихий удар — не тихий слой")

    def test_layer_monotone_by_rank(self):
        from scipy.stats import spearmanr
        rng = np.random.default_rng(5)
        amps = list(rng.permutation(np.linspace(0.2, 1.0, 15)))
        pos = _positions(len(amps))
        x = _hits(pos, amps, pos[-1] + SR)
        y = self._run(x, [_sampler(floor_db=-40)])
        layers = [_layer_of(y, p) for p in pos]
        # ранговая корреляция силы удара и слоя: слой — по рангу, сдвиг соседей ±1
        rho = spearmanr(amps, layers).correlation
        self.assertGreater(rho, 0.8, f"слои {layers} при силе {np.round(amps, 2)}")

    def test_equal_hits_alternate_layers(self):
        # соседние удары одной силы — не один и тот же сэмпл («пулемёт»)
        pos = _positions(8)
        x = _hits(pos, [0.6] * len(pos), pos[-1] + SR)
        y = self._run(x, [_sampler()])
        layers = [_layer_of(y, p) for p in pos]
        self.assertGreaterEqual(len(set(layers)), 2, f"слои {layers}")

    def test_strength_scales_sample(self):
        # сила удара = пик удара / пик сэмпла: отношение пиков выхода = отношению пиков ударов
        amps = [1.0, 0.5, 0.8, 0.3]
        pos = _positions(len(amps))
        x = _hits(pos, amps, pos[-1] + SR)
        y = self._run(x, [_sampler(floor_db=-40)])
        peaks = [_peak_near(y, p)[1] for p in pos]
        for a, pk in zip(amps, peaks, strict=True):
            with self.subTest(amp=a):
                self.assertAlmostEqual(pk / peaks[0], a / amps[0], delta=0.03 * a / amps[0])

    def test_output_is_only_new_hits(self):
        # исходного удара (90 Гц) в выходе нет — только сэмплы
        pos = _positions(5)
        x = _hits(pos, [0.8] * 5, pos[-1] + SR)
        y = self._run(x, [_sampler()])
        for p in pos:
            with self.subTest(hit=p):
                layer_amp = max(_tone_amp(y, f, t0=p / SR, t1=p / SR + 0.05) for f in LAYER_HZ)
                self.assertLess(_tone_amp(y, HIT_HZ, t0=p / SR, t1=p / SR + 0.05), 0.1 * layer_amp)

    # --- порог floor_db ---

    def test_floor_drops_quiet_leak_hits(self):
        # протечка соседних барабанов — удары на −24 дБ от громких: при −18 отсекаются
        loud, quiet = 0.8, 0.8 * 10 ** (-24 / 20)
        amps = [loud, quiet, loud, quiet, loud, loud, quiet, loud, quiet, loud]
        pos = _positions(len(amps))
        x = _hits(pos, amps, pos[-1] + SR)
        y = self._run(x, [_sampler()])  # floor_db по умолчанию −18
        top = float(_mono(y).max())
        self.assertGreater(top, 0)
        for p, a in zip(pos, amps, strict=True):
            with self.subTest(hit=p, quiet=a == quiet):
                _, pk = _peak_near(y, p, before=0.002, after=0.05)
                if a == quiet:
                    self.assertLess(pk, 1e-3 * top, "тихий удар протечки не отсечён")
                else:
                    self.assertGreater(pk, 0.5 * top)

    def test_floor_lower_keeps_quiet_hits(self):
        loud, quiet = 0.8, 0.8 * 10 ** (-24 / 20)
        amps = [loud, quiet, loud, quiet, loud, loud]
        pos = _positions(len(amps))
        x = _hits(pos, amps, pos[-1] + SR)
        y = self._run(x, [_sampler(floor_db=-40)])
        top = float(_mono(y).max())
        for p, a in zip(pos, amps, strict=True):
            if a == quiet:
                with self.subTest(hit=p):
                    _, pk = _peak_near(y, p, before=0.002, after=0.05)
                    self.assertGreater(pk, 0.02 * top, "при floor_db −40 удар −24 дБ потерян")

    # --- громкость ---

    def test_rms_matches_input(self):
        pos = _positions(8)
        x = _hits(pos, [0.9, 0.5, 0.7, 0.6, 0.9, 0.4, 0.8, 0.7], pos[-1] + SR)
        for ch in (None, 2):
            with self.subTest(ch=ch):
                xi = x if ch is None else np.stack([x, x], axis=1)
                y = self._run(xi, [_sampler()])
                self.assertAlmostEqual(_db(_rms(y, 0, len(x) / SR) / _rms(xi, 0, len(x) / SR)), 0, delta=0.1)

    def test_output_db(self):
        pos = _positions(6)
        x = _hits(pos, [0.9] * 6, pos[-1] + SR)
        base = self._run(x, [_sampler()])
        for g in (-12, 6):
            with self.subTest(output_db=g):
                y = self._run(x, [_sampler(output_db=g)])
                dur = len(x) / SR
                self.assertAlmostEqual(_db(_rms(y, 0, dur) / _rms(base, 0, dur)), g, delta=0.05)

    # --- края ---

    def test_no_hits_is_silence(self):
        for ch in (None, 2):
            with self.subTest(ch=ch):
                x = np.zeros(SR if ch is None else (SR, ch), dtype=np.float32)
                y = self._run(x, [_sampler()])
                self.assertTrue(np.all(y == 0))

    def test_kit_other_sample_rate_resampled(self):
        # набор 22,05 кГц, дорожка 48 кГц: тон слоя тот же (не ускорен), пики на месте
        sr_kit = 22050
        pos = _positions(5)
        x = _hits(pos, [0.8] * 5, pos[-1] + SR)
        res = FakeResources(kits={"test/kick": _kit(sr=sr_kit, hz=(1000,) * 5)})
        y = self._run(x, [_sampler()], res=res)
        for p in pos:
            with self.subTest(hit=p):
                at, _ = _peak_near(y, p)
                self.assertLessEqual(abs(at - p), 1, f"пик выхода {at}, удар {p}")
                right = _tone_amp(y, 1000, t0=p / SR, t1=p / SR + 0.05)
                wrong = _tone_amp(y, 1000 * SR / sr_kit, t0=p / SR, t1=p / SR + 0.05)
                self.assertGreater(right, 3 * wrong, "частота сэмпла не пересчитана")

    def test_missing_kit_is_chain_error(self):
        fx = _fx()
        x = _hits([1000], [0.8], SR)
        with self.assertRaises(fx.ChainError) as cm:
            fx.process(x, SR, [_sampler(kit="nope/kick")], _kit_res())
        self.assertIn("nope/kick", str(cm.exception), "причина не называет набор")
        with self.assertRaises(fx.ChainError):
            fx.process(x, SR, [_sampler(kit="test/kick")], FakeResources())
        with self.assertRaises(fx.ChainError):
            fx.process(x, SR, [_sampler(kit="test/kick")], None)

    # --- проверка блока ---

    def test_parse_defaults_and_bounds(self):
        fx = _fx()
        got = fx.parse_chain([{"type": "sampler", "kit": "osdk/kick"}])[0]
        self.assertEqual(got["kit"], "osdk/kick")
        self.assertAlmostEqual(float(got["floor_db"]), -18)
        self.assertAlmostEqual(float(got["output_db"]), 0)
        ok = fx.parse_chain([{"type": "sampler", "kit": "a/b", "floor_db": -40, "output_db": -24},
                             {"type": "sampler", "kit": "a/b", "floor_db": 0, "output_db": 24}])
        self.assertEqual(len(ok), 2)
        for bad in ({"floor_db": -41}, {"floor_db": 1}, {"output_db": -25}, {"output_db": 25},
                    {"floor_db": "loud"}):
            with self.subTest(bad=bad), self.assertRaises(fx.ChainError):
                fx.parse_chain([{"type": "sampler", "kit": "a/b", **bad}])

    def test_kit_required_string(self):
        fx = _fx()
        for blk in ({"type": "sampler"}, {"type": "sampler", "kit": 5}, {"type": "sampler", "kit": ["a/b"]}):
            with self.subTest(block=blk):
                with self.assertRaises(fx.ChainError) as cm:
                    fx.parse_chain([blk])
                self.assertIn("kit", str(cm.exception), "причина не называет поле kit")

    def test_in_blocks_json_after_gain(self):
        import os
        with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "fx_blocks.json")) as f:
            blocks = json.load(f)
        keys = list(blocks)
        self.assertIn("sampler", keys)
        self.assertEqual(keys.index("sampler"), keys.index("gain") + 1)
        spec = blocks["sampler"]
        params = {p["id"]: p for p in spec["params"]}
        self.assertEqual((params["floor_db"]["default"], params["floor_db"]["min"], params["floor_db"]["max"]),
                         (-18, -40, 0))
        self.assertEqual((params["output_db"]["default"], params["output_db"]["min"], params["output_db"]["max"]),
                         (0, -24, 24))
        kit = [s for s in spec["strings"] if s["id"] == "kit"]
        self.assertEqual(len(kit), 1)
        self.assertEqual(kit[0].get("asset"), "kit")
        self.assertTrue(kit[0].get("required"))


# ---------- Условие 14/ТК19: место удара sampler не зависит от начала окна ----------
#
# Удар с двумя почти равными пиками: вспышки 300 Гц длиной 5 мс (огибающая cos²) —
# 0,95 на 6 мс и 1,0 на 28 мс от начала удара, дальше хвост 55 Гц. То же содержимое
# режется окнами с началом 0, 137, 911, 1777, 2403 сэмпла; пик выхода у каждого удара —
# на его главном пике (28 мс, ±1) во всех окнах.

KICK_PEAK1 = int(0.006 * SR)    # 288
KICK_PEAK2 = int(0.028 * SR)    # 1344 — главный пик


def _kick_hits(positions, n, amp=0.8, sr=SR):
    k = np.arange(int(0.4 * sr))
    shape = np.zeros(len(k))
    half = int(0.0025 * sr)
    for c, pk in ((KICK_PEAK1, 0.95), (KICK_PEAK2, 1.0)):
        d = np.arange(-half, half + 1)
        env = np.cos(np.pi * d / (2 * half)) ** 2
        shape[c + d] += pk * env * np.cos(2 * np.pi * 300 * d / sr)
    t0 = KICK_PEAK2 + half + 1                  # хвост 55 Гц после второй вспышки
    tt = (k[t0:] - t0) / sr
    shape[t0:] += 0.4 * np.minimum(tt / 0.001, 1.0) * np.exp(-tt / 0.12) * np.sin(2 * np.pi * 55 * tt)
    x = np.zeros(n, dtype=np.float64)
    for p in positions:
        m = min(len(k), n - p)
        x[p:p + m] += amp * shape[:m]
    return x.astype(np.float32)


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestSamplerWindowStart(unittest.TestCase):

    def test_tc19_hit_place_independent_of_window_start(self):
        pos = _positions(8, start=0.3, step=0.35)
        n = pos[-1] + SR
        x = _kick_hits(pos, n)
        # самопроверка входа: главный пик — на 28 мс, второй (6 мс) — 0,95 от него
        seg = np.abs(x[pos[0]:pos[0] + int(0.04 * SR)])
        self.assertEqual(int(np.argmax(seg)), KICK_PEAK2)
        self.assertAlmostEqual(float(seg[KICK_PEAK1] / seg[KICK_PEAK2]), 0.95, places=3)
        for s in (0, 137, 911, 1777, 2403):              # 0…50 мс, некруглые
            with self.subTest(window_start=s):
                y = _fx().process(x[s:], SR, [_sampler()], _kit_res())
                self.assertEqual(y.shape, x[s:].shape)
                for p in pos:
                    at = s + _peak_near(y, p - s)[0]
                    self.assertLessEqual(abs(at - (p + KICK_PEAK2)), 1,
                                         f"удар {p}: пик выхода {at - p} сэмплов от начала удара, "
                                         f"главный пик на {KICK_PEAK2}")


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


# ---------- Условие 15 (internal-studio-engine, этап 5б): блок «бас» (bass), ТК10–13 ----------
#
# Контракт (карточка): вход — басовая дорожка; ритм — сетка долей по самой дорожке,
# division ячеек на долю (1…4, по умолчанию 2 = восьмые); на ячейке со звуком — нота
# с частотой дорожки; тише p90·10^(floor_db/20) (по умолчанию −20) — тишина. Новый
# щипок — только при смене ноты или новом ударе во входе, иначе нота тянется; начало
# щипка — на начале удара входа (±1 кадр поиска = 128 сэмплов при 48 кГц). Высота
# сэмпла — из самого звука; нужная нота — ближайший сэмпл, сдвиг пересэмплированием;
# громкость и тембр следуют за входом (две полосы, окно 0,5 с, предел ±15 дБ) + output_db.
# Набор — resources.kit(name) → (список сэмплов, sr); нет набора — ChainError.
#
# Фейковый набор: щипки на E1/A1/C2/E2, по 3 слоя силы; у каждого сэмпла — тишина
# перед атакой (разная), атака 1 мс, щелчок 3 кГц 8 мс (по нему считаются щипки
# в выходе: у входа щелчков нет), основной тон + 2-я/3-я гармоники, спад τ = 1 с.
# Имена файлов движку не передаются — высоту он берёт из звука.

BASS_KIT = "test/bass"
BASS_KIT_HZ = (41.2, 55.0, 65.4, 82.4)          # E1, A1, C2, E2
BASS_LAYER_PEAK = (0.3, 0.6, 1.0)
A1, D2, E2 = 55.0, 73.42, 82.41
BEAT = 0.5                                      # 120 BPM
EIGHTH = BEAT / 2
CLICK_HZ = 3000
ONSET_TOL = 128                                 # ±1 кадр поиска при 48 кГц


def _pluck(hz, peak, sr=SR, dur=2.5, lead=0, tau=1.0, click=True):
    """Щипок: `lead` сэмплов тишины, атака 1 мс, спад tau; пик |x| ≈ peak."""
    k = np.arange(int(dur * sr))
    t = k / sr
    att = 0.001 * sr
    env = np.minimum(k / att, 1.0) * np.exp(-t / tau)
    x = (np.sin(2 * np.pi * hz * t) + 0.5 * np.sin(2 * np.pi * 2 * hz * t)
         + 0.25 * np.sin(2 * np.pi * 3 * hz * t)) * env
    if click:
        x += 0.5 * np.sin(2 * np.pi * CLICK_HZ * t) * np.exp(-t / 0.002) * (k < int(0.008 * sr))
    x = x / np.abs(x).max() * peak
    return np.concatenate([np.zeros(lead), x]).astype(np.float32)


def _bass_kit(sr=SR, ch=None):
    leads = (0, 97, 160, 240, 31, 205)
    samples = []
    for i, hz in enumerate(BASS_KIT_HZ):
        for j, pk in enumerate(BASS_LAYER_PEAK):
            s = _pluck(hz, pk, sr=sr, lead=leads[(i * 3 + j) % len(leads)] * sr // SR)
            samples.append(np.stack([s, s], axis=1) if ch else s)
    # перемешанный порядок: высоту и силу движок определяет сам
    order = [7, 2, 10, 0, 5, 11, 3, 8, 1, 6, 9, 4]
    return [samples[i] for i in order], sr


def _bass_res(sr=SR, ch=None):
    return FakeResources(kits={BASS_KIT: _bass_kit(sr=sr, ch=ch)})


def _bass(**params):
    return _block("bass", kit=params.pop("kit", BASS_KIT), **params)


def _note(hz, amp, dur, sr=SR, tau=0.3):
    """Нота входа: резкая атака 1 мс, спад tau, 5 мс затухания в конце; без щелчка."""
    k = np.arange(int(dur * sr))
    t = k / sr
    env = np.minimum(k / (0.001 * sr), 1.0) * np.exp(-t / tau)
    env *= np.clip((len(k) - k) / (0.005 * sr), 0, 1)
    x = (np.sin(2 * np.pi * hz * t) + 0.4 * np.sin(2 * np.pi * 2 * hz * t)) * env
    return x / np.abs(x).max() * amp


def _bass_line(events, total, sr=SR):
    """events: (начало, с; частота; амплитуда; длительность, с; tau) → дорожка и отсчёты ударов."""
    x = np.zeros(int(total * sr))
    hits = []
    for t0, hz, amp, dur, tau in events:
        p = int(round(t0 * sr))
        y = _note(hz, amp, dur, sr=sr, tau=tau)
        x[p:p + len(y)] += y[:len(x) - p]
        hits.append(p)
    return x.astype(np.float32), hits


def _first_above(a, lo, hi, thr):
    idx = np.nonzero(a[lo:hi] > thr)[0]
    return lo + int(idx[0]) if len(idx) else None


def _onset(y, p, sr=SR, before=0.1, after=0.1, rel=0.1):
    """Первый отсчёт около p, где |y| превышает rel × пик |y| в [p, p + after)."""
    a = _mono(y)
    lo, hi = max(0, p - int(before * sr)), min(len(a), p + int(after * sr))
    pk = float(a[p:hi].max())
    return _first_above(a, lo, hi, rel * pk) if pk > 0 else None


def _f0(y, t0, t1, sr=SR, fmin=30.0, fmax=130.0):
    """Основной тон куска [t0, t1) по автокорреляции: первый пик ≥ 0,9 максимума."""
    x = np.asarray(y, dtype=np.float64)
    if x.ndim > 1:
        x = x.mean(axis=1)
    seg = x[int(t0 * sr):int(t1 * sr)]
    seg = seg - seg.mean()
    n = len(seg)
    spec = np.fft.rfft(seg, 2 * n)
    r = np.fft.irfft(spec * np.conj(spec))[:n]
    r = r / np.maximum(n - np.arange(n), 1)       # несмещённая оценка
    lo, hi = int(sr / fmax), int(sr / fmin)
    seg_r = r[lo:hi + 2]
    m = seg_r[1:-1].max()
    for i in range(1, len(seg_r) - 1):
        if seg_r[i] >= 0.9 * m and seg_r[i] >= seg_r[i - 1] and seg_r[i] >= seg_r[i + 1]:
            a, b, c = seg_r[i - 1], seg_r[i], seg_r[i + 1]
            d = 0.5 * (a - c) / (a - 2 * b + c) if (a - 2 * b + c) != 0 else 0.0
            return sr / (lo + i + d)
    return None


def _cents(f, ref):
    return 1200 * np.log2(f / ref)


def _clicks(y, t0, t1, ref, sr=SR):
    """Щипки в выходе на [t0, t1): щелчки выше 1 кГц громче 0,01·ref (−40 дБ; громкость
    щелчка зависит от слоя и полосы тембра, фон между щелчками ниже −90 дБ), разнесённые ≥ 60 мс."""
    from scipy.signal import butter, sosfiltfilt
    x = np.asarray(y, dtype=np.float64)
    if x.ndim == 2:
        x = x.mean(axis=1)
    hp = np.abs(sosfiltfilt(butter(4, 1000, "highpass", fs=sr, output="sos"), x))
    a, b = int(t0 * sr), int(t1 * sr)
    idx = np.nonzero(hp[a:b] > 0.01 * ref)[0]
    out = []
    for i in idx:
        if not out or i - out[-1] > int(0.06 * sr):
            out.append(int(i))
    # начало щелчка — первый отсчёт выше 0,25 его собственного пика (порог −40 дБ
    # срабатывает раньше на «предзвоне» фильтра у громких щелчков)
    res = []
    for i in out:
        lo = a + i
        w = hp[lo:lo + int(0.01 * sr)]
        res.append(lo + int(np.argmax(w > 0.25 * w.max())))
    return res


def _hp_peak(y, sr=SR):
    from scipy.signal import butter, sosfiltfilt
    x = np.asarray(y, dtype=np.float64)
    if x.ndim == 2:
        x = x.mean(axis=1)
    return float(np.abs(sosfiltfilt(butter(4, 1000, "highpass", fs=sr, output="sos"), x)).max())


def _rms_db(y, t0, t1, sr=SR):
    x = np.asarray(y, dtype=np.float64)
    seg = x[int(t0 * sr):int(t1 * sr)]
    return 10 * np.log10(max(float(np.mean(seg ** 2)), 1e-24))


# ТК10: нота/пауза на восьмых (ноты — на долях), 120 BPM, A1/D2/E2; 0,5 с тишины до и после
LEAD = 0.5
TK10_NOTES = (A1, D2, E2, A1, E2, D2, A1, E2)


def _tk10_input(sr=SR):
    ev = [(LEAD + i * BEAT, hz, 0.5, 0.22, 0.3) for i, hz in enumerate(TK10_NOTES)]
    return _bass_line(ev, LEAD + len(ev) * BEAT + 0.5, sr=sr)


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestBassHelpers(unittest.TestCase):
    """Самопроверка замеров теста на заведомо известных сигналах (не движок)."""

    def test_f0_and_onset_on_known_signal(self):
        for hz in (A1, D2, E2) + BASS_KIT_HZ:
            with self.subTest(hz=hz):
                y = _pluck(hz, 0.8, lead=500, dur=0.5)
                self.assertLess(abs(_cents(_f0(y, 0.03, 0.25), hz)), 5)
                self.assertLessEqual(abs(_onset(y, 500) - 500), 20)
        x, hits = _tk10_input()
        for p, hz in zip(hits, TK10_NOTES, strict=True):
            self.assertLess(abs(_cents(_f0(x, p / SR + 0.02, p / SR + 0.2), hz)), 5)
            self.assertLessEqual(abs(_onset(x, p) - p), 40)

    def test_clicks_count_known_signal(self):
        y = np.zeros(3 * SR, dtype=np.float32)
        for p in (SR // 2, SR, int(1.25 * SR)):
            s = _pluck(A1, 0.8, dur=3.0)  # без обрыва в пределах y
            y[p:p + len(s)] += s[:len(y) - p]
        self.assertEqual(len(_clicks(y, 0, 3, _hp_peak(y))), 3)


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestBass(unittest.TestCase):

    def _run(self, x, chain, res=None, sr=SR):
        out = _fx().process(x, sr, chain, res if res is not None else _bass_res(sr=sr))
        self.assertEqual(out.shape, x.shape, "длина/форма выхода ≠ входу")
        self.assertEqual(out.dtype, np.float32)
        self.assertTrue(np.all(np.isfinite(out)))
        return out

    # --- ТК10 ---

    def _check_tk10(self, x, hits, y, sr=SR):
        a = _mono(y)
        loud = float(a.max())
        self.assertGreater(loud, 1e-3, "выход молчит")
        for p, hz in zip(hits, TK10_NOTES, strict=True):
            with self.subTest(note=hz, at=p / sr):
                f = _f0(y, p / sr + 0.02, p / sr + 0.2, sr=sr)
                self.assertIsNotNone(f, "в ячейке со звуком нет тона")
                self.assertLess(abs(_cents(f, hz)), 50, f"частота выхода {f:.2f} Гц, вход {hz} Гц")
                on_in, on_out = _onset(x, p, sr=sr), _onset(y, p, sr=sr)
                self.assertIsNotNone(on_out, "щипка нет")
                self.assertLessEqual(abs(on_out - on_in), ONSET_TOL * sr // SR,
                                     f"щипок на {on_out}, удар входа на {on_in}")
        # вход молчит: начало трека, пауза-ячейки (с запасом на спад 50 мс), хвост
        quiet = [(0.0, LEAD - 2 * ONSET_TOL / SR)]
        quiet += [(p / sr + EIGHTH + 0.05, p / sr + BEAT - 2 * ONSET_TOL / SR) for p in hits]
        quiet[-1] = (hits[-1] / sr + EIGHTH + 0.05, len(a) / sr)
        for t0, t1 in quiet:
            with self.subTest(silence=(round(t0, 3), round(t1, 3))):
                seg = a[int(t0 * sr):int(t1 * sr)]
                self.assertLessEqual(_db(float(seg.max()) / loud), -60,
                                     "вход молчит, а выход звучит")

    def test_tc10_pitch_onsets_silence(self):
        x, hits = _tk10_input()
        y = self._run(x, [_bass()])
        self._check_tk10(x, hits, y)

    def test_tc10_stereo_input(self):
        # басовая дорожка — стерео-файл: форма выхода как у входа, ноты те же
        x, hits = _tk10_input()
        xs = np.stack([x, x], axis=1)
        y = self._run(xs, [_bass()])
        self._check_tk10(x, hits, y)

    def test_tc10_stereo_kit(self):
        x, hits = _tk10_input()
        y = self._run(x, [_bass()], res=_bass_res(ch=2))
        self._check_tk10(x, hits, y)

    def test_tc10_kit_other_rate_pitch_from_sound(self):
        # набор 44,1 кГц, дорожка 48 кГц: высота — из звука с учётом частоты набора
        x, hits = _tk10_input()
        y = self._run(x, [_bass()], res=_bass_res(sr=44100))
        self._check_tk10(x, hits, y)

    def test_tc10_silent_input_silent_output(self):
        for ch in (None, 2):
            with self.subTest(ch=ch):
                x = np.zeros(3 * SR if ch is None else (3 * SR, ch), dtype=np.float32)
                y = self._run(x, [_bass()])
                self.assertTrue(np.all(y == 0))

    # --- ТК11: долгая нота тянется, новый удар — новый щипок ---

    def _context(self, t_from, t_to, hz=E2):
        """Восьмые с ударом на каждой (для сетки по дорожке) на [t_from, t_to)."""
        n = int(round((t_to - t_from) / EIGHTH))
        return [(t_from + i * EIGHTH, hz, 0.5, EIGHTH, 0.12) for i in range(n)]

    def _tc11_input(self, middle):
        # такт восьмых E2 | 4 доли A1 (middle) | такт восьмых E2
        t_mid = LEAD + 4 * BEAT
        t_end = t_mid + 4 * BEAT
        ev = self._context(LEAD, t_mid) + middle(t_mid) + self._context(t_end, t_end + 4 * BEAT)
        x, _ = _bass_line(ev, t_end + 4 * BEAT + 0.5)
        return x, t_mid, t_end

    def test_tc11_long_note_one_pluck(self):
        x, t_mid, t_end = self._tc11_input(lambda t: [(t, A1, 0.5, 4 * BEAT, 1.5)])
        y = self._run(x, [_bass()])
        ref = _hp_peak(y)
        self.assertGreater(ref, 0, "в выходе нет щипков")
        got = _clicks(y, t_mid - ONSET_TOL / SR, t_end - 2 * ONSET_TOL / SR, ref)
        self.assertEqual(len(got), 1, f"долгая нота без новых ударов: щипков {len(got)}, нужен 1")
        self.assertLessEqual(abs(got[0] - int(t_mid * SR)), ONSET_TOL + int(0.002 * SR))
        # нота тянется: в середине долгой ноты выход звучит на A1
        f = _f0(y, t_mid + 1.0, t_mid + 1.3)
        self.assertIsNotNone(f, "долгая нота оборвалась")
        self.assertLess(abs(_cents(f, A1)), 50)

    def test_tc11_same_note_hit_every_eighth(self):
        x, t_mid, t_end = self._tc11_input(
            lambda t: [(t + i * EIGHTH, A1, 0.5, EIGHTH, 0.12) for i in range(8)])
        y = self._run(x, [_bass()])
        ref = _hp_peak(y)
        got = _clicks(y, t_mid - ONSET_TOL / SR, t_end - 2 * ONSET_TOL / SR, ref)
        self.assertEqual(len(got), 8, f"удар на каждой восьмой: щипков {len(got)}, нужно 8")
        for i, p in enumerate(got):
            with self.subTest(eighth=i):
                self.assertLessEqual(abs(p - int((t_mid + i * EIGHTH) * SR)),
                                     ONSET_TOL + int(0.002 * SR))

    # --- ТК12: громкость следует за входом ---

    def test_tc12_loudness_follows_input(self):
        # 16 восьмых A1 с ударом на каждой: первые 8 — тихо, вторые 8 — на 12 дБ громче
        lo_amp = 0.1
        hi_amp = lo_amp * 10 ** (12 / 20)
        ev = [(LEAD + i * EIGHTH, A1, lo_amp if i < 8 else hi_amp, EIGHTH, 0.12) for i in range(16)]
        x, _ = _bass_line(ev, LEAD + 16 * EIGHTH + 0.5)
        y = self._run(x, [_bass()])
        mid = LEAD + 8 * EIGHTH
        q = (LEAD + EIGHTH, mid - 0.25)          # тихая половина без краёв
        hi = (mid + 0.5, LEAD + 16 * EIGHTH - 0.05)  # громкая: после окна 0,5 с
        din = _rms_db(x, *hi) - _rms_db(x, *q)
        self.assertAlmostEqual(din, 12, delta=0.5)  # самопроверка входа
        dout = _rms_db(y, *hi) - _rms_db(y, *q)
        self.assertAlmostEqual(dout, 12, delta=3, msg=f"вход +12 дБ, выход {dout:+.1f} дБ")

    def test_output_db_scales(self):
        x, _ = _tk10_input()
        base = self._run(x, [_bass()])
        for g in (-6, 6):
            with self.subTest(output_db=g):
                y = self._run(x, [_bass(output_db=g)])
                r = float(np.sqrt(np.mean(np.asarray(y, np.float64) ** 2)
                                  / np.mean(np.asarray(base, np.float64) ** 2)))
                self.assertAlmostEqual(_db(r), g, delta=0.5)

    def test_floor_db_quiet_cell_is_silence(self):
        # ячейка на 30 дБ тише громких: при floor_db −20 (умолчание) — тишина, при −40 — нота
        amps = [0.5, 0.5, 0.5 * 10 ** (-30 / 20), 0.5, 0.5, 0.5, 0.5, 0.5]
        ev = [(LEAD + i * BEAT, A1, a, 0.22, 0.3) for i, a in enumerate(amps)]
        x, hits = _bass_line(ev, LEAD + len(ev) * BEAT + 0.5)
        p = hits[2]
        for floor, sounds in ((None, False), (-40, True)):
            with self.subTest(floor_db=floor):
                y = self._run(x, [_bass() if floor is None else _bass(floor_db=floor)])
                a = _mono(y)
                cell = float(a[p + int(0.02 * SR):p + int(0.2 * SR)].max())
                rel = _db(cell / float(a.max()))
                if sounds:
                    self.assertGreater(rel, -60, "тихая нота выше floor_db пропала")
                else:
                    self.assertLessEqual(rel, -60, "нота тише floor_db не заглушена")

    # --- ТК13: ошибки цепочки ---

    def test_tc13_missing_kit_is_chain_error(self):
        fx = _fx()
        x, _ = _tk10_input()
        with self.assertRaises(fx.ChainError) as cm:
            fx.process(x, SR, [_bass(kit="growlybass/bass")], _bass_res())
        self.assertIn("growlybass/bass", str(cm.exception), "причина не называет набор")
        for res in (FakeResources(), None):
            with self.subTest(resources=res), self.assertRaises(fx.ChainError):
                fx.process(x, SR, [_bass()], res)

    def test_tc13_division_out_of_range(self):
        fx = _fx()
        x, _ = _tk10_input()
        for bad in (0, 5, -1, 2.5, "2"):
            with self.subTest(division=bad):
                with self.assertRaises(fx.ChainError):
                    fx.parse_chain([_bass(division=bad)])
                with self.assertRaises(fx.ChainError):
                    fx.process(x, SR, [_bass(division=bad)], _bass_res())

    def test_parse_defaults_and_bounds(self):
        fx = _fx()
        got = fx.parse_chain([{"type": "bass", "kit": "growlybass/bass"}])[0]
        self.assertEqual(got["kit"], "growlybass/bass")
        self.assertEqual(int(got["division"]), 2)
        self.assertAlmostEqual(float(got["floor_db"]), -20)
        self.assertAlmostEqual(float(got["output_db"]), 0)
        ok = fx.parse_chain([_bass(division=d) for d in (1, 2, 3, 4)])
        self.assertEqual(len(ok), 4)

    def test_kit_required_string(self):
        fx = _fx()
        for blk in ({"type": "bass"}, {"type": "bass", "kit": 5}, {"type": "bass", "kit": ["a/b"]}):
            with self.subTest(block=blk):
                with self.assertRaises(fx.ChainError) as cm:
                    fx.parse_chain([blk])
                self.assertIn("kit", str(cm.exception), "причина не называет поле kit")


# ---------- Условие 15, ТК16–18 (ревью c15): лишние/пропущенные щипки ----------
#
# Вход ТК16: тон + 2-я гармоника, атака линейно 1/5/10 мс, спад exp(−t/0,4), в конце
# ноты спад 5 мс; длина ноты 0,9 восьмой (пауза 10 %) или 1,0 (легато); ноты
# A1 D2 E2 A1 B1 D2 A1 E2 ×2; темп 97 и 120 BPM. Щипки выхода — по щелчкам набора.

B1 = 61.74
TK16_NOTES = (A1, D2, E2, A1, B1, D2, A1, E2) * 2


def _note2(hz, amp, dur, attack_ms, sr=SR, tau=0.4, phase=0.0):
    """Нота ТК16: тон + 2-я гармоника, линейная атака attack_ms, спад tau, 5 мс в конце."""
    k = np.arange(int(round(dur * sr)))
    t = k / sr
    att = max(attack_ms * 0.001 * sr, 1.0)
    env = np.minimum(k / att, 1.0) * np.exp(-t / tau)
    env *= np.clip((len(k) - k) / (0.005 * sr), 0, 1)
    x = (np.sin(2 * np.pi * hz * t + phase) + 0.5 * np.sin(2 * np.pi * 2 * hz * t + 2 * phase)) * env
    return x / 1.5 * amp


def _line2(notes, eighth, frac, attack_ms, lead=LEAD, tail=0.5, amp=0.5, sr=SR):
    """Ноты подряд на восьмых; возвращает дорожку и отсчёты начал нот."""
    total = lead + len(notes) * eighth + tail
    x = np.zeros(int(round(total * sr)))
    hits = []
    for i, hz in enumerate(notes):
        p = int(round((lead + i * eighth) * sr))
        y = _note2(hz, amp, frac * eighth, attack_ms, sr=sr)
        x[p:p + len(y)] += y[:len(x) - p]
        hits.append(p)
    return x.astype(np.float32), hits


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestBassOnsets(unittest.TestCase):
    """ТК16–18: щипок — только на настоящем ударе или смене ноты; ни лишних, ни пропущенных."""

    _run = TestBass._run

    def _plucks(self, y):
        ref = _hp_peak(y)
        self.assertGreater(ref, 0, "в выходе нет щипков")
        return _clicks(y, 0, len(y) / SR, ref)

    # --- ТК16 ---

    def test_tc16_gapped_notes_exactly_16_on_onsets(self):
        for bpm in (97, 120):
            for att in (1, 5, 10):
                with self.subTest(bpm=bpm, attack_ms=att):
                    x, hits = _line2(TK16_NOTES, 30 / bpm, 0.9, att)
                    got = self._plucks(self._run(x, [_bass()]))
                    self.assertEqual(len(got), 16, f"щипков {len(got)}, нот 16: {[g / SR for g in got]}")
                    for p, g in zip(hits, got, strict=True):
                        self.assertLessEqual(abs(g - p), ONSET_TOL,
                                             f"щипок на {g}, начало ноты {p} ({(g - p) / SR * 1000:+.1f} мс)")

    def test_tc16_legato_exactly_16_within_minus5_plus10_ms(self):
        for bpm in (97, 120):
            for att in (1, 5, 10):
                with self.subTest(bpm=bpm, attack_ms=att):
                    x, hits = _line2(TK16_NOTES, 30 / bpm, 1.0, att)
                    got = self._plucks(self._run(x, [_bass()]))
                    self.assertEqual(len(got), 16, f"щипков {len(got)}, нот 16: {[g / SR for g in got]}")
                    for p, g in zip(hits, got, strict=True):
                        d_ms = (g - p) / SR * 1000
                        self.assertTrue(-5 <= d_ms <= 10, f"щипок {d_ms:+.1f} мс от начала ноты")

    # --- ТК17 ---

    def test_tc17_window_starts_mid_note(self):
        # с первого сэмпла звучит A1 на полной громкости (без атаки), дальше удары каждую восьмую
        hits_n = 12
        notes = TK16_NOTES[1:1 + hits_n]
        x, hits = _line2(notes, EIGHTH, 0.9, 1, lead=EIGHTH)
        k = np.arange(int(round(0.9 * EIGHTH * SR)))
        env = np.exp(-k / SR / 0.4) * np.clip((len(k) - k) / (0.005 * SR), 0, 1)
        head = (np.sin(2 * np.pi * A1 * k / SR + np.pi / 2)
                + 0.5 * np.sin(2 * np.pi * 2 * A1 * k / SR + np.pi)) * env / 1.5 * 0.5
        x[:len(head)] += head.astype(np.float32)
        self.assertGreater(abs(float(x[0])), 0.1, "самопроверка: вход звучит с первого сэмпла")
        got = self._plucks(self._run(x, [_bass()]))
        first = [g for g in got if g < int(0.04 * SR)]
        self.assertEqual(len(first), 1, f"в первые 40 мс щипков {len(first)}, нужен 1")
        strikes = hits_n + 1  # звучащая с начала нота + удары
        self.assertLessEqual(abs(len(got) - strikes), 1,
                             f"щипков {len(got)}, ударов {strikes}: {[g / SR for g in got]}")

    # --- ТК18 ---

    def _tc18_input(self, second_hz, frac):
        # такт восьмых E2 (сетка) | A1 удар | вторая нота без роста громкости | такт E2
        ev_ctx = [(LEAD + i * EIGHTH, E2) for i in range(8)]
        t1 = LEAD + 8 * EIGHTH
        x, _ = _line2([hz for _, hz in ev_ctx], EIGHTH, 0.9, 1)
        total = t1 + 6 * EIGHTH + 8 * EIGHTH + 0.5
        x = np.concatenate([x, np.zeros(int(round(total * SR)) - len(x), np.float32)])
        p1 = int(round(t1 * SR))
        # frac: длина A1 в восьмых — 0,9 (пауза 10 %, как в ТК16) или 1,0 (легато)
        a = _note2(A1, 0.5, frac * EIGHTH, 1)
        x[p1:p1 + len(a)] += a
        end_level = 0.5 * np.exp(-frac * EIGHTH / 0.4)       # уровень A1 перед спадом 5 мс
        p2 = p1 + int(round(EIGHTH * SR))
        b = _note2(second_hz, end_level, 4 * EIGHTH, 5, tau=0.4)
        x[p2:p2 + len(b)] += b
        ctx2 = t1 + 6 * EIGHTH
        for i in range(8):
            p = int(round((ctx2 + i * EIGHTH) * SR))
            n = _note2(E2, 0.5, 0.9 * EIGHTH, 1)
            x[p:p + len(n)] += n
        return x.astype(np.float32), p1, p2, int(round(ctx2 * SR))

    def test_tc18_same_note_without_growth_sustains(self):
        # легато: повтор без роста громкости (провал стыка ≤ 10 мс) — нота тянется
        self._tc18_same(1.0, plucks=1)

    def test_tc18_same_note_after_pause_is_new_pluck(self):
        # нота 0,9 восьмой: 25 мс тишины перед повтором — новый щипок «после тишины» (условие 15)
        self._tc18_same(0.9, plucks=2)

    def _tc18_same(self, frac, plucks):
        x, p1, p2, p_ctx = self._tc18_input(A1, frac)
        # самопроверка: рост громкости < 2 дБ (RMS 20 мс перед спадом конца A1 против 20 мс
        # после атаки повтора)
        a_end = p1 + int(round(frac * EIGHTH * SR)) - int(0.005 * SR)
        before = _rms_db(x, (a_end - int(0.02 * SR)) / SR, a_end / SR)
        after = _rms_db(x, (p2 + int(0.005 * SR)) / SR, (p2 + int(0.025 * SR)) / SR)
        self.assertLess(after - before, 2)
        y = self._run(x, [_bass()])
        got = self._plucks(y)
        mid = [g for g in got if p1 - ONSET_TOL <= g < p_ctx - 2 * ONSET_TOL]
        self.assertEqual(len(mid), plucks, f"щипков {[g / SR for g in mid]}, нужно {plucks}")
        self.assertLessEqual(abs(mid[0] - p1), ONSET_TOL)
        if plucks == 2:
            self.assertLessEqual(abs(mid[1] - p2), ONSET_TOL, "щипок после паузы не на начале повтора")
        f = _f0(y, p2 / SR + 0.1, p2 / SR + 0.4)
        self.assertIsNotNone(f, "нота не тянется")
        self.assertLess(abs(_cents(f, A1)), 50)

    def test_tc18_note_change_without_growth_plucks(self):
        for frac in (0.9, 1.0):
            with self.subTest(note_len=frac):
                self._tc18_change(frac)

    def _tc18_change(self, frac):
        x, p1, p2, p_ctx = self._tc18_input(D2, frac)
        y = self._run(x, [_bass()])
        got = self._plucks(y)
        mid = [g for g in got if p1 - ONSET_TOL <= g < p_ctx - 2 * ONSET_TOL]
        self.assertEqual(len(mid), 2, f"A1 и смена на D2 — 2 щипка, получено {[g / SR for g in mid]}")
        d_ms = (mid[1] - p2) / SR * 1000
        if frac < 1.0:   # после паузы — как ТК16 с паузой: ±128 сэмплов
            self.assertLessEqual(abs(mid[1] - p2), ONSET_TOL, f"щипок смены ноты {d_ms:+.1f} мс")
        else:            # легато — предел ТК16/ТК18: −5…+10 мс
            self.assertTrue(-5 <= d_ms <= 10, f"щипок смены ноты {d_ms:+.1f} мс от начала ноты")
        f = _f0(y, p2 / SR + 0.1, p2 / SR + 0.4)
        self.assertIsNotNone(f)
        self.assertLess(abs(_cents(f, D2)), 50)


# ---------- Условие 15, ТК20–23 (кросс-ревью c15) ----------

A2 = 110.0
TAG_HZ = tuple(1500 + 250 * j for j in range(16))   # метки сэмплов ТК23: щелчок своей частоты


def _pluck_tagged(hz, peak, tag_hz, seed, sr=SR, dur=2.0):
    """Щипок A1 с меткой: щелчок tag_hz 8 мс в начале и свой низкий шум (разные дубли)."""
    k = np.arange(int(dur * sr))
    t = k / sr
    env = np.minimum(k / (0.001 * sr), 1.0) * np.exp(-t / 1.0)
    x = (np.sin(2 * np.pi * hz * t) + 0.5 * np.sin(2 * np.pi * 2 * hz * t)) * env
    x += 0.5 * np.sin(2 * np.pi * tag_hz * t) * np.exp(-t / 0.002) * (k < int(0.008 * sr))
    # шум — ниже 300 Гц: не мешает поиску щелчков выше 1 кГц
    from scipy.signal import butter, sosfilt
    noise = sosfilt(butter(4, 300, "lowpass", fs=sr, output="sos"),
                    np.random.default_rng(seed).standard_normal(len(k)))
    x += 0.05 * noise * env
    return (x / np.abs(x).max() * peak).astype(np.float32)


def _tag_of(y, p, sr=SR):
    """Индекс метки (TAG_HZ) щелчка у щипка около p: наибольшая амплитуда в [p − 3 мс, p + 12 мс)."""
    x = np.asarray(y, dtype=np.float64)
    if x.ndim == 2:
        x = x.mean(axis=1)
    seg = x[max(0, p - int(0.003 * sr)):p + int(0.012 * sr)]
    w = seg * np.hanning(len(seg))
    t = np.arange(len(seg)) / sr
    amps = [abs(np.sum(w * np.exp(-2j * np.pi * f * t))) for f in TAG_HZ]
    return int(np.argmax(amps)), float(max(amps))


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestBassReview(unittest.TestCase):
    """ТК20–23: начало окна с тишиной, легато-смена на октаву, долгая нота, разные дубли."""

    _run = TestBass._run
    _plucks = TestBassOnsets._plucks

    # --- ТК20 ---

    def test_tc20_window_starts_with_10ms_silence(self):
        x, hits = _line2(TK16_NOTES[:8], EIGHTH, 0.9, 1, lead=0.01)
        self.assertTrue(np.all(x[:int(0.01 * SR)] == 0), "самопроверка: первые 10 мс — тишина")
        got = self._plucks(self._run(x, [_bass()]))
        self.assertEqual(len(got), 8, f"щипков {len(got)}, нот 8: {[g / SR for g in got]}")
        self.assertLessEqual(abs(got[0] - hits[0]), ONSET_TOL,
                             f"первый щипок на {got[0]}, начало первой ноты {hits[0]}")

    # --- ТК21 ---

    def test_tc21_legato_octave_change_plucks_a2(self):
        x, p1, p2, p_ctx = TestBassOnsets._tc18_input(self, A2, 1.0)
        a_end = p1 + int(round(EIGHTH * SR)) - int(0.005 * SR)
        before = _rms_db(x, (a_end - int(0.02 * SR)) / SR, a_end / SR)
        after = _rms_db(x, (p2 + int(0.005 * SR)) / SR, (p2 + int(0.025 * SR)) / SR)
        self.assertLess(after - before, 2, "самопроверка: рост громкости < 2 дБ")
        y = self._run(x, [_bass()])
        got = self._plucks(y)
        mid = [g for g in got if p1 - ONSET_TOL <= g < p_ctx - 2 * ONSET_TOL]
        self.assertEqual(len(mid), 2, f"A1 и смена на A2 — 2 щипка, получено {[g / SR for g in mid]}")
        d_ms = (mid[1] - p2) / SR * 1000
        self.assertTrue(-5 <= d_ms <= 15, f"щипок смены на октаву {d_ms:+.1f} мс от начала ноты")
        f = _f0(y, p2 / SR + 0.1, p2 / SR + 0.4)
        self.assertIsNotNone(f)
        self.assertLess(abs(_cents(f, A2)), 50, f"после смены {f:.1f} Гц, нужно A2 {A2}")

    # --- ТК22 ---

    def test_tc22_long_note_sounds_to_the_end(self):
        # одна нота A1 6 с без новых ударов; в наборе — сэмплы A1 по 8 с с медленным спадом
        kit = [_pluck(A1, pk, dur=8.0, tau=4.0) for pk in BASS_LAYER_PEAK], SR
        res = FakeResources(kits={BASS_KIT: kit})
        x, (p,) = _bass_line([(LEAD, A1, 0.5, 6.0, 4.0)], LEAD + 6.5)
        y = self._run(x, [_bass()], res=res)
        t0, t1 = LEAD + 5.0, LEAD + 6.0 - 0.01
        din, dout = _rms_db(x, t0, t1), _rms_db(_mono(y) if y.ndim == 2 else y, t0, t1)
        self.assertLessEqual(abs(dout - din), 3,
                             f"на 5–6 с вход {din:.1f} дБ, выход {dout:.1f} дБ")
        f = _f0(y, t0, t0 + 0.3)
        self.assertIsNotNone(f, "на 5–6 с нота не звучит")
        self.assertLess(abs(_cents(f, A1)), 50)

    # --- ТК23 ---

    def test_tc23_adjacent_plucks_never_same_sample(self):
        peaks = np.linspace(0.3, 1.0, 16)
        order = [11, 3, 14, 7, 0, 9, 5, 15, 2, 12, 8, 1, 13, 6, 10, 4]   # перемешано
        samples = [_pluck_tagged(A1, float(peaks[j]), TAG_HZ[j], seed=j) for j in order]
        # самопроверка: метка сэмпла распознаётся по самому сэмплу
        for j in range(16):
            s = _pluck_tagged(A1, float(peaks[j]), TAG_HZ[j], seed=j)
            z = np.concatenate([np.zeros(1000, np.float32), s])
            self.assertEqual(_tag_of(z, 1000)[0], j)
        res = FakeResources(kits={BASS_KIT: (samples, SR)})
        ev = [(LEAD + i * EIGHTH, A1, 0.5 * 0.97 ** i, EIGHTH, 0.12) for i in range(16)]
        x, hits = _bass_line(ev, LEAD + 16 * EIGHTH + 0.5)
        y = self._run(x, [_bass()], res=res)
        got = self._plucks(y)
        self.assertEqual(len(got), 16, f"щипков {len(got)}, ударов 16")
        used = [_tag_of(y, g)[0] for g in got]
        for i in range(1, 16):
            with self.subTest(pluck=i):
                self.assertNotEqual(used[i], used[i - 1],
                                    f"соседние щипки {i - 1} и {i} — один сэмпл (метка {used[i]}); все: {used}")


# ---------- Условие 26 (этап 5в): хэт и тарелки — kit_open и choke, ТК27–29 ----------
#
# Контракт (из карточки): у sampler необязательные kit_open (строка, "" = нет) и choke (0/1,
# по умолчанию 0). Вид удара — по тому, сколько он звучит во входе: время спада на −20 дБ
# от пика, не дольше чем до следующего удара; граница — среднее геометрическое медиан
# времён спада сэмплов kit и kit_open, замеренных по самим сэмплам. Короткие удары — kit,
# длинные — kit_open. choke=1: новый удар глушит звучащий сэмпл предыдущего (спад 10 мс к
# началу нового). kit_open указан, а набора нет — ChainError с именем набора.
#
# Фейковые наборы различимы по частоте тона; сэмпл — атака ATT отсчётов, дальше
# экспоненциальный спад, −20 дБ от пика ровно через t20. Удар во входе — затухающий тон
# HAT_HZ с пиком |x| на первом отсчёте и спадом −20 дБ через заданное время.

HAT_HZ = 3500          # тон удара во входе
CLOSED_HZ = 800        # тон сэмплов kit («закрытый»)
OPEN_HZ = 9000         # тон сэмплов kit_open («открытый»)
HH_CLOSED, HH_OPEN = "test/hh-closed", "test/hh-open"


def _hat_sample(hz, peak, t20, dur, sr=SR):
    k = np.arange(int(dur * sr))
    tau = t20 * sr / np.log(10)
    env = np.where(k <= ATT, k / ATT, np.exp(-(k - ATT) / tau))
    return (peak * env * np.cos(2 * np.pi * hz * (k - ATT) / sr)).astype(np.float32)


def _hat_kit(hz, t20s, dur, sr=SR):
    peaks = (0.8, 0.6, 1.0)
    return [_hat_sample(hz, pk, t, dur, sr=sr) for pk, t in zip(peaks, t20s, strict=True)], sr


def _hat_res(closed_t20=(0.09, 0.10, 0.11), open_t20=(0.38, 0.40, 0.42), closed_dur=0.4, open_dur=1.2):
    return FakeResources(kits={HH_CLOSED: _hat_kit(CLOSED_HZ, closed_t20, closed_dur),
                               HH_OPEN: _hat_kit(OPEN_HZ, open_t20, open_dur)})


def _hat_hits(events, n, sr=SR):
    """Удары: (отсчёт, пик, t20 — время спада на −20 дБ, с); тон HAT_HZ, пик |x| на отсчёте."""
    x = np.zeros(n, dtype=np.float64)
    for p, a, t20 in events:
        k = np.arange(min(int(3 * t20 * sr), n - p))
        x[p:p + len(k)] += a * np.exp(-k * np.log(10) / (t20 * sr)) * np.cos(2 * np.pi * HAT_HZ * k / sr)
    return x.astype(np.float32)


def _band_amp(y, hz, t0, t1, sr=SR):
    """Амплитуда тона hz на [t0, t1) с окном Ханна (меньше протечки соседнего тона)."""
    y = np.asarray(y, dtype=np.float64)
    if y.ndim > 1:
        y = y.mean(axis=1)
    a, b = int(round(t0 * sr)), int(round(t1 * sr))
    seg = y[a:b]
    w = np.hanning(len(seg))
    t = np.arange(len(seg)) / sr
    return 2 * abs(np.sum(seg * w * np.exp(-2j * np.pi * hz * t))) / w.sum()


def _hh(**params):
    return _sampler(kit=params.pop("kit", HH_CLOSED), **params)


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestSamplerOpenChoke(unittest.TestCase):

    def _run(self, x, chain, res=None):
        out = _fx().process(x, SR, chain, res if res is not None else _hat_res())
        self.assertEqual(out.shape, x.shape)
        self.assertEqual(out.dtype, np.float32)
        self.assertTrue(np.all(np.isfinite(out)))
        return out

    def _kind(self, y, p, win=0.05):
        """'closed' / 'open' — какой набор звучит на [p, p+win); None — не различить."""
        c = _band_amp(y, CLOSED_HZ, p / SR, p / SR + win)
        o = _band_amp(y, OPEN_HZ, p / SR, p / SR + win)
        if c > 3 * o:
            return "closed"
        if o > 3 * c:
            return "open"
        return None

    # --- ТК27 ---

    def test_tc27_short_hits_kit_long_hits_kit_open(self):
        # граница = √(100 мс · 400 мс) = 200 мс; удары 60 мс — kit, 300 мс — kit_open;
        # до следующего удара 1 с (хвост открытого сэмпла к нему −50 дБ — не сдвигает пик)
        kinds = "SLSSLLSL"
        pos = _positions(len(kinds), start=0.2, step=1.0)
        ev = [(p, 0.8, 0.06 if k == "S" else 0.3) for p, k in zip(pos, kinds, strict=True)]
        x = _hat_hits(ev, pos[-1] + int(1.5 * SR))
        y = self._run(x, [_hh(kit_open=HH_OPEN)])
        for p, k in zip(pos, kinds, strict=True):
            with self.subTest(hit=p, sounds_ms=60 if k == "S" else 300):
                want = "closed" if k == "S" else "open"
                self.assertEqual(self._kind(y, p), want,
                                 f"удар звучит {60 if k == 'S' else 300} мс — ждали набор {want}")
                at, _ = _peak_near(y, p)
                self.assertLessEqual(abs(at - p), 1, f"пик выхода {at}, удар {p}")

    def test_tc27_long_hit_cut_by_next_hit_is_kit(self):
        # удар со спадом 300 мс, но следующий через 100 мс → звучал 100 мс < 200 → kit
        q = int(0.3 * SR) + 23
        ev = [(q, 0.8, 0.3), (q + int(0.1 * SR), 0.8, 0.06), (q + int(0.8 * SR), 0.8, 0.3)]
        x = _hat_hits(ev, q + int(2.0 * SR))
        y = self._run(x, [_hh(kit_open=HH_OPEN)])
        self.assertEqual(self._kind(y, q), "closed", "удар оборван следующим через 100 мс — ждали kit")
        # контроль: тот же удар без соседа рядом — kit_open
        self.assertEqual(self._kind(y, ev[2][0]), "open")

    def test_tc27_threshold_from_kit_samples_not_fixed(self):
        # ручного порога нет: граница — по сэмплам наборов. Удар 300 мс при наборах
        # 400 мс / 1600 мс (граница 800 мс) — уже «закрытый»; при 100/400 — «открытый».
        p = int(0.3 * SR) + 11
        x = _hat_hits([(p, 0.8, 0.3)], p + int(2.5 * SR))
        slow = _hat_res(closed_t20=(0.38, 0.40, 0.42), open_t20=(1.5, 1.6, 1.7),
                        closed_dur=1.5, open_dur=2.4)
        self.assertEqual(self._kind(self._run(x, [_hh(kit_open=HH_OPEN)], res=slow), p), "closed")
        self.assertEqual(self._kind(self._run(x, [_hh(kit_open=HH_OPEN)]), p), "open")

    # --- ТК28 ---

    def _tc28(self, choke):
        # наборы: kit спад 10 мс, kit_open 800 мс (граница ≈ 89 мс); удар 1 звучит 120 мс
        # (→ kit_open), удар 2 через 150 мс, короткий (→ kit, другой тон)
        res = _hat_res(closed_t20=(0.01, 0.01, 0.01), open_t20=(0.8, 0.8, 0.8),
                       closed_dur=0.2, open_dur=2.0)
        p1 = int(0.3 * SR) + 17
        p2 = p1 + int(0.15 * SR)
        x = _hat_hits([(p1, 0.8, 0.12), (p2, 0.4, 0.01)], p2 + int(2.5 * SR))
        y = self._run(x, [_hh(kit_open=HH_OPEN, choke=choke)], res=res)
        self.assertEqual(self._kind(y, p1, win=0.01), "open", "первый удар не из kit_open — проверка choke невозможна")
        ref = _band_amp(y, OPEN_HZ, p1 / SR, p1 / SR + 0.01)
        before = _band_amp(y, OPEN_HZ, (p2 - int(0.015 * SR)) / SR, (p2 - int(0.010 * SR)) / SR)
        after = _band_amp(y, OPEN_HZ, p2 / SR, p2 / SR + 0.01)
        return _db(before / ref), _db(after / ref)

    def test_tc28_choke_1_silences_previous_sample(self):
        before, after = self._tc28(choke=1)
        self.assertGreater(before, -12, f"за 10 мс до следующего удара сэмпл уже заглушен ({before:.1f} дБ)")
        self.assertLessEqual(after, -40, f"к началу следующего удара сэмпл звучит {after:.1f} дБ от пика")

    def test_tc28_choke_0_keeps_ringing(self):
        _, after = self._tc28(choke=0)
        self.assertGreater(after, -12, f"choke=0, а сэмпл заглушен ({after:.1f} дБ)")

    def test_tc28_choke_default_is_0(self):
        res = _hat_res()
        p1 = int(0.3 * SR) + 17
        p2 = p1 + int(0.15 * SR)
        x = _hat_hits([(p1, 0.8, 0.3), (p2, 0.4, 0.06)], p2 + int(2.0 * SR))
        a = self._run(x, [_hh(kit_open=HH_OPEN)], res=res)
        b = self._run(x, [_hh(kit_open=HH_OPEN, choke=0)], res=res)
        np.testing.assert_allclose(a, b, atol=1e-6)

    # --- ТК29 ---

    def test_tc29_missing_kit_open_is_chain_error(self):
        fx = _fx()
        x = _hat_hits([(1000, 0.8, 0.06)], SR)
        with self.assertRaises(fx.ChainError) as cm:
            fx.process(x, SR, [_hh(kit_open="nope/hh-open")], _hat_res())
        self.assertIn("nope/hh-open", str(cm.exception), "причина не называет набор kit_open")
        only_closed = FakeResources(kits={HH_CLOSED: _hat_kit(CLOSED_HZ, (0.09, 0.1, 0.11), 0.4)})
        with self.assertRaises(fx.ChainError) as cm:
            fx.process(x, SR, [_hh(kit_open=HH_OPEN)], only_closed)
        self.assertIn(HH_OPEN, str(cm.exception))

    def test_tc29_empty_kit_open_same_as_without(self):
        pos = _positions(6, start=0.2, step=0.6)
        ev = [(p, 0.8, (0.06, 0.3)[i % 2]) for i, p in enumerate(pos)]
        x = _hat_hits(ev, pos[-1] + SR)
        res = _hat_res()
        base = self._run(x, [_hh()], res=res)
        empty = self._run(x, [_hh(kit_open="")], res=res)
        np.testing.assert_allclose(empty, base, atol=1e-6)
        # и kit_open="" не требует второго набора
        only_closed = FakeResources(kits={HH_CLOSED: _hat_kit(CLOSED_HZ, (0.09, 0.1, 0.11), 0.4)})
        self._run(x, [_hh(kit_open="")], res=only_closed)

    def test_tc29_without_kit_open_all_hits_from_kit(self):
        # без kit_open — как этап 5а: и длинные удары получают сэмплы kit
        pos = _positions(4, start=0.2, step=0.6)
        ev = [(p, 0.8, 0.3) for p in pos]
        y = self._run(_hat_hits(ev, pos[-1] + SR), [_hh()])
        for p in pos:
            with self.subTest(hit=p):
                self.assertEqual(self._kind(y, p), "closed")

    def test_parse_kit_open_and_choke(self):
        fx = _fx()
        got = fx.parse_chain([{"type": "sampler", "kit": "osdk/hh-closed"}])[0]
        self.assertEqual(got.get("kit_open", ""), "", "kit_open по умолчанию — не пустая строка")
        self.assertEqual(float(got["choke"]), 0, "choke по умолчанию не 0")
        for choke in (0, 1):
            with self.subTest(choke=choke):
                ok = fx.parse_chain([{"type": "sampler", "kit": "osdk/hh-closed",
                                      "kit_open": "osdk/hh-half", "choke": choke}])[0]
                self.assertEqual(ok["kit_open"], "osdk/hh-half")
                self.assertEqual(float(ok["choke"]), choke)
        for bad in ({"choke": 2}, {"choke": -1}, {"choke": "yes"}, {"kit_open": 5}, {"kit_open": ["a/b"]}):
            with self.subTest(bad=bad), self.assertRaises(fx.ChainError):
                fx.parse_chain([{"type": "sampler", "kit": "osdk/hh-closed", **bad}])


# ---------- Условие 26, ТК34 (кросс-ревью c26): choke глушит к началу атаки сэмпла ----------
#
# Сэмплы с атакой 2 мс (пик не в начале) и длинным хвостом. choke=1: к НАЧАЛУ АТАКИ сэмпла
# следующего удара (не к его пику) звучание предыдущего ≤ −40 дБ от его пика; пик выхода у
# второго удара — на пике удара (±1). Часть «удары ближе 10 мс» тестом не покрыта: удары ближе
# 40 мс сэмплер сливает в один (место удара — главный пик в первых 40 мс, ТК19), choke не на что.

ATT2 = int(0.002 * SR)     # атака 2 мс: пик сэмпла на 96-м отсчёте


def _att_sample(hz, peak, t20, dur, att=ATT2, sr=SR):
    k = np.arange(int(dur * sr))
    tau = t20 * sr / np.log(10)
    env = np.where(k <= att, k / att, np.exp(-(k - att) / tau))
    return (peak * env * np.cos(2 * np.pi * hz * (k - att) / sr)).astype(np.float32)


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestSamplerChokeAttack(unittest.TestCase):

    def _run(self, x, chain, res):
        out = _fx().process(x, SR, chain, res)
        self.assertEqual(out.shape, x.shape)
        self.assertTrue(np.all(np.isfinite(out)))
        return out

    def _tc34(self, choke):
        # kit — «закрытый» (спад 5 мс), kit_open — «открытый» (800 мс), граница ≈ 63 мс;
        # удар 1 (пик 0,8) звучит 100 мс → kit_open, удар 2 через 150 мс (пик 0,08) → kit
        res = FakeResources(kits={
            HH_CLOSED: ([_att_sample(CLOSED_HZ, 1.0, 0.005, 0.2)] * 3, SR),
            HH_OPEN: ([_att_sample(OPEN_HZ, 1.0, 0.8, 2.0)] * 3, SR),
        })
        p1 = int(0.3 * SR) + 17
        p2 = p1 + int(0.15 * SR)
        x = _hat_hits([(p1, 0.8, 0.10), (p2, 0.08, 0.01)], p2 + int(2.5 * SR))
        y = self._run(x, [_hh(kit_open=HH_OPEN, choke=choke, floor_db=-40)], res)
        c = _band_amp(y, CLOSED_HZ, p1 / SR, p1 / SR + 0.01)
        o = _band_amp(y, OPEN_HZ, p1 / SR, p1 / SR + 0.01)
        self.assertGreater(o, 3 * c, "первый удар не из kit_open — проверка choke невозможна")
        return y, p1, p2

    def test_tc34_silenced_by_attack_start_of_next_sample(self):
        y, p1, p2 = self._tc34(choke=1)
        ref = _band_amp(y, OPEN_HZ, p1 / SR, p1 / SR + 0.01)
        a = p2 - ATT2                                   # начало атаки сэмпла второго удара
        during = _band_amp(y, OPEN_HZ, a / SR, p2 / SR)
        self.assertLessEqual(_db(during / ref), -40,
                             f"на атаке сэмпла следующего удара предыдущий звучит {_db(during / ref):.1f} дБ")

    def test_tc34_second_hit_peak_in_place(self):
        y, _, p2 = self._tc34(choke=1)
        at, _ = _peak_near(y, p2, before=0.003, after=0.004)
        self.assertLessEqual(abs(at - p2), 1, f"пик выхода у второго удара на {at - p2} от пика удара")


if __name__ == "__main__":
    unittest.main()
