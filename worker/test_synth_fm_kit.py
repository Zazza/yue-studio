"""Тесты карточки internal-own-track, этап 7в: условия 61 и 62 (тест-кейсы ТК95 и ТК96) — FM-генератор
и сэмплы набора в блоке synth.

Контракт (из карточки):
- условие 61: osc1/osc2 принимают 0…5; 5 — «FM»: sin(2π f t + I(t)·sin(2π·r·f·t)),
  I(t) = fm_index·e^(−t/fm_decay_s) от начала ноты; fm_ratio 0,5…14 (1), fm_index 0…10 (2),
  fm_decay_s 0,01…5 (0,5); fm_index 0 — чистый синус;
- ТК95: fm_index 0 → у ноты A4 энергия вне 440 ±5 Гц ≤ −40 дБ; fm_index 3, fm_ratio 1, fm_decay 5 →
  гармоники 880 и 1320 (каждая ≥ −30 дБ от основной) в первые 0,2 с; fm_decay 0,05 → к 1 с гармоники
  ≤ −40 дБ от основной; fm_ratio 3,5 → составляющие на 440 ± 1540 Гц (1980 есть); osc1 6 → ChainError;
- условие 62: строковый kit (умолчание «») — ноты играются сэмплами набора, генераторы/суб/шум не звучат;
  высота сэмпла — из имени («A0v10» → MIDI 21, «C#4…», «D#1…»); нота — ближайшим сэмплом со сдвигом
  высоты пересчётом частоты; vel — громкость; набора нет → ChainError с понятным текстом;
- ТК96: набор из синусов «A3v10» (220 Гц), «C4v10» (261,6), «D#4v10» (311,1): MIDI 57 → 220 ±1 Гц;
  MIDI 59 → 246,9 ±1,5 Гц; vel 0,4 тише vel 0,8 на 6 ±0,5 дБ; kit «нет/такого» → ChainError;
  имя без ноты («foo») пропускается; длина выхода = входу.

Внешняя граница — хранилище наборов: подставное (kit(name) → (сэмплы, sr), kit_names(name) → имена без
расширения в том же порядке; нет — KeyError, как у настоящего). Вход — тишина: громкость партии тогда
по пику (−6 дБФS у всей партии), и ноты одной партии сравнимы между собой.

Предположения (карточка не уточняет):
- нота дальше ±6 полутонов от всех сэмплов — проверяется только, что она звучит (высота не задана однозначно);
- подставное хранилище отдаёт сэмплы в порядке, отличном от высоты: пара «сэмпл ↔ имя» — по индексу.

Запуск: cd worker && python3 -m unittest test_synth_fm_kit -v
"""
import unittest
from unittest import mock

try:
    import test_fx_api as _fa
    _API_OK = _fa._OK
except ImportError:   # нет fastapi/httpx — тесты /fx пропускаются
    _fa, _API_OK = None, False

try:
    import numpy as np
    import scipy  # noqa: F401
    import fx_engine as fx
    _HAS_DEPS = True
except ImportError:   # окружение без numpy/scipy (как make test на ПК) — тесты пропускаются
    _HAS_DEPS = False

_SKIP = "нужны numpy/scipy (окружение воркера)"

SR = 48000
A3, B3, C4, DS4, A4 = 57, 59, 60, 63, 69


def _hz(midi):
    return 440.0 * 2 ** ((midi - 69) / 12)


class FakeKits:
    """Хранилище наборов по контракту: kit(name) → (список сэмплов, sr), kit_names(name) → имена без
    расширения в том же порядке; нет набора — KeyError."""

    def __init__(self, kits=None):
        self.kits = dict(kits or {})   # имя → (список (имя, сэмпл), sr)
        self.calls = []

    def kit(self, name):
        self.calls.append(name)
        items, sr = self.kits[name]
        return [s for _, s in items], sr

    def kit_names(self, name):
        items, _ = self.kits[name]
        return [n for n, _ in items]


def _sine(hz, sr=SR, seconds=3.0, amp=0.5, harm2=0.0):
    t = np.arange(int(seconds * sr)) / sr
    x = amp * (np.sin(2 * np.pi * hz * t) + harm2 * np.sin(2 * np.pi * 2 * hz * t))
    fade = int(0.003 * sr)
    x[:fade] *= np.linspace(0, 1, fade)
    return x.astype(np.float32)


