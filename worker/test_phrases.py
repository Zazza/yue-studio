"""Тесты карточки internal-own-track, этап 13: чистый модуль worker/phrases.py —
каталог фраз, render_dry (ТК132) и круг без шва loop_render (ТК133); этап 13б (условие 100,
ТК137): семья synth (часть synth — тишина, ноты дают блоки цепочки), у барабанных фраз часть
perc (тишина), каталог отдаёт beats/chords/style; ноты блоков synth/perc повторяются во втором
круге входа; громкость synth-фразы — цель −20 dBFS A-RMS; bypass с synth/perc — только эти блоки.

Контракт (из карточки, условия 93–94):
- PHRASES — каталог: id, family (guitar | bass | drums), name {ru, en}, bpm;
  гитарных фраз ≥ 5, басовых ≥ 2, барабанных ≥ 2; у барабанной — части kick, snare, hh;
- render_dry(id, tempo, sr) → ({часть: моно-массив}, cycle_sec): части одной длины,
  cycle_sec·sr = длина ± 1; cycle_sec = база / tempo (± 1 отсчёт); tempo вне 0,5…1,5 →
  ValueError; неизвестный id → KeyError; высота не зависит от темпа (нотные ± 2 %,
  записи ± 3 %);
- loop_render(dry_parts, stems, chain, sr, res=None, bypass=False) → (стерео (n, 2), clipped):
  цепочка — на частях из stems, вход — три круга подряд, результат — средний круг (хвост
  конца круга звучит в начале); прочие части сухие; громкость = A-RMS сухого сведения;
  пик ≤ 0,99 (clipped=True, если пришлось опускать); bypass — сухое сведение тем же путём.

Запуск: cd worker && python3 -m unittest test_phrases -v
"""
import unittest

try:
    import numpy as np
    import scipy  # noqa: F401
    _HAS_NP = True
except ImportError:
    _HAS_NP = False

_SKIP = "нужны numpy/scipy (окружение воркера)"
SR = 44100
FAMILIES = ("guitar", "bass", "drums", "synth")
SILENT_FAMILIES = ("synth",)  # сухая фраза — тишина, звук дают ноты блока synth (условие 100)


# ---------- помощники ----------

def _catalog():
    """Каталог как {id: запись} — PHRASES может быть словарём id → запись или списком записей с id."""
    import phrases
    cat = phrases.PHRASES
    if isinstance(cat, dict):
        return {k: dict(v, id=v.get("id", k)) for k, v in cat.items()}
    return {e["id"]: e for e in cat}


def _ids(family=None):
    return [i for i, e in _catalog().items() if family is None or e["family"] == family]


def _render(pid, tempo=1.0, sr=SR):
    import phrases
    return phrases.render_dry(pid, tempo, sr)


def _sum(parts):
    return np.sum([np.asarray(p, dtype=np.float64) for p in parts.values()], axis=0)


def _log_spectrum(x, sr, lo=40.0, hi=4000.0, bins_per_oct=240):
    """Средний амплитудный спектр на логарифмической сетке частот (в дБ)."""
    from scipy import signal
    f, p = signal.welch(np.asarray(x, dtype=np.float64), sr, nperseg=8192)
    grid = lo * 2 ** (np.arange(int(np.log2(hi / lo) * bins_per_oct)) / bins_per_oct)
    return 10 * np.log10(np.interp(grid, f, p) + 1e-20), bins_per_oct


def _pitch_ratio(a, b, sr):
    """Во сколько раз высота b выше высоты a: сдвиг логарифмического спектра с максимумом
    корреляции (в пределах ± 1/3 октавы)."""
    sa, bpo = _log_spectrum(a, sr)
    sb, _ = _log_spectrum(b, sr)
    sa, sb = sa - sa.mean(), sb - sb.mean()
    best, best_lag = -np.inf, 0
    m = bpo // 3
    for lag in range(-m, m + 1):
        if lag >= 0:
            c = np.dot(sa[:len(sa) - lag], sb[lag:])
        else:
            c = np.dot(sa[-lag:], sb[:len(sb) + lag])
        if c > best:
            best, best_lag = c, lag
    return 2 ** (best_lag / bpo)


def _a_rms(x, sr=SR):
    import fx_engine
    y = fx_engine.a_weight(np.asarray(x, dtype=np.float64), sr)
    return float(np.sqrt(np.mean(y ** 2)))


