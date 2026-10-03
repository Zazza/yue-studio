package mcp

import (
	"strings"

	"yue-studio/internal/yue"
)

// jobsNoFolder — значение фильтра folder «только песни без папки».
const jobsNoFolder = "-"

// jobBrief — краткая строка списка треков для агента: без стиля, текста и плана.
type jobBrief struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`
	DurationSec float64 `json:"duration_sec,omitempty"`
	ParentID    *int64  `json:"parent_id,omitempty"`
	Role        string  `json:"role,omitempty"`
	Folder      string  `json:"folder,omitempty"`
	CreatedAt   string  `json:"created_at"`
	// VocalLeak — в треке «без голоса» звучит дорожка голоса (секунды начала)
	VocalLeak string `json:"vocal_leak,omitempty"`
	// Mixes — готовые миксы трека (вклейки, эффекты на дорожки): dsp_variants
	Mixes int `json:"mixes,omitempty"`
}

// jobParentID — родитель производного трека: parent_id или overdub_of.
func jobParentID(j yue.Job) *int64 {
	if j.ParentID != nil {
		return j.ParentID
	}
	return j.OverdubOf
}

// listJobs — список треков для MCP jobs: folder "" — все; jobsNoFolder — песни
// без папки; иначе — песни этой папки (без учёта регистра и пробелов по краям).
// Папка — у корня песни, версии идут вместе с ним. Порядок входа сохраняется,
// limit > 0 — первые limit строк после фильтра; brief — краткие строки.
func listJobs(jobs []yue.Job, folder string, limit int, brief bool) any {
	byID := make(map[int64]yue.Job, len(jobs))
	for _, j := range jobs {
		byID[j.ID] = j
	}
	rootOf := func(j yue.Job) yue.Job {
		seen := map[int64]bool{}
		for p := jobParentID(j); p != nil && !seen[j.ID]; p = jobParentID(j) {
			parent, ok := byID[*p]
			if !ok {
				break
			}
			seen[j.ID] = true
			j = parent
		}
		return j
	}
	want := strings.TrimSpace(folder)
	keep := func(j yue.Job) bool {
		if want == "" {
			return true
		}
		f := strings.TrimSpace(rootOf(j).Folder)
		if want == jobsNoFolder {
			return f == ""
		}
		return strings.EqualFold(f, want)
	}
	full := []yue.Job{}
	short := []jobBrief{}
	for _, j := range jobs {
		if !keep(j) {
			continue
		}
		if limit > 0 && len(full)+len(short) >= limit {
			break
		}
		if brief {
			short = append(short, jobBrief{ID: j.ID, Title: j.Title, Status: j.Status, DurationSec: j.DurationSec,
				ParentID: jobParentID(j), Role: j.Role, Folder: rootOf(j).Folder, CreatedAt: j.CreatedAt,
				VocalLeak: j.VocalLeak, Mixes: j.Mixes})
		} else {
			full = append(full, j)
		}
	}
	if brief {
		return short
	}
	return full
}
