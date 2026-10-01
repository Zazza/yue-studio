package dsp

import (
	"fmt"
	"math"
	"testing"
)

// Тесты голосовых цепочек-примочек (мегафон/телефон/перегруз/слэпбэк) —
// «эффект на голос»: сами графы (полоса, повтор эха, dry/wet). Применение
// на дорожку голоса с выравниванием громкости — в internal/studio
// (voice_fx_test.go). Хелперы — из inserts_test.go / chains_test.go.

// voiceTone — выражение тона амплитуды 0.3 для genIn.
func voiceTone(hz float64) string { return fmt.Sprintf("0.3*sin(2*PI*%.0f*t)", hz) }

// Карточка: голосовые цепочки доступны по ID, помечены Voice, у каждой есть
// крутилка mix «сухой/обработанный» с дефолтом 1; старые цепочки — не голосовые.
func TestVoiceChainsMarked(t *testing.T) {
	for _, id := range []string{"megaphone", "phone", "voice-drive", "slapback"} {
		c := ByID(id)
		if c == nil {
			t.Errorf("ByID(%q) = nil, want голосовую цепочку", id)
			continue
		}
		if !c.Voice {
			t.Errorf("%s: Voice=false, want true", id)
		}
		mix := -1.0
		for _, p := range c.Params {
			if p.ID == mixParamID {
				mix = p.Default
			}
		}
		if mix != 1 {
			t.Errorf("%s: mix default=%v, want 1 (по умолчанию только эффект)", id, mix)
		}
	}
	for _, id := range []string{"wall", "grit", "dewhistle", "soften"} {
		if c := ByID(id); c != nil && c.Voice {
			t.Errorf("%s: Voice=true, want false", id)
		}
	}
}

// Карточка: мегафон — узкая полоса: тон ниже 300 Гц и выше 4 кГц гасится
// ≥ 12 дБ (каскады по два фильтра), тон в середине полосы остаётся.
func TestMegaphoneBand(t *testing.T) {
	needFFmpeg(t)
	for _, hz := range []float64{200, 6000} {
		in, src := genIn(t, voiceTone(hz), 3)
		out := runChain(t, "megaphone", in, nil)
		if d := dbfs(segRMS(out, 0.5, 2.5)) - dbfs(segRMS(src, 0.5, 2.5)); d > -12 {
			t.Errorf("мегафон: тон %.0f Гц изменился на %+.1f дБ, want ≤ −12 (вне полосы)", hz, d)
		}
	}
	in, src := genIn(t, voiceTone(1000), 3)
	out := runChain(t, "megaphone", in, nil)
	if d := dbfs(segRMS(out, 0.5, 2.5)) - dbfs(segRMS(src, 0.5, 2.5)); d < -12 {
		t.Errorf("мегафон: тон 1000 Гц (в полосе) %.1f дБ, want > −12", d)
	}
}

// Карточка: телефон — полоса ещё уже и суше: 200 Гц и 6 кГц гасятся ≥ 12 дБ,
// тон в полосе остаётся.
func TestPhoneBand(t *testing.T) {
	needFFmpeg(t)
	for _, hz := range []float64{200, 6000} {
		in, src := genIn(t, voiceTone(hz), 3)
		out := runChain(t, "phone", in, nil)
		if d := dbfs(segRMS(out, 0.5, 2.5)) - dbfs(segRMS(src, 0.5, 2.5)); d > -12 {
			t.Errorf("телефон: тон %.0f Гц изменился на %+.1f дБ, want ≤ −12 (вне полосы)", hz, d)
		}
	}
	in, src := genIn(t, voiceTone(1000), 3)
	out := runChain(t, "phone", in, nil)
	if d := dbfs(segRMS(out, 0.5, 2.5)) - dbfs(segRMS(src, 0.5, 2.5)); d < -12 {
		t.Errorf("телефон: тон 1000 Гц (в полосе) %.1f дБ, want > −12", d)
	}
}

// Карточка: слэпбэк 100 мс — щелчок и одиночный повтор через 100 ± 5 мс
// громкостью echo; после повтора тишина (второго повтора нет).
func TestSlapbackEchoTiming(t *testing.T) {
	needFFmpeg(t)
	in, _ := genIn(t, "if(gte(t,1)*lt(t,1.01),1,0)", 2.5)
	out := runChain(t, "slapback", in, map[string]float64{"delay": 100, "echo": 0.45})
	peakAt := func(from, to float64) (float64, float64) {
		bi, bv := -1, -1.0
		for i := int(from * chSR); i < min(int(to*chSR), len(out)); i++ {
			if a := math.Abs(float64(out[i])); a > bv {
				bv, bi = a, i
			}
		}
		if bi < 0 {
			return 0, 0
		}
		return float64(bi) / chSR, bv
	}
	if at, v := peakAt(0.95, 1.05); math.Abs(at-1) > 0.005 || v < 0.5 {
		t.Errorf("основной щелчок: %.3f с пик %.2f, want 1.000 ± 0.005 и ≥ 0.5", at, v)
	}
	if at, v := peakAt(1.05, 1.3); math.Abs(at-1.1) > 0.005 || v < 0.2 || v > 0.7 {
		t.Errorf("повтор: %.3f с пик %.2f, want 1.100 ± 0.005 и 0.2..0.7 (delay 100 мс, echo 0.45)", at, v)
	}
	if r := dbfs(segRMS(out, 1.2, 2.4)); r > -50 {
		t.Errorf("после повтора RMS %.1f дБ, want тишина (< −50, эхо одиночное)", r)
	}
}

