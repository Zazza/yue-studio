package dsp

import (
	"encoding/binary"
	"math"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Тесты графа вклеек партий в трек (filter_complex для ffmpeg).

func mustMatch(t *testing.T, g, pattern, what string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(g) {
		t.Errorf("%s: /%s/ not found in graph: %s", what, pattern, g)
	}
}

func TestInsertsGraphSingle(t *testing.T) {
	g := InsertsGraph([]Insert{{AtSec: 2.0, SkipSec: 0.5, Tempo: 1.05, Gain: 0.7}})
	mustMatch(t, g, `atrim=start=0\.50*([^0-9]|$)`, "обрезка начала партии")
	mustMatch(t, g, `atempo=1\.050*([^0-9]|$)`, "растяжение темпа")
	mustMatch(t, g, `adelay=(delays=)?2000([^0-9]|$)`, "задержка в мс")
	mustMatch(t, g, `volume=0\.70*([^0-9]|$)`, "гейн")
	for _, want := range []string{"amix=inputs=2", "normalize=0", "duration=first", "[out]"} {
		if !strings.Contains(g, want) {
			t.Errorf("%q missing in graph: %s", want, g)
		}
	}
}

func TestInsertsGraphNoTempo(t *testing.T) {
	// Tempo 0 и 1 — без растяжения, фильтра atempo нет
	for _, tempo := range []float64{0, 1} {
		g := InsertsGraph([]Insert{{AtSec: 1, Tempo: tempo, Gain: 1}})
		if strings.Contains(g, "atempo") {
			t.Errorf("Tempo=%v: unexpected atempo in graph: %s", tempo, g)
		}
	}
}

func TestInsertsGraphTempoClamp(t *testing.T) {
	// вне [0.5, 2] — зажимается
	g := InsertsGraph([]Insert{{AtSec: 1, Tempo: 5, Gain: 1}})
	mustMatch(t, g, `atempo=2(\.0*)?([^0-9.]|$)`, "Tempo 5 → 2")
	g = InsertsGraph([]Insert{{AtSec: 1, Tempo: 0.1, Gain: 1}})
	mustMatch(t, g, `atempo=0\.50*([^0-9]|$)`, "Tempo 0.1 → 0.5")
}

func TestInsertsGraphThree(t *testing.T) {
	g := InsertsGraph([]Insert{
		{AtSec: 1, Gain: 1},
		{AtSec: 2, Gain: 0.5},
		{AtSec: 3, Gain: 0.25},
	})
	if !strings.Contains(g, "amix=inputs=4") {
		t.Errorf("3 inserts → amix=inputs=4 expected: %s", g)
	}
}

// --- интеграция с ffmpeg ---

func needFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

// lavfi — сгенерировать файл из источника lavfi.
func lavfi(t *testing.T, src, path string) {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", src, path).CombinedOutput()
	if err != nil {
		t.Fatalf("lavfi %q: %v %s", src, err, out)
	}
}

// decode — декодировать файл в моно float32 16 кГц.
func decode(t *testing.T, path string) []float32 {
	t.Helper()
	raw, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", path, "-f", "f32le", "-ac", "1", "-ar", "16000", "-").Output()
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	s := make([]float32, len(raw)/4)
	for i := range s {
		s[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return s
}

// firstPeak — момент (с) первого отсчёта по модулю > thr; -1 — нет такого.
func firstPeak(s []float32, thr float32) float64 {
	for i, v := range s {
		if v > thr || v < -thr {
			return float64(i) / 16000
		}
	}
	return -1
}

// clickParty — партия 3 с тишины с коротким щелчком (5 мс, 0.8) на 1.0 с.
const clickParty = "aevalsrc=exprs='if(between(t\\,1.0\\,1.005)\\,0.8\\,0)':d=3:s=16000"

// silentBase — база 6 с тишины.
const silentBase = "anullsrc=r=16000:cl=mono:d=6"

func TestRunInputsEmptyInsertsCopiesBase(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.wav")
	out := filepath.Join(dir, "out.wav")
	lavfi(t, "sine=frequency=440:duration=3:sample_rate=16000", base)
	if err := RunInputs([]string{base}, out, InsertsGraph(nil)); err != nil {
		t.Fatalf("run: %v", err)
	}
	s := decode(t, out)
	if d := float64(len(s)) / 16000; math.Abs(d-3) > 0.05 {
		t.Errorf("output duration %.3f, want 3 ± 0.05", d)
	}
	// lavfi sine по умолчанию амплитуды 1/8 → RMS ≈ 0.088; тишина дала бы 0
	if RMS(s) < 0.05 {
		t.Errorf("output lost the base signal: RMS=%.3f", RMS(s))
	}
}

func TestRunInputsInsertPlacement(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.wav")
	party := filepath.Join(dir, "party.wav")
	out := filepath.Join(dir, "out.wav")
	lavfi(t, silentBase, base)
	lavfi(t, clickParty, party)
	// партия с 0.5 с звучит с 2.0 с трека → щелчок партии (1.0) попадает на 2.5
	g := InsertsGraph([]Insert{{AtSec: 2.0, SkipSec: 0.5, Gain: 1}})
	if err := RunInputs([]string{base, party}, out, g); err != nil {
		t.Fatalf("run: %v", err)
	}
	s := decode(t, out)
	if d := float64(len(s)) / 16000; math.Abs(d-6) > 0.05 {
		t.Errorf("output duration %.3f, want base length 6 ± 0.05", d)
	}
	if at := firstPeak(s, 0.1); math.Abs(at-2.5) > 0.01 {
		t.Errorf("click at %.4f s, want 2.5 ± 0.01", at)
	}
}

func TestRunInputsInsertDuration(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.wav")
	party := filepath.Join(dir, "party.wav")
	out := filepath.Join(dir, "out.wav")
	lavfi(t, silentBase, base)
	lavfi(t, clickParty, party)
	// звучит только 0.3 с партии (0.5..0.8) — щелчок на 1.0 вне окна
	g := InsertsGraph([]Insert{{AtSec: 2.0, SkipSec: 0.5, DurSec: 0.3, Gain: 1}})
	if err := RunInputs([]string{base, party}, out, g); err != nil {
		t.Fatalf("run: %v", err)
	}
	s := decode(t, out)
	if d := float64(len(s)) / 16000; math.Abs(d-6) > 0.05 {
		t.Errorf("output duration %.3f, want base length 6 ± 0.05", d)
	}
	if at := firstPeak(s, 0.1); at >= 0 {
		t.Errorf("click leaked into output at %.4f s, DurSec must cut it", at)
	}
}

// --- плавный вход/выход и инверсия (карточка «+ инструмент/приём» по стемам) ---

func TestInsertsGraphFadeIn(t *testing.T) {
	g := InsertsGraph([]Insert{{AtSec: 1, DurSec: 2, Gain: 1, FadeIn: 0.5}})
	mustMatch(t, g, `afade=t=in`, "плавный вход")
	if strings.Contains(g, "afade=t=out") {
		t.Errorf("FadeOut=0, а в графе afade=t=out: %s", g)
	}
}

func TestInsertsGraphFadeOut(t *testing.T) {
	g := InsertsGraph([]Insert{{AtSec: 1, DurSec: 2, Gain: 1, FadeOut: 0.5}})
	mustMatch(t, g, `afade=t=out`, "плавный выход")
	if strings.Contains(g, "afade=t=in") {
		t.Errorf("FadeIn=0, а в графе afade=t=in: %s", g)
	}
}

func TestInsertsGraphNoFadeByDefault(t *testing.T) {
	g := InsertsGraph([]Insert{{AtSec: 1, DurSec: 2, Gain: 1}})
	if strings.Contains(g, "afade") {
		t.Errorf("FadeIn=FadeOut=0 — afade быть не должно: %s", g)
	}
}

// segRMS — RMS отсчётов 16 кГц на [from,to] с.
func segRMS(s []float32, from, to float64) float64 {
	a, b := int(from*16000), int(to*16000)
	if b > len(s) {
		b = len(s)
	}
	if a >= b {
		return 0
	}
	return RMS(s[a:b])
}

// Отрицательный Gain — инверсия: та же база, вклеенная с Gain −1, гасит её в окне.
func TestRunInputsNegativeGainSubtracts(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.wav")
	out := filepath.Join(dir, "out.wav")
	lavfi(t, "aevalsrc=exprs='0.5*sin(2*PI*440*t)':d=6:s=16000", base)
	// та же база: SkipSec = AtSec — отсчёты совпадают, окно [2,4] гасится
	g := InsertsGraph([]Insert{{AtSec: 2, SkipSec: 2, DurSec: 2, Gain: -1}})
	if err := RunInputs([]string{base, base}, out, g); err != nil {
		t.Fatalf("run: %v", err)
	}
	s := decode(t, out)
	if d := float64(len(s)) / 16000; math.Abs(d-6) > 0.05 {
		t.Errorf("output duration %.3f, want 6 ± 0.05", d)
	}
	if r := segRMS(s, 2.2, 3.8); r > 0.01 {
		t.Errorf("в окне вычитания RMS=%.4f, want < 0.01 (тишина)", r)
	}
	// синус 0.5 → RMS 0.354
	for _, w := range [][2]float64{{0.5, 1.5}, {4.5, 5.5}} {
		if r := segRMS(s, w[0], w[1]); math.Abs(r-0.354) > 0.03 {
			t.Errorf("вне окна [%.1f,%.1f] RMS=%.4f, want ≈0.354 (база на месте)", w[0], w[1], r)
		}
	}
}

// --- фильтр нижних частот у вклейки (сбивка: у старых барабанов вычитается только низ) ---

func TestInsertsGraphLowpass(t *testing.T) {
	g := InsertsGraph([]Insert{{AtSec: 1, DurSec: 2, Gain: -1, LowpassHz: 6000}})
	// низ без сдвига фазы: FFT-маска «частота ≤ 6000» (обычный lowpass недовычитал)
	mustMatch(t, g, `afftfilt=real='re\*lte\(b\*sr/4096\\,6000\)'`, "маска нижних частот со срезом 6000")
}

func TestInsertsGraphNoLowpassByDefault(t *testing.T) {
	g := InsertsGraph([]Insert{{AtSec: 1, DurSec: 2, Gain: -1}})
	if strings.Contains(g, "lowpass") || strings.Contains(g, "afftfilt") {
		t.Errorf("LowpassHz=0 — lowpass быть не должно: %s", g)
	}
}

// hiSR — частота для проверок с 9000 Гц: при 16 кГц он выше Найквиста (8 кГц).
const hiSR = 44100

// decodeAt — декодировать файл в моно float32 с частотой rate.
func decodeAt(t *testing.T, path string, rate int) []float32 {
	t.Helper()
	raw, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", path, "-f", "f32le", "-ac", "1", "-ar", strconv.Itoa(rate), "-").Output()
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	s := make([]float32, len(raw)/4)
	for i := range s {
		s[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return s
}

// toneAmpAt — амплитуда синуса hz на [from,to] с (одна точка ДПФ) для частоты rate.
func toneAmpAt(s []float32, rate int, hz, from, to float64) float64 {
	a, b := int(from*float64(rate)), int(to*float64(rate))
	if b > len(s) {
		b = len(s)
	}
	if a >= b {
		return 0
	}
	w := 2 * math.Pi * hz / float64(rate)
	var re, im float64
	for i, v := range s[a:b] {
		re += float64(v) * math.Cos(w*float64(i))
		im -= float64(v) * math.Sin(w*float64(i))
	}
	return 2 * math.Hypot(re, im) / float64(b-a)
}

// Вклейка той же базы с Gain −1 и LowpassHz 6000 гасит в окне только низ (200 Гц),
// верх (9000 Гц) остаётся; вне окна оба на месте.
func TestRunInputsLowpassSubtractsOnlyLows(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.wav")
	out := filepath.Join(dir, "out.wav")
	lavfi(t, "aevalsrc=exprs='0.3*sin(2*PI*200*t)+0.3*sin(2*PI*9000*t)':d=6:s=44100", base)
	g := InsertsGraph([]Insert{{AtSec: 2, SkipSec: 2, DurSec: 2, Gain: -1, LowpassHz: 6000}})
	if err := RunInputs([]string{base, base}, out, g); err != nil {
		t.Fatalf("run: %v", err)
	}
	s := decodeAt(t, out, hiSR)
	if d := float64(len(s)) / hiSR; math.Abs(d-6) > 0.05 {
		t.Errorf("output duration %.3f, want 6 ± 0.05", d)
	}
	if a := toneAmpAt(s, hiSR, 200, 2.2, 3.8); a > 0.05 {
		t.Errorf("200 Гц в окне %.4f, want < 0.05 (низ вычтен)", a)
	}
	// Допуск по верху шире ±0.06 из постановки: фазовый сдвиг 2-полюсного lowpass
	// у 9000 Гц даёт после вычитания ≈0.37 (проверено ffmpeg на 44.1 кГц).
	// Контракт — «верх остаётся», а не «ровно 0.3»; полное гашение дало бы < 0.06.
	if a := toneAmpAt(s, hiSR, 9000, 2.2, 3.8); a < 0.24 || a > 0.42 {
		t.Errorf("9000 Гц в окне %.4f, want 0.24..0.42 (верх остаётся)", a)
	}
	for _, w := range [][2]float64{{0.5, 1.5}, {4.5, 5.5}} {
		for _, hz := range []float64{200, 9000} {
			if a := toneAmpAt(s, hiSR, hz, w[0], w[1]); math.Abs(a-0.3) > 0.03 {
				t.Errorf("вне окна [%.1f,%.1f] %.0f Гц %.4f, want ≈0.3", w[0], w[1], hz, a)
			}
		}
	}
}

// Вычитание куска на некруглой секунде гасит звук до нуля: начало куска (atrim)
// и задержка (adelay) совпадают до сэмпла (было: %.3f и целые мс — остаток 0.45 при 0.3).
func TestInsertsSubtractNonRoundSecond(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "s.wav")
	if out, err := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-f", "lavfi", "-i",
		"aevalsrc=exprs='0.3*sin(2*PI*500*t)':d=6:s=44100", src).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	out := filepath.Join(dir, "o.wav")
	g := InsertsGraph([]Insert{{AtSec: 3.4567, SkipSec: 3.4567, Gain: -1}})
	if err := RunInputs([]string{src, src}, out, g); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("ffmpeg", "-loglevel", "error", "-i", out, "-ss", "4", "-t", "1", "-f", "f32le", "-ac", "1", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	n := len(raw) / 4
	for i := 0; i < n; i++ {
		v := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:])))
		sum += v * v
	}
	if rms := math.Sqrt(sum / float64(n)); rms > 0.005 {
		t.Errorf("остаток после вычитания RMS %.4f, want ≈ 0", rms)
	}
}

