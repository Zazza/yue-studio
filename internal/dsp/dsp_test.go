package dsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilterGraphClampAndDefaults(t *testing.T) {
	c := ByID("wall")
	if c == nil {
		t.Fatal("no wall chain")
	}
	g := c.FilterGraph(map[string]float64{"exciter": 99, "noise": -5})
	if !strings.Contains(g, "amount=6.00") {
		t.Errorf("exciter not clamped to max: %s", g)
	}
	if !strings.Contains(g, "amplitude=0.000") {
		t.Errorf("noise not clamped to min: %s", g)
	}
	d := c.FilterGraph(nil)
	for _, want := range []string{"amount=2.50", "limit=0.50", "amplitude=0.090"} {
		if !strings.Contains(d, want) {
			t.Errorf("defaults: %q missing in %s", want, d)
		}
	}
}

func TestRunWallOnSine(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.flac")
	out := filepath.Join(dir, "out.flac")
	if err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3", in).Run(); err != nil {
		t.Fatalf("make test input: %v", err)
	}
	c := ByID("wall")
	if err := Run(in, out, c.FilterGraph(nil), nil); err != nil {
		t.Fatalf("run wall: %v", err)
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() < 1000 {
		t.Fatalf("suspicious output: %v %v", st, err)
	}
}