// Карточка: mix=0 — сухой сигнал: эффект (включая шум и эхо внутри графа)
// выключен, выход совпадает со входом.
func TestVoiceMixZeroIsDry(t *testing.T) {
	needFFmpeg(t)
	for _, id := range []string{"megaphone", "phone", "voice-drive", "slapback"} {
		in, src := genIn(t, voiceTone(1000), 3)
		out := runChain(t, id, in, map[string]float64{mixParamID: 0})
		if r := diffSeg(out, src, 0.3, 2.7); r > 0.01 {
			t.Errorf("%s mix=0: RMS разности с входом %.4f, want ≤ 0.01", id, r)
		}
	}
}

// Перегруз голоса — по эталону (стем голоса «Гражданской обороны»): плотная
// середина, срезанный низ, верх НЕ раздут (прежняя цепочка поднимала >4 кГц
// на +15 дБ и звучала шипящим песком), голос уплотнён (пики над средним ниже).
func TestVoiceDriveLikeReference(t *testing.T) {
	needFFmpeg(t)
	// «голос»: гармоники 150 Гц до 3 кГц со спадом, амплитуда как у живого стема
	expr := "0"
	for k := 1; k <= 20; k++ {
		expr += fmt.Sprintf("+%.4f*sin(2*PI*%d*t)", 0.25/float64(k), 150*k)
	}
	in, src := genIn(t, expr, 3)
	out := runChain(t, "voice-drive", in, nil)
	bandIn := func(lo, hi float64) float64 { return bandDb(src, lo, hi) }
	bandOut := func(lo, hi float64) float64 { return bandDb(out, lo, hi) }
	// верх ниже середины, как у эталона (там −15 дБ): грязь — середина, не песок;
	// прежняя цепочка давала верх ВЫШЕ середины
	if d := bandOut(4000, 8000) - bandOut(300, 1000); d > -10 {
		t.Errorf("верх относительно середины %+.1f дБ, want ≤ −10", d)
	}
	// низ ниже середины сильнее, чем на входе (срез низа)
	if d := (bandOut(0, 250) - bandOut(300, 1000)) - (bandIn(0, 250) - bandIn(300, 1000)); d > -4 {
		t.Errorf("низ относительно середины %+.1f дБ, want ≤ −4 (срез низа)", d)
	}
	// перегруз, а не эквалайзер: чистый тон 300 Гц даёт 3-ю гармонику (900 Гц)
	// не тише −30 дБ к основному тону (≈3% — слышимый перегруз; прежняя цепочка
	// давала −44 дБ, т.е. почти чистый звук — «перегруз не работает»)
	tin, _ := genIn(t, "0.3*sin(2*PI*300*t)", 3)
	tout := runChain(t, "voice-drive", tin, nil)
	if h := dbfs(toneAmpAt(tout, testSR, 900, 0.5, 2.5)) - dbfs(toneAmpAt(tout, testSR, 300, 0.5, 2.5)); h < -30 {
		t.Errorf("3-я гармоника %.1f дБ к основному тону, want ≥ −30 (перегруз)", h)
	}
}

// testSR — частота decode/genIn в тестах dsp
const testSR = 16000.0

// biquad — фильтр RBJ (lowpass/highpass, Q=0.707) для замера полос в тестах.
func biquad(s []float32, sr, f0 float64, high bool) []float32 {
	w := 2 * math.Pi * f0 / sr
	cw, sw := math.Cos(w), math.Sin(w)
	alpha := sw / (2 * 0.7071)
	var b0, b1, b2 float64
	if high {
		b0, b1, b2 = (1+cw)/2, -(1 + cw), (1+cw)/2
	} else {
		b0, b1, b2 = (1-cw)/2, 1-cw, (1-cw)/2
	}
	a0, a1, a2 := 1+alpha, -2*cw, 1-alpha
	out := make([]float32, len(s))
	var x1, x2, y1, y2 float64
	for i, v := range s {
		x := float64(v)
		y := (b0*x + b1*x1 + b2*x2 - a1*y1 - a2*y2) / a0
		x2, x1, y2, y1 = x1, x, y1, y
		out[i] = float32(y)
	}
	return out
}

// bandDb — уровень полосы [lo, hi) дБ (каскад по два фильтра с каждой стороны),
// по середине сигнала (0.5–2.5 с), частота дискретизации — как у decode (testSR).
func bandDb(s []float32, lo, hi float64) float64 {
	x := s
	if lo > 0 {
		x = biquad(biquad(x, testSR, lo, true), testSR, lo, true)
	}
	if hi < testSR/2 {
		x = biquad(biquad(x, testSR, hi, false), testSR, hi, false)
	}
	return dbfs(segRMS(x, 0.5, 2.5))
}
