package mcp

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-own-track, этап 1 «Пресеты звука», условие 5 (MCP),
// тест-кейс ТК22: sound_presets — список; sound_preset_create / _update / _delete
// (delete только с confirm=true); sound_preset_apply (job_id, preset_id) — синхронно,
// ответ — id версии; submit с sound_preset_ids → SubmitParams.SoundPresetIDs.
// Воркер — fakeService (server_test.go) + методы пресетов.

type presetUpdate struct {
	id int64
	p  yue.SoundPreset
}

type presetFake struct {
	*fakeService
	presets []yue.SoundPreset
	created []yue.SoundPreset
	updated []presetUpdate
	delIDs  []int64
}

func (f *presetFake) SoundPresets(context.Context) ([]yue.SoundPreset, error) {
	return f.presets, nil
}

func (f *presetFake) SoundPresetCreate(_ context.Context, p yue.SoundPreset) (*yue.SoundPreset, error) {
	f.created = append(f.created, p)
	p.ID = 41
	return &p, nil
}

func (f *presetFake) SoundPresetUpdate(_ context.Context, id int64, p yue.SoundPreset) (*yue.SoundPreset, error) {
	f.updated = append(f.updated, presetUpdate{id, p})
	p.ID = id
	return &p, nil
}

func (f *presetFake) SoundPresetDelete(_ context.Context, id int64) error {
	f.delIDs = append(f.delIDs, id)
	return nil
}

func (f *presetFake) SoundPresetState(context.Context, int64, int64, yue.JobPreset) ([]yue.JobPreset, error) {
	return nil, fmt.Errorf("sound_preset_apply не пишет state (применение вручную)")
}

// FxAssets / JobStems — наборов и дорожек нет; пресеты в apply-тесте без движка и specs.
func (f *presetFake) FxAssets(context.Context) (map[string]any, error) {
	return map[string]any{"kits": []any{}}, nil
}

func (f *presetFake) JobStems(context.Context, int64) ([]map[string]any, error) { return nil, nil }

// newPresetServer — сервер со всеми инструментами (как newTestServer), воркер — presetFake.
func newPresetServer(t *testing.T) (*Server, *presetFake) {
	t.Helper()
	s, base := newTestServer(t)
	fake := &presetFake{fakeService: base, presets: []yue.SoundPreset{
		{ID: 1, Slug: "transmission", Name: "Пост-панк · Transmission", Builtin: true, ReferenceJobID: 376,
			Final: []yue.PresetStep{{Chain: "level", Params: map[string]float64{"gain": 5.5, "ceiling": -1}}}},
		{ID: 12, Name: "Мой тёплый", Note: "мягче верх",
			Final: []yue.PresetStep{{Chain: "level", Params: map[string]float64{"gain": -6, "ceiling": -0.5}}}},
	}}
	s.client = fake
	return s, fake
}

// ТК22: sound_presets отдаёт список (id и имена, встроенный — с признаком).
func TestSoundPresetsToolLists(t *testing.T) {
	s, _ := newPresetServer(t)
	out, ok := call(t, s, "sound_presets", map[string]any{})
	if !ok {
		t.Fatalf("sound_presets failed: %s", out)
	}
	for _, want := range []string{"Пост-панк · Transmission", "Мой тёплый", "12"} {
		if !strings.Contains(out, want) {
			t.Errorf("в ответе нет %q: %s", want, out)
		}
	}
}

// Условие 5: sound_preset_create передаёт имя, описание, specs и final воркеру; ответ — с id.
func TestSoundPresetCreateTool(t *testing.T) {
	s, fake := newPresetServer(t)
	out, ok := call(t, s, "sound_preset_create", jsonArgs(t, `{
		"name":"Гаражный","note":"сырой",
		"specs":[{"stems":["bass"],"chain":"eq","params":{"low":3}}],
		"final":[{"chain":"level","params":{"gain":4,"ceiling":-1}}]}`))
	if !ok {
		t.Fatalf("sound_preset_create failed: %s", out)
	}
	if len(fake.created) != 1 {
		t.Fatalf("SoundPresetCreate вызван %d раз, want 1", len(fake.created))
	}
	p := fake.created[0]
	if p.Name != "Гаражный" || p.Note != "сырой" {
		t.Errorf("name/note = %q/%q", p.Name, p.Note)
	}
	wantSpecs := []yue.PresetSpec{{Stems: []string{"bass"}, Chain: "eq", Params: map[string]float64{"low": 3}}}
	if !reflect.DeepEqual(p.Specs, wantSpecs) {
		t.Errorf("specs %+v, want %+v", p.Specs, wantSpecs)
	}
	wantFinal := []yue.PresetStep{{Chain: "level", Params: map[string]float64{"gain": 4, "ceiling": -1}}}
	if !reflect.DeepEqual(p.Final, wantFinal) {
		t.Errorf("final %+v, want %+v", p.Final, wantFinal)
	}
	if !strings.Contains(out, "41") {
		t.Errorf("в ответе нет id созданного пресета 41: %s", out)
	}
}

