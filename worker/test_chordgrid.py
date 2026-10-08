"""Тесты карточки internal-own-track, этап 4 «Синты»: сетка аккордов трека —
чистый модуль chordgrid.py (условие 21, ТК44) и эндпоинт GET /jobs/{id}/chord_grid (ТК45).

Контракт (из карточки и контракта задачи; реализацию не читали):
- chord_pcs(name) → классы высот аккорда от основного тона: основной тон, терция
  (у sus — секунда/кварта), квинта, (септима); «Dm» → [2, 5, 9], «Bb» → [10, 2, 5];
  нераспознанный → None. Аккорды: мажор, m, dim, 7, maj7, m7, sus2, sus4, с b/#;
- plan_bars(abc_text) → [{chord, section}] по тактам плана: аккорд такта — первый
  аккорд такта голоса с аккордами, такт без аккорда — прежний (до первого — None);
  секции — комментарии % плана (как в abcparse.parse_abc);
- best_shift(plan_chords, beat_chroma (nbeats, 12), beats_per_bar=4, shifts=range(-8, 17))
  → (сдвиг в тактах, фаза доли 0..3): такт плана k совпадает с тактом звука,
  начинающимся с доли phase + (k + shift)·4;
- GET /jobs/{id}/chord_grid → {bpm, bars: [{start, end, chord, section}]}; нет плана
  (score.abc) → 404; нет долей → 422.

Запуск: cd worker && python3 -m unittest test_chordgrid -v
Чистые классы требуют только numpy; эндпоинт — окружение воркера (fastapi/librosa/soundfile).
"""
import unittest

try:
    import numpy as np
    _HAS_NP = True
except ImportError:
    _HAS_NP = False

_SKIP_NP = "нужен numpy"


def _cg():
    # импорт внутри теста: отсутствие модуля роняет тесты, а не пропускает
    import chordgrid
    return chordgrid


# ---------- chord_pcs: классы высот аккорда ----------

class TestChordPcs(unittest.TestCase):
    """Условие 21/22: разбор имени аккорда (тот же набор, что во фронте chordPcs)."""

    def test_examples_from_contract(self):
        cg = _cg()
        self.assertEqual(cg.chord_pcs("Dm"), [2, 5, 9])
        self.assertEqual(cg.chord_pcs("Bb"), [10, 2, 5])

    def test_major_minor_with_accidentals(self):
        cg = _cg()
        cases = {
            "C": [0, 4, 7],
            "A": [9, 1, 4],
            "F#m": [6, 9, 1],
            "Eb": [3, 7, 10],
            "C#": [1, 5, 8],
            "Am": [9, 0, 4],
        }
        for name, want in cases.items():
            with self.subTest(chord=name):
                self.assertEqual(cg.chord_pcs(name), want)

    def test_other_chord_kinds(self):
        cg = _cg()
        cases = {
            "Bdim": [11, 2, 5],
            "G7": [7, 11, 2, 5],
            "Cmaj7": [0, 4, 7, 11],
            "Am7": [9, 0, 4, 7],
            "Dsus2": [2, 4, 9],
            "Dsus4": [2, 7, 9],
        }
        for name, want in cases.items():
            with self.subTest(chord=name):
                self.assertEqual(cg.chord_pcs(name), want)

    def test_unknown_is_none(self):
        cg = _cg()
        for name in ("Xyz", "", "m"):
            with self.subTest(chord=name):
                self.assertIsNone(cg.chord_pcs(name))


# ---------- plan_bars: аккорды и секции по тактам плана ----------

# 8 тактов Dm C Bb A ×2: куплет и припев, аккорды в голосе Ins
_PLAN8 = (
    "X:1\nM:4/4\nL:1/16\nQ:1/4=120\nK:Dm\n"
    "% verse\n"
    "V: Ins\n"
    "\"Dm\"D16|\"C\"C16|\"Bb\"B,16|\"A\"A,16|\n"
    "% chorus\n"
    "\"Dm\"D16|\"C\"C16|\"Bb\"B,16|\"A\"A,16|\n"
)
PLAN8_CHORDS = ["Dm", "C", "Bb", "A", "Dm", "C", "Bb", "A"]


