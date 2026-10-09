"""Тесты карточки internal-own-track, «Хвосты тасклога», условие 49 (а, б): тест-кейсы ТК81, ТК82.

Контракт (из карточки, без чтения реализации):
- ТК81 / 49а: повтор того же превью synth/perc (те же ноты/удары, окно, файлы) отдаёт файл кэша, НЕ
  считая уровень партии (synth_part_level для synth, сырые удары perc) и окно; другое окно —
  считается заново. Внешней границы здесь нет: функции расчёта подменяются счётчиками-обёртками
  (поведение не меняется, считается число вызовов): yue_worker.synth_part_level и
  fx_engine._perc_notes (сырые удары perc — и для уровня партии, и для окна).
- ТК82 / 49б: старая база (таблица sound_presets без target_lufs и master + одна своя запись) →
  старт воркера (init_db + _migrate) → GET /sound-presets: своя запись с target_lufs null и
  master []; встроенные есть; второй старт — без ошибок.

Запуск: cd worker && python3 -m unittest test_tails -v
"""
import json
import unittest
from unittest import mock

import test_fx_api as fa
import test_pure as tp
from test_fx_api import _perc_chain, _synth_chain


# ---------- ТК81: превью из кэша — без расчётов партии ----------

@unittest.skipUnless(fa._OK, fa._SKIP)
class TestPreviewCacheSkipsPartLevel(fa._FxApiCase):

    def _track(self, dur=12.0):
        import numpy as np
        import soundfile as sf
        jid = self._job(duration=dur, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        sf.write(str(d / "audio.flac"), fa._tone(fa.SYN_TRACK_HZ, amp=0.3, dur=dur).astype(np.float32), fa.SR)
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid

    def _preview(self, jid, chain, fr, to, add):
        body = {"source": "mix", "output": "solo", "preview": True, "chain": chain, "from": fr, "to": to}
        if add:
            body["add"] = True
        r = self.client.post(f"/jobs/{jid}/fx", json=body)
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()["file"]

    def _check(self, target, attr, chain, add):
        jid = self._track()
        with mock.patch.object(target, attr, wraps=getattr(target, attr)) as spy:
            a = self._preview(jid, json.loads(json.dumps(chain)), 2.0, 6.0, add)
            first = spy.call_count
            self.assertGreater(first, 0, f"{attr} не вызван даже в первый раз — счётчик не на том месте")
            b = self._preview(jid, json.loads(json.dumps(chain)), 2.0, 6.0, add)
            self.assertEqual(b, a, "повтор того же превью — другой файл")
            self.assertEqual(spy.call_count, first, f"повтор из кэша считал {attr}")
            c = self._preview(jid, json.loads(json.dumps(chain)), 3.0, 7.0, add)
            self.assertNotEqual(c, a)
            self.assertGreater(spy.call_count, first, f"другое окно — {attr} не вызван")

    def test_tc81_synth_repeat_from_cache(self):
        chain = _synth_chain([{"t": 2.5, "d": 1.0, "midi": [69], "vel": 1.0},
                              {"t": 5.0, "d": 1.0, "midi": [72], "vel": 1.0}])
        self._check(self.w, "synth_part_level", chain, add=False)

    def test_tc81_perc_repeat_from_cache(self):
        import fx_engine
        self._check(fx_engine, "_perc_notes", _perc_chain([1.0, 2.5, 4.0, 5.5, 7.0]), add=True)


# ---------- ТК82: миграция старой таблицы пресетов ----------

OLD_SPECS = [{"stems": ["bass"], "engine": [{"type": "eq", "highpass_hz": 80}], "db": 0}]
OLD_FINAL = [{"chain": "width", "params": {"width": 1.1, "bass": 120}}]


@unittest.skipUnless(tp._HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestSoundPresetsMigration(tp._WorkerDbCase):

    def _legacy(self):
        # таблица пресетов этапа 1 (до target_lufs и master) и одна своя запись
        with self._conn() as c:
            # импорт воркера мог уже создать новую таблицу в этой базе — старая схема строго с нуля
            c.execute("DROP TABLE IF EXISTS sound_presets")
            c.execute("""CREATE TABLE sound_presets (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                slug TEXT UNIQUE,
                name TEXT NOT NULL,
                note TEXT NOT NULL DEFAULT '',
                specs TEXT NOT NULL DEFAULT '[]',
                final TEXT NOT NULL DEFAULT '[]',
                reference_job_id INTEGER,
                builtin INTEGER NOT NULL DEFAULT 0,
                created_at TEXT NOT NULL)""")
            c.execute("INSERT INTO sound_presets (name, note, specs, final, created_at) VALUES (?, ?, ?, ?, ?)",
                      ("Старый свой", "до мастера", json.dumps(OLD_SPECS), json.dumps(OLD_FINAL),
                       "2026-10-01T00:00:00"))

    def _start(self):
        self.w.init_db()
        self.w._migrate()

    def _list(self):
        from fastapi.testclient import TestClient
        r = TestClient(self.w.app, raise_server_exceptions=False).get("/sound-presets")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def test_tc82_old_table_migrated(self):
        self._legacy()
        self._start()
        with self._conn() as c:
            cols = {r[1] for r in c.execute("PRAGMA table_info(sound_presets)")}
        self.assertTrue({"target_lufs", "master"} <= cols, cols)
        lst = self._list()
        own = [p for p in lst if p.get("name") == "Старый свой"]
        self.assertEqual(len(own), 1, lst)
        p = own[0]
        self.assertFalse(p.get("builtin"))
        self.assertIsNone(p.get("target_lufs"))
        self.assertEqual(p.get("master"), [])
        self.assertEqual(p.get("specs"), OLD_SPECS)
        self.assertEqual([s["chain"] for s in p.get("final")], ["width"])
        self.assertTrue(any(x.get("builtin") for x in lst), "встроенные не досеяны")

    def test_tc82_second_start_no_errors(self):
        self._legacy()
        self._start()
        before = self._list()
        self._start()
        after = self._list()
        self.assertEqual(len(after), len(before), "второй старт задвоил пресеты")
        self.assertEqual(len([p for p in after if p.get("name") == "Старый свой"]), 1)


if __name__ == "__main__":
    unittest.main()
