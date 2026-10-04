package dsp

import (
	"fmt"
	"math"
	"testing"
)

// Тесты карточки internal-dsp-space, разделы 1–3: реверб, дилей в темп, стерео
// (ширина, Хаас). Хелперы nc* — из newchains_test.go.

var ncReverbIDs = []string{"reverb-room", "reverb-hall", "reverb-plate", "reverb-spring"}

// --- 1. Реверб ---

// ncToneThenSilence — тон 440 Гц амплитуды 0.5 первую секунду, затем тишина.
const ncToneThenSilence = "0.5*sin(2*PI*440*t)*lt(t\\,1)"

// Карточка 1 (параметры): size/predelay/tone/wet/width у всех ревербов; диапазоны size.
func TestReverbParams(t *testing.T) {
	sizes := map[string][3]float64{
		"reverb-room": {0.3, 1.5, 0.6}, "reverb-hall": {1, 6, 2.5},
		"reverb-plate": {0.5, 4, 1.6}, "reverb-spring": {0.5, 3, 1.2},
	}
	for _, id := range ncReverbIDs {
		c := ByID(id)
		if c == nil {
			t.Errorf("%s не найден", id)
			continue
		}
		got := map[string]Param{}
		for _, p := range c.Params {
			got[p.ID] = p
		}
		for _, pid := range []string{"size", "predelay", "tone", "wet", "width"} {
			if _, ok := got[pid]; !ok {
				t.Errorf("%s: нет параметра %s", id, pid)
			}
		}
		if s, want := got["size"], sizes[id]; s.Min != want[0] || s.Max != want[1] || s.Default != want[2] {
			t.Errorf("%s: size [%v..%v] деф %v, want [%v..%v] деф %v", id, s.Min, s.Max, s.Default, want[0], want[1], want[2])
		}
		if p := got["predelay"]; p.Min != 0 || p.Max != 150 {
			t.Errorf("%s: predelay [%v..%v], want [0..150]", id, p.Min, p.Max)
		}
		if p := got["tone"]; p.Min != 1 || p.Max != 16 {
			t.Errorf("%s: tone [%v..%v], want [1..16]", id, p.Min, p.Max)
		}
		for _, pid := range []string{"wet", "width"} {
			if p := got[pid]; p.Min != 0 || p.Max != 1 {
				t.Errorf("%s: %s [%v..%v], want [0..1]", id, pid, p.Min, p.Max)
			}
		}
	}
}

// Карточка 1.1 (изменена 2026-10-04): вход — шумовой всплеск 1 с, wet 0.5, predelay 0:
// в окне 0–0.1 с после конца звука хвост > −40 дБFS; окна по 0.2 с монотонно падают
// (пока уровень > −70 дБ); скорость спада ≈ 60 дБ за size (±40 %) — наклон (МНК) по
// окнам 50 мс выше −70 дБ.
func TestReverbTailAudibleAndDecays(t *testing.T) {
	needFFmpeg(t)
	in := ncNoise(t, 0.5, 1, 4.5, chSR)
	for _, id := range ncReverbIDs {
		t.Run(id, func(t *testing.T) {
			size := ncChain(t, id).Defaults()["size"]
			out := decode(t, ncRun(t, id, in, map[string]float64{"wet": 0.5, "predelay": 0}))
			if l := dbfs(segRMS(out, 1.0, 1.1)); l <= -40 {
				t.Errorf("0–0.1 с после конца звука %.1f дБFS, want > −40", l)
			}
			prev := math.Inf(1)
			for w := 1.0; w+0.2 <= 4.5; w += 0.2 {
				cur := dbfs(segRMS(out, w, w+0.2))
				if cur < -70 {
					break // хвост ушёл ниже −70 дБ — дальше сравнивать нечего
				}
				if cur >= prev {
					t.Errorf("окно %.1f–%.1f с: %.1f дБ не тише предыдущего (%.1f дБ)", w, w+0.2, cur, prev)
				}
				prev = cur
			}
			// наклон — по окнам 50 мс (у комнаты size 0.6 окон по 0.2 с выше −70 дБ всего 2–3)
			var xs, ys []float64
			for w := 1.0; w+0.05 <= 4.5; w += 0.05 {
				cur := dbfs(segRMS(out, w, w+0.05))
				if cur < -70 {
					break
				}
				xs, ys = append(xs, w+0.025), append(ys, cur)
			}
			if len(xs) < 3 {
				t.Fatalf("окон выше −70 дБ %d, want ≥ 3 для оценки спада", len(xs))
			}
			var mx, my float64
			for i := range xs {
				mx += xs[i]
				my += ys[i]
			}
			mx /= float64(len(xs))
			my /= float64(len(xs))
			var sxy, sxx float64
			for i := range xs {
				sxy += (xs[i] - mx) * (ys[i] - my)
				sxx += (xs[i] - mx) * (xs[i] - mx)
			}
			perSize := -sxy / sxx * size // дБ спада за size секунд
			if perSize < 36 || perSize > 84 {
				t.Errorf("спад %.1f дБ за size %.2f с, want 60 ± 40%% (36…84)", perSize, size)
			}
		})
	}
}

