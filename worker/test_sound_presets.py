"""Тесты карточки internal-own-track, этап 1 «Пресеты звука», условия 1, 2, 8
(тест-кейсы ТК13–ТК17): хранилище пресетов звука на воркере и статусы пресетов у трека.

Контракт (из карточки, без чтения реализации):
- таблица sound_presets; встроенные (slug transmission, sex-on-fire) досеиваются при
  старте по slug — повторный старт не дублирует;
- GET /sound-presets — список, встроенные первыми; POST — создать (→ объект с id);
  PUT /sound-presets/{id} — заменить name/note/specs/final/reference_job_id;
  DELETE /sound-presets/{id}; встроенный — PUT/DELETE → 409; нет такого → 404;
- проверка → 422 с причиной: name 1–80 после trim; note ≤ 500; specs ≤ 16, у записи
  stems — непустой список известных дорожек и ровно одно из engine | chain(+params) |
  steps (1–12); db −24…24; final ≤ 12; reference_job_id int|null; specs и final оба
  пустые → 422;
- POST /jobs: sound_preset_ids (≤ 3 разных существующих; draft + непустой → 422);
  у джобы sound_presets [{id, status, child_id, error}], новая — pending; у старых — [];
- POST /jobs/{id}/sound-presets/{pid}/state {status, child_id?, error?}: pending→running,
  running→done (child_id обязателен), running→error (error обязателен, ≤ 500),
  error→pending, running→pending; прочие → 409; пресета нет у джобы → 404;
  ответ — актуальный список sound_presets джобы.

Допущения тестов (карточка их не называет — см. отчёт test-author):
- «старт» воркера = init_db() + _migrate(): их зовёт и импорт модуля, и база тестов
  (_WorkerApiCase); startup-событие FastAPI в тестах не запускается (там очередь);
- POST /jobs в тестах не рендерит: startup-событие с очередью не стартует, джоба
  остаётся queued; переходы статусов пресета от статуса джобы не зависят;
- ответ PUT/DELETE успешен любым 2xx.

Внешних границ здесь нет (ни GPU, ни сети) — ничего не подменяется.

Запуск: cd worker && python3 -m unittest test_sound_presets -v
Без окружения воркера (fastapi/httpx/numpy) — пропуск.
"""
import copy
import unittest

from test_pure import _HAS_WORKER_DEPS, _WorkerApiCase

_SKIP = "нужны fastapi/httpx/numpy (окружение воркера)"

BUILTIN_SLUGS = {"transmission", "sex-on-fire"}
# валидная цепочка движка: блок eq из fx_blocks.json
ENGINE = [{"type": "eq", "highpass_hz": 80}]
FINAL = [{"chain": "width", "params": {"width": 1.1, "bass": 120}},
         {"chain": "level", "params": {"gain": 5.5, "ceiling": -1}}]


def _preset(**over):
    """Валидное тело пресета: запись движка на бас + финал."""
    body = {
        "name": "Мой звук",
        "note": "проверка",
        "specs": [{"stems": ["bass"], "engine": copy.deepcopy(ENGINE), "db": -3}],
        "final": copy.deepcopy(FINAL),
        "reference_job_id": None,
    }
    body.update(over)
    return body


class _PresetCase(_WorkerApiCase):
    def _list(self):
        r = self.client.get("/sound-presets")
        self.assertEqual(r.status_code, 200, r.text)
        data = r.json()
        self.assertIsInstance(data, list, data)
        return data

    def _builtins(self):
        return [p for p in self._list() if p.get("builtin")]

    def _create(self, **over):
        r = self.client.post("/sound-presets", json=_preset(**over))
        self.assertEqual(r.status_code, 200, r.text)
        data = r.json()
        self.assertIsInstance(data.get("id"), int, data)
        return data

    def _by_id(self, pid):
        return next((p for p in self._list() if p.get("id") == pid), None)

    def _assert_422(self, r):
        self.assertEqual(r.status_code, 422, r.text)
        # «422 с причиной»: в ответе есть непустое объяснение
        detail = r.json().get("detail")
        self.assertTrue(detail, r.text)

    # джобы
    def _submit(self, **extra):
        body = {"style": "dark rock", "lyrics": "[verse] la la"}
        body.update(extra)
        return self.client.post("/jobs", json=body)

    def _submit_ok(self, **extra):
        r = self._submit(**extra)
        self.assertEqual(r.status_code, 200, r.text)
        data = r.json()
        jid = data.get("id", data.get("job_id"))
        self.assertTrue(jid, data)
        return int(jid)

    def _get_job(self, jid):
        r = self.client.get(f"/jobs/{jid}")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def _job_count(self):
        with self._conn() as c:
            return c.execute("SELECT COUNT(*) FROM jobs").fetchone()[0]

    def _state(self, jid, pid, **body):
        return self.client.post(f"/jobs/{jid}/sound-presets/{pid}/state", json=body)

    def _status_of(self, jid, pid):
        sp = self._get_job(jid).get("sound_presets")
        item = next((x for x in sp or [] if x.get("id") == pid), None)
        self.assertIsNotNone(item, sp)
        return item


