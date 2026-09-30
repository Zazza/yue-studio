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
