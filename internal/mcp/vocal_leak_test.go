package mcp

import (
	"encoding/json"
	"testing"

	"yue-studio/internal/yue"
)

// jobs brief=true: vocal_leak виден у джоб, где он непустой, и отсутствует у остальных.
func TestJobsToolBriefVocalLeak(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{
		{ID: 1, Title: "leaky", Status: "done", Style: "instrumental", VocalLeak: "25.4, 61.2"},
		{ID: 2, Title: "clean", Status: "done", Style: "instrumental"},
	}
	out, ok := call(t, s, "jobs", map[string]any{"brief": true})
	if !ok {
		t.Fatalf("jobs failed: %s", out)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("jobs out is not a JSON array: %s", out)
	}
	byID := map[float64]map[string]any{}
	for _, r := range rows {
		id, _ := r["id"].(float64)
		byID[id] = r
	}
	if len(byID) != 2 {
		t.Fatalf("want 2 rows, got %s", out)
	}
	if got := byID[1]["vocal_leak"]; got != "25.4, 61.2" {
		t.Fatalf("job 1 vocal_leak = %v, want %q: %s", got, "25.4, 61.2", out)
	}
	if _, has := byID[2]["vocal_leak"]; has {
		t.Fatalf("job 2 must not have vocal_leak: %s", out)
	}
}
