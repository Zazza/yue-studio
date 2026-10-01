package dsp

import (
	"math"
	"testing"
)

// Тесты цепочки drop («Провал»): с отметки start кусок dur секунд тормозит (темп 1 → slow,
// высота та же) и затихает до нуля, затем вставленная тишина gap секунд, затем остаток
// трека в обычном темпе с нарастанием от floor дБ за rise секунд.
//
// Длина растяжки торможения S заранее не известна (dur ≤ S ≤ dur/slow), поэтому тесты
// выводят её из длины выхода по спецификации: len(out) = len(in) + (S − dur) + gap.

const dropExpr = "0.5*sin(2*PI*440*t)"

// dropStretch — растяжка торможения, выведенная из длин входа/выхода.
func dropStretch(src, out []float32, dur, gap float64) float64 {
	return secs(out) - secs(src) + dur - gap
}

// zcFreq — частота по пересечениям нуля на [from,to] с (16 кГц).
func zcFreq(s []float32, from, to float64) float64 {
	a, b := int(from*chSR), min(int(to*chSR), len(s))
	if b-a < 2 {
		return 0
	}
	n := 0
	for i := a + 1; i < b; i++ {
		if (s[i-1] < 0) != (s[i] < 0) {
			n++
		}
	}
	return float64(n) / 2 / (float64(b-a) / chSR)
}

// Карточка: drop доступна через ByID и в All(); крутилки start/dur/slow/gap/rise/floor;
// своя отметка start — общего from нет.
func TestDropChainParams(t *testing.T) {
	c := ByID("drop")
	if c == nil || c.ID != "drop" || c.Name == "" {
		t.Fatalf("ByID(drop) = %+v, want цепочку с ID drop и именем", c)
	}
	for _, id := range []string{"start", "dur", "slow", "gap", "rise", "floor"} {
		if !hasParam(c, id) {
			t.Errorf("drop: нет параметра %s", id)
		}
	}
	if hasParam(c, "from") {
		t.Errorf("drop: есть общий from, а у цепочки своя отметка start")
	}
	found := false
	for _, x := range All() {
		if x.ID == "drop" {
			found = true
		}
	}
	if !found {
		t.Errorf("drop нет в All()")
	}
}

// Карточка: полный сценарий — до start без изменений, торможение затихает, тишина gap,
// нарастание от floor до полной за rise, длина в границах растяжки.
func TestDropScenario(t *testing.T) {
	needFFmpeg(t)
	const start, dur, slow, gap, rise, floor = 3.0, 2.0, 0.5, 2.0, 2.0, -20.0
	in, src := genIn(t, dropExpr, 12)
	out := runChain(t, "drop", in, map[string]float64{
		"start": start, "dur": dur, "slow": slow, "gap": gap, "rise": rise, "floor": floor,
	})
	ref := dbfs(segRMS(src, 1, 2))

	// До start звук не меняется.
	if d := diffSeg(out, src, 0.1, start-0.1); d > 0.005 {
		t.Errorf("до start RMS разницы с входом %.4f, want ≈ 0 (< 0.005)", d)
	}

	// Длина: dur < S ≤ dur/slow (допуск 0.1 с).
	S := dropStretch(src, out, dur, gap)
	if S <= dur || S > dur/slow+0.1 {
		t.Fatalf("растяжка торможения %.2f с (выход %.2f с), want (%.1f; %.1f]", S, secs(out), dur, dur/slow)
	}

	// Торможение затихает к концу окна.
	head := dbfs(segRMS(out, start+0.05, start+0.35))
	tail := dbfs(segRMS(out, start+S-0.35, start+S-0.05))
	if head-ref < -3 {
		t.Errorf("начало торможения %.1f дБ, вход %.1f — want почти полная громкость", head, ref)
	}
	if head-tail < 10 {
		t.Errorf("торможение: начало %.1f дБ, конец %.1f дБ — want конец тише хотя бы на 10 дБ", head, tail)
	}

	// Высота тона в торможении та же (первая половина окна).
	if f := zcFreq(out, start+0.05, start+S/2); math.Abs(f-440) > 440*0.03 {
		t.Errorf("частота в торможении %.1f Гц, want 440 ±3%%", f)
	}

	// Тишина gap.
	mid := start + S + gap/2
	if r := dbfs(segRMS(out, mid-0.5, mid+0.5)); r > -60 {
		t.Errorf("середина паузы %.2f–%.2f с: %.1f дБ, want < −60", mid-0.5, mid+0.5, r)
	}

	// Нарастание: сразу после паузы ≈ floor, через rise — почти исходный уровень.
	t0 := start + S + gap
	if d := dbfs(segRMS(out, t0+0.02, t0+0.12)) - ref; math.Abs(d-floor) > 6 {
		t.Errorf("сразу после паузы уровень %+.1f дБ к входу, want ≈ floor %.0f (±6)", d, floor)
	}
	if d := dbfs(segRMS(out, t0+rise+0.05, t0+rise+0.8)) - ref; math.Abs(d) > 1.5 {
		t.Errorf("через rise после паузы уровень %+.1f дБ к входу, want ≈ 0 (±1.5)", d)
	}
}