// --- вставка на месте: SkipSec == AtSec, без растяжения — дорожка ложится на своё же
// место без сдвига при любой частоте и любом времени (было: +3 дБ остатка при 44,1 кГц
// на 20.007 с — atrim/adelay расходились на сэмпл) ---

// ipNoise — белый шум (float32 wav) длительностью dur с частотой sr и каналами ch.
func ipNoise(t *testing.T, path string, sr, ch int, dur float64) {
	t.Helper()
	src := "anoisesrc=d=" + strconv.FormatFloat(dur, 'f', 3, 64) +
		":r=" + strconv.Itoa(sr) + ":a=0.3:c=white:seed=7"
	out, err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", src, "-ac", strconv.Itoa(ch), "-c:a", "pcm_f32le", path).CombinedOutput()
	if err != nil {
		t.Fatalf("noise %q: %v %s", src, err, out)
	}
}

// ipRelDB — энергия (out − want) относительно энергии want на отсчётах [a,b), дБ.
func ipRelDB(out, want []float64, a, b int) float64 {
	if a < 0 {
		a = 0
	}
	if b > len(out) {
		b = len(out)
	}
	if b > len(want) {
		b = len(want)
	}
	var e, ref float64
	for i := a; i < b; i++ {
		d := out[i] - want[i]
		e += d * d
		ref += want[i] * want[i]
	}
	if ref == 0 {
		return math.Inf(1)
	}
	if e == 0 {
		return math.Inf(-1)
	}
	return 10 * math.Log10(e/ref)
}

