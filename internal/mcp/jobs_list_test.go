package mcp

import (
	"encoding/json"
	"testing"

	"yue-studio/internal/yue"
)

func idp(v int64) *int64 { return &v }

// listRows — результат listJobs через JSON: не зависит от имени типа.
func listRows(t *testing.T, v any) []map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatalf("result is not a JSON array: %s", b)
	}
	return rows
}

func rowIDs(rows []map[string]any) []int64 {
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		f, _ := r["id"].(float64)
		ids = append(ids, int64(f))
	}
	return ids
}

func eqIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// listFixture: 1 «Альбом» (2 — секция от 1, 3 — овердаб на 2, 7 — пересборка от 3),
// 4 без папки, 5 «Основы», 6 — производный от несуществующего 99 (сам себе корень).
func listFixture() []yue.Job {
	return []yue.Job{
		{ID: 1, Title: "root", Status: "done", Folder: "Альбом", Style: "doom", Lyrics: "la", CreatedAt: "2026-01-01"},
		{ID: 2, Title: "sec", Status: "done", ParentID: idp(1), Role: "section"},
		{ID: 3, Title: "od", Status: "done", OverdubOf: idp(2)},
		{ID: 4, Title: "loose", Status: "done"},
		{ID: 5, Title: "basics", Status: "done", Folder: "Основы"},
		{ID: 6, Title: "orphan", Status: "done", ParentID: idp(99), Folder: "Альбом"},
		{ID: 7, Title: "rb", Status: "running", ParentID: idp(3), Role: "rebuild"},
	}
}

func TestListJobsAllWhenFolderEmpty(t *testing.T) {
	rows := listRows(t, listJobs(listFixture(), "", 0, false))
	if got := rowIDs(rows); !eqIDs(got, []int64{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatalf("ids %v", got)
	}
}

func TestListJobsEmptyInput(t *testing.T) {
	for _, brief := range []bool{false, true} {
		rows := listRows(t, listJobs(nil, "Альбом", 10, brief))
		if len(rows) != 0 {
			t.Fatalf("brief=%v: want empty, got %v", brief, rows)
		}
	}
}

func TestListJobsFolderIncludesDerivedChainKeepsOrder(t *testing.T) {
	rows := listRows(t, listJobs(listFixture(), "Альбом", 0, false))
	// 6 — сирота с собственной папкой «Альбом»: сам себе корень, тоже попадает
	if got := rowIDs(rows); !eqIDs(got, []int64{1, 2, 3, 6, 7}) {
		t.Fatalf("ids %v", got)
	}
}

func TestListJobsFolderCaseAndSpaceInsensitive(t *testing.T) {
	rows := listRows(t, listJobs(listFixture(), "  альбом ", 0, false))
	if got := rowIDs(rows); !eqIDs(got, []int64{1, 2, 3, 6, 7}) {
		t.Fatalf("ids %v", got)
	}
	rows = listRows(t, listJobs(listFixture(), "ОСНОВЫ", 0, false))
	if got := rowIDs(rows); !eqIDs(got, []int64{5}) {
		t.Fatalf("ids %v", got)
	}
}

func TestListJobsDashMeansNoFolder(t *testing.T) {
	rows := listRows(t, listJobs(listFixture(), "-", 0, false))
	if got := rowIDs(rows); !eqIDs(got, []int64{4}) {
		t.Fatalf("ids %v", got)
	}
}

func TestListJobsDerivedFolderComesFromRootNotOwn(t *testing.T) {
	jobs := []yue.Job{
		{ID: 1, Title: "root", Folder: "A"},
		{ID: 2, Title: "child", ParentID: idp(1), Folder: "B"},
	}
	if got := rowIDs(listRows(t, listJobs(jobs, "A", 0, false))); !eqIDs(got, []int64{1, 2}) {
		t.Fatalf("folder A: %v", got)
	}
	if got := rowIDs(listRows(t, listJobs(jobs, "B", 0, false))); len(got) != 0 {
		t.Fatalf("folder B must be empty (child follows root): %v", got)
	}
}

func TestListJobsUnknownFolderEmpty(t *testing.T) {
	if rows := listRows(t, listJobs(listFixture(), "нет такой", 0, false)); len(rows) != 0 {
		t.Fatalf("want empty, got %v", rowIDs(rows))
	}
}

func TestListJobsLimitAfterFilter(t *testing.T) {
	rows := listRows(t, listJobs(listFixture(), "Альбом", 2, false))
	if got := rowIDs(rows); !eqIDs(got, []int64{1, 2}) {
		t.Fatalf("ids %v", got)
	}
	// limit больше выборки — всё
	if got := rowIDs(listRows(t, listJobs(listFixture(), "-", 5, false))); !eqIDs(got, []int64{4}) {
		t.Fatalf("ids %v", got)
	}
	// limit 0 — без ограничения
	if got := rowIDs(listRows(t, listJobs(listFixture(), "", 0, false))); len(got) != 7 {
		t.Fatalf("limit 0: %v", got)
	}
}

func TestListJobsFullReturnsJobSlice(t *testing.T) {
	v := listJobs(listFixture(), "Основы", 0, false)
	jobs, ok := v.([]yue.Job)
	if !ok {
		t.Fatalf("brief=false must return []yue.Job, got %T", v)
	}
	if len(jobs) != 1 || jobs[0].ID != 5 {
		t.Fatalf("jobs %+v", jobs)
	}
	rows := listRows(t, listJobs(listFixture(), "Альбом", 1, false))
	if rows[0]["style"] != "doom" || rows[0]["lyrics"] != "la" {
		t.Fatalf("full rows keep style/lyrics: %v", rows[0])
	}
}

func TestListJobsBriefFields(t *testing.T) {
	rows := listRows(t, listJobs(listFixture(), "Альбом", 0, true))
	if got := rowIDs(rows); !eqIDs(got, []int64{1, 2, 3, 6, 7}) {
		t.Fatalf("ids %v", got)
	}
	allowed := map[string]bool{"id": true, "title": true, "status": true, "duration_sec": true,
		"parent_id": true, "role": true, "folder": true, "created_at": true}
	for _, r := range rows {
		for k := range r {
			if !allowed[k] {
				t.Fatalf("brief row has unexpected field %q: %v", k, r)
			}
		}
		if _, has := r["style"]; has {
			t.Fatalf("brief must not contain style: %v", r)
		}
		if _, has := r["lyrics"]; has {
			t.Fatalf("brief must not contain lyrics: %v", r)
		}
		// папка — корня песни, даже у производных с пустым Folder
		if r["folder"] != "Альбом" {
			t.Fatalf("brief folder must be root folder: %v", r)
		}
	}
	root := rows[0]
	if root["title"] != "root" || root["status"] != "done" || root["created_at"] != "2026-01-01" {
		t.Fatalf("root row %v", root)
	}
	sec := rows[1]
	if sec["parent_id"] != float64(1) || sec["role"] != "section" {
		t.Fatalf("section row %v", sec)
	}
}
