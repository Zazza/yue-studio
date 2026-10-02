package yue

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type updateReq struct {
	method, path string
	body         map[string]any
}

// updateServer — воркер, записывающий PATCH-запрос и отвечающий status/resp.
func updateServer(t *testing.T, status int, resp string, got *updateReq) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		got.body = map[string]any{}
		if len(b) > 0 {
			if err := json.Unmarshal(b, &got.body); err != nil {
				t.Errorf("body is not JSON object: %q", b)
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

func strp(s string) *string { return &s }

func TestUpdateJobTitleOnly(t *testing.T) {
	var got updateReq
	c := updateServer(t, 200, `{"id":12,"title":"Новое","status":"done","folder":"Альбом"}`, &got)
	j, err := c.UpdateJob(context.Background(), 12, strp("Новое"), nil)
	if err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if got.method != http.MethodPatch || got.path != "/jobs/12" {
		t.Fatalf("request: %s %s", got.method, got.path)
	}
	if len(got.body) != 1 || got.body["title"] != "Новое" {
		t.Fatalf("body must contain only title: %v", got.body)
	}
	if j == nil || j.ID != 12 || j.Title != "Новое" || j.Status != "done" || j.Folder != "Альбом" {
		t.Fatalf("decoded %+v", j)
	}
}

func TestUpdateJobFolderOnly(t *testing.T) {
	var got updateReq
	c := updateServer(t, 200, `{"id":5,"title":"x","folder":"Основы"}`, &got)
	j, err := c.UpdateJob(context.Background(), 5, nil, strp("Основы"))
	if err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if got.method != http.MethodPatch || got.path != "/jobs/5" {
		t.Fatalf("request: %s %s", got.method, got.path)
	}
	if len(got.body) != 1 || got.body["folder"] != "Основы" {
		t.Fatalf("body must contain only folder: %v", got.body)
	}
	if j.Folder != "Основы" {
		t.Fatalf("folder decoded %q", j.Folder)
	}
}

func TestUpdateJobEmptyFolderIsSent(t *testing.T) {
	var got updateReq
	c := updateServer(t, 200, `{"id":5,"title":"x"}`, &got)
	j, err := c.UpdateJob(context.Background(), 5, nil, strp(""))
	if err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	v, ok := got.body["folder"]
	if !ok || v != "" {
		t.Fatalf("folder \"\" must be sent as empty string: %v", got.body)
	}
	if _, has := got.body["title"]; has {
		t.Fatalf("title must be absent: %v", got.body)
	}
	if j.Folder != "" {
		t.Fatalf("folder decoded %q", j.Folder)
	}
}

func TestUpdateJobBothFields(t *testing.T) {
	var got updateReq
	c := updateServer(t, 200, `{"id":3,"title":"t","folder":"f"}`, &got)
	if _, err := c.UpdateJob(context.Background(), 3, strp("t"), strp("f")); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if len(got.body) != 2 || got.body["title"] != "t" || got.body["folder"] != "f" {
		t.Fatalf("body: %v", got.body)
	}
}

func TestUpdateJobErrorStatus(t *testing.T) {
	for _, code := range []int{422, 404} {
		var got updateReq
		c := updateServer(t, code, `{"detail":"bad"}`, &got)
		j, err := c.UpdateJob(context.Background(), 9, strp("x"), nil)
		if err == nil {
			t.Fatalf("%d: expected error, got job %+v", code, j)
		}
		var se *StatusError
		if !errors.As(err, &se) || se.Code != code {
			t.Fatalf("%d: want *StatusError with code, got %T %v", code, err, err)
		}
	}
}
