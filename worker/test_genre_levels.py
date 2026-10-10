"""Тесты карточки internal-own-track, этап 10: условия 82 и 84 (тест-кейсы ТК122, ТК125 — часть воркера).
Написаны по карточке, без чтения новой реализации.

- ТК122 / 82: у каждого жанрового пресета (genres.build()) цели громкости по эталонам: бочка/малый — таблица
  семьи (мягкий мастер soft — обе на 2 дБ ниже); запись хэта [hh] — −23 (Электроника −25); гитара — прежняя цель
  варианта + сдвиг семьи (Рок −2,5, Тяжёлое −4,5, Поп и другое −1, Электроника 0), медианы −10,5 / −10,5 / −11 /
  −13; бас — без изменений (медианы −7, −7, −6, −7).
- ТК125 / 84: хэт «Живой ритм-секции» (presets.BUILTIN, slug live-rhythm) — sampler (output_db 0) → eq: срез низа
  ≥ 800 Гц, полоса около 10 кГц ≥ +6 дБ, около 1 кГц ≤ −3 дБ; готовая цепочка drums-hh-kit (копия fxPresets на
  воркере, fx_presets.json) — та же; хэты драм-машин — только sampler. Звук: eq цепочки drums-hh-kit на шуме
  «как хэт набора» (центр ≈ 5 кГц, 13 % энергии ниже 500 Гц) → центр ≥ 7 кГц, ниже 500 Гц ≤ 2 %.

Толкования (карточка не уточняет):
- «Различия вариантов внутри семьи сохраняются» проверяется по снимку целей баса и гитары до этапа 10
  (BEFORE — значения genres.build() на коммите fdc7a59): гитара = было + сдвиг семьи, бас = было.
- Мягкий вариант — вариант, у которого в таблице GENRES мастер «soft» (последний элемент кортежа варианта).
- Хэт: цель −23/−25 одинакова и для soft (условие сдвигает на 2 дБ только бочку и малый).
- «Центр спектра» — средняя частота, взвешенная по мощности спектра всего сигнала; «ниже 500 Гц» — доля
  мощности. Частоты полос — «около»: 10 кГц = 8–12,5 кГц, 1 кГц = 0,8–1,25 кГц.
- Звуковая проверка берёт из цепочки только блоки eq (sampler требует сэмплов набора) — проверяется, что
  эквалайзер цепочки делает из звука «как хэт набора» яркий хэт.

Запуск: cd worker && python3 -m unittest test_genre_levels -v
"""
import json
import statistics
import unittest
from pathlib import Path

import genres
import presets

try:
    import numpy as np

    import fx_engine
    _HAS_ENGINE = True
except ImportError:   # окружение без numpy/scipy — звуковая проверка пропускается
    _HAS_ENGINE = False

WORKER = Path(__file__).resolve().parent

DRUMS = {  # семья → (бочка, малый)
    "Рок": (-7.5, -9.5),
    "Поп и другое": (-7.5, -9.5),
    "Тяжёлое": (-8.0, -10.0),
    "Электроника": (-6.0, -11.0),
}
HH = {"Рок": -23.0, "Поп и другое": -23.0, "Тяжёлое": -23.0, "Электроника": -25.0}
GTR_SHIFT = {"Рок": -2.5, "Тяжёлое": -4.5, "Поп и другое": -1.0, "Электроника": 0.0}
GTR_MEDIAN = {"Рок": -10.5, "Тяжёлое": -10.5, "Поп и другое": -11.0, "Электроника": -13.0}
BASS_MEDIAN = {"Рок": -7.0, "Тяжёлое": -7.0, "Поп и другое": -7.0, "Электроника": -6.0}
MACHINES = ("tr808", "tr909", "linn", "cr78", "simmons")

