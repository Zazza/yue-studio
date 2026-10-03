package mcp

import (
	"context"
	"testing"

	"yue-studio/internal/yue"
)

// lyricsFake — fakeService + JobLyrics, записывающий id и язык.
type lyricsFake struct {
	*fakeService
	ids   []int64
	langs []string
}

func (f *lyricsFake) JobLyrics(ctx context.Context, id int64, language string) (*yue.LyricsResult, error) {
	f.ids = append(f.ids, id)
	f.langs = append(f.langs, language)
	return &yue.LyricsResult{Text: "la la", Seconds: 1}, nil
}

func newLyricsServer(t *testing.T) (*Server, *lyricsFake) {
	t.Helper()
	fake := &lyricsFake{fakeService: &fakeService{url: "http://w:8091"}}
	s := NewServer(fake, t.TempDir())
	RegisterWorkflowTools(s)
	RegisterStudioTools(s)
	RegisterLibraryTools(s)
	RegisterInstallTools(s)
	return s, fake
}

func TestJobLyricsPassesLanguage(t *testing.T) {
	s, fake := newLyricsServer(t)
	out, ok := call(t, s, "job_lyrics", map[string]any{"job_id": float64(44), "language": "ru"})
	if !ok {
		t.Fatalf("job_lyrics failed: %s", out)
	}
	if len(fake.ids) != 1 || fake.ids[0] != 44 || fake.langs[0] != "ru" {
		t.Fatalf("JobLyrics calls ids=%v langs=%q", fake.ids, fake.langs)
	}
}

func TestJobLyricsNoLanguageIsEmpty(t *testing.T) {
	s, fake := newLyricsServer(t)
	out, ok := call(t, s, "job_lyrics", map[string]any{"job_id": float64(45)})
	if !ok {
		t.Fatalf("job_lyrics failed: %s", out)
	}
	if len(fake.ids) != 1 || fake.ids[0] != 45 || fake.langs[0] != "" {
		t.Fatalf("JobLyrics calls ids=%v langs=%q", fake.ids, fake.langs)
	}
}
