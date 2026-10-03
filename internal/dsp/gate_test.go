package dsp

import (
	"math"
	"testing"
)

// Тесты цепочки «Ритм-гейт» (gate) по спецификации: громкость открывается и
// закрывается по сетке ударов период 60/(bpm·div) с, начало ударов offset + k·период
// (сетка тянется и назад, до offset). Внутри удара первые duty·период — открыто
// (усиление 1), остаток — уровень 1−depth, края — линейные рампы smooth мс.
// Проверяется звук на синтетике через Run; уровни сравниваются с входом в том же окне,
// чтобы неполные периоды синуса не давали погрешности. В пределах ±(smooth+2 мс) от
// краёв не меряем.

const gateMargin = 0.002 // запас от края рампы, с

// gateWin — окна «середина открытой части» и «середина закрытой части» удара k.
func gateWin(offset, period, duty, smoothMs float64, k int) (open, closed [2]float64) {
	s := smoothMs/1000 + gateMargin
	st := offset + float64(k)*period
	open = [2]float64{st + s, st + duty*period - s}
	closed = [2]float64{st + duty*period + s, st + period - s}
	return
}

// relDB — уровень выхода относительно входа в окне w, дБ.
func relDB(out, src []float32, w [2]float64) float64 {
	return dbfs(segRMS(out, w[0], w[1])) - dbfs(segRMS(src, w[0], w[1]))
}

// checkGateHits — для ударов k проверить: открытая середина ≈ вход (±1 дБ),
// закрытая середина — closedDB (±tol) или ниже maxClosed, если tol < 0.
func checkGateHits(t *testing.T, out, src []float32, offset, period, duty, smooth float64,
	ks []int, closedDB, tol float64) {
	t.Helper()
	for _, k := range ks {
		o, c := gateWin(offset, period, duty, smooth, k)
		if o[1]-o[0] < 0.004 || c[1]-c[0] < 0.004 {
			t.Fatalf("окна удара %d слишком узкие: %v %v — плохие параметры теста", k, o, c)
		}
		if d := relDB(out, src, o); math.Abs(d) > 1 {
			t.Errorf("удар %d: открыто %.3f–%.3f с, уровень %+.1f дБ к входу, want ≈ 0 (±1)",
				k, o[0], o[1], d)
		}
		d := relDB(out, src, c)
		if tol < 0 {
			if d > closedDB {
				t.Errorf("удар %d: закрыто %.3f–%.3f с, уровень %+.1f дБ к входу, want < %.0f",
					k, c[0], c[1], d, closedDB)
			}
		} else if math.Abs(d-closedDB) > tol {
			t.Errorf("удар %d: закрыто %.3f–%.3f с, уровень %+.1f дБ к входу, want %.1f (±%.0f)",
				k, c[0], c[1], d, closedDB, tol)
		}
	}
}

// Условие 1: bpm 120, div 4, duty 0.5, depth 1, offset 0.1 — открытые половины ≈ вход,
// закрытые глушатся (< −40 дБ); подряд несколько ударов.
func TestGateOpenClosedHalves(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.5*sin(2*PI*440*t)", 2)
	out := runChain(t, "gate", in, map[string]float64{
		"bpm": 120, "div": 4, "duty": 0.5, "depth": 1, "smooth": 5, "offset": 0.1, "from": 0,
	})
	checkGateHits(t, out, src, 0.1, 60.0/(120*4), 0.5, 5, []int{0, 1, 2, 3, 4, 5, 6, 7, 10, 13}, -40, -1)
}

// Условие 2: depth 0.5 — между ударами уровень 1−0.5 = −6 дБ (±1).
func TestGateDepthHalf(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.5*sin(2*PI*440*t)", 2)
	out := runChain(t, "gate", in, map[string]float64{
		"bpm": 120, "div": 4, "duty": 0.5, "depth": 0.5, "smooth": 5, "offset": 0.1, "from": 0,
	})
	checkGateHits(t, out, src, 0.1, 0.125, 0.5, 5, []int{1, 2, 3, 6, 9}, dbfs(0.5), 1)
}

// Условие 3: duty 0.25 — открыта первая четверть удара, остальное закрыто
// (включая вторую четверть, которая при duty 0.5 была бы открыта).
func TestGateDutyQuarter(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.5*sin(2*PI*1000*t)", 2)
	out := runChain(t, "gate", in, map[string]float64{
		"bpm": 120, "div": 4, "duty": 0.25, "depth": 1, "smooth": 2, "offset": 0.1, "from": 0,
	})
	const p = 0.125
	checkGateHits(t, out, src, 0.1, p, 0.25, 2, []int{1, 2, 3, 5, 8}, -40, -1)
	// вторая четверть удара (0.25p–0.5p, без краёв) — закрыта
	for _, k := range []int{1, 2, 3} {
		st := 0.1 + float64(k)*p
		w := [2]float64{st + 0.25*p + 0.004, st + 0.5*p - 0.002}
		if d := relDB(out, src, w); d > -40 {
			t.Errorf("удар %d: вторая четверть %.3f–%.3f с %+.1f дБ, want закрыто (< −40)", k, w[0], w[1], d)
		}
	}
}