def _db(v):
    return 20 * np.log10(max(v, 1e-12))


# ---------- ТК132: каталог и render_dry ----------

@unittest.skipUnless(_HAS_NP, _SKIP)
class TestCatalog(unittest.TestCase):

    def test_entries_shape(self):
        cat = _catalog()
        self.assertTrue(cat)
        for pid, e in cat.items():
            with self.subTest(id=pid):
                self.assertIsInstance(pid, str)
                self.assertTrue(pid)
                self.assertIn(e["family"], FAMILIES)
                self.assertIsInstance(e["name"]["ru"], str)
                self.assertIsInstance(e["name"]["en"], str)
                self.assertTrue(e["name"]["ru"] and e["name"]["en"])
                # карточка требует поле bpm, но не его знак: у записи без сетки допустим 0
                self.assertIsInstance(e["bpm"], (int, float))
                self.assertGreaterEqual(e["bpm"], 0)

    def test_tc132_counts_by_family(self):
        self.assertGreaterEqual(len(_ids("guitar")), 5)
        self.assertGreaterEqual(len(_ids("bass")), 2)
        self.assertGreaterEqual(len(_ids("drums")), 2)

    def test_tc132_drum_phrases_have_kick_snare_hh(self):
        for pid in _ids("drums"):
            with self.subTest(id=pid):
                parts, _ = _render(pid)
                for k in ("kick", "snare", "hh"):
                    self.assertIn(k, parts)


@unittest.skipUnless(_HAS_NP, _SKIP)
class TestRenderDry(unittest.TestCase):

    def test_tc132_every_phrase_renders_parts_same_length(self):
        for pid in _ids():
            with self.subTest(id=pid):
                parts, cycle = _render(pid, 1.0)
                self.assertTrue(parts)
                lens = set()
                for name, x in parts.items():
                    x = np.asarray(x)
                    self.assertEqual(x.ndim, 1, f"часть {name} не моно: {x.shape}")
                    self.assertTrue(np.all(np.isfinite(x)), name)
                    lens.add(len(x))
                self.assertEqual(len(lens), 1, f"части разной длины: {lens}")
                n = lens.pop()
                self.assertGreater(n, 0)
                self.assertLessEqual(abs(cycle * SR - n), 1)
                if _catalog()[pid]["family"] not in SILENT_FAMILIES:
                    self.assertGreater(np.max(np.abs(_sum(parts))), 1e-3, "фраза беззвучна")

    def test_tc132_tempo_075_cycle_longer(self):
        for pid in _ids():
            with self.subTest(id=pid):
                parts1, c1 = _render(pid, 1.0)
                parts2, c2 = _render(pid, 0.75)
                self.assertLessEqual(abs(c2 * SR - c1 / 0.75 * SR), 1)
                n2 = len(next(iter(parts2.values())))
                self.assertLessEqual(abs(c2 * SR - n2), 1)
                self.assertEqual(set(parts1), set(parts2))

    def test_tempo_bounds_inclusive(self):
        pid = _ids("drums")[0]
        _, c1 = _render(pid, 1.0)
        for t in (0.5, 1.5):
            with self.subTest(tempo=t):
                parts, c = _render(pid, t)
                self.assertLessEqual(abs(c * SR - c1 / t * SR), 1)

    def test_tc132_tempo_out_of_range_value_error(self):
        pid = _ids()[0]
        for t in (0.4, 1.6, 0, -1):
            with self.subTest(tempo=t):
                with self.assertRaises(ValueError):
                    _render(pid, t)

    def test_tc132_unknown_id_key_error(self):
        with self.assertRaises(KeyError):
            _render("no-such-phrase-xyz")

    def test_tc132_bass_pitch_independent_of_tempo(self):
        for pid in _ids("bass"):
            with self.subTest(id=pid):
                slow, _ = _render(pid, 0.75)
                fast, _ = _render(pid, 1.25)
                r = _pitch_ratio(_sum(slow), _sum(fast), SR)
                self.assertAlmostEqual(r, 1.0, delta=0.02, msg=f"высота сдвинулась в {r:.3f} раза")

    def test_tc132_recording_pitch_independent_of_tempo(self):
        for pid in _ids("guitar"):
            with self.subTest(id=pid):
                slow, _ = _render(pid, 0.75)
                fast, _ = _render(pid, 1.25)
                r = _pitch_ratio(_sum(slow), _sum(fast), SR)
                self.assertAlmostEqual(r, 1.0, delta=0.03, msg=f"высота сдвинулась в {r:.3f} раза")

    def test_pitch_ratio_helper_sees_resample(self):
        # самопроверка замера: пересэмплирование (смена высоты на 25 %) замер обязан увидеть
        pid = _ids("guitar")[0]
        x = _sum(_render(pid, 1.0)[0])
        n = int(len(x) / 1.25)
        y = np.interp(np.arange(n) * 1.25, np.arange(len(x)), x)
        self.assertAlmostEqual(_pitch_ratio(x, y, SR), 1.25, delta=0.02)


