package dsp

import (
	"math"
	"os/exec"
	"path/filepath"
	"testing"
)

// Тесты цепочки «Смягчить звон» (id "soften", карточка internal-stem-fx, условие 2):
// де-эссер для голоса — снижает 4–9 кГц на звонких местах, тело голоса почти не
// трогает. Имена крутилок карточкой не фиксируются — берутся Defaults().
// Синтетика 44.1 кГц: «голос» — гармоники 200 Гц до 2 кГц, «шипящие» — короткие
// всплески шума 6–8 кГц. Хелперы (needFFmpeg, decodeAt, toneAmpAt, dbfs) — из
// inserts_test.go / chains_test.go.

const (
	sfDur = 6.0
	// всплеск шипящего: [k+sfBurstAt, k+sfBurstAt+sfBurstLen) каждую секунду
	sfBurstAt  = 0.4
	sfBurstLen = 0.15
)

// sfVoiceExpr — «голос»: гармоники 200·k Гц, k = 1..10 (до 2 кГц), амплитуда 0.25/k.
const sfVoiceExpr = "0.25*sin(2*PI*200*t)+0.125*sin(2*PI*400*t)+0.0833*sin(2*PI*600*t)+" +
	"0.0625*sin(2*PI*800*t)+0.05*sin(2*PI*1000*t)+0.0417*sin(2*PI*1200*t)+" +
	"0.0357*sin(2*PI*1400*t)+0.03125*sin(2*PI*1600*t)+0.0278*sin(2*PI*1800*t)+0.025*sin(2*PI*2000*t)"

// sfGen — вход 44.1 кГц моно: голос + всплески полосового шума 6–8 кГц (детерминированный seed).
func sfGen(t *testing.T) string {
	t.Helper()
	needFFmpeg(t)
	in := filepath.Join(t.TempDir(), "in.wav")
	graph := "aevalsrc=exprs='" + sfVoiceExpr + "':d=6:s=44100[v];" +
		"anoisesrc=d=6:c=white:r=44100:a=0.9:seed=7," +
		"highpass=f=6000,highpass=f=6000,highpass=f=6000,lowpass=f=8000,lowpass=f=8000,lowpass=f=8000," +
		"volume='between(mod(t\\,1)\\,0.4\\,0.55)':eval=frame[n];" +
		"[v][n]amix=inputs=2:normalize=0:duration=first[out]"
	out, err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-filter_complex", graph, "-map", "[out]", "-ac", "1", "-ar", "44100", in).CombinedOutput()
	if err != nil {
		t.Fatalf("генерация входа: %v %s", err, out)
	}
	return in
}

// bandEnergy — энергия спектра в полосе [lo, hi] Гц на [from,to] с (ДПФ по бинам полосы).
func bandEnergy(s []float32, rate int, lo, hi, from, to float64) float64 {
	a, b := int(from*float64(rate)), int(to*float64(rate))
	if b > len(s) {
		b = len(s)
	}
	if a >= b {
		return 0
	}
	n := b - a
	k0, k1 := int(math.Ceil(lo*float64(n)/float64(rate))), int(math.Floor(hi*float64(n)/float64(rate)))
	var e float64
	for k := k0; k <= k1; k++ {
		w := 2 * math.Pi * float64(k) / float64(n)
		var re, im float64
		for i, v := range s[a:b] {
			re += float64(v) * math.Cos(w*float64(i))
			im -= float64(v) * math.Sin(w*float64(i))
		}
		e += re*re + im*im
	}
	return e
}

// burstsEnergy — суммарная энергия полосы 6–8 кГц во всех всплесках (кроме первой секунды).
func burstsEnergy(s []float32) float64 {
	var e float64
	for k := 1.0; k+1 <= sfDur; k++ {
		e += bandEnergy(s, dwSR, 6000, 8000, k+sfBurstAt, k+sfBurstAt+sfBurstLen)
	}
	return e
}

// bodyPower — мощность тела голоса: сумма квадратов амплитуд гармоник 200..2000 Гц на [from,to].
func bodyPower(s []float32, from, to float64) float64 {
	var p float64
	for hz := 200.0; hz <= 2000; hz += 200 {
		a := toneAmpAt(s, dwSR, hz, from, to)
		p += a * a
	}
	return p
}

func pdb(p float64) float64 {
	if p <= 0 {
		return -400
	}
	return 10 * math.Log10(p)
}

func softenRun(t *testing.T, in string) []float32 {
	t.Helper()
	c := ByID("soften")
	if c == nil {
		t.Fatal(`ByID("soften") = nil, цепочка должна существовать`)
	}
	out := filepath.Join(t.TempDir(), "out.wav")
	if err := Run(in, out, c.FilterGraph(c.Defaults()), nil); err != nil {
		t.Fatalf("Run soften (defaults): %v", err)
	}
	return decodeAt(t, out, dwSR)
}

// Карточка: цепочка доступна по ID и имеет имя; есть крутилки (сила, частота),
// дефолты лежат в своих диапазонах.
func TestSoftenByIDAndDefaults(t *testing.T) {
	c := ByID("soften")
	if c == nil || c.ID != "soften" || c.Name == "" {
		t.Fatalf(`ByID("soften") = %+v, want цепочку с этим ID и именем`, c)
	}
	if len(c.Params) < 2 {
		t.Errorf("крутилок %d, want ≥ 2 (сила и частота по карточке)", len(c.Params))
	}
	for _, p := range c.Params {
		if p.Min > p.Max || p.Default < p.Min || p.Default > p.Max {
			t.Errorf("%s: дефолт %v вне [%v, %v]", p.ID, p.Default, p.Min, p.Max)
		}
	}
}

// Карточка: с дефолтами Run не падает, длина выхода = длине входа (±0.05 с).
func TestSoftenDefaultsKeepLength(t *testing.T) {
	out := softenRun(t, sfGen(t))
	if d := float64(len(out)) / dwSR; math.Abs(d-sfDur) > 0.05 {
		t.Errorf("длина выхода %.3f с, want %.0f ± 0.05", d, sfDur)
	}
}

// Карточка: шипящие всплески 6–8 кГц ослаблены ≥ 4 дБ, тело голоса 200–2000 Гц
// изменилось не больше чем на 1 дБ.
func TestSoftenTamesSibilantsKeepsBody(t *testing.T) {
	in := sfGen(t)
	src := decodeAt(t, in, dwSR)
	out := softenRun(t, in)

	if e := burstsEnergy(src); e <= 0 {
		t.Fatal("во входе нет энергии 6–8 кГц — синтетика сломана")
	}
	if d := pdb(burstsEnergy(out)) - pdb(burstsEnergy(src)); d > -4 {
		t.Errorf("6–8 кГц во всплесках изменилось на %+.1f дБ, want ≤ −4", d)
	}
	if d := pdb(bodyPower(out, 0.5, sfDur-0.5)) - pdb(bodyPower(src, 0.5, sfDur-0.5)); math.Abs(d) > 1 {
		t.Errorf("тело голоса 200–2000 Гц изменилось на %+.1f дБ, want |Δ| ≤ 1", d)
	}
}
