package mcp

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"yue-studio/internal/studio"
	"yue-studio/internal/yue"
)

// Тесты карточки internal-stem-fx, условие 5 (MCP): rebuild_sections — спека
// принимает chain/params; dsp_apply — необязательные stem/from/to (эффект на
// дорожку через ту же пересборку); find_tones — stem. Воркер — fakeService
// (server_test.go); звук — синтетика ffmpeg.

// ---------- parseSectionSpecs: chain / params ----------

func TestParseSectionSpecsChainParams(t *testing.T) {
	var raw any
	if err := json.Unmarshal([]byte(`[
		{"from":176,"to":0,"stems":["vocals"],"chain":"dewhistle","params":{"freq":2638,"freq2":3628,"depth":15}},
		{"from":1,"to":2,"stems":["drums"],"db":-100}
	]`), &raw); err != nil {
		t.Fatal(err)
	}
	got, err := parseSectionSpecs(raw)
	if err != nil {
		t.Fatalf("parseSectionSpecs: %v", err)
	}
	want := []studio.SectionSpec{
		{From: 176, To: 0, Stems: []string{"vocals"}, Chain: "dewhistle",
			Params: map[string]float64{"freq": 2638, "freq2": 3628, "depth": 15}},
		// без chain/params — как раньше: пустая цепочка, без параметров
		{From: 1, To: 2, Stems: []string{"drums"}, Db: -100},
	}
	if len(got) != 2 {
		t.Fatalf("спек %d, want 2: %+v", len(got), got)
	}
	if got[0].Chain != want[0].Chain || !reflect.DeepEqual(got[0].Params, want[0].Params) ||
		got[0].From != 176 || got[0].To != 0 || !reflect.DeepEqual(got[0].Stems, want[0].Stems) {
		t.Errorf("спека с цепочкой:\n got %+v\nwant %+v", got[0], want[0])
	}
	if got[1].Chain != "" || len(got[1].Params) != 0 || got[1].Db != -100 {
		t.Errorf("спека без цепочки: %+v, want Chain пустой, Params пустые, Db −100", got[1])
	}
}

// ---------- find_tones: stem ----------

func TestFindTonesPassesStem(t *testing.T) {
	s, fake := newTestServer(t)
	fake.tonesOut = []yue.Tone{{Hz: 2638, ProminenceDb: 18}}
	out, ok := call(t, s, "find_tones", jsonArgs(t, `{"job_id":254,"from":176,"to":0,"stem":"vocals"}`))
	if !ok {
		t.Fatalf("find_tones failed: %s", out)
	}
	if len(fake.tonesCalls) != 1 || fake.tonesCalls[0] != (tonesCall{254, 176, 0, "vocals"}) {
		t.Fatalf("JobTones args: %+v, want {254 176 0 vocals}", fake.tonesCalls)
	}
	if !strings.Contains(out, "2638") {
		t.Errorf("в ответе нет найденного тона 2638: %s", out)
	}
}

// Без stem — микс, как раньше: клиенту уходит пустой stem.
func TestFindTonesWithoutStemIsMix(t *testing.T) {
	s, fake := newTestServer(t)
	out, ok := call(t, s, "find_tones", jsonArgs(t, `{"job_id":3,"from":1,"to":5}`))
	if !ok {
		t.Fatalf("find_tones failed: %s", out)
	}
	if len(fake.tonesCalls) != 1 || fake.tonesCalls[0] != (tonesCall{3, 1, 5, ""}) {
		t.Fatalf("JobTones args: %+v, want {3 1 5 \"\"}", fake.tonesCalls)
	}
}

// ---------- dsp_apply: stem / from / to ----------

// С stem неизвестная цепочка — ошибка, ничего не загружено.
func TestDspApplyStemUnknownChainIsError(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{{ID: 5, Status: "done", AudioFile: "audio.flac"}}
	out, ok := call(t, s, "dsp_apply", jsonArgs(t, `{"job_id":5,"chain":"no-such-chain","stem":"vocals","from":2,"to":4}`))
	if ok {
		t.Fatalf("неизвестная цепочка со stem: want ошибку, ответ: %s", out)
	}
	if len(fake.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", fake.uploads)
	}
}

