package dsp

import (
	"math"
	"testing"
)

// Тесты карточки internal-dsp-space, разделы 7–9: pitch, octaver (rubberband),
// reverse, stutter, tape-stop, vinyl. Хелперы nc* — из newchains_test.go.

const ncTone440 = "0.5*sin(2*PI*440*t)"

// ncChirp — свип частоты 200→~1100 Гц за 6 с: каждый кусок отличается от соседних,
// реверс и повтор узнаются по корреляции.
const ncChirp = "0.5*sin(2*PI*(200*t+75*t*t))"

// --- 7. Высота ---

// Карточка 7.1: тон 440 → 440·2^(semis/12) ±1 %; длина та же ±0.05 с.
func TestPitchShift(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncTone440, 3, chSR)
	for _, semis := range []float64{1, 3, -5, 12, -12} {
		out := decode(t, ncRun(t, "pitch", in, map[string]float64{"semis": semis}))
		if d := secs(out); math.Abs(d-3) > 0.05 {
			t.Errorf("semis %v: длина %.3f с, want 3 ± 0.05", semis, d)
		}
		want := 440 * math.Pow(2, semis/12)
		if f := ncZCFreq(out, chSR, 0.5, 2.5); math.Abs(f-want) > 0.01*want {
			t.Errorf("semis %v: частота %.1f Гц, want %.1f ± 1%%", semis, f, want)
		}
	}
}

// Карточка 7.1: semis 0 → вход.
func TestPitchZeroIsDry(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncTone440, 3, chSR)
	src := decode(t, in)
	out := decode(t, ncRun(t, "pitch", in, map[string]float64{"semis": 0}))
	if d := ncDiffDb(out, src, chSR, 0.05, 2.95); d > -30 {
		t.Errorf("semis 0: выход отличается от входа на %.1f дБ, want ≈ вход (≤ −30)", d)
	}
}

// Карточка 7.2: при down 0.5 есть компонента 220 Гц не тише −12 дБ к 440; up 0 → нет
// 880 сверх входного (во входе 880 нет — порог −30 дБ к 440).
func TestOctaverDown(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncTone440, 3, chSR)
	out := decode(t, ncRun(t, "octaver", in, map[string]float64{"down": 0.5, "up": 0}))
	a440, a220, a880 := toneAmpAt(out, chSR, 440, 0.5, 2.5), toneAmpAt(out, chSR, 220, 0.5, 2.5), toneAmpAt(out, chSR, 880, 0.5, 2.5)
	if d := dbfs(a220) - dbfs(a440); d < -12 {
		t.Errorf("down 0.5: 220 Гц %.1f дБ к 440, want ≥ −12", d)
	}
	if d := dbfs(a880) - dbfs(a440); d > -30 {
		t.Errorf("up 0: 880 Гц %.1f дБ к 440, want нет (≤ −30)", d)
	}
}

// Карточка 7.2: up — октава вверх (обратная сторона «up 0 → нет 880»).
func TestOctaverUp(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncTone440, 3, chSR)
	out := decode(t, ncRun(t, "octaver", in, map[string]float64{"down": 0, "up": 1}))
	if d := dbfs(toneAmpAt(out, chSR, 880, 0.5, 2.5)) - dbfs(toneAmpAt(out, chSR, 440, 0.5, 2.5)); d < -12 {
		t.Errorf("up 1: 880 Гц %.1f дБ к 440, want слышна (≥ −12)", d)
	}
}

// Карточка 7.2: down 0 и up 0 → вход.
func TestOctaverZeroIsDry(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncTone440, 3, chSR)
	src := decode(t, in)
	out := decode(t, ncRun(t, "octaver", in, map[string]float64{"down": 0, "up": 0}))
	if d := ncDiffDb(out, src, chSR, 0.05, 2.95); d > -30 {
		t.Errorf("оба 0: выход отличается от входа на %.1f дБ, want ≈ вход (≤ −30)", d)
	}
}

// --- 8. Время/глитч ---

func ncReversed(s []float32) []float32 {
	r := make([]float32, len(s))
	for i, v := range s {
		r[len(s)-1-i] = v
	}
	return r
}