# ---------- ТК13: встроенные пресеты ----------

@unittest.skipUnless(_HAS_WORKER_DEPS, _SKIP)
class TestBuiltinPresets(_PresetCase):

    def test_tc13_empty_db_has_two_builtins(self):
        lst = self._list()
        self.assertEqual(len(lst), 2, lst)
        self.assertEqual({p.get("slug") for p in lst}, BUILTIN_SLUGS)
        for p in lst:
            self.assertIs(p.get("builtin"), True, p)
            self.assertIsInstance(p.get("id"), int, p)

    def test_tc13_restart_does_not_duplicate(self):
        before = self._builtins()
        # повторный старт воркера на той же базе
        self.w.init_db()
        self.w._migrate()
        self.w.init_db()
        self.w._migrate()
        after = self._builtins()
        self.assertEqual(len(after), 2, after)
        self.assertEqual(sorted(p["id"] for p in after), sorted(p["id"] for p in before))

    def test_tc13_builtins_first_after_own_presets(self):
        own = self._create(name="Свой")
        lst = self._list()
        self.assertEqual(len(lst), 3, lst)
        self.assertEqual({p.get("slug") for p in lst[:2]}, BUILTIN_SLUGS)
        self.assertEqual(lst[2]["id"], own["id"])
        self.assertFalse(lst[2].get("builtin"))

    def test_cond8_transmission_recipe(self):
        p = next(x for x in self._list() if x.get("slug") == "transmission")
        self.assertEqual(p.get("reference_job_id"), 376)
        stems = {s for spec in p["specs"] for s in spec["stems"]}
        self.assertEqual(stems, {"kick", "snare", "bass", "other", "vocals"})
        # бочка и малый — сэмплер движка
        for part in ("kick", "snare"):
            spec = next(s for s in p["specs"] if part in s["stems"])
            self.assertTrue(any(b.get("type") == "sampler" for b in spec.get("engine") or []), spec)
        chains = [f["chain"] for f in p["final"] if not f.get("off")]
        # этап 1б, условие 13: шаг level из финала убран (громкость — target_lufs)
        self.assertEqual(chains, ["width"])

    def test_cond8_sex_on_fire_recipe(self):
        p = next(x for x in self._list() if x.get("slug") == "sex-on-fire")
        self.assertEqual(p.get("reference_job_id"), 383)
        stems = {s for spec in p["specs"] for s in spec["stems"]}
        self.assertEqual(stems, {"kick", "snare", "bass", "other", "vocals"})
        chains = [f["chain"] for f in p["final"] if not f.get("off")]
        # этап 1б, условие 13: шаг level из финала убран (громкость — target_lufs)
        self.assertEqual(chains, ["eq", "width"])


# ---------- ТК14: создание, изменение, удаление ----------

