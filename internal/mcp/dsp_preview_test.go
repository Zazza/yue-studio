package mcp

import (
	"os/exec"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-guitar-pedals, условие 1.8 (MCP dsp_preview): stem,
// from, to, steps — вместо chain/params (один из двух способов); результат —
// вариант с метриками, как раньше (загруженный превью-вариант). Воркер —
// fakeService (server_test.go), звук — синтетика ffmpeg (fxLavfi из stem_fx_test.go).

// pvServer — сервер с джобой 5: audio.flac = other + drums, стемы other/drums/bass/vocals (8 с).
func pvServer(t *testing.T) (*Server, *fakeService) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{{ID: 5, Status: "done", AudioFile: "audio.flac"}}
	fake.fetch = map[string]string{
		"audio.flac":       fxLavfi(t, "0.3*sin(2*PI*440*t)+0.2*sin(2*PI*300*t)", 8),
		"stem-other.flac":  fxLavfi(t, "0.3*sin(2*PI*440*t)", 8),
		"stem-drums.flac":  fxLavfi(t, "0.2*sin(2*PI*300*t)", 8),
		"stem-bass.flac":   fxLavfi(t, "0", 8),
		"stem-vocals.flac": fxLavfi(t, "0", 8),
	}
	return s, fake
}

// Карточка 1.8: dsp_preview со stem/from/to/steps принимается и отдаёт вариант.
func TestDspPreviewStepsOnStem(t *testing.T) {
	s, fake := pvServer(t)
	out, ok := call(t, s, "dsp_preview", jsonArgs(t,
		`{"job_id":5,"stem":"other","from":2.5,"to":4.5,"steps":[{"chain":"grit","params":{"drive":2}},{"chain":"reverb-room","off":true}]}`))
	if !ok {
		t.Fatalf("dsp_preview со steps на дорожке: %s", out)
	}
	if len(fake.uploads) != 1 {
		t.Errorf("загружено вариантов %d (%v), want 1 — превью-вариант с метриками, как раньше", len(fake.uploads), fake.uploads)
	}
	if !strings.Contains(out, `"file"`) {
		t.Errorf("ответ без варианта (нет \"file\"): %s", out)
	}
}

// Карточка 1.8: steps без stem — превью на весь трек.
func TestDspPreviewStepsWholeTrack(t *testing.T) {
	s, fake := pvServer(t)
	out, ok := call(t, s, "dsp_preview", jsonArgs(t,
		`{"job_id":5,"from":1,"to":3,"steps":[{"chain":"eq","params":{"high":-12}}]}`))
	if !ok {
		t.Fatalf("dsp_preview со steps на весь трек: %s", out)
	}
	if len(fake.uploads) != 1 {
		t.Errorf("загружено вариантов %d, want 1", len(fake.uploads))
	}
}

// Карточка 1.8: старый способ (chain/params) по-прежнему принимается.
func TestDspPreviewChainStillWorks(t *testing.T) {
	s, _ := pvServer(t)
	out, ok := call(t, s, "dsp_preview", jsonArgs(t,
		`{"job_id":5,"chain":"eq","params":{"high":-12},"from":1,"to":3}`))
	if !ok {
		t.Fatalf("dsp_preview с chain/params: %s", out)
	}
}

// Карточка 1.8: ни chain, ни steps — ошибка, ничего не загружено.
func TestDspPreviewNoChainNoStepsIsError(t *testing.T) {
	for name, args := range map[string]string{
		"без chain и steps": `{"job_id":5,"from":1,"to":3}`,
		"пустые steps":      `{"job_id":5,"from":1,"to":3,"steps":[]}`,
		"все шаги выкл":     `{"job_id":5,"from":1,"to":3,"steps":[{"chain":"eq","off":true}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			s, fake := pvServer(t)
			out, ok := call(t, s, "dsp_preview", jsonArgs(t, args))
			if ok {
				t.Fatalf("%s: want ошибку, ответ: %s", name, out)
			}
			if len(fake.uploads) != 0 {
				t.Errorf("при ошибке загружено: %v", fake.uploads)
			}
		})
	}
}

// Карточка 1.8 + 1.1: неизвестная или key-цепочка в steps — ошибка, ничего не загружено.
func TestDspPreviewBadStepIsError(t *testing.T) {
	for name, args := range map[string]string{
		"неизвестная": `{"job_id":5,"stem":"other","from":1,"to":3,"steps":[{"chain":"no-such"}]}`,
		"key-цепочка": `{"job_id":5,"stem":"other","from":1,"to":3,"steps":[{"chain":"ducking"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			s, fake := pvServer(t)
			out, ok := call(t, s, "dsp_preview", jsonArgs(t, args))
			if ok {
				t.Fatalf("%s: want ошибку, ответ: %s", name, out)
			}
			if len(fake.uploads) != 0 {
				t.Errorf("при ошибке загружено: %v", fake.uploads)
			}
		})
	}
}

// Карточка 1.8: схема инструмента описывает stem/from/to/steps.
func TestDspPreviewSchemaHasNewArgs(t *testing.T) {
	s, _ := newTestServer(t)
	s.mu.RLock()
	tool := s.tools["dsp_preview"]
	s.mu.RUnlock()
	schema, _ := tool.InputSchema["properties"].(map[string]any)
	for _, p := range []string{"stem", "from", "to", "steps", "chain"} {
		if _, ok := schema[p]; !ok {
			t.Errorf("dsp_preview: в схеме нет параметра %q", p)
		}
	}
	req, _ := tool.InputSchema["required"].([]string)
	for _, r := range req {
		if r == "chain" {
			t.Errorf("dsp_preview: chain обязателен, а должен быть один из chain/steps")
		}
	}
}