def _sine_kit(sr=SR, extra=(), c4_harm2=0.0):
    """Набор из ТК96 (порядок не по высоте); extra — дополнительные (имя, сэмпл)."""
    items = [("D#4v10", _sine(_hz(DS4), sr)), ("A3v10", _sine(_hz(A3), sr)),
             ("C4v10", _sine(_hz(C4), sr, harm2=c4_harm2))] + list(extra)
    return items, sr


def _run(block, seconds=3.0, resources=None):
    x = np.zeros(int(seconds * SR), dtype=np.float32)
    y = fx.process(x, SR, [dict({"type": "synth"}, **block)], resources)
    return x, np.asarray(y, dtype=np.float64)


def _spec(seg, sr=SR, pad=8):
    w = np.hanning(len(seg))
    nfft = 1 << int(np.ceil(np.log2(len(seg) * pad)))
    s = np.abs(np.fft.rfft(seg * w, n=nfft))
    return np.fft.rfftfreq(nfft, 1 / sr), s


def _band_peak(f, s, hz, half=8.0):
    m = (f >= hz - half) & (f <= hz + half)
    return float(s[m].max())


def _db(a, b):
    return 20 * np.log10(max(a, 1e-30) / max(b, 1e-30))


def _f0(seg, lo=60.0, hi=1500.0, sr=SR):
    """Частота самого сильного пика спектра (параболическое уточнение)."""
    f, s = _spec(seg, sr)
    m = np.where((f >= lo) & (f <= hi))[0]
    k = int(m[np.argmax(s[m])])
    a, b, c = np.log(s[k - 1] + 1e-30), np.log(s[k] + 1e-30), np.log(s[k + 1] + 1e-30)
    d = 0.5 * (a - c) / (a - 2 * b + c)
    return float(f[k] + d * (f[1] - f[0]))


def _seg(y, t0, t1):
    return y[int(t0 * SR):int(t1 * SR)]


def _rms(x):
    return float(np.sqrt(np.mean(np.square(x)))) if len(x) else 0.0


# ---------- ТК95: FM-генератор (условие 61) ----------

def _fm(**kw):
    """Нота A4 FM-генератором: ровная огибающая, фильтр открыт — спектр задаёт только генератор."""
    blk = {"osc1": 5, "cutoff_hz": 16000, "attack_s": 0.005, "decay_s": 0.01, "sustain": 1.0,
           "release_s": 0.05, "notes": [{"t": 0.0, "d": 1.8, "midi": [A4], "vel": 0.8}]}
    blk.update(kw)
    return _run(blk, seconds=2.0)[1]


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestFmParams(unittest.TestCase):

    def test_osc_range_0_to_5(self):
        for k in ("osc1", "osc2"):
            with self.subTest(param=k):
                fx.parse_chain([{"type": "synth", k: 5}])
                with self.assertRaises(fx.ChainError):
                    fx.parse_chain([{"type": "synth", k: 6}])

    def test_fm_defaults(self):
        b = fx.parse_chain([{"type": "synth", "osc1": 5}])[0]
        self.assertEqual(b["fm_ratio"], 1.0)
        self.assertEqual(b["fm_index"], 2.0)
        self.assertEqual(b["fm_decay_s"], 0.5)

    def test_fm_ranges(self):
        for k, ok, bad in (("fm_ratio", (0.5, 14), (0.4, 14.5)), ("fm_index", (0, 10), (-0.1, 10.5)),
                           ("fm_decay_s", (0.01, 5), (0.005, 5.5))):
            for v in ok:
                with self.subTest(param=k, value=v):
                    fx.parse_chain([{"type": "synth", "osc1": 5, k: v}])
            for v in bad:
                with self.subTest(param=k, value=v), self.assertRaises(fx.ChainError):
                    fx.parse_chain([{"type": "synth", "osc1": 5, k: v}])


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestFmSound(unittest.TestCase):

    def test_tc95_index0_pure_sine(self):
        y = _fm(fm_index=0)
        f, s = _spec(_seg(y, 0.3, 1.5))
        p = s ** 2
        near = (f >= 435) & (f <= 445)
        self.assertGreater(p[near].sum(), 0, "нота не звучит")
        self.assertLessEqual(10 * np.log10(p[~near].sum() / p[near].sum()), -40,
                             "fm_index 0 — не чистый синус")

    def test_tc95_harmonics_early(self):
        y = _fm(fm_index=3, fm_ratio=1, fm_decay_s=5)
        f, s = _spec(_seg(y, 0.01, 0.2))
        base = _band_peak(f, s, 440)
        for h in (880, 1320):
            with self.subTest(harmonic=h):
                self.assertGreaterEqual(_db(_band_peak(f, s, h), base), -30,
                                        f"нет гармоники {h} Гц в первые 0,2 с")

    def test_tc95_index_decays(self):
        y = _fm(fm_index=3, fm_ratio=1, fm_decay_s=0.05)
        f, s = _spec(_seg(y, 1.0, 1.6))
        base = _band_peak(f, s, 440)
        self.assertGreater(base, 0)
        for h in (880, 1320):
            with self.subTest(harmonic=h):
                self.assertLessEqual(_db(_band_peak(f, s, h), base), -40,
                                     f"гармоника {h} Гц не погасла к 1 с (fm_decay 0,05)")

    def test_tc95_ratio_3_5_sidebands(self):
        y = _fm(fm_index=3, fm_ratio=3.5, fm_decay_s=5)
        f, s = _spec(_seg(y, 0.01, 0.3))
        top = float(s[(f >= 50) & (f <= 6000)].max())
        for hz in (1980, 1100):   # 440 + 1540 и |440 − 1540|
            with self.subTest(component=hz):
                self.assertGreaterEqual(_db(_band_peak(f, s, hz), top), -30, f"нет составляющей {hz} Гц")
        # гармоники 440 (кратные) при r = 3,5 не появляются: 880 Гц в FM-спектре нет
        self.assertLessEqual(_db(_band_peak(f, s, 880), top), -40, "при fm_ratio 3,5 есть 880 Гц")