// Карточка: rise=0 — после паузы сразу полная громкость.
func TestDropRiseZeroFullLevel(t *testing.T) {
	needFFmpeg(t)
	const start, dur, slow, gap = 3.0, 2.0, 0.6, 1.0
	in, src := genIn(t, dropExpr, 10)
	out := runChain(t, "drop", in, map[string]float64{
		"start": start, "dur": dur, "slow": slow, "gap": gap, "rise": 0, "floor": -30,
	})
	ref := dbfs(segRMS(src, 1, 2))
	t0 := start + dropStretch(src, out, dur, gap) + gap
	if d := dbfs(segRMS(out, t0+0.05, t0+0.3)) - ref; math.Abs(d) > 1.5 {
		t.Errorf("rise=0: сразу после паузы %+.1f дБ к входу, want ≈ 0 (±1.5)", d)
	}
}

// Карточка: slow=1 и gap=0 — торможения нет, тишины нет: длина ≈ входной.
func TestDropNoSlowNoGapKeepsLength(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, dropExpr, 10)
	out := runChain(t, "drop", in, map[string]float64{
		"start": 3, "dur": 2, "slow": 1, "gap": 0, "rise": 1, "floor": -20,
	})
	if d := secs(out) - secs(src); math.Abs(d) > 0.1 {
		t.Errorf("slow=1, gap=0: выход %.2f с, вход %.2f с, want равны (±0.1)", secs(out), secs(src))
	}
}

// Карточка: длина выхода = вход + (S − dur) + gap, dur < S ≤ dur/slow — на другом наборе.
func TestDropLengthBounds(t *testing.T) {
	needFFmpeg(t)
	const dur, slow, gap = 4.0, 0.7, 1.5
	in, src := genIn(t, dropExpr, 12)
	out := runChain(t, "drop", in, map[string]float64{
		"start": 2, "dur": dur, "slow": slow, "gap": gap, "rise": 1, "floor": -20,
	})
	lo, hi := secs(src)+gap, secs(src)+dur/slow-dur+gap
	if d := secs(out); d <= lo || d > hi+0.1 {
		t.Errorf("выход %.2f с, want (%.2f; %.2f]", d, lo, hi)
	}
}

// Карточка: вне диапазона — зажимается: slow=0.1 → 0.5 (растяжка ≤ dur/0.5),
// floor=−100 → −40 (после паузы уровень ≈ −40 дБ, а не −100).
func TestDropClampsOutOfRange(t *testing.T) {
	needFFmpeg(t)
	const start, dur, gap = 2.0, 2.0, 1.0
	in, src := genIn(t, dropExpr, 10)
	out := runChain(t, "drop", in, map[string]float64{
		"start": start, "dur": dur, "slow": 0.1, "gap": gap, "rise": 3, "floor": -100,
	})
	S := dropStretch(src, out, dur, gap)
	if S <= dur || S > dur/0.5+0.1 {
		t.Fatalf("slow=0.1: растяжка %.2f с, want зажато к slow=0.5 → (%.1f; %.1f]", S, dur, dur/0.5)
	}
	ref := dbfs(segRMS(src, 1, 1.5))
	t0 := start + S + gap
	if d := dbfs(segRMS(out, t0+0.02, t0+0.12)) - ref; math.Abs(d-(-40)) > 6 {
		t.Errorf("floor=−100: после паузы %+.1f дБ к входу, want ≈ −40 (зажато, ±6)", d)
	}
}
