package dsp

import (
	"math"
	"math/rand"
	"testing"
)

// Тесты выравнивания вклейки по оригиналу: сигналы синтетические, частота 16 кГц.

const testRate = 16000

// event — атака в «мастер-времени» (время оригинала) и её громкость.
type event struct {
	t   float64
	amp float64
}

// irregularPattern — нерегулярный рисунок на сетке 1/16 при 120 BPM (шаг 0.125 с)
// в диапазоне [from, to): случайные пропуски и акценты убирают периодическую
// неоднозначность — совпасть рисунок может только при истинном сдвиге.
func irregularPattern(seed int64, from, to float64) []event {
	r := rand.New(rand.NewSource(seed))
	var ev []event
	for t := from; t < to; t += 0.125 {
		if r.Float64() < 0.4 {
			ev = append(ev, event{t: t, amp: 0.3 + 0.7*r.Float64()})
		}
	}
	return ev
}

// addClick — короткий (8 мс) затухающий синус 1 кГц с началом в момент at.
func addClick(buf []float32, at, amp float64) {
	start := int(math.Round(at * testRate))
	n := int(0.008 * testRate)
	for i := 0; i < n; i++ {
		j := start + i
		if j < 0 || j >= len(buf) {
			continue
		}
		tt := float64(i) / testRate
		buf[j] += float32(amp * math.Exp(-tt/0.002) * math.Sin(2*math.Pi*1000*tt))
	}
}

// renderRef — оригинал длиной durSec: события мастер-времени как есть.
func renderRef(ev []event, durSec float64) []float32 {
	buf := make([]float32, int(durSec*testRate))
	for _, e := range ev {
		addClick(buf, e.t, e.amp)
	}
	return buf
}

// renderCand — партия длиной durSec: событие оригинала в момент T звучит в партии
// в момент (T − offset) × ratio. Тогда по контракту партия, проигранная со
// скоростью ratio и задержанная на offset, совпадает с оригиналом.
func renderCand(ev []event, durSec, offset, ratio float64) []float32 {
	buf := make([]float32, int(durSec*testRate))
	for _, e := range ev {
		addClick(buf, (e.t-offset)*ratio, e.amp)
	}
	return buf
}

func checkAlign(t *testing.T, got Alignment, wantOffset, offTol, wantRatio, ratioTol float64) {
	t.Helper()
	if math.Abs(got.OffsetSec-wantOffset) > offTol {
		t.Errorf("OffsetSec = %.4f, want %.4f ± %.3f (%+v)", got.OffsetSec, wantOffset, offTol, got)
	}
	if math.Abs(got.Ratio-wantRatio) > ratioTol {
		t.Errorf("Ratio = %.4f, want %.4f ± %.4f (%+v)", got.Ratio, wantRatio, ratioTol, got)
	}
	if got.Score < AlignMinScore || got.Score > 1 {
		t.Errorf("Score = %.3f, want in [AlignMinScore=%.2f, 1] (%+v)", got.Score, AlignMinScore, got)
	}
}

func TestAlignAudioPositiveOffset(t *testing.T) {
	// партия вступает на 0.25 с позже: событие оригинала в 1.0 — в партии на 0.75
	ev := irregularPattern(1, -3, 15)
	ref := renderRef(ev, 12)
	cand := renderCand(ev, 10, 0.25, 1)
	got := AlignAudio(ref, cand, testRate, AlignOpts{})
	checkAlign(t, got, 0.25, 0.02, 1, 0.005)
}

func TestAlignAudioRegularAccentedClickTrack(t *testing.T) {
	// клик-трек 120 BPM (каждые 0.5 с), акцент на каждый 4-й клик; сдвиг 0.25 с
	// меньше полупериода акцента (1 с), так что ответ однозначен
	var ev []event
	for i := -8; i < 32; i++ {
		amp := 0.3
		if i%4 == 0 {
			amp = 1
		}
		ev = append(ev, event{t: float64(i) * 0.5, amp: amp})
	}
	ref := renderRef(ev, 12)
	cand := renderCand(ev, 10, 0.25, 1)
	got := AlignAudio(ref, cand, testRate, AlignOpts{})
	checkAlign(t, got, 0.25, 0.02, 1, 0.005)
}

func TestAlignAudioSlowerCandidate(t *testing.T) {
	// партия сыграна на 3% медленнее (≈116.5 BPM против 120): ускорить в 1.03 раза
	ev := irregularPattern(2, -3, 15)
	ref := renderRef(ev, 12)
	cand := renderCand(ev, 11, 0.4, 1.03)
	got := AlignAudio(ref, cand, testRate, AlignOpts{})
	checkAlign(t, got, 0.4, 0.03, 1.03, 0.006)
}

