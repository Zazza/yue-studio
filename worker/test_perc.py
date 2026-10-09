"""Тесты карточки internal-own-track, этап 5: блок движка perc (условие 30, ТК54–ТК56)
и динамика ударов sampler (условие 35, ТК61).

Контракт (из карточки):
- perc: вход — только длина; удары notes [{t, d, vel}] (t от начала окна, d > 0 — ячейка
  удара, vel 0…1); voice 0 — сэмплы набора kit, 1 хэт, 2 шейкер, 3 хлопок, 4 ковбелл,
  5 римшот, 6 бубен; tone, decay, humanize_ms (детерминированный сдвиг), rel_db (−30…+6,
  по умолчанию −14), output_db; начало звука удара — в t (у kit — первый отсчёт сэмпла
  ≥ 10 % его пика); громкость удара ∝ vel; у kit сильнее vel — громче слой;
  уровень: RMS выхода на ячейках ударов = RMS входа там же × 10^(rel_db/20) ×
  10^(output_db/20), тихий вход — пик −6 дБFS; ударов ≤ 8000; неверные → ChainError.
- sampler: dynamics 0…2 (по умолчанию 1): pk' = med·(pk/med)^dynamics; места ударов
  и общая громкость (RMS входа) — как раньше.

Внешняя граница — набор сэмплов (resources.kit): фейк FakeResources из test_fx_engine.
Написаны по карточке, без чтения реализации.

Запуск: cd worker && python3 -m unittest test_perc -v
"""
import unittest

try:
    import numpy as np
    import scipy  # noqa: F401
    _HAS_DEPS = True
except ImportError:
    _HAS_DEPS = False

_SKIP = "нужны numpy/scipy (окружение воркера)"

SR = 48000


def _fx():
    import fx_engine
    return fx_engine


def _perc(notes, **params):
    # humanize_ms 0 — места ударов точно в t (кроме теста humanize)
    return {"type": "perc", "notes": notes, "humanize_ms": params.pop("humanize_ms", 0), **params}


def _hit(t, d=0.25, vel=1.0):
    return {"t": t, "d": d, "vel": vel}


def _mono(y):
    y = np.abs(np.asarray(y, dtype=np.float64))
    return y.max(axis=1) if y.ndim == 2 else y


def _db(r):
    return 20 * np.log10(max(float(r), 1e-12))


def _tone_amp(x, hz, sr=SR, t0=0.0, t1=0.05):
    x = np.asarray(x, dtype=np.float64)
    if x.ndim > 1:
        x = x.mean(axis=1)
    seg = x[int(t0 * sr):int(t1 * sr)]
    t = np.arange(len(seg)) / sr
    return 2 * abs(np.sum(seg * np.exp(-2j * np.pi * hz * t))) / len(seg)


def _onset(y, t, sr=SR, before=0.02, after=0.06, frac=0.1):
    """Первый отсчёт около t, где |y| ≥ frac × местного пика (определение начала атаки — условие 30)."""
    a = _mono(y)
    lo, hi = max(0, int((t - before) * sr)), min(len(a), int((t + after) * sr))
    seg = a[lo:hi]
    return (lo + int(np.argmax(seg >= frac * seg.max()))) / sr, float(seg.max())


def _silence(n, ch=2):
    return np.zeros((n, ch), dtype=np.float32)


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class _PercCase(unittest.TestCase):

    def _run(self, x, chain, res=None, sr=SR):
        from test_fx_engine import FakeResources
        out = _fx().process(x, sr, chain, res if res is not None else FakeResources())
        self.assertEqual(out.shape, x.shape, "длина/форма выхода ≠ входу")
        self.assertTrue(np.all(np.isfinite(out)))
        return np.asarray(out, dtype=np.float64)


# ---------- ТК54: голоса драм-машины 1…6 ----------

VOICES = {1: "хэт", 2: "шейкер", 3: "хлопок", 4: "ковбелл", 5: "римшот", 6: "бубен"}


