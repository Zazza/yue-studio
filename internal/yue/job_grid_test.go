package yue

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// Спецификация Client.JobGrid: POST /jobs/{id}/grid с JSON {"from_sec", "to_sec"},
// ответ {bpm, offset, strength, source} → *BeatGrid; статус ошибки воркера → error.

func TestJobGridSendsWindowAndParses(t *testing.T) {
	var got updateReq
	c := updateServer(t, 200, `{"bpm":127.5,"offset":10.206,"strength":0.83,"source":"drums"}`, &got)
	g, err := c.JobGrid(context.Background(), 42, 10, 25.5)
	if err != nil {
		t.Fatalf("JobGrid: %v", err)
	}
	if got.method != http.MethodPost || got.path != "/jobs/42/grid" {
		t.Fatalf("request: %s %s", got.method, got.path)
	}
	if got.body["from_sec"] != 10.0 || got.body["to_sec"] != 25.5 {
		t.Fatalf("body %v, want from_sec=10 to_sec=25.5", got.body)
	}
	want := BeatGrid{BPM: 127.5, Offset: 10.206, Strength: 0.83, Source: "drums"}
	if g == nil || *g != want {
		t.Fatalf("grid %+v, want %+v", g, want)
	}
}

func TestJobGridWholeTrackMix(t *testing.T) {
	var got updateReq
	c := updateServer(t, 200, `{"bpm":120,"offset":0.2,"strength":0.5,"source":"mix"}`, &got)
	g, err := c.JobGrid(context.Background(), 3, 0, 0)
	if err != nil {
		t.Fatalf("JobGrid: %v", err)
	}
	if got.path != "/jobs/3/grid" {
		t.Fatalf("path %s", got.path)
	}
	if g.Source != "mix" || g.BPM != 120 {
		t.Fatalf("grid %+v", g)
	}
}

func TestJobGridErrorStatus(t *testing.T) {
	for _, code := range []int{404, 422, 500} {
		var got updateReq
		c := updateServer(t, code, `{"detail":"bad window"}`, &got)
		g, err := c.JobGrid(context.Background(), 7, 10, 12)
		if err == nil {
			t.Fatalf("%d: expected error, got %+v", code, g)
		}
		var se *StatusError
		if !errors.As(err, &se) || se.Code != code {
			t.Fatalf("%d: want *StatusError with code, got %T %v", code, err, err)
		}
	}
}