// ipRelToBase — как ipRelDB, но остаток (out − want) меряется относительно базы
// (когда ожидается тишина, want = 0 и сравнивать не с чем).
func ipRelToBase(out, want, base []float64, a, b int) float64 {
	if a < 0 {
		a = 0
	}
	n := len(out)
	if len(base) < n {
		n = len(base)
	}
	if b > n {
		b = n
	}
	var e, ref float64
	for i := a; i < b; i++ {
		d := out[i] - want[i]
		e += d * d
		ref += base[i] * base[i]
	}
	if ref == 0 {
		return math.Inf(1)
	}
	if e == 0 {
		return math.Inf(-1)
	}
	return 10 * math.Log10(e/ref)
}

func ipF64(s []float32) []float64 {
	r := make([]float64, len(s))
	for i, v := range s {
		r[i] = float64(v)
	}
	return r
}

// ipRun — база-шум (sr, длина dur), вставка той же базы по in; база и результат
// в отсчётах своей частоты (без пересэмплирования).
func ipRun(t *testing.T, sr int, dur float64, in Insert) (base, out []float64) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	bp := filepath.Join(dir, "base.wav")
	op := filepath.Join(dir, "out.wav")
	ipNoise(t, bp, sr, 1, dur)
	if err := RunInputs([]string{bp, bp}, op, InsertsGraph([]Insert{in})); err != nil {
		t.Fatalf("run: %v", err)
	}
	base, out = ipF64(decodeAt(t, bp, sr)), ipF64(decodeAt(t, op, sr))
	if d := len(out) - len(base); d < -1 || d > 1 {
		t.Fatalf("длина результата %d отсчётов, база %d — должна совпасть", len(out), len(base))
	}
	return base, out
}

