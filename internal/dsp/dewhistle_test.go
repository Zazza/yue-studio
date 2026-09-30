package dsp

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// Тесты цепочки «Убрать свист» (id "dewhistle") по спецификации: узкий вырез на freq
// (и гармониках 2·freq, 3·freq ниже 20 кГц) глубиной depth в окне [start, end].
// Проверяется звук на синтетике 44.1 кГц через Run; уровень тона — одна точка ДПФ
// (toneAmpAt/decodeAt из inserts_test.go).

const dwSR = 44100

// dwGen — вход 44.1 кГц моно из выражения aevalsrc (6 с по умолчанию в тестах).
func dwGen(t *testing.T, expr string, dur float64) string {
	t.Helper()
	in := filepath.Join(t.TempDir(), "in.wav")
	lavfi(t, fmt.Sprintf("aevalsrc=exprs='%s':d=%g:s=%d", expr, dur, dwSR), in)
	return in
}

// dwRun — прогнать вход через dewhistle с параметрами p, вернуть выход 44.1 кГц моно.
func dwRun(t *testing.T, in string, p map[string]float64) []float32 {
	t.Helper()
	c := ByID("dewhistle")
	if c == nil {
		t.Fatalf(`ByID("dewhistle") = nil, цепочка должна существовать`)
	}
	out := filepath.Join(t.TempDir(), "out.wav")
	if err := Run(in, out, c.FilterGraph(p), nil); err != nil {
		t.Fatalf("Run dewhistle %v: %v", p, err)
	}
	return decodeAt(t, out, dwSR)
}

// dwDelta — изменение уровня тона hz на [from,to] выход относительно входа, дБ
// (отрицательное — ослаблен).
func dwDelta(src, out []float32, hz, from, to float64) float64 {
	return dbfs(toneAmpAt(out, dwSR, hz, from, to)) - dbfs(toneAmpAt(src, dwSR, hz, from, to))
}

// Двухтональная синтетика спецификации: музыка 1000 Гц + свист 5000 Гц, равные амплитуды.
const dwMusicWhistle = "0.3*sin(2*PI*1000*t)+0.3*sin(2*PI*5000*t)"

var dwBase = map[string]float64{"freq": 5000, "depth": 30, "width": 60, "harmonics": 1, "start": 2, "end": 4}

func dwWith(over map[string]float64) map[string]float64 {
	p := map[string]float64{}
	for k, v := range dwBase {
		p[k] = v
	}
	for k, v := range over {
		p[k] = v
	}
	return p
}

// Спецификация: цепочка доступна по ID и имеет имя.
func TestDewhistleByID(t *testing.T) {
	c := ByID("dewhistle")
	if c == nil || c.ID != "dewhistle" || c.Name == "" {
		t.Fatalf(`ByID("dewhistle") = %+v, want цепочку с этим ID и именем`, c)
	}
}

// Спецификация: крутилки freq/depth/width/harmonics/start/end с заданными диапазонами;
// есть свой start — общего from нет; дефолты лежат внутри диапазонов.
func TestDewhistleParams(t *testing.T) {
	c := ByID("dewhistle")
	if c == nil {
		t.Fatal(`нет цепочки "dewhistle"`)
	}
	want := map[string][2]float64{
		"freq": {1000, 16000}, "depth": {6, 40}, "width": {10, 400}, "harmonics": {1, 3},
	}
	got := map[string]Param{}
	for _, p := range c.Params {
		got[p.ID] = p
	}
	for id, r := range want {
		p, ok := got[id]
		if !ok {
			t.Errorf("нет параметра %s", id)
			continue
		}
		if p.Min != r[0] || p.Max != r[1] {
			t.Errorf("%s: диапазон [%v, %v], want [%v, %v]", id, p.Min, p.Max, r[0], r[1])
		}
	}
	for _, id := range []string{"start", "end"} {
		if _, ok := got[id]; !ok {
			t.Errorf("нет параметра %s", id)
		}
	}
	if _, ok := got["end"]; ok && got["end"].Min > 0 {
		t.Errorf("end: Min %v, want 0 допустимо (0 — до конца трека)", got["end"].Min)
	}
	if hasParam(c, "from") {
		t.Error("есть общий параметр from, хотя у цепочки свой start")
	}
	for _, p := range c.Params {
		if p.Default < p.Min || p.Default > p.Max {
			t.Errorf("%s: дефолт %v вне [%v, %v]", p.ID, p.Default, p.Min, p.Max)
		}
	}
}

