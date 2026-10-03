package yue

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// dspReq — что воркер получил на POST /jobs/{id}/dsp.
type dspReq struct {
	method, path, fname string
	query               url.Values
	body                []byte
}

func dspServer(t *testing.T, resp string, got *dspReq) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path = r.Method, r.URL.Path
		got.query = r.URL.Query()
		got.fname = r.Header.Get("X-Filename")
		got.body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

// Подпись с кириллицей, пробелами и «·» доходит до воркера как есть,
// а label из ответа попадает в DspVariant.Label.
func TestUploadDspSendsLabel(t *testing.T) {
	var got dspReq
	label := "Перегруз голоса · голос 1:20–1:28"
	c := dspServer(t, `{"file":"overdub-inst-0.flac","created_at":"2026-01-01T00:00:00",`+
		`"metrics":{},"label":"Перегруз голоса · голос 1:20–1:28"}`, &got)
	v, err := c.UploadDsp(context.Background(), 5, "overdub-inst-0.flac", label, []byte("FLAC"))
	if err != nil {
		t.Fatalf("UploadDsp: %v", err)
	}
	if got.method != http.MethodPost || got.path != "/jobs/5/dsp" {
		t.Fatalf("request: %s %s", got.method, got.path)
	}
	if got.query.Get("label") != label {
		t.Fatalf("label в запросе: %q, ждали %q", got.query.Get("label"), label)
	}
	if got.fname != "overdub-inst-0.flac" || string(got.body) != "FLAC" {
		t.Fatalf("X-Filename=%q body=%q", got.fname, got.body)
	}
	if v.Label != label || v.File != "overdub-inst-0.flac" {
		t.Fatalf("ответ: %+v", v)
	}
}

// Без подписи параметр label не отправляется вовсе.
func TestUploadDspNoLabel(t *testing.T) {
	var got dspReq
	c := dspServer(t, `{"file":"dsp-wall.flac","created_at":"","metrics":{},"label":""}`, &got)
	v, err := c.UploadDsp(context.Background(), 9, "dsp-wall.flac", "", []byte("x"))
	if err != nil {
		t.Fatalf("UploadDsp: %v", err)
	}
	if _, ok := got.query["label"]; ok {
		t.Fatalf("пустая подпись не должна уходить в запрос: %v", got.query)
	}
	if got.path != "/jobs/9/dsp" || got.fname != "dsp-wall.flac" {
		t.Fatalf("request: %s X-Filename=%q", got.path, got.fname)
	}
	if v.Label != "" {
		t.Fatalf("Label: %q", v.Label)
	}
}

// Число миксов трека из списка джоб попадает в Job.Mixes; нет поля — 0.
func TestJobsDecodeMixes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1,"status":"done","mixes":3},{"id":2,"status":"done"}]`))
	}))
	t.Cleanup(srv.Close)
	jobs, err := New(srv.URL).Jobs(context.Background())
	if err != nil {
		t.Fatalf("Jobs: %v", err)
	}
	if len(jobs) != 2 || jobs[0].Mixes != 3 || jobs[1].Mixes != 0 {
		t.Fatalf("mixes: %+v", jobs)
	}
}