var ipTimes = []float64{20.007, 1.995, 123.407, 0.5}

// 1. Вычитание на месте гасит базу в окне до ≤ −80 дБ, вне окна база нетронута.
func TestInsertInPlaceSubtractsAnyRateAnyTime(t *testing.T) {
	for _, sr := range []int{44100, 48000} {
		for _, at := range ipTimes {
			at, sr := at, sr
			t.Run(strconv.Itoa(sr)+"/"+strconv.FormatFloat(at, 'f', -1, 64), func(t *testing.T) {
				const dur, fade = 10.0, 0.05
				in := Insert{AtSec: at, SkipSec: at, DurSec: dur, Gain: -1, FadeIn: fade, FadeOut: fade}
				base, out := ipRun(t, sr, math.Max(30, at+dur+5), in)
				f := float64(sr)
				n0, n1 := int(math.Round(at*f)), int(math.Round((at+dur)*f))
				zero := make([]float64, len(out))
				// середина окна: после фейда входа, до фейда выхода
				mid := ipRelToBase(out, zero, base, n0+int(math.Ceil(fade*f))+1, n1-int(math.Ceil(fade*f))-1)
				before := ipRelDB(out, base, 0, n0)
				after := ipRelDB(out, base, n1, len(base))
				t.Logf("sr=%d at=%.3f: середина %.1f дБ, до окна %.1f дБ, после окна %.1f дБ", sr, at, mid, before, after)
				if mid > -80 {
					t.Errorf("остаток в середине окна %.1f дБ относительно базы, want ≤ −80", mid)
				}
				if before > -80 {
					t.Errorf("до AtSec база изменилась: %.1f дБ, want ≤ −80", before)
				}
				if after > -80 {
					t.Errorf("после AtSec+DurSec база изменилась: %.1f дБ, want ≤ −80", after)
				}
			})
		}
	}
}

