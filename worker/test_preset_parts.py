"""Тесты карточки internal-own-track, этап 8а: условия 67 и 68 (тест-кейс ТК100) — проверка рецепта
пресета на воркере: громкость дорожки level_db и партии-рецепты parts.

Контракт (из карточки):
- условие 67: у записи пресета поле level_db (−40…+6) — громкость дорожки к треку после применения;
  запись с level_db — одной дорожки (иначе 422); может быть без обработки ({stems [X], level_db});
- условие 68: поле пресета parts (≤ 8): {kind synth|perc, engine — цепочка движка (первый блок synth
  у synth, perc у perc; notes в рецепте нет), style pad|arp|pulse|drone (synth), octave −2…2 (synth, 0),
  pattern fours|eighths|sixteenths|backbeat|offbeat (perc), swing 0…0,5, accent 0…1 (perc, 1),
  sections [имена частей песни] (пусто — все), place}; ошибка — 422 с причиной (PresetError).

Проверяется чистая функция presets.validate(p, parse_engine) с настоящей проверкой цепочки движка
(fx_engine.parse_chain): внешних границ нет, ничего не подменяется.

Предположения (карточка не уточняет — см. отчёт test-author):
- в нормализованном рецепте level_db лежит в записи под тем же именем, parts — в поле "parts"
  результата; «сохранены как заданы» — каждое заданное поле части возвращается тем же значением
  (умолчания нормализация может дописать);
- пресет из одних партий (specs пусты, финала и мастера нет) — не «пустой пресет».

Запуск: cd worker && python3 -m unittest test_preset_parts -v
Без окружения воркера (numpy/scipy для fx_engine) — пропуск.
"""
import copy
import unittest

try:
    import fx_engine
    import presets
    _OK = True
except ImportError:   # нет numpy/scipy — движок не импортируется
    fx_engine = presets = None
    _OK = False

_SKIP = "нужно окружение воркера (fx_engine: numpy/scipy)"

ENGINE = [{"type": "eq", "highpass_hz": 80}]


def _synth(**over):
    part = {"kind": "synth", "engine": [{"type": "synth", "osc1": 4}, {"type": "eq", "highpass_hz": 200}],
            "style": "pad", "octave": 1, "sections": ["chorus"], "place": {"pan": -0.3, "width": 1.2}}
    part.update(over)
    return part


def _perc(**over):
    part = {"kind": "perc", "engine": [{"type": "perc", "voice": 3}], "pattern": "backbeat",
            "swing": 0.2, "accent": 0.5, "sections": []}
    part.update(over)
    return part


def _preset(specs=None, parts=None):
    body = {"name": "Жанр", "note": "",
            "specs": specs if specs is not None else [{"stems": ["bass"], "engine": copy.deepcopy(ENGINE)}],
            "final": []}
    if parts is not None:
        body["parts"] = parts
    return body


@unittest.skipUnless(_OK, _SKIP)
class TestLevelDb(unittest.TestCase):
    """Условие 67: level_db записи — −40…+6, одной дорожке, можно без обработки."""

    def _ok(self, p):
        return presets.validate(p, fx_engine.parse_chain)

    def _bad(self, p):
        with self.assertRaises(presets.PresetError) as cm:
            presets.validate(p, fx_engine.parse_chain)
        self.assertTrue(str(cm.exception).strip(), "причина 422 пустая")

    def test_out_of_range_rejected(self):
        for v in (-41, 7, -40.5, 6.01):
            with self.subTest(level_db=v):
                self._bad(_preset([{"stems": ["bass"], "engine": copy.deepcopy(ENGINE), "level_db": v}]))

    def test_not_a_number_rejected(self):
        for v in ("-8", True, float("nan")):
            with self.subTest(level_db=v):
                self._bad(_preset([{"stems": ["bass"], "level_db": v}]))

    def test_bounds_accepted(self):
        for v in (-40, 6):
            with self.subTest(level_db=v):
                out = self._ok(_preset([{"stems": ["bass"], "engine": copy.deepcopy(ENGINE), "level_db": v}]))
                self.assertEqual(out["specs"][0]["level_db"], v)

    def test_two_stems_rejected(self):
        self._bad(_preset([{"stems": ["bass", "other"], "engine": copy.deepcopy(ENGINE), "level_db": -10}]))
        self._bad(_preset([{"stems": ["bass", "other"], "level_db": -10}]))

    def test_level_only_record_accepted(self):
        out = self._ok(_preset([{"stems": ["bass"], "level_db": -8}]))
        self.assertEqual(len(out["specs"]), 1)
        sp = out["specs"][0]
        self.assertEqual(sp["stems"], ["bass"])
        self.assertEqual(sp["level_db"], -8)
        for k in ("engine", "chain", "steps"):
            self.assertIn(sp.get(k), (None, "", []), f"запись «только громкость» получила {k}: {sp.get(k)!r}")

    def test_level_only_on_drum_part_accepted(self):
        # ТК101 применяет level_db к hh: часть барабанов без обработки — только громкость
        out = self._ok(_preset([{"stems": ["hh"], "level_db": -28}]))
        self.assertEqual(out["specs"][0]["level_db"], -28)

    def test_level_with_processing_accepted(self):
        out = self._ok(_preset([{"stems": ["vocals"], "engine": copy.deepcopy(ENGINE), "level_db": -3}]))
        self.assertEqual(out["specs"][0]["level_db"], -3)
        self.assertEqual(out["specs"][0]["engine"], ENGINE)

    def test_without_level_unchanged(self):
        out = self._ok(_preset())
        self.assertIn(out["specs"][0].get("level_db"), (None,), "level_db появился у записи без него")

    def test_level_with_place_rejected(self):
        # ТК100а (условие 67а): пересборка места громкость не применяет — level_db у записи «место» = 422;
        # цель громкости — отдельной записью. Контроль: та же запись без level_db принимается.
        self._ok(_preset([{"stems": ["bass"], "place": {"pan": 0.3}}]))
        self._bad(_preset([{"stems": ["bass"], "place": {"pan": 0.3}, "level_db": -8}]))