// Спецификация: с дефолтами Run не падает, длина выхода = длине входа (±0.05 с).
func TestDewhistleDefaultsKeepLength(t *testing.T) {
	needFFmpeg(t)
	in := dwGen(t, dwMusicWhistle, 6)
	c := ByID("dewhistle")
	if c == nil {
		t.Fatal(`нет цепочки "dewhistle"`)
	}
	out := dwRun(t, in, c.Defaults())
	if d := float64(len(out)) / dwSR; math.Abs(d-6) > 0.05 {
		t.Errorf("длина выхода %.3f с, want 6 ± 0.05", d)
	}
}

// Спецификация: в окне 2.3–3.7 с свист 5000 Гц ослаблен ≥ 20 дБ, музыка 1000 Гц —
// в пределах 3 дБ; вне окна свист не тронут (≤ 1 дБ); длина не меняется.
func TestDewhistleNotchInWindow(t *testing.T) {
	needFFmpeg(t)
	in := dwGen(t, dwMusicWhistle, 6)
	src := decodeAt(t, in, dwSR)
	out := dwRun(t, in, dwBase)

	if d := dwDelta(src, out, 5000, 2.3, 3.7); d > -20 {
		t.Errorf("5000 Гц в окне изменился на %+.1f дБ, want ≤ −20", d)
	}
	if d := dwDelta(src, out, 1000, 2.3, 3.7); math.Abs(d) > 3 {
		t.Errorf("1000 Гц в окне изменился на %+.1f дБ, want |Δ| ≤ 3", d)
	}
	for _, w := range [][2]float64{{0.2, 1.7}, {4.3, 5.8}} {
		if d := dwDelta(src, out, 5000, w[0], w[1]); math.Abs(d) > 1 {
			t.Errorf("5000 Гц вне окна %.1f–%.1f с изменился на %+.1f дБ, want |Δ| ≤ 1", w[0], w[1], d)
		}
		if d := dwDelta(src, out, 1000, w[0], w[1]); math.Abs(d) > 1 {
			t.Errorf("1000 Гц вне окна %.1f–%.1f с изменился на %+.1f дБ, want |Δ| ≤ 1", w[0], w[1], d)
		}
	}
	if d := float64(len(out)) / dwSR; math.Abs(d-6) > 0.05 {
		t.Errorf("длина выхода %.3f с, want 6 ± 0.05", d)
	}
}

// Спецификация: end=0 — вырез до конца трека: свист ослаблен и в 4.3–5.8 с,
// а до start не тронут.
func TestDewhistleEndZeroToTrackEnd(t *testing.T) {
	needFFmpeg(t)
	in := dwGen(t, dwMusicWhistle, 6)
	src := decodeAt(t, in, dwSR)
	out := dwRun(t, in, dwWith(map[string]float64{"end": 0}))

	for _, w := range [][2]float64{{2.3, 3.7}, {4.3, 5.8}} {
		if d := dwDelta(src, out, 5000, w[0], w[1]); d > -20 {
			t.Errorf("end=0: 5000 Гц в %.1f–%.1f с изменился на %+.1f дБ, want ≤ −20", w[0], w[1], d)
		}
	}
	if d := dwDelta(src, out, 5000, 0.2, 1.7); math.Abs(d) > 1 {
		t.Errorf("end=0: 5000 Гц до start изменился на %+.1f дБ, want |Δ| ≤ 1", d)
	}
}