// 2. Края окна: на фейдах результат = база·(1 − w), w — линейная рампа от сэмпла
// round(AtSec·sr) длиной FadeIn·sr (и спад длиной FadeOut·sr к концу окна).
func TestInsertInPlaceFadeEdges(t *testing.T) {
	for _, sr := range []int{44100, 48000} {
		for _, at := range ipTimes {
			at, sr := at, sr
			t.Run(strconv.Itoa(sr)+"/"+strconv.FormatFloat(at, 'f', -1, 64), func(t *testing.T) {
				const dur, fade = 10.0, 0.05
				in := Insert{AtSec: at, SkipSec: at, DurSec: dur, Gain: -1, FadeIn: fade, FadeOut: fade}
				base, out := ipRun(t, sr, math.Max(30, at+dur+5), in)
				f := float64(sr)
				n0, n1 := int(math.Round(at*f)), int(math.Round((at+dur)*f))
				L := fade * f
				want := make([]float64, len(base))
				for i := range base {
					var w float64
					switch {
					case i < n0 || i >= n1:
						w = 0
					case float64(i-n0) < L:
						w = float64(i-n0) / L
					case float64(n1-i) < L:
						w = float64(n1-i) / L
					default:
						w = 1
					}
					want[i] = base[i] * (1 - w)
				}
				m := int(L) + 50
				in1 := ipRelToBase(out, want, base, n0-50, n0+m)
				out1 := ipRelToBase(out, want, base, n1-m, n1+50)
				t.Logf("sr=%d at=%.3f: вход %.1f дБ, выход %.1f дБ", sr, at, in1, out1)
				if in1 > -60 {
					t.Errorf("фейд входа расходится с рампой от round(AtSec·sr): %.1f дБ, want ≤ −60", in1)
				}
				if out1 > -60 {
					t.Errorf("фейд выхода расходится с рампой к концу окна: %.1f дБ, want ≤ −60", out1)
				}
			})
		}
	}
}

// 3. DurSec 0 и без фейдов: от AtSec до конца вычтено, до AtSec нетронуто;
// край резкий (переход ≤ 10 сэмплов).
func TestInsertInPlaceToEndNoFade(t *testing.T) {
	for _, sr := range []int{44100, 48000} {
		for _, at := range ipTimes {
			at, sr := at, sr
			t.Run(strconv.Itoa(sr)+"/"+strconv.FormatFloat(at, 'f', -1, 64), func(t *testing.T) {
				in := Insert{AtSec: at, SkipSec: at, Gain: -1}
				base, out := ipRun(t, sr, math.Max(30, at+15), in)
				n0 := int(math.Round(at * float64(sr)))
				zero := make([]float64, len(out))
				before := ipRelDB(out, base, 0, n0-10)
				after := ipRelToBase(out, zero, base, n0+10, len(base))
				t.Logf("sr=%d at=%.3f: до %.1f дБ, после %.1f дБ", sr, at, before, after)
				if before > -80 {
					t.Errorf("до AtSec база изменилась: %.1f дБ, want ≤ −80", before)
				}
				if after > -80 {
					t.Errorf("от AtSec до конца остаток %.1f дБ относительно базы, want ≤ −80", after)
				}
			})
		}
	}
}

