"""Тесты карточки internal-own-track, этап 13в, условие 103 (тест-кейс ТК139): свои инструменты на воркере.

Контракт (из карточки, без чтения реализации):
- таблица fx_instruments (id, name, base — id готового, group, stems, chain, extra — style/octave/pattern/
  swing/accent/amp_hint/place);
- GET /fx/instruments → список по id; POST → созданный {id, name, base, group, stems, chain, extra};
  PUT /fx/instruments/{id} → обновлённый (меняются поля, что пришли); DELETE → {ok};
- проверка → 422: name 1…80 символов после обрезки пробелов; chain — список блоков (fx_engine.parse_chain;
  synth/perc без notes допустимы); stems — непустой список известных дорожек (vocals drums bass other guitar
  piano kick snare toms hh ride crash synth perc); group ≤ 40 символов; нет id → 404;
- сохраняется как пришло: chain без нормализации (умолчания блоков не дописываются).

Допущения тестов (карточка их не называет):
- POST отвечает 200 или 201; PUT/DELETE — 200;
- имя хранится уже обрезанным (и в ответе create, и в списке);
- проверки условия 103 действуют и на PUT (пустое имя при правке → 422).

Внешних границ нет (ни GPU, ни сети) — ничего не подменяется. БД и каталог данных — временные (_WorkerApiCase).

Запуск: cd worker && python3 -m unittest test_fx_instruments -v
Без окружения воркера (fastapi/httpx/numpy) — пропуск.
"""
import copy
import unittest

from test_pure import _HAS_WORKER_DEPS, _WorkerApiCase

_SKIP = "нужны fastapi/httpx/numpy (окружение воркера)"

KNOWN_STEMS = ["vocals", "drums", "bass", "other", "guitar", "piano", "kick", "snare", "toms", "hh",
               "ride", "crash", "synth", "perc"]
# неполные блоки: нормализация дописала бы умолчания — по ним видно «сохранено как пришло»
CHAIN = [{"type": "eq", "highpass_hz": 80}, {"type": "delay", "time_ms": 375, "wet": 0.25}]
EXTRA = {"style": "pad", "octave": 1, "place": {"pan": 0, "width": 1.5}}
FIELDS = {"id", "name", "base", "group", "stems", "chain", "extra"}


def _body(**over):
    body = {
        "name": "Мой пэд",
        "base": "synth-juno",
        "group": "synth-pad",
        "stems": ["synth"],
        "chain": copy.deepcopy(CHAIN),
        "extra": copy.deepcopy(EXTRA),
    }
    body.update(over)
    return body


@unittest.skipUnless(_HAS_WORKER_DEPS, _SKIP)
class _InstrCase(_WorkerApiCase):
    def _list(self):
        r = self.client.get("/fx/instruments")
        self.assertEqual(r.status_code, 200, r.text)
        data = r.json()
        self.assertIsInstance(data, list, data)
        return data

    def _post(self, **over):
        return self.client.post("/fx/instruments", json=_body(**over))

    def _create(self, **over):
        r = self._post(**over)
        self.assertIn(r.status_code, (200, 201), r.text)
        data = r.json()
        self.assertIsInstance(data.get("id"), int, data)
        return data

    def _by_id(self, iid):
        return next((x for x in self._list() if x.get("id") == iid), None)

    def _assert_422(self, r):
        self.assertEqual(r.status_code, 422, r.text)


class TestCreateAndList(_InstrCase):
    def test_empty_list_on_fresh_db(self):
        self.assertEqual(self._list(), [])

    def test_create_returns_full_object(self):
        body = _body()
        got = self._create()
        self.assertTrue(FIELDS <= set(got), FIELDS - set(got))
        for k in ("name", "base", "group", "stems", "chain", "extra"):
            self.assertEqual(got[k], body[k], k)

    def test_created_is_in_list(self):
        got = self._create()
        item = self._by_id(got["id"])
        self.assertIsNotNone(item, self._list())
        body = _body()
        for k in ("name", "base", "group", "stems", "chain", "extra"):
            self.assertEqual(item[k], body[k], k)

    def test_list_ordered_by_id(self):
        ids = [self._create(name=f"n{i}")["id"] for i in range(3)]
        self.assertEqual(len(set(ids)), 3, ids)
        self.assertEqual([x["id"] for x in self._list()], sorted(ids))

    def test_chain_saved_as_sent(self):
        # без нормализации: умолчания блоков не дописаны, лишних ключей нет
        got = self._create()
        self.assertEqual(got["chain"], CHAIN)
        self.assertEqual(self._by_id(got["id"])["chain"], CHAIN)

    def test_extra_roundtrip(self):
        extra = {"pattern": "eighths", "swing": 0.1, "place": {"pan": 0.3, "width": 1}}
        got = self._create(stems=["perc"], chain=[{"type": "perc", "voice": 1, "rel_db": -16}], extra=extra)
        self.assertEqual(self._by_id(got["id"])["extra"], extra)

    def test_all_known_stems_accepted(self):
        got = self._create(stems=list(KNOWN_STEMS))
        self.assertEqual(self._by_id(got["id"])["stems"], KNOWN_STEMS)


