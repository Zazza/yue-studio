"""Тесты карточки internal-own-track: этап 13 (условия 94, 95 — флаг clipped в ответе POST /fx/phrase,
в том числе при повторе из кэша) и этап 13г (условие 107 — басовая фраза сэмплами набора).

Контракт (из карточки):
- условие 94: пик > 0,99 — весь круг ниже до 0,99, clipped=true в ответе, если пришлось;
- условие 95: POST /fx/phrase → {file, cycle_sec, clipped}; кэш: повтор — тот же файл без пересчёта.
  Повтор из кэша обязан отдавать тот же ответ, что и первый расчёт, — clipped тоже;
- условие 107: фраза баса — сэмплами набора phrases.BASS_KIT (ближайший по высоте сэмпл, сдвиг
  пересэмплированием), если набор есть у хранилища; без него (res=None или набора нет) — синт-бас как раньше.

Внешняя граница — хранилище наборов: подставное, kit(name) → (список моно-сэмплов, sr),
kit_names(name) → имена без расширения в том же порядке (формат m<MIDI>, docs/effects.md); нет — KeyError.
Сэмплы — чистые синусы на частоте из имени, частота набора 44100 при расчёте в 48000: если частота
набора не учтена, высота уедет на 8,8 % (больше допуска 3 %).

Предположения (карточка не уточняет):
- высота нот bass-long — из данных модуля phrases.BASS (E1, C1, G1, D1 по такту);
- цепочка «gain +24 дБ на kick» фразы drums-rock упирается в пик (проверяется первым ответом — clipped=true);
- цепочка «gain −6 дБ» на всех частях не упирается: выравнивание возвращает громкость сухого,
  а сухое — с запасом по пику.

Запуск: cd worker && python3 -m unittest test_phrase_more -v
"""
import unittest

import test_fx_api as fa

try:
    import numpy as np
    import scipy  # noqa: F401
    _HAS_NP = True
except ImportError:
    _HAS_NP = False

_SKIP_NP = "нужны numpy/scipy (окружение воркера)"

# Упор в пик: kick +24 дБ над остальными частями — выравнивание по A-RMS сведения опускает всё,
# но пик бочки к общему уровню слишком велик (замер: clipped=true). Подъём 40 Гц на басе и drive +40 дБ
# на всех частях бита в пик не упираются (выравнивание громкости его опускает).
CLIP = {"phrase": "drums-rock", "chain": [{"type": "gain", "gain_db": 24}], "stems": ["kick"]}
SAFE = {"phrase": "drums-rock", "chain": [{"type": "gain", "gain_db": -6}], "stems": []}
BASS_PHRASE = "bass-long"

SR = 48000
KIT_SR = 44100
KIT_MIDI = (40, 43, 47)


def _hz(midi):
    return 440.0 * 2 ** ((midi - 69) / 12)


# ---------- условия 94, 95: clipped в ответе и при повторе из кэша ----------

@unittest.skipUnless(fa._OK, fa._SKIP)
class TestPhraseClippedFlag(fa._FxApiCase):

    def _ok(self, **body):
        r = self.client.post("/fx/phrase", json=body)
        self.assertEqual(r.status_code, 200, r.text)
        out = r.json()
        self.assertIn("clipped", out)
        self.assertIsInstance(out["clipped"], bool)
        return out

    def test_clipped_true_and_kept_on_cache_repeat(self):
        a = self._ok(**CLIP)
        self.assertIs(a["clipped"], True, "kick +24 дБ не упёрся в пик — подобрать другую цепочку")
        b = self._ok(**CLIP)
        self.assertEqual(a["file"], b["file"], "повтор не из кэша")
        self.assertIs(b["clipped"], True, "повтор из кэша потерял clipped")

    def test_not_clipped_false_and_kept_on_cache_repeat(self):
        a = self._ok(**SAFE)
        self.assertIs(a["clipped"], False)
        b = self._ok(**SAFE)
        self.assertEqual(a["file"], b["file"], "повтор не из кэша")
        self.assertIs(b["clipped"], False)

    def test_flags_independent_per_settings(self):
        """Кэш хранит флаг своего расчёта: чередование настроек не путает clipped."""
        clip = self._ok(**CLIP)
        safe = self._ok(**SAFE)
        self.assertNotEqual(clip["file"], safe["file"])
        for _ in range(2):
            self.assertIs(self._ok(**CLIP)["clipped"], True)
            self.assertIs(self._ok(**SAFE)["clipped"], False)

    def test_clipped_file_peak_within_limit(self):
        """Ужатый круг в файле — пик не выше 0,99 (с запасом на 16 бит)."""
        import io

        import soundfile as sf
        out = self._ok(**CLIP)
        r = self.client.get(f"/fx/phrase/files/{out['file']}")
        self.assertEqual(r.status_code, 200, r.text)
        y, _ = sf.read(io.BytesIO(r.content), always_2d=True)
        self.assertLessEqual(float(np.abs(y).max()), 0.99 + 1e-3)


