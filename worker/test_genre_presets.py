"""Тесты карточки internal-own-track, этап 8б: условия 70–72 (тест-кейсы ТК104, ТК105, ТК108).
Написаны по карточке, без чтения новой реализации.

- ТК104 / 70: worker/fx_presets.json побайтно равен internal/mcp/fx_presets.json; deploy.sh увозит
  fx_presets.json на машину воркера (scp).
- ТК105 / 71–72: встроенные пресеты звука (presets.BUILTIN) — жанровые ≥ 100, все проходят validate
  (parse_engine = fx_engine.parse_chain), slug уникальны [a-z0-9-], family из четырёх семей, течение
  (название до « · ») ≥ 3 варианта, течений ≥ 34, без записей голоса, с барабанами и басом, наборы — из
  известных, партии ≤ 3 с первым блоком synth|perc, мастер или цель громкости; в name/note нет имён
  групп и исполнителей из списка условия 71.
- ТК108 / 72: старая база без колонки family → после старта колонка есть, встроенные с семьёй, свои — ''.

Предположения (карточка не уточняет):
- Жанровые проверки (семья-течение-вариант, голос, барабаны/бас, мастер) — для всех встроенных, кроме
  прежних рецептов человека transmission / sex-on-fire / live-rhythm: их условие 71 велит только
  обезличить (slug прежний), а запись голоса в transmission — рецепт человека. Общие проверки (validate,
  slug, family, имена, наборы, партии) — для всех встроенных.
- Имя ищется целым словом без учёта регистра («Eno» не ловит «enough», «Muse» — «museum»).

Запуск: cd worker && python3 -m unittest test_genre_presets -v
"""
import json
import re
import unittest
from pathlib import Path

import presets
import test_pure as tp

try:
    import fx_engine
    _HAS_ENGINE = True
except ImportError:   # окружение без numpy/scipy (как make test на ПК) — проверка validate пропускается
    _HAS_ENGINE = False

WORKER = Path(__file__).resolve().parent
ROOT = WORKER.parent

FAMILIES = ("Рок", "Тяжёлое", "Электроника", "Поп и другое")
KNOWN_KITS = {"osdk", "tr808", "tr909", "linn", "cr78", "simmons", "growlybass", "synthbass", "salamander"}
DRUM_STEMS = {"drums", "kick", "snare", "toms", "hh", "ride", "crash"}
LEGACY = {"transmission", "sex-on-fire", "live-rhythm"}
SEP = " · "

# условие 71: имена групп и исполнителей, которых не должно быть в названиях и подсказках
BANNED = [
    "Interpol", "Joy Division", "The Cure", "New Order", "Rammstein", "Nightwish", "Placebo", "Radiohead",
    "Smiths", "R.E.M.", "Jack White", "White Stripes", "My Bloody Valentine", "Slowdive", "Muse",
    "Royal Blood", "Hook", "Хук", "Beatles", "King Crimson", "Depeche Mode", "Deep Purple", "Doors",
    "Nick Cave", "Portishead", "Massive Attack", "Supertramp", "Ray Charles", "Whitney Houston", "a-ha",
    "Stevie Wonder", "Stranglers", "Pixies", "Bowie", "Björk", "Чайковский", "Eno", "Gorillaz", "Trio",
    "Daniel Johnston", "Animals", "Тарантино", "Tarantino", "Молчат Дома", "Transmission", "Sex on Fire",
]
_BANNED_RE = [(n, re.compile(r"(?<!\w)" + re.escape(n) + r"(?!\w)", re.IGNORECASE)) for n in BANNED]


def banned_in(text: str) -> list:
    return [n for n, rx in _BANNED_RE if rx.search(text or "")]


def _texts(v):
    """Строка или {ru, en} → список строк."""
    if isinstance(v, dict):
        return [x for x in v.values() if isinstance(x, str)]
    return [v] if isinstance(v, str) else []


def _blocks(p):
    """Все блоки движка пресета: записи дорожек, партии, мастер."""
    for s in p.get("specs", []):
        yield from (b for b in (s.get("engine") or []) if isinstance(b, dict))
    for pt in p.get("parts", []) or []:
        yield from (b for b in (pt.get("engine") or []) if isinstance(b, dict))
    yield from (b for b in (p.get("master") or []) if isinstance(b, dict))


def _deploy_scp_files():
    """worker/*-файлы из команд scp в deploy.sh (продолжения строк через \\ склеены)."""
    text = (ROOT / "deploy.sh").read_text(encoding="utf-8").replace("\\\n", " ")
    files = set()
    for line in text.splitlines():
        if re.match(r"\s*scp\b", line):
            files.update(re.findall(r"worker/([\w.\-]+)", line))
    return files


def genre_presets():
    return [b for b in presets.BUILTIN if b.get("slug") not in LEGACY]


# ---------- ТК104: готовые цепочки на воркере — копия побайтно, уезжает деплоем ----------

