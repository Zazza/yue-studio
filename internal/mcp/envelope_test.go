package mcp

import (
	"math"
	"os/exec"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-vocal-ride, условие 3 (MCP): volume_envelope
// {job_id, points:[{t,db}], stem?} → JSON варианта; без points/пустые — ошибка.
// Воркер — fakeService (server_test.go); звук — синтетика ffmpeg (fxLavfi/fxDecode/fxTone
// из stem_fx_test.go).

// Инструмент зарегистрирован, в схеме аргументов есть job_id, points, stem.
func TestVolumeEnvelopeToolRegistered(t *testing.T) {
	s, _ := newTestServer(t)
	s.mu.RLock()
	tool, ok := s.tools["volume_envelope"]
	s.mu.RUnlock()
	if !ok || tool.Name == "" {
		t.Fatal("инструмент volume_envelope не зарегистрирован")
	}
	props, _ := tool.InputSchema["properties"].(map[string]any)
	for _, p := range []string{"job_id", "points", "stem"} {
		if _, ok := props[p]; !ok {
			t.Errorf("в схеме нет свойства %q: %v", p, tool.InputSchema)
		}
	}
}

// Без points или с пустым массивом — ошибка, ничего не загружено.
func TestVolumeEnvelopeToolWithoutPointsIsError(t *testing.T) {
	for _, args := range []string{
		`{"job_id":5}`,
		`{"job_id":5,"points":[]}`,
		`{"job_id":5,"points":[],"stem":"vocals"}`,
	} {
		s, fake := newTestServer(t)
		fake.jobs = []yue.Job{{ID: 5, Status: "done", AudioFile: "audio.flac"}}
		fake.fetch = map[string]string{"audio.flac": "x"}
		if out, ok := call(t, s, "volume_envelope", jsonArgs(t, args)); ok {
			t.Errorf("%s: want ошибку, ответ: %s", args, out)
		}
		if len(fake.uploads) != 0 {
			t.Errorf("%s: при ошибке загружено: %v", args, fake.uploads)
		}
	}
}

// Неизвестная дорожка — ошибка, ничего не загружено.
func TestVolumeEnvelopeToolUnknownStemIsError(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{{ID: 5, Status: "done", AudioFile: "audio.flac"}}
	out, ok := call(t, s, "volume_envelope", jsonArgs(t, `{"job_id":5,"stem":"piano","points":[{"t":1,"db":-6}]}`))
	if ok {
		t.Fatalf("stem piano: want ошибку, ответ: %s", out)
	}
	if len(fake.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", fake.uploads)
	}
}

func needFF(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

// Весь трек (stem не задан): загружен вариант dsp-envelope.flac, ответ — JSON варианта
// с его файлом; громкость по точкам: до 1 с исходная, после 3 с −12 дБ.
func TestVolumeEnvelopeToolWholeTrack(t *testing.T) {
	needFF(t)
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{{ID: 5, Status: "done", AudioFile: "audio.flac"}}
	fake.fetch = map[string]string{"audio.flac": fxLavfi(t, "0.3*sin(2*PI*500*t)", 6)}
	out, ok := call(t, s, "volume_envelope", jsonArgs(t, `{"job_id":5,"points":[{"t":1,"db":0},{"t":3,"db":-12}]}`))
	if !ok {
		t.Fatalf("volume_envelope: %s", out)
	}
	data, ok := fake.uploads["dsp-envelope.flac"]
	if !ok || len(fake.uploads) != 1 {
		t.Fatalf("загрузки %d, want одна dsp-envelope.flac", len(fake.uploads))
	}
	if !strings.Contains(out, "dsp-envelope.flac") {
		t.Errorf("в ответе нет файла варианта: %s", out)
	}
	res := fxDecode(t, data)
	if d := 20 * math.Log10(fxTone(res, 500, 0, 1)/0.3); math.Abs(d) > 0.5 {
		t.Errorf("0–1 с: %+.2f дБ, want ≈ 0", d)
	}
	if d := 20 * math.Log10(fxTone(res, 500, 4, 5)/0.3); math.Abs(d-(-12)) > 0.5 {
		t.Errorf("4–5 с: %+.2f дБ, want −12 ± 0.5", d)
	}
}

// stem vocals: через пересборку меняется только голос (3000 Гц), остальное (500 Гц) цело;
// загружен один вариант.
func TestVolumeEnvelopeToolStem(t *testing.T) {
	needFF(t)
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
	out, ok := call(t, s, "volume_envelope", jsonArgs(t, `{"job_id":5,"stem":"vocals","points":[{"t":1,"db":0},{"t":3,"db":-12}]}`))
	if !ok {
		t.Fatalf("volume_envelope со stem: %s", out)
	}
	if len(fake.uploads) != 1 {
		t.Fatalf("загрузок %d, want 1", len(fake.uploads))
	}
	var res []float32
	for name, data := range fake.uploads {
		if !strings.Contains(out, name) {
			t.Errorf("в ответе нет файла варианта %q: %s", name, out)
		}
		res = fxDecode(t, data)
	}
	if d := 20 * math.Log10(fxTone(res, 3000, 4, 7)/0.3); math.Abs(d-(-12)) > 0.5 {
		t.Errorf("голос 4–7 с: %+.2f дБ, want −12 ± 0.5", d)
	}
	for _, w := range [][2]float64{{0, 1}, {4, 7}} {
		if d := 20 * math.Log10(fxTone(res, 500, w[0], w[1])/0.3); math.Abs(d) > 0.5 {
			t.Errorf("500 Гц (остальное) %.0f–%.0f с: %+.2f дБ, want ±0.5", w[0], w[1], d)
		}
	}
}
