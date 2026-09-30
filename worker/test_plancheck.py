"""Тесты чистых функций проверки изменённого плана (карточка internal-plan-check).

Модуль plancheck чистый — без fastapi/numpy, тесты идут в системном python3.
Запуск: cd worker && python3 -m unittest test_plancheck
"""
import re
import unittest

import plancheck

# 4/4, L:1/16, Q=120 → такт = 16 единиц = 2 с. По 6 тактов у Vocal и Ins,
# план кончается на 12 с. Вершина Vocal — «c» (ступень 7), потолок — «e».
HEADER = """\
X:1
M:4/4
L:1/16
Q:1/4=120
V: Vocal clef=treble name="Vocal Melody" snm="Vocal"
V: Ins clef=treble name="Ins Melody" snm="Inst."
K:C
"""

VOCAL_BARS = ['"C"c4B4A4G4', '"F"A4A4c4c4', '"G"B4B4G4G4',
              '"C"c16', '"Am"A4A4G4G4', '"C"c16']
INS_BARS = ["C4E4G4E4", "F4A4c4A4", "G4B4d4B4", "C16", "A,4C4E4C4", "C16"]


def _plan(vocal=VOCAL_BARS, ins=INS_BARS) -> str:
    """План из двух секций: такты 1–3 — verse, 4–6 — chorus (голос
    продолжается в следующей секции новой строкой)."""
    v1, v2 = vocal[:3], vocal[3:]
    i1, i2 = ins[:3], ins[3:]
    out = HEADER + "% verse\nV: Vocal\n" + "|".join(v1) + "|\n"
    out += "V: Ins\n" + "|".join(i1) + "|\n"
    if v2 or i2:
        out += "% chorus\n"
        if v2:
            out += "V: Vocal\n" + "|".join(v2) + "|\n"
        if i2:
            out += "V: Ins\n" + "|".join(i2) + "|\n"
    return out


BASE = _plan()


def _replace(bars, i, text):
    b = list(bars)
    b[i] = text
    return b


def _squash(s: str) -> str:
    return re.sub(r"\s+", "", s)


class TestNoteStep(unittest.TestCase):
    """Ступень — по букве и октаве; знаки ключа и акциденты не влияют."""

    def test_uppercase_octave(self):
        self.assertEqual(plancheck.note_step("C"), 0)
        self.assertEqual(plancheck.note_step("D"), 1)
        self.assertEqual(plancheck.note_step("B"), 6)

    def test_lowercase_octave(self):
        self.assertEqual(plancheck.note_step("c"), 7)
        self.assertEqual(plancheck.note_step("e"), 9)

    def test_octave_marks(self):
        self.assertEqual(plancheck.note_step("c'"), 14)
        self.assertEqual(plancheck.note_step("C,"), -7)
        self.assertEqual(plancheck.note_step("c''"), 21)
        self.assertEqual(plancheck.note_step("C,,"), -14)

    def test_accidental_ignored(self):
        self.assertEqual(plancheck.note_step("^f"), 10)
        self.assertEqual(plancheck.note_step("_B"), 6)
        self.assertEqual(plancheck.note_step("=c"), 7)


class TestStepNote(unittest.TestCase):
    def test_examples(self):
        self.assertEqual(plancheck.step_note(9), "e")
        self.assertEqual(plancheck.step_note(14), "c'")
        self.assertEqual(plancheck.step_note(-7), "C,")
        self.assertEqual(plancheck.step_note(0), "C")
        self.assertEqual(plancheck.step_note(6), "B")
        self.assertEqual(plancheck.step_note(7), "c")

    def test_roundtrip(self):
        for step in range(-14, 22):
            self.assertEqual(plancheck.note_step(plancheck.step_note(step)), step, step)