@unittest.skipUnless(_HAS_WORKER_DEPS, _SKIP)
class TestPresetCrud(_PresetCase):

    def test_tc14_create_returns_object_with_id_and_listed(self):
        created = self._create()
        self.assertEqual(created.get("name"), "Мой звук")
        self.assertFalse(created.get("builtin"))
        got = self._by_id(created["id"])
        self.assertIsNotNone(got)
        self.assertEqual(got["name"], "Мой звук")
        self.assertEqual(got["note"], "проверка")
        self.assertEqual(len(got["specs"]), 1)
        self.assertEqual(got["specs"][0]["stems"], ["bass"])
        self.assertEqual(got["specs"][0]["engine"], ENGINE)
        self.assertEqual(got["specs"][0]["db"], -3)
        self.assertEqual([f["chain"] for f in got["final"]], ["width", "level"])

    def test_tc14_put_replaces_name_and_specs(self):
        pid = self._create()["id"]
        new_specs = [{"stems": ["vocals"], "chain": "soften", "params": {"strength": 0.6}, "db": 2}]
        r = self.client.put(f"/sound-presets/{pid}", json=_preset(name="Другое имя", specs=new_specs))
        self.assertLess(r.status_code, 300, r.text)
        got = self._by_id(pid)
        self.assertEqual(got["name"], "Другое имя")
        self.assertEqual(len(got["specs"]), 1)
        self.assertEqual(got["specs"][0]["stems"], ["vocals"])
        self.assertEqual(got["specs"][0]["chain"], "soften")
        self.assertEqual(got["specs"][0]["params"], {"strength": 0.6})
        self.assertNotIn("engine", {k for k, v in got["specs"][0].items() if v})

    def test_tc14_delete_removes_from_list(self):
        pid = self._create()["id"]
        r = self.client.delete(f"/sound-presets/{pid}")
        self.assertLess(r.status_code, 300, r.text)
        self.assertIsNone(self._by_id(pid))
        # встроенные на месте
        self.assertEqual(len(self._builtins()), 2)

    def test_tc14_builtin_put_and_delete_409(self):
        for b in self._builtins():
            with self.subTest(slug=b.get("slug")):
                r = self.client.put(f"/sound-presets/{b['id']}", json=_preset(name="взлом"))
                self.assertEqual(r.status_code, 409, r.text)
                r = self.client.delete(f"/sound-presets/{b['id']}")
                self.assertEqual(r.status_code, 409, r.text)
                got = self._by_id(b["id"])
                self.assertIsNotNone(got)
                self.assertEqual(got["name"], b["name"])

    def test_tc14_missing_put_and_delete_404(self):
        r = self.client.put("/sound-presets/99999", json=_preset())
        self.assertEqual(r.status_code, 404, r.text)
        r = self.client.delete("/sound-presets/99999")
        self.assertEqual(r.status_code, 404, r.text)

    def test_db_defaults_to_zero(self):
        specs = [{"stems": ["bass"], "engine": copy.deepcopy(ENGINE)}]
        pid = self._create(specs=specs)["id"]
        self.assertEqual(self._by_id(pid)["specs"][0]["db"], 0)

    def test_steps_record_valid(self):
        specs = [{"stems": ["other", "guitar"],
                  "steps": [{"chain": "soften", "params": {"strength": 0.5}, "off": False}]}]
        pid = self._create(specs=specs, final=[])["id"]
        got = self._by_id(pid)
        self.assertEqual(got["specs"][0]["stems"], ["other", "guitar"])
        self.assertEqual(len(got["specs"][0]["steps"]), 1)

    def test_only_final_valid(self):
        pid = self._create(specs=[])["id"]
        self.assertEqual(self._by_id(pid)["specs"], [])

    def test_name_trimmed_80_chars_ok(self):
        name = "я" * 80
        created = self._create(name="  " + name + "  ")
        self.assertEqual(self._by_id(created["id"])["name"], name)

    def test_reference_job_id_int_stored(self):
        pid = self._create(reference_job_id=376)["id"]
        self.assertEqual(self._by_id(pid)["reference_job_id"], 376)

    def test_put_validates_same_as_post(self):
        pid = self._create()["id"]
        r = self.client.put(f"/sound-presets/{pid}", json=_preset(name="   "))
        self._assert_422(r)
        self.assertEqual(self._by_id(pid)["name"], "Мой звук")


# ---------- ТК15: проверка тела → 422 ----------