# ---------- ТК133: loop_render ----------

CYCLE = 2.0
N = int(CYCLE * SR)
DELAY = [{"type": "delay", "time_ms": 300, "feedback": 0.5}]
GAIN12 = [{"type": "gain", "gain_db": 12}]


def _tone(hz, amp, t0, t1, n=N, sr=SR):
    x = np.zeros(n, dtype=np.float32)
    a, b = int(t0 * sr), int(t1 * sr)
    t = np.arange(b - a) / sr
    x[a:b] = amp * np.sin(2 * np.pi * hz * t)
    return x


def _loop(parts, stems, chain, bypass=False):
    import phrases
    y, clipped = phrases.loop_render(parts, stems, chain, SR, res=None, bypass=bypass)
    y = np.asarray(y, dtype=np.float64)
    return y, clipped


def _late_parts():
    """Фраза с тишиной в первые 0,2 с круга: звук 0,2 с … конец круга."""
    return {"guitar": _tone(440, 0.3, 0.2, CYCLE)}


@unittest.skipUnless(_HAS_NP, _SKIP)
class TestLoopRender(unittest.TestCase):

    def _check_shape(self, y):
        self.assertEqual(y.ndim, 2)
        self.assertEqual(y.shape, (N, 2))

    def test_tc133_delay_tail_wraps_into_cycle_start(self):
        y, _ = _loop(_late_parts(), ["guitar"], DELAY)
        self._check_shape(y)
        head = y[:int(0.2 * SR)]
        self.assertGreater(float(np.sqrt(np.mean(head ** 2))), 1e-3,
                           "в первые 0,2 с нет хвоста дилея с конца круга")

    def test_tc133_bypass_head_silent(self):
        y, clipped = _loop(_late_parts(), ["guitar"], DELAY, bypass=True)
        self._check_shape(y)
        self.assertLess(float(np.max(np.abs(y[:int(0.19 * SR)]))), 1e-4,
                        "сухой круг: в первые 0,2 с должна быть тишина")
        self.assertGreater(float(np.max(np.abs(y[int(0.3 * SR):]))), 0.05)
        self.assertFalse(clipped)

    def test_tc133_length_equals_cycle(self):
        for bypass in (False, True):
            with self.subTest(bypass=bypass):
                y, _ = _loop(_late_parts(), ["guitar"], DELAY, bypass=bypass)
                self._check_shape(y)

    def test_tc133_gain12_loudness_equals_dry(self):
        parts = {"guitar": _tone(1000, 0.05, 0.0, CYCLE), "bass": _tone(110, 0.05, 0.0, CYCLE)}
        wet, clipped = _loop(parts, ["guitar"], GAIN12)
        dry, _ = _loop(parts, ["guitar"], GAIN12, bypass=True)
        self.assertFalse(clipped)
        self.assertAlmostEqual(_db(_a_rms(wet)), _db(_a_rms(dry)), delta=0.5)
        # и bypass не громче/не тише сухого моно-сведения больше, чем на панораму (≤ 3 дБ)
        mono = _sum(parts)
        self.assertAlmostEqual(_db(_a_rms(dry.mean(axis=1))), _db(_a_rms(mono)), delta=3.0)

    def test_tc133_peak_limited_and_clipped_flag(self):
        # две части в фазе по 0,7 → сухой пик 1,4: круг обязан опуститься до 0,99
        parts = {"guitar": _tone(220, 0.7, 0.0, CYCLE), "bass": _tone(220, 0.7, 0.0, CYCLE)}
        for bypass in (False, True):
            with self.subTest(bypass=bypass):
                y, clipped = _loop(parts, ["guitar"], GAIN12, bypass=bypass)
                self.assertLessEqual(float(np.max(np.abs(y))), 0.99 + 1e-6)
                self.assertIs(bool(clipped), True)
                self.assertGreater(float(np.max(np.abs(y))), 0.5, "круг опущен сильнее нужного")

    def test_peak_never_above_099_with_gain(self):
        parts = {"guitar": _tone(1000, 0.5, 0.0, CYCLE)}
        y, _ = _loop(parts, ["guitar"], GAIN12)
        self.assertLessEqual(float(np.max(np.abs(y))), 0.99 + 1e-6)

    def test_tc133_part_not_in_stems_unchanged(self):
        """Цепочка gain +12 дБ на kick: остальные части — сухие. Части разнесены по времени,
        поэтому их вклад видно по отдельности. Общий множитель громкости g (выравнивание
        по сухому сведению) одинаков для всего круга: snare = g·сухой snare, kick = g·4·сухой kick."""
        kick = _tone(60, 0.1, 0.1, 0.9)
        snare = _tone(800, 0.1, 1.1, 1.9)
        parts = {"kick": kick, "snare": snare}
        y, _ = _loop(parts, ["kick"], GAIN12)
        self._check_shape(y)
        sk = slice(int(0.2 * SR), int(0.8 * SR))
        ss = slice(int(1.2 * SR), int(1.8 * SR))
        for ch in range(2):
            with self.subTest(channel=ch):
                yc = y[:, ch]
                g_s = np.dot(yc[ss], snare[ss]) / np.dot(snare[ss], snare[ss])
                g_k = np.dot(yc[sk], kick[sk]) / np.dot(kick[sk], kick[sk])
                self.assertGreater(g_s, 0)
                # snare — точная (масштабированная) копия сухого, без обработки
                res = yc[ss] - g_s * snare[ss]
                self.assertLess(_db(np.sqrt(np.mean(res ** 2)) / np.sqrt(np.mean((g_s * snare[ss]) ** 2))), -60)
                # kick громче snare ровно на +12 дБ относительно сухих
                self.assertAlmostEqual(_db(g_k / g_s), 12.0, delta=0.5)

    def test_tc133_identity_sum_minus_dry_kick_equals_rest(self):
        """Буквальная форма ТК133 «сумма минус сухой kick = сухие остальные» — для цепочки,
        не меняющей громкость (gain 0 дБ): выравнивание здесь — множитель 1."""
        kick = _tone(60, 0.1, 0.1, 0.9)
        snare = _tone(800, 0.1, 1.1, 1.9)
        hh = _tone(6000, 0.02, 0.0, CYCLE)
        parts = {"kick": kick, "snare": snare, "hh": hh}
        zero = [{"type": "gain", "gain_db": 0}]
        y, _ = _loop(parts, ["kick"], zero)
        dry, _ = _loop(parts, ["kick"], zero, bypass=True)
        np.testing.assert_allclose(y, dry, atol=1e-4)


