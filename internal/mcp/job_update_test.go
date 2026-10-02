package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

type updateCall struct {
	id            int64
	title, folder *string
}

// updateFake — fakeService + UpdateJob, записывающий аргументы.
type updateFake struct {
	*fakeService
	updates []updateCall
}

func (f *updateFake) UpdateJob(ctx context.Context, jobID int64, title, folder *string) (*yue.Job, error) {
	c := updateCall{id: jobID}
	if title != nil {
		v := *title
		c.title = &v
	}
	if folder != nil {
		v := *folder
		c.folder = &v
	}
	f.updates = append(f.updates, c)
	j := yue.Job{ID: jobID, Title: "старое имя", Status: "done"}
	if title != nil {
		j.Title = *title
	}
	if folder != nil {
		j.Folder = *folder
	}
	return &j, nil
}

func newUpdateServer(t *testing.T) (*Server, *updateFake) {
	t.Helper()
	fake := &updateFake{fakeService: &fakeService{url: "http://w:8091"}}
	s := NewServer(fake, t.TempDir())
	RegisterWorkflowTools(s)
	RegisterStudioTools(s)
	RegisterLibraryTools(s)
	RegisterInstallTools(s)
	return s, fake
}

func TestJobUpdateTitleOnly(t *testing.T) {
	s, fake := newUpdateServer(t)
	out, ok := call(t, s, "job_update", map[string]any{"job_id": float64(12), "title": "Ночной гараж"})
	if !ok {
		t.Fatalf("job_update failed: %s", out)
	}
	if len(fake.updates) != 1 {
		t.Fatalf("calls %d", len(fake.updates))
	}
	c := fake.updates[0]
	if c.id != 12 || c.title == nil || *c.title != "Ночной гараж" || c.folder != nil {
		t.Fatalf("args %+v", c)
	}
	if !strings.Contains(out, "12") || !strings.Contains(out, "Ночной гараж") {
		t.Fatalf("out must mention id and title: %s", out)
	}
	if !strings.Contains(out, "без папки") {
		t.Fatalf("empty folder must read «без папки»: %s", out)
	}
}

func TestJobUpdateFolderOnly(t *testing.T) {
	s, fake := newUpdateServer(t)
	out, ok := call(t, s, "job_update", map[string]any{"job_id": float64(7), "folder": "Альбом"})
	if !ok {
		t.Fatalf("job_update failed: %s", out)
	}
	c := fake.updates[0]
	if c.id != 7 || c.title != nil || c.folder == nil || *c.folder != "Альбом" {
		t.Fatalf("args %+v", c)
	}
	if !strings.Contains(out, "7") || !strings.Contains(out, "Альбом") {
		t.Fatalf("out must mention id and folder: %s", out)
	}
	if strings.Contains(out, "без папки") {
		t.Fatalf("folder set, but out says «без папки»: %s", out)
	}
}

func TestJobUpdateEmptyFolderClears(t *testing.T) {
	s, fake := newUpdateServer(t)
	out, ok := call(t, s, "job_update", map[string]any{"job_id": float64(7), "folder": ""})
	if !ok {
		t.Fatalf("job_update failed: %s", out)
	}
	if len(fake.updates) != 1 {
		t.Fatalf("calls %d", len(fake.updates))
	}
	c := fake.updates[0]
	if c.folder == nil || *c.folder != "" {
		t.Fatalf("folder \"\" must be passed as pointer to empty string: %+v", c)
	}
	if c.title != nil {
		t.Fatalf("title must be nil: %+v", c)
	}
	if !strings.Contains(out, "без папки") {
		t.Fatalf("out: %s", out)
	}
}

func TestJobUpdateBothFields(t *testing.T) {
	s, fake := newUpdateServer(t)
	out, ok := call(t, s, "job_update", map[string]any{"job_id": float64(3), "title": "T", "folder": "F"})
	if !ok {
		t.Fatalf("job_update failed: %s", out)
	}
	c := fake.updates[0]
	if c.title == nil || *c.title != "T" || c.folder == nil || *c.folder != "F" {
		t.Fatalf("args %+v", c)
	}
}

func TestJobUpdateNothingToChangeIsError(t *testing.T) {
	s, fake := newUpdateServer(t)
	out, ok := call(t, s, "job_update", map[string]any{"job_id": float64(3)})
	if ok {
		t.Fatalf("expected tool error, got %s", out)
	}
	if len(fake.updates) != 0 {
		t.Fatalf("service must not be called: %+v", fake.updates)
	}
}

func TestJobsToolBriefHasNoStyle(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{
		{ID: 1, Title: "root", Status: "done", Folder: "Альбом", Style: "doom-style-marker", Lyrics: "lyrics-marker"},
		{ID: 2, Title: "sec", Status: "done", ParentID: idp(1), Role: "section", Style: "doom-style-marker"},
		{ID: 3, Title: "other", Status: "done", Style: "doom-style-marker"},
	}
	out, ok := call(t, s, "jobs", map[string]any{"folder": "альбом", "limit": float64(10), "brief": true})
	if !ok {
		t.Fatalf("jobs failed: %s", out)
	}
	if strings.Contains(out, `"style"`) || strings.Contains(out, "doom-style-marker") {
		t.Fatalf("brief jobs must not contain style: %s", out)
	}
	// содержимое — то же, что listJobs
	want, _ := json.Marshal(listJobs(fake.jobs, "альбом", 10, true))
	var gotV, wantV any
	if err := json.Unmarshal([]byte(out), &gotV); err == nil {
		_ = json.Unmarshal(want, &wantV)
		gb, _ := json.Marshal(gotV)
		wb, _ := json.Marshal(wantV)
		if string(gb) != string(wb) {
			t.Fatalf("jobs out %s, want %s", gb, wb)
		}
	} else if !strings.Contains(out, `"root"`) || strings.Contains(out, `"other"`) {
		t.Fatalf("jobs out must be the folder subset: %s", out)
	}
}