class TestPercVoices(_PercCase):
    """ТК54: один удар t = 0,5 vel 1 на тишине 2 с — начало в 0,5 ±2 мс, к 1,9 с затух."""

    def _one(self, voice):
        x = _silence(2 * SR)
        return self._run(x, [_perc([_hit(0.5)], voice=voice)])

    def test_onset_at_t_and_decay(self):
        for v, name in VOICES.items():
            with self.subTest(voice=v, name=name):
                y = _mono(self._one(v))
                peak = float(y.max())
                self.assertGreater(peak, 0.01, "удар не звучит")
                # до 0,498 с — тишина < −80 дБ от пика
                self.assertLess(_db(y[:int(0.498 * SR)].max() / peak), -80, "звук раньше t")
                # начало (первый отсчёт ≥ −20 дБ от пика — порог выбран тестом) — не позже 0,502 с
                first = int(np.argmax(y >= 0.1 * peak)) / SR
                self.assertLessEqual(first, 0.502, f"звук начинается в {first:.4f} с, а не в 0,5")
                # к 1,9 с затух: < −50 дБ от пика
                self.assertLess(_db(y[int(1.9 * SR):].max() / peak), -50, "удар не затух к 1,9 с")

    def test_bright_voices_centroid_above_4k(self):
        # хэт, шейкер, бубен — центр спектра > 4 кГц
        for v in (1, 2, 6):
            with self.subTest(voice=v, name=VOICES[v]):
                y = self._one(v).mean(axis=1)
                p = np.abs(np.fft.rfft(y)) ** 2
                f = np.fft.rfftfreq(len(y), 1 / SR)
                centroid = float(np.sum(f * p) / np.sum(p))
                self.assertGreater(centroid, 4000, f"центр спектра {centroid:.0f} Гц")

    def test_cowbell_spectrum_peak(self):
        # ковбелл — пик спектра 400…1200 Гц
        y = self._one(4).mean(axis=1)
        a = np.abs(np.fft.rfft(y))
        f = np.fft.rfftfreq(len(y), 1 / SR)
        a[f < 20] = 0           # постоянная составляющая не в счёт
        fpk = float(f[int(np.argmax(a))])
        self.assertGreaterEqual(fpk, 400)
        self.assertLessEqual(fpk, 1200)

    def test_clap_has_three_bursts(self):
        # хлопок — ≥ 3 отдельных всплесков в первые 40 мс. Огибающая — RMS кадров по 1 мс;
        # всплеск — подъём выше 50 % максимума после провала ниже 25 % (пороги выбраны тестом)
        y = self._one(3).mean(axis=1)
        seg = y[int(0.5 * SR) - 48:int(0.54 * SR)]
        fr = 48
        env = np.sqrt(np.mean(seg[:len(seg) // fr * fr].reshape(-1, fr) ** 2, axis=1))
        hi, lo = 0.5 * env.max(), 0.25 * env.max()
        bursts, armed = 0, True
        for e in env:
            if armed and e >= hi:
                bursts += 1
                armed = False
            elif not armed and e < lo:
                armed = True
        self.assertGreaterEqual(bursts, 3, f"всплесков {bursts}, огибающая {np.round(env / env.max(), 2)}")

    def test_no_hits_silence(self):
        # край: пустые notes — тишина той же длины
        x = _silence(SR)
        y = self._run(x, [_perc([], voice=1)])
        self.assertTrue(np.all(y == 0))


# ---------- ТК55: уровень относительно трека ----------

def _tone100(dur, rms_db=-20.0, sr=SR):
    t = np.arange(int(dur * sr)) / sr
    x = (10 ** (rms_db / 20) * np.sqrt(2) * np.sin(2 * np.pi * 100 * t)).astype(np.float32)
    return np.stack([x, x], axis=1)


# 8 ударов хэта по 0,25 с подряд: ячейки покрывают [0,5; 2,5) с (вход 3 с — свобода ТК55)
HAT8 = [_hit(0.5 + 0.25 * i, d=0.25) for i in range(8)]


class TestPercLevel(_PercCase):

    def test_rms_on_cells_rel_db(self):
        # ТК55: тон 100 Гц RMS −20 дБFS, rel_db −14 → RMS выхода на ячейках −34 ±1 дБFS
        x = _tone100(3.0)
        y = self._run(x, [_perc(HAT8, voice=1, rel_db=-14)])
        cells = y[int(0.5 * SR):int(2.5 * SR)]
        got = _db(np.sqrt(np.mean(cells ** 2)))
        self.assertAlmostEqual(got, -34, delta=1)

    def test_output_db_adds(self):
        # условие 30: × 10^(output_db/20) поверх rel_db
        x = _tone100(3.0)
        y = self._run(x, [_perc(HAT8, voice=1, rel_db=-14, output_db=-6)])
        cells = y[int(0.5 * SR):int(2.5 * SR)]
        self.assertAlmostEqual(_db(np.sqrt(np.mean(cells ** 2))), -40, delta=1)

    def test_silent_input_peak_minus6(self):
        # ТК55: вход нулевой → пик −6 ±0,5 дБFS
        y = self._run(_silence(3 * SR), [_perc(HAT8, voice=1, rel_db=-14)])
        self.assertAlmostEqual(_db(np.abs(y).max()), -6, delta=0.5)

    def test_vel_half_is_6db_quieter(self):
        # ТК55: в одной партии удар vel 0,5 тише удара vel 1 на 6 ±1 дБ (пики). Голос — ковбелл:
        # тональный, пик удара не зависит от случайного шума (выбор теста)
        y = self._run(_silence(3 * SR), [_perc([_hit(0.5, d=0.5, vel=1.0), _hit(1.5, d=0.5, vel=0.5)], voice=4)])
        a = _mono(y)
        p1 = a[int(0.5 * SR):int(1.4 * SR)].max()
        p2 = a[int(1.5 * SR):int(2.4 * SR)].max()
        self.assertAlmostEqual(_db(p1 / p2), 6, delta=1)


# ---------- ТК56: voice 0 — сэмплы набора; humanize; ошибки ----------

KIT = "test/perc"
PRE = 240                                  # 5 мс «подхода» в начале сэмпла: шум 2 % пика (< 10 %)
KIT_HZ = (400, 1250, 4000)                 # слои различимы по частоте
KIT_PEAK = (0.2, 0.5, 1.0)                 # по возрастанию громкости


def _kit_sample(hz, peak, sr=SR):
    """Сэмпл: PRE отсчётов тихого подхода (2 % пика), затем атака — сразу ≥ 50 % пика (это и есть
    «первый отсчёт ≥ 10 % пика»), пик ≈ через 2–5 мс, затухание 20 мс."""
    k = np.arange(int(0.1 * sr))
    j = k - PRE
    env = np.where(j < 0, 0.02, np.where(j < 96, 0.5 + 0.5 * j / 96, np.exp(-(j - 96) / (0.02 * sr))))
    sig = np.where(j < 0, np.sign(np.sin(2 * np.pi * 3000 * k / sr)), np.cos(2 * np.pi * hz * np.maximum(j, 0) / sr))
    return (peak * env * sig).astype(np.float32)


def _kit_res():
    from test_fx_engine import FakeResources
    order = [2, 0, 1]                      # перемешаны: сортирует по громкости сам движок
    return FakeResources(kits={KIT: ([_kit_sample(KIT_HZ[i], KIT_PEAK[i]) for i in order], SR)})


def _layer_of(y, t):
    amps = [_tone_amp(y, f, t0=t, t1=t + 0.05) for f in KIT_HZ]
    return int(np.argmax(amps))


class TestPercKit(_PercCase):

    def _kit_run(self, notes, **params):
        return self._run(_silence(2 * SR), [_perc(notes, voice=0, kit=KIT, **params)], res=_kit_res())

    def test_vel1_loudest_layer_vel01_quietest(self):
        y = self._kit_run([_hit(0.5, vel=1.0)])
        self.assertEqual(_layer_of(y, 0.5), 2, "vel 1 — не самый громкий слой")
        y = self._kit_run([_hit(0.5, vel=0.1)])
        self.assertEqual(_layer_of(y, 0.5), 0, "vel 0,1 — не самый тихий слой")

    def test_attack_start_at_t(self):
        # начало атаки (первый отсчёт ≥ 10 % пика) — в t ±1 мс; некруглое t — не на сетке блоков
        t = 0.73129
        y = self._kit_run([_hit(t, vel=1.0)])
        at, pk = _onset(y, t)
        self.assertGreater(pk, 0)
        self.assertLessEqual(abs(at - t), 0.001, f"атака в {at:.5f} с, удар в {t}")

    def test_voice0_without_kit_chain_error(self):
        fx = _fx()
        with self.assertRaises(fx.ChainError):
            fx.process(_silence(SR), SR, [_perc([_hit(0.5)], voice=0)], _kit_res())


class TestPercHumanize(_PercCase):

    NOTES = [_hit(0.2 + 0.25 * i, d=0.25) for i in range(8)]

    def _go(self, ms):
        # ковбелл: чёткая атака, место удара меряется однозначно (выбор теста)
        return self._run(_silence(3 * SR), [_perc(self.NOTES, voice=4, humanize_ms=ms)])

    def test_shifts_within_limit_and_present(self):
        base = self._go(0)
        hum = self._go(10)
        shifts = []
        for n in self.NOTES:
            a0, _ = _onset(base, n["t"])
            a1, _ = _onset(hum, n["t"])
            shifts.append(abs(a1 - a0))
        self.assertLessEqual(max(shifts), 0.0105, f"сдвиги {np.round(np.array(shifts) * 1000, 2)} мс")
        # humanize — «случайный сдвиг»: хотя бы один удар сдвинут заметно
        self.assertGreater(max(shifts), 0.0005, "humanize_ms 10 не сдвинул ни одного удара")

    def test_deterministic(self):
        self.assertTrue(np.array_equal(self._go(10), self._go(10)), "два прогона — разный выход")


class TestPercErrors(_PercCase):

    def test_too_many_hits(self):
        fx = _fx()
        notes = [_hit(i * 0.001, d=0.001) for i in range(8001)]
        with self.assertRaises(fx.ChainError):
            fx.process(_silence(9 * 8000), 8000, [_perc(notes, voice=1)], None)

    def test_8000_hits_ok(self):
        # край: ровно 8000 — можно (частота 8 кГц — чтобы тест был быстрым)
        notes = [_hit(i * 0.001, d=0.001) for i in range(8000)]
        y = self._run(_silence(9 * 8000), [_perc(notes, voice=1)], sr=8000)
        self.assertGreater(float(np.abs(y).max()), 0)

    def test_bad_notes(self):
        fx = _fx()
        bad = {
            "vel 1,5": [_hit(0.5, vel=1.5)],
            "vel −0,1": [_hit(0.5, vel=-0.1)],
            "d 0": [_hit(0.5, d=0)],
            "d < 0": [_hit(0.5, d=-0.1)],
            "t < −600": [_hit(-601.0)],
            "не список": "0.5",
        }
        for name, notes in bad.items():
            with self.subTest(case=name):
                with self.assertRaises(fx.ChainError):
                    fx.process(_silence(SR), SR, [{"type": "perc", "voice": 1, "notes": notes}], None)


# ---------- ТК61: sampler dynamics ----------

class TestSamplerDynamics(unittest.TestCase):
    """Удары с пиками 0,1/0,2/0,4/0,8 (дважды, вперемешку); floor_db −40 — чтобы удар 0,1
    (−18 дБ от громких) не отсёкся порогом по умолчанию."""

    AMPS = [0.8, 0.1, 0.4, 0.2, 0.2, 0.8, 0.1, 0.4]

    def setUp(self):
        if not _HAS_DEPS:
            self.skipTest(_SKIP)
        import test_fx_engine as te
        self.te = te
        self.pos = te._positions(len(self.AMPS))
        self.x = te._hits(self.pos, self.AMPS, self.pos[-1] + te.SR)

    def _run(self, **params):
        te = self.te
        y = _fx().process(self.x, te.SR, [te._sampler(floor_db=-40, **params)], te._kit_res())
        self.assertEqual(y.shape, self.x.shape)
        return y

    def _peaks_db(self, y):
        out = []
        for p in self.pos:
            at, pk = self.te._peak_near(y, p, before=0.002, after=0.05)
            self.assertLessEqual(abs(at - p), 1, f"место удара {p} сдвинулось на {at - p}")
            out.append(_db(pk))
        return np.array(out)

    def test_dynamics1_ratios_as_input(self):
        got = self._peaks_db(self._run(dynamics=1))
        want = np.array([_db(a) for a in self.AMPS])
        np.testing.assert_allclose(got - got[0], want - want[0], atol=1.0)

    def test_dynamics_default_is_1(self):
        np.testing.assert_allclose(self._run(), self._run(dynamics=1), atol=1e-6)

    def test_dynamics0_equal_peaks(self):
        got = self._peaks_db(self._run(dynamics=0))
        self.assertLessEqual(float(np.max(np.abs(got - got.mean()))), 1.0, f"пики {np.round(got, 1)} дБ")

    def test_dynamics2_double_range(self):
        got = self._peaks_db(self._run(dynamics=2))
        want = _db(max(self.AMPS)) - _db(min(self.AMPS))          # ≈ 18,1 дБ
        self.assertAlmostEqual(float(got.max() - got.min()), 2 * want, delta=2)

    def test_dynamics_overall_rms_as_input(self):
        # условие 35: общая громкость выхода — как раньше (RMS входа)
        for dyn in (0, 2):
            with self.subTest(dynamics=dyn):
                y = self._run(dynamics=dyn)
                rx = np.sqrt(np.mean(np.square(self.x, dtype=np.float64)))
                ry = np.sqrt(np.mean(np.square(np.asarray(y, dtype=np.float64))))
                self.assertAlmostEqual(_db(ry / rx), 0, delta=0.1)


if __name__ == "__main__":
    unittest.main()