// 4. LowpassHz у вставки на месте с некруглым AtSec при 44,1 кГц: вычитается только
// низ (200 Гц), верх (9000 Гц) остаётся; вне окна оба на месте.
func TestInsertInPlaceLowpassNonRound(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.wav")
	out := filepath.Join(dir, "out.wav")
	lavfi(t, "aevalsrc=exprs='0.3*sin(2*PI*200*t)+0.3*sin(2*PI*9000*t)':d=30:s=44100", base)
	const at = 20.007
	g := InsertsGraph([]Insert{{AtSec: at, SkipSec: at, DurSec: 4, Gain: -1, LowpassHz: 6000}})
	if err := RunInputs([]string{base, base}, out, g); err != nil {
		t.Fatalf("run: %v", err)
	}
	s := decodeAt(t, out, hiSR)
	if d := float64(len(s)) / hiSR; math.Abs(d-30) > 0.05 {
		t.Errorf("output duration %.3f, want 30 ± 0.05", d)
	}
	lo := toneAmpAt(s, hiSR, 200, at+0.2, at+3.8)
	hi := toneAmpAt(s, hiSR, 9000, at+0.2, at+3.8)
	t.Logf("в окне: 200 Гц %.5f, 9000 Гц %.4f", lo, hi)
	// сдвиг вклейки на 1 сэмпл оставил бы 0.3·2·sin(π·200/44100) ≈ 0.0085 — порог ниже
	if lo > 0.003 {
		t.Errorf("200 Гц в окне %.5f, want < 0.003 (низ вычтен без сдвига)", lo)
	}
	// верх остаётся (полное гашение дало бы ≈0); границы — как у TestRunInputsLowpassSubtractsOnlyLows
	if hi < 0.24 || hi > 0.42 {
		t.Errorf("9000 Гц в окне %.4f, want 0.24..0.42 (верх остаётся)", hi)
	}
	for _, w := range [][2]float64{{at - 3, at - 0.5}, {at + 4.5, at + 7}} {
		for _, hz := range []float64{200, 9000} {
			if a := toneAmpAt(s, hiSR, hz, w[0], w[1]); math.Abs(a-0.3) > 0.03 {
				t.Errorf("вне окна [%.1f,%.1f] %.0f Гц %.4f, want ≈0.3", w[0], w[1], hz, a)
			}
		}
	}
}

// 5. Скорость: 6 вставок на месте на 240 с стерео 44,1 кГц — не дольше 5 с
// (посэмпловые выражения aeval были в 35 раз медленнее).
func TestInsertInPlaceSpeed(t *testing.T) {
	needFFmpeg(t)
	if testing.Short() {
		t.Skip("short")
	}
	dir := t.TempDir()
	bp := filepath.Join(dir, "base.wav")
	op := filepath.Join(dir, "out.wav")
	ipNoise(t, bp, 44100, 2, 240)
	ins := make([]Insert, 6)
	inputs := []string{bp}
	for k := range ins {
		at := 10.007 + 37.3*float64(k)
		ins[k] = Insert{AtSec: at, SkipSec: at, DurSec: 20, Gain: -1, FadeIn: 0.05, FadeOut: 0.05}
		inputs = append(inputs, bp)
	}
	start := time.Now()
	if err := RunInputs(inputs, op, InsertsGraph(ins)); err != nil {
		t.Fatalf("run: %v", err)
	}
	el := time.Since(start)
	t.Logf("6 вставок на месте, 240 с стерео 44,1 кГц: %.2f с", el.Seconds())
	if el > 5*time.Second {
		t.Errorf("RunInputs %.2f с, want ≤ 5 с", el.Seconds())
	}
}

// --- регрессии ревью: маска частот у вставки на месте и короткие окна ---