// Условие 4: период следует bpm/div: bpm 138, div 2 → удары на offset + k·60/276.
// Сдвиг фазы от неверного периода накапливается, поэтому меряем и дальние удары.
func TestGatePeriodFollowsBpmDiv(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.5*sin(2*PI*440*t)", 3)
	out := runChain(t, "gate", in, map[string]float64{
		"bpm": 138, "div": 2, "duty": 0.5, "depth": 1, "smooth": 5, "offset": 0.1, "from": 0,
	})
	checkGateHits(t, out, src, 0.1, 60.0/276, 0.5, 5, []int{0, 1, 2, 5, 8, 11, 12}, -40, -1)
}

// Условие 5: сетка до offset — на [0, offset) гейт работает в той же фазе
// (удары offset − k·период).
func TestGateGridBeforeOffset(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.5*sin(2*PI*440*t)", 1.5)
	const off = 0.53 // не кратно периоду 0.125 — фаза не совпадает с нулём
	out := runChain(t, "gate", in, map[string]float64{
		"bpm": 120, "div": 4, "duty": 0.5, "depth": 1, "smooth": 5, "offset": off, "from": 0,
	})
	// удары с началом 0.03, 0.155, 0.28, 0.405 — все до offset
	checkGateHits(t, out, src, off, 0.125, 0.5, 5, []int{-4, -3, -2, -1}, -40, -1)
	// и после offset — та же сетка
	checkGateHits(t, out, src, off, 0.125, 0.5, 5, []int{0, 1, 3}, -40, -1)
}

// Условие 6: from=1.0 — до 0.9 с выход равен входу, после 1.1 с гейт работает.
func TestGateFromKeepsAudioBefore(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.5*sin(2*PI*440*t)", 2.5)
	out := runChain(t, "gate", in, map[string]float64{
		"bpm": 120, "div": 4, "duty": 0.5, "depth": 1, "smooth": 5, "offset": 0.1, "from": 1.0,
	})
	if r := diffSeg(out, src, 0, 0.9); r > 0.01 {
		t.Errorf("до from (0–0.9 с) выход отличается от входа: RMS разности %.4f", r)
	}
	// удары с началом 1.1, 1.225, … (offset 0.1 + k·0.125, k ≥ 8) — время абсолютное
	checkGateHits(t, out, src, 0.1, 0.125, 0.5, 5, []int{8, 9, 10, 12, 15}, -40, -1)
}

// Условие 7: длина выхода = длине входа (некруглая длительность, допуск 1 мс на кодек).
func TestGateKeepsLength(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.5*sin(2*PI*440*t)", 1.37)
	for _, p := range []map[string]float64{
		nil,
		{"bpm": 138, "div": 2, "duty": 0.25, "depth": 1, "offset": 0.3, "from": 0},
		{"from": 0.5, "smooth": 30, "div": 8},
	} {
		out := runChain(t, "gate", in, p)
		if d := len(out) - len(src); d < -16 || d > 16 {
			t.Errorf("%v: длина %d отсчётов, вход %d (разница %d)", p, len(out), len(src), d)
		}
	}
}

// Условие 8: gate есть в All(), это не голосовая цепочка; крутилки и дефолты по спеке.
func TestGateRegistered(t *testing.T) {
	var g *Chain
	for _, c := range All() {
		if c.ID == "gate" {
			c := c
			g = &c
		}
	}
	if g == nil {
		t.Fatal("gate нет в All()")
	}
	if g.Voice {
		t.Error("gate помечена Voice=true, want false (не голосовая цепочка)")
	}
	want := map[string][3]float64{ // min, max, default
		"bpm": {40, 240, 120}, "div": {1, 8, 4}, "duty": {0.1, 0.9, 0.5},
		"depth": {0, 1, 0.9}, "smooth": {1, 30, 5}, "offset": {0, 600, 0},
	}
	for id, w := range want {
		found := false
		for _, p := range g.Params {
			if p.ID != id {
				continue
			}
			found = true
			if p.Min != w[0] || p.Max != w[1] || p.Default != w[2] {
				t.Errorf("gate.%s: min/max/default %v/%v/%v, want %v/%v/%v",
					id, p.Min, p.Max, p.Default, w[0], w[1], w[2])
			}
		}
		if !found {
			t.Errorf("у gate нет крутилки %s", id)
		}
	}
	if !hasParam(g, "from") {
		t.Error("у gate нет общего параметра from")
	}
}

// Условие 9: offset принимает до 600 с — сетка тянется назад от далёкого offset:
// offset 300.1 при периоде 0.125 (300 = 2400 периодов) даёт в первых 2 с ту же фазу,
// что offset 0.1 (открытые/закрытые окна в те же моменты).
func TestGateLargeOffsetSamePhase(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.5*sin(2*PI*440*t)", 2)
	p := map[string]float64{"bpm": 120, "div": 4, "duty": 0.5, "depth": 1, "smooth": 5, "from": 0}
	p["offset"] = 300.1
	far := runChain(t, "gate", in, p)
	// окна ударов offset 0.1 + k·0.125 — по ним меряем выход с offset 300.1
	checkGateHits(t, far, src, 0.1, 0.125, 0.5, 5, []int{0, 1, 2, 3, 5, 8, 11, 14}, -40, -1)
	p["offset"] = 0.1
	near := runChain(t, "gate", in, p)
	if r := diffSeg(far, near, 0, 2); r > 0.01 {
		t.Errorf("выход с offset 300.1 отличается от offset 0.1: RMS разности %.4f", r)
	}
}
