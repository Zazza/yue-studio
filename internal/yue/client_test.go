package yue

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetDecodesJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`[{"id":7,"title":"demo","status":"done"}]`))
	}))
	defer srv.Close()
	c := New(srv.URL)

	jobs, err := c.Jobs(context.Background())
	if err != nil {
		t.Fatalf("Jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != 7 || jobs[0].Title != "demo" || jobs[0].Status != "done" {
		t.Fatalf("decoded %+v", jobs)
	}
}

func TestPostJSONSendsBodyAndDecodesID(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type: %q", ct)
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Write([]byte(`{"id":42}`))
	}))
	defer srv.Close()
	c := New(srv.URL)

	id, err := c.Submit(context.Background(), SubmitParams{Title: "x", Style: "blues"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if id != 42 {
		t.Fatalf("id = %d", id)
	}
	if !strings.Contains(gotBody, `"style":"blues"`) {
		t.Fatalf("body: %s", gotBody)
	}
}

func TestErrorStatusIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom-reason", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := New(srv.URL)

	_, err := c.Jobs(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "boom-reason") {
		t.Fatalf("error should carry status and body: %v", err)
	}
}

func TestPostRawSendsFilenameHeader(t *testing.T) {
	var gotName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotName = r.Header.Get("X-Filename")
		b, _ := io.ReadAll(r.Body)
		if len(b) != 5 {
			t.Errorf("body len = %d", len(b))
		}
		w.Write([]byte(`{"id":"ref-1"}`))
	}))
	defer srv.Close()
	c := New(srv.URL)

	if _, err := c.AddReference(context.Background(), "song.flac", []byte("12345")); err != nil {
		t.Fatalf("AddReference: %v", err)
	}
	if gotName != "song.flac" {
		t.Fatalf("X-Filename = %q", gotName)
	}
}

func TestDeleteJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/jobs/3" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"deleted":true}`))
	}))
	defer srv.Close()
	c := New(srv.URL)

	ok, err := c.DeleteJob(context.Background(), 3)
	if err != nil || !ok {
		t.Fatalf("DeleteJob = %v, %v", ok, err)
	}
}

func TestValidFile(t *testing.T) {
	// spec: имена артефактов плоские, известных расширений, без обхода путей
	for _, ok := range []string{"audio.flac", "audio.mp3", "audio.wav", "score.abc", "m.json", "latent.npy"} {
		if !validFile(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "../secret.flac", "a/b.flac", "..\\x.flac", "x.txt", "noext"} {
		if validFile(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestContentTypeByExt(t *testing.T) {
	cases := map[string]string{
		"a.flac": "audio/flac", "a.mp3": "audio/mpeg", "a.wav": "audio/wav",
		"a.abc": "text/plain; charset=utf-8", "a.json": "application/json",
		"a.bin": "application/octet-stream",
	}
	for f, want := range cases {
		if got := contentTypeByExt(f); got != want {
			t.Errorf("%s: %q != %q", f, got, want)
		}
	}
}

func TestSetURLSwitchesBackend(t *testing.T) {
	var hitPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitPath = r.URL.Path
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	c := New("http://127.0.0.1:1") // недоступный

	c.SetURL(srv.URL)
	if c.GetURL() != srv.URL {
		t.Fatalf("GetURL = %q", c.GetURL())
	}
	if _, err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health after SetURL: %v", err)
	}
	if hitPath != "/health" {
		t.Fatalf("hit %q", hitPath)
	}
}

func TestAudioURL(t *testing.T) {
	c := New("http://w:8091")
	if got, want := c.AudioURL(5, "audio.flac"), "http://w:8091/audio/5/audio.flac"; got != want {
		t.Fatalf("AudioURL = %q, want %q", got, want)
	}
}
