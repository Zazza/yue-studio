package dsp

import (
	"math"
	"sort"
	"testing"
)

// Тесты карточки internal-dsp-space, разделы 4–5: модуляция (chorus, flanger,
// phaser, tremolo) и фильтры (eq, sweep, autowah). Хелперы nc* — из newchains_test.go.

// --- 4. Модуляция ---

// ncGainProfile — АЧХ выхода к входу на [from,to] в полосах по 20 Гц до 7 кГц, дБ.
// Шум входа сокращается: остаётся передаточная функция эффекта в этом окне.
func ncGainProfile(out, in []float32, from, to float64) []float64 {
	po, bin := ncSpectrum(out, chSR, from, to)
	pi, _ := ncSpectrum(in, chSR, from, to)
	var g []float64
	for lo := 100.0; lo+20 <= 7000; lo += 20 {
		eo, ei := ncBand(po, bin, lo, lo+20), ncBand(pi, bin, lo, lo+20)
		g = append(g, 10*math.Log10((eo+1e-20)/(ei+1e-20)))
	}
	return g
}

func ncMeanAbsDiff(a, b []float64) float64 {
	var s float64
	for i := range a {
		s += math.Abs(a[i] - b[i])
	}
	return s / float64(len(a))
}

// Карточка 4.1: chorus/flanger/phaser на белом шуме — спектр меняется во времени:
// АЧХ в окне 0–0.5 с отличается от окна, сдвинутого на полпериода LFO; mix 0 → сухой.
func TestModulationSpectrumMoves(t *testing.T) {
	needFFmpeg(t)
	in := ncNoise(t, 0.3, 3, 3, chSR)
	src := decode(t, in)
	for _, id := range []string{"chorus", "flanger", "phaser"} {
		t.Run(id, func(t *testing.T) {
			const rate = 0.5 // период LFO 2 с, полпериода — 1 с
			p := map[string]float64{"rate": rate, "mix": 1}
			out := decode(t, ncRun(t, id, in, p))
			a, b := ncGainProfile(out, src, 0, 0.5), ncGainProfile(out, src, 1, 1.5)
			if d := ncMeanAbsDiff(a, b); d < 1 {
				t.Errorf("АЧХ в окнах 0–0.5 и 1–1.5 с отличается в среднем на %.2f дБ, want ≥ 1 (спектр движется)", d)
			}
			p["mix"] = 0
			dry := decode(t, ncRun(t, id, in, p))
			if d := ncDiffDb(dry, src, chSR, 0.05, 2.95); d > -50 {
				t.Errorf("mix 0: отличие от сухого %.1f дБ, want ≤ −50", d)
			}
		})
	}
}

// ncEnvelope5ms — RMS по окнам 5 мс на [from,to] (тон 1000 Гц — ровно 5 периодов в окне).
func ncEnvelope5ms(s []float32, from, to float64) []float64 {
	var e []float64
	for w := from; w+0.005 <= to; w += 0.005 {
		e = append(e, segRMS(s, w, w+0.005))
	}
	return e
}

// Карточка 4.2: tremolo на тоне — огибающая с периодом 1/rate (±10 %), минимум
// ≈ 1−depth (±0.1), без ступенек (соседние окна 5 мс отличаются < 1.5 дБ при rate 5).
func TestTremoloEnvelope(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, "0.5*sin(2*PI*1000*t)", 3, chSR)
	src := decode(t, in)
	ref := segRMS(src, 0.5, 2.5)
	for _, tc := range []struct{ rate, depth float64 }{{5, 0.5}, {3, 0.8}, {8, 0.3}} {
		out := decode(t, ncRun(t, "tremolo", in, map[string]float64{"rate": tc.rate, "depth": tc.depth}))
		env := ncEnvelope5ms(out, 0.5, 2.5)
		g := make([]float32, len(env))
		minG := math.Inf(1)
		for i, v := range env {
			g[i] = float32(v / ref)
			minG = math.Min(minG, v/ref)
		}
		period := ncAutocorrPeak(g, 200, 0.5/tc.rate, 1.5/tc.rate)
		if want := 1 / tc.rate; math.Abs(period-want) > 0.1*want {
			t.Errorf("rate %v: период огибающей %.3f с, want %.3f ± 10%%", tc.rate, period, want)
		}
		if want := 1 - tc.depth; math.Abs(minG-want) > 0.1 {
			t.Errorf("rate %v depth %v: минимум огибающей %.2f, want %.2f ± 0.1", tc.rate, tc.depth, minG, want)
		}
		if tc.rate == 5 {
			for i := 1; i < len(env); i++ {
				if d := math.Abs(dbfs(env[i]) - dbfs(env[i-1])); d >= 1.5 {
					t.Errorf("rate 5: ступенька %.2f дБ между окнами 5 мс на %.3f с, want < 1.5", d, 0.5+float64(i)*0.005)
					break
				}
			}
		}
	}
}