# снимок до этапа 10: slug → (цель баса, цель гитары)
BEFORE = {
    "postpunk-cold": (-6, -10), "postpunk-angular": (-7, -9), "postpunk-machine": (-6, -11),
    "coldwave-tape": (-6, -11), "coldwave-synth": (-6, -12), "coldwave-fog": (-7, -11), "newwave-gated": (-7, -10),
    "newwave-synthbass": (-6, -11), "newwave-electro": (-7, -11), "goth-machine": (-5, -10),
    "goth-cathedral": (-7, -10), "goth-drive": (-6, -10), "indie-britpop": (-7, -8), "indie-garage": (-7, -7),
    "indie-cold": (-7, -9), "garage-fuzz": (-7, -6), "garage-lofi": (-7, -7), "garage-organ": (-7, -8),
    "punk-fast": (-7, -6), "punk-garage": (-7, -6), "punk-pop": (-7, -7), "alt90s-phaser": (-7, -7),
    "alt90s-grunge": (-6, -6), "alt90s-dreamy": (-7, -8), "classic70s-organ": (-7, -7), "classic70s-hall": (-7, -7),
    "classic70s-blues": (-7, -8), "bluesrock-warm": (-7, -8), "bluesrock-dirty": (-7, -7),
    "bluesrock-piano": (-8, -9), "surf-spring": (-8, -7), "surf-organ": (-8, -8), "surf-dark": (-8, -8),
    "psych60s-mellotron": (-7, -9), "psych60s-organ": (-7, -9), "psych60s-harpsichord": (-8, -9),
    "shoegaze-wall": (-8, -6), "shoegaze-dream": (-8, -8), "shoegaze-haze": (-8, -8), "postrock-ambient": (-8, -7),
    "postrock-piano": (-8, -8), "postrock-bells": (-8, -7), "stoner-fuzz": (-6, -6), "stoner-desert": (-6, -6),
    "stoner-doomy": (-6, -6), "heavy-classic": (-8, -5), "heavy-modern": (-8, -5), "heavy-epic": (-8, -5),
    "numetal-chug": (-7, -5), "numetal-machine": (-7, -6), "numetal-dark": (-7, -6), "industrial-machine": (-7, -6),
    "industrial-80s": (-7, -6), "industrial-rust": (-7, -6), "symphonic-choir": (-8, -6),
    "symphonic-strings": (-8, -6), "symphonic-piano": (-8, -7), "doom-slow": (-6, -6), "doom-funeral": (-6, -6),
    "doom-sludge": (-6, -6), "synthpop-808": (-6, -14), "synthpop-dx7": (-6, -14), "synthpop-lead": (-6, -14),
    "synthwave-retro": (-6, -13), "synthwave-night": (-6, -13), "synthwave-dark": (-6, -15),
    "chiptune-toy": (-7, -14), "chiptune-guitars": (-7, -8), "chiptune-bleep": (-7, -15), "ebm-march": (-6, -12),
    "ebm-electro": (-6, -15), "ebm-cold": (-6, -13), "techno-acid": (-6, -16), "techno-minimal": (-6, -16),
    "techno-dark": (-6, -15), "house-piano": (-6, -15), "house-disco": (-6, -13), "house-deep": (-6, -15),
    "triphop-rhodes": (-6, -12), "triphop-strings": (-6, -12), "triphop-piano": (-6, -13),
    "lofihiphop-rhodes": (-6, -12), "lofihiphop-piano": (-6, -13), "lofihiphop-box": (-6, -13),
    "ambient-pad": (-10, -8), "ambient-bells": (-10, -8), "ambient-choir": (-10, -8), "pop80s-ballad": (-7, -11),
    "pop80s-dance": (-6, -12), "pop80s-bright": (-7, -10), "indiepop-jangle": (-7, -9), "indiepop-toy": (-7, -10),
    "indiepop-dream": (-7, -9), "funk-funk": (-6, -10), "funk-disco": (-6, -10), "funk-boogie": (-6, -10),
    "soul-organ": (-7, -10), "soul-rhodes": (-7, -11), "soul-strings": (-7, -11), "lounge-rhodes": (-8, -11),
    "lounge-piano": (-8, -12), "lounge-bossa": (-8, -10),
}


def _specs(p, stem):
    return [s for s in p["specs"] if s.get("stems") == [stem]]


def _level(p, stem):
    found = _specs(p, stem)
    if len(found) != 1:
        raise AssertionError(f"{p['slug']}: записей [{stem}] {len(found)}, want 1")
    return found[0].get("level_db")