class TestPlanBars(unittest.TestCase):
    """ТК44 (часть плана): аккорд такта, наследование, секции."""

    def test_tk44_chords_and_sections(self):
        bars = _cg().plan_bars(_PLAN8)
        self.assertEqual([b["chord"] for b in bars], PLAN8_CHORDS)
        self.assertEqual([b["section"] for b in bars], ["verse"] * 4 + ["chorus"] * 4)

    def test_tk44_bar_without_chord_inherits_previous(self):
        abc = ("X:1\nM:4/4\nL:1/16\nQ:1/4=120\nK:C\n% verse\nV: Ins\n"
               "C16|\"F\"F16|F16|G16|\"G\"G16|\n")
        bars = _cg().plan_bars(abc)
        # до первого аккорда — None, дальше прежний
        self.assertEqual([b["chord"] for b in bars], [None, "F", "F", "F", "G"])

    def test_first_chord_of_bar(self):
        abc = ("X:1\nM:4/4\nL:1/16\nQ:1/4=120\nK:C\n% verse\nV: Ins\n"
               "\"Am\"A8\"F\"F8|G16|\n")
        bars = _cg().plan_bars(abc)
        self.assertEqual([b["chord"] for b in bars], ["Am", "Am"])

    def test_chords_from_voice_with_chords(self):
        # голоса в плане идут блоками: Vocal без аккордов, Ins — с аккордами;
        # такты плана — такты голоса с аккордами (4, а не 8)
        abc = ("X:1\nM:4/4\nL:1/16\nQ:1/4=120\nK:Dm\n% verse\n"
               "V: Vocal\nd4c4B4A4|d16|c16|A16|\n"
               "V: Ins\n\"Dm\"D16|\"C\"C16|\"Bb\"B,16|\"A\"A,16|\n")
        bars = _cg().plan_bars(abc)
        self.assertEqual([b["chord"] for b in bars], ["Dm", "C", "Bb", "A"])

    def test_empty_plan(self):
        self.assertEqual(_cg().plan_bars("X:1\nM:4/4\nL:1/16\nK:C\n"), [])


# ---------- best_shift: сдвиг плана по хроме ----------

def _chroma_for(plan_chords, shift, phase, nbeats, seed=0, noise=0.25):
    """Хрома по долям: такт плана k лежит на долях phase + (k + shift)·4 … +4
    (ноты аккорда — 1), остальные доли — шум (вступление/хвост трека без аккордов плана)."""
    pcs = {"Dm": [2, 5, 9], "C": [0, 4, 7], "Bb": [10, 2, 5], "A": [9, 1, 4]}
    rng = np.random.default_rng(seed)
    ch = noise * rng.random((nbeats, 12))
    for k, name in enumerate(plan_chords):
        b0 = phase + (k + shift) * 4
        for b in range(b0, b0 + 4):
            if 0 <= b < nbeats:
                ch[b] = 0.05 * rng.random(12)
                ch[b, pcs[name]] = 1.0
    return ch


