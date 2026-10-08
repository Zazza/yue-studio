package yue

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// Карточка internal-own-track, этап 1, условие 5: клиент пресетов звука — пути, методы, тело
// запроса (без id/slug/builtin) и 409 захвата статуса как *StatusError.

func TestSoundPresetUpdateSendsPutWithoutIdentity(t *testing.T) {
	var got updateReq
	c := updateServer(t, http.StatusOK, `{"id":7,"name":"Мой"}`, &got)
	p := SoundPreset{ID: 99, Slug: "x", Builtin: true, Name: "Мой", Final: []PresetStep{{Chain: "level"}}}
	out, err := c.SoundPresetUpdate(context.Background(), 7, p)
	if err != nil || out.ID != 7 {
		t.Fatalf("update: %v %+v", err, out)
	}
	if got.method != http.MethodPut || got.path != "/sound-presets/7" {
		t.Fatalf("%s %s, want PUT /sound-presets/7", got.method, got.path)
	}
	for _, k := range []string{"id", "slug", "builtin"} {
		if _, ok := got.body[k]; ok {
			t.Errorf("в теле лишнее поле %s: %v", k, got.body)
		}
	}
	if specs, ok := got.body["specs"].([]any); !ok || len(specs) != 0 {
		t.Errorf("specs — пустой список, а не null: %v", got.body["specs"])
	}
}

func TestSoundPresetStateConflictIsStatusError(t *testing.T) {
	var got updateReq
	c := updateServer(t, http.StatusConflict, `{"detail":"running → running нельзя"}`, &got)
	_, err := c.SoundPresetState(context.Background(), 5, 2, JobPreset{Status: "running"})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusConflict {
		t.Fatalf("want *StatusError 409, got %v", err)
	}
	if got.path != "/jobs/5/sound-presets/2/state" || got.body["status"] != "running" {
		t.Fatalf("запрос %s %v", got.path, got.body)
	}
	if _, ok := got.body["child_id"]; ok {
		t.Errorf("child_id без значения не отправляется: %v", got.body)
	}
}