// Карточка 1.2: больший size → громче хвост через 1 с после конца звука.
// В карточке «size 2.5 vs 0.6 у зала-диапазона», но минимум size у зала — 1
// (0.6 зажмётся до 1), поэтому сравнение у зала 2.5 vs 1.
func TestReverbBiggerSizeLongerTail(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncToneThenSilence, 4, chSR)
	big := decode(t, ncRun(t, "reverb-hall", in, map[string]float64{"size": 2.5, "wet": 0.5, "predelay": 0}))
	small := decode(t, ncRun(t, "reverb-hall", in, map[string]float64{"size": 1, "wet": 0.5, "predelay": 0}))
	lb, ls := dbfs(segRMS(big, 1.9, 2.1)), dbfs(segRMS(small, 1.9, 2.1))
	if lb <= ls {
		t.Errorf("через 1 с после конца: size 2.5 — %.1f дБ, size 1 — %.1f дБ, want больший size громче", lb, ls)
	}
}

// Карточка 1.3: predelay N мс — мокрый сигнал (wet 1, вход — щелчок) появляется не
// раньше N − 5 мс после щелчка. Мокрый = выход − вход (сухой не убавляется, 1.6).
func TestReverbPredelay(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, "0.8*between(t\\,0.5\\,0.505)", 2.5, chSR)
	src := decode(t, in)
	for _, id := range []string{"reverb-room", "reverb-hall"} {
		for _, pd := range []float64{50, 100} {
			out := decode(t, ncRun(t, id, in, map[string]float64{"predelay": pd, "wet": 1}))
			n := min(len(out), len(src))
			wet := make([]float32, n)
			for i := range wet {
				wet[i] = out[i] - src[i]
			}
			at := firstPeak(wet, 0.01)
			if at < 0 {
				t.Errorf("%s predelay %v: мокрого сигнала нет", id, pd)
				continue
			}
			if d := (at - 0.5) * 1000; d < pd-5 {
				t.Errorf("%s predelay %v мс: мокрый появился через %.1f мс после щелчка, want ≥ %.0f", id, pd, d, pd-5)
			}
		}
	}
}

// Карточка 1.4: tone — доля энергии > 5 кГц в хвосте при tone 2 кГц меньше, чем при
// 12 кГц, на ≥ 6 дБ. Вход — всплеск белого шума 1 с.
func TestReverbToneDarkensTail(t *testing.T) {
	needFFmpeg(t)
	in := ncNoise(t, 0.5, 1, 3, ncSR)
	for _, id := range ncReverbIDs {
		run := func(tone float64) float64 {
			out := decodeAt(t, ncRun(t, id, in, map[string]float64{"tone": tone, "wet": 1, "predelay": 0}), ncSR)
			return ncHighShareDb(out, ncSR, 1.1, 1.5, 5000)
		}
		dark, bright := run(2), run(12)
		if bright-dark < 6 {
			t.Errorf("%s: доля > 5 кГц в хвосте tone 2 — %.1f дБ, tone 12 — %.1f дБ, want разница ≥ 6", id, dark, bright)
		}
	}
}

