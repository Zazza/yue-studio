package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

type voiceCall struct {
	jobID int64
	p     yue.VoiceParams
}

// voiceFake — fakeService + VoiceConvert, записывающий аргументы.
type voiceFake struct {
	*fakeService
	calls []voiceCall
	retID int64
	err   error
}

func (f *voiceFake) VoiceConvert(ctx context.Context, jobID int64, p yue.VoiceParams) (int64, error) {
	f.calls = append(f.calls, voiceCall{jobID: jobID, p: p})
	if f.err != nil {
		return 0, f.err
	}
	return f.retID, nil
}

func newVoiceServer(t *testing.T, retID int64, err error) (*Server, *voiceFake) {
	t.Helper()
	fake := &voiceFake{fakeService: &fakeService{url: "http://w:8091"}, retID: retID, err: err}
	s := NewServer(fake, t.TempDir())
	RegisterWorkflowTools(s)
	RegisterStudioTools(s)
	RegisterLibraryTools(s)
	RegisterInstallTools(s)
	return s, fake
}

func TestVoiceConvertToolAllParams(t *testing.T) {
	s, fake := newVoiceServer(t, 4321, nil)
	out, ok := call(t, s, "voice_convert", map[string]any{
		"job_id": float64(214), "ref_job_id": float64(135),
		"ref_from": 12.5, "ref_dur": float64(20), "steps": float64(60), "title": "голос 135",
	})
	if !ok {
		t.Fatalf("voice_convert failed: %s", out)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls %d", len(fake.calls))
	}
	c := fake.calls[0]
	want := yue.VoiceParams{RefJobID: 135, RefFrom: 12.5, RefDur: 20, Steps: 60, Title: "голос 135"}
	if c.jobID != 214 || c.p != want {
		t.Fatalf("args job=%d %+v, want 214 %+v", c.jobID, c.p, want)
	}
	if !strings.Contains(out, "4321") {
		t.Fatalf("out must contain new job id: %s", out)
	}
}

func TestVoiceConvertToolDefaults(t *testing.T) {
	s, fake := newVoiceServer(t, 88, nil)
	out, ok := call(t, s, "voice_convert", map[string]any{"job_id": float64(5), "ref_job_id": float64(9)})
	if !ok {
		t.Fatalf("voice_convert failed: %s", out)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls %d", len(fake.calls))
	}
	c := fake.calls[0]
	if c.jobID != 5 || c.p != (yue.VoiceParams{RefJobID: 9}) {
		t.Fatalf("unset params must be zero: job=%d %+v", c.jobID, c.p)
	}
	if !strings.Contains(out, "88") {
		t.Fatalf("out must contain new job id: %s", out)
	}
}

func TestVoiceConvertToolServiceError(t *testing.T) {
	s, fake := newVoiceServer(t, 0, &yue.StatusError{Code: 503, Msg: "yue /jobs/5/voice: 503: Seed-VC не установлен"})
	out, ok := call(t, s, "voice_convert", map[string]any{"job_id": float64(5), "ref_job_id": float64(9)})
	if ok {
		t.Fatalf("expected tool error, got %s", out)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("service must be called once: %d", len(fake.calls))
	}
	if !strings.Contains(out, "Seed-VC не установлен") {
		t.Fatalf("service error text must reach caller: %s", out)
	}
}

func TestVoiceConvertToolPlainErrorPropagates(t *testing.T) {
	s, _ := newVoiceServer(t, 0, errors.New("нет такого трека"))
	out, ok := call(t, s, "voice_convert", map[string]any{"job_id": float64(5), "ref_job_id": float64(9)})
	if ok || !strings.Contains(out, "нет такого трека") {
		t.Fatalf("error must propagate: ok=%v %s", ok, out)
	}
}

func TestVoiceConvertToolRequiresIDs(t *testing.T) {
	cases := []map[string]any{
		{"ref_job_id": float64(9)},
		{"job_id": float64(5)},
		{},
	}
	for _, args := range cases {
		s, fake := newVoiceServer(t, 1, nil)
		out, ok := call(t, s, "voice_convert", args)
		if ok {
			t.Fatalf("args %v: expected error, got %s", args, out)
		}
		if len(fake.calls) != 0 {
			t.Fatalf("args %v: service must not be called: %+v", args, fake.calls)
		}
	}
}