@unittest.skipUnless(_HAS_WORKER_DEPS, _SKIP)
class TestPresetValidation(_PresetCase):

    def _bad(self, **over):
        n = len(self._list())
        r = self.client.post("/sound-presets", json=_preset(**over))
        self._assert_422(r)
        self.assertEqual(len(self._list()), n, "невалидный пресет сохранился")

    def test_tc15_name_empty(self):
        self._bad(name="")

    def test_tc15_name_spaces(self):
        self._bad(name="    ")

    def test_tc15_name_81(self):
        self._bad(name="я" * 81)

    def test_note_501(self):
        self._bad(note="x" * 501)

    def test_tc15_stems_empty(self):
        self._bad(specs=[{"stems": [], "engine": ENGINE}])

    def test_tc15_stems_unknown(self):
        self._bad(specs=[{"stems": ["flute"], "engine": ENGINE}])

    def test_tc15_two_kinds_engine_and_chain(self):
        self._bad(specs=[{"stems": ["bass"], "engine": ENGINE, "chain": "soften", "params": {"strength": 0.5}}])

    def test_tc15_no_kind(self):
        self._bad(specs=[{"stems": ["bass"], "db": 2}])

    def test_tc15_engine_unknown_block(self):
        self._bad(specs=[{"stems": ["bass"], "engine": [{"type": "no-such-block"}]}])

    def test_tc15_steps_13(self):
        steps = [{"chain": "soften", "params": {"strength": 0.5}}] * 13
        self._bad(specs=[{"stems": ["vocals"], "steps": steps}])

    def test_steps_empty(self):
        self._bad(specs=[{"stems": ["vocals"], "steps": []}])

    def test_tc15_db_30(self):
        self._bad(specs=[{"stems": ["bass"], "engine": ENGINE, "db": 30}])

    def test_db_minus_30(self):
        self._bad(specs=[{"stems": ["bass"], "engine": ENGINE, "db": -30}])

    def test_tc15_final_13(self):
        self._bad(final=[{"chain": "level", "params": {"gain": 1}}] * 13)

    def test_tc15_specs_and_final_empty(self):
        self._bad(specs=[], final=[])

    def test_specs_17(self):
        self._bad(specs=[{"stems": ["bass"], "engine": ENGINE}] * 17)

    def test_chain_too_long(self):
        self._bad(specs=[{"stems": ["bass"], "chain": "x" * 41, "params": {}}])

    def test_params_not_numbers(self):
        self._bad(specs=[{"stems": ["bass"], "chain": "soften", "params": {"strength": "сильно"}}])

    def test_specs_and_final_wrong_types(self):
        # кросс-ревью s1: false/{}/"" — не «пусто», а ошибка типа (иначе PUT молча стирал бы правки)
        for bad in (None, False, {}, ""):
            with self.subTest(specs=bad):
                self._bad(specs=bad, final=[{"chain": "level", "params": {"gain": 3}}])
            with self.subTest(final=bad):
                self._bad(final=bad)

    def test_ffmpeg_chain_on_drum_part(self):
        # кросс-ревью s1: части барабанов пересборка обрабатывает только движком
        self._bad(specs=[{"stems": ["kick"], "chain": "eq", "params": {"low": -12}}])
        self._bad(specs=[{"stems": ["snare", "bass"], "steps": [{"chain": "eq"}]}])

    def test_final_all_off_without_specs(self):
        # ревью s1: финал из одних выключенных шагов и без правок — пустой пресет
        self._bad(specs=[], final=[{"chain": "level", "params": {"gain": 3}, "off": True}])

    def test_params_nan_and_infinity(self):
        # ревью безопасности s1: NaN/Infinity из JSON — не число рецепта (ломали бы ответ и графы)
        for bad in ("NaN", "Infinity"):
            with self.subTest(bad=bad):
                n = len(self._list())
                body = ('{"name": "x", "specs": [{"stems": ["bass"], "chain": "soften", '
                        f'"params": {{"strength": {bad}}}}}]}}')
                r = self.client.post("/sound-presets", content=body, headers={"content-type": "application/json"})
                self._assert_422(r)
                self.assertEqual(len(self._list()), n)

    def test_final_chain_empty(self):
        self._bad(final=[{"chain": "", "params": {}}])

    def test_reference_job_id_not_int(self):
        self._bad(reference_job_id="376")


# ---------- ТК16: пресеты у нового трека ----------

