package dsp

import (
	"math"
	"testing"
)

// Тесты цепочки «Громкость альбома» (level) по спецификации: выход = вход × 10^(gain/20),
// пики отсчётов не выше 10^(ceiling/20) (лимитер работает только у громких пиков, тихий
// материал не трогает), тембр не меняется, длина сохраняется. Звук — через Run на синтетике.

const levelQuiet = "0.1*sin(2*PI*440*t)"

// levelGainDB — изменение RMS выхода относительно входа на [from,to], дБ.
func levelGainDB(out, src []float32, from, to float64) float64 {
	return dbfs(segRMS(out, from, to)) - dbfs(segRMS(src, from, to))
}

// Случай 1: тихий синус, gain ±3 дБ → RMS меняется на ±3 дБ (±0.2), до потолка далеко.
func TestLevelGainShiftsRMS(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, levelQuiet, 3)
	for _, g := range []float64{-3, 3} {
		out := runChain(t, "level", in, map[string]float64{"gain": g})
		if d := levelGainDB(out, src, 0.2, 2.8); math.Abs(d-g) > 0.2 {
			t.Errorf("gain=%v: RMS изменился на %.2f дБ, want %v±0.2", g, d, g)
		}
	}
}

// Случай 2: gain 0 на тихом сигнале — выход совпадает со входом
// (разность ниже −50 дБ относительно входа, первые 50 мс не считаем).
func TestLevelZeroGainTransparent(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, levelQuiet, 3)
	out := runChain(t, "level", in, map[string]float64{"gain": 0})
	end := math.Min(secs(src), secs(out))
	rel := dbfs(diffSeg(out, src, 0.05, end)) - dbfs(segRMS(src, 0.05, end))
	if rel > -50 {
		t.Errorf("gain=0: разность выход−вход %.1f дБ относительно входа, want < −50", rel)
	}
}

// Случай 3: громкий синус 0.99, gain +3, ceiling −1.5 → после 100 мс (атака лимитера)
// ни один отсчёт не выше 10^(−1.5/20)+0.01.
func TestLevelCeilingLimitsPeaks(t *testing.T) {
	needFFmpeg(t)
	in, _ := genIn(t, "0.99*sin(2*PI*440*t)", 3)
	out := runChain(t, "level", in, map[string]float64{"gain": 3, "ceiling": -1.5})
	limit := math.Pow(10, -1.5/20) + 0.01
	var peak float64
	at := 0
	for i := int(0.1 * chSR); i < len(out); i++ {
		if a := math.Abs(float64(out[i])); a > peak {
			peak, at = a, i
		}
	}
	if peak > limit {
		t.Errorf("пик %.4f на %.3f с, want ≤ %.4f (ceiling −1.5 дБ)", peak, float64(at)/chSR, limit)
	}
	// Лимитер не должен «глушить» сигнал: громкий синус остаётся у потолка.
	if peak < math.Pow(10, -1.5/20)-0.1 {
		t.Errorf("пик %.4f — сильно ниже потолка %.4f, сигнал задавлен", peak, limit-0.01)
	}
}

// Случай 4: тембр не меняется — соотношение амплитуд 440 Гц и 3000 Гц при gain −6
// то же, что на входе (±0.3 дБ).
func TestLevelKeepsTimbre(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.08*sin(2*PI*440*t)+0.04*sin(2*PI*3000*t)", 3)
	out := runChain(t, "level", in, map[string]float64{"gain": -6})
	ratio := func(s []float32) float64 {
		return dbfs(toneAmpAt(s, chSR, 440, 0.5, 2.5)) - dbfs(toneAmpAt(s, chSR, 3000, 0.5, 2.5))
	}
	if d := ratio(out) - ratio(src); math.Abs(d) > 0.3 {
		t.Errorf("соотношение 440/3000 Гц сдвинулось на %.2f дБ, want ±0.3", d)
	}
	if d := levelGainDB(out, src, 0.5, 2.5); math.Abs(d+6) > 0.2 {
		t.Errorf("gain=−6: RMS изменился на %.2f дБ, want −6±0.2", d)
	}
}

// Случай 5: длина выхода равна длине входа — при любых параметрах.
func TestLevelKeepsLength(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, levelQuiet, 3)
	for _, p := range []map[string]float64{
		nil,
		{"gain": 12, "ceiling": -3},
		{"gain": -12, "ceiling": -0.1},
	} {
		out := runChain(t, "level", in, p)
		if d := len(out) - len(src); d < -16 || d > 16 {
			t.Errorf("%v: длина %d отсчётов, вход %d (разница %d)", p, len(out), len(src), d)
		}
	}
}

// Случай 6: level есть в All(), не голосовая; крутилки gain/ceiling по спеке; есть from.
func TestLevelRegistered(t *testing.T) {
	var l *Chain
	for _, c := range All() {
		if c.ID == "level" {
			c := c
			l = &c
		}
	}
	if l == nil {
		t.Fatal("level нет в All()")
	}
	if l.Voice {
		t.Error("level помечена Voice=true, want false (не голосовая цепочка)")
	}
	want := map[string][3]float64{ // min, max, default
		"gain": {-12, 12, 0}, "ceiling": {-3, -0.1, -1.5},
	}
	for id, w := range want {
		found := false
		for _, p := range l.Params {
			if p.ID != id {
				continue
			}
			found = true
			if p.Min != w[0] || p.Max != w[1] || p.Default != w[2] {
				t.Errorf("level.%s: min/max/default %v/%v/%v, want %v/%v/%v",
					id, p.Min, p.Max, p.Default, w[0], w[1], w[2])
			}
		}
		if !found {
			t.Errorf("у level нет крутилки %s", id)
		}
	}
	if !hasParam(l, "from") {
		t.Error("у level нет общего параметра from")
	}
}
