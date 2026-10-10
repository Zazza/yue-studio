"""Тесты карточки internal-own-track, этап 14 «Оркестр» (условия 110–112, ТК142).

Контракт (из карточки):
- 110: FX_KITS — наборы VSCO-2 CE (repo sgossner/VSCO-2-CE, ref — 40 hex, коммит из карточки):
  vsco-violin (solo, ens, pizz, spic), vsco-viola/vsco-cello (ens, pizz, spic),
  vsco-contrabass (sus, pizz, spic), vsco-harp (harp), духовые и медь (sus, stac),
  vsco-mallets (glock, marimba, xylo); части — (каталог, regex), max_s 8.
  Каталог с пробелами («Solo Violin/Arco Vib») экранируется в запросе GitHub (%20).
- 111: fx_engine.sample_midi — кроме «m<MIDI>» и имён, начинающихся с ноты, — нота-лексема
  внутри имени между «_» (или в конце); нижний регистр («a1_f_rr1») не нота → None. C4 = 60.
  _kit_notes берёт высоту из имени (named=True), если sample_midi её нашёл; иначе — по звуку.
- 112: блок bass — параметры fmin_hz (умолч. 30, 20…1000) и fmax_hz (умолч. 400, 100…2000) —
  диапазон поиска высоты входа; по умолчанию поведение прежнее; ноты сворачиваются по октавам
  в диапазон набора; подпись блока «Замена партии (набор)».

Внешние границы: хранилище наборов (resources с kit()/kit_names() — фейк) и сеть
(_http_get воркера подменяется). Сигналы (мелодия-синус, набор из синусов) — генерируются здесь.

Запуск: cd worker && python3 -m unittest test_orchestra -v
"""
import json
import re
import unittest
import urllib.parse
from pathlib import Path
from unittest import mock

try:
    import numpy as np
    import scipy  # noqa: F401
    import librosa  # noqa: F401
    _HAS_DEPS = True
except ImportError:
    _HAS_DEPS = False

_SKIP = "нужны numpy/scipy/librosa (окружение воркера)"

SR = 48000
KIT_SR = 44100
VSCO_REPO = "sgossner/VSCO-2-CE"
VSCO_REF = "440300901dfe9275fd84e0b7763af1f8443ae62e"
VSCO_PARTS = {
    "vsco-violin": {"solo", "ens", "pizz", "spic"},
    "vsco-viola": {"ens", "pizz", "spic"},
    "vsco-cello": {"ens", "pizz", "spic"},
    "vsco-contrabass": {"sus", "pizz", "spic"},
    "vsco-harp": {"harp"},
    **{f"vsco-{k}": {"sus", "stac"}
       for k in ("flute", "oboe", "clarinet", "bassoon", "trumpet", "horn", "trombone", "tuba")},
    "vsco-mallets": {"glock", "marimba", "xylo"},
}

# мелодия ТК142: 440/494/523 Гц восьмыми (120 BPM), нота 0,22 с из 0,25; 0,5 с тишины до и после
BEAT = 0.5
EIGHTH = BEAT / 2
LEAD = 0.5
MELODY = (440.0, 494.0, 523.0) * 4   # соседи всегда разные: одиночная нота между равными — не выброс


def _fx():
    import fx_engine
    return fx_engine


def _midi_hz(m):
    return 440.0 * 2 ** ((m - 69) / 12)


# ---------- фейк хранилища наборов ----------

class FakeKitsNoNames:
    """resources по контракту движка: kit(name) → (сэмплы, sr); имён у хранилища нет."""

    def __init__(self, kits):
        self.kits = dict(kits)      # name → ([(имя, сэмпл)], sr)

    def kit(self, name):
        items, sr = self.kits[name]
        return [s for _, s in items], sr


class FakeKits(FakeKitsNoNames):
    """То же + kit_names(name) → имена без расширения в том же порядке, что kit()."""

    def kit_names(self, name):
        items, _ = self.kits[name]
        return [n for n, _ in items]