// --- 5. Фильтры ---

// ncEqTones — тоны 50, 250, 1000, 4000, 12000 Гц по 0.1 (44.1 кГц).
const ncEqTones = "0.1*sin(2*PI*50*t)+0.1*sin(2*PI*250*t)+0.1*sin(2*PI*1000*t)+" +
	"0.1*sin(2*PI*4000*t)+0.1*sin(2*PI*12000*t)"

func ncEqDelta(out, src []float32, hz float64) float64 {
	return dbfs(toneAmpAt(out, ncSR, hz, 0.5, 1.5)) - dbfs(toneAmpAt(src, ncSR, hz, 0.5, 1.5))
}

// Карточка 5.1: все 0 → выход = вход (±0.1 дБ на каждом тоне).
func TestEQFlatIsTransparent(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncEqTones, 2, ncSR)
	src := decodeAt(t, in, ncSR)
	out := decodeAt(t, ncRun(t, "eq", in, map[string]float64{"low": 0, "mid": 0, "high": 0}), ncSR)
	for _, hz := range []float64{50, 250, 1000, 4000, 12000} {
		if d := ncEqDelta(out, src, hz); math.Abs(d) > 0.1 {
			t.Errorf("все 0: тон %v Гц изменился на %+.2f дБ, want ±0.1", hz, d)
		}
	}
}

// Карточка 5.1: mid +6 на midf → тон на midf +6±1 дБ; на midf·4 и midf/4 — < 1.5 дБ.
func TestEQMidBell(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncEqTones, 2, ncSR)
	src := decodeAt(t, in, ncSR)
	out := decodeAt(t, ncRun(t, "eq", in, map[string]float64{"mid": 6, "midf": 1000, "midq": 1}), ncSR)
	if d := ncEqDelta(out, src, 1000); math.Abs(d-6) > 1 {
		t.Errorf("mid +6: тон 1000 Гц %+.2f дБ, want +6 ± 1", d)
	}
	for _, hz := range []float64{250, 4000} {
		if d := ncEqDelta(out, src, hz); math.Abs(d) >= 1.5 {
			t.Errorf("mid +6 на 1000: тон %v Гц изменился на %+.2f дБ, want < 1.5", hz, d)
		}
	}
}

// Карточка 5.1: полки — тон 50 Гц при low +6 → +5…+7; тон 12 кГц при high −6 → −5…−7.
func TestEQShelves(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncEqTones, 2, ncSR)
	src := decodeAt(t, in, ncSR)
	out := decodeAt(t, ncRun(t, "eq", in, map[string]float64{"low": 6, "lowf": 120, "high": -6, "highf": 8000}), ncSR)
	if d := ncEqDelta(out, src, 50); d < 5 || d > 7 {
		t.Errorf("low +6: тон 50 Гц %+.2f дБ, want +5…+7", d)
	}
	if d := ncEqDelta(out, src, 12000); d > -5 || d < -7 {
		t.Errorf("high −6: тон 12 кГц %+.2f дБ, want −7…−5", d)
	}
}

// ncSweepParams — свип 1–4 с с параметрами поверх.
func ncSweepParams(over map[string]float64) map[string]float64 {
	p := map[string]float64{"start": 1, "dur": 3}
	for k, v := range over {
		p[k] = v
	}
	return p
}

