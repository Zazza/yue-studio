"""Тесты карточки internal-own-track, этап 7б: условия 56 и 58 (тест-кейсы ТК90 и ТК92, часть
про сами сэмплы). Модуль worker/drumsynth.py — чистый синтез наборов, без сети и файлов.

Контракт (из карточки):
- KITS {набор: [части]}: tr808, tr909, linn, cr78, simmons; части у каждого — kick, snare, hh-closed,
  hh-open, ride, crash, tom-small, tom-medium, tom-large, clap, rim, cowbell;
- DRUM_LAYERS = 8 слоёв по силе; render(kit, part, layer, sr) → моно float, звук с отсчёта 0;
  пик растёт со слоем строго; детерминированно;
- ТК90: пик огибающей в первых 25 мс; длина 30 мс…4 с; хвост к концу ≤ −50 дБ от пика; в каждом наборе
  центроид kick < snare < hh-closed; hh-open звучит дольше hh-closed (до −20 дБ); тамы по высоте
  (максимум спектра 50–500 Гц первых 60 мс) small > medium > large; tr808 kick — основной тон хвоста
  40–70 Гц; linn — энергия выше 14 кГц ≤ −40 дБ от всей;
- BASS_KITS {"synthbass": (moog, sub808, acid)}, BASS_RANGE {часть: range MIDI}: у всех трёх E1 (28)…G3 (55),
  по 28 нот (условие 58б, ТК92 новой редакции); render_bass(part, midi, sr) — 2,5 с;
- ТК92 (новая редакция): основной тон каждого сэмпла точным методом (yin с frame 8192 или пик спектра
  с уточнением; карточка допускает любой — тест засчитывает лучший из двух) — ±10 центов от своей ноты.

Предположение (карточка не уточняет): слои нумеруются 0…DRUM_LAYERS−1, сила растёт с номером.
Все сэмплы считаются один раз на модуль (кэш) и переиспользуются тестами.

Запуск: cd worker && python3 -m unittest test_drumsynth -v
"""
import unittest

try:
    import numpy as np
    import scipy  # noqa: F401
    import drumsynth as ds
    _HAS_DEPS = True
except ImportError:   # окружение без numpy/scipy (как make test на ПК) — тесты пропускаются
    _HAS_DEPS = False

_SKIP = "нужны numpy/scipy (окружение воркера)"

SR = 48000
MACHINES = ("tr808", "tr909", "linn", "cr78", "simmons")
PARTS = ("kick", "snare", "hh-closed", "hh-open", "ride", "crash",
         "tom-small", "tom-medium", "tom-large", "clap", "rim", "cowbell")
BASS_PARTS = ("moog", "sub808", "acid")
BASS_FIRST = {"moog": 28, "sub808": 28, "acid": 28}
BASS_LAST = 55
BASS_COUNT = {"moog": 28, "sub808": 28, "acid": 28}

_drums: dict = {}
_bass: dict = {}


def drum(kit, part, layer):
    key = (kit, part, layer)
    if key not in _drums:
        _drums[key] = np.asarray(ds.render(kit, part, layer, SR))
    return _drums[key]


def bass(part, midi):
    key = (part, midi)
    if key not in _bass:
        _bass[key] = np.asarray(ds.render_bass(part, midi, SR))
    return _bass[key]


def layers():
    return range(int(ds.DRUM_LAYERS))


def top():
    return int(ds.DRUM_LAYERS) - 1


def env(x, ms=2.0):
    """Огибающая: скользящее среднеквадратичное за ms."""
    x = np.asarray(x, dtype=np.float64)
    w = max(1, int(ms / 1000 * SR))
    c = np.concatenate([[0.0], np.cumsum(x * x)])
    m = (c[w:] - c[:-w]) / w
    return np.sqrt(np.maximum(np.concatenate([m, np.full(w - 1, m[-1] if len(m) else 0.0)]), 0.0))


def centroid(x):
    p = np.abs(np.fft.rfft(np.asarray(x, dtype=np.float64))) ** 2
    f = np.fft.rfftfreq(len(x), 1 / SR)
    return float(np.sum(f * p) / max(np.sum(p), 1e-30))


def ring_s(x):
    """Сколько звучит: до последнего места, где огибающая ≥ −20 дБ от её пика, с."""
    e = env(x, 5.0)
    return float(np.flatnonzero(e >= 0.1 * e.max())[-1]) / SR


def peak_hz(x, lo, hi, pad=1 << 17):
    x = np.asarray(x, dtype=np.float64) * np.hanning(len(x))
    s = np.abs(np.fft.rfft(x, n=max(pad, len(x))))
    f = np.fft.rfftfreq(max(pad, len(x)), 1 / SR)
    band = (f >= lo) & (f <= hi)
    return float(f[band][np.argmax(s[band])])