# ---------- ТК137 (этап 13б, условие 100): синты и перкуссия фразой ----------

STYLES = {"pad", "arp", "pulse", "drone"}


@unittest.skipUnless(_HAS_NP, _SKIP)
class TestSynthCatalog(unittest.TestCase):

    def test_tc137_synth_phrases_have_chords_style_beats(self):
        ids = _ids("synth")
        self.assertTrue(ids, "нет фраз семьи synth")
        styles = set()
        for pid in ids:
            e = _catalog()[pid]
            with self.subTest(id=pid):
                self.assertIn(e["style"], STYLES)
                styles.add(e["style"])
                self.assertIsInstance(e["beats"], int)
                self.assertGreater(e["beats"], 0)
                self.assertIsInstance(e["chords"], list)
                self.assertTrue(e["chords"])
                for c in e["chords"]:
                    self.assertIsInstance(c, str)
                    self.assertTrue(c)
                # условие 100: Em C G D, 4 такта, 100 BPM
                self.assertEqual(e["chords"], ["Em", "C", "G", "D"])
                self.assertEqual(e["beats"], 16)
                self.assertEqual(e["bpm"], 100)
        self.assertEqual(styles, STYLES, "нужны стили pad, arp, pulse, drone")

    def test_tc137_synth_part_silent_cycle_by_beats(self):
        for pid in _ids("synth"):
            e = _catalog()[pid]
            with self.subTest(id=pid):
                parts, cycle = _render(pid, 1.0)
                self.assertEqual(set(parts), {"synth"})
                self.assertLess(float(np.max(np.abs(parts["synth"]))), 1e-6, "часть synth — тишина")
                self.assertAlmostEqual(cycle, e["beats"] * 60.0 / e["bpm"], delta=1.5 / SR)

    def test_tc137_every_phrase_has_beats_and_chords(self):
        for pid, e in _catalog().items():
            with self.subTest(id=pid):
                self.assertIn("beats", e)
                self.assertIsInstance(e["beats"], (int, float))
                self.assertIsInstance(e["chords"], list)

    def test_tc137_drum_phrases_have_silent_perc_and_beats(self):
        for pid in _ids("drums"):
            with self.subTest(id=pid):
                self.assertGreater(_catalog()[pid]["beats"], 0)
                parts, _ = _render(pid)
                self.assertIn("perc", parts)
                self.assertLess(float(np.max(np.abs(parts["perc"]))), 1e-6, "часть perc — тишина")
                self.assertGreater(float(np.max(np.abs(_sum(parts)))), 1e-3, "бит пропал")