func TestAlignAudioNegativeOffset(t *testing.T) {
	// партия начинается раньше оригинала: событие партии в 0.5 — начало оригинала
	ev := irregularPattern(3, -3, 15)
	ref := renderRef(ev, 12)
	cand := renderCand(ev, 12, -0.5, 1)
	got := AlignAudio(ref, cand, testRate, AlignOpts{})
	checkAlign(t, got, -0.5, 0.02, 1, 0.005)
}

func TestAlignAudioExpectedOffsetWindow(t *testing.T) {
	// поиск вокруг ожидаемого сдвига в узком радиусе находит истинный сдвиг 1.0
	ev := irregularPattern(4, -3, 15)
	ref := renderRef(ev, 12)
	cand := renderCand(ev, 10, 1.0, 1)
	got := AlignAudio(ref, cand, testRate, AlignOpts{ExpectOffsetSec: 1.0, MaxShiftSec: 0.3})
	checkAlign(t, got, 1.0, 0.02, 1, 0.005)
}

func TestAlignAudioNoiseIsUnreliable(t *testing.T) {
	// белый шум без атак — совпадения нет, выравнивание ненадёжно
	ev := irregularPattern(5, -3, 15)
	ref := renderRef(ev, 12)
	r := rand.New(rand.NewSource(42))
	cand := make([]float32, 10*testRate)
	for i := range cand {
		cand[i] = float32(0.3 * (2*r.Float64() - 1))
	}
	got := AlignAudio(ref, cand, testRate, AlignOpts{})
	if got.Score >= AlignMinScore {
		t.Errorf("noise: Score = %.3f, want < %.2f (%+v)", got.Score, AlignMinScore, got)
	}
	if got.Score < -1 || got.Score > 1 || math.IsNaN(got.Score) {
		t.Errorf("noise: Score = %v out of [-1, 1]", got.Score)
	}
}

func TestAlignAudioSilence(t *testing.T) {
	// тишина: без паники, без NaN, темп не трогаем, выравнивание ненадёжно
	ev := irregularPattern(6, -3, 15)
	ref := renderRef(ev, 12)
	cand := make([]float32, 10*testRate)
	got := AlignAudio(ref, cand, testRate, AlignOpts{})
	if got.Score >= AlignMinScore || math.IsNaN(got.Score) {
		t.Errorf("silence: Score = %v, want < %.2f", got.Score, AlignMinScore)
	}
	if got.Ratio != 1 {
		t.Errorf("silence: Ratio = %v, want 1", got.Ratio)
	}
	if math.IsNaN(got.OffsetSec) || math.IsInf(got.OffsetSec, 0) {
		t.Errorf("silence: OffsetSec = %v", got.OffsetSec)
	}
}

func TestAlignAudioEmptyInputs(t *testing.T) {
	// пустые входы — краевой случай: не паниковать и не выдавать надёжный результат
	ref := renderRef(irregularPattern(7, 0, 12), 12)
	for name, pair := range map[string][2][]float32{
		"empty cand": {ref, nil},
		"empty ref":  {nil, ref},
		"both empty": {nil, nil},
	} {
		got := AlignAudio(pair[0], pair[1], testRate, AlignOpts{})
		if got.Score >= AlignMinScore || math.IsNaN(got.Score) {
			t.Errorf("%s: Score = %v, want < %.2f", name, got.Score, AlignMinScore)
		}
	}
}

func TestInsertGain(t *testing.T) {
	cases := []struct {
		name                string
		ref, cand, db, want float64
	}{
		{"выравнивание по RMS", 0.2, 0.1, 0, 2},
		{"-6 дБ относительно оригинала", 0.1, 0.1, -6, 0.501187},
		{"потолок", 1, 0.01, 0, MaxInsertGain},
		{"тишину не раздуваем", 0.1, 0, 0, 0},
		{"почти тишина тоже", 0.1, 1e-9, 0, 0},
	}
	for _, c := range cases {
		got := InsertGain(c.ref, c.cand, c.db)
		if math.Abs(got-c.want) > 1e-3 {
			t.Errorf("%s: InsertGain(%v, %v, %v) = %v, want %v", c.name, c.ref, c.cand, c.db, got, c.want)
		}
	}
}

func TestRMS(t *testing.T) {
	if got := RMS(nil); got != 0 {
		t.Errorf("RMS(nil) = %v, want 0", got)
	}
	if got := RMS([]float32{}); got != 0 {
		t.Errorf("RMS(empty) = %v, want 0", got)
	}
	c := make([]float32, 1000)
	for i := range c {
		c[i] = 0.5
	}
	if got := RMS(c); math.Abs(got-0.5) > 1e-6 {
		t.Errorf("RMS(const 0.5) = %v, want 0.5", got)
	}
	// знак не важен: ±0.5 даёт тот же уровень
	for i := range c {
		if i%2 == 1 {
			c[i] = -0.5
		}
	}
	if got := RMS(c); math.Abs(got-0.5) > 1e-6 {
		t.Errorf("RMS(±0.5) = %v, want 0.5", got)
	}
}