// ipNoiseStereo — стерео белый шум с независимыми каналами (разные seed), float32 wav.
func ipNoiseStereo(t *testing.T, path string, sr int, dur float64) {
	t.Helper()
	d := strconv.FormatFloat(dur, 'f', 3, 64)
	r := strconv.Itoa(sr)
	fc := "anoisesrc=d=" + d + ":r=" + r + ":a=0.3:c=white:seed=7[l];" +
		"anoisesrc=d=" + d + ":r=" + r + ":a=0.3:c=white:seed=11[r];[l][r]amerge=inputs=2[o]"
	out, err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-filter_complex", fc, "-map", "[o]", "-c:a", "pcm_f32le", path).CombinedOutput()
	if err != nil {
		t.Fatalf("stereo noise: %v %s", err, out)
	}
}

// ipDecodeStereo — декодировать файл в два канала float64 с частотой rate.
func ipDecodeStereo(t *testing.T, path string, rate int) [2][]float64 {
	t.Helper()
	raw, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", path, "-f", "f32le", "-ac", "2", "-ar", strconv.Itoa(rate), "-").Output()
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	n := len(raw) / 8
	var ch [2][]float64
	ch[0], ch[1] = make([]float64, n), make([]float64, n)
	for i := 0; i < n; i++ {
		ch[0][i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[i*8:])))
		ch[1][i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[i*8+4:])))
	}
	return ch
}

// ipFIR — КИХ-фильтр (окно Блэкмана, taps нечётное): lowpass с частотой среза fc,
// или highpass (спектральная инверсия). Подавление вне полосы ≈ −74 дБ.
func ipFIR(fc float64, rate, taps int, high bool) []float64 {
	h := make([]float64, taps)
	m := float64(taps - 1)
	wc := 2 * math.Pi * fc / float64(rate)
	var sum float64
	for i := range h {
		x := float64(i) - m/2
		v := wc / math.Pi
		if x != 0 {
			v = math.Sin(wc*x) / (math.Pi * x)
		}
		w := 0.42 - 0.5*math.Cos(2*math.Pi*float64(i)/m) + 0.08*math.Cos(4*math.Pi*float64(i)/m)
		h[i] = v * w
		sum += h[i]
	}
	for i := range h {
		h[i] /= sum // единичное усиление на нуле
	}
	if high {
		for i := range h {
			h[i] = -h[i]
		}
		h[(taps-1)/2] += 1
	}
	return h
}

// ipBandEnergy — энергия сигнала s, пропущенного через h, на отсчётах [a,b).
func ipBandEnergy(s, h []float64, a, b int) float64 {
	half := len(h) / 2
	var e float64
	for i := a; i < b; i++ {
		var y float64
		for k, c := range h {
			j := i + half - k
			if j >= 0 && j < len(s) {
				y += c * s[j]
			}
		}
		e += y * y
	}
	return e
}

