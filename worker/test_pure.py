"""Тесты чистых функций воркера: llm.strip_md, abcparse.parse_abc.

Запуск: python3 -m unittest worker.test_pure (из корня репозитория)
или: cd worker && python3 -m unittest test_pure
"""
import unittest

import abcparse
import llm


class TestStripMd(unittest.TestCase):
    def test_removes_code_fences(self):
        self.assertEqual(llm.strip_md("```text\nкуплет\n```"), "куплет")

    def test_removes_fences_with_lang(self):
        self.assertEqual(llm.strip_md("```python\nprint(1)\n```"), "print(1)")

    def test_plain_text_untouched(self):
        self.assertEqual(llm.strip_md("  обычный текст  "), "обычный текст")

    def test_inner_fences_kept(self):
        # spec: срезаются только заборы в начале/конце ответа модели
        self.assertEqual(llm.strip_md("до ``` внутрь ``` после"), "до ``` внутрь ``` после")

    def test_empty(self):
        self.assertEqual(llm.strip_md(""), "")


SAMPLE_ABC = """\
X:1
T:demo
M:4/4
L:1/8
Q:1/4=120
V:Vocal
% verse
"Am" A2 B c2 | "D" z4 d2 |
V:Ins
E2 F G2 | A4 z2 |
K:G
"""


class TestParseAbc(unittest.TestCase):
    def setUp(self):
        self.r = abcparse.parse_abc(SAMPLE_ABC)

    def test_header_fields(self):
        self.assertEqual(self.r["tempo_bpm"], 120.0)
        self.assertEqual(self.r["key"], "G")
        self.assertEqual(self.r["meter"], "4/4")

    def test_voice_order(self):
        self.assertEqual(self.r["voice_order"], ["Vocal", "Ins"])

    def test_bars_counted_by_pipes(self):
        # spec: каждый | закрывает такт — по 2 такта на голос
        self.assertEqual(len(self.r["bars"]), 4)
        self.assertEqual([b["idx"] for b in self.r["bars"]], [0, 1, 2, 3])

    def test_chords_extracted(self):
        self.assertEqual(self.r["bars"][0]["chords"], ["Am"])
        self.assertEqual(self.r["bars"][1]["chords"], ["D"])

    def test_rests_counted(self):
        # второй такт вокала: z4 → 4 единицы паузы
        self.assertEqual(self.r["bars"][1]["rests"]["Vocal"], 4)

    def test_section_comment(self):
        self.assertEqual(self.r["bars"][0]["section"], "verse")

    def test_timeline_monotonic_and_consistent(self):
        # spec: такты идут подряд без дыр, end[i] == start[i+1]
        bars = self.r["bars"]
        for a, b in zip(bars, bars[1:], strict=False):
            self.assertEqual(a["end_sec"], b["start_sec"])
        self.assertEqual(self.r["duration_sec"], bars[-1]["end_sec"])

    def test_chords_not_counted_as_notes(self):
        # spec: "Am" — аннотация, в такте 3 ноты: A2 B c2
        self.assertEqual(self.r["bars"][0]["voices"]["Vocal"], 3)

    def test_timing_math(self):
        # M:4/4, L:1/8, Q=120: единица = 0.25 с; такт A2 B c2 = 5 единиц = 1.25 с
        first = self.r["bars"][0]
        self.assertEqual(first["start_sec"], 0.0)
        self.assertEqual(first["end_sec"], 1.25)

    def test_empty_input(self):
        r = abcparse.parse_abc("")
        self.assertEqual(r["bars"], [])
        self.assertEqual(r["duration_sec"], 0.0)
        self.assertEqual(r["tempo_bpm"], 120.0)  # дефолт


if __name__ == "__main__":
    unittest.main()