// Карточка 1.5: wet 0 → выход = вход (оба канала).
func TestReverbWetZeroIsDry(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncToneThenSilence, 3, chSR)
	srcL, _ := ncDecode2(t, in, chSR)
	for _, id := range ncReverbIDs {
		l, r := ncDecode2(t, ncRun(t, id, in, map[string]float64{"wet": 0}), chSR)
		if d := ncDiffDb(l, srcL, chSR, 0, 2.95); d > -50 {
			t.Errorf("%s wet 0: L отличается от входа на %.1f дБ, want ≤ −50", id, d)
		}
		if d := ncDiffDb(r, srcL, chSR, 0, 2.95); d > -50 {
			t.Errorf("%s wet 0: R отличается от входа на %.1f дБ, want ≤ −50", id, d)
		}
	}
}

// Карточка 1.5: width 0 → в хвосте L ≈ R (корреляция > 0.95); width 1 → < 0.5.
func TestReverbWidth(t *testing.T) {
	needFFmpeg(t)
	in := ncNoise(t, 0.5, 1, 3, chSR)
	for _, id := range ncReverbIDs {
		corr := func(w float64) float64 {
			l, r := ncDecode2(t, ncRun(t, id, in, map[string]float64{"width": w, "wet": 1, "predelay": 0}), chSR)
			return ncCorr(ncSeg(l, chSR, 1.05, 1.5), ncSeg(r, chSR, 1.05, 1.5))
		}
		if c := corr(0); c <= 0.95 {
			t.Errorf("%s width 0: корреляция L/R хвоста %.2f, want > 0.95", id, c)
		}
		if c := corr(1); c >= 0.5 {
			t.Errorf("%s width 1: корреляция L/R хвоста %.2f, want < 0.5", id, c)
		}
	}
}

// Карточка 1.6 (изменена 2026-10-04 решением человека): во время звучания сухой не
// ослаблен — на широкополосном шуме уровень в середине звучания (0.8–1.4 с, хвост уже
// набрался) ≥ входа − 1 дБ в каждом канале; параметры по умолчанию; 44.1 и 48 кГц.
func TestReverbDryNotAttenuated(t *testing.T) {
	needFFmpeg(t)
	for _, rate := range []int{44100, 48000} {
		in := ncNoise(t, 0.3, 1.6, 3, rate)
		src, _ := ncDecode2(t, in, rate)
		ref := dbfs(ncRMS(src, rate, 0.8, 1.4))
		for _, id := range ncReverbIDs {
			l, r := ncDecode2(t, ncRun(t, id, in, nil), rate)
			for ch, s := range map[string][]float32{"L": l, "R": r} {
				if d := dbfs(ncRMS(s, rate, 0.8, 1.4)) - ref; d < -1 {
					t.Errorf("%s %d Гц, %s: уровень во время звучания %+.2f дБ к входу, want ≥ −1", id, rate, ch, d)
				}
			}
		}
	}
}

// Карточка 1.7: TailSec = size + predelay/1000.
func TestReverbTailSec(t *testing.T) {
	for _, id := range ncReverbIDs {
		c := ByID(id)
		if c == nil {
			t.Errorf("%s не найден", id)
			continue
		}
		d := c.Defaults()
		if got, want := c.TailSec(nil), d["size"]+d["predelay"]/1000; math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: TailSec(defaults) = %v, want %v", id, got, want)
		}
		var size float64
		for _, p := range c.Params {
			if p.ID == "size" {
				size = p.Max
			}
		}
		if got, want := c.TailSec(map[string]float64{"size": size, "predelay": 120}), size+0.12; math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: TailSec(size %v, predelay 120) = %v, want %v", id, size, got, want)
		}
		// вне диапазона — зажимается, как в FilterGraph
		if got, want := c.TailSec(map[string]float64{"size": size + 100, "predelay": 0}), size; math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: TailSec(size > max) = %v, want %v (зажим)", id, got, want)
		}
	}
}

