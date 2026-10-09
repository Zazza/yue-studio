package yue

import (
	"context"
	"net/http"
	"testing"
)

// Карточка internal-own-track, уточнение 69б / ТК103б: тело запроса создания и правки пресета
// звука несёт master и parts как заданы; не заданы (nil) — пустые списки, а не null и не пропуск.

func presetWithMasterParts() SoundPreset {
	return SoundPreset{
		Name:   "С мастером",
		Master: []map[string]any{{"chain": "glue"}, {"chain": "limiter"}},
		Parts: []PresetPart{
			{Kind: "synth", Engine: []map[string]any{{"chain": "pad"}}, Sections: []string{"chorus"}},
			{Kind: "perc", Engine: []map[string]any{{"chain": "drums"}}, Pattern: "x...x..."},
		},
	}
}

func checkMasterParts(t *testing.T, body map[string]any) {
	t.Helper()
	master, ok := body["master"].([]any)
	if !ok || len(master) != 2 {
		t.Fatalf("master не передан как задан: %v", body["master"])
	}
	if m0, _ := master[0].(map[string]any); m0["chain"] != "glue" {
		t.Errorf("master[0] = %v, want chain glue", master[0])
	}
	parts, ok := body["parts"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("parts не переданы как заданы: %v", body["parts"])
	}
	p0, _ := parts[0].(map[string]any)
	p1, _ := parts[1].(map[string]any)
	if p0["kind"] != "synth" || p1["kind"] != "perc" || p1["pattern"] != "x...x..." {
		t.Errorf("parts искажены: %v", parts)
	}
	if secs, _ := p0["sections"].([]any); len(secs) != 1 || secs[0] != "chorus" {
		t.Errorf("parts[0].sections = %v, want [chorus]", p0["sections"])
	}
}

func checkEmptyMasterParts(t *testing.T, body map[string]any) {
	t.Helper()
	for _, k := range []string{"master", "parts"} {
		v, ok := body[k].([]any)
		if !ok || len(v) != 0 {
			t.Errorf("%s без значения — пустой список, а не null/пропуск: %v (есть=%v)", k, body[k], ok)
		}
	}
}

func TestSoundPresetCreateSendsMasterAndParts(t *testing.T) {
	var got updateReq
	c := updateServer(t, http.StatusOK, `{"id":3,"name":"С мастером"}`, &got)
	if _, err := c.SoundPresetCreate(context.Background(), presetWithMasterParts()); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.method != http.MethodPost {
		t.Fatalf("метод %s, want POST", got.method)
	}
	checkMasterParts(t, got.body)
}

func TestSoundPresetUpdateSendsMasterAndParts(t *testing.T) {
	var got updateReq
	c := updateServer(t, http.StatusOK, `{"id":7,"name":"С мастером"}`, &got)
	if _, err := c.SoundPresetUpdate(context.Background(), 7, presetWithMasterParts()); err != nil {
		t.Fatalf("update: %v", err)
	}
	checkMasterParts(t, got.body)
}

func TestSoundPresetCreateNilMasterPartsAreEmptyLists(t *testing.T) {
	var got updateReq
	c := updateServer(t, http.StatusOK, `{"id":4,"name":"Пустой"}`, &got)
	if _, err := c.SoundPresetCreate(context.Background(), SoundPreset{Name: "Пустой"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	checkEmptyMasterParts(t, got.body)
}

func TestSoundPresetUpdateNilMasterPartsAreEmptyLists(t *testing.T) {
	var got updateReq
	c := updateServer(t, http.StatusOK, `{"id":8,"name":"Пустой"}`, &got)
	if _, err := c.SoundPresetUpdate(context.Background(), 8, SoundPreset{Name: "Пустой"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	checkEmptyMasterParts(t, got.body)
}