// 6. Маска частот у вставки на месте (LowpassHz) не идёт по всей дорожке: 6 вставок
// с LowpassHz 6000 на 240 с стерео 44,1 кГц — не дольше 5 с (маска по всей дорожке — 16 с),
// и точность: низ (< 1,5 кГц) в середине окна вычтен до ≤ −50 дБ, верх (> 9 кГц)
// остался (±0,5 дБ), вне окна — база (≤ −80 дБ). AtSec некруглые и у самого 0.
func TestInsertInPlaceLowpassSpeedAndAccuracy(t *testing.T) {
	needFFmpeg(t)
	if testing.Short() {
		t.Skip("short")
	}
	const sr, total, dur, fade = 44100, 240.0, 15.0, 0.05
	dir := t.TempDir()
	bp := filepath.Join(dir, "base.wav")
	op := filepath.Join(dir, "out.wav")
	ipNoiseStereo(t, bp, sr, total)
	ats := []float64{0.15, 20.007, 45.333, 70.611, 123.407, 180.013}
	ins := make([]Insert, len(ats))
	inputs := []string{bp}
	for k, at := range ats {
		ins[k] = Insert{AtSec: at, SkipSec: at, DurSec: dur, Gain: -1,
			FadeIn: fade, FadeOut: fade, LowpassHz: 6000}
		inputs = append(inputs, bp)
	}
	start := time.Now()
	if err := RunInputs(inputs, op, InsertsGraph(ins)); err != nil {
		t.Fatalf("run: %v", err)
	}
	el := time.Since(start)
	t.Logf("6 вставок на месте с LowpassHz 6000, 240 с стерео 44,1 кГц: %.2f с", el.Seconds())
	if el > 5*time.Second {
		t.Errorf("RunInputs %.2f с, want ≤ 5 с", el.Seconds())
	}

	base, out := ipDecodeStereo(t, bp, sr), ipDecodeStereo(t, op, sr)
	for c := 0; c < 2; c++ {
		if d := len(out[c]) - len(base[c]); d < -1 || d > 1 {
			t.Fatalf("длина результата %d отсчётов, база %d — должна совпасть", len(out[c]), len(base[c]))
		}
	}
	lp := ipFIR(1200, sr, 1023, false) // полоса ≤ ~1,3 кГц
	hp := ipFIR(10000, sr, 1023, true) // полоса ≥ ~10,1 кГц
	const meas = 1.0                   // длина отрезка замера в середине окна, с
	for _, at := range ats {
		mid := at + dur/2
		a, b := int((mid-meas/2)*sr), int((mid+meas/2)*sr)
		for c := 0; c < 2; c++ {
			lo := 10 * math.Log10(ipBandEnergy(out[c], lp, a, b)/ipBandEnergy(base[c], lp, a, b))
			hi := 10 * math.Log10(ipBandEnergy(out[c], hp, a, b)/ipBandEnergy(base[c], hp, a, b))
			t.Logf("at=%.3f кан.%d: низ %.1f дБ, верх %+.2f дБ", at, c, lo, hi)
			if lo > -50 {
				t.Errorf("at=%.3f кан.%d: низ в середине окна %.1f дБ относительно базы, want ≤ −50", at, c, lo)
			}
			if math.Abs(hi) > 0.5 {
				t.Errorf("at=%.3f кан.%d: верх в середине окна %+.2f дБ, want 0 ± 0.5", at, c, hi)
			}
		}
	}
	// вне окон: [0, n0 первой), между окнами, после последней — база
	prev := 0
	for _, at := range append(ats, total) {
		n0 := int(math.Round(at * sr))
		for c := 0; c < 2; c++ {
			if r := ipRelDB(out[c], base[c], prev, n0); r > -80 && n0 > prev {
				t.Errorf("вне окна [%d,%d) кан.%d: отличие от базы %.1f дБ, want ≤ −80", prev, n0, c, r)
			} else {
				t.Logf("вне окна [%.3f,%.3f) кан.%d: %.1f дБ", float64(prev)/sr, float64(n0)/sr, c, r)
			}
		}
		prev = int(math.Round((at + dur) * sr))
	}
}

// 7. Короткое окно у самого нуля: вставка с DurSec 5e-05 и фейдами 0.05 не роняет
// ffmpeg (было: «st=5e-05» в afade не разбирался) — на месте с AtSec 0 и 5e-05, вклейка с 5e-05.
func TestInsertInPlaceShortWindowAtZero(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	bp := filepath.Join(dir, "base.wav")
	op := filepath.Join(dir, "out.wav")
	ipNoise(t, bp, 44100, 2, 3)
	cases := map[string]Insert{
		"на месте, 0":     {AtSec: 0, SkipSec: 0, DurSec: 0.00005, Gain: -1, FadeIn: 0.05, FadeOut: 0.05},
		"на месте, 5e-05": {AtSec: 5e-05, SkipSec: 5e-05, DurSec: 0.00005, Gain: -1, FadeIn: 0.05, FadeOut: 0.05},
		"вклейка, 5e-05":  {AtSec: 5e-05, SkipSec: 0, DurSec: 0.00005, Gain: -1, FadeIn: 0.05, FadeOut: 0.05},
	}
	for name, in := range cases {
		g := InsertsGraph([]Insert{in})
		if err := RunInputs([]string{bp, bp}, op, g); err != nil {
			t.Errorf("%s: RunInputs с коротким окном: %v\ngraph: %s", name, err, g)
		}
	}
}

// 8. Малые времена пишутся в граф без экспоненты (ffmpeg не разбирает «5e-05»
// в части опций): и у вставки на месте, и у вклейки.
func TestInsertsGraphNoExponent(t *testing.T) {
	exp := regexp.MustCompile(`[0-9.][eE][-+]?[0-9]`)
	cases := map[string]Insert{
		"на месте": {AtSec: 5e-05, SkipSec: 5e-05, DurSec: 0.00005, Gain: -1, FadeIn: 0.05, FadeOut: 0.05},
		"вклейка":  {AtSec: 5e-05, SkipSec: 0, DurSec: 0.00005, Gain: -1, FadeIn: 0.05, FadeOut: 0.05},
	}
	for name, in := range cases {
		g := InsertsGraph([]Insert{in})
		if strings.Contains(g, "e-0") || strings.Contains(g, "e+0") || exp.MatchString(g) {
			t.Errorf("%s: в графе число с экспонентой: %s", name, g)
		}
	}
}