class TestFxPresetsOnWorker(unittest.TestCase):

    def test_tc104_worker_copy_bytes_equal_mcp(self):
        worker_copy = WORKER / "fx_presets.json"
        mcp_copy = ROOT / "internal" / "mcp" / "fx_presets.json"
        self.assertTrue(worker_copy.is_file(), "нет worker/fx_presets.json (make mcp-data)")
        self.assertEqual(worker_copy.read_bytes(), mcp_copy.read_bytes(),
                         "worker/fx_presets.json и internal/mcp/fx_presets.json разошлись — make mcp-data")

    def test_tc104_deploy_ships_fx_presets(self):
        shipped = _deploy_scp_files()
        self.assertIn("presets.py", shipped, "разбор deploy.sh не нашёл scp с файлами воркера")
        self.assertIn("fx_presets.json", shipped, "deploy.sh не увозит worker/fx_presets.json")


# ---------- ТК105: встроенные жанровые пресеты ----------

class TestBuiltinGenrePresets(unittest.TestCase):

    def test_tc105_at_least_100_genre_presets(self):
        self.assertGreaterEqual(len(presets.BUILTIN), 100)
        self.assertGreaterEqual(len(genre_presets()), 100)

    @unittest.skipUnless(_HAS_ENGINE, "нужен fx_engine (numpy/scipy — окружение воркера)")
    def test_tc105_all_pass_validate(self):
        for b in presets.BUILTIN:
            with self.subTest(slug=b.get("slug")):
                try:
                    presets.validate(dict(b), fx_engine.parse_chain)
                except presets.PresetError as e:
                    self.fail(f"{b.get('slug')}: {e}")

    def test_tc105_slugs_unique_latin(self):
        slugs = [b.get("slug") for b in presets.BUILTIN]
        self.assertEqual(len(slugs), len(set(slugs)), "slug повторяется")
        for s in slugs:
            with self.subTest(slug=s):
                self.assertIsInstance(s, str)
                self.assertRegex(s, r"^[a-z0-9-]+$")

    def test_tc105_legacy_slugs_kept(self):
        # условие 71: transmission / sex-on-fire — slug прежний (переименованы только название и подсказка)
        slugs = {b.get("slug") for b in presets.BUILTIN}
        self.assertIn("transmission", slugs)
        self.assertIn("sex-on-fire", slugs)

    def test_tc105_family_one_of_four(self):
        for b in presets.BUILTIN:
            with self.subTest(slug=b.get("slug")):
                self.assertIn(b.get("family"), FAMILIES)

    def test_tc105_name_genre_dot_variant(self):
        for b in genre_presets():
            with self.subTest(slug=b.get("slug")):
                parts = b["name"].split(SEP)
                self.assertEqual(len(parts), 2, f"название не «<Течение> · <вариант>»: {b['name']!r}")
                self.assertTrue(parts[0].strip() and parts[1].strip(), b["name"])

    def test_tc105_genres_three_variants_and_34_genres(self):
        counts = {}
        for b in genre_presets():
            g = b["name"].split(SEP)[0]
            counts[g] = counts.get(g, 0) + 1
        self.assertGreaterEqual(len(counts), 34, f"течений {len(counts)}: {sorted(counts)}")
        few = {g: n for g, n in counts.items() if n < 3}
        self.assertEqual(few, {}, "у течений меньше трёх вариантов")

    def test_tc105_genre_in_single_family(self):
        # течение — внутри своей семьи (список в UI — семья → течение → варианты)
        fam = {}
        for b in genre_presets():
            fam.setdefault(b["name"].split(SEP)[0], set()).add(b.get("family"))
        mixed = {g: f for g, f in fam.items() if len(f) > 1}
        self.assertEqual(mixed, {}, "течение в нескольких семьях")

    def test_tc105_no_vocals_records(self):
        for b in genre_presets():
            with self.subTest(slug=b.get("slug")):
                for s in b.get("specs", []):
                    self.assertNotIn("vocals", s.get("stems", []), "голос жанровый пресет не трогает")

    def test_tc105_drums_and_bass_records(self):
        for b in genre_presets():
            with self.subTest(slug=b.get("slug")):
                stems = [set(s.get("stems", [])) for s in b.get("specs", [])]
                self.assertTrue(any(st & DRUM_STEMS for st in stems), "нет записи барабанов")
                self.assertTrue(any("bass" in st for st in stems), "нет записи баса")

    def test_tc105_guitar_processing_and_levels(self):
        # условие 72: обработка гитары (дорожка guitar) и цели громкости level_db барабанов, баса, гитары
        for b in genre_presets():
            with self.subTest(slug=b.get("slug")):
                specs = b.get("specs", [])
                self.assertTrue(any("guitar" in s.get("stems", []) and
                                    any(s.get(k) not in (None, "", []) for k in ("engine", "chain", "steps"))
                                    for s in specs), "нет обработки гитары")
                lv = [set(s.get("stems", [])) for s in specs if s.get("level_db") is not None]
                self.assertTrue(any(st & DRUM_STEMS for st in lv), "нет цели громкости барабанов")
                self.assertTrue(any("bass" in st for st in lv), "нет цели громкости баса")
                self.assertTrue(any("guitar" in st for st in lv), "нет цели громкости гитары")

    def test_tc105_specs_limit(self):
        for b in presets.BUILTIN:
            with self.subTest(slug=b.get("slug")):
                self.assertLessEqual(len(b.get("specs", [])), 16)

    def test_tc105_kits_known(self):
        for b in presets.BUILTIN:
            for blk in _blocks(b):
                for k, v in blk.items():
                    if k.startswith("kit") and isinstance(v, str) and v:
                        with self.subTest(slug=b.get("slug"), key=k, kit=v):
                            self.assertIn(v.split("/")[0], KNOWN_KITS)

    def test_tc105_parts_limit_and_first_block(self):
        for b in presets.BUILTIN:
            with self.subTest(slug=b.get("slug")):
                parts = b.get("parts", []) or []
                self.assertLessEqual(len(parts), 3)
                for pt in parts:
                    eng = pt.get("engine") or []
                    self.assertTrue(eng and isinstance(eng[0], dict), pt)
                    self.assertIn(eng[0].get("type"), ("synth", "perc"))

    def test_tc105_master_or_target(self):
        for b in genre_presets():
            with self.subTest(slug=b.get("slug")):
                self.assertTrue(b.get("master") or b.get("target_lufs") is not None,
                                "нет ни мастера, ни цели громкости")

    def test_tc105_no_artist_names(self):
        for b in presets.BUILTIN:
            for field in ("name", "note"):
                for text in _texts(b.get(field)):
                    with self.subTest(slug=b.get("slug"), field=field):
                        self.assertEqual(banned_in(text), [], text)

    def test_banned_matcher_self_check(self):
        # проверка самой сверки: регистр не важен, слово целиком
        self.assertEqual(banned_in("как у the cure"), ["The Cure"])
        self.assertEqual(banned_in("r.e.m. и BOWIE"), ["R.E.M.", "Bowie"])
        self.assertEqual(banned_in("enough room, museum hall"), [])