// Карточка 8.1: в окне [start−len, start] звучит реверсированный кусок источника
// (replace 1: корреляция с реверсом > 0.9); вне окна выход = вход (кроме краёв ≤ 20 мс).
// src 0 — реверс куска до отметки, src 1 — куска после отметки.
func TestReverseReplace(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncChirp, 6, chSR)
	src := decode(t, in)
	const start, ln = 3.0, 1.0
	for _, tc := range []struct {
		src      float64
		from, to float64 // откуда берётся кусок источника
	}{{0, start - ln, start}, {1, start, start + ln}} {
		out := decode(t, ncRun(t, "reverse", in, map[string]float64{
			"start": start, "len": ln, "src": tc.src, "level": 1, "replace": 1,
		}))
		if d := secs(out); math.Abs(d-6) > 0.05 {
			t.Errorf("src %v: длина %.3f с, want 6 ± 0.05", tc.src, d)
		}
		rev := ncReversed(ncSeg(src, chSR, tc.from, tc.to))
		win := ncSeg(out, chSR, start-ln, start)
		e := int(0.02 * chSR)
		if c := ncCorr(win[e:len(win)-e], rev[e:len(rev)-e]); c <= 0.9 {
			t.Errorf("src %v: корреляция окна с реверсом источника %.3f, want > 0.9", tc.src, c)
		}
		for _, w := range [][2]float64{{0, start - ln - 0.02}, {start + 0.02, 6}} {
			if d := ncDiffDb(out, src, chSR, w[0], w[1]); d > -40 {
				t.Errorf("src %v: вне окна %.2f–%.2f с выход отличается от входа на %.1f дБ, want ≤ −40",
					tc.src, w[0], w[1], d)
			}
		}
	}
}

// Карточка 8.1: replace 0 — реверс поверх: в окне выход − вход = реверс источника.
func TestReverseOverlay(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncChirp, 6, chSR)
	src := decode(t, in)
	out := decode(t, ncRun(t, "reverse", in, map[string]float64{
		"start": 3, "len": 1, "src": 1, "level": 0.8, "replace": 0,
	}))
	n := min(len(out), len(src))
	added := make([]float32, n)
	for i := range added {
		added[i] = out[i] - src[i]
	}
	rev := ncReversed(ncSeg(src, chSR, 3, 4))
	win := ncSeg(added, chSR, 2, 3)
	e := int(0.02 * chSR)
	if c := ncCorr(win[e:len(win)-e], rev[e:len(rev)-e]); c <= 0.9 {
		t.Errorf("поверх: добавка в окне коррелирует с реверсом на %.3f, want > 0.9", c)
	}
	if d := ncDiffDb(out, src, chSR, 0, 1.98); d > -40 {
		t.Errorf("поверх: до окна выход отличается от входа на %.1f дБ, want ≤ −40", d)
	}
}

// Карточка 8.2: период p = 60/(bpm·div); окна k = 1…count−1 совпадают с окном k = 0
// (корреляция > 0.95, края 5 мс исключены); после start + count·p — вход.
func TestStutterRepeats(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncChirp, 5, chSR)
	src := decode(t, in)
	for _, tc := range []struct{ bpm, div, count float64 }{{120, 2, 4}, {100, 4, 6}} {
		const start = 1.0
		p := 60 / (tc.bpm * tc.div)
		out := decode(t, ncRun(t, "stutter", in, map[string]float64{
			"start": start, "bpm": tc.bpm, "div": tc.div, "count": tc.count,
		}))
		e := 0.005
		ref := ncSeg(out, chSR, start+e, start+p-e)
		for k := 1.0; k < tc.count; k++ {
			w := ncSeg(out, chSR, start+k*p+e, start+(k+1)*p-e)
			if c := ncCorr(w, ref); c <= 0.95 {
				t.Errorf("bpm %v div %v: окно %v коррелирует с первым на %.3f, want > 0.95", tc.bpm, tc.div, k, c)
			}
		}
		end := start + tc.count*p
		if d := ncDiffDb(out, src, chSR, end+0.02, 5); d > -40 {
			t.Errorf("bpm %v div %v: после %.2f с выход отличается от входа на %.1f дБ, want ≤ −40", tc.bpm, tc.div, end, d)
		}
		if d := ncDiffDb(out, src, chSR, 0, start-0.02); d > -40 {
			t.Errorf("bpm %v div %v: до start выход отличается от входа на %.1f дБ, want ≤ −40", tc.bpm, tc.div, d)
		}
	}
}

