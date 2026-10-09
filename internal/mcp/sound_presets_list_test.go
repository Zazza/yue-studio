package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// ТК109 (карточка internal-own-track, ревью s8b, условие 74а): sound_presets без аргументов —
// краткий список [{id, slug, name, family, note, builtin}] без рецептов; с id — полный пресет
// (specs/parts/master), нет такого — ошибка. Воркер — presetFake (sound_presets_test.go).
// Предположение: ответ — JSON (массив без id, объект с id), как отдаёт рецепт sound_preset_create.

func fullPreset() yue.SoundPreset {
	lufs := -14.0
	return yue.SoundPreset{
		ID: 7, Slug: "post-punk-cold", Name: "Пост-панк · холодный", Note: "сухие барабаны, звонкий бас",
		Family: "Рок", Builtin: true, TargetLUFS: &lufs, ReferenceJobID: 466,
		Specs:  []yue.PresetSpec{{Stems: []string{"bass"}, Chain: "eq", Params: map[string]float64{"low": 3}, Db: -2}},
		Final:  []yue.PresetStep{{Chain: "level", Params: map[string]float64{"gain": 2}}},
		Master: []map[string]any{{"type": "limiter", "target_lufs": -14.0}},
		Parts:  []yue.PresetPart{{Kind: "synth", Engine: []map[string]any{{"type": "synth"}}, Style: "pad"}},
	}
}

func newListPresetServer(t *testing.T) *Server {
	t.Helper()
	s, fake := newPresetServer(t)
	fake.presets = append(fake.presets, fullPreset())
	return s
}

func TestSoundPresetsListIsShort(t *testing.T) {
	s := newListPresetServer(t)
	out, ok := call(t, s, "sound_presets", map[string]any{})
	if !ok {
		t.Fatalf("sound_presets: %s", out)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("ответ не JSON-массив: %v\n%s", err, out)
	}
	if len(items) != 3 {
		t.Fatalf("элементов %d, want 3: %s", len(items), out)
	}
	allowed := map[string]bool{"id": true, "slug": true, "name": true, "family": true, "note": true, "builtin": true}
	for _, it := range items {
		for k := range it {
			if !allowed[k] {
				t.Errorf("в кратком списке лишнее поле %q: %v", k, it)
			}
		}
		if _, ok := it["id"]; !ok {
			t.Errorf("нет id: %v", it)
		}
		if _, ok := it["name"]; !ok {
			t.Errorf("нет name: %v", it)
		}
	}
	for _, bad := range []string{`"specs"`, `"parts"`, `"master"`, `"final"`, "limiter", `"eq"`} {
		if strings.Contains(out, bad) {
			t.Errorf("в кратком списке есть рецепт (%s): %s", bad, out)
		}
	}
	// полностью заполненный встроенный — все шесть полей с его значениями
	var full map[string]any
	for _, it := range items {
		if id, _ := it["id"].(float64); id == 7 {
			full = it
		}
	}
	if full == nil {
		t.Fatalf("нет пресета 7: %s", out)
	}
	want := map[string]any{"id": 7.0, "slug": "post-punk-cold", "name": "Пост-панк · холодный",
		"family": "Рок", "note": "сухие барабаны, звонкий бас", "builtin": true}
	for k, v := range want {
		if full[k] != v {
			t.Errorf("пресет 7: %s = %v, want %v", k, full[k], v)
		}
	}
}

func TestSoundPresetsByIDIsFull(t *testing.T) {
	s := newListPresetServer(t)
	out, ok := call(t, s, "sound_presets", map[string]any{"id": 7})
	if !ok {
		t.Fatalf("sound_presets id=7: %s", out)
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("ответ не JSON-объект: %v\n%s", err, out)
	}
	if id, _ := p["id"].(float64); id != 7 {
		t.Errorf("id = %v, want 7", p["id"])
	}
	for _, k := range []string{"specs", "parts", "master"} {
		v, ok := p[k].([]any)
		if !ok || len(v) == 0 {
			t.Errorf("нет %s в полном пресете: %s", k, out)
		}
	}
	for _, want := range []string{"limiter", "pad", "bass"} {
		if !strings.Contains(out, want) {
			t.Errorf("в рецепте нет %q: %s", want, out)
		}
	}
}

func TestSoundPresetsByUnknownIDIsError(t *testing.T) {
	s := newListPresetServer(t)
	if out, ok := call(t, s, "sound_presets", map[string]any{"id": 999}); ok {
		t.Fatalf("sound_presets с несуществующим id прошёл: %s", out)
	}
}