const fxSR = 16000

func fxLavfi(t *testing.T, expr string, dur float64) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.flac")
	src := fmt.Sprintf("aevalsrc=exprs='%s':d=%g:s=%d", expr, dur, fxSR)
	if out, err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", src, "-ac", "1", "-ar", "16000", p).CombinedOutput(); err != nil {
		t.Fatalf("lavfi %q: %v %s", src, err, out)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func fxDecode(t *testing.T, data []byte) []float32 {
	t.Helper()
	p := filepath.Join(t.TempDir(), "up.flac")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", p, "-f", "f32le", "-ac", "1", "-ar", "16000", "-").Output()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	s := make([]float32, len(raw)/4)
	for i := range s {
		s[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return s
}

func fxTone(s []float32, hz, from, to float64) float64 {
	a, b := int(from*fxSR), int(to*fxSR)
	if b > len(s) {
		b = len(s)
	}
	if a >= b {
		return 0
	}
	w := 2 * math.Pi * hz / fxSR
	var re, im float64
	for i, v := range s[a:b] {
		re += float64(v) * math.Cos(w*float64(i))
		im -= float64(v) * math.Sin(w*float64(i))
	}
	return 2 * math.Hypot(re, im) / float64(b-a)
}

// С stem/from/to эффект ложится только на дорожку и только в окне: у трека
// (голос 3000 Гц + остальное 500 Гц) вырез 3000 на голосе в 2–4 с — в окне 3000
// ослаблен ≥ 15 дБ, 500 цел; вне окна 3000 на месте (вырез на весь трек срезал бы
// его везде). Результат — новый вариант у той же джобы.
func TestDspApplyStemWindowGoesThroughRebuild(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{{ID: 5, Status: "done", AudioFile: "audio.flac"}}
	const a3000, b500 = "0.3*sin(2*PI*3000*t)", "0.3*sin(2*PI*500*t)"
	fake.fetch = map[string]string{
		"audio.flac":       fxLavfi(t, a3000+"+"+b500, 8),
		"stem-vocals.flac": fxLavfi(t, a3000, 8),
		"stem-other.flac":  fxLavfi(t, b500, 8),
		"stem-drums.flac":  fxLavfi(t, "0", 8),
		"stem-bass.flac":   fxLavfi(t, "0", 8),
	}
	out, ok := call(t, s, "dsp_apply", jsonArgs(t, `{"job_id":5,"chain":"dewhistle","stem":"vocals","from":2,"to":4,
		"params":{"freq":3000,"depth":30,"width":60,"harmonics":1,"start":0,"end":0}}`))
	if !ok {
		t.Fatalf("dsp_apply со stem: %s", out)
	}
	if len(fake.uploads) != 1 {
		t.Fatalf("загрузок %d, want 1", len(fake.uploads))
	}
	var res []float32
	for _, data := range fake.uploads {
		res = fxDecode(t, data)
	}
	if d := 20*math.Log10(fxTone(res, 3000, 2.5, 3.5)) - 20*math.Log10(0.3); d > -15 {
		t.Errorf("3000 Гц (голос) в окне изменился на %+.1f дБ, want ≤ −15", d)
	}
	if a := fxTone(res, 500, 2.5, 3.5); math.Abs(20*math.Log10(a/0.3)) > 0.5 {
		t.Errorf("500 Гц (остальное) в окне %.4f, want ≈ 0.3 (не та дорожка)", a)
	}
	for _, w := range [][2]float64{{0, 1.5}, {4.5, 8}} {
		if a := fxTone(res, 3000, w[0], w[1]); math.Abs(20*math.Log10(a/0.3)) > 0.5 {
			t.Errorf("3000 Гц вне окна %.1f–%.1f с %.4f, want ≈ 0.3 (эффект только в окне)", w[0], w[1], a)
		}
	}
}
