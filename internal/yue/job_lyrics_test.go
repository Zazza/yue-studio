package yue

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type lyricsReq struct {
	method, path string
	query        url.Values
}

// lyricsServer — воркер, записывающий запрос распознавания текста.
func lyricsServer(t *testing.T, got *lyricsReq) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.query = r.Method, r.URL.Path, r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello world","seconds":1.5}`))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

func TestJobLyricsWithLanguage(t *testing.T) {
	var got lyricsReq
	c := lyricsServer(t, &got)
	res, err := c.JobLyrics(context.Background(), 17, "en")
	if err != nil {
		t.Fatalf("JobLyrics: %v", err)
	}
	if got.method != http.MethodPost || got.path != "/jobs/17/lyrics" {
		t.Fatalf("request: %s %s", got.method, got.path)
	}
	if v := got.query["language"]; len(v) != 1 || v[0] != "en" {
		t.Fatalf("language query: %v", got.query)
	}
	if res == nil || res.Text != "hello world" {
		t.Fatalf("result: %+v", res)
	}
}

func TestJobLyricsWithoutLanguageHasNoQuery(t *testing.T) {
	var got lyricsReq
	c := lyricsServer(t, &got)
	if _, err := c.JobLyrics(context.Background(), 5, ""); err != nil {
		t.Fatalf("JobLyrics: %v", err)
	}
	if got.method != http.MethodPost || got.path != "/jobs/5/lyrics" {
		t.Fatalf("request: %s %s", got.method, got.path)
	}
	if _, has := got.query["language"]; has {
		t.Fatalf("empty language must not be sent: %v", got.query)
	}
}

func TestJobLyricsLanguageEscaped(t *testing.T) {
	var got lyricsReq
	c := lyricsServer(t, &got)
	lang := "zh&x=1 ?"
	if _, err := c.JobLyrics(context.Background(), 3, lang); err != nil {
		t.Fatalf("JobLyrics: %v", err)
	}
	if v := got.query["language"]; len(v) != 1 || v[0] != lang {
		t.Fatalf("language must arrive intact: %v", got.query)
	}
	if _, injected := got.query["x"]; injected {
		t.Fatalf("language not escaped: %v", got.query)
	}
	if got.path != "/jobs/3/lyrics" {
		t.Fatalf("path %s", got.path)
	}
}