// Условие 5: sound_preset_update передаёт id и новые поля.
func TestSoundPresetUpdateTool(t *testing.T) {
	s, fake := newPresetServer(t)
	out, ok := call(t, s, "sound_preset_update", jsonArgs(t, `{
		"preset_id":12,"name":"Мой тёплый 2","note":"",
		"final":[{"chain":"level","params":{"gain":-3}}]}`))
	if !ok {
		t.Fatalf("sound_preset_update failed: %s", out)
	}
	if len(fake.updated) != 1 || fake.updated[0].id != 12 || fake.updated[0].p.Name != "Мой тёплый 2" {
		t.Fatalf("SoundPresetUpdate %+v, want (12, name «Мой тёплый 2»)", fake.updated)
	}
}

// ТК22: sound_preset_delete без confirm → ошибка, воркер не вызван; с confirm=true — удалён.
func TestSoundPresetDeleteRequiresConfirm(t *testing.T) {
	s, fake := newPresetServer(t)
	out, ok := call(t, s, "sound_preset_delete", map[string]any{"preset_id": 12})
	if ok {
		t.Fatalf("sound_preset_delete без confirm прошёл: %s", out)
	}
	if !strings.Contains(out, "подтверждение") {
		t.Errorf("сообщение без «подтверждение»: %s", out)
	}
	if len(fake.delIDs) != 0 {
		t.Fatalf("удалено без confirm: %v", fake.delIDs)
	}
	if out, ok := call(t, s, "sound_preset_delete", map[string]any{"preset_id": 12, "confirm": true}); !ok {
		t.Fatalf("sound_preset_delete с confirm: %s", out)
	}
	if !reflect.DeepEqual(fake.delIDs, []int64{12}) {
		t.Errorf("SoundPresetDelete %v, want [12]", fake.delIDs)
	}
}

// ТК22: submit передаёт sound_preset_ids в SubmitParams.SoundPresetIDs.
func TestSubmitPassesSoundPresetIDs(t *testing.T) {
	s, fake := newPresetServer(t)
	out, ok := call(t, s, "submit", jsonArgs(t,
		`{"style":"post-punk","lyrics":"[Instrumental]","seed":3,"sound_preset_ids":[1,12]}`))
	if !ok {
		t.Fatalf("submit failed: %s", out)
	}
	if len(fake.submitted) != 1 {
		t.Fatalf("submitted %d, want 1", len(fake.submitted))
	}
	if got := fake.submitted[0].SoundPresetIDs; !reflect.DeepEqual(got, []int64{1, 12}) {
		t.Errorf("SoundPresetIDs = %v, want [1 12]", got)
	}
}

// Без sound_preset_ids — пустой список (как раньше).
func TestSubmitWithoutSoundPresetIDs(t *testing.T) {
	s, fake := newPresetServer(t)
	if out, ok := call(t, s, "submit", map[string]any{"style": "punk", "lyrics": "[Instrumental]"}); !ok {
		t.Fatalf("submit failed: %s", out)
	}
	if got := fake.submitted[0].SoundPresetIDs; len(got) != 0 {
		t.Errorf("SoundPresetIDs = %v, want пусто", got)
	}
}

// ТК22: sound_preset_apply (job_id, preset_id) — применение сразу через приложение:
// создана версия-трек «<трек> · <пресет>», ответ — её id.
func TestSoundPresetApplyReturnsVersionID(t *testing.T) {
	needFF(t)
	s, fake := newPresetServer(t)
	fake.jobs = []yue.Job{{ID: 466, Title: "Warm Wire", Status: "done", AudioFile: "audio.flac", DurationSec: 4}}
	fake.fetch = map[string]string{"audio.flac": fxLavfi(t, "0.3*sin(2*PI*500*t)", 4)}

	out, ok := call(t, s, "sound_preset_apply", map[string]any{"job_id": 466, "preset_id": 12})
	if !ok {
		t.Fatalf("sound_preset_apply failed: %s", out)
	}
	if len(fake.promoted) != 1 {
		t.Fatalf("VariantToTrack вызван %d раз, want 1", len(fake.promoted))
	}
	pr := fake.promoted[0]
	if pr.jobID != 466 || pr.title != "Warm Wire · Мой тёплый" {
		t.Errorf("VariantToTrack(%d, %q, %q), want (466, …, «Warm Wire · Мой тёплый»)", pr.jobID, pr.file, pr.title)
	}
	if pr.file != "dsp-preset-12.flac" {
		t.Errorf("версия из файла %q, want dsp-preset-12.flac (финал пресета)", pr.file)
	}
	if len(fake.uploads["dsp-preset-12.flac"]) == 0 {
		t.Errorf("вариант финала не загружен: %v", fake.uploads)
	}
	if !strings.Contains(out, "77") { // fakeService.VariantToTrack отвечает id 77
		t.Errorf("в ответе нет id версии 77: %s", out)
	}
}

// sound_preset_apply с несуществующим пресетом — ошибка, версии нет.
func TestSoundPresetApplyUnknownPreset(t *testing.T) {
	s, fake := newPresetServer(t)
	fake.jobs = []yue.Job{{ID: 466, Title: "Warm Wire", Status: "done", AudioFile: "audio.flac"}}
	if out, ok := call(t, s, "sound_preset_apply", map[string]any{"job_id": 466, "preset_id": 999}); ok {
		t.Fatalf("sound_preset_apply с несуществующим пресетом прошёл: %s", out)
	}
	if len(fake.promoted) != 0 {
		t.Errorf("версия создана: %+v", fake.promoted)
	}
}