# ---------- условие 107: бас сэмплами набора ----------

class FakeKits:
    """Хранилище наборов по контракту: kit(name) → (список сэмплов, sr), kit_names(name) → имена;
    нет набора — KeyError."""

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


def _sine(hz, sr=KIT_SR, seconds=4.0, amp=0.5):
    t = np.arange(int(seconds * sr)) / sr
    x = amp * np.sin(2 * np.pi * hz * t)
    fade = int(0.003 * sr)
    x[:fade] *= np.linspace(0, 1, fade)
    x[-fade:] *= np.linspace(1, 0, fade)
    return x.astype(np.float32)


def _bass_kit():
    import phrases
    items = [(f"m{m}", _sine(_hz(m))) for m in KIT_MIDI]
    return FakeKits({phrases.BASS_KIT: (items, KIT_SR)})


def _note_windows(pid):
    """[(MIDI, начало с, конец с)] нот фразы из данных модуля (phrases.BASS: (MIDI, доля, длина))."""
    import phrases
    ph = phrases.BASS[pid]
    beat = 60.0 / ph["bpm"]
    return [(m, s * beat, (s + d) * beat) for m, s, d in ph["notes"]]


def _f0(x, sr, lo=20.0, hi=120.0):
    """Частота самого сильного пика спектра в [lo, hi] (окно Ханна, дополнение нулями, парабола)."""
    x = np.asarray(x, dtype=np.float64)
    x = x - x.mean()
    n = 1 << int(np.ceil(np.log2(len(x) * 8)))
    s = np.abs(np.fft.rfft(x * np.hanning(len(x)), n))
    f = np.fft.rfftfreq(n, 1 / sr)
    band = np.where((f >= lo) & (f <= hi))[0]
    k = band[np.argmax(s[band])]
    a, b, c = np.log(s[k - 1] + 1e-30), np.log(s[k] + 1e-30), np.log(s[k + 1] + 1e-30)
    p = 0.5 * (a - c) / (a - 2 * b + c)
    return (k + p) * sr / n


def _window(y, sr, t0, t1, pad=0.2):
    return np.asarray(y)[int((t0 + pad) * sr):int((t1 - pad) * sr)]