class TestVoiceBars(unittest.TestCase):
    def test_short_voice_names_and_counts(self):
        vb = plancheck.voice_bars(BASE)
        self.assertEqual(len(vb["Vocal"]), 6)
        self.assertEqual(len(vb["Ins"]), 6)

    def test_lines_of_voice_joined_in_plan_order(self):
        # такты второй секции идут за тактами первой, а не отдельным голосом
        vb = plancheck.voice_bars(BASE)
        self.assertIn("c4B4A4G4", _squash(vb["Vocal"][0]))
        self.assertIn("c16", _squash(vb["Vocal"][3]))
        self.assertIn("A4A4G4G4", _squash(vb["Vocal"][4]))
        self.assertIn("A,4C4E4C4", _squash(vb["Ins"][4]))

    def test_trailing_bar_line_is_not_a_bar(self):
        vb = plancheck.voice_bars(HEADER + "V: Vocal\nc16|\nd16|\n")
        self.assertEqual(len(vb["Vocal"]), 2)

    def test_multirest_expands(self):
        vb = plancheck.voice_bars(
            HEADER + "% intro\nV: Vocal\nc16|c16|c16|c16|\nV: Ins\nZ4|\n"
            "% verse\nV: Vocal\nd16|\nV: Ins\nE16|\n")
        self.assertEqual(len(vb["Ins"]), 5)
        self.assertEqual(len(vb["Vocal"]), 5)
        self.assertIn("E16", _squash(vb["Ins"][4]))

    def test_bare_z_is_one_bar(self):
        vb = plancheck.voice_bars(HEADER + "V: Ins\nZ|\n")
        self.assertEqual(len(vb["Ins"]), 1)

    def test_empty(self):
        vb = plancheck.voice_bars("")
        self.assertEqual({k: v for k, v in vb.items() if v}, {})


class TestVocalTop(unittest.TestCase):
    def test_top_of_vocal_only(self):
        # у Ins есть «d» выше, но считается только Vocal
        self.assertEqual(plancheck.vocal_top(BASE), 7)

    def test_chords_are_not_notes(self):
        # "G" — аккорд (ступень 4), нота в такте — только C (0)
        abc = HEADER + 'V: Vocal\n"G"C16|\n'
        self.assertEqual(plancheck.vocal_top(abc), 0)

    def test_voice_name_case_insensitive(self):
        abc = HEADER.replace("V: Vocal", "V: LeadVOCAL") + "V: LeadVOCAL\ne16|\n"
        self.assertEqual(plancheck.vocal_top(abc), 9)

    def test_octave_and_accidental(self):
        abc = HEADER + "V: Vocal\nc8^c'8|\n"
        self.assertEqual(plancheck.vocal_top(abc), 14)

    def test_no_notes_none(self):
        self.assertIsNone(plancheck.vocal_top(HEADER + "V: Vocal\nz16|Z2|\n"))
        self.assertIsNone(plancheck.vocal_top(HEADER + "V: Ins\nc16|\n"))
        self.assertIsNone(plancheck.vocal_top(""))


class TestPlanDiffSame(unittest.TestCase):
    def test_same_plan_no_changes(self):
        d = plancheck.plan_diff(BASE, BASE)
        self.assertEqual(d["changed_total"], 0)
        self.assertEqual(d["changed"], [])
        self.assertEqual(d["warnings"], [])
        self.assertEqual(d["bars"], {"Vocal": [6, 6], "Ins": [6, 6]})
        self.assertEqual(d["duration"], [12.0, 12.0])

    def test_same_plan_with_from_sec_no_warnings(self):
        d = plancheck.plan_diff(BASE, BASE, from_sec=6.0)
        self.assertEqual(d["warnings"], [])

    def test_ceiling_block(self):
        d = plancheck.plan_diff(BASE, BASE)
        self.assertEqual(d["ceiling"], {"top": "c", "ceiling": "e", "new_top": "c"})

    def test_ceiling_none_without_vocal_notes(self):
        abc = HEADER + "V: Vocal\nz16|\nV: Ins\nc16|\n"
        d = plancheck.plan_diff(abc, abc)
        self.assertIsNone(d["ceiling"]["top"])
        self.assertIsNone(d["ceiling"]["ceiling"])
        self.assertIsNone(d["ceiling"]["new_top"])