def _soft_slugs():
    return {f"{gslug}-{v[0]}" for _f, _g, gslug, variants in genres.GENRES for v in variants if v[-1] == "soft"}


def _eq_ok(test, eq, where):
    """Условие 84: срез низа ≥ 800 Гц, полоса около 10 кГц ≥ +6 дБ, около 1 кГц ≤ −3 дБ."""
    test.assertGreaterEqual(float(eq.get("highpass_hz") or 0), 800, f"{where}: срез низа eq {eq.get('highpass_hz')}")
    bands = eq.get("bands") or []
    hi = [b for b in bands if 8000 <= float(b.get("freq_hz", 0)) <= 12500]
    lo = [b for b in bands if 800 <= float(b.get("freq_hz", 0)) <= 1250]
    test.assertTrue(any(float(b.get("gain_db", 0)) >= 6 for b in hi), f"{where}: нет полосы ~10 кГц ≥ +6: {bands}")
    test.assertTrue(any(float(b.get("gain_db", 0)) <= -3 for b in lo), f"{where}: нет полосы ~1 кГц ≤ −3: {bands}")


def _fx_presets():
    return {p["id"]: p for p in json.loads((WORKER / "fx_presets.json").read_text(encoding="utf-8"))}


# ---------- ТК122: цели громкости жанровых пресетов ----------

class TestGenreLevels(unittest.TestCase):

    def setUp(self):
        self.built = genres.build()
        self.soft = _soft_slugs()

    def test_tc122_snapshot_covers_all(self):
        # снимок и пресеты — одно множество (иначе проверки гитары/баса ниже пропустили бы пресет)
        self.assertEqual({p["slug"] for p in self.built}, set(BEFORE))
        self.assertTrue(self.soft, "в таблице нет вариантов с мастером soft")
        self.assertLess(len(self.soft), len(self.built))

    def test_tc122_kick_snare_by_family(self):
        for p in self.built:
            with self.subTest(slug=p["slug"], family=p["family"]):
                kick, snare = DRUMS[p["family"]]
                if p["slug"] in self.soft:
                    kick, snare = kick - 2, snare - 2
                self.assertAlmostEqual(_level(p, "kick"), kick, places=6)
                self.assertAlmostEqual(_level(p, "snare"), snare, places=6)

    def test_tc122_hh_target(self):
        for p in self.built:
            with self.subTest(slug=p["slug"], family=p["family"]):
                lv = _level(p, "hh")
                self.assertIsNotNone(lv, "у записи хэта нет level_db")
                self.assertAlmostEqual(lv, HH[p["family"]], places=6)

    def test_tc122_guitar_shift_keeps_variants(self):
        for p in self.built:
            with self.subTest(slug=p["slug"], family=p["family"]):
                was = BEFORE[p["slug"]][1]
                self.assertAlmostEqual(_level(p, "guitar"), was + GTR_SHIFT[p["family"]], places=6)

    def test_tc122_guitar_median_by_family(self):
        for fam, want in GTR_MEDIAN.items():
            with self.subTest(family=fam):
                lv = [_level(p, "guitar") for p in self.built if p["family"] == fam]
                self.assertTrue(lv, f"нет пресетов семьи {fam}")
                self.assertLessEqual(abs(statistics.median(lv) - want), 0.5, f"медиана гитары {fam}")

    def test_tc122_bass_unchanged(self):
        for p in self.built:
            with self.subTest(slug=p["slug"]):
                self.assertAlmostEqual(_level(p, "bass"), BEFORE[p["slug"]][0], places=6)
        for fam, want in BASS_MEDIAN.items():
            with self.subTest(family=fam):
                lv = [_level(p, "bass") for p in self.built if p["family"] == fam]
                self.assertLessEqual(abs(statistics.median(lv) - want), 0.5, f"медиана баса {fam}")


# ---------- ТК125: хэт набора ярче ----------