@unittest.skipUnless(_HAS_NP, _SKIP_NP)
class TestBestShift(unittest.TestCase):
    """ТК44: хрома звука сдвинута на 2 такта и 1 долю → (2, 1)."""

    def test_tk44_shift_2_bars_1_beat(self):
        ch = _chroma_for(PLAN8_CHORDS, 2, 1, nbeats=1 + 4 * 14)
        got = _cg().best_shift(PLAN8_CHORDS, ch)
        self.assertEqual(tuple(int(v) for v in got), (2, 1))

    def test_each_phase_found(self):
        for phase in range(4):
            with self.subTest(phase=phase):
                ch = _chroma_for(PLAN8_CHORDS, 3, phase, nbeats=4 * 14, seed=phase)
                got = _cg().best_shift(PLAN8_CHORDS, ch)
                self.assertEqual(tuple(int(v) for v in got), (3, phase))

    def test_zero_shift(self):
        plan = ["Dm", "C", "Bb", "A", "C", "Dm", "A", "Bb"]
        ch = _chroma_for(plan, 0, 0, nbeats=4 * 12)
        self.assertEqual(tuple(int(v) for v in _cg().best_shift(plan, ch)), (0, 0))

    def test_negative_shift(self):
        # звук начинается с середины плана: такты плана 0–2 до начала звука; «наибольшее
        # совпадение» — по всем тактам плана, попавшим в звук (5 совпавших тактов больше,
        # чем 1–2 такта случайного частичного наложения)
        plan = ["Dm", "C", "Bb", "A", "C", "Dm", "A", "Bb"]
        ch = _chroma_for(plan, -3, 2, nbeats=4 * 10, seed=5)
        self.assertEqual(tuple(int(v) for v in _cg().best_shift(plan, ch)), (-3, 2))

    def test_bars_without_chord_ignored(self):
        # None в плане (такт без аккорда до первого) не мешает подбору
        plan = [None] + PLAN8_CHORDS
        ch = _chroma_for(PLAN8_CHORDS, 3, 1, nbeats=4 * 16, seed=7)  # PLAN8 с такта 1 плана
        self.assertEqual(tuple(int(v) for v in _cg().best_shift(plan, ch)), (2, 1))

    def test_shift_within_given_range(self):
        ch = _chroma_for(PLAN8_CHORDS, 2, 1, nbeats=1 + 4 * 14)
        shift, phase = _cg().best_shift(PLAN8_CHORDS, ch, shifts=range(-1, 2))
        self.assertIn(int(shift), range(-1, 2))
        self.assertIn(int(phase), range(4))


# ---------- ТК45: эндпоинт GET /jobs/{id}/chord_grid ----------

try:
    import test_pure as _tp
    import librosa  # noqa: F401
    import soundfile  # noqa: F401
    _API_OK = _tp._HAS_WORKER_DEPS
    _ApiBase = _tp._WorkerApiCase
except ImportError:
    _API_OK = False
    _ApiBase = unittest.TestCase

_SKIP_API = "нужны fastapi/httpx/numpy/librosa/soundfile (окружение воркера)"
_SR = 44100


def _beats_audio(dur=20.0, bpm=120, sr=_SR, silent=False):
    """Синтетика: удары (затухающий 60 Гц + щелчок шума) каждые 60/bpm с с 0,25 с,
    поверх — трезвучия плана по тактам."""
    n = int(dur * sr)
    x = np.zeros(n)
    if silent:
        return np.stack([x, x], axis=1).astype(np.float32)
    rng = np.random.default_rng(0)
    k = np.arange(int(0.08 * sr))
    hit = (np.exp(-k / (0.015 * sr)) * np.sin(2 * np.pi * 60 * k / sr)
           + 0.3 * np.exp(-k / (0.003 * sr)) * rng.standard_normal(len(k)))
    step = 60.0 / bpm
    i = 0
    while True:
        p = int((0.25 + i * step) * sr)
        if p + len(k) >= n:
            break
        x[p:p + len(k)] += 0.6 * hit
        i += 1
    # гармония по плану: с первого удара каждые 4 доли — трезвучие очередного аккорда
    # Dm C Bb A по кругу (частоты 3-й октавы), чтобы сдвиг плана находился однозначно
    triads = ([146.83, 174.61, 220.0], [130.81, 164.81, 196.0],
              [116.54, 146.83, 174.61], [110.0, 138.59, 164.81])
    t = np.arange(n) / sr
    bar = 4 * step
    for j in range(int(dur / bar) + 1):
        a, b = int((0.25 + j * bar) * sr), int((0.25 + (j + 1) * bar) * sr)
        a, b = min(a, n), min(b, n)
        for hz in triads[j % 4]:
            x[a:b] += 0.08 * np.sin(2 * np.pi * hz * t[a:b])
    return np.stack([x, x], axis=1).astype(np.float32)


