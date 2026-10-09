package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-own-track, этап 6, условие 47 (тест-кейс ТК77, MCP):
// rebuild_sections передаёт place и master записей; fx_apply — file и in_place;
// sound_preset_create / _update — master (и place у записей specs).
// Воркер — fxFake (fx_test.go) и presetFake (sound_presets_test.go); звук — fxLavfi.
// Порядок вызовов пересборки с мастером проверяет ТК72 (internal/studio/master_test.go);
// здесь — только что MCP доносит поля до пересборки и воркера.

const mixMasterChain = `[{"type":"glue","threshold_db":-18,"ratio":2},{"type":"limiter","target_lufs":-14}]`

func mixChain(t *testing.T, s string) []map[string]any {
	t.Helper()
	var c []map[string]any
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// ТК77: записи с place и master разбираются в SectionSpec.Place / Master; place без width — ширина 1.
func TestParseSectionSpecsPlaceMaster(t *testing.T) {
	src := `[
		{"from":0,"to":0,"stems":["other"],"place":{"pan":0.3,"width":1.4}},
		{"from":2,"to":6,"stems":["mix"],"engine":[{"type":"synth"}],"add":true,"place":{"pan":-0.5}},
		{"from":0,"to":0,"stems":[],"master":true,"engine":` + mixMasterChain + `},
		{"from":1,"to":2,"stems":["drums"],"db":-100}
	]`
	var raw any
	if err := json.Unmarshal([]byte(src), &raw); err != nil {
		t.Fatal(err)
	}
	got, err := parseSectionSpecs(raw)
	if err != nil {
		t.Fatalf("parseSectionSpecs: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("спек %d, want 4", len(got))
	}
	if p := got[0].Place; p == nil || p.Pan != 0.3 || p.Width != 1.4 {
		t.Errorf("место: Place = %+v, want {0.3 1.4}", p)
	}
	if p := got[1].Place; p == nil || p.Pan != -0.5 || p.Width != 1 || !got[1].Add {
		t.Errorf("добавление: Place = %+v Add=%v, want {−0.5 1} и Add", p, got[1].Add)
	}
	if !got[2].Master || !reflect.DeepEqual(got[2].Engine, mixChain(t, mixMasterChain)) {
		t.Errorf("мастер: Master=%v Engine=%v, want true и цепочка как есть", got[2].Master, got[2].Engine)
	}
	if got[3].Place != nil || got[3].Master {
		t.Errorf("обычная запись: Place=%+v Master=%v, want nil/false", got[3].Place, got[3].Master)
	}
}

// Условие 47: описание rebuild_sections называет поля place и master.
func TestRebuildSectionsDescribesPlaceMaster(t *testing.T) {
	s, _ := newTestServer(t)
	s.mu.RLock()
	tool := s.tools["rebuild_sections"]
	s.mu.RUnlock()
	b, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"place", "master"} {
		if !strings.Contains(tool.Description+string(b), w) {
			t.Errorf("в описании rebuild_sections нет поля %s", w)
		}
	}
}

// ТК77 сквозной: rebuild_sections с записью мастера — ApplyFx у трека (source mix, file =
// загруженный микс overdub-inst-0.flac, in_place, цепочка мастера как есть).
func TestRebuildSectionsMasterCallsWorker(t *testing.T) {
	needFF(t)
	s, fake := newFxServer(t)
	const a, b = "0.3*sin(2*PI*3000*t)", "0.3*sin(2*PI*500*t)"
	fake.fetch = map[string]string{
		"audio.flac":       fxLavfi(t, a+"+"+b, 4),
		"stem-vocals.flac": fxLavfi(t, a, 4),
		"stem-other.flac":  fxLavfi(t, b, 4),
		"stem-drums.flac":  fxLavfi(t, "0", 4),
		"stem-bass.flac":   fxLavfi(t, "0", 4),
	}
	out, ok := call(t, s, "rebuild_sections", jsonArgs(t, `{"job_id":466,"specs":[
		{"from":1,"to":2,"stems":["other"],"db":-100},
		{"from":0,"to":0,"stems":[],"master":true,"engine":`+mixMasterChain+`}]}`))
	if !ok {
		t.Fatalf("rebuild_sections с мастером: %s", out)
	}
	const mix = "overdub-inst-0.flac"
	if _, ok := fake.fakeService.uploads[mix]; !ok {
		t.Fatalf("микс %s не загружен", mix)
	}
	if len(fake.applies) != 1 {
		t.Fatalf("ApplyFx вызван %d раз, want 1 (мастер)", len(fake.applies))
	}
	r := fake.applies[0]
	if r.id != 466 || r.req.Source != "mix" || r.req.File != mix || !r.req.InPlace || r.req.Preview {
		t.Errorf("ApplyFx(%d, source=%q file=%q in_place=%v preview=%v), want 466/mix/%s/true/false",
			r.id, r.req.Source, r.req.File, r.req.InPlace, r.req.Preview, mix)
	}
	if !reflect.DeepEqual(r.req.Chain, mixChain(t, mixMasterChain)) {
		t.Errorf("цепочка мастера %v, want как в записи", r.req.Chain)
	}
}

// ТК77: fx_apply передаёт file и in_place.
func TestFxApplyPassesFileInPlace(t *testing.T) {
	s, fake := newFxServer(t)
	out, ok := call(t, s, "fx_apply", jsonArgs(t,
		`{"job_id":466,"source":"mix","file":"overdub-inst-0.flac","in_place":true,"chain":`+mixMasterChain+`}`))
	if !ok {
		t.Fatalf("fx_apply failed: %s", out)
	}
	if len(fake.applies) != 1 {
		t.Fatalf("ApplyFx вызван %d раз", len(fake.applies))
	}
	r := fake.applies[0].req
	if r.File != "overdub-inst-0.flac" || !r.InPlace || r.Source != "mix" {
		t.Errorf("запрос file=%q in_place=%v source=%q, want overdub-inst-0.flac/true/mix", r.File, r.InPlace, r.Source)
	}
}

// fx_apply без file/in_place — в запрос они не придумываются.
func TestFxApplyNoFileByDefault(t *testing.T) {
	s, fake := newFxServer(t)
	if out, ok := call(t, s, "fx_apply", jsonArgs(t, `{"job_id":466,"source":"mix","chain":[{"type":"comp"}]}`)); !ok {
		t.Fatalf("fx_apply failed: %s", out)
	}
	if r := fake.applies[0].req; r.File != "" || r.InPlace {
		t.Errorf("file=%q in_place=%v придуманы", r.File, r.InPlace)
	}
}

// ТК77: sound_preset_create передаёт master и place у записей specs.
func TestSoundPresetCreatePassesMasterPlace(t *testing.T) {
	s, fake := newPresetServer(t)
	out, ok := call(t, s, "sound_preset_create", jsonArgs(t, `{
		"name":"Сведение","note":"",
		"specs":[{"stems":["other"],"place":{"pan":0.3,"width":1.4},"db":0}],
		"final":[],
		"master":`+mixMasterChain+`}`))
	if !ok {
		t.Fatalf("sound_preset_create failed: %s", out)
	}
	if len(fake.created) != 1 {
		t.Fatalf("SoundPresetCreate вызван %d раз, want 1", len(fake.created))
	}
	p := fake.created[0]
	if !reflect.DeepEqual(p.Master, mixChain(t, mixMasterChain)) {
		t.Errorf("master %v, want как в запросе", p.Master)
	}
	if len(p.Specs) != 1 || p.Specs[0].Place == nil || *p.Specs[0].Place != (yue.Place{Pan: 0.3, Width: 1.4}) {
		t.Errorf("specs %+v, want запись с place {0.3 1.4}", p.Specs)
	}
}

// Условие 47: sound_preset_update тоже передаёт master.
func TestSoundPresetUpdatePassesMaster(t *testing.T) {
	s, fake := newPresetServer(t)
	out, ok := call(t, s, "sound_preset_update", jsonArgs(t, `{
		"preset_id":12,"name":"Мой тёплый","note":"",
		"final":[{"chain":"width","params":{"width":1.1}}],
		"master":[{"type":"limiter","target_lufs":-11}]}`))
	if !ok {
		t.Fatalf("sound_preset_update failed: %s", out)
	}
	if len(fake.updated) != 1 {
		t.Fatalf("SoundPresetUpdate вызван %d раз", len(fake.updated))
	}
	want := mixChain(t, `[{"type":"limiter","target_lufs":-11}]`)
	if !reflect.DeepEqual(fake.updated[0].p.Master, want) {
		t.Errorf("master %v, want %v", fake.updated[0].p.Master, want)
	}
}
