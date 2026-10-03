package mcp

import (
	"encoding/json"
	"testing"

	"yue-studio/internal/yue"
)

// jobs brief=true: число миксов (mixes) видно у трека с миксами,
// у трека без миксов поле отсутствует.
func TestJobsToolBriefMixes(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{
		{ID: 1, Title: "с миксами", Status: "done", Mixes: 2},
		{ID: 2, Title: "без миксов", Status: "done"},
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
	if got := byID[1]["mixes"]; got != float64(2) {
		t.Fatalf("job 1 mixes = %v, want 2: %s", got, out)
	}
	if _, has := byID[2]["mixes"]; has {
		t.Fatalf("job 2 must not have mixes: %s", out)
	}
}
