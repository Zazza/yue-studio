"""Тесты карточки internal-own-track, этап 13: эндпоинты фраз воркера (ТК134, условие 95).

Контракт:
- GET /fx/phrases → [{id, family, name, bpm, cycle_sec}];
- POST /fx/phrase {phrase, tempo=1, chain, stems=[], bypass=false} → {file, cycle_sec, clipped};
  stems — части фразы под цепочку (пусто — все);
- GET /fx/phrase/files/{file} → WAV 16 бит стерео;
- кэш по (фраза, темп, цепочка, части, bypass, …): повтор — тот же файл без пересчёта;
  хранится последних 40;
- движок выключен → 503; неизвестная фраза → 404; темп вне 0,5…1,5, ошибка цепочки,
  часть не из фразы → 422; файл не из кэша или с «/» → 404;
- цепочка с усилителем — в yue_worker.gpu_queue; /config: fx_phrases=true.

Допущение теста: файлы кэша лежат внутри каталога данных воркера (DATA_DIR, подменяется
на временный) — иначе тест не найдёт файл для проверки mtime.

Внешние границы подменяются как в test_fx_api: fx_resources — фейк с захватом «fake»,
gpu_queue — фейк-контекст.

Запуск: cd worker && python3 -m unittest test_phrase_api -v
"""
import io
import os
import unittest
from contextlib import contextmanager
from unittest import mock

import test_fx_api as fa
from test_fx_api import _Wrap
from test_fx_engine import FakeResources

GAIN = [{"type": "gain", "gain_db": 3}]


@unittest.skipUnless(fa._OK, fa._SKIP)
class _PhraseApiCase(fa._FxApiCase):

    def _phrases(self):
        r = self.client.get("/fx/phrases")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def _first(self, family):
        for p in self._phrases():
            if p["family"] == family:
                return p
        self.fail(f"нет фразы семьи {family}")

    def _post(self, **body):
        return self.client.post("/fx/phrase", json=body)

    def _ok(self, **body):
        r = self._post(**body)
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def _find(self, name):
        """Файл кэша по имени внутри каталога данных воркера."""
        found = [p for p in self.data.rglob(name) if p.is_file()]
        self.assertEqual(len(found), 1, f"файл {name} не найден в DATA_DIR (или их несколько): {found}")
        return found[0]

    def _wav(self, name):
        import soundfile as sf
        r = self.client.get(f"/fx/phrase/files/{name}")
        self.assertEqual(r.status_code, 200, r.text)
        return sf.info(io.BytesIO(r.content)), sf.read(io.BytesIO(r.content), always_2d=True)


class TestPhraseList(_PhraseApiCase):

    def test_tc134_list(self):
        import phrases
        lst = self._phrases()
        self.assertIsInstance(lst, list)
        self.assertTrue(lst)
        for p in lst:
            with self.subTest(id=p.get("id")):
                for k in ("id", "family", "name", "bpm", "cycle_sec"):
                    self.assertIn(k, p)
                self.assertIn(p["family"], ("guitar", "bass", "drums", "synth"))  # synth — этап 13б
                self.assertTrue(p["name"]["ru"] and p["name"]["en"])
                self.assertGreater(p["cycle_sec"], 0)
        cat = phrases.PHRASES
        ids = set(cat) if isinstance(cat, dict) else {e["id"] for e in cat}
        self.assertEqual({p["id"] for p in lst}, ids)

    def test_tc137_list_gives_beats_chords_style(self):
        """Этап 13б, условие 100: каталог отдаёт beats и chords (фронт строит по ним такты), у synth — style."""
        lst = self._phrases()
        fams = {p["family"] for p in lst}
        self.assertIn("synth", fams)
        for p in lst:
            with self.subTest(id=p["id"]):
                self.assertIn("beats", p)
                self.assertIsInstance(p["chords"], list)
                if p["family"] == "synth":
                    self.assertIn(p["style"], ("pad", "arp", "pulse", "drone"))
                    self.assertTrue(p["chords"])
                    self.assertGreater(p["beats"], 0)

    def test_tc134_config_flag(self):
        r = self.client.get("/config")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertIs(r.json().get("fx_phrases"), True)


