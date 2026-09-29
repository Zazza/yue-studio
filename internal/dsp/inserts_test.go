package dsp

import (
	"encoding/binary"
	"math"
	"os/exec"
	"path/filepath"
	"regexp"
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