// Карточка 5.2: до start звук не тронут; type 1 (ВЧ) 20→2000 на белом шуме — энергия
// < 200 Гц в последней секунде окна на ≥ 10 дБ ниже, чем в первой; после окна при
// hold 0 — снова сухой.
func TestSweepHighpassHold0(t *testing.T) {
	needFFmpeg(t)
	in := ncNoise(t, 0.3, 6, 6, ncSR)
	src := decode(t, in)
	out := decode(t, ncRun(t, "sweep", in, ncSweepParams(map[string]float64{"type": 1, "f0": 20, "f1": 2000, "hold": 0})))
	if d := ncDiffDb(out, src, chSR, 0, 0.9); d > -40 {
		t.Errorf("до start выход отличается от входа на %.1f дБ, want ≤ −40", d)
	}
	first, last := ncBandDb(out, chSR, 1, 2, 0, 200), ncBandDb(out, chSR, 3, 4, 0, 200)
	if first-last < 10 {
		t.Errorf("низ < 200 Гц: первая секунда окна %.1f дБ, последняя %.1f дБ, want ниже ≥ 10 дБ", first, last)
	}
	if d := ncDiffDb(out, src, chSR, 4.3, 5.9); d > -30 {
		t.Errorf("hold 0: после окна выход отличается от входа на %.1f дБ, want сухой (≤ −30)", d)
	}
}

// Карточка 5.2: hold 1 — после окна низ остаётся срезан.
func TestSweepHighpassHold1(t *testing.T) {
	needFFmpeg(t)
	in := ncNoise(t, 0.3, 6, 6, ncSR)
	src := decode(t, in)
	out := decode(t, ncRun(t, "sweep", in, ncSweepParams(map[string]float64{"type": 1, "f0": 20, "f1": 2000, "hold": 1})))
	if d := ncBandDb(src, chSR, 4.3, 5.9, 0, 200) - ncBandDb(out, chSR, 4.3, 5.9, 0, 200); d < 10 {
		t.Errorf("hold 1: низ < 200 Гц после окна ниже входа на %.1f дБ, want ≥ 10 (держим срез)", d)
	}
}

// Карточка 5.2: type 0 (НЧ) 20000→500 — верх > 4 кГц к концу окна ≥ 10 дБ ниже.
func TestSweepLowpass(t *testing.T) {
	needFFmpeg(t)
	in := ncNoise(t, 0.3, 6, 6, ncSR)
	out := decode(t, ncRun(t, "sweep", in, ncSweepParams(map[string]float64{"type": 0, "f0": 20000, "f1": 500, "hold": 0})))
	first, last := ncBandDb(out, chSR, 1, 2, 4000, 8000), ncBandDb(out, chSR, 3, 4, 4000, 8000)
	if first-last < 10 {
		t.Errorf("верх > 4 кГц: первая секунда окна %.1f дБ, последняя %.1f дБ, want ниже ≥ 10 дБ", first, last)
	}
}

// Карточка 5.3: autowah на белом шуме — спектральный центроид колеблется с периодом
// 1/rate (±15 %), размах ≥ (hi−lo)/3. Карточка не задаёт mix — берётся 1 (только
// эффект): сухой широкополосный шум при mix < 1 сам задаёт центроид.
func TestAutowahCentroidSwings(t *testing.T) {
	needFFmpeg(t)
	in := ncNoise(t, 0.3, 5, 5, chSR)
	const rate, lo, hi = 2.0, 400.0, 2200.0
	out := decode(t, ncRun(t, "autowah", in, map[string]float64{"rate": rate, "lo": lo, "hi": hi, "mix": 1}))
	const hop = 0.025
	var cen []float32
	for w := 0.5; w+0.05 <= 4.5; w += hop {
		pw, bin := ncSpectrum(out, chSR, w, w+0.05)
		var num, den float64
		for i, p := range pw {
			num += float64(i) * bin * p
			den += p
		}
		cen = append(cen, float32(num/(den+1e-30)))
	}
	period := ncAutocorrPeak(cen, int(1/hop), 0.5*1/rate, 1.5*1/rate)
	if want := 1 / rate; math.Abs(period-want) > 0.15*want {
		t.Errorf("период центроида %.3f с, want %.3f ± 15%%", period, want)
	}
	s := append([]float32(nil), cen...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	swing := float64(s[len(s)*95/100] - s[len(s)*5/100])
	if swing < (hi-lo)/3 {
		t.Errorf("размах центроида %.0f Гц (5–95 %%), want ≥ %.0f", swing, (hi-lo)/3)
	}
}