// Спецификация: harmonics=2 режет и 2·freq — при свисте 5000+10000 Гц оба ослаблены
// ≥ 20 дБ в окне; harmonics=1 оставляет 10000 Гц почти нетронутым (≤ 3 дБ).
func TestDewhistleHarmonics(t *testing.T) {
	needFFmpeg(t)
	in := dwGen(t, "0.3*sin(2*PI*1000*t)+0.3*sin(2*PI*5000*t)+0.3*sin(2*PI*10000*t)", 6)
	src := decodeAt(t, in, dwSR)

	out2 := dwRun(t, in, dwWith(map[string]float64{"harmonics": 2}))
	for _, hz := range []float64{5000, 10000} {
		if d := dwDelta(src, out2, hz, 2.3, 3.7); d > -20 {
			t.Errorf("harmonics=2: %.0f Гц в окне изменился на %+.1f дБ, want ≤ −20", hz, d)
		}
	}
	if d := dwDelta(src, out2, 1000, 2.3, 3.7); math.Abs(d) > 3 {
		t.Errorf("harmonics=2: 1000 Гц в окне изменился на %+.1f дБ, want |Δ| ≤ 3", d)
	}

	out1 := dwRun(t, in, dwWith(map[string]float64{"harmonics": 1}))
	if d := dwDelta(src, out1, 5000, 2.3, 3.7); d > -20 {
		t.Errorf("harmonics=1: 5000 Гц в окне изменился на %+.1f дБ, want ≤ −20", d)
	}
	if d := dwDelta(src, out1, 10000, 2.3, 3.7); math.Abs(d) > 3 {
		t.Errorf("harmonics=1: 10000 Гц в окне изменился на %+.1f дБ, want |Δ| ≤ 3", d)
	}
}

// Спецификация (край): harmonics=3 режет и 3·freq, если ниже 20 кГц (5000 → 15000 Гц);
// при freq=16000 гармоники 32/48 кГц выше 20 кГц — граф всё равно валиден, длина цела,
// основной тон ослаблен.
func TestDewhistleThirdHarmonicAndCap(t *testing.T) {
	needFFmpeg(t)
	in := dwGen(t, "0.3*sin(2*PI*5000*t)+0.3*sin(2*PI*15000*t)", 6)
	src := decodeAt(t, in, dwSR)
	out := dwRun(t, in, dwWith(map[string]float64{"harmonics": 3}))
	if d := dwDelta(src, out, 15000, 2.3, 3.7); d > -20 {
		t.Errorf("harmonics=3: 15000 Гц в окне изменился на %+.1f дБ, want ≤ −20", d)
	}

	hi := dwGen(t, "0.3*sin(2*PI*1000*t)+0.3*sin(2*PI*16000*t)", 6)
	hiSrc := decodeAt(t, hi, dwSR)
	hiOut := dwRun(t, hi, dwWith(map[string]float64{"freq": 16000, "harmonics": 3}))
	if d := float64(len(hiOut)) / dwSR; math.Abs(d-6) > 0.05 {
		t.Errorf("freq=16000 harmonics=3: длина %.3f с, want 6 ± 0.05", d)
	}
	if d := dwDelta(hiSrc, hiOut, 16000, 2.3, 3.7); d > -20 {
		t.Errorf("freq=16000: 16000 Гц в окне изменился на %+.1f дБ, want ≤ −20", d)
	}
}

// Несколько тонов за проход: freq2/freq3 режутся так же, 0 — выключен.
func TestDewhistleExtraFreqs(t *testing.T) {
	c := ByID("dewhistle")
	if c == nil {
		t.Fatal("нет цепочки")
	}
	d := c.Defaults()
	if d["freq2"] != 0 || d["freq3"] != 0 {
		t.Fatalf("по умолчанию доп. тоны выключены: %v", d)
	}
	g := c.FilterGraph(map[string]float64{"freq": 5000, "freq2": 3500, "freq3": 0, "harmonics": 1})
	if !strings.Contains(g, "f=5000") || !strings.Contains(g, "f=3500") || strings.Count(g, "equalizer=") != 2 {
		t.Errorf("граф %s: нужны вырезы 5000 и 3500, без третьего", g)
	}
}
