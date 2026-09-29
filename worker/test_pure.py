"""Тесты чистых функций воркера: llm, abcparse, media.encode_mp3.

Запуск: python3 -m unittest worker.test_pure (из корня репозитория)
или: cd worker && python3 -m unittest test_pure
"""
import unittest
from pathlib import Path

import abcparse
import arc
import llm


class TestAdaptPrompts(unittest.TestCase):
    """Спецификация llm.adapt_prompts: системный промпт фиксирует язык и
    требование сохранения просодии, user — исходный текст без изменений."""

    def test_system_mentions_language_and_prosody(self):
        system, user = llm.adapt_prompts("la la", "Russian")
        self.assertIn("Russian", system)
        self.assertIn("syllable", system)
        self.assertIn("[Verse]", system)  # секционные теги не переводить

    def test_user_is_source_text_trimmed(self):
        system, user = llm.adapt_prompts("  hello \n world \n", "Russian")
        self.assertEqual(user, "hello \n world")
        self.assertNotIn("hello", system)


# CI-джоба worker ставит только ruff: аудио-зависимости (numpy/soundfile/
# lameenc) есть на GPU-машине — там тест и работает, в CI пропускается.
try:
    import lameenc  # noqa: F401
    import numpy  # noqa: F401
    import soundfile  # noqa: F401
    _HAS_MP3_DEPS = True
except ImportError:
    _HAS_MP3_DEPS = False


@unittest.skipUnless(_HAS_MP3_DEPS, "нужны numpy/soundfile/lameenc (GPU-окружение)")
class TestEncodeMp3(unittest.TestCase):
    """Спецификация media.encode_mp3: каналы идут interleaved — левый сигнал
    остаётся слева, правый справа (planar-буфер давал 2x-ускорение каналов)."""

    def test_channels_interleaved(self):
        import numpy as np
        import soundfile as sf
        from media import encode_mp3
        import tempfile

        sr = 24000
        t = np.arange(sr) / sr
        # левый — тон, правый — тишина
        data = np.stack([np.sin(2 * np.pi * 440 * t), np.zeros_like(t)], axis=1).astype("float32")
        with tempfile.TemporaryDirectory() as td:
            wav = Path(td) / "in.wav"
            mp3 = Path(td) / "out.mp3"
            sf.write(str(wav), data, sr)
            encode_mp3(wav, mp3)
            out, _ = sf.read(str(mp3), dtype="float32")
        self.assertEqual(out.shape[1], 2)
        rms_l = float(np.sqrt((out[:, 0] ** 2).mean()))
        rms_r = float(np.sqrt((out[:, 1] ** 2).mean()))
        self.assertGreater(rms_l, 0.2)   # тон в левом
        self.assertLess(rms_r, 0.02)     # справа тишина


ABC = """X:1
T:arc test
M:4/4
L:1/16
Q:1/4=100
V: Vocal clef=treble name="Vocal Melody" snm="Vocal"
V: Ins clef=treble name="Inst." snm="Ins."
K:Dm
% intro
V: Vocal
z16|
V: Ins
d8z8|
% verse
V: Vocal
"Dm"d2a2g2e2|
V: Ins
d4d4d4d4|
% chorus
V: Vocal
"Dm"d2a2g2e2|
V: Ins
e4e4e4e4|
% interlude
V: Ins
f4f4f4f4|
% chorus
V: Vocal
"Dm"d2a2g2e2|
V: Ins
g4g4g4g4|
"""


class TestApplyArc(unittest.TestCase):
    """Спецификация arc.apply_arc: дуги темпа по секциям от базового Q,
    burst поднимает голос на октаву в финальном припеве."""

    def test_no_arc_unchanged(self):
        self.assertEqual(arc.apply_arc(ABC, ""), ABC)
        self.assertEqual(arc.apply_arc(ABC, "nope"), ABC)

    def test_burst_tempos_and_octave(self):
        out = arc.apply_arc(ABC, "burst")
        qs = [ln for ln in out.split("\n") if ln.startswith("Q:1/4=")]
        self.assertIn("Q:1/4=55", qs)   # intro 0.55
        self.assertIn("Q:1/4=72", qs)   # interlude 0.72
        self.assertIn("Q:1/4=117", qs)  # финальный chorus 1.17
        # голос в финальном припеве поднялся: d2 -> d'2
        self.assertIn("d'2a'2g'2e'2", out)
        # ранние секции не транспонированы (куплет + первый припев)
        self.assertEqual(out.count("d2a2g2e2"), 2)

    def test_build_lifts_only_finale(self):
        out = arc.apply_arc(ABC, "build")
        qs = [ln for ln in out.split("\n") if ln.startswith("Q:1/4=")]
        self.assertIn("Q:1/4=85", qs)   # intro 0.85
        self.assertIn("Q:1/4=112", qs)  # финальный chorus 1.12
        self.assertNotIn("d'2", out)    # голос не трогаем

    def test_style_suffix(self):
        s = arc.style_with_arc("post-punk", "burst")
        self.assertTrue(s.startswith("post-punk, "))
        self.assertIn("soaring", s)
        # повторное применение не дублирует
        self.assertEqual(arc.style_with_arc(s, "burst"), s)


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

    def test_timeline_per_voice(self):
        # spec: у каждого голоса свой ход часов — голоса звучат одновременно,
        # хотя в тексте их такты идут последовательными блоками
        bars = self.r["bars"]
        self.assertEqual((bars[0]["start_sec"], bars[0]["end_sec"]), (0.0, 1.25))   # Vocal 1
        self.assertEqual((bars[1]["start_sec"], bars[1]["end_sec"]), (1.25, 2.75))  # Vocal 2
        self.assertEqual((bars[2]["start_sec"], bars[2]["end_sec"]), (0.0, 1.25))   # Ins 1 — параллельно
        self.assertEqual((bars[3]["start_sec"], bars[3]["end_sec"]), (1.25, 2.75))  # Ins 2
        self.assertEqual(self.r["duration_sec"], 2.75)

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