// --- 2. Дилей ---

var ncDelayMult = map[int]float64{1: 1, 2: 0.5, 3: 0.75, 4: 0.25, 5: 1.5}

func ncDelaySec(bpm float64, div int) float64 { return 60 / bpm * ncDelayMult[div] }

// ncClick10ms — щелчок 10 мс на 0.2 с.
const ncClick10ms = "0.8*between(t\\,0.2\\,0.21)"

// ncEchoOnset — начало повтора в окне ±30 мс вокруг ожидаемого: первый отсчёт выше
// половины максимума окна (с); amp — максимум окна.
func ncEchoOnset(s []float32, rate int, at float64) (onset, amp float64) {
	from := at - 0.03
	seg := ncSeg(s, rate, from, at+0.03)
	for _, v := range seg {
		amp = math.Max(amp, math.Abs(float64(v)))
	}
	for i, v := range seg {
		if math.Abs(float64(v)) >= amp/2 {
			return from + float64(i)/float64(rate), amp
		}
	}
	return -1, amp
}

// Карточка 2 (параметры): bpm/div/feedback/pingpong/cut/wet с диапазонами и дефолтами.
func TestDelayParams(t *testing.T) {
	c := ByID("delay")
	if c == nil {
		t.Fatal("delay не найден")
	}
	want := map[string][3]float64{
		"bpm": {40, 240, 120}, "div": {1, 5, 3}, "feedback": {0, 0.9, 0.45},
		"cut": {1, 16, 6}, "wet": {0, 1, 0.5},
	}
	got := map[string]Param{}
	for _, p := range c.Params {
		got[p.ID] = p
	}
	for id, w := range want {
		p, ok := got[id]
		if !ok {
			t.Errorf("нет параметра %s", id)
			continue
		}
		if p.Min != w[0] || p.Max != w[1] || p.Default != w[2] {
			t.Errorf("%s: [%v..%v] деф %v, want [%v..%v] деф %v", id, p.Min, p.Max, p.Default, w[0], w[1], w[2])
		}
	}
	if p, ok := got["pingpong"]; !ok || p.Default != 1 {
		t.Errorf("pingpong: есть=%v деф %v, want деф 1", ok, p.Default)
	}
}

// Карточка 2.1: повторы щелчка на k·d (k = 1, 2, 3) ±2 мс, d = 60/bpm × доля.
func TestDelayEchoTiming(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncClick10ms, 3, ncSR)
	for _, tc := range []struct {
		bpm float64
		div int
	}{{120, 1}, {120, 3}, {100, 4}, {90, 2}} {
		d := ncDelaySec(tc.bpm, tc.div)
		l, _ := ncDecode2(t, ncRun(t, "delay", in, map[string]float64{
			"bpm": tc.bpm, "div": float64(tc.div), "feedback": 0.5, "wet": 0.5, "pingpong": 0, "cut": 16,
		}), ncSR)
		dry, _ := ncEchoOnset(l, ncSR, 0.2)
		for k := 1; k <= 3; k++ {
			at := 0.2 + float64(k)*d
			if at+0.05 > 3 {
				break
			}
			on, _ := ncEchoOnset(l, ncSR, at)
			if math.Abs((on-dry)-float64(k)*d) > 0.002 {
				t.Errorf("bpm %v div %d: повтор %d через %.4f с, want %.4f ± 0.002", tc.bpm, tc.div, k, on-dry, float64(k)*d)
			}
		}
	}
}

// Карточка 2.1: на непрерывном материале пик автокорреляции выхода — на задержке d.
func TestDelayAutocorrPeak(t *testing.T) {
	needFFmpeg(t)
	in := ncNoise(t, 0.3, 3, 3, chSR)
	d := ncDelaySec(120, 3) // 0.375
	l, _ := ncDecode2(t, ncRun(t, "delay", in, map[string]float64{
		"bpm": 120, "div": 3, "feedback": 0.45, "wet": 0.5, "pingpong": 0, "cut": 16,
	}), chSR)
	if lag := ncAutocorrPeak(ncSeg(l, chSR, 0.5, 3), chSR, 0.02, 0.6); math.Abs(lag-d) > 0.002 {
		t.Errorf("пик автокорреляции на %.4f с, want %.4f ± 0.002", lag, d)
	}
}