// Карточка 8.3: в окне [start, start+dur] частота тона падает (последняя четверть
// окна ≤ половины исходной), к концу окна уровень < −30 дБ; после окна — вход.
func TestTapeStop(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncTone440, 5, chSR)
	src := decode(t, in)
	const start, dur = 1.0, 2.0
	out := decode(t, ncRun(t, "tape-stop", in, map[string]float64{"start": start, "dur": dur}))
	if d := secs(out); math.Abs(d-5) > 0.05 {
		t.Errorf("длина %.3f с, want 5 ± 0.05", d)
	}
	if f := ncZCFreq(out, chSR, start+0.75*dur, start+dur); f > 220 {
		t.Errorf("последняя четверть окна: частота %.0f Гц, want ≤ 220 (половина 440)", f)
	}
	if l := dbfs(segRMS(out, start+dur-0.1, start+dur) / segRMS(src, start+dur-0.1, start+dur)); l >= -30 {
		t.Errorf("к концу окна %.1f дБ к входу, want < −30", l)
	}
	if d := ncDiffDb(out, src, chSR, start+dur+0.05, 5); d > -40 {
		t.Errorf("после окна выход отличается от входа на %.1f дБ, want ≤ −40", d)
	}
	if d := ncDiffDb(out, src, chSR, 0, start-0.02); d > -40 {
		t.Errorf("до окна выход отличается от входа на %.1f дБ, want ≤ −40", d)
	}
}

// --- 9. Винил ---

// ncClicksPerSec — число щелчков (групп отсчётов > 0.05, разделённых ≥ 2 мс) в секунду.
func ncClicksPerSec(s []float32, rate int, from, to float64) float64 {
	seg := ncSeg(s, rate, from, to)
	gap := int(0.002 * float64(rate))
	n, last := 0, -gap-1
	for i, v := range seg {
		if math.Abs(float64(v)) > 0.05 {
			if i-last > gap {
				n++
			}
			last = i
		}
	}
	return float64(n) / (to - from)
}

// Карточка 9.1: на тишине crackle 0.4 → щелчки (> 0.05) от 2 до 200 в секунду.
func TestVinylCrackle(t *testing.T) {
	needFFmpeg(t)
	in := ncFile(t, "anullsrc=r=44100:cl=mono:d=4")
	out := decodeAt(t, ncRun(t, "vinyl", in, map[string]float64{"crackle": 0.4}), ncSR)
	if n := ncClicksPerSec(out, ncSR, 0.5, 3.5); n < 2 || n > 200 {
		t.Errorf("crackle 0.4: %.1f щелчков в секунду, want 2…200", n)
	}
}

// Карточка 9.1: crackle 0 и hiss 0 → тишина (< −80 дБ).
func TestVinylZeroIsSilent(t *testing.T) {
	needFFmpeg(t)
	in := ncFile(t, "anullsrc=r=44100:cl=mono:d=3")
	out := decodeAt(t, ncRun(t, "vinyl", in, map[string]float64{"crackle": 0, "hiss": 0}), ncSR)
	if l := dbfs(ncRMS(out, ncSR, 0, 3)); l >= -80 {
		t.Errorf("crackle 0, hiss 0: %.1f дБFS, want < −80 (тишина)", l)
	}
}

// Карточка 9.1: на тоне тон сохраняется (уровень ±1 дБ) при параметрах по умолчанию.
func TestVinylKeepsTone(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncTone440, 3, ncSR)
	src := decodeAt(t, in, ncSR)
	out := decodeAt(t, ncRun(t, "vinyl", in, nil), ncSR)
	if d := dbfs(toneAmpAt(out, ncSR, 440, 0.5, 2.5)) - dbfs(toneAmpAt(src, ncSR, 440, 0.5, 2.5)); math.Abs(d) > 1 {
		t.Errorf("тон 440 изменился на %+.2f дБ, want ±1", d)
	}
}