# мультипауза Z<n> = n тактов тишины (карточка internal-studio-insert-sync, п.8):
# 4/4, L:1/16, Q=120 → такт 2 с. Секция A: Vocal 4 такта нот, Ins — Z4;
# секция B: оба голоса по 2 такта нот. Оба голоса заканчиваются на 12 с.
MULTIREST_ABC = """\
X:1
M:4/4
L:1/16
Q:1/4=120
V: Vocal clef=treble name="Vocal Melody" snm="Vocal"
V: Ins clef=treble name="Ins Melody" snm="Inst."
K:Em
% sectionA
V: Vocal
"Em"B4B2B2B4B2B2|"C"c2c2c2c2"D"B2A2A4|"Em"e4e4e4e4|"G"d4d4d4d4|
V: Ins
Z4|
% sectionB
V: Vocal
"Am"a4a4a4a4|"C"g4g4g4g4|
V: Ins
E4E4E4E4|F4F4F4F4|
"""


def _bars_of(result: dict, voice: str) -> list:
    """Такты голоса: голос такта — ключ в voices или rests (как на ролле)."""
    return [b for b in result["bars"]
            if voice in (b.get("voices") or {}) or voice in (b.get("rests") or {})]


def _single(voice: str, body: str) -> dict:
    return abcparse.parse_abc(
        "X:1\nM:4/4\nL:1/16\nQ:1/4=120\nK:C\n% s\nV: " + voice + "\n" + body + "\n")


class TestParseAbcMultiRest(unittest.TestCase):
    """Спецификация: Z<n> — n целых тактов паузы, Z — один такт; время голоса
    идёт по всем n тактам (иначе квадратики ролла уезжают раньше звука)."""

    def setUp(self):
        self.r = abcparse.parse_abc(MULTIREST_ABC)

    def test_ins_has_six_bars(self):
        self.assertEqual(len(_bars_of(self.r, "Ins")), 6)
        self.assertEqual(len(_bars_of(self.r, "Vocal")), 6)

    def test_ins_ends_with_vocal(self):
        ins = _bars_of(self.r, "Ins")
        voc = _bars_of(self.r, "Vocal")
        self.assertAlmostEqual(ins[-1]["end_sec"], 12.0, places=6)
        self.assertAlmostEqual(voc[-1]["end_sec"], 12.0, places=6)
        self.assertAlmostEqual(self.r["duration_sec"], 12.0, places=6)

    def test_multirest_bars_are_whole_bars(self):
        ins = _bars_of(self.r, "Ins")
        spans = [(round(b["start_sec"], 6), round(b["end_sec"], 6)) for b in ins]
        self.assertEqual(spans, [(0.0, 2.0), (2.0, 4.0), (4.0, 6.0), (6.0, 8.0),
                                 (8.0, 10.0), (10.0, 12.0)])

    def test_multirest_bars_are_rests(self):
        ins = _bars_of(self.r, "Ins")
        for b in ins[:4]:
            self.assertEqual((b.get("voices") or {}).get("Ins", 0), 0)
            self.assertEqual(b["section"], "sectionA")
        # такты с нотами после мультипаузы — ноты считаются
        self.assertEqual(ins[4]["voices"]["Ins"], 4)
        self.assertEqual(ins[4]["section"], "sectionB")

    def test_bare_z_is_one_bar(self):
        r = _single("Ins", "Z|")
        bars = _bars_of(r, "Ins")
        self.assertEqual(len(bars), 1)
        self.assertAlmostEqual(bars[0]["end_sec"], 2.0, places=6)

    def test_z2_is_two_bars(self):
        r = _single("Ins", "Z2|")
        bars = _bars_of(r, "Ins")
        self.assertEqual(len(bars), 2)
        self.assertAlmostEqual(bars[-1]["end_sec"], 4.0, places=6)


if __name__ == "__main__":
    unittest.main()
