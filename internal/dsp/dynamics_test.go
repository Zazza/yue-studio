package dsp

import (
	"math"
	"path/filepath"
	"testing"
)

// Тесты карточки internal-dsp-space, раздел 6: fade, multiband, ducking
// (transient убрана из задачи, карточка 6.2).
// Хелперы nc* — из newchains_test.go.

// --- fade ---

// Карточка 6.1: in N — громкость в 0…N растёт монотонно от тишины (< −40 дБ на
// первых 50 мс) до полной. N = 2 с — выбор теста (карточка задаёт порог для любого N).
func TestFadeIn(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, "0.5*sin(2*PI*1000*t)", 4, chSR)
	src := decode(t, in)
	out := decode(t, ncRun(t, "fade", in, map[string]float64{"in": 2, "start": 0}))
	full := segRMS(src, 0, 4)
	if l := dbfs(segRMS(out, 0, 0.05) / full); l >= -40 {
		t.Errorf("первые 50 мс: %.1f дБ к полной громкости, want < −40", l)
	}
	prev := 0.0
	for w := 0.0; w+0.05 <= 2; w += 0.05 {
		cur := segRMS(out, w, w+0.05)
		if cur < prev*0.99 {
			t.Errorf("нарастание не монотонно: %.2f–%.2f с %.4f < предыдущего %.4f", w, w+0.05, cur, prev)
			break
		}
		prev = cur
	}
	if d := dbfs(segRMS(out, 2.1, 3.9)) - dbfs(segRMS(src, 2.1, 3.9)); math.Abs(d) > 0.5 {
		t.Errorf("после нарастания уровень %+.2f дБ к входу, want полная громкость (±0.5)", d)
	}
}

// Карточка 6.1: затухание — до start вход без изменений, в середине затухания тише
// входа ≥ 3 дБ, после start+out — тишина (< −60 дБ).
func TestFadeOut(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, "0.5*sin(2*PI*1000*t)", 4.5, chSR)
	src := decode(t, in)
	out := decode(t, ncRun(t, "fade", in, map[string]float64{"in": 0, "start": 1, "out": 2}))
	if d := ncDiffDb(out, src, chSR, 0, 0.95); d > -50 {
		t.Errorf("до start выход отличается от входа на %.1f дБ, want без изменений (≤ −50)", d)
	}
	if d := dbfs(segRMS(out, 1.9, 2.1)) - dbfs(segRMS(src, 1.9, 2.1)); d > -3 {
		t.Errorf("середина затухания %+.1f дБ к входу, want ≤ −3", d)
	}
	if l := dbfs(segRMS(out, 3.05, 4.5) / segRMS(src, 3.05, 4.5)); l >= -60 {
		t.Errorf("после start+out %.1f дБ к входу, want тишина (< −60)", l)
	}
	if d := secs(out); math.Abs(d-4.5) > 0.05 {
		t.Errorf("длина %.3f с, want 4.5 ± 0.05", d)
	}
}

// Карточка 6.1: все 0 → выход = вход.
func TestFadeAllZeroIsDry(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, "0.5*sin(2*PI*1000*t)", 3, chSR)
	src := decode(t, in)
	out := decode(t, ncRun(t, "fade", in, map[string]float64{"in": 0, "start": 0}))
	if d := ncDiffDb(out, src, chSR, 0, 2.95); d > -50 {
		t.Errorf("все 0: выход отличается от входа на %.1f дБ, want ≤ −50", d)
	}
}

// --- multiband ---

// ncStepLevels — тон 1000 Гц: тихие (−30 дБ) и громкие (−6 дБ) секунды по очереди.
const ncStepLevels = "sin(2*PI*1000*t)*if(lt(mod(t\\,2)\\,1)\\,0.0316\\,0.5)"

func ncStepDiff(s []float32) float64 {
	return dbfs(segRMS(s, 3.3, 3.9)) - dbfs(segRMS(s, 2.3, 2.9))
}

// Карточка 6.3: разница уровней тихого и громкого сегментов при amount 1 меньше
// входной ≥ 6 дБ; amount 0 → выход = вход (±0.5 дБ).
func TestMultibandCompresses(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncStepLevels, 4, chSR)
	src := decode(t, in)
	out := decode(t, ncRun(t, "multiband", in, map[string]float64{"amount": 1}))
	if d := ncStepDiff(src) - ncStepDiff(out); d < 6 {
		t.Errorf("amount 1: перепад %.1f дБ (вход %.1f), want меньше входного ≥ 6 дБ", ncStepDiff(out), ncStepDiff(src))
	}
	out = decode(t, ncRun(t, "multiband", in, map[string]float64{"amount": 0}))
	for _, w := range [][2]float64{{2.3, 2.9}, {3.3, 3.9}} {
		if d := dbfs(segRMS(out, w[0], w[1])) - dbfs(segRMS(src, w[0], w[1])); math.Abs(d) > 0.5 {
			t.Errorf("amount 0: сегмент %.1f–%.1f с %+.2f дБ, want ±0.5", w[0], w[1], d)
		}
	}
}

// --- ducking ---

// Карточка 6.4: тон (вход 0) + ключ-щелчки каждые 0.5 с (вход 1): уровень тона в
// 20–60 мс после щелчка ≥ 6 дБ ниже, чем перед следующим щелчком; ключ в выходе
// не слышен; длина = длине входа 0 (ключ длиннее).
func TestDuckingFromKey(t *testing.T) {
	needFFmpeg(t)
	c := ncChain(t, "ducking")
	if c.Key != "drums" {
		t.Fatalf("ducking.Key = %q, want drums", c.Key)
	}
	in := ncGen(t, "0.3*sin(2*PI*1000*t)", 3, chSR)
	key := ncGen(t, "0.8*lt(mod(t\\,0.5)\\,0.02)", 4, chSR)
	out := filepath.Join(t.TempDir(), "out.flac")
	if err := RunInputs([]string{in, key}, out, c.FilterGraph(nil)); err != nil {
		t.Fatalf("RunInputs: %v", err)
	}
	s := decode(t, out)
	if d := secs(s); math.Abs(d-3) > 0.05 {
		t.Errorf("длина %.3f с, want 3 ± 0.05 (длина входа 0)", d)
	}
	for _, cl := range []float64{1.0, 1.5, 2.0} {
		after := toneAmpAt(s, chSR, 1000, cl+0.02, cl+0.06)
		before := toneAmpAt(s, chSR, 1000, cl+0.40, cl+0.48)
		if d := dbfs(before) - dbfs(after); d < 6 {
			t.Errorf("щелчок %.1f с: тон через 20–60 мс тише, чем перед следующим, на %.1f дБ, want ≥ 6", cl, d)
		}
		if p := ncPeak(s, chSR, cl, cl+0.02); p > 0.35 {
			t.Errorf("щелчок %.1f с: пик выхода %.3f — ключ слышен (тон ≤ 0.3)", cl, p)
		}
	}
}