// Карточка 2.2: амплитуда k-го повтора ≈ wet·feedback^(k−1) от сухого (±2 дБ), затухают.
func TestDelayEchoAmplitude(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncClick10ms, 3, ncSR)
	const wet, fb = 0.5, 0.5
	d := ncDelaySec(120, 4) // 0.125
	l, _ := ncDecode2(t, ncRun(t, "delay", in, map[string]float64{
		"bpm": 120, "div": 4, "feedback": fb, "wet": wet, "pingpong": 0, "cut": 16,
	}), ncSR)
	_, dry := ncEchoOnset(l, ncSR, 0.2)
	prev := dry
	for k := 1; k <= 4; k++ {
		_, a := ncEchoOnset(l, ncSR, 0.2+float64(k)*d)
		want := wet * math.Pow(fb, float64(k-1))
		if diff := dbfs(a/dry) - dbfs(want); math.Abs(diff) > 2 {
			t.Errorf("повтор %d: %.3f от сухого (%.1f дБ), want %.3f (±2 дБ)", k, a/dry, dbfs(a/dry), want)
		}
		if a >= prev {
			t.Errorf("повтор %d (%.3f) не тише предыдущего (%.3f)", k, a, prev)
		}
		prev = a
	}
}

// Карточка 2.3: pingpong 1, моно-щелчок — нечётные повторы в L (R тише ≥ 20 дБ),
// чётные — в R; pingpong 0 — в обоих каналах поровну.
func TestDelayPingPong(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncClick10ms, 3, ncSR)
	d := ncDelaySec(120, 2) // 0.25
	p := map[string]float64{"bpm": 120, "div": 2, "feedback": 0.6, "wet": 0.7, "pingpong": 1, "cut": 16}
	l, r := ncDecode2(t, ncRun(t, "delay", in, p), ncSR)
	for k := 1; k <= 4; k++ {
		at := 0.2 + float64(k)*d
		_, al := ncEchoOnset(l, ncSR, at)
		_, ar := ncEchoOnset(r, ncSR, at)
		loud, quiet, side := al, ar, "L"
		if k%2 == 0 {
			loud, quiet, side = ar, al, "R"
		}
		if dbfs(loud)-dbfs(quiet) < 20 {
			t.Errorf("pingpong 1, повтор %d: должен быть в %s; L %.4f, R %.4f (разница %.1f дБ, want ≥ 20)",
				k, side, al, ar, dbfs(loud)-dbfs(quiet))
		}
	}
	p["pingpong"] = 0
	l, r = ncDecode2(t, ncRun(t, "delay", in, p), ncSR)
	for k := 1; k <= 2; k++ {
		at := 0.2 + float64(k)*d
		_, al := ncEchoOnset(l, ncSR, at)
		_, ar := ncEchoOnset(r, ncSR, at)
		if math.Abs(dbfs(al)-dbfs(ar)) > 1 || al < 0.01 {
			t.Errorf("pingpong 0, повтор %d: L %.4f, R %.4f, want поровну (±1 дБ)", k, al, ar)
		}
	}
}

// Карточка 2.4: cut 2 кГц — доля > 5 кГц в первом повторе ≥ 10 дБ ниже, чем в сухом.
func TestDelayCutDarkensRepeats(t *testing.T) {
	needFFmpeg(t)
	in := ncNoise(t, 0.5, 0.2, 2, ncSR)
	l, _ := ncDecode2(t, ncRun(t, "delay", in, map[string]float64{
		"bpm": 120, "div": 1, "feedback": 0, "wet": 1, "pingpong": 0, "cut": 2,
	}), ncSR)
	dry := ncHighShareDb(l, ncSR, 0.01, 0.19, 5000)
	rep := ncHighShareDb(l, ncSR, 0.51, 0.69, 5000)
	if dry-rep < 10 {
		t.Errorf("доля > 5 кГц: сухой %.1f дБ, первый повтор %.1f дБ, want повтор ниже ≥ 10 дБ", dry, rep)
	}
}