def _kit_sample(hz, sr=KIT_SR, dur=2.5, amp=0.8):
    """Сэмпл набора: синус hz, атака 2 мс, спад exp(−t/1 с)."""
    k = np.arange(int(dur * sr))
    t = k / sr
    env = np.minimum(k / (0.002 * sr), 1.0) * np.exp(-t / 1.0)
    return (amp * env * np.sin(2 * np.pi * hz * t)).astype(np.float32)


def _named_kit(notes, prefix="Test", sr=KIT_SR):
    """Набор из синусов с именами в стиле VSCO: «Test_A4_v1» → синус частоты A4."""
    items = []
    for nm, midi in notes:
        items.append((f"{prefix}_{nm}_v1", _kit_sample(_midi_hz(midi), sr=sr)))
    items.reverse()   # порядок файлов не по высоте: высоту движок берёт из имени
    return items, sr


def _line(freqs, sr=SR, harm2=0.0):
    """Дорожка: ноты-синусы восьмыми (атака 2 мс, спад exp(−t/0,4), затухание 5 мс в конце)."""
    dur = 0.22
    total = LEAD + len(freqs) * EIGHTH + 0.5
    x = np.zeros(int(total * sr))
    hits = []
    k = np.arange(int(dur * sr))
    t = k / sr
    env = np.minimum(k / (0.002 * sr), 1.0) * np.exp(-t / 0.4)
    env *= np.clip((len(k) - k) / (0.005 * sr), 0, 1)
    for i, hz in enumerate(freqs):
        p = int(round((LEAD + i * EIGHTH) * sr))
        y = 0.5 * env * (np.sin(2 * np.pi * hz * t) + harm2 * np.sin(2 * np.pi * 2 * hz * t))
        x[p:p + len(y)] += y
        hits.append(p)
    return x.astype(np.float32), hits


def _peak_hz(y, t0, t1, sr=SR, lo=60.0, hi=2500.0):
    """Частота пика спектра куска [t0, t1) в полосе [lo, hi] (Ханн, дополнение нулями ×16,
    параболическое уточнение); тишина — None."""
    x = np.asarray(y, dtype=np.float64)
    if x.ndim > 1:
        x = x.mean(axis=1)
    seg = x[int(t0 * sr):int(t1 * sr)]
    if len(seg) < 16 or float(np.abs(seg).max()) < 1e-6:
        return None
    n = 16 * len(seg)
    mag = np.abs(np.fft.rfft(seg * np.hanning(len(seg)), n))
    f = np.fft.rfftfreq(n, 1 / sr)
    band = np.flatnonzero((f >= lo) & (f <= hi))
    i = int(band[np.argmax(mag[band])])
    a, b, c = np.log(mag[i - 1] + 1e-30), np.log(mag[i] + 1e-30), np.log(mag[i + 1] + 1e-30)
    d = 0.5 * (a - c) / (a - 2 * b + c) if (a - 2 * b + c) != 0 else 0.0
    return (i + d) * sr / n


def _bass(**params):
    return {"type": "bass", "kit": params.pop("kit", "vsco-violin/solo"), **params}


# ---------- условие 111: высота сэмпла по имени ----------

