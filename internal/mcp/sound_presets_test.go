package mcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
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

// ---------- Этап 1б. ТК30: sound_preset_apply с db (условие 12) ----------
//
// db — объект {«индекс записи»: дБ −24…24} поверх записей пресета: запись с этим
// индексом уходит в пересборку с новым Db, прочие — как в пресете; неверный индекс
// или дБ вне границ — ошибка без работы (ничего не скачано, не загружено, версии нет).
//
// Db записи проверяется по звуку результата: тот же пресет без db — опора; с db {"2": 3}
// тон дорожки записи 2 громче на 3 дБ, тоны прочих дорожек — как в опоре.

// stemPresetFake — presetFake с дорожками у трека 466: vocals 3000 Гц, bass 200 Гц, other 500 Гц.
type stemPresetFake struct {
	*presetFake
}

func (f *stemPresetFake) JobStems(context.Context, int64) ([]map[string]any, error) {
	var out []map[string]any
	for _, n := range []string{"drums", "bass", "other", "vocals"} {
		out = append(out, map[string]any{"file": "stem-" + n + ".flac", "name": n})
	}
	return out, nil
}

const dbPresetID = 50

// dbPreset — три записи-эффекта (почти нейтральная полка верха) на голос, бас и «прочее».
func dbPreset() yue.SoundPreset {
	eq := func(stem string, db float64) yue.PresetSpec {
		return yue.PresetSpec{Stems: []string{stem}, Chain: "eq",
			Params: map[string]float64{"high": -1, "highf": 7000}, Db: db}
	}
	return yue.SoundPreset{ID: dbPresetID, Name: "Уровни",
		Specs: []yue.PresetSpec{eq("vocals", -2), eq("bass", 0), eq("other", 0)}}
}

func newDbPresetServer(t *testing.T) (*Server, *stemPresetFake) {
	t.Helper()
	needFF(t)
	s, pf := newPresetServer(t)
	f := &stemPresetFake{presetFake: pf}
	s.client = f
	pf.presets = append(pf.presets, dbPreset())
	const dur = 6
	pf.jobs = []yue.Job{{ID: 466, Title: "Warm Wire", Status: "done", AudioFile: "audio.flac", DurationSec: dur}}
	pf.fetch = map[string]string{
		"audio.flac":       fxLavfi(t, "0.2*sin(2*PI*3000*t)+0.2*sin(2*PI*200*t)+0.2*sin(2*PI*500*t)", dur),
		"stem-vocals.flac": fxLavfi(t, "0.2*sin(2*PI*3000*t)", dur),
		"stem-bass.flac":   fxLavfi(t, "0.2*sin(2*PI*200*t)", dur),
		"stem-other.flac":  fxLavfi(t, "0.2*sin(2*PI*500*t)", dur),
		"stem-drums.flac":  fxLavfi(t, "0", dur),
	}
	return s, f
}

// dbTones — амплитуды тонов дорожек (3000/200/500 Гц) в файле, из которого сделана версия.
func dbTones(t *testing.T, f *stemPresetFake) map[string]float64 {
	t.Helper()
	if len(f.promoted) != 1 {
		t.Fatalf("VariantToTrack вызван %d раз, want 1", len(f.promoted))
	}
	data := f.uploads[f.promoted[0].file]
	if len(data) == 0 {
		t.Fatalf("файл версии %q не загружен: %v", f.promoted[0].file, f.uploads)
	}
	s := dbDecode(t, data)
	return map[string]float64{"vocals": dbTone(s, 3000), "bass": dbTone(s, 200), "other": dbTone(s, 500)}
}

func dbDecode(t *testing.T, data []byte) []float64 {
	t.Helper()
	p := filepath.Join(t.TempDir(), "v.flac")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-i", p,
		"-f", "f32le", "-ac", "1", "-ar", "16000", "-").Output()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	s := make([]float64, len(raw)/4)
	for i := range s {
		s[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:])))
	}
	return s
}

// dbTone — амплитуда синусоиды hz на 1…5 с (проекция на sin/cos).
func dbTone(s []float64, hz float64) float64 {
	a, b := int(1*fxSR), int(5*fxSR)
	if b > len(s) {
		b = len(s)
	}
	var re, im float64
	for i := a; i < b; i++ {
		ph := 2 * math.Pi * hz * float64(i) / fxSR
		re += s[i] * math.Cos(ph)
		im += s[i] * math.Sin(ph)
	}
	n := float64(b - a)
	return 2 * math.Hypot(re, im) / n
}

func dbRatio(a, ref float64) float64 { return 20 * math.Log10(a/ref) }