@unittest.skipUnless(_HAS_WORKER_DEPS, _SKIP)
class TestJobPresetIds(_PresetCase):

    def test_tc16_job_gets_pending_presets(self):
        pid = self._create()["id"]
        jid = self._submit_ok(sound_preset_ids=[pid])
        sp = self._get_job(jid).get("sound_presets")
        self.assertEqual(len(sp), 1, sp)
        self.assertEqual(sp[0]["id"], pid)
        self.assertEqual(sp[0]["status"], "pending")
        self.assertFalse(sp[0].get("child_id"))
        self.assertFalse(sp[0].get("error"))

    def test_tc16_three_presets_ok_and_in_list_endpoint(self):
        ids = [b["id"] for b in self._builtins()] + [self._create()["id"]]
        jid = self._submit_ok(sound_preset_ids=ids)
        sp = self._get_job(jid)["sound_presets"]
        self.assertEqual({x["id"] for x in sp}, set(ids))
        self.assertTrue(all(x["status"] == "pending" for x in sp), sp)
        # тот же список в GET /jobs
        r = self.client.get("/jobs")
        self.assertEqual(r.status_code, 200, r.text)
        data = r.json()
        jobs = data if isinstance(data, list) else data.get("jobs", data.get("items", []))
        job = next(j for j in jobs if j.get("id") == jid)
        self.assertEqual({x["id"] for x in job["sound_presets"]}, set(ids))

    def _bad_submit(self, **extra):
        n = self._job_count()
        r = self._submit(**extra)
        self._assert_422(r)
        self.assertEqual(self._job_count(), n, "джоба создана при 422")

    def test_tc16_four_ids_422(self):
        ids = [b["id"] for b in self._builtins()] + [self._create(name="a")["id"], self._create(name="b")["id"]]
        self.assertEqual(len(set(ids)), 4)
        self._bad_submit(sound_preset_ids=ids)

    def test_tc16_missing_id_422(self):
        self._bad_submit(sound_preset_ids=[99999])

    def test_tc16_duplicate_id_422(self):
        pid = self._create()["id"]
        self._bad_submit(sound_preset_ids=[pid, pid])

    def test_tc16_draft_with_ids_422(self):
        pid = self._create()["id"]
        self._bad_submit(sound_preset_ids=[pid], draft=True)

    def test_draft_with_empty_ids_ok(self):
        jid = self._submit_ok(sound_preset_ids=[], draft=True)
        self.assertEqual(self._get_job(jid).get("sound_presets"), [])

    def test_tc16_without_field_empty_list(self):
        jid = self._submit_ok()
        self.assertEqual(self._get_job(jid).get("sound_presets"), [])

    def test_old_job_empty_list(self):
        # джоба «до пресетов» — вставлена в базу напрямую, без поля
        jid = self._job(semantic=False)
        self.assertEqual(self._get_job(jid).get("sound_presets"), [])


# ---------- ТК17: переходы статуса пресета у трека ----------