@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestSampleMidiByName(unittest.TestCase):

    def test_note_token_inside_name(self):
        # таблица условия 111
        for name, midi in (("LLVln_ArcoVib_A3_f", 57), ("BKCtbss_Pizz_A#0_v3_rr1", 22),
                           ("glock_medium_C5", 72), ("Oboe_Sus_A#2_v3_Main", 46),
                           ("Test_A4_v1", 69), ("X_C4", 60)):
            with self.subTest(name=name):
                self.assertEqual(_fx().sample_midi(name), midi)

    def test_previous_forms_unchanged(self):
        for name, midi in (("A0v10", 21), ("C#4v4", 61), ("m28", 28), ("Eb3", 51)):
            with self.subTest(name=name):
                self.assertEqual(_fx().sample_midi(name), midi)

    def test_no_note_is_none(self):
        # нет ноты; нижний регистр — не нота (Karoryfer: высота по звуку, как раньше); пусто
        for name in ("Rode_Man3Open_01", "a1_f_rr1", "", "kick1", "snare-top12"):
            with self.subTest(name=name):
                self.assertIsNone(_fx().sample_midi(name))


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestKitNotesNamed(unittest.TestCase):

    def test_vsco_name_gives_pitch_from_name(self):
        # звук — A4 (440 Гц), имя — C5: высота берётся из имени, отметка named=True
        raw = [_kit_sample(440.0)]
        got = _fx()._kit_notes(raw, KIT_SR, ["LLVln_ArcoVib_C5_f"])
        self.assertEqual(len(got), 1)
        self.assertAlmostEqual(float(got[0][0]), 72.0, places=6)
        self.assertIs(bool(got[0][4]), True)

    def test_without_note_in_name_measured_by_sound(self):
        # нет ноты в имени («a1_f_rr1» — нижний регистр) → замер по звуку, named=False
        raw = [_kit_sample(110.0)]
        got = _fx()._kit_notes(raw, KIT_SR, ["a1_f_rr1"])
        self.assertEqual(len(got), 1)
        self.assertLess(abs(float(got[0][0]) - 45.0), 0.3, "высота не по звуку (A2 = 45)")
        self.assertIs(bool(got[0][4]), False)

    def test_no_names_measured_by_sound(self):
        raw = [_kit_sample(110.0)]
        got = _fx()._kit_notes(raw, KIT_SR, None)
        self.assertLess(abs(float(got[0][0]) - 45.0), 0.3)
        self.assertIs(bool(got[0][4]), False)


# ---------- условие 112: замена партии в любом регистре ----------