// Карточка 2.5: TailSec = d × число повторов (до уровня < 1 %, не больше 16).
// Карточка не уточняет, считается ли уровень с учётом wet и включается ли первый
// повтор ниже 1 % — проверяется то, что следует однозначно: кратность d, один
// повтор без обратной связи, потолок 16 при feedback 0.9 и рост с feedback.
func TestDelayTailSec(t *testing.T) {
	c := ByID("delay")
	if c == nil {
		t.Fatal("delay не найден")
	}
	for _, div := range []int{1, 3, 5} {
		d := ncDelaySec(100, div)
		base := map[string]float64{"bpm": 100, "div": float64(div), "wet": 0.5}
		with := func(fb float64) float64 {
			p := map[string]float64{"feedback": fb}
			for k, v := range base {
				p[k] = v
			}
			return c.TailSec(p)
		}
		if got := with(0); math.Abs(got-d) > 1e-6 {
			t.Errorf("div %d feedback 0: TailSec %v, want d = %v (один повтор)", div, got, d)
		}
		if got := with(0.9); math.Abs(got-16*d) > 1e-6 {
			t.Errorf("div %d feedback 0.9: TailSec %v, want 16·d = %v (потолок)", div, got, 16*d)
		}
		prev := 0.0
		for _, fb := range []float64{0, 0.2, 0.45, 0.7, 0.9} {
			got := with(fb)
			if n := got / d; math.Abs(n-math.Round(n)) > 1e-6 || n < 1 || n > 16 {
				t.Errorf("div %d feedback %v: TailSec/d = %v, want целое 1..16", div, fb, n)
			}
			if got < prev {
				t.Errorf("div %d: TailSec уменьшился с ростом feedback (%v → %v)", div, prev, got)
			}
			prev = got
		}
	}
}

// --- 3. Стерео ---

// ncCorrStereo — стерео шум: L = n1 + 0.5·n2, R = n1 − 0.5·n2 (корреляция 0.6).
func ncCorrStereo(t *testing.T, dur float64) string {
	t.Helper()
	return ncFile(t, fmt.Sprintf("anoisesrc=d=%[1]g:c=white:seed=11:a=0.3:r=16000[a];"+
		"anoisesrc=d=%[1]g:c=white:seed=23:a=0.3:r=16000[b];"+
		"[a][b]amerge=inputs=2,pan=stereo|c0=c0+0.5*c1|c1=c0-0.5*c1[out0]", dur))
}

// Карточка 3.1: width 0 → L = R; width 1, bass 0 → выход ≈ вход; width 2 → корреляция
// L/R ниже входной.
func TestWidthChain(t *testing.T) {
	needFFmpeg(t)
	in := ncCorrStereo(t, 3)
	sl, sr := ncDecode2(t, in, chSR)
	cin := ncCorr(ncSeg(sl, chSR, 0.2, 2.8), ncSeg(sr, chSR, 0.2, 2.8))

	l, r := ncDecode2(t, ncRun(t, "width", in, map[string]float64{"width": 0, "bass": 0}), chSR)
	if d := ncDiffDb(l, r, chSR, 0.1, 2.9); d > -50 {
		t.Errorf("width 0: L−R %.1f дБ к R, want L = R (≤ −50 дБ)", d)
	}
	l, r = ncDecode2(t, ncRun(t, "width", in, map[string]float64{"width": 1, "bass": 0}), chSR)
	if d := ncDiffDb(l, sl, chSR, 0.1, 2.9); d > -30 {
		t.Errorf("width 1: L отличается от входа на %.1f дБ, want ≈ вход (≤ −30)", d)
	}
	if d := ncDiffDb(r, sr, chSR, 0.1, 2.9); d > -30 {
		t.Errorf("width 1: R отличается от входа на %.1f дБ, want ≈ вход (≤ −30)", d)
	}
	l, r = ncDecode2(t, ncRun(t, "width", in, map[string]float64{"width": 2, "bass": 0}), chSR)
	if c := ncCorr(ncSeg(l, chSR, 0.2, 2.8), ncSeg(r, chSR, 0.2, 2.8)); c >= cin-0.05 {
		t.Errorf("width 2: корреляция L/R %.2f, входная %.2f — want ниже", c, cin)
	}
}