@unittest.skipUnless(_HAS_WORKER_DEPS, _SKIP)
class TestPresetState(_PresetCase):

    def setUp(self):
        super().setUp()
        self.pid = self._create()["id"]
        self.jid = self._submit_ok(sound_preset_ids=[self.pid])
        self.child = self._job(semantic=False)  # версия-трек, которую «сделало» приложение

    def _run(self):
        r = self._state(self.jid, self.pid, status="running")
        self.assertEqual(r.status_code, 200, r.text)
        return r

    def test_tc17_pending_to_running_returns_list(self):
        r = self._run()
        data = r.json()
        self.assertIsInstance(data, list, data)
        item = next(x for x in data if x["id"] == self.pid)
        self.assertEqual(item["status"], "running")
        self.assertEqual(self._status_of(self.jid, self.pid)["status"], "running")

    def test_tc17_second_capture_409(self):
        self._run()
        r = self._state(self.jid, self.pid, status="running")
        self.assertEqual(r.status_code, 409, r.text)
        self.assertEqual(self._status_of(self.jid, self.pid)["status"], "running")

    def test_tc17_done_without_child_rejected(self):
        self._run()
        r = self._state(self.jid, self.pid, status="done")
        self.assertIn(r.status_code, (409, 422), r.text)
        self.assertEqual(self._status_of(self.jid, self.pid)["status"], "running")

    def test_tc17_done_with_child(self):
        self._run()
        r = self._state(self.jid, self.pid, status="done", child_id=self.child)
        self.assertEqual(r.status_code, 200, r.text)
        item = self._status_of(self.jid, self.pid)
        self.assertEqual(item["status"], "done")
        self.assertEqual(item["child_id"], self.child)

    def test_tc17_error_with_reason(self):
        self._run()
        r = self._state(self.jid, self.pid, status="error", error="нет дорожек: kick")
        self.assertEqual(r.status_code, 200, r.text)
        item = self._status_of(self.jid, self.pid)
        self.assertEqual(item["status"], "error")
        self.assertEqual(item["error"], "нет дорожек: kick")

    def test_error_without_reason_rejected(self):
        self._run()
        r = self._state(self.jid, self.pid, status="error")
        self.assertIn(r.status_code, (409, 422), r.text)
        self.assertEqual(self._status_of(self.jid, self.pid)["status"], "running")

    def test_error_reason_501_rejected(self):
        self._run()
        r = self._state(self.jid, self.pid, status="error", error="x" * 501)
        self.assertIn(r.status_code, (409, 422), r.text)
        self.assertEqual(self._status_of(self.jid, self.pid)["status"], "running")

    def test_tc17_error_to_pending_retry(self):
        self._run()
        self._state(self.jid, self.pid, status="error", error="сбой")
        r = self._state(self.jid, self.pid, status="pending")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(self._status_of(self.jid, self.pid)["status"], "pending")
        # после повтора снова можно захватить
        self._run()

    def test_running_to_pending_retry(self):
        self._run()
        r = self._state(self.jid, self.pid, status="pending")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(self._status_of(self.jid, self.pid)["status"], "pending")

    def test_tc17_done_to_running_409(self):
        self._run()
        self._state(self.jid, self.pid, status="done", child_id=self.child)
        r = self._state(self.jid, self.pid, status="running")
        self.assertEqual(r.status_code, 409, r.text)
        item = self._status_of(self.jid, self.pid)
        self.assertEqual(item["status"], "done")
        self.assertEqual(item["child_id"], self.child)

    def test_done_to_pending_409(self):
        self._run()
        self._state(self.jid, self.pid, status="done", child_id=self.child)
        r = self._state(self.jid, self.pid, status="pending")
        self.assertEqual(r.status_code, 409, r.text)

    def test_pending_to_done_409(self):
        r = self._state(self.jid, self.pid, status="done", child_id=self.child)
        self.assertEqual(r.status_code, 409, r.text)
        self.assertEqual(self._status_of(self.jid, self.pid)["status"], "pending")

    def test_tc17_preset_not_on_job_404(self):
        other = self._builtins()[0]["id"]
        r = self._state(self.jid, other, status="running")
        self.assertEqual(r.status_code, 404, r.text)

    def test_job_missing_404(self):
        r = self._state(99999, self.pid, status="running")
        self.assertEqual(r.status_code, 404, r.text)

    def test_capture_independent_per_job(self):
        # тот же пресет у второй джобы: захват на первой не трогает вторую
        jid2 = self._submit_ok(sound_preset_ids=[self.pid])
        self._run()
        self.assertEqual(self._status_of(jid2, self.pid)["status"], "pending")
        r = self._state(jid2, self.pid, status="running")
        self.assertEqual(r.status_code, 200, r.text)


if __name__ == "__main__":
    unittest.main()


@unittest.skipUnless(_HAS_WORKER_DEPS, _SKIP)
class TestBuiltinRecipesValid(unittest.TestCase):
    """Условие 8: встроенные рецепты проходят ту же проверку, что свои (движок — parse_chain)."""

    def test_builtins_pass_validation(self):
        import fx_engine
        import presets
        for b in presets.BUILTIN:
            with self.subTest(slug=b["slug"]):
                presets.validate(b, fx_engine.parse_chain)


# ---------- Этап 1б. ТК27: target_lufs у пресета (условие 11) ----------