# ---------- ТК96: сэмплы набора в synth (условие 62) ----------

def _kit_block(notes, kit="keys/sines", **kw):
    blk = {"kit": kit, "attack_s": 0.005, "decay_s": 0.01, "sustain": 1.0, "release_s": 0.05, "notes": notes}
    blk.update(kw)
    return blk


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestSynthKit(unittest.TestCase):

    def _res(self, **kw):
        return FakeKits({"keys/sines": _sine_kit(**kw)})

    def test_kit_param_default_empty(self):
        self.assertEqual(fx.parse_chain([{"type": "synth"}])[0].get("kit"), "")

    def test_tc96_exact_sample_pitch(self):
        res = self._res()
        _, y = _run(_kit_block([{"t": 0.1, "d": 2.0, "midi": [A3], "vel": 0.8}]), resources=res)
        self.assertIn("keys/sines", res.calls, "набор не прочитан")
        self.assertAlmostEqual(_f0(_seg(y, 0.5, 1.8)), 220.0, delta=1.0)

    def test_tc96_shifted_nearest_pitch(self):
        _, y = _run(_kit_block([{"t": 0.1, "d": 2.0, "midi": [B3], "vel": 0.8}]), resources=self._res())
        self.assertAlmostEqual(_f0(_seg(y, 0.5, 1.8)), 246.9, delta=1.5)

    def test_nearest_sample_is_used(self):
        # B3 (59): ближе C4 (60, сэмпл со 2-й гармоникой), чем A3 (57, чистый синус) — звучит C4 вниз на полутон
        _, y = _run(_kit_block([{"t": 0.1, "d": 2.0, "midi": [B3], "vel": 0.8}]),
                    resources=self._res(c4_harm2=0.5))
        f, s = _spec(_seg(y, 0.5, 1.8))
        self.assertGreaterEqual(_db(_band_peak(f, s, 2 * 246.9), _band_peak(f, s, 246.9)), -12,
                                "нота сыграна не ближайшим сэмплом (C4)")

    def test_kit_sample_rate_respected(self):
        # набор в 44,1 кГц, выход 48 кГц — высота та же
        res = FakeKits({"keys/sines": _sine_kit(sr=44100)})
        _, y = _run(_kit_block([{"t": 0.1, "d": 2.0, "midi": [A3], "vel": 0.8}]), resources=res)
        self.assertAlmostEqual(_f0(_seg(y, 0.5, 1.8)), 220.0, delta=1.0)

    def test_tc96_velocity_is_volume(self):
        notes = [{"t": 0.1, "d": 0.8, "midi": [A3], "vel": 0.8},
                 {"t": 1.5, "d": 0.8, "midi": [A3], "vel": 0.4}]
        _, y = _run(_kit_block(notes, rel_db=-6), resources=self._res())
        loud, soft = _rms(_seg(y, 0.4, 0.8)), _rms(_seg(y, 1.8, 2.2))
        self.assertGreater(soft, 0, "тихая нота не звучит")
        self.assertAlmostEqual(_db(loud, soft), 6.0, delta=0.5)

    def test_generators_silent_with_kit(self):
        # sub (110 Гц квадрат), шум и пила — не звучат: вне 220 ±5 Гц почти ничего
        _, y = _run(_kit_block([{"t": 0.1, "d": 2.0, "midi": [A3], "vel": 0.8}], osc1=0, sub=1.0, noise=1.0),
                    resources=self._res())
        f, s = _spec(_seg(y, 0.5, 1.8))
        p = s ** 2
        near = (f >= 215) & (f <= 225)
        self.assertLessEqual(10 * np.log10(p[~near].sum() / p[near].sum()), -30,
                             "с kit звучат генераторы/суб/шум")

    def test_tc96_missing_kit_chain_error(self):
        with self.assertRaises(fx.ChainError) as e:
            _run(_kit_block([{"t": 0.1, "d": 1.0, "midi": [A3], "vel": 0.8}], kit="нет/такого"),
                 resources=self._res())
        self.assertIn("нет/такого", str(e.exception), "текст ошибки не называет набор")

    def test_tc96_name_without_note_skipped(self):
        # «foo» — без высоты в имени: не играется (иначе 1 кГц был бы слышен), остальные работают
        res = FakeKits({"keys/sines": _sine_kit(extra=[("foo", _sine(1000.0))])})
        _, y = _run(_kit_block([{"t": 0.1, "d": 2.0, "midi": [A3], "vel": 0.8}]), resources=res)
        seg = _seg(y, 0.5, 1.8)
        self.assertAlmostEqual(_f0(seg, hi=3000), 220.0, delta=1.0)
        f, s = _spec(seg)
        self.assertLessEqual(_db(_band_peak(f, s, 1000), _band_peak(f, s, 220)), -40, "звучит сэмпл foo")

    def test_note_names_with_sharps(self):
        # «D#4v10» — MIDI 63: нота D#4 звучит этим сэмплом без сдвига
        _, y = _run(_kit_block([{"t": 0.1, "d": 2.0, "midi": [DS4], "vel": 0.8}]), resources=self._res())
        self.assertAlmostEqual(_f0(_seg(y, 0.5, 1.8)), _hz(DS4), delta=1.5)

    def test_far_note_still_sounds(self):
        # дальше ±6 полутонов от всех сэмплов — всё равно ближайший: нота не молчит
        _, y = _run(_kit_block([{"t": 0.1, "d": 1.5, "midi": [A4 + 12], "vel": 0.8}]), resources=self._res())
        self.assertGreater(_rms(_seg(y, 0.4, 1.4)), 1e-3, "нота дальше ±6 полутонов не звучит")

    def test_tc96_output_length_equals_input(self):
        res = self._res()
        for seconds in (0.5, 2.3):
            with self.subTest(seconds=seconds):
                notes = [{"t": seconds - 0.2, "d": 1.0, "midi": [C4], "vel": 0.8}]
                x, y = _run(_kit_block(notes, release_s=2.0), seconds=seconds, resources=res)
                self.assertEqual(y.shape, x.shape)