class TestPlanDiffChangedBar(unittest.TestCase):
    def test_one_vocal_bar_changed(self):
        # третий такт Vocal (4–6 с): B → d, «выше» в пределах потолка
        new = _plan(vocal=_replace(VOCAL_BARS, 2, '"G"d4d4G4G4'))
        d = plancheck.plan_diff(BASE, new)
        self.assertEqual(d["changed_total"], 1)
        self.assertEqual(len(d["changed"]), 1)
        c = d["changed"][0]
        self.assertEqual(c["voice"], "Vocal")
        self.assertAlmostEqual(c["start"], 4.0, places=2)
        self.assertAlmostEqual(c["end"], 6.0, places=2)
        self.assertIn("B4B4G4G4", _squash(c["before"]))
        self.assertIn("d4d4G4G4", _squash(c["after"]))
        self.assertEqual(d["bars"], {"Vocal": [6, 6], "Ins": [6, 6]})

    def test_bar_numbers_distinguish_bars(self):
        # номер такта — позиция в голосе: у разных тактов он разный и растёт
        new = _plan(vocal=_replace(_replace(VOCAL_BARS, 0, '"C"c4c4c4c4'), 4, '"Am"A16'))
        d = plancheck.plan_diff(BASE, new)
        bars = [c["bar"] for c in d["changed"]]
        self.assertEqual(len(bars), 2)
        self.assertLess(bars[0], bars[1])
        self.assertEqual(bars[1] - bars[0], 4)

    def test_ins_change_reported_with_voice(self):
        new = _plan(ins=_replace(INS_BARS, 5, "C8G8"))
        d = plancheck.plan_diff(BASE, new)
        self.assertEqual(d["changed_total"], 1)
        c = d["changed"][0]
        self.assertEqual(c["voice"], "Ins")
        self.assertAlmostEqual(c["start"], 10.0, places=2)
        self.assertAlmostEqual(c["end"], 12.0, places=2)

    def test_higher_within_ceiling_no_warning(self):
        new = _plan(vocal=_replace(VOCAL_BARS, 2, '"G"d4d4G4G4'))
        d = plancheck.plan_diff(BASE, new)
        self.assertFalse(any("потолок" in w for w in d["warnings"]), d["warnings"])
        self.assertEqual(d["ceiling"]["new_top"], "d")

    def test_note_on_ceiling_no_warning(self):
        # «e» = вершина+2 — это ещё потолок, не выше
        new = _plan(vocal=_replace(VOCAL_BARS, 3, '"C"e16'))
        d = plancheck.plan_diff(BASE, new)
        self.assertFalse(any("потолок" in w for w in d["warnings"]), d["warnings"])


class TestPlanDiffCeiling(unittest.TestCase):
    def test_octave_up_warns(self):
        # финальный такт Vocal (10–12 с): c → c' — октава вверх
        new = _plan(vocal=_replace(VOCAL_BARS, 5, "\"C\"c'16"))
        d = plancheck.plan_diff(BASE, new)
        self.assertEqual(d["ceiling"], {"top": "c", "ceiling": "e", "new_top": "c'"})
        warns = [w for w in d["warnings"] if "потолок" in w]
        self.assertEqual(len(warns), 1, d["warnings"])
        bar = d["changed"][0]["bar"]
        # в тексте — номер такта или его время
        self.assertTrue(re.search(rf"\b{bar}\b", warns[0]) or "10" in warns[0], warns[0])

    def test_high_ins_does_not_warn(self):
        new = _plan(ins=_replace(INS_BARS, 5, "c'16"))
        d = plancheck.plan_diff(BASE, new)
        self.assertFalse(any("потолок" in w for w in d["warnings"]), d["warnings"])


class TestPlanDiffCut(unittest.TestCase):
    def test_cut_four_bars(self):
        # вырезаны такты 2–5 у обоих голосов: осталось по 2 такта, 4 с
        new = _plan(vocal=[VOCAL_BARS[0], VOCAL_BARS[5]], ins=[INS_BARS[0], INS_BARS[5]])
        d = plancheck.plan_diff(BASE, new)
        self.assertEqual(d["bars"], {"Vocal": [6, 2], "Ins": [6, 2]})
        self.assertEqual(d["duration"], [12.0, 4.0])
        self.assertTrue(any("тактов" in w for w in d["warnings"]), d["warnings"])
        self.assertGreater(d["changed_total"], 0)

    def test_missing_bars_have_empty_text(self):
        # в новом плане только 2 такта Vocal: такты 3–6 есть лишь в старом
        new = _plan(vocal=VOCAL_BARS[:2])
        d = plancheck.plan_diff(BASE, new)
        gone = [c for c in d["changed"] if c["voice"] == "Vocal" and c["after"] == ""]
        self.assertEqual(len(gone), 4)
        self.assertTrue(all(c["before"] for c in gone))

    def test_added_bars_have_empty_before(self):
        new = _plan(vocal=VOCAL_BARS + ['"C"c16'], ins=INS_BARS + ["C16"])
        d = plancheck.plan_diff(BASE, new)
        self.assertEqual(d["bars"], {"Vocal": [6, 7], "Ins": [6, 7]})
        added = [c for c in d["changed"] if c["before"] == ""]
        self.assertEqual(len(added), 2)
        voc = [c for c in added if c["voice"] == "Vocal"][0]
        self.assertAlmostEqual(voc["start"], 12.0, places=2)
        self.assertAlmostEqual(voc["end"], 14.0, places=2)
        self.assertEqual(d["duration"], [12.0, 14.0])