@unittest.skipUnless(_HAS_WORKER_DEPS, _SKIP)
class TestPresetTargetLufs(_PresetCase):
    """Условие 11: target_lufs — число −24…−6 или null; вне границ / не число → 422."""

    def _bad(self, **over):
        n = len(self._list())
        r = self.client.post("/sound-presets", json=_preset(**over))
        self._assert_422(r)
        self.assertEqual(len(self._list()), n, "невалидный пресет сохранился")

    def test_tc27_minus13_stored_and_returned(self):
        created = self._create(target_lufs=-13)
        got = self._by_id(created["id"])
        self.assertEqual(got.get("target_lufs"), -13)

    def test_tc27_null_ok(self):
        created = self._create(target_lufs=None)
        got = self._by_id(created["id"])
        self.assertIn("target_lufs", got, got)
        self.assertIsNone(got["target_lufs"])

    def test_tc27_bounds_inclusive(self):
        # края диапазона −24…−6 допустимы
        for v in (-24, -6, -13.5):
            with self.subTest(v=v):
                pid = self._create(name=f"п {v}", target_lufs=v)["id"]
                self.assertEqual(self._by_id(pid)["target_lufs"], v)

    def test_tc27_minus30_422(self):
        self._bad(target_lufs=-30)

    def test_tc27_zero_422(self):
        self._bad(target_lufs=0)

    def test_tc27_just_outside_bounds_422(self):
        for v in (-24.5, -5.5):
            with self.subTest(v=v):
                self._bad(target_lufs=v)

    def test_tc27_string_422(self):
        self._bad(target_lufs="-13")

    def test_put_changes_target_lufs(self):
        pid = self._create(target_lufs=-13)["id"]
        r = self.client.put(f"/sound-presets/{pid}", json=_preset(target_lufs=-10))
        self.assertLess(r.status_code, 300, r.text)
        self.assertEqual(self._by_id(pid)["target_lufs"], -10)
        r = self.client.put(f"/sound-presets/{pid}", json=_preset(target_lufs=None))
        self.assertLess(r.status_code, 300, r.text)
        self.assertIsNone(self._by_id(pid)["target_lufs"])

    def test_put_out_of_bounds_422_keeps_old(self):
        pid = self._create(target_lufs=-13)["id"]
        r = self.client.put(f"/sound-presets/{pid}", json=_preset(target_lufs=0))
        self._assert_422(r)
        self.assertEqual(self._by_id(pid)["target_lufs"], -13)

    def test_target_only_with_empty_final_ok(self):
        # правки дорожек + цель громкости без шагов финала — валидный пресет
        pid = self._create(final=[], target_lufs=-14)["id"]
        got = self._by_id(pid)
        self.assertEqual(got["final"], [])
        self.assertEqual(got["target_lufs"], -14)


# ---------- Этап 1б. ТК28: встроенные обновляются при старте (условие 13) ----------

def _bass_spec(preset):
    return next(s for s in preset["specs"] if "bass" in s["stems"])


def _engine_block(spec, typ):
    return next((b for b in spec.get("engine") or [] if b.get("type") == typ), None)