def _synth_block(notes, **kw):
    blk = {"type": "synth", "osc1": 1, "release_s": 2.0, "notes": notes}
    blk.update(kw)
    return blk


def _pad_notes(cycle):
    """Пэд Em C G D, аккорд на такт (4 такта на круг)."""
    bar = cycle / 4
    chords = [[64, 67, 71], [60, 64, 67], [62, 67, 71], [62, 66, 69]]
    return [{"t": i * bar, "d": bar, "midi": m, "vel": 0.8} for i, m in enumerate(chords)]


@unittest.skipUnless(_HAS_NP, _SKIP)
class TestSynthLoop(unittest.TestCase):

    def test_tc137_note_tail_wraps_into_cycle_start(self):
        dry = {"synth": np.zeros(N, dtype=np.float32)}
        chain = [_synth_block([{"t": CYCLE - 0.1, "d": 0.08, "midi": [60], "vel": 0.8}])]
        y, _ = _loop(dry, ["synth"], chain)
        self.assertEqual(y.shape, (N, 2))
        head = y[:int(0.05 * SR)]
        self.assertGreater(float(np.sqrt(np.mean(head ** 2))), 1e-4,
                           "в первые 0,05 с нет хвоста ноты с конца круга")

    def test_tc137_bypass_synth_reverb_not_silent_and_differs(self):
        dry = {"synth": np.zeros(N, dtype=np.float32)}
        chain = [_synth_block([{"t": 0.0, "d": 0.5, "midi": [60, 64, 67], "vel": 0.8}], release_s=0.3),
                 {"type": "reverb", "decay_s": 2.0, "wet": 0.6}]
        wet, _ = _loop(dry, ["synth"], chain)
        byp, _ = _loop(dry, ["synth"], chain, bypass=True)
        self.assertEqual(byp.shape, (N, 2))
        self.assertGreater(float(np.max(np.abs(byp))), 1e-2, "bypass с synth — тишина, а должен быть звук блока")
        diff = float(np.sqrt(np.mean((wet - byp) ** 2)))
        self.assertGreater(diff, 1e-3, "bypass не отличается от результата с ревербом")

    def test_tc137_synth_phrase_loudness_target(self):
        """A-RMS ≈ −20 dBFS (0,1) ± 1 дБ, если пик не упёрся в 0,99; упёрся (clipped) — ниже −20, но не ниже −30."""
        for pid in _ids("synth"):
            parts, cycle = _render(pid, 1.0)
            n = len(parts["synth"])
            for chain in ([_synth_block(_pad_notes(cycle), release_s=0.3)],
                          [_synth_block(_pad_notes(cycle), release_s=0.3),
                           {"type": "reverb", "decay_s": 1.5, "wet": 0.3}]):
                for bypass in (False, True):
                    with self.subTest(id=pid, chain=[b["type"] for b in chain], bypass=bypass):
                        y, clipped = _loop(parts, ["synth"], chain, bypass=bypass)
                        self.assertEqual(y.shape, (n, 2))
                        self.assertLessEqual(float(np.max(np.abs(y))), 0.99 + 1e-6)
                        level = _db(_a_rms(y))
                        if clipped:
                            self.assertLess(level, -20.0 + 1e-6)
                            self.assertGreaterEqual(level, -30.0)
                        else:
                            self.assertAlmostEqual(level, -20.0, delta=1.0)
