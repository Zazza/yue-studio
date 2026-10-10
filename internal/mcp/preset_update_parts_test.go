package mcp

import (
	"reflect"
	"testing"

	"yue-studio/internal/yue"
)

// ТК112 (карточка internal-own-track, «Мелкие долги этапов 7–8», условие 76): sound_preset_update
// без parts сохраняет прежние партии пресета; с parts [] — снимает их. Проверяется тело
// SoundPresetUpdate, которое уходит воркеру (presetFake из sound_presets_test.go).
// Написаны по карточке, без чтения реализации.

func ownPartsPreset() []yue.PresetPart {
	return []yue.PresetPart{
		{Kind: "synth", Engine: []map[string]any{{"type": "synth"}}, Style: "pad"},
		{Kind: "perc", Style: "shaker"},
	}
}

func TestSoundPresetUpdateWithoutPartsKeepsOld(t *testing.T) {
	s, fake := newPresetServer(t)
	fake.presets[1].Parts = ownPartsPreset()
	out, ok := call(t, s, "sound_preset_update", jsonArgs(t, `{"preset_id":12,"name":"Мой тёплый","note":"новое"}`))
	if !ok {
		t.Fatalf("sound_preset_update: %s", out)
	}
	if len(fake.updated) != 1 || fake.updated[0].id != 12 {
		t.Fatalf("SoundPresetUpdate %+v, want один вызов для 12", fake.updated)
	}
	if got := fake.updated[0].p.Parts; !reflect.DeepEqual(got, ownPartsPreset()) {
		t.Errorf("update без parts: parts %+v, want прежние %+v", got, ownPartsPreset())
	}
}

func TestSoundPresetUpdateEmptyPartsClears(t *testing.T) {
	s, fake := newPresetServer(t)
	fake.presets[1].Parts = ownPartsPreset()
	out, ok := call(t, s, "sound_preset_update", jsonArgs(t, `{"preset_id":12,"name":"Мой тёплый","parts":[]}`))
	if !ok {
		t.Fatalf("sound_preset_update: %s", out)
	}
	if len(fake.updated) != 1 {
		t.Fatalf("SoundPresetUpdate вызван %d раз, want 1", len(fake.updated))
	}
	if got := fake.updated[0].p.Parts; len(got) != 0 {
		t.Errorf("update с parts []: parts %+v, want пусто", got)
	}
}

// Пресет без партий: update без parts — партий не появляется.
func TestSoundPresetUpdateWithoutPartsNoParts(t *testing.T) {
	s, fake := newPresetServer(t)
	out, ok := call(t, s, "sound_preset_update", jsonArgs(t, `{"preset_id":12,"name":"Мой тёплый"}`))
	if !ok {
		t.Fatalf("sound_preset_update: %s", out)
	}
	if len(fake.updated) != 1 || len(fake.updated[0].p.Parts) != 0 {
		t.Errorf("update пресета без партий: %+v, want parts пусто", fake.updated)
	}
}