# ---------- ТК96а: превью окна = тот же кусок полного рендера (условие 62а) ----------
#
# Контракт: synth с kit или с osc1 5 (FM) — сэмпл и спад FM-глубины идут от начала ноты, поэтому превью окна
# [3, 5) партии (source mix, add true, ноты в секундах трека) = отрезок [3, 5) полного рендера (превью [0, DUR))
# ±0,5 дБ по RMS и ±1e-3 по форме. Прочие синты (osc1 0 без kit) — как раньше; прежнее поведение (перезапуск
# ноты за lead до окна) по форме не задано, проверяется только уровень (±0,5 дБ — было и раньше).
# Сравнение — output solo: в mix одинаковый в обоих файлах трек маскировал бы разницу синта.
# Набор — настоящее хранилище воркера (файлы в каталоге данных), внешних границ нет.

_API_SKIP = "нужны fastapi/httpx/numpy/scipy/soundfile/librosa (окружение воркера)"
WIN_DUR = 8.0
W0, W1 = 3.0, 5.0
DECAY_TAU = 1.5
_API_BASE = _fa._FxApiCase if _API_OK else unittest.TestCase


@unittest.skipUnless(_API_OK, _API_SKIP)
class TestSynthPreviewEqualsRender(_API_BASE):
    """ТК96а: нота t 0, d 6; превью окна [3, 5) = [3, 5) полного рендера."""

    def setUp(self):
        super().setUp()
        p = mock.patch.object(self.w, "fx_resources", self.real_fx_resources)
        p.start()
        self.addCleanup(p.stop)
        self._put_decay_kit()

    def _put_decay_kit(self):
        """Подставной набор «test/decay»: затухающий синус A3 (220 Гц), τ 1,5 с, 8 с."""
        import soundfile as sf
        sr = 48000
        t = np.arange(int(WIN_DUR * sr)) / sr
        x = 0.8 * np.exp(-t / DECAY_TAU) * np.sin(2 * np.pi * _hz(A3) * t)
        path = self.data / "fx" / "kits" / "test" / "decay" / "A3v10.wav"
        path.parent.mkdir(parents=True, exist_ok=True)
        sf.write(str(path), x.astype(np.float32), sr, subtype="FLOAT")

    def _track_job(self):
        import soundfile as sf
        jid = self._job(duration=WIN_DUR, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        sf.write(str(d / "audio.flac"), _fa._tone(100, amp=0.3, dur=WIN_DUR).astype(np.float32), _fa.SR)
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid, d

    def _solo(self, jid, d, block, fr, to):
        chain = [dict({"type": "synth", "attack_s": 0.005, "decay_s": 0.01, "sustain": 1.0, "release_s": 0.05,
                       "cutoff_hz": 16000, "notes": [{"t": 0.0, "d": 6.0, "midi": [A3], "vel": 0.8}]}, **block)]
        r = self._fx(jid, source="mix", output="solo", preview=True, add=True, chain=chain,
                     **{"from": fr, "to": to})
        self.assertEqual(r.status_code, 200, r.text[:300])
        y, sr = _fa._read(d / r.json()["file"])
        self.assertEqual(sr, _fa.SR)
        return y

    def _pair(self, block):
        jid, d = self._track_job()
        full = self._solo(jid, d, block, 0.0, WIN_DUR)
        win = self._solo(jid, d, block, W0, W1)
        a, b = int(round(W0 * _fa.SR)), int(round(W1 * _fa.SR))
        want = full[a:b]
        got = win[:b - a]
        self.assertEqual(got.shape, want.shape, "превью короче окна")
        self.assertGreater(_rms(want), 1e-4, "в полном рендере нота к 3 с не звучит — тест ничего не проверяет")
        return got, want

    def _assert_same(self, block):
        got, want = self._pair(block)
        self.assertAlmostEqual(_db(_rms(got), _rms(want)), 0.0, delta=0.5,
                               msg="громкость превью окна не как у того же куска полного рендера")
        # края окна: синт плавно входит/выходит за 10 мс (SYNTH_EDGE_S — без щелчка на краю), форма — вне краёв
        e = int(0.01 * _fa.SR) + 1
        np.testing.assert_allclose(got[e:-e], want[e:-e], rtol=0, atol=1e-3,
                                   err_msg="форма превью окна не как у того же куска полного рендера")

    def test_tc96a_kit_preview_equals_render(self):
        self._assert_same({"kit": "test/decay"})

    def test_tc96a_fm_preview_equals_render(self):
        self._assert_same({"osc1": 5, "fm_index": 3, "fm_ratio": 1, "fm_decay_s": 0.6})

    def test_tc96a_plain_synth_level_unchanged(self):
        # osc1 0 без kit — как раньше: форма не задана (перезапуск ноты за lead), уровень совпадает
        got, want = self._pair({"osc1": 0})
        self.assertAlmostEqual(_db(_rms(got), _rms(want)), 0.0, delta=0.5)



# ---------- Кросс-ревью s7c r1: условия 62б, 62в (ТК96б, ТК96в) ----------
#
# Контракт (из карточки):
# - 62б: начальная фаза генераторов ноты (и унисона) — из сида по (начало ноты в треке, мс; MIDI; голос унисона),
#   а не по порядку нот в окне: превью окна, где отброшены ранние ноты, звучит той же формой, что трек;
# - ТК96б: FM osc1 5 fm_index 3 fm_decay 0,6, ноты A3 {t 0, d 0,5} и {t 1, d 6}: превью окна [3, 5) = отрезок
#   полного рендера ±1e-3 по форме вне краёв окна (10 мс); обычный синт osc1 0 с теми же нотами — уровень ±0,5 дБ;
# - 62в: для kit/FM начало ноты переносится к окну не дальше чем на 60 с: нота 700 с, окно [610, 612) → 200, не 422;
# - ТК96в: FM-нота {t 0, d 700}: превью окна [610, 612) → 200, выход ненулевой.
# Обвязка — та же, что у ТК96а (output solo, настоящее хранилище наборов).
#
# Предположение (карточка не уточняет): kit с нотой 700 с в окне [610, 612) — тоже 200 (62в говорит «kit/FM»);
# звучание не проверяется — сэмпл дольше 8 с не звучит, тишина там допустима.

TWO_NOTES = [{"t": 0.0, "d": 0.5, "midi": [A3], "vel": 0.8}, {"t": 1.0, "d": 6.0, "midi": [A3], "vel": 0.8}]
FM_B = {"osc1": 5, "fm_index": 3, "fm_ratio": 1, "fm_decay_s": 0.6}


def _real_kits_setup(case):
    """Как setUp у TestSynthPreviewEqualsRender: настоящее хранилище наборов + набор «test/decay»."""
    _API_BASE.setUp(case)
    p = mock.patch.object(case.w, "fx_resources", case.real_fx_resources)
    p.start()
    case.addCleanup(p.stop)
    TestSynthPreviewEqualsRender._put_decay_kit(case)


@unittest.skipUnless(_API_OK, _API_SKIP)
class TestSynthPhaseFromTrackPosition(_API_BASE):
    """ТК96б: ранняя нота вне окна отброшена — поздняя нота в превью звучит той же формой, что в треке."""

    setUp = _real_kits_setup
    _track_job = TestSynthPreviewEqualsRender._track_job
    _solo = TestSynthPreviewEqualsRender._solo
    _pair = TestSynthPreviewEqualsRender._pair
    _assert_same = TestSynthPreviewEqualsRender._assert_same

    def test_tc96b_fm_two_notes_form_equals_render(self):
        self._assert_same(dict(FM_B, notes=TWO_NOTES))

    def test_tc96b_fm_unison_two_notes_form_equals_render(self):
        # фаза каждого голоса унисона — тоже по месту ноты в треке
        self._assert_same(dict(FM_B, notes=TWO_NOTES, unison=3, detune_cents=12))

    def test_tc96b_plain_synth_two_notes_level_unchanged(self):
        got, want = self._pair({"osc1": 0, "notes": TWO_NOTES})
        self.assertAlmostEqual(_db(_rms(got), _rms(want)), 0.0, delta=0.5)


@unittest.skipUnless(_API_OK, _API_SKIP)
class TestSynthLongNoteFarWindow(_API_BASE):
    """ТК96в: нота 700 с, окно [610, 612) — кусок окна принимается цепочкой (не 422) и звучит (FM); для kit — принят.

    Без 10-минутного трека через /fx: полный трек считал бы синт на всю длину в памяти (гигабайты — 2026-10-09
    прогон этого теста совпал с зависанием машины). Проверяется ровно то, что давало 422: подготовка окна воркером
    (synth_window) и разбор/обработка куска движком."""

    setUp = _real_kits_setup

    def _window(self, block):
        import fx_engine
        chain = [dict({"type": "synth", "attack_s": 0.005, "decay_s": 0.01, "sustain": 1.0, "release_s": 0.05,
                       "cutoff_hz": 16000, "notes": [{"t": 0.0, "d": 700.0, "midi": [A3], "vel": 0.8}]}, **block)]
        win = self.w.synth_window(fx_engine.parse_chain(chain), 610.0, 2.0)
        x = np.zeros((int(2.0 * _fa.SR), 2), dtype=np.float32)
        res = self.w.fx_resources()
        try:
            return np.asarray(fx_engine.process(x, _fa.SR, win, res), dtype=np.float64)
        finally:
            res.close()

    def test_tc96v_fm_long_note_far_window_ok_and_sounds(self):
        y = self._window(FM_B)
        self.assertGreater(_rms(y), 1e-4, "длинная FM-нота в далёком окне молчит")

    def test_tc96v_kit_long_note_far_window_ok(self):
        self._window({"kit": "test/decay"})


# ---------- Кросс-ревью s7c r1: условие 63а (ТК96г) ----------
#
# Контракт: последние 50 мс сэмпла набора synth — спад к нулю (без щелчка там, где сэмпл кончается);
# ТК96г: хранилище отдаёт сэмпл 2 с синуса постоянной громкости (как обрезанный по max_s), нота 4 с →
# 1,95…2,0 с от начала ноты гаснут к 0 (|y| в конце сэмпла < 1 % пика), после — тишина.
# Сэмпл — косинус с максимумом ровно в последнем отсчёте: без спада там был бы пик, а не случайный ноль.
#
# Предположения (карточка не уточняет форму спада):
# - «последние 50 мс» — до 1,95 с сэмпл звучит как был: уровень [1,8; 1,9) с — в пределах 1 дБ от [0,5; 1,5);
# - «последний отсчёт» проверяется по последним 10 отсчётам (0,2 мс): допускает сдвиг сэмпла на пару отсчётов.

SAMPLE_S = 2.0
NOTE_T = 0.1


def _const_cos_kit(sr=SR):
    n = int(SAMPLE_S * sr)
    t = (np.arange(n) - (n - 1)) / sr   # 0 в последнем отсчёте
    x = 0.5 * np.cos(2 * np.pi * _hz(A3) * t)
    return FakeKits({"keys/const": ([("A3v10", x.astype(np.float32))], sr)})


@unittest.skipUnless(_HAS_DEPS, _SKIP)
class TestSynthKitSampleEndFades(unittest.TestCase):

    def _render(self):
        blk = _kit_block([{"t": NOTE_T, "d": 4.0, "midi": [A3], "vel": 0.8}], kit="keys/const", cutoff_hz=16000)
        return _run(blk, seconds=5.0, resources=_const_cos_kit())[1]

    def test_tc96g_sample_end_fades_to_zero(self):
        y = self._render()
        peak = float(np.max(np.abs(_seg(y, NOTE_T + 0.5, NOTE_T + 1.5))))
        self.assertGreater(peak, 1e-3, "нота не звучит")
        end = int(round((NOTE_T + SAMPLE_S) * SR))
        tail = np.abs(y[end - 10:end])
        self.assertLess(float(tail.max()), 0.01 * peak, "сэмпл обрывается без спада (щелчок в конце сэмпла)")

    def test_tc96g_fade_is_only_last_50ms(self):
        y = self._render()
        body = _rms(_seg(y, NOTE_T + 0.5, NOTE_T + 1.5))
        before = _rms(_seg(y, NOTE_T + 1.8, NOTE_T + 1.9))
        self.assertAlmostEqual(_db(before, body), 0.0, delta=1.0, msg="спад начался раньше последних 50 мс")
        fading = _rms(_seg(y, NOTE_T + 1.95, NOTE_T + SAMPLE_S))
        self.assertLess(fading, body * 0.9, "на 1,95…2,0 с нет спада")

    def test_tc96g_silence_after_sample(self):
        y = self._render()
        peak = float(np.max(np.abs(_seg(y, NOTE_T + 0.5, NOTE_T + 1.5))))
        after = _seg(y, NOTE_T + SAMPLE_S + 0.001, NOTE_T + 4.0)
        self.assertLess(float(np.max(np.abs(after))), 1e-3 * peak, "после конца сэмпла не тишина")



if __name__ == "__main__":
    unittest.main()