@unittest.skipUnless(_OK, _SKIP)
class TestParts(unittest.TestCase):
    """Условие 68: партии-рецепты пресета."""

    def _ok(self, p):
        return presets.validate(p, fx_engine.parse_chain)

    def _bad(self, part=None, parts=None):
        p = _preset(parts=parts if parts is not None else [part])
        with self.assertRaises(presets.PresetError) as cm:
            presets.validate(p, fx_engine.parse_chain)
        self.assertTrue(str(cm.exception).strip(), "причина 422 пустая")

    def test_valid_parts_saved_as_given(self):
        given = [_synth(), _perc()]
        out = self._ok(_preset(parts=copy.deepcopy(given)))
        self.assertEqual(len(out["parts"]), 2)
        for g, o in zip(given, out["parts"], strict=True):
            for k, v in g.items():
                with self.subTest(kind=g["kind"], field=k):
                    self.assertEqual(o.get(k), v)

    def test_no_parts_field_is_empty(self):
        out = self._ok(_preset())
        self.assertIn(out.get("parts"), (None, []))

    def test_parts_only_preset_accepted(self):
        out = self._ok(_preset(specs=[], parts=[_perc()]))
        self.assertEqual(len(out["parts"]), 1)

    def test_minimal_parts_accepted(self):
        # необязательные поля — умолчания (octave 0, accent 1, sections — все)
        out = self._ok(_preset(parts=[{"kind": "synth", "engine": [{"type": "synth"}], "style": "drone"},
                                      {"kind": "perc", "engine": [{"type": "perc"}], "pattern": "fours"}]))
        self.assertEqual([p["kind"] for p in out["parts"]], ["synth", "perc"])

    def test_bounds_accepted(self):
        for part in (_synth(octave=-2), _synth(octave=2), _perc(swing=0), _perc(swing=0.5),
                     _perc(accent=0), _perc(accent=1)):
            with self.subTest(part=part):
                self._ok(_preset(parts=[part]))

    def test_all_styles_and_patterns_accepted(self):
        for st in ("pad", "arp", "pulse", "drone"):
            with self.subTest(style=st):
                self._ok(_preset(parts=[_synth(style=st)]))
        for pt in ("fours", "eighths", "sixteenths", "backbeat", "offbeat"):
            with self.subTest(pattern=pt):
                self._ok(_preset(parts=[_perc(pattern=pt)]))

    def test_unknown_kind(self):
        self._bad(_synth(kind="x"))

    def test_synth_without_synth_first(self):
        self._bad(_synth(engine=[{"type": "eq", "highpass_hz": 80}, {"type": "synth"}]))
        self._bad(_synth(engine=[{"type": "perc"}]))
        self._bad(_synth(engine=[]))

    def test_perc_without_perc_first(self):
        self._bad(_perc(engine=[{"type": "synth"}]))
        self._bad(_perc(engine=[{"type": "eq", "highpass_hz": 80}, {"type": "perc"}]))

    def test_engine_checked_by_engine(self):
        self._bad(_synth(engine=[{"type": "synth", "osc1": 99}]))

    def test_unknown_style(self):
        self._bad(_synth(style="x"))

    def test_unknown_pattern(self):
        self._bad(_perc(pattern="x"))

    def test_octave_out_of_range(self):
        for v in (3, -3):
            with self.subTest(octave=v):
                self._bad(_synth(octave=v))

    def test_swing_out_of_range(self):
        for v in (0.6, -0.1):
            with self.subTest(swing=v):
                self._bad(_perc(swing=v))

    def test_accent_out_of_range(self):
        for v in (1.1, -0.1):
            with self.subTest(accent=v):
                self._bad(_perc(accent=v))

    def test_too_many_parts(self):
        self._ok(_preset(parts=[_perc() for _ in range(8)]))
        self._bad(parts=[_perc() for _ in range(9)])

    def test_notes_in_recipe_rejected(self):
        note = {"t": 0, "d": 1, "midi": [60], "vel": 0.8}
        self._bad(_synth(engine=[{"type": "synth", "notes": [note]}]))
        self._bad(_perc(engine=[{"type": "perc", "notes": [{"t": 0, "d": 0.5, "vel": 0.9}]}]))

    def test_parts_not_a_list(self):
        self._bad(parts={"kind": "synth"})

    def test_sections_must_be_list_of_names(self):
        self._bad(_synth(sections="chorus"))
        self._bad(_synth(sections=[1]))


if __name__ == "__main__":
    unittest.main()