def _body(s, sr=SR):
    """Тело сэмпла: 0,05–0,6 с после атаки (первый отсчёт выше 10 % пика)."""
    s = np.asarray(s, dtype=np.float64)
    peak = float(np.abs(s).max())
    a = int(np.argmax(np.abs(s) > 0.1 * peak))
    return s[a + int(0.05 * sr):a + int(0.6 * sr)]


def midi_yin8192(s, sr=SR):
    """Основной тон: librosa.yin, frame 8192, медиана по телу сэмпла."""
    import librosa
    body = _body(s, sr).astype(np.float32)
    with np.errstate(invalid="ignore"):
        f0 = librosa.yin(body, fmin=25, fmax=500, sr=sr, frame_length=8192)
    return float(librosa.hz_to_midi(np.median(f0)))


def midi_spectrum(s, midi, sr=SR):
    """Основной тон: пик спектра тела в окне ±1 полутон от ноты, уточнённый параболой по лог-амплитуде."""
    body = _body(s, sr) * np.hanning(len(_body(s, sr)))
    nfft = 1 << 20
    mag = np.abs(np.fft.rfft(body, n=nfft))
    f = np.fft.rfftfreq(nfft, 1 / sr)
    hz = 440.0 * 2 ** ((midi - 69) / 12)
    band = np.flatnonzero((f >= hz * 2 ** (-1 / 12)) & (f <= hz * 2 ** (1 / 12)))
    k = int(band[np.argmax(mag[band])])
    a, b, c = (np.log(max(mag[i], 1e-300)) for i in (k - 1, k, k + 1))
    den = a - 2 * b + c
    d = 0.5 * (a - c) / den if den != 0 else 0.0
    peak_hz_ = (k + d) * sr / nfft
    # пик в окне ноты должен быть главным в 20–500 Гц, иначе основной тон не там (октава/гармоника)
    wide = (f >= 20) & (f <= 500)
    if mag[k] < 0.5 * float(mag[wide].max()):
        return float("nan")
    return float(12 * np.log2(peak_hz_ / 440.0) + 69)


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestKitsCatalog(unittest.TestCase):
    """Условие 56: наборы и части, 8 слоёв."""

    def test_machines_and_parts(self):
        for kit in MACHINES:
            with self.subTest(kit=kit):
                self.assertIn(kit, ds.KITS)
                self.assertEqual(sorted(ds.KITS[kit]), sorted(PARTS))

    def test_eight_layers(self):
        self.assertEqual(int(ds.DRUM_LAYERS), 8)


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestDrumSamples(unittest.TestCase):
    """ТК90: каждый набор × часть × слой."""

    def test_mono_float_length_and_attack(self):
        for kit in MACHINES:
            for part in PARTS:
                for layer in layers():
                    with self.subTest(kit=kit, part=part, layer=layer):
                        x = drum(kit, part, layer)
                        self.assertEqual(x.ndim, 1, "сэмпл не моно")
                        self.assertTrue(np.issubdtype(x.dtype, np.floating), x.dtype)
                        self.assertTrue(np.all(np.isfinite(x)))
                        self.assertGreaterEqual(len(x), int(0.030 * SR), "короче 30 мс")
                        self.assertLessEqual(len(x), int(4.0 * SR), "длиннее 4 с")
                        pk = float(np.abs(x).max())
                        self.assertGreater(pk, 0.0)
                        self.assertLessEqual(pk, 1.0, "пик выше полной шкалы (PCM 16 обрежет)")
                        # звук с отсчёта 0: первые 2 мс не пустые
                        self.assertGreaterEqual(float(np.abs(x[:int(0.002 * SR)]).max()), 0.01 * pk,
                                                "звук начинается не с начала")
                        self.assertLess(int(np.argmax(env(x, 10.0))), int(0.025 * SR), "пик огибающей позже 25 мс")

    def test_tail_fades_without_click(self):
        for kit in MACHINES:
            for part in PARTS:
                for layer in layers():
                    with self.subTest(kit=kit, part=part, layer=layer):
                        x = drum(kit, part, layer)
                        pk = float(np.abs(x).max())
                        end = float(np.abs(x[-int(0.005 * SR):]).max())
                        self.assertLessEqual(end, pk * 10 ** (-50 / 20), "хвост обрывается громче −50 дБ")

    def test_layers_peak_strictly_grows(self):
        for kit in MACHINES:
            for part in PARTS:
                with self.subTest(kit=kit, part=part):
                    pk = [float(np.abs(drum(kit, part, i)).max()) for i in layers()]
                    for a, b in zip(pk, pk[1:], strict=False):
                        self.assertLess(a, b, f"пики слоёв не растут строго: {pk}")

    def test_deterministic(self):
        for kit in MACHINES:
            for part in PARTS:
                with self.subTest(kit=kit, part=part):
                    for layer in (0, top()):
                        again = np.asarray(ds.render(kit, part, layer, SR))
                        np.testing.assert_array_equal(again, drum(kit, part, layer))


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestKitCharacter(unittest.TestCase):
    """ТК90: соотношения частей внутри набора (по верхнему слою)."""

    def test_centroid_kick_snare_hh(self):
        for kit in MACHINES:
            with self.subTest(kit=kit):
                k, s, h = (centroid(drum(kit, p, top())) for p in ("kick", "snare", "hh-closed"))
                self.assertLess(k, s, f"центроид бочки {k:.0f} не ниже малого {s:.0f}")
                self.assertLess(s, h, f"центроид малого {s:.0f} не ниже хэта {h:.0f}")

    def test_open_hat_rings_longer(self):
        for kit in MACHINES:
            with self.subTest(kit=kit):
                c, o = ring_s(drum(kit, "hh-closed", top())), ring_s(drum(kit, "hh-open", top()))
                self.assertGreater(o, c, f"открытый хэт {o:.3f} с не дольше закрытого {c:.3f} с")

    def test_toms_pitch_order(self):
        for kit in MACHINES:
            with self.subTest(kit=kit):
                f = [peak_hz(drum(kit, p, top())[:int(0.06 * SR)], 50, 500)
                     for p in ("tom-small", "tom-medium", "tom-large")]
                self.assertGreater(f[0], f[1], f"малый там не выше среднего: {f}")
                self.assertGreater(f[1], f[2], f"средний там не выше большого: {f}")

    def test_tr808_kick_tail_fundamental(self):
        for layer in layers():
            with self.subTest(layer=layer):
                x = drum("tr808", "kick", layer)
                tail = x[int(0.1 * SR):int(0.6 * SR)]
                self.assertGreater(len(tail), int(0.1 * SR), "у бочки 808 нет хвоста после 100 мс")
                hz = peak_hz(tail, 20, 200)
                self.assertTrue(40 <= hz <= 70, f"основной тон хвоста {hz:.1f} Гц")

    def test_linn_no_energy_above_14k(self):
        for part in PARTS:
            for layer in (0, top()):
                with self.subTest(part=part, layer=layer):
                    x = drum("linn", part, layer).astype(np.float64)
                    p = np.abs(np.fft.rfft(x)) ** 2
                    f = np.fft.rfftfreq(len(x), 1 / SR)
                    hi = float(p[f > 14000].sum()) / max(float(p.sum()), 1e-30)
                    self.assertLessEqual(10 * np.log10(max(hi, 1e-30)), -40.0,
                                         "linn: энергия выше 14 кГц больше −40 дБ")


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestSynthBass(unittest.TestCase):
    """Условие 58, ТК92: синт-басы — части, ноты, высота каждого сэмпла."""

    def test_bass_kits_parts(self):
        self.assertEqual(list(ds.BASS_KITS), ["synthbass"])
        self.assertEqual(sorted(ds.BASS_KITS["synthbass"]), sorted(BASS_PARTS))

    def test_bass_range(self):
        for part in BASS_PARTS:
            with self.subTest(part=part):
                notes = list(ds.BASS_RANGE[part])
                self.assertEqual(notes, list(range(BASS_FIRST[part], BASS_LAST + 1)))
                self.assertEqual(len(notes), BASS_COUNT[part])

    def test_samples_mono_25s(self):
        for part in BASS_PARTS:
            for midi in ds.BASS_RANGE[part]:
                with self.subTest(part=part, midi=midi):
                    s = bass(part, midi)
                    self.assertEqual(s.ndim, 1)
                    self.assertTrue(np.issubdtype(s.dtype, np.floating))
                    self.assertTrue(np.all(np.isfinite(s)))
                    self.assertAlmostEqual(len(s) / SR, 2.5, delta=0.05)
                    self.assertGreater(float(np.abs(s).max()), 0.0)
                    self.assertLessEqual(float(np.abs(s).max()), 1.0)

    def test_pitch_within_10_cents(self):
        """ТК92 новой редакции: точный замер — ±10 центов (лучший из yin 8192 и пика спектра)."""
        for part in BASS_PARTS:
            for midi in range(BASS_FIRST[part], BASS_LAST + 1):   # по карточке, не по BASS_RANGE
                with self.subTest(part=part, midi=midi):
                    s = bass(part, midi)
                    got = [midi_yin8192(s), midi_spectrum(s, midi)]
                    err = min(abs(g - midi) * 100 for g in got if np.isfinite(g)) \
                        if any(np.isfinite(g) for g in got) else float("inf")
                    self.assertLessEqual(err, 10.0, f"{part} MIDI {midi}: замеры {got}")

if __name__ == "__main__":
    unittest.main()