@unittest.skipUnless(_API_OK, _SKIP_API)
class TestChordGridEndpoint(_ApiBase):
    """ТК45 и условие 21: 404 без плана, такты по долям звука, 422 без долей."""

    def _job_with(self, plan=_PLAN8, silent=False):
        import soundfile as sf
        jid = self._job(duration=20.0, semantic=False)
        d = self.jobs_dir / str(jid)
        d.mkdir(parents=True, exist_ok=True)
        sf.write(str(d / "audio.flac"), _beats_audio(silent=silent), _SR)
        if plan is not None:
            (d / "score.abc").write_text(plan)
        with self._conn() as c:
            c.execute("UPDATE jobs SET audio_file='audio.flac' WHERE id=?", (jid,))
        return jid

    def test_tk45_no_plan_404(self):
        jid = self._job_with(plan=None)
        r = self.client.get(f"/jobs/{jid}/chord_grid")
        self.assertEqual(r.status_code, 404, r.text)

    def test_missing_job_404(self):
        r = self.client.get("/jobs/9999/chord_grid")
        self.assertEqual(r.status_code, 404, r.text)

    def test_tk45_bars_by_beats_120_bpm(self):
        jid = self._job_with()
        r = self.client.get(f"/jobs/{jid}/chord_grid")
        self.assertEqual(r.status_code, 200, r.text)
        body = r.json()
        self.assertAlmostEqual(float(body["bpm"]), 120, delta=2)
        bars = body["bars"]
        self.assertGreater(len(bars), 0)
        starts = [b["start"] for b in bars]
        ends = [b["end"] for b in bars]
        self.assertEqual(starts, sorted(starts), "start такта не возрастает")
        self.assertEqual(len(set(starts)), len(starts), "start такта повторяется")
        self.assertEqual(ends, sorted(ends), "end такта не возрастает")
        for b in bars:
            self.assertLess(b["start"], b["end"])
            # такт 4/4 при 120 BPM — 2 с, и по долям звука, и в достроенной сетке
            self.assertAlmostEqual(b["end"] - b["start"], 2.0, delta=0.05, msg=b)
        for a, b in zip(bars, bars[1:], strict=False):
            self.assertAlmostEqual(a["end"], b["start"], delta=0.05)

    def test_bars_carry_plan_chords_and_sections(self):
        jid = self._job_with()
        bars = self.client.get(f"/jobs/{jid}/chord_grid").json()["bars"]
        for b in bars:
            for k in ("start", "end", "chord", "section"):
                self.assertIn(k, b)
            self.assertIn(b["chord"], set(PLAN8_CHORDS) | {None})
        self.assertTrue({"Dm", "C", "Bb", "A"} <= {b["chord"] for b in bars},
                        "аккорды плана не попали в сетку")
        self.assertTrue({"verse", "chorus"} <= {b["section"] for b in bars},
                        "секции плана не попали в сетку")

    def test_no_beats_422(self):
        jid = self._job_with(silent=True)
        r = self.client.get(f"/jobs/{jid}/chord_grid")
        self.assertEqual(r.status_code, 422, r.text)


    def test_short_track_few_beats_ok(self):
        # кросс-ревью s4: короткий трек (3,5 с, ~6 долей) — сетка строится, не 422
        import soundfile as sf
        jid = self._job_with()
        d = self.jobs_dir / str(jid)
        y = _beats_audio()
        sf.write(str(d / "audio.flac"), y[:int(3.5 * _SR)], _SR)
        r = self.client.get(f"/jobs/{jid}/chord_grid")
        self.assertEqual(r.status_code, 200, r.text)
        self.assertGreater(len(r.json()["bars"]), 0)

if __name__ == "__main__":
    unittest.main()