class TestPhraseRender(_PhraseApiCase):

    def test_tc134_render_gives_file(self):
        ph = self._first("drums")
        out = self._ok(phrase=ph["id"], chain=GAIN)
        for k in ("file", "cycle_sec", "clipped"):
            self.assertIn(k, out)
        self.assertIsInstance(out["clipped"], bool)
        self.assertAlmostEqual(out["cycle_sec"], ph["cycle_sec"], delta=1e-3)  # tempo по умолчанию 1
        info, (y, sr) = self._wav(out["file"])
        self.assertEqual(info.subtype, "PCM_16")
        self.assertEqual(info.channels, 2)
        self.assertEqual(y.shape[1], 2)
        self.assertAlmostEqual(len(y) / sr, out["cycle_sec"], delta=2 / sr)

    def test_tempo_changes_cycle(self):
        ph = self._first("drums")
        out = self._ok(phrase=ph["id"], chain=GAIN, tempo=0.75)
        self.assertAlmostEqual(out["cycle_sec"], ph["cycle_sec"] / 0.75, delta=1e-3)

    def test_tc134_repeat_same_file_not_recomputed(self):
        ph = self._first("drums")
        a = self._ok(phrase=ph["id"], chain=GAIN)
        path = self._find(a["file"])
        old = 1_000_000_000  # отметка в прошлом: пересчёт/перезапись её сотрёт
        os.utime(path, (old, old))
        b = self._ok(phrase=ph["id"], chain=GAIN)
        self.assertEqual(a["file"], b["file"])
        self.assertEqual(os.stat(path).st_mtime, old, "файл пересчитан при повторе")

    def test_tc134_bypass_other_file(self):
        ph = self._first("drums")
        a = self._ok(phrase=ph["id"], chain=GAIN)["file"]
        b = self._ok(phrase=ph["id"], chain=GAIN, bypass=True)["file"]
        self.assertNotEqual(a, b)

    def test_cache_key_differs_by_chain_tempo_stems(self):
        ph = self._first("drums")
        files = {
            self._ok(phrase=ph["id"], chain=GAIN)["file"],
            self._ok(phrase=ph["id"], chain=[{"type": "gain", "gain_db": -3}])["file"],
            self._ok(phrase=ph["id"], chain=GAIN, tempo=1.25)["file"],
            self._ok(phrase=ph["id"], chain=GAIN, stems=["kick"])["file"],
        }
        self.assertEqual(len(files), 4)

    def test_stems_of_phrase_accepted(self):
        ph = self._first("drums")
        out = self._ok(phrase=ph["id"], chain=GAIN, stems=["kick", "snare"])
        self.assertTrue(out["file"])

    def test_tc137_perc_on_drum_phrase(self):
        ph = self._first("drums")
        hits = [{"t": i * 0.25, "d": 0.25, "vel": 0.9} for i in range(8)]
        out = self._ok(phrase=ph["id"], chain=[{"type": "perc", "voice": 1, "notes": hits}], stems=["perc"])
        _, (y, _) = self._wav(out["file"])
        self.assertGreater(float(abs(y).max()), 1e-3)

    def test_tc137_synth_phrase_renders_sound(self):
        ph = self._first("synth")
        notes = [{"t": 0.0, "d": 1.0, "midi": [60, 64, 67], "vel": 0.8}]
        for bypass in (False, True):
            with self.subTest(bypass=bypass):
                out = self._ok(phrase=ph["id"], chain=[{"type": "synth", "notes": notes}], stems=["synth"],
                               bypass=bypass)
                _, (y, _) = self._wav(out["file"])
                self.assertGreater(float(abs(y).max()), 1e-3, "synth-фраза беззвучна")

    def test_cache_keeps_last_40(self):
        ph = self._first("drums")
        first = self._ok(phrase=ph["id"], chain=[{"type": "gain", "gain_db": -12}])["file"]
        last = None
        for i in range(41):
            last = self._ok(phrase=ph["id"], chain=[{"type": "gain", "gain_db": -11 + i * 0.5}])["file"]
        self.assertEqual(self.client.get(f"/fx/phrase/files/{first}").status_code, 404,
                         "старейший файл не вытеснен (хранится больше 40)")
        self.assertEqual(self.client.get(f"/fx/phrase/files/{last}").status_code, 200)

    # ---------- ошибки ----------

    def test_tc134_unknown_phrase_404(self):
        r = self._post(phrase="no-such-phrase-xyz", chain=GAIN)
        self.assertEqual(r.status_code, 404, r.text)

    def test_tc134_tempo_out_of_range_422(self):
        ph = self._first("drums")
        for t in (2, 0.4, 1.6):
            with self.subTest(tempo=t):
                self.assertEqual(self._post(phrase=ph["id"], chain=GAIN, tempo=t).status_code, 422)

    def test_tc134_stem_not_in_phrase_422(self):
        ph = self._first("drums")
        r = self._post(phrase=ph["id"], chain=GAIN, stems=["vocals"])
        self.assertEqual(r.status_code, 422, r.text)

    def test_bad_chain_422(self):
        ph = self._first("drums")
        for chain in ([{"type": "fuzz"}], [{"type": "gain", "gain_db": 999}], "gain"):
            with self.subTest(chain=chain):
                self.assertEqual(self._post(phrase=ph["id"], chain=chain).status_code, 422)

    def test_tc134_engine_off_503(self):
        ph = self._first("drums")
        self.assertLess(self.client.post("/config", json={"fx_engine": False}).status_code, 300)
        r = self._post(phrase=ph["id"], chain=GAIN)
        self.assertEqual(r.status_code, 503, r.text)

    def test_tc134_bad_file_names_404(self):
        ph = self._first("drums")
        self._ok(phrase=ph["id"], chain=GAIN)  # кэш не пуст
        (self.data / "x").write_text("secret", encoding="utf-8")
        for name in ("..%2Fx", "..%2Fsettings.json", "x", "settings.json",
                     "not-from-cache.wav", "a%2Fb.wav"):
            with self.subTest(name=name):
                r = self.client.get(f"/fx/phrase/files/{name}")
                self.assertEqual(r.status_code, 404, r.text)
                self.assertNotIn(b"secret", r.content)