class TestHhKitEqChains(unittest.TestCase):

    def test_tc125_fx_preset_hh_kit(self):
        chain = _fx_presets()["drums-hh-kit"]["chain"]
        self.assertGreaterEqual(len(chain), 2, f"drums-hh-kit: {chain}")
        self.assertEqual(chain[0].get("type"), "sampler")
        self.assertEqual(float(chain[0].get("output_db", 0)), 0.0, "sampler хэта: output_db должен быть 0")
        self.assertEqual(chain[1].get("type"), "eq", "после sampler — eq")
        _eq_ok(self, chain[1], "drums-hh-kit")

    def test_tc125_live_rhythm_hh(self):
        live = [b for b in presets.BUILTIN if b.get("slug") == "live-rhythm"]
        self.assertEqual(len(live), 1, "нет встроенного пресета live-rhythm")
        hh = _specs(live[0], "hh")
        self.assertEqual(len(hh), 1, "у live-rhythm нет записи хэта")
        eng = hh[0]["engine"]
        types = [b.get("type") for b in eng]
        self.assertEqual(types[0], "sampler")
        self.assertEqual(float(eng[0].get("output_db", 0)), 0.0, "sampler хэта: output_db должен быть 0")
        self.assertIn("eq", types[1:], f"хэт live-rhythm без eq после sampler: {types}")
        _eq_ok(self, eng[types.index("eq", 1)], "live-rhythm hh")

    def test_tc125_machines_hh_sampler_only(self):
        fx = _fx_presets()
        for m in MACHINES:
            with self.subTest(machine=m):
                chain = fx[f"drums-hh-{m}"]["chain"]
                self.assertEqual([b.get("type") for b in chain], ["sampler"])


SR = 44100


def _hat_like(seed=3, dur=4.0):
    """Шум «как хэт набора»: 87 % мощности — 2–16 кГц с наклоном вниз (центр всего ≈ 5 кГц),
    13 % — протечка бочки/малого 60–400 Гц."""
    rng = np.random.default_rng(seed)
    n = int(SR * dur)
    f = np.fft.rfftfreq(n, 1 / SR)

    def band(lo, hi, slope):
        x = np.fft.rfft(rng.standard_normal(n))
        m = (f >= lo) & (f <= hi)
        x[~m] = 0
        x[m] *= (f[m] / lo) ** (-slope / 2)
        y = np.fft.irfft(x, n)
        return y / np.sqrt(np.mean(y ** 2))

    x = np.sqrt(0.87) * band(2000, 16000, 1.5) + np.sqrt(0.13) * band(60, 400, 0.0)
    return (0.1 * x).astype(np.float32)


def _spectrum_stats(x):
    """(центр спектра по мощности, Гц; доля мощности ниже 500 Гц)."""
    p = np.abs(np.fft.rfft(np.asarray(x, dtype=np.float64))) ** 2
    f = np.fft.rfftfreq(len(x), 1 / SR)
    return float((p * f).sum() / p.sum()), float(p[f < 500].sum() / p.sum())


@unittest.skipUnless(_HAS_ENGINE, "нужен fx_engine (numpy/scipy — окружение воркера)")
class TestHhKitEqSound(unittest.TestCase):

    def test_tc125_input_is_kit_like(self):
        # предпосылка: шум похож на хэт набора osdk по замеру карточки (5,0 кГц, 13 %)
        c, low = _spectrum_stats(_hat_like())
        self.assertAlmostEqual(c, 5000, delta=300)
        self.assertAlmostEqual(low, 0.13, delta=0.01)

    def test_tc125_eq_makes_hat_bright(self):
        chain = _fx_presets()["drums-hh-kit"]["chain"]
        eqs = [b for b in chain if b.get("type") == "eq"]
        self.assertTrue(eqs, f"в drums-hh-kit нет eq: {[b.get('type') for b in chain]}")
        out = fx_engine.process(_hat_like(), SR, eqs)
        c, low = _spectrum_stats(out)
        self.assertGreaterEqual(c, 7000, f"центр спектра {c:.0f} Гц, want ≥ 7000")
        self.assertLessEqual(low, 0.02, f"ниже 500 Гц {low:.3%}, want ≤ 2 %")


if __name__ == "__main__":
    unittest.main()