// Карточка 3.2: bass N при width 2 — низ ниже N Гц в моно: корреляция L/R полосы
// < N/2 Гц > 0.95.
func TestWidthBassMono(t *testing.T) {
	needFFmpeg(t)
	in := ncCorrStereo(t, 3)
	out := ncRun(t, "width", in, map[string]float64{"width": 2, "bass": 120})
	l, r := ncDecode2AF(t, out, chSR, "lowpass=f=60,lowpass=f=60,lowpass=f=60")
	if c := ncCorr(ncSeg(l, chSR, 0.3, 2.7), ncSeg(r, chSR, 0.3, 2.7)); c <= 0.95 {
		t.Errorf("bass 120: корреляция L/R ниже 60 Гц %.3f, want > 0.95 (низ в моно)", c)
	}
}

// Карточка 3.3: Хаас на моно-входе — пик взаимной корреляции L/R на лаге delay мс
// ±1 мс; side выбирает, какой канал позже.
func TestHaasDelaySide(t *testing.T) {
	needFFmpeg(t)
	in := ncNoise(t, 0.3, 2, 2, chSR)
	for _, tc := range []struct{ delay, side float64 }{{18, 0}, {18, 1}, {25, 0}, {12, 1}} {
		l, r := ncDecode2(t, ncRun(t, "haas", in, map[string]float64{"delay": tc.delay, "side": tc.side, "mix": 1}), chSR)
		lag := float64(ncXcorrLag(ncSeg(l, chSR, 0.2, 1.8), ncSeg(r, chSR, 0.2, 1.8), chSR*40/1000)) / chSR * 1000
		want := tc.delay // side 0 — правый позже: R отстаёт, лаг положительный
		if tc.side == 1 {
			want = -tc.delay
		}
		if math.Abs(lag-want) > 1 {
			t.Errorf("delay %v side %v: лаг L→R %.1f мс, want %+.0f ± 1", tc.delay, tc.side, lag, want)
		}
	}
}

// Карточка 1.5 на стерео-входе с независимыми L/R (кросс-ревью): width 0 → в хвосте
// L ≈ R (корреляция > 0.95), width 1 → < 0.5. Вход — шумовой всплеск 1 с, разные
// зёрна в каналах, 44.1 кГц.
func TestReverbWidthStereoInput(t *testing.T) {
	needFFmpeg(t)
	in := ncFile(t, "anoisesrc=d=1:c=white:seed=31:a=0.5:r=44100[a];"+
		"anoisesrc=d=1:c=white:seed=47:a=0.5:r=44100[b];"+
		"[a][b]amerge=inputs=2,apad=whole_dur=3[out0]")
	if n := ncChannels(t, in); n != 2 {
		t.Fatalf("вход: каналов %d, want 2", n)
	}
	for _, id := range ncReverbIDs {
		corr := func(w float64) float64 {
			l, r := ncDecode2(t, ncRun(t, id, in, map[string]float64{"width": w, "wet": 1, "predelay": 0}), ncSR)
			return ncCorr(ncSeg(l, ncSR, 1.05, 1.5), ncSeg(r, ncSR, 1.05, 1.5))
		}
		if c := corr(0); c <= 0.95 {
			t.Errorf("%s стерео-вход, width 0: корреляция L/R хвоста %.2f, want > 0.95", id, c)
		}
		if c := corr(1); c >= 0.5 {
			t.Errorf("%s стерео-вход, width 1: корреляция L/R хвоста %.2f, want < 0.5", id, c)
		}
	}
}