class TestPlanDiffFromSec(unittest.TestCase):
    def test_change_before_mark_warns(self):
        # такт 1 Vocal (0–2 с) изменён, продолжение — с 6 с
        new = _plan(vocal=_replace(VOCAL_BARS, 0, '"C"c4c4c4c4'))
        d = plancheck.plan_diff(BASE, new, from_sec=6.0)
        self.assertTrue(any("до отметки" in w for w in d["warnings"]), d["warnings"])

    def test_change_after_mark_no_warning(self):
        new = _plan(vocal=_replace(VOCAL_BARS, 4, '"Am"A16'))
        d = plancheck.plan_diff(BASE, new, from_sec=6.0)
        self.assertFalse(any("до отметки" in w for w in d["warnings"]), d["warnings"])

    def test_bar_crossing_mark_not_before(self):
        # такт 3 (4–6 с) целиком раньше 6 с → предупреждение; такт 4 (6–8 с) — нет
        new4 = _plan(vocal=_replace(VOCAL_BARS, 3, '"C"A16'))
        d = plancheck.plan_diff(BASE, new4, from_sec=7.0)
        self.assertFalse(any("до отметки" in w for w in d["warnings"]), d["warnings"])
        new3 = _plan(vocal=_replace(VOCAL_BARS, 2, '"G"A16'))
        d = plancheck.plan_diff(BASE, new3, from_sec=6.0)
        self.assertTrue(any("до отметки" in w for w in d["warnings"]), d["warnings"])

    def test_no_from_sec_no_warning(self):
        new = _plan(vocal=_replace(VOCAL_BARS, 0, '"C"c4c4c4c4'))
        d = plancheck.plan_diff(BASE, new)
        self.assertEqual(d["warnings"], [])


class TestPlanDiffMultiRest(unittest.TestCase):
    def test_z4_counts_as_four_bars(self):
        old = HEADER + "V: Vocal\nc16|c16|c16|c16|\nV: Ins\nZ4|\n"
        new = HEADER + "V: Vocal\nc16|c16|c16|c16|\nV: Ins\nZ4|\n"
        d = plancheck.plan_diff(old, new)
        self.assertEqual(d["bars"], {"Vocal": [4, 4], "Ins": [4, 4]})
        self.assertEqual(d["duration"], [8.0, 8.0])
        self.assertEqual(d["changed_total"], 0)

    def test_note_in_place_of_rest_is_one_change(self):
        old = HEADER + "V: Ins\nZ4|\n"
        new = HEADER + "V: Ins\nZ3|C16|\n"
        d = plancheck.plan_diff(old, new)
        self.assertEqual(d["bars"]["Ins"], [4, 4])
        self.assertEqual(d["changed_total"], 1)
        self.assertAlmostEqual(d["changed"][0]["start"], 6.0, places=2)


class TestPlanDiffLimit(unittest.TestCase):
    def test_changed_capped_at_64_with_total(self):
        old = HEADER + "V: Vocal\n" + "c16|" * 70 + "\n"
        new = HEADER + "V: Vocal\n" + "B16|" * 70 + "\n"
        d = plancheck.plan_diff(old, new)
        self.assertEqual(d["changed_total"], 70)
        self.assertEqual(len(d["changed"]), 64)


if __name__ == "__main__":
    unittest.main()


class TestPlanDiffBroken(unittest.TestCase):
    """Битый план — ValueError (эндпоинт отдаёт 422), а не 500 и не «0 тактов»."""

    def test_garbage_without_bars(self):
        with self.assertRaises(ValueError):
            plancheck.plan_diff(BASE, "garbage")

    def test_zero_meter_or_unit(self):
        for bad in (BASE.replace("M:4/4", "M:0/0"), BASE.replace("L:1/16", "L:1/0")):
            with self.assertRaises(ValueError):
                plancheck.plan_diff(BASE, bad)


class TestPlanDiffLimits(unittest.TestCase):
    """Кривой ввод не съедает память/время воркера: лимиты → ValueError (422)."""

    def test_huge_multirest(self):
        with self.assertRaises(ValueError):
            plancheck.plan_diff(BASE, BASE.replace("|\n% chorus", "|Z999999999|\n% chorus", 1))

    def test_too_long_plan(self):
        with self.assertRaises(ValueError):
            plancheck.plan_diff(BASE, BASE + "%" + "x" * plancheck.ABC_MAX_CHARS)

    def test_many_accidentals_fast(self):
        import time
        t0 = time.time()
        plancheck.plan_diff(BASE, BASE.replace('"C"c16', '"C"c16' + "=" * 40000, 1))
        self.assertLess(time.time() - t0, 2.0)
