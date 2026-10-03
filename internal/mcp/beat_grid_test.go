package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Спецификация инструмента beat_grid {job_id, from?, to?}: вызывает JobGrid с этими
// значениями; в тексте — bpm и offset с 3 знаками; ошибка клиента → ошибка инструмента.

type gridCall struct {
	id       int64
	from, to float64
}

type gridFake struct {
	*fakeService
	calls []gridCall
	grid  *yue.BeatGrid
	err   error
}

func (f *gridFake) JobGrid(ctx context.Context, id int64, from, to float64) (*yue.BeatGrid, error) {
	f.calls = append(f.calls, gridCall{id, from, to})
	return f.grid, f.err
}

func newGridServer(t *testing.T) (*Server, *gridFake) {
	t.Helper()
	fake := &gridFake{fakeService: &fakeService{url: "http://w:8091"},
		grid: &yue.BeatGrid{BPM: 127.5, Offset: 10.2064, Strength: 0.81, Source: "drums"}}
	s := NewServer(fake, t.TempDir())
	RegisterWorkflowTools(s)
	RegisterStudioTools(s)
	RegisterLibraryTools(s)
	RegisterInstallTools(s)
	return s, fake
}

func TestBeatGridPassesWindow(t *testing.T) {
	s, fake := newGridServer(t)
	out, ok := call(t, s, "beat_grid", map[string]any{
		"job_id": float64(214), "from": float64(10), "to": float64(25.5)})
	if !ok {
		t.Fatalf("beat_grid failed: %s", out)
	}
	if len(fake.calls) != 1 || fake.calls[0] != (gridCall{214, 10, 25.5}) {
		t.Fatalf("JobGrid calls %+v, want [{214 10 25.5}]", fake.calls)
	}
	if !strings.Contains(out, "127.5") {
		t.Errorf("в выводе нет bpm 127.5: %s", out)
	}
	if !strings.Contains(out, "10.206") {
		t.Errorf("в выводе нет offset с 3 знаками (10.206): %s", out)
	}
	if strings.Contains(out, "10.2064") {
		t.Errorf("offset должен быть с 3 знаками, а не полной точностью: %s", out)
	}
}

func TestBeatGridWithoutWindowCallsOnce(t *testing.T) {
	s, fake := newGridServer(t)
	out, ok := call(t, s, "beat_grid", map[string]any{"job_id": float64(5)})
	if !ok {
		t.Fatalf("beat_grid failed: %s", out)
	}
	if len(fake.calls) != 1 || fake.calls[0].id != 5 {
		t.Fatalf("JobGrid calls %+v", fake.calls)
	}
}

func TestBeatGridClientErrorIsToolError(t *testing.T) {
	s, fake := newGridServer(t)
	fake.grid, fake.err = nil, errors.New("422: window shorter than 4 s")
	out, ok := call(t, s, "beat_grid", map[string]any{
		"job_id": float64(9), "from": float64(10), "to": float64(12)})
	if ok {
		t.Fatalf("expected tool error, got %s", out)
	}
	if len(fake.calls) != 1 || fake.calls[0] != (gridCall{9, 10, 12}) {
		t.Fatalf("JobGrid calls %+v", fake.calls)
	}
}
