package yue

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestVoiceConvertFullParams(t *testing.T) {
	var got updateReq
	c := updateServer(t, 200, `{"id":301}`, &got)
	id, err := c.VoiceConvert(context.Background(), 214, VoiceParams{
		RefJobID: 135, RefFrom: 12.5, RefDur: 20, Steps: 60, Title: "голос 135",
	})
	if err != nil {
		t.Fatalf("VoiceConvert: %v", err)
	}
	if id != 301 {
		t.Fatalf("id %d, want 301", id)
	}
	if got.method != http.MethodPost || got.path != "/jobs/214/voice" {
		t.Fatalf("request: %s %s", got.method, got.path)
	}
	want := map[string]any{"ref_job_id": float64(135), "ref_from": 12.5, "ref_dur": float64(20), "steps": float64(60), "title": "голос 135"}
	if len(got.body) != len(want) {
		t.Fatalf("body %v, want %v", got.body, want)
	}
	for k, v := range want {
		if got.body[k] != v {
			t.Fatalf("body[%s]=%v, want %v (body %v)", k, got.body[k], v, got.body)
		}
	}
}

func TestVoiceConvertOnlyRefJobWhenZeroes(t *testing.T) {
	var got updateReq
	c := updateServer(t, 200, `{"id":7}`, &got)
	id, err := c.VoiceConvert(context.Background(), 5, VoiceParams{RefJobID: 9})
	if err != nil {
		t.Fatalf("VoiceConvert: %v", err)
	}
	if id != 7 {
		t.Fatalf("id %d", id)
	}
	if got.method != http.MethodPost || got.path != "/jobs/5/voice" {
		t.Fatalf("request: %s %s", got.method, got.path)
	}
	if len(got.body) != 1 || got.body["ref_job_id"] != float64(9) {
		t.Fatalf("body must contain only ref_job_id: %v", got.body)
	}
}

func TestVoiceConvertRefJobZeroStillSent(t *testing.T) {
	// ref_job_id — «всегда», даже нулевой.
	var got updateReq
	c := updateServer(t, 200, `{"id":1}`, &got)
	if _, err := c.VoiceConvert(context.Background(), 5, VoiceParams{}); err != nil {
		t.Fatalf("VoiceConvert: %v", err)
	}
	v, ok := got.body["ref_job_id"]
	if !ok || v != float64(0) {
		t.Fatalf("ref_job_id must always be sent: %v", got.body)
	}
	if len(got.body) != 1 {
		t.Fatalf("zero optional fields must be omitted: %v", got.body)
	}
}

func TestVoiceConvertErrorStatus(t *testing.T) {
	for _, code := range []int{503, 404, 422} {
		var got updateReq
		c := updateServer(t, code, `{"detail":"Seed-VC не установлен"}`, &got)
		id, err := c.VoiceConvert(context.Background(), 5, VoiceParams{RefJobID: 9})
		if err == nil {
			t.Fatalf("%d: expected error, got id %d", code, id)
		}
		var se *StatusError
		if !errors.As(err, &se) || se.Code != code {
			t.Fatalf("%d: want *StatusError with code, got %T %v", code, err, err)
		}
		if id != 0 {
			t.Fatalf("%d: id must be 0 on error, got %d", code, id)
		}
	}
}