class TestValidation(_InstrCase):
    def test_name_trimmed(self):
        got = self._create(name="   Мой пэд  ")
        self.assertEqual(got["name"], "Мой пэд")
        self.assertEqual(self._by_id(got["id"])["name"], "Мой пэд")

    def test_name_empty_422(self):
        self._assert_422(self._post(name=""))

    def test_name_spaces_only_422(self):
        self._assert_422(self._post(name="    "))

    def test_name_80_ok(self):
        got = self._create(name="я" * 80)
        self.assertEqual(got["name"], "я" * 80)

    def test_name_80_after_trim_ok(self):
        got = self._create(name="  " + "я" * 80 + "  ")
        self.assertEqual(got["name"], "я" * 80)

    def test_name_81_422(self):
        self._assert_422(self._post(name="я" * 81))

    def test_chain_not_list_422(self):
        self._assert_422(self._post(chain={"type": "eq"}))

    def test_chain_unknown_block_422(self):
        self._assert_422(self._post(chain=[{"type": "no-such-block"}]))

    def test_chain_bad_param_422(self):
        # parse_chain: неизвестный параметр блока — ошибка цепочки
        self._assert_422(self._post(chain=[{"type": "eq", "no_such_param": 1}]))

    def test_stems_empty_422(self):
        self._assert_422(self._post(stems=[]))

    def test_stems_unknown_422(self):
        self._assert_422(self._post(stems=["x"]))

    def test_stems_mixed_unknown_422(self):
        self._assert_422(self._post(stems=["guitar", "x"]))

    def test_group_40_ok(self):
        got = self._create(group="g" * 40)
        self.assertEqual(got["group"], "g" * 40)

    def test_group_41_422(self):
        self._assert_422(self._post(group="g" * 41))

    def test_synth_without_notes_ok(self):
        chain = [{"type": "synth", "osc1": 2, "cutoff_hz": 1500}, {"type": "reverb", "wet": 0.2}]
        got = self._create(chain=chain)
        self.assertEqual(self._by_id(got["id"])["chain"], chain)

    def test_perc_without_notes_ok(self):
        chain = [{"type": "perc", "voice": 1, "tone": 1, "rel_db": -16}]
        got = self._create(stems=["perc"], chain=chain, extra={"pattern": "eighths"})
        self.assertEqual(self._by_id(got["id"])["chain"], chain)

    def test_rejected_not_saved(self):
        self._post(name="")
        self._post(stems=["x"])
        self.assertEqual(self._list(), [])


class TestUpdate(_InstrCase):
    def test_update_name_and_chain_rest_kept(self):
        got = self._create()
        new_chain = [{"type": "gain", "gain_db": 3}]
        r = self.client.put(f"/fx/instruments/{got['id']}", json={"name": "  Другое имя ", "chain": new_chain})
        self.assertEqual(r.status_code, 200, r.text)
        upd = r.json()
        self.assertEqual(upd["id"], got["id"])
        self.assertEqual(upd["name"], "Другое имя")
        self.assertEqual(upd["chain"], new_chain)
        item = self._by_id(got["id"])
        self.assertEqual(item["name"], "Другое имя")
        self.assertEqual(item["chain"], new_chain)
        body = _body()
        for k in ("base", "group", "stems", "extra"):
            self.assertEqual(item[k], body[k], k)
            self.assertEqual(upd[k], body[k], k)

    def test_update_only_name_keeps_chain(self):
        got = self._create()
        r = self.client.put(f"/fx/instruments/{got['id']}", json={"name": "Только имя"})
        self.assertEqual(r.status_code, 200, r.text)
        item = self._by_id(got["id"])
        self.assertEqual(item["name"], "Только имя")
        self.assertEqual(item["chain"], CHAIN)

    def test_update_other_untouched(self):
        a = self._create(name="A")
        b = self._create(name="B")
        r = self.client.put(f"/fx/instruments/{a['id']}", json={"name": "A2"})
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(self._by_id(b["id"])["name"], "B")

    def test_update_unknown_404(self):
        r = self.client.put("/fx/instruments/999999", json={"name": "x"})
        self.assertEqual(r.status_code, 404, r.text)

    def test_update_invalid_422_and_unchanged(self):
        got = self._create()
        for bad in ({"name": ""}, {"name": "я" * 81}, {"chain": "eq"}, {"stems": []}, {"group": "g" * 41}):
            r = self.client.put(f"/fx/instruments/{got['id']}", json=bad)
            self.assertEqual(r.status_code, 422, f"{bad}: {r.text}")
        item = self._by_id(got["id"])
        self.assertEqual(item["name"], "Мой пэд")
        self.assertEqual(item["chain"], CHAIN)


class TestDelete(_InstrCase):
    def test_delete_then_absent_and_repeat_404(self):
        got = self._create()
        r = self.client.delete(f"/fx/instruments/{got['id']}")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertTrue(r.json().get("ok"), r.text)
        self.assertIsNone(self._by_id(got["id"]))
        r2 = self.client.delete(f"/fx/instruments/{got['id']}")
        self.assertEqual(r2.status_code, 404, r2.text)

    def test_delete_keeps_others(self):
        a = self._create(name="A")
        b = self._create(name="B")
        self.assertEqual(self.client.delete(f"/fx/instruments/{a['id']}").status_code, 200)
        self.assertEqual([x["id"] for x in self._list()], [b["id"]])

    def test_delete_unknown_404(self):
        r = self.client.delete("/fx/instruments/999999")
        self.assertEqual(r.status_code, 404, r.text)

    def test_update_deleted_404(self):
        got = self._create()
        self.client.delete(f"/fx/instruments/{got['id']}")
        r = self.client.put(f"/fx/instruments/{got['id']}", json={"name": "x"})
        self.assertEqual(r.status_code, 404, r.text)


if __name__ == "__main__":
    unittest.main()