@unittest.skipUnless(_HAS_NP, _SKIP_NP)
class TestBassPhraseFromKit(unittest.TestCase):

    def setUp(self):
        # Изоляция тестов: модуль кэширует загруженный набор между вызовами (одно хранилище на воркер),
        # а здесь у каждого теста своё подставное хранилище.
        import phrases
        cache = getattr(phrases, "_kit_cache", None)
        if isinstance(cache, dict):
            cache.clear()
            self.addCleanup(cache.clear)

    def _bass(self, res=None):
        import phrases
        if res is None:
            parts, cycle = phrases.render_dry(BASS_PHRASE, 1.0, SR)
        else:
            parts, cycle = phrases.render_dry(BASS_PHRASE, 1.0, SR, res=res)
        self.assertIn("bass", parts)
        return np.asarray(parts["bass"], dtype=np.float64), cycle

    def test_phrase_data_as_assumed(self):
        """Предпосылка теста: первые ноты bass-long — E1 (40), C1 (36)."""
        notes = _note_windows(BASS_PHRASE)
        self.assertEqual([m for m, _, _ in notes[:2]], [40, 36])

    def test_first_note_pitch_from_kit(self):
        res = _bass_kit()
        y, _ = self._bass(res)
        m, t0, t1 = _note_windows(BASS_PHRASE)[0]
        f = _f0(_window(y, SR, t0, t1), SR)
        self.assertAlmostEqual(f, _hz(m), delta=_hz(m) * 0.03)
        import phrases
        self.assertIn(phrases.BASS_KIT, res.calls, "набор не запрашивался")

    def test_shifted_note_pitch_from_nearest_sample(self):
        """C1 (36) — ближайший сэмпл m40, сдвиг вниз на 4 полутона → 32,7 Гц."""
        y, _ = self._bass(_bass_kit())
        m, t0, t1 = _note_windows(BASS_PHRASE)[1]
        f = _f0(_window(y, SR, t0, t1), SR)
        self.assertAlmostEqual(f, _hz(m), delta=_hz(m) * 0.03)

    def test_all_notes_pitch(self):
        y, _ = self._bass(_bass_kit())
        for m, t0, t1 in _note_windows(BASS_PHRASE):
            with self.subTest(midi=m):
                f = _f0(_window(y, SR, t0, t1), SR)
                self.assertAlmostEqual(f, _hz(m), delta=_hz(m) * 0.03)

    def test_kit_sound_is_sample_not_synth(self):
        """Набор из чистых синусов: в окне ноты почти нет второй гармоники (сэмпл звучит как есть),
        и сигнал отличается от синт-баса."""
        y, _ = self._bass(_bass_kit())
        dry, _ = self._bass(None)
        self.assertEqual(len(y), len(dry), "длина круга зависит от набора")
        self.assertGreater(float(np.abs(y).max()), 1e-3, "бас набором беззвучен")
        self.assertFalse(np.allclose(y, dry, atol=1e-4), "с набором звучит тот же синт-бас")
        m, t0, t1 = _note_windows(BASS_PHRASE)[0]
        w = _window(y, SR, t0, t1)
        n = 1 << int(np.ceil(np.log2(len(w) * 4)))
        s = np.abs(np.fft.rfft(w * np.hanning(len(w)), n))
        f = np.fft.rfftfreq(n, 1 / SR)

        def peak(hz):
            band = (f > hz * 0.95) & (f < hz * 1.05)
            return float(s[band].max())

        f1 = _hz(m)
        self.assertLess(20 * np.log10(peak(2 * f1) / peak(f1) + 1e-30), -30,
                        "вторая гармоника заметна — играет не синус набора")

    def test_without_res_synth_bass_unchanged(self):
        import phrases
        parts, cycle = phrases.render_dry(BASS_PHRASE, 1.0, SR)
        y = np.asarray(parts["bass"], dtype=np.float64)
        self.assertGreater(float(np.abs(y).max()), 1e-3)
        a = np.asarray(phrases.render_dry(BASS_PHRASE, 1.0, SR, res=None)[0]["bass"], dtype=np.float64)
        self.assertTrue(np.allclose(a, y), "res=None ≠ вызову без res")
        m, t0, t1 = _note_windows(BASS_PHRASE)[0]
        f = _f0(_window(y, SR, t0, t1), SR)
        self.assertAlmostEqual(f, _hz(m), delta=_hz(m) * 0.03)

    def test_storage_without_kit_falls_back_to_synth(self):
        """Набора нет у хранилища (kit → KeyError) — синт-бас, как без res."""
        import phrases
        y, _ = self._bass(FakeKits({}))
        parts, _ = phrases.render_dry(BASS_PHRASE, 1.0, SR)
        self.assertTrue(np.allclose(y, np.asarray(parts["bass"], dtype=np.float64)))

    def test_kit_installed_later_is_used(self):
        """Условие 107 «если набор есть у хранилища»: набора не было (первый расчёт — синт-бас), набор
        поставили (fx_kit_install) — следующий расчёт фразы играет набором, без перезапуска воркера."""
        import phrases
        res = FakeKits({})
        before, _ = self._bass(res)
        res.kits.update(_bass_kit().kits)   # то же хранилище, набор появился
        res.calls.clear()
        after, _ = self._bass(res)
        self.assertIn(phrases.BASS_KIT, res.calls, "набор, появившийся после первого расчёта, не запрашивался")
        self.assertFalse(np.allclose(before, after, atol=1e-4), "после установки набора звучит прежний синт-бас")

    def test_kit_pitch_does_not_depend_on_tempo(self):
        """Высота нот набора от темпа не зависит (± 3 %)."""
        import phrases
        for tempo in (0.75, 1.25):
            with self.subTest(tempo=tempo):
                parts, _ = phrases.render_dry(BASS_PHRASE, tempo, SR, res=_bass_kit())
                y = np.asarray(parts["bass"], dtype=np.float64)
                m, t0, t1 = _note_windows(BASS_PHRASE)[0]
                f = _f0(_window(y, SR, t0 / tempo, t1 / tempo), SR)
                self.assertAlmostEqual(f, _hz(m), delta=_hz(m) * 0.03)


if __name__ == "__main__":
    unittest.main()