# ---------- ТК108: миграция — колонка family ----------

OLD_SPECS = [{"stems": ["bass"], "engine": [{"type": "eq", "highpass_hz": 80}], "db": 0}]


@unittest.skipUnless(tp._HAS_WORKER_DEPS, "нужны fastapi/httpx/numpy (окружение воркера)")
class TestSoundPresetsFamilyMigration(tp._WorkerDbCase):

    def _legacy(self):
        # таблица пресетов этапа 8 (до family) и одна своя запись
        with self._conn() as c:
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
                created_at TEXT NOT NULL,
                target_lufs REAL,
                master TEXT NOT NULL DEFAULT '[]',
                parts TEXT NOT NULL DEFAULT '[]')""")
            c.execute("INSERT INTO sound_presets (name, note, specs, final, created_at) VALUES (?, ?, ?, ?, ?)",
                      ("Старый свой", "до семей", json.dumps(OLD_SPECS), "[]", "2026-10-01T00:00:00"))

    def _start(self):
        self.w.init_db()
        self.w._migrate()

    def _client(self):
        from fastapi.testclient import TestClient
        return TestClient(self.w.app, raise_server_exceptions=False)

    def _list(self):
        r = self._client().get("/sound-presets")
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()

    def test_tc108_column_added_builtins_have_family_own_empty(self):
        self._legacy()
        self._start()
        with self._conn() as c:
            cols = {r[1] for r in c.execute("PRAGMA table_info(sound_presets)")}
        self.assertIn("family", cols)
        lst = self._list()
        own = [p for p in lst if p.get("name") == "Старый свой"]
        self.assertEqual(len(own), 1, lst)
        self.assertEqual(own[0].get("family"), "")
        self.assertEqual(own[0].get("specs"), OLD_SPECS)
        built = [p for p in lst if p.get("builtin")]
        self.assertGreaterEqual(len(built), 100)
        for p in built:
            with self.subTest(name=p.get("name")):
                self.assertIn(p.get("family"), FAMILIES)

    def test_tc108_second_start_no_errors(self):
        self._legacy()
        self._start()
        before = self._list()
        self._start()
        after = self._list()
        self.assertEqual(len(after), len(before), "второй старт задвоил пресеты")

    def test_tc108_new_own_preset_family_empty(self):
        self._start()
        r = self._client().post("/sound-presets", json={"name": "Мой", "specs": OLD_SPECS})
        self.assertIn(r.status_code, (200, 201), r.text)
        self.assertEqual(r.json().get("family"), "")


if __name__ == "__main__":
    unittest.main()