@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestBassAnyRegister(unittest.TestCase):

    def _run(self, x, chain, res):
        out = _fx().process(x, SR, chain, res)
        self.assertEqual(out.shape, x.shape, "длина/форма выхода ≠ входу")
        self.assertTrue(np.all(np.isfinite(out)))
        self.assertGreater(float(np.abs(out).max()), 1e-3, "выход молчит")
        return out

    def _check_notes(self, y, hits, want):
        for p, hz in zip(hits, want, strict=True):
            with self.subTest(at=round(p / SR, 3), want=hz):
                f = _peak_hz(y, p / SR + 0.03, p / SR + 0.19)
                self.assertIsNotNone(f, "в ноте входа выход молчит")
                self.assertLess(abs(f / hz - 1), 0.02, f"частота выхода {f:.1f} Гц, нужна {hz} Гц")

    def test_tc142_melody_with_wide_range(self):
        # ТК142: синус-мелодия 440/494/523, fmin 80 / fmax 1500, набор синусов с именами VSCO;
        # B4 (494) в наборе нет — сдвиг ближайшего сэмпла
        kit = _named_kit((("G4", 67), ("A4", 69), ("C5", 72), ("E5", 76)))
        res = FakeKits({"vsco-violin/solo": kit})
        x, hits = _line(MELODY)
        y = self._run(x, [_bass(fmin_hz=80, fmax_hz=1500)], res)
        self._check_notes(y, hits, MELODY)

    def test_notes_fold_into_kit_range(self):
        # набор на октаву ниже мелодии (A3…C4): ноты сворачиваются по октавам в диапазон набора
        kit = _named_kit((("A3", 57), ("B3", 59), ("C4", 60)))
        res = FakeKits({"vsco-cello/ens": kit})
        x, hits = _line(MELODY)
        y = self._run(x, [_bass(kit="vsco-cello/ens", fmin_hz=80, fmax_hz=1500)], res)
        self._check_notes(y, hits, [hz / 2 for hz in MELODY])


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestBassDefaultsUnchanged(unittest.TestCase):
    """«По умолчанию поведение прежнее»: цепочка без fmin_hz/fmax_hz и с явными 30/400 —
    один и тот же выход побайтно (на басовом входе)."""

    BASS = (55.0, 73.42, 82.41, 55.0, 82.41, 73.42, 55.0, 82.41)

    def _same(self, res, kit):
        fx = _fx()
        x, _ = _line(self.BASS, harm2=0.4)
        a = fx.process(x, SR, [_bass(kit=kit)], res)
        b = fx.process(x, SR, [_bass(kit=kit, fmin_hz=30, fmax_hz=400)], res)
        self.assertGreater(float(np.abs(a).max()), 1e-3, "выход молчит — сравнение пустое")
        self.assertTrue(np.array_equal(a, b), "умолчания ≠ явным fmin_hz 30 / fmax_hz 400")

    def test_named_kit(self):
        kit = _named_kit((("E1", 28), ("A1", 33), ("D2", 38), ("E2", 40)), prefix="Bass")
        self._same(FakeKits({"vsco-contrabass/sus": kit}), "vsco-contrabass/sus")

    def test_kit_without_names(self):
        kit = ([(None, _kit_sample(hz)) for hz in (41.2, 55.0, 73.42, 82.41)], KIT_SR)
        self._same(FakeKitsNoNames({"growlybass/bass": kit}), "growlybass/bass")


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestBassRangeParams(unittest.TestCase):

    def test_defaults(self):
        got = _fx().parse_chain([_bass()])[0]
        self.assertAlmostEqual(float(got["fmin_hz"]), 30)
        self.assertAlmostEqual(float(got["fmax_hz"]), 400)

    def test_bounds_accepted(self):
        fx = _fx()
        for fmin, fmax in ((20, 100), (1000, 2000), (80, 1500)):
            with self.subTest(fmin=fmin, fmax=fmax):
                got = fx.parse_chain([_bass(fmin_hz=fmin, fmax_hz=fmax)])[0]
                self.assertAlmostEqual(float(got["fmin_hz"]), fmin)
                self.assertAlmostEqual(float(got["fmax_hz"]), fmax)

    def test_out_of_range_is_chain_error(self):
        # ТК142: fmin_hz 10 / fmax_hz 3000 → ChainError; и прочие края за диапазоном
        fx = _fx()
        for bad in ({"fmin_hz": 10}, {"fmax_hz": 3000}, {"fmin_hz": 19}, {"fmin_hz": 1001},
                    {"fmax_hz": 99}, {"fmax_hz": 2001}, {"fmin_hz": "80"}):
            with self.subTest(**bad):
                with self.assertRaises(fx.ChainError):
                    fx.parse_chain([_bass(**bad)])

    def test_block_description(self):
        blocks = json.loads(Path(__file__).with_name("fx_blocks.json").read_text(encoding="utf-8"))
        bass = blocks["bass"]
        self.assertEqual(bass["label"]["ru"], "Замена партии (набор)")
        params = {p["id"]: p for p in bass["params"]}
        for pid, (default, lo, hi) in (("fmin_hz", (30, 20, 1000)), ("fmax_hz", (400, 100, 2000))):
            with self.subTest(param=pid):
                self.assertIn(pid, params)
                self.assertEqual((params[pid]["default"], params[pid]["min"], params[pid]["max"]),
                                 (default, lo, hi))


# ---------- условие 110: наборы VSCO на воркере ----------

try:
    import test_fx_kits as fk
    _KitBase = fk._KitCase
    _KIT_OK = fk.fa._OK
except ImportError:   # окружение без fastapi и т.п. — классы ниже пропускаются
    _KitBase, _KIT_OK = unittest.TestCase, False