class TestPhraseGpuQueue(_PhraseApiCase):

    def setUp(self):
        super().setUp()
        self.gpu_calls = []
        self.inside = False
        test = self

        @contextmanager
        def fake_queue(section="gpu"):
            test.gpu_calls.append(section)
            test.inside = True
            try:
                yield
            finally:
                test.inside = False

        p = mock.patch.object(self.w, "gpu_queue", fake_queue)
        p.start()
        self.addCleanup(p.stop)
        self.model_inside = []
        orig = self.amp_model.__call__

        def model(x):
            self.model_inside.append(self.inside)
            return orig(x)

        self.resources = FakeResources(amps={"fake": _Wrap(model, self.amp_model)})

    def test_chain_with_amp_takes_gpu_queue(self):
        ph = self._first("guitar")
        self._ok(phrase=ph["id"], chain=[{"type": "amp", "model": "fake"}])
        self.assertTrue(self.gpu_calls, "amp прошёл мимо gpu_queue")
        self.assertTrue(self.model_inside, "модель не вызывалась")
        self.assertTrue(all(self.model_inside), "модель вызвана вне gpu_queue")

    def test_chain_without_amp_no_gpu_queue(self):
        ph = self._first("guitar")
        self._ok(phrase=ph["id"], chain=GAIN)
        self.assertEqual(self.gpu_calls, [])