@unittest.skipUnless(_HAS_WORKER_DEPS, _SKIP)
class TestBuiltinUpsert(_PresetCase):
    """Условие 13: досев встроенных — upsert по slug (имя, описание, рецепт, target_lufs,
    reference_job_id), id прежний; свои пресеты не трогаются."""

    def _patch_builtin(self, slug, change):
        """Меняет рецепт встроенного «в коде» (presets.BUILTIN) на время теста."""
        import presets
        saved = copy.deepcopy(presets.BUILTIN)

        def restore():
            presets.BUILTIN[:] = saved
        self.addCleanup(restore)
        for i, b in enumerate(presets.BUILTIN):
            if b["slug"] == slug:
                nb = copy.deepcopy(b)
                change(nb)
                presets.BUILTIN[i] = nb
                return nb
        self.fail(f"нет встроенного {slug}")

    def _restart(self):
        self.w.init_db()
        self.w._migrate()

    def test_tc28_changed_builtin_recipe_upserted_same_id(self):
        own = self._create(name="Свой", target_lufs=-14)
        own_before = self._by_id(own["id"])
        before = {p["slug"]: p for p in self._builtins()}

        def change(b):
            b["name"] = "Пост-панк · новое имя"
            b["note"] = "обновлённое описание"
            b["specs"] = [{"stems": ["bass"], "engine": [{"type": "eq", "highpass_hz": 60}], "db": 1}]
            b["final"] = [{"chain": "width", "params": {"width": 1.3, "bass": 100}}]
            b["target_lufs"] = -11
            b["reference_job_id"] = 488
        self._patch_builtin("transmission", change)
        self._restart()

        after = {p["slug"]: p for p in self._builtins()}
        self.assertEqual(set(after), BUILTIN_SLUGS)
        t = after["transmission"]
        self.assertEqual(t["id"], before["transmission"]["id"], "id встроенного сменился")
        self.assertEqual(t["name"], "Пост-панк · новое имя")
        self.assertEqual(t["note"], "обновлённое описание")
        self.assertEqual(len(t["specs"]), 1)
        self.assertEqual(t["specs"][0]["stems"], ["bass"])
        self.assertEqual(t["specs"][0]["engine"], [{"type": "eq", "highpass_hz": 60}])
        self.assertEqual(t["specs"][0]["db"], 1)
        self.assertEqual([f["chain"] for f in t["final"]], ["width"])
        self.assertEqual(t["final"][0]["params"], {"width": 1.3, "bass": 100})
        self.assertEqual(t["target_lufs"], -11)
        self.assertEqual(t["reference_job_id"], 488)
        self.assertIs(t["builtin"], True)
        # второй встроенный не изменился
        self.assertEqual(after["sex-on-fire"], before["sex-on-fire"])
        # свой пресет не тронут
        self.assertEqual(self._by_id(own["id"]), own_before)
        # всего: два встроенных + свой, без дублей
        self.assertEqual(len(self._list()), 3)

    def test_tc28_upsert_does_not_touch_own_preset_with_same_name(self):
        # свой пресет с именем встроенного — не встроенный, upsert его не трогает
        t_name = next(p for p in self._builtins() if p["slug"] == "transmission")["name"]
        own = self._create(name=t_name)
        own_before = self._by_id(own["id"])
        self._patch_builtin("transmission", lambda b: b.update(note="другое"))
        self._restart()
        self.assertEqual(self._by_id(own["id"]), own_before)
        self.assertEqual(len(self._builtins()), 2)

    def test_tc28_repeat_restart_idempotent(self):
        self._patch_builtin("sex-on-fire", lambda b: b.update(target_lufs=-9))
        self._restart()
        first = self._builtins()
        self._restart()
        second = self._builtins()
        self.assertEqual(first, second)
        sof = next(p for p in second if p["slug"] == "sex-on-fire")
        self.assertEqual(sof["target_lufs"], -9)


@unittest.skipUnless(_HAS_WORKER_DEPS, _SKIP)
class TestBuiltinRecipes1b(_PresetCase):
    """Условие 13 и 11: рецепты встроенных после прослушивания (#488)."""

    def _get(self, slug):
        return next(p for p in self._list() if p.get("slug") == slug)

    def test_tc28_transmission_bass_output_db_0(self):
        bass = _engine_block(_bass_spec(self._get("transmission")), "bass")
        self.assertIsNotNone(bass)
        self.assertEqual(bass.get("output_db"), 0)

    def test_tc28_transmission_bass_eq_without_150(self):
        eq = _engine_block(_bass_spec(self._get("transmission")), "eq")
        self.assertIsNotNone(eq)
        freqs = [band.get("freq_hz") for band in eq.get("bands") or []]
        self.assertNotIn(150, freqs, eq)
        # остальное в eq баса как было: 800 Гц +4, срез низа 80
        self.assertIn(800, freqs, eq)
        b800 = next(band for band in eq["bands"] if band.get("freq_hz") == 800)
        self.assertEqual(b800.get("gain_db"), 4)
        self.assertEqual(eq.get("highpass_hz"), 80)

    def test_tc28_transmission_final_and_target(self):
        p = self._get("transmission")
        self.assertNotIn("level", [f["chain"] for f in p["final"]])
        self.assertEqual(p.get("target_lufs"), -13)

    def test_tc28_sex_on_fire_final_and_target(self):
        p = self._get("sex-on-fire")
        self.assertNotIn("level", [f["chain"] for f in p["final"]])
        self.assertEqual(p.get("target_lufs"), -12)

    def test_builtins_in_code_match_condition_13(self):
        # то же — в самом BUILTIN (источник досева)
        import presets
        by = {b["slug"]: b for b in presets.BUILTIN}
        self.assertEqual(by["transmission"].get("target_lufs"), -13)
        self.assertEqual(by["sex-on-fire"].get("target_lufs"), -12)
        for b in by.values():
            self.assertNotIn("level", [f["chain"] for f in b["final"]], b["slug"])