@unittest.skipUnless(_KIT_OK, "нужно окружение воркера (fastapi/httpx/numpy/soundfile/librosa)")
class TestVscoCatalog(_KitBase):

    def test_tc142_vsco_kits(self):
        kits = self.w.FX_KITS
        for name, parts in VSCO_PARTS.items():
            with self.subTest(kit=name):
                self.assertIn(name, kits)
                spec = kits[name]
                self.assertEqual(spec["repo"], VSCO_REPO)
                self.assertRegex(spec["ref"], r"^[0-9a-f]{40}$", "версия набора не закреплена коммитом")
                self.assertEqual(spec["ref"], VSCO_REF)
                self.assertEqual(set(spec["parts"]), parts)
                self.assertAlmostEqual(float(spec["max_s"]), 8.0)
                for part, (folder, pattern, *shift) in spec["parts"].items():
                    # поправка октавы (замер): скрипка соло и арфа 0, остальные +12
                    want = 0 if folder in ("Strings/Solo Violin/Arco Vib", "Strings/Harp") else 12
                    self.assertEqual(shift, [want], part)
                    self.assertIsInstance(folder, str, part)
                    self.assertTrue(folder, part)
                    re.compile(pattern)

    def test_tc142_listing_url_encodes_spaces(self):
        # каталог с пробелами: в URL запроса GitHub — %20, файл скачивается и кладётся в часть
        calls = []
        files = {"https://raw.githubusercontent.com/x/LLVln_ArcoVib_A3_f.wav": fk._wav(0.5)}

        def fake_get(url, timeout=60):
            calls.append(url)
            if "api.github.com" in url:
                return json.dumps([{"name": "LLVln_ArcoVib_A3_f.wav", "type": "file",
                                    "download_url": next(iter(files))}]).encode()
            return files[url]

        spec = {"repo": VSCO_REPO, "ref": VSCO_REF,
                "parts": {"solo": ("Strings/Solo Violin/Arco Vib", r".+\.wav")}, "max_s": 8.0}
        with mock.patch.object(self.w, "_http_get", fake_get):
            got = self.w._fx_kit_install("vsco-test", spec)
        self.assertEqual(got["parts"], {"solo": 1})
        listing = calls[0]
        self.assertNotIn(" ", listing, "пробел в URL запроса")
        self.assertIn("Strings/Solo%20Violin/Arco%20Vib", listing)
        self.assertIn(VSCO_REPO, listing)
        self.assertIn(VSCO_REF, listing)
        self.assertTrue((self.kits / "vsco-test" / "solo" / "LLVln_ArcoVib_A3_f.wav").is_file())

    def test_every_vsco_part_requested_encoded(self):
        # условие 115 (заменило запрос списка через API): у всех частей VSCO список закреплён, файлы качаются прямыми
        # ссылками raw.githubusercontent.com с экранированным путём каталога; к API GitHub обращений нет
        for name in VSCO_PARTS:
            spec = self.w.FX_KITS.get(name)
            if spec is None:
                self.fail(f"нет набора {name}")
            for part, (folder, pattern, *_shift) in spec["parts"].items():
                with self.subTest(kit=name, part=part):
                    calls = []

                    def fake_get(url, timeout=60, calls=calls):
                        calls.append(url)
                        return b"RIFF"

                    one = {**spec, "parts": {part: (folder, pattern, *_shift)}}
                    with mock.patch.object(self.w, "_http_get", fake_get):
                        self.w._fx_kit_install(name, one)
                    self.assertTrue(calls, "файлы не качались")
                    for url in calls:
                        self.assertNotIn("api.github.com", url)
                        self.assertNotIn(" ", url)
                        base = f"raw.githubusercontent.com/{spec['repo']}/{spec['ref']}/{urllib.parse.quote(folder)}/"
                        self.assertIn(base, url)

if __name__ == "__main__":
    unittest.main()


class TestVscoInstallRenamesToMidi(unittest.TestCase):
    """Условие 110 (уточнено): часть с поправкой октавы ставится под точной высотой m<MIDI>.wav."""

    def test_shift_renames(self):
        import tempfile
        from pathlib import Path
        from unittest import mock
        import yue_worker as w
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(w, "_kits_dir", return_value=Path(tmp)):
            listing = [{"name": "X_A3_v1.wav", "download_url": "u1"}, {"name": "X_C#4_v1.wav", "download_url": "u2"}]

            def fake_get(url, timeout=60):
                return json.dumps(listing).encode() if "api.github.com" in url else b"RIFF"
            spec = {"repo": "o/r", "ref": "0" * 40, "parts": {"p": ("A B", r"X_.*\.wav", 12)}}
            with mock.patch.object(w, "_http_get", side_effect=fake_get):
                r = w._fx_kit_install("t", spec)
            self.assertEqual(r["parts"], {"p": 2})
            self.assertEqual(sorted(f.name for f in (Path(tmp) / "t" / "p").iterdir()), ["m69.wav", "m73.wav"])