// ТК30: db {"2": 3} → запись 2 («прочее», в пресете 0 дБ) уходит в пересборку с Db 3:
// 500 Гц громче опоры на 3 дБ; голос (−2 в пресете) и бас — как в опоре.
func TestSoundPresetApplyDbOverridesRecord(t *testing.T) {
	sb, base := newDbPresetServer(t)
	if out, ok := call(t, sb, "sound_preset_apply", map[string]any{"job_id": 466, "preset_id": dbPresetID}); !ok {
		t.Fatalf("опора (без db): %s", out)
	}
	ref := dbTones(t, base)

	s, f := newDbPresetServer(t)
	out, ok := call(t, s, "sound_preset_apply", jsonArgs(t, `{"job_id":466,"preset_id":50,"db":{"2":3}}`))
	if !ok {
		t.Fatalf("sound_preset_apply с db: %s", out)
	}
	got := dbTones(t, f)
	if d := dbRatio(got["other"], ref["other"]); math.Abs(d-3) > 0.4 {
		t.Errorf("«прочее» (запись 2) %+.2f дБ к опоре, want +3", d)
	}
	for _, stem := range []string{"vocals", "bass"} {
		if d := dbRatio(got[stem], ref[stem]); math.Abs(d) > 0.4 {
			t.Errorf("%s %+.2f дБ к опоре, want 0 (запись как в пресете)", stem, d)
		}
	}
	// пресет в списке не изменился: db — правка копии при применении
	for _, p := range f.presets {
		if p.ID == dbPresetID && !reflect.DeepEqual(p, dbPreset()) {
			t.Errorf("пресет изменён применением: %+v", p)
		}
	}
	if len(f.updated) != 0 || len(f.created) != 0 {
		t.Errorf("применение с db сохранило пресет: updated %v created %v", f.updated, f.created)
	}
}

// ТК30: db с неверным индексом / вне −24…24 → ошибка без работы.
func TestSoundPresetApplyDbInvalid(t *testing.T) {
	five := yue.SoundPreset{ID: 51, Name: "Пять записей"}
	for _, stem := range []string{"vocals", "bass", "other", "drums", "vocals"} {
		five.Specs = append(five.Specs, yue.PresetSpec{Stems: []string{stem}, Chain: "eq",
			Params: map[string]float64{"high": -1}})
	}
	for _, tc := range []struct{ name, args string }{
		{"индекс 9 при 5 записях", `{"job_id":466,"preset_id":51,"db":{"9":3}}`},
		{"индекс 5 при 5 записях", `{"job_id":466,"preset_id":51,"db":{"5":3}}`},
		{"индекс −1", `{"job_id":466,"preset_id":51,"db":{"-1":3}}`},
		{"индекс не число", `{"job_id":466,"preset_id":51,"db":{"x":3}}`},
		{"db 30", `{"job_id":466,"preset_id":51,"db":{"2":30}}`},
		{"db −30", `{"job_id":466,"preset_id":51,"db":{"2":-30}}`},
		{"db не число", `{"job_id":466,"preset_id":51,"db":{"2":"громче"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := newDbPresetServer(t)
			f.presets = append(f.presets, five)
			out, ok := call(t, s, "sound_preset_apply", jsonArgs(t, tc.args))
			if ok {
				t.Fatalf("want ошибку, ответ: %s", out)
			}
			if len(f.fetched) != 0 || len(f.uploads) != 0 || len(f.promoted) != 0 {
				t.Errorf("работа при ошибке: fetched %v, uploads %d, promoted %+v", f.fetched, len(f.uploads), f.promoted)
			}
		})
	}
}

// Края −24 и 24 допустимы.
func TestSoundPresetApplyDbBoundsOk(t *testing.T) {
	for _, v := range []string{"24", "-24"} {
		t.Run(v, func(t *testing.T) {
			s, f := newDbPresetServer(t)
			out, ok := call(t, s, "sound_preset_apply", jsonArgs(t, `{"job_id":466,"preset_id":50,"db":{"1":`+v+`}}`))
			if !ok {
				t.Fatalf("db %s отклонён: %s", v, out)
			}
			if len(f.promoted) != 1 {
				t.Errorf("версия не создана: %+v", f.promoted)
			}
		})
	}
}

// Ревью/кросс-ревью этапа 1б: target_lufs в create/update. Создание — с целью; update без параметра
// сохраняет прежнюю цель (правка описания не стирает громкость), явный null — снимает.
func TestSoundPresetTargetLUFSCreateUpdate(t *testing.T) {
	s, fake := newPresetServer(t)
	m13 := -13.0
	fake.presets[1].TargetLUFS = &m13
	if out, ok := call(t, s, "sound_preset_create", jsonArgs(t, `{"name":"Цель","target_lufs":-12,
		"final":[{"chain":"width","params":{"width":1.1}}]}`)); !ok {
		t.Fatalf("create: %s", out)
	}
	if got := fake.created[0].TargetLUFS; got == nil || *got != -12 {
		t.Fatalf("create: target %v, want -12", got)
	}
	if out, ok := call(t, s, "sound_preset_update", jsonArgs(t, `{"preset_id":12,"name":"Мой тёплый","note":"новое"}`)); !ok {
		t.Fatalf("update: %s", out)
	}
	if got := fake.updated[0].p.TargetLUFS; got == nil || *got != -13 {
		t.Fatalf("update без target_lufs: %v, want прежняя -13", got)
	}
	if out, ok := call(t, s, "sound_preset_update", jsonArgs(t, `{"preset_id":12,"name":"Мой тёплый","target_lufs":null}`)); !ok {
		t.Fatalf("update null: %s", out)
	}
	if got := fake.updated[1].p.TargetLUFS; got != nil {
		t.Fatalf("update с null: %v, want nil", *got)
	}
}
