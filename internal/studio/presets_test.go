package studio

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-own-track, этап 1 «Пресеты звука», условия 3 и 4,
// тест-кейсы ТК18–ТК21 (+ ТК20а — наборы сэмплов).
//
// ApplySoundPreset(ctx, svc, jobID, preset) → {ChildID, File}:
//   (а) дорожки из specs должны быть у трека; нет — разделение (части барабанов —
//       MakeStemsWith(id, "roformer"), иначе MakeStems); всё ещё нет — ошибка с именами;
//   (а') наборы сэмплов движка «<набор>/<часть>», которых нет в FxAssets().kits, —
//       InstallFxKit(<набор>) один раз на набор;
//   (б) specs → RebuildSections окном «весь трек» → файл F; specs пусты → F = звук трека;
//   (в) final (без off) → граф на F → вариант dsp-preset-<id>.flac с подписью-именем → F2;
//   (г) VariantToTrack(jobID, F2, "<название трека> · <имя пресета>") → ChildID.
//
// Воркер — psFake: engFake (engine_test.go: secFake + ApplyFx + WorkerConfig) и
// недостающие внешние границы (список джоб, стемы, разделение, наборы, версия-трек).
// Загруженный вариант фейк хранит у джобы, как воркер: его можно скачать.
//
// Звук трека (16 кГц моно, 10 с): vocals — 3000 Гц, other — 500 Гц, drums/bass — тишина.
// Движок фейка отдаёт «обработанный кусок» 1000/1500 Гц (engine_test.go), поэтому
// 1500 Гц в итоге — след пересборки, которого в исходном звуке нет.

const (
	psJobID   int64 = parentID
	psTitle         = "Тёплый провод"
	psChild   int64 = 900 // id версии, которую «создаёт» VariantToTrack
	psDur           = engDur
	exprPs3k        = "0.3*sin(2*PI*3000*t)"
	exprPs500       = "0.3*sin(2*PI*500*t)"
)

type psPromote struct {
	jobID       int64
	file, title string
}

// psFake — фейковый воркер для ApplySoundPreset. Только внешние границы.
type psFake struct {
	*engFake
	jobs []yue.Job
	log  []string // порядок значимых вызовов: split:<model>, install:<kit>, applyfx:<source>, upload:<file>, track:<file>

	afterRoformer map[string]string // дорожки, которые появляются после MakeStemsWith(id, "roformer")
	afterDefault  map[string]string // … после MakeStems(id) / MakeStemsWith(id, "")
	splits        []string          // модели разделения по порядку ("" — по настройке)

	kits     []any // FxAssets()["kits"]: [{name, samples}]
	installs []string

	promoted   []psPromote
	promoteErr error
	deleted    []string // любые удаления (их быть не должно)

	// этап 1б (условие 11): замеры громкости, которые отдаёт воркер
	uploadMetrics map[string]any // Metrics варианта из UploadDsp (nil — замера нет)
	analyze       map[string]any // ответ AnalyzeJob (nil — замера нет)
	analyzed      []int64        // у каких джоб звали AnalyzeJob
}

func (f *psFake) Jobs(context.Context) ([]yue.Job, error) { return f.jobs, nil }

func (f *psFake) JobStems(_ context.Context, id int64) ([]map[string]any, error) {
	var out []map[string]any
	prefix := fmt.Sprintf("%d/stem-", id)
	for k := range f.files {
		if strings.HasPrefix(k, prefix) && strings.HasSuffix(k, ".flac") {
			file := strings.TrimPrefix(k, fmt.Sprintf("%d/", id))
			out = append(out, map[string]any{"file": file,
				"name": strings.TrimSuffix(strings.TrimPrefix(file, "stem-"), ".flac")})
		}
	}
	return out, nil
}

func (f *psFake) split(id int64, model string) {
	f.splits = append(f.splits, model)
	f.log = append(f.log, "split:"+model)
	add := f.afterDefault
	if model == "roformer" {
		add = f.afterRoformer
	}
	for name, p := range add {
		f.files[key(id, name)] = p
	}
}

func (f *psFake) MakeStems(_ context.Context, id int64) (map[string]any, error) {
	f.split(id, "")
	return map[string]any{"ok": true}, nil
}

func (f *psFake) MakeStemsWith(_ context.Context, id int64, model string) (map[string]any, error) {
	f.split(id, model)
	return map[string]any{"ok": true}, nil
}

func (f *psFake) FxAssets(context.Context) (map[string]any, error) {
	return map[string]any{"kits": f.kits, "captures": []any{}, "irs": []any{}}, nil
}

func (f *psFake) InstallFxKit(_ context.Context, name string) (map[string]any, error) {
	f.installs = append(f.installs, name)
	f.log = append(f.log, "install:"+name)
	return map[string]any{"name": name, "downloaded": true}, nil
}

func (f *psFake) ApplyFx(ctx context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	f.log = append(f.log, "applyfx:"+req.Source)
	return f.engFake.ApplyFx(ctx, id, req)
}

// UploadDsp — как воркер: вариант сохраняется у джобы и дальше скачивается по имени.
func (f *psFake) UploadDsp(ctx context.Context, id int64, fname, label string, data []byte) (*yue.DspVariant, error) {
	f.log = append(f.log, "upload:"+fname)
	p := filepath.Join(f.dir, fmt.Sprintf("up-%d-%s", id, fname))
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return nil, err
	}
	f.files[key(id, fname)] = p
	v, err := f.secFake.UploadDsp(ctx, id, fname, label, data)
	if v != nil && f.uploadMetrics != nil {
		v.Metrics = f.uploadMetrics
	}
	return v, err
}

// AnalyzeJob — метрики трека (этап 1б: LUFS трека, когда правок дорожек нет).
func (f *psFake) AnalyzeJob(_ context.Context, id int64) (map[string]any, error) {
	f.analyzed = append(f.analyzed, id)
	f.log = append(f.log, fmt.Sprintf("analyze:%d", id))
	return f.analyze, nil
}

func (f *psFake) VariantToTrack(_ context.Context, jobID int64, file, title string, _ int64) (int64, error) {
	f.log = append(f.log, "track:"+file)
	f.promoted = append(f.promoted, psPromote{jobID, file, title})
	if f.promoteErr != nil {
		return 0, f.promoteErr
	}
	return psChild, nil
}

func (f *psFake) DeleteJob(_ context.Context, id int64) (bool, error) {
	f.deleted = append(f.deleted, fmt.Sprintf("job %d", id))
	return true, nil
}

func (f *psFake) DspVariantDelete(_ context.Context, id int64, fname string) (bool, error) {
	f.deleted = append(f.deleted, key(id, fname))
	return true, nil
}

// psSetup — трек psJobID с основными дорожками и extra (имя → выражение aevalsrc).
func psSetup(t *testing.T, extra map[string]string) *psFake {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	p := func(n string) string { return filepath.Join(dir, n) }
	files := map[string]string{
		"audio.flac":       lavfi(t, aeval(exprPs3k+"+"+exprPs500, psDur), p("ps-audio.flac")),
		"stem-vocals.flac": lavfi(t, aeval(exprPs3k, psDur), p("ps-vocals.flac")),
		"stem-other.flac":  lavfi(t, aeval(exprPs500, psDur), p("ps-other.flac")),
		"stem-drums.flac":  lavfi(t, aeval("0", psDur), p("ps-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", psDur), p("ps-bass.flac")),
	}
	for name, expr := range extra {
		files["stem-"+name+".flac"] = lavfi(t, aeval(expr, psDur), p("ps-x-"+name+".flac"))
	}
	sf := newSecFake()
	put(sf, psJobID, files)
	return &psFake{
		engFake: newEngFake(t, sf),
		jobs: []yue.Job{{ID: psJobID, Title: psTitle, Status: "done", AudioFile: "audio.flac",
			DurationSec: psDur}},
		kits: []any{
			map[string]any{"name": "osdk/kick", "samples": 3.0},
			map[string]any{"name": "osdk/snare", "samples": 3.0},
			map[string]any{"name": "growlybass/bass", "samples": 12.0},
		},
	}
}

// psStemFile — файл дорожки, который появится после разделения.
func psStemFile(t *testing.T, name, expr string) string {
	t.Helper()
	return lavfi(t, aeval(expr, psDur), filepath.Join(t.TempDir(), "split-"+name+".flac"))
}

// --- пресеты тестов ---

func psAmpEngine() []map[string]any {
	return []map[string]any{{"type": "amp", "model": "JCM2000.nam", "input_db": -6.0}}
}

// psFinal — финал из карточки (Transmission): ширина → громкость.
func psFinal() []yue.PresetStep {
	return []yue.PresetStep{
		{Chain: "width", Params: map[string]float64{"width": 1.1, "bass": 120}},
		{Chain: "level", Params: map[string]float64{"gain": 5.5, "ceiling": -1}},
	}
}

// psVocalCut — запись-эффект на голос: полка верха −12 дБ от 2 кГц (3000 Гц голоса тише).
func psVocalCut() yue.PresetSpec {
	return yue.PresetSpec{Stems: []string{"vocals"}, Chain: "eq",
		Params: map[string]float64{"high": -12, "highf": 2000}}
}

func psPreset(id int64, specs []yue.PresetSpec, final []yue.PresetStep) yue.SoundPreset {
	return yue.SoundPreset{ID: id, Name: "Пост-панк", Specs: specs, Final: final}
}

func psApply(t *testing.T, f *psFake, p yue.SoundPreset) *PresetResult {
	t.Helper()
	res, err := ApplySoundPreset(context.Background(), f, psJobID, p)
	if err != nil {
		t.Fatalf("ApplySoundPreset: %v", err)
	}
	if res == nil {
		t.Fatal("ApplySoundPreset: nil результат без ошибки")
	}
	return res
}

func psIndex(log []string, item string) int {
	for i, v := range log {
		if v == item {
			return i
		}
	}
	return -1
}

func psCount(log []string, prefix string) int {
	n := 0
	for _, v := range log {
		if strings.HasPrefix(v, prefix) {
			n++
		}
	}
	return n
}

// psTrackTitle — название версии по условию 3 (г).
func psTrackTitle(p yue.SoundPreset) string { return psTitle + " · " + p.Name }

// ---------- ТК18: specs + final, стемы есть ----------

// ТК18: движок на бас и эффект на голос + финал (ширина, громкость). Пересборка
// всеми записями окном «весь трек», финал — на РЕЗУЛЬТАТЕ пересборки (а не на
// исходном звуке), вариант dsp-preset-<id>.flac с подписью-именем пресета,
// версия-трек из него с названием «<трек> · <пресет>»; ChildID — id версии.
func TestApplySoundPresetSpecsAndFinal(t *testing.T) {
	f := psSetup(t, nil)
	p := psPreset(5, []yue.PresetSpec{
		{Stems: []string{"bass"}, Engine: psAmpEngine()},
		psVocalCut(),
	}, psFinal())

	res := psApply(t, f, p)

	// стемы были — разделения нет
	if len(f.splits) != 0 {
		t.Errorf("разделение %v, want нет (все дорожки у трека есть)", f.splits)
	}

	// (б) движок на бас: один запрос у трека, source bass, цепочка как в пресете, окно — весь трек
	if len(f.calls) != 1 {
		t.Fatalf("ApplyFx вызван %d раз, want 1 (движок на бас)", len(f.calls))
	}
	c := f.calls[0]
	if c.id != psJobID || c.req.Source != "bass" {
		t.Errorf("ApplyFx у #%d source=%q, want #%d bass", c.id, c.req.Source, psJobID)
	}
	if !reflect.DeepEqual(c.req.Chain, psAmpEngine()) {
		t.Errorf("цепочка движка %v, want как в пресете %v", c.req.Chain, psAmpEngine())
	}
	if c.req.From == nil || *c.req.From > engFade+1e-9 {
		t.Errorf("окно движка начинается с %v, want от начала трека (запись «весь трек»)", deref(c.req.From))
	}
	if c.req.To == nil || *c.req.To < psDur-0.01 {
		t.Errorf("окно движка до %v, want до конца трека %.0f с", deref(c.req.To), psDur)
	}

	// результат пересборки — у трека своим файлом (не «микс с правками» студии overdub-inst-0 — ревью s1),
	// затем вариант финала, затем версия
	rebuilt := fmt.Sprintf("dsp-preset-%d-mix.flac", p.ID)
	if _, ok := f.uploads["overdub-inst-0.flac"]; ok {
		t.Errorf("пресет перезаписал микс с правками студии overdub-inst-0.flac")
	}
	final := fmt.Sprintf("dsp-preset-%d.flac", p.ID)
	iRebuilt, iFinal, iTrack := psIndex(f.log, "upload:"+rebuilt), psIndex(f.log, "upload:"+final), psIndex(f.log, "track:"+final)
	if iRebuilt < 0 || iFinal < 0 || iTrack < 0 || iRebuilt >= iFinal || iFinal >= iTrack {
		t.Fatalf("порядок вызовов %v, want upload:%s → upload:%s → track:%s", f.log, rebuilt, final, final)
	}
	if got := f.labels[final]; got != p.Name {
		t.Errorf("подпись варианта %q, want имя пресета %q", got, p.Name)
	}

	// финал применён к результату пересборки: в варианте слышны и движок (1500 Гц,
	// которого нет в исходном звуке), и эффект на голос (3000 Гц тише 500 Гц)
	out := decodeBytes(t, f.uploads[final])
	if len(out) == 0 {
		t.Fatal("вариант финала пустой")
	}
	if a := toneAmp(out, 1500, 2, 8); a < 0.05 {
		t.Errorf("1500 Гц (движок пересборки) в варианте финала %.4f, want ≥ 0.05: финал взят не с результата пересборки", a)
	}
	a3k, a500 := toneAmp(out, 3000, 2, 8), toneAmp(out, 500, 2, 8)
	if a500 < 0.05 || a3k/a500 > 0.6 {
		t.Errorf("3000/500 Гц = %.4f/%.4f, want голос ниже «прочего» (эффект на голос из пересборки)", a3k, a500)
	}

	// (г) версия-трек
	if len(f.promoted) != 1 {
		t.Fatalf("VariantToTrack вызван %d раз, want 1", len(f.promoted))
	}
	pr := f.promoted[0]
	if pr.jobID != psJobID || pr.file != final || pr.title != psTrackTitle(p) {
		t.Errorf("VariantToTrack(%d, %q, %q), want (%d, %q, %q)", pr.jobID, pr.file, pr.title,
			psJobID, final, psTrackTitle(p))
	}
	if res.ChildID != psChild || res.File != final {
		t.Errorf("результат %+v, want ChildID %d File %s", *res, psChild, final)
	}
	if len(f.deleted) != 0 {
		t.Errorf("удалено %v, want ничего", f.deleted)
	}
}

// ---------- ТК19: дорожек не хватает ----------

// ТК19: нет kick (часть барабанов) → MakeStemsWith(id, "roformer"), после
// разделения kick есть → применение продолжается.
func TestApplySoundPresetSplitsRoformerForDrumParts(t *testing.T) {
	f := psSetup(t, nil)
	f.afterRoformer = map[string]string{"stem-kick.flac": psStemFile(t, "kick", "0.5*lt(mod(t\\,0.5)\\,0.02)")}
	p := psPreset(6, []yue.PresetSpec{{Stems: []string{"kick"},
		Engine: []map[string]any{{"type": "sampler", "kit": "osdk/kick", "output_db": 0.0}}}}, nil)

	psApply(t, f, p)

	if !reflect.DeepEqual(f.splits, []string{"roformer"}) {
		t.Errorf("разделения %q, want ровно одно roformer (не хватает части барабанов)", f.splits)
	}
	if len(f.promoted) != 1 {
		t.Errorf("VariantToTrack вызван %d раз, want 1 (после разделения kick есть — продолжение)", len(f.promoted))
	}
}

// ТК19: нет только guitar (не часть барабанов) → MakeStems (разделение по настройке, не roformer).
func TestApplySoundPresetSplitsDefaultForGuitar(t *testing.T) {
	f := psSetup(t, nil)
	f.afterDefault = map[string]string{"stem-guitar.flac": psStemFile(t, "guitar", "0.2*sin(2*PI*700*t)")}
	f.afterRoformer = f.afterDefault
	p := psPreset(7, []yue.PresetSpec{{Stems: []string{"guitar"}, Chain: "eq",
		Params: map[string]float64{"mid": 3, "midf": 1200}}}, nil)

	psApply(t, f, p)

	if !reflect.DeepEqual(f.splits, []string{""}) {
		t.Errorf("разделения %q, want ровно одно MakeStems (по настройке, не roformer)", f.splits)
	}
	if len(f.promoted) != 1 {
		t.Errorf("VariantToTrack вызван %d раз, want 1", len(f.promoted))
	}
}

// ТК19: после разделения kick и snare так и нет → ошибка с именами дорожек,
// пересборки нет (ни движка, ни загрузок), версии нет.
func TestApplySoundPresetMissingStemsAfterSplit(t *testing.T) {
	f := psSetup(t, nil)
	p := psPreset(8, []yue.PresetSpec{
		{Stems: []string{"kick"}, Engine: []map[string]any{{"type": "sampler", "kit": "osdk/kick"}}},
		{Stems: []string{"snare"}, Engine: []map[string]any{{"type": "sampler", "kit": "osdk/snare"}}},
	}, psFinal())

	_, err := ApplySoundPreset(context.Background(), f, psJobID, p)
	if err == nil {
		t.Fatal("ApplySoundPreset без kick/snare после разделения: нет ошибки")
	}
	msg := err.Error()
	for _, want := range []string{"kick", "snare"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ошибка %q без имени дорожки %q", msg, want)
		}
	}
	if !strings.Contains(strings.ToLower(msg), "roformer") {
		t.Errorf("ошибка %q без подсказки про разделение RoFormer", msg)
	}
	if len(f.calls) != 0 || len(f.uploads) != 0 {
		t.Errorf("пересборка шла: ApplyFx %d, загрузки %v; want ничего", len(f.calls), keys(f.uploads))
	}
	if len(f.promoted) != 0 || len(f.deleted) != 0 {
		t.Errorf("версия %v / удаления %v, want нет", f.promoted, f.deleted)
	}
}

// ---------- ТК20: только final / только specs / final весь off ----------

// ТК20: specs пусты → финал на исходном звуке трека, пересборки нет; вариант и версия из него.
func TestApplySoundPresetFinalOnly(t *testing.T) {
	f := psSetup(t, nil)
	p := psPreset(9, nil, []yue.PresetStep{{Chain: "level", Params: map[string]float64{"gain": -6, "ceiling": -0.5}}})

	res := psApply(t, f, p)

	final := fmt.Sprintf("dsp-preset-%d.flac", p.ID)
	if len(f.calls) != 0 || psCount(f.log, "upload:overdub-inst-") != 0 {
		t.Errorf("пересборка шла (ApplyFx %d, лог %v), want нет: specs пусты", len(f.calls), f.log)
	}
	if !contains(f.fetched, key(psJobID, "audio.flac")) {
		t.Errorf("звук трека не скачан: %v", f.fetched)
	}
	data := f.uploads[final]
	if len(data) == 0 {
		t.Fatalf("вариант %s не загружен: %v", final, keys(f.uploads))
	}
	out := decodeBytes(t, data)
	// исходный звук −6 дБ: 500 Гц ≈ 0.3·0.5, 3000 Гц тоже на месте
	if a := toneAmp(out, 500, 2, 8); !near(a, 0.15, 0.03) {
		t.Errorf("500 Гц в варианте %.4f, want ≈ 0.15 (исходный звук −6 дБ)", a)
	}
	if a := toneAmp(out, 3000, 2, 8); !near(a, 0.15, 0.03) {
		t.Errorf("3000 Гц в варианте %.4f, want ≈ 0.15 (исходный звук −6 дБ)", a)
	}
	if len(f.promoted) != 1 || f.promoted[0].file != final || f.promoted[0].title != psTrackTitle(p) {
		t.Errorf("VariantToTrack %+v, want один вызов (%s, %q)", f.promoted, final, psTrackTitle(p))
	}
	if res.ChildID != psChild || res.File != final {
		t.Errorf("результат %+v, want ChildID %d File %s", *res, psChild, final)
	}
}

// ТК20: final пуст → без варианта финала, версия прямо из файла пересборки.
// Final из одних off — то же самое.
func TestApplySoundPresetSpecsOnly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		final []yue.PresetStep
	}{
		{"без финала", nil},
		{"финал весь выключен", []yue.PresetStep{
			{Chain: "width", Params: map[string]float64{"width": 1.1}, Off: true},
			{Chain: "level", Params: map[string]float64{"gain": 5}, Off: true},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := psSetup(t, nil)
			p := psPreset(10, []yue.PresetSpec{psVocalCut()}, tc.final)

			res := psApply(t, f, p)

			rebuilt := fmt.Sprintf("dsp-preset-%d-mix.flac", p.ID)
			if _, ok := f.uploads[rebuilt]; !ok {
				t.Fatalf("пересборка не загружена: %v", keys(f.uploads))
			}
			for name := range f.uploads {
				if name == fmt.Sprintf("dsp-preset-%d.flac", p.ID) {
					t.Errorf("загружен вариант финала %s, want нет (финала нет)", name)
				}
			}
			if len(f.promoted) != 1 || f.promoted[0].file != rebuilt || f.promoted[0].title != psTrackTitle(p) {
				t.Errorf("VariantToTrack %+v, want один вызов (%s, %q)", f.promoted, rebuilt, psTrackTitle(p))
			}
			if res.ChildID != psChild || res.File != rebuilt {
				t.Errorf("результат %+v, want ChildID %d File %s", *res, psChild, rebuilt)
			}
		})
	}
}

// ---------- ТК20а: наборы сэмплов движка ----------

// ТК20а: kits = [osdk/kick]; sampler osdk/kick и bass growlybass/bass (+ ещё запись
// с тем же набором growlybass) → InstallFxKit("growlybass") ровно один раз, до
// работы движка; osdk не ставится.
func TestApplySoundPresetInstallsMissingKits(t *testing.T) {
	f := psSetup(t, map[string]string{"kick": "0.5*lt(mod(t\\,0.5)\\,0.02)"})
	f.kits = []any{map[string]any{"name": "osdk/kick", "samples": 3.0}}
	p := psPreset(11, []yue.PresetSpec{
		{Stems: []string{"kick"}, Engine: []map[string]any{{"type": "sampler", "kit": "osdk/kick", "output_db": 0.0}}},
		{Stems: []string{"bass"}, Engine: []map[string]any{
			{"type": "bass", "kit": "growlybass/bass", "division": 2.0, "output_db": -6.0},
			{"type": "comp"},
		}},
		{Stems: []string{"other"}, Engine: []map[string]any{{"type": "bass", "kit": "growlybass/bass"}}},
	}, nil)

	psApply(t, f, p)

	if !reflect.DeepEqual(f.installs, []string{"growlybass"}) {
		t.Errorf("InstallFxKit %q, want ровно [growlybass] (osdk/kick уже есть)", f.installs)
	}
	if i, j := psIndex(f.log, "install:growlybass"), psIndex(f.log, "applyfx:bass"); i < 0 || j < 0 || i > j {
		t.Errorf("порядок %v, want набор поставлен до движка на бас", f.log)
	}
}

// ТК20а: все наборы есть (в т.ч. kit_open) → InstallFxKit не вызывается.
func TestApplySoundPresetKitsPresentNoInstall(t *testing.T) {
	f := psSetup(t, map[string]string{"snare": "0.5*lt(mod(t\\,0.5)\\,0.02)"})
	f.kits = append(f.kits, map[string]any{"name": "osdk/hh_open", "samples": 2.0})
	p := psPreset(12, []yue.PresetSpec{
		{Stems: []string{"snare"}, Engine: []map[string]any{{"type": "sampler", "kit": "osdk/snare",
			"kit_open": "osdk/hh_open", "output_db": 2.5}}},
		{Stems: []string{"bass"}, Engine: []map[string]any{{"type": "bass", "kit": "growlybass/bass"}}},
	}, nil)

	psApply(t, f, p)

	if len(f.installs) != 0 {
		t.Errorf("InstallFxKit %q, want ни одного (все наборы есть)", f.installs)
	}
}

// ---------- условие 3: ошибка шага — ошибка функции, ничего не удаляется ----------

func TestApplySoundPresetStepErrors(t *testing.T) {
	t.Run("движок упал", func(t *testing.T) {
		f := psSetup(t, nil)
		f.err = errors.New("fx engine off")
		p := psPreset(13, []yue.PresetSpec{{Stems: []string{"bass"}, Engine: psAmpEngine()}}, psFinal())
		_, err := ApplySoundPreset(context.Background(), f, psJobID, p)
		if err == nil || !strings.Contains(err.Error(), "fx engine off") {
			t.Errorf("ошибка %v, want с причиной «fx engine off»", err)
		}
		if len(f.promoted) != 0 || len(f.deleted) != 0 {
			t.Errorf("версия %v / удаления %v, want нет", f.promoted, f.deleted)
		}
	})
	t.Run("версия не создалась", func(t *testing.T) {
		f := psSetup(t, nil)
		f.promoteErr = errors.New("promote refused")
		p := psPreset(14, nil, psFinal())
		_, err := ApplySoundPreset(context.Background(), f, psJobID, p)
		if err == nil || !strings.Contains(err.Error(), "promote refused") {
			t.Errorf("ошибка %v, want с причиной «promote refused»", err)
		}
		if len(f.deleted) != 0 {
			t.Errorf("удаления %v, want ничего", f.deleted)
		}
	})
}

// ---------- ТК21: PresetRunner.Tick ----------

type rnState struct {
	jobID, presetID int64
	st              yue.JobPreset
}

type rnApply struct {
	jobID  int64
	preset yue.SoundPreset
}

// rnFake — воркер для раннера: список джоб, пресетов и смена состояния пресета у джобы
// (как воркер: меняет статус в списке джобы; conflict — ответ 409).
type rnFake struct {
	yue.Service
	jobs       []yue.Job
	presets    []yue.SoundPreset
	jobsErr    error
	presetsErr error
	conflict   map[[2]int64]bool
	states     []rnState
}

func (f *rnFake) Jobs(context.Context) ([]yue.Job, error) {
	if f.jobsErr != nil {
		return nil, f.jobsErr
	}
	return f.jobs, nil
}

func (f *rnFake) SoundPresets(context.Context) ([]yue.SoundPreset, error) {
	if f.presetsErr != nil {
		return nil, f.presetsErr
	}
	return f.presets, nil
}

func (f *rnFake) SoundPresetState(_ context.Context, jobID, presetID int64, st yue.JobPreset) ([]yue.JobPreset, error) {
	f.states = append(f.states, rnState{jobID, presetID, st})
	if f.conflict[[2]int64{jobID, presetID}] {
		return nil, &yue.StatusError{Code: 409, Msg: "409 conflict"}
	}
	for i := range f.jobs {
		if f.jobs[i].ID != jobID {
			continue
		}
		for k := range f.jobs[i].SoundPresets {
			if f.jobs[i].SoundPresets[k].ID == presetID {
				sp := &f.jobs[i].SoundPresets[k]
				sp.Status, sp.ChildID, sp.Error = st.Status, st.ChildID, st.Error
			}
		}
		return f.jobs[i].SoundPresets, nil
	}
	return nil, &yue.StatusError{Code: 404, Msg: "404 not found"}
}

func rnPending(ids ...int64) []yue.JobPreset {
	var out []yue.JobPreset
	for _, id := range ids {
		out = append(out, yue.JobPreset{ID: id, Status: "pending"})
	}
	return out
}

func rnPresets(ids ...int64) []yue.SoundPreset {
	var out []yue.SoundPreset
	for _, id := range ids {
		out = append(out, yue.SoundPreset{ID: id, Name: fmt.Sprintf("пресет %d", id),
			Final: []yue.PresetStep{{Chain: "level", Params: map[string]float64{"gain": 1}}}})
	}
	return out
}

// rnRunner — раннер с подменённым Apply: записывает вызовы, отвечает child 99 или err.
func rnRunner(f *rnFake, err error) (*PresetRunner, *[]rnApply) {
	var calls []rnApply
	r := &PresetRunner{Svc: f, Apply: func(_ context.Context, jobID int64, p yue.SoundPreset) (*PresetResult, error) {
		calls = append(calls, rnApply{jobID, p})
		if err != nil {
			return nil, err
		}
		return &PresetResult{ChildID: 99, File: fmt.Sprintf("dsp-preset-%d.flac", p.ID)}, nil
	}}
	return r, &calls
}

// ТК21: есть джоба queued или running → тик ничего не делает (видеокарта занята).
func TestPresetRunnerBusyWorkerDoesNothing(t *testing.T) {
	for _, busy := range []string{"queued", "running"} {
		t.Run(busy, func(t *testing.T) {
			f := &rnFake{
				jobs: []yue.Job{
					{ID: 1, Status: "done", SoundPresets: rnPending(10)},
					{ID: 2, Status: busy},
				},
				presets: rnPresets(10),
			}
			r, calls := rnRunner(f, nil)
			r.Tick(context.Background())
			if len(f.states) != 0 || len(*calls) != 0 {
				t.Errorf("при джобе %s: state %+v, Apply %+v; want ничего", busy, f.states, *calls)
			}
		})
	}
}

// ТК21: очередь пуста → первая по id готовая джоба с pending-пресетом: захват
// (running), Apply с пресетом из списка, state done с child_id. Один пресет за тик.
func TestPresetRunnerAppliesFirstPending(t *testing.T) {
	f := &rnFake{
		jobs: []yue.Job{
			{ID: 7, Status: "done", SoundPresets: rnPending(30)},
			{ID: 3, Status: "done", SoundPresets: []yue.JobPreset{{ID: 10, Status: "done", ChildID: 50}}},
			{ID: 5, Status: "done", SoundPresets: rnPending(20, 21)},
			{ID: 9, Status: "error", SoundPresets: rnPending(40)},
		},
		presets: rnPresets(10, 20, 21, 30, 40),
	}
	r, calls := rnRunner(f, nil)
	r.Tick(context.Background())

	if len(*calls) != 1 {
		t.Fatalf("Apply вызван %d раз, want 1 (один пресет за тик)", len(*calls))
	}
	if c := (*calls)[0]; c.jobID != 5 || c.preset.ID != 20 || c.preset.Name != "пресет 20" {
		t.Errorf("Apply(%d, пресет %d %q), want (5, 20 «пресет 20») — первая по id готовая джоба с pending",
			c.jobID, c.preset.ID, c.preset.Name)
	}
	want := []rnState{
		{5, 20, yue.JobPreset{ID: 20, Status: "running"}},
		{5, 20, yue.JobPreset{ID: 20, Status: "done", ChildID: 99}},
	}
	if len(f.states) != 2 || f.states[0].jobID != 5 || f.states[0].presetID != 20 ||
		f.states[0].st.Status != "running" ||
		f.states[1].jobID != 5 || f.states[1].presetID != 20 ||
		f.states[1].st.Status != "done" || f.states[1].st.ChildID != 99 {
		t.Errorf("state-вызовы %+v, want %+v", f.states, want)
	}

	// следующий тик — следующий pending той же джобы
	r.Tick(context.Background())
	if len(*calls) != 2 || (*calls)[1].jobID != 5 || (*calls)[1].preset.ID != 21 {
		t.Errorf("второй тик: Apply %+v, want (5, 21)", *calls)
	}
}

// ТК21: ошибка Apply → state error с причиной; done не пишется.
func TestPresetRunnerApplyErrorMarksError(t *testing.T) {
	f := &rnFake{
		jobs:    []yue.Job{{ID: 4, Status: "done", SoundPresets: rnPending(10)}},
		presets: rnPresets(10),
	}
	r, calls := rnRunner(f, errors.New("нет дорожек: kick"))
	r.Tick(context.Background())

	if len(*calls) != 1 {
		t.Fatalf("Apply вызван %d раз, want 1", len(*calls))
	}
	if len(f.states) != 2 || f.states[0].st.Status != "running" {
		t.Fatalf("state-вызовы %+v, want running → error", f.states)
	}
	last := f.states[1]
	if last.jobID != 4 || last.presetID != 10 || last.st.Status != "error" ||
		!strings.Contains(last.st.Error, "нет дорожек: kick") {
		t.Errorf("итоговый state %+v, want error с причиной «нет дорожек: kick»", last)
	}
}

// ТК21: пресета нет в списке (удалён) → захват, затем state error «пресет удалён»; Apply не зовётся.
func TestPresetRunnerDeletedPreset(t *testing.T) {
	f := &rnFake{
		jobs:    []yue.Job{{ID: 4, Status: "done", SoundPresets: rnPending(10)}},
		presets: rnPresets(11),
	}
	r, calls := rnRunner(f, nil)
	r.Tick(context.Background())

	if len(*calls) != 0 {
		t.Errorf("Apply %+v, want не вызывался (пресет удалён)", *calls)
	}
	if len(f.states) != 2 || f.states[0].st.Status != "running" {
		t.Fatalf("state-вызовы %+v, want running → error", f.states)
	}
	if last := f.states[1]; last.presetID != 10 || last.st.Status != "error" ||
		!strings.Contains(last.st.Error, "пресет удалён") {
		t.Errorf("итоговый state %+v, want error «пресет удалён»", last)
	}
}

// ТК21: захват ответил 409 (пресет уже взят) → Apply не зовётся, done/error не пишется.
func TestPresetRunnerCaptureConflictSkips(t *testing.T) {
	f := &rnFake{
		jobs:     []yue.Job{{ID: 4, Status: "done", SoundPresets: rnPending(10)}},
		presets:  rnPresets(10),
		conflict: map[[2]int64]bool{{4, 10}: true},
	}
	r, calls := rnRunner(f, nil)
	r.Tick(context.Background())

	if len(*calls) != 0 {
		t.Errorf("Apply %+v, want не вызывался после 409 на захвате", *calls)
	}
	for _, s := range f.states {
		if s.jobID == 4 && s.presetID == 10 && s.st.Status != "running" {
			t.Errorf("после 409 записан state %+v, want только попытка захвата", s)
		}
	}
}

// ТК21: draft-джоба (и не done) пропускается, даже если её id меньше.
func TestPresetRunnerSkipsDraft(t *testing.T) {
	f := &rnFake{
		jobs: []yue.Job{
			{ID: 1, Status: "done", Draft: true, SoundPresets: rnPending(10)},
			{ID: 2, Status: "cancelled", SoundPresets: rnPending(10)},
			{ID: 3, Status: "done", SoundPresets: rnPending(10)},
		},
		presets: rnPresets(10),
	}
	r, calls := rnRunner(f, nil)
	r.Tick(context.Background())

	if len(*calls) != 1 || (*calls)[0].jobID != 3 {
		t.Errorf("Apply %+v, want один вызов для джобы 3 (черновик и неготовая пропущены)", *calls)
	}
	for _, s := range f.states {
		if s.jobID != 3 {
			t.Errorf("state у джобы %d, want только у 3: %+v", s.jobID, s)
		}
	}
}

// ТК21 / условие 4: ошибка списка джоб или пресетов — тик тихо выходит, без паники и без state.
func TestPresetRunnerListErrorsAreQuiet(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    *rnFake
	}{
		{"джобы", &rnFake{jobsErr: errors.New("worker down")}},
		{"пресеты", &rnFake{
			jobs:       []yue.Job{{ID: 4, Status: "done", SoundPresets: rnPending(10)}},
			presetsErr: errors.New("worker down"),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, calls := rnRunner(tc.f, nil)
			r.Tick(context.Background()) // паника уронит тест
			if len(*calls) != 0 {
				t.Errorf("Apply %+v, want нет", *calls)
			}
			for _, s := range tc.f.states {
				if s.st.Status == "done" || s.st.Status == "error" {
					t.Errorf("записан state %+v при ошибке списка, want нет", s)
				}
			}
		})
	}
}

// ТК21: нечего применять (pending нет) → ничего.
func TestPresetRunnerNothingPending(t *testing.T) {
	f := &rnFake{
		jobs: []yue.Job{
			{ID: 1, Status: "done"},
			{ID: 2, Status: "done", SoundPresets: []yue.JobPreset{{ID: 10, Status: "error", Error: "x"}}},
		},
		presets: rnPresets(10),
	}
	r, calls := rnRunner(f, nil)
	r.Tick(context.Background())
	if len(f.states) != 0 || len(*calls) != 0 {
		t.Errorf("state %+v, Apply %+v; want ничего (pending нет, error не повторяется сам)", f.states, *calls)
	}
}

// Кросс-ревью s1: эффект ffmpeg на части барабанов пересборка молча пропускала бы — ошибка до работы.
func TestApplySoundPresetFfmpegOnDrumPartIsError(t *testing.T) {
	f := psSetup(t, nil)
	p := psPreset(11, []yue.PresetSpec{{Stems: []string{"kick"}, Chain: "eq", Params: map[string]float64{"low": -12}}}, nil)
	_, err := ApplySoundPreset(context.Background(), f, psJobID, p)
	if err == nil || !strings.Contains(err.Error(), "kick") {
		t.Fatalf("want ошибку про kick, got %v", err)
	}
	if len(f.promoted) != 0 || len(f.splits) != 0 {
		t.Fatalf("работа началась: promoted %v splits %v", f.promoted, f.splits)
	}
}

// ---------- Этап 1б. ТК29: финал по целевой громкости (условие 11) ----------
//
// target_lufs задан → после шагов финала пресета добавляется level с усилением
// target − LUFS микса (зажим −12…+12) и потолком −1 дБ. LUFS микса — Metrics["lufs"]
// варианта пересборки (UploadDsp); без правок дорожек — AnalyzeJob(трек). Замера нет —
// ошибка «нет замера громкости», версия не создаётся. target nil — как раньше.
//
// Усиление проверяется по звуку: тот же пресет без цели (nil) — опора; с целью —
// амплитуда тона 500 Гц варианта финала отличается на ожидаемые дБ.

func psTarget(v float64) *float64 { return &v }

func psWidth() []yue.PresetStep {
	return []yue.PresetStep{{Chain: "width", Params: map[string]float64{"width": 1.1, "bass": 120}}}
}

// psFinalAmp — амплитуда 500 Гц в загруженном варианте финала dsp-preset-<id>.flac.
func psFinalAmp(t *testing.T, f *psFake, id int64) float64 {
	t.Helper()
	name := fmt.Sprintf("dsp-preset-%d.flac", id)
	data := f.uploads[name]
	if len(data) == 0 {
		t.Fatalf("вариант финала %s не загружен: %v", name, keys(f.uploads))
	}
	return toneAmp(decodeBytes(t, data), 500, 2, 8)
}

func psDb(a, ref float64) float64 { return 20 * math.Log10(a/ref) }

// psQuietAudio — заменить звук трека тихим 500 Гц (амплитуда 0.01), чтобы +12 дБ не упирались в потолок.
func psQuietAudio(t *testing.T, f *psFake) {
	t.Helper()
	f.files[key(psJobID, "audio.flac")] = lavfi(t, aeval("0.01*sin(2*PI*500*t)", psDur),
		filepath.Join(t.TempDir(), "ps-quiet.flac"))
}

// ТК29: правка на голос + финал [width], цель −13, LUFS пересборки −15 → level +2 дБ
// поверх финала; вариант dsp-preset-<id>.flac, версия из него. AnalyzeJob отвечает
// другим числом (−30): LUFS берётся у микса пересборки, а не у исходного трека.
func TestApplySoundPresetTargetLUFSFromRebuildMetrics(t *testing.T) {
	specs := []yue.PresetSpec{psVocalCut()}

	base := psSetup(t, nil)
	base.uploadMetrics = map[string]any{"lufs": -15.0}
	base.analyze = map[string]any{"lufs": -30.0}
	pb := psPreset(31, specs, psWidth())
	psApply(t, base, pb)
	ref := psFinalAmp(t, base, pb.ID)

	f := psSetup(t, nil)
	f.uploadMetrics = map[string]any{"lufs": -15.0}
	f.analyze = map[string]any{"lufs": -30.0}
	p := psPreset(31, specs, psWidth())
	p.TargetLUFS = psTarget(-13)
	res := psApply(t, f, p)

	final := fmt.Sprintf("dsp-preset-%d.flac", p.ID)
	if got := psDb(psFinalAmp(t, f, p.ID), ref); !near(got, 2, 0.5) {
		t.Errorf("усиление финала %.2f дБ относительно пресета без цели, want +2 (−13 − (−15))", got)
	}
	if len(f.promoted) != 1 || f.promoted[0].file != final || f.promoted[0].title != psTrackTitle(p) {
		t.Errorf("VariantToTrack %+v, want один вызов (%s, %q)", f.promoted, final, psTrackTitle(p))
	}
	if res.ChildID != psChild || res.File != final {
		t.Errorf("результат %+v, want ChildID %d File %s", *res, psChild, final)
	}
}

// ТК29: только цель (финал пуст) → вариант финала всё равно есть: один level поверх
// результата пересборки (+2 дБ к файлу пересборки).
func TestApplySoundPresetTargetLUFSWithEmptyFinal(t *testing.T) {
	f := psSetup(t, nil)
	f.uploadMetrics = map[string]any{"lufs": -15.0}
	p := psPreset(32, []yue.PresetSpec{psVocalCut()}, nil)
	p.TargetLUFS = psTarget(-13)
	res := psApply(t, f, p)

	rebuilt := fmt.Sprintf("dsp-preset-%d-mix.flac", p.ID)
	final := fmt.Sprintf("dsp-preset-%d.flac", p.ID)
	mix := f.uploads[rebuilt]
	if len(mix) == 0 {
		t.Fatalf("пересборка не загружена: %v", keys(f.uploads))
	}
	ref := toneAmp(decodeBytes(t, mix), 500, 2, 8)
	if got := psDb(psFinalAmp(t, f, p.ID), ref); !near(got, 2, 0.5) {
		t.Errorf("вариант финала %.2f дБ к пересборке, want +2", got)
	}
	if res.File != final || len(f.promoted) != 1 || f.promoted[0].file != final {
		t.Errorf("версия из %q (promoted %+v), want из %s", res.File, f.promoted, final)
	}
}

// ТК29: без правок дорожек — LUFS трека через AnalyzeJob(трек): −10 при цели −13 → −3 дБ.
func TestApplySoundPresetTargetLUFSFinalOnlyUsesAnalyze(t *testing.T) {
	base := psSetup(t, nil)
	pb := psPreset(33, nil, psWidth())
	psApply(t, base, pb)
	ref := psFinalAmp(t, base, pb.ID)

	f := psSetup(t, nil)
	f.analyze = map[string]any{"lufs": -10.0}
	p := psPreset(33, nil, psWidth())
	p.TargetLUFS = psTarget(-13)
	psApply(t, f, p)

	if !reflect.DeepEqual(f.analyzed, []int64{psJobID}) {
		t.Errorf("AnalyzeJob вызван для %v, want [%d] (трек без правок дорожек)", f.analyzed, psJobID)
	}
	if got := psDb(psFinalAmp(t, f, p.ID), ref); !near(got, -3, 0.5) {
		t.Errorf("усиление финала %.2f дБ, want −3 (−13 − (−10))", got)
	}
	if len(f.calls) != 0 || psCount(f.log, "upload:dsp-preset-33-mix") != 0 {
		t.Errorf("пересборка шла без specs: ApplyFx %d, лог %v", len(f.calls), f.log)
	}
}

// ТК29: нужно +20 → зажим +12; нужно −22 → зажим −12.
func TestApplySoundPresetTargetLUFSClamp(t *testing.T) {
	for _, tc := range []struct {
		name         string
		quiet        bool
		target, lufs float64
		wantDb       float64
	}{
		{"+20 → +12", true, -13, -33, 12},
		{"−22 → −12", false, -24, -2, -12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := psSetup(t, nil)
			if tc.quiet {
				psQuietAudio(t, base)
			}
			pb := psPreset(34, nil, psWidth())
			psApply(t, base, pb)
			ref := psFinalAmp(t, base, pb.ID)

			f := psSetup(t, nil)
			if tc.quiet {
				psQuietAudio(t, f)
			}
			f.analyze = map[string]any{"lufs": tc.lufs}
			p := psPreset(34, nil, psWidth())
			p.TargetLUFS = psTarget(tc.target)
			psApply(t, f, p)

			if got := psDb(psFinalAmp(t, f, p.ID), ref); !near(got, tc.wantDb, 0.6) {
				t.Errorf("усиление %.2f дБ, want %+.0f (зажим −12…+12)", got, tc.wantDb)
			}
		})
	}
}

// ТК29: граф финала с целью = шаги пресета → level(gain, ceiling −1). Проверка по звуку:
// пресет [width] с целью −13 при LUFS −25 (усиление +12 упирается в потолок) звучит так же,
// как пресет без цели с финалом [width, level{gain 12, ceiling −1}]. Потолок −0.5 или 0
// дал бы громче на 0.5–1 дБ (опорный замер на этом звуке).
func TestApplySoundPresetTargetLUFSGraphIsFinalThenLevel(t *testing.T) {
	ref := psSetup(t, nil)
	explicit := append(psWidth(), yue.PresetStep{Chain: "level", Params: map[string]float64{"gain": 12, "ceiling": -1}})
	pr := psPreset(35, nil, explicit)
	psApply(t, ref, pr)
	want := decodeBytes(t, ref.uploads[fmt.Sprintf("dsp-preset-%d.flac", pr.ID)])

	f := psSetup(t, nil)
	f.analyze = map[string]any{"lufs": -25.0}
	p := psPreset(35, nil, psWidth())
	p.TargetLUFS = psTarget(-13)
	psApply(t, f, p)

	data := f.uploads[fmt.Sprintf("dsp-preset-%d.flac", p.ID)]
	if len(data) == 0 {
		t.Fatalf("вариант финала не загружен: %v", keys(f.uploads))
	}
	got := decodeBytes(t, data)
	gw, ww := rms(slice(got, 1, 9)), rms(slice(want, 1, 9))
	if ww == 0 || !near(psDb(gw, ww), 0, 0.2) {
		t.Errorf("громкость варианта %.4f, want как у финала width → level(+12, −1) %.4f (%.2f дБ)",
			gw, ww, psDb(gw, ww))
	}
	var gp, wp float64
	for _, v := range slice(got, 1, 9) {
		gp = math.Max(gp, math.Abs(float64(v)))
	}
	for _, v := range slice(want, 1, 9) {
		wp = math.Max(wp, math.Abs(float64(v)))
	}
	if !near(psDb(gp, wp), 0, 0.2) {
		t.Errorf("пик варианта %.3f, want как у width → level(+12, −1) %.3f", gp, wp)
	}
}

// ТК29: замера громкости нет → ошибка «нет замера громкости», версия не создаётся,
// вариант финала не загружается (не применение вслепую).
func TestApplySoundPresetTargetLUFSNoMetricsIsError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		specs []yue.PresetSpec
		up    map[string]any
		an    map[string]any
	}{
		{"с правками: у пересборки нет lufs", []yue.PresetSpec{psVocalCut()}, nil, nil},
		{"с правками: metrics без lufs", []yue.PresetSpec{psVocalCut()}, map[string]any{"peak": -1.0}, map[string]any{}},
		{"без правок: AnalyzeJob без lufs", nil, nil, map[string]any{"bpm": 120.0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := psSetup(t, nil)
			f.uploadMetrics, f.analyze = tc.up, tc.an
			p := psPreset(36, tc.specs, psWidth())
			p.TargetLUFS = psTarget(-13)

			res, err := ApplySoundPreset(context.Background(), f, psJobID, p)
			if err == nil {
				t.Fatalf("ApplySoundPreset без замера громкости прошёл: %+v", res)
			}
			if !strings.Contains(err.Error(), "нет замера громкости") {
				t.Errorf("ошибка %q, want с «нет замера громкости»", err)
			}
			if len(f.promoted) != 0 {
				t.Errorf("VariantToTrack вызван: %+v", f.promoted)
			}
			if _, ok := f.uploads[fmt.Sprintf("dsp-preset-%d.flac", p.ID)]; ok {
				t.Errorf("вариант финала загружен без замера громкости")
			}
		})
	}
}

// ТК29: target nil — поведение прежнее: замеры не нужны (их нет — не ошибка), AnalyzeJob
// не зовётся; финал пуст → без варианта финала, версия из пересборки.
func TestApplySoundPresetNoTargetNeedsNoMetrics(t *testing.T) {
	t.Run("финал пуст", func(t *testing.T) {
		f := psSetup(t, nil)
		p := psPreset(37, []yue.PresetSpec{psVocalCut()}, nil)
		res := psApply(t, f, p)
		rebuilt := fmt.Sprintf("dsp-preset-%d-mix.flac", p.ID)
		if res.File != rebuilt {
			t.Errorf("версия из %q, want %s (финала нет)", res.File, rebuilt)
		}
		if _, ok := f.uploads[fmt.Sprintf("dsp-preset-%d.flac", p.ID)]; ok {
			t.Errorf("загружен вариант финала без финала и цели")
		}
		if len(f.analyzed) != 0 {
			t.Errorf("AnalyzeJob вызван %v, want нет (цели нет)", f.analyzed)
		}
	})
	t.Run("финал [width] без правок", func(t *testing.T) {
		f := psSetup(t, nil)
		p := psPreset(38, nil, psWidth())
		res := psApply(t, f, p)
		if res.File != fmt.Sprintf("dsp-preset-%d.flac", p.ID) {
			t.Errorf("версия из %q, want вариант финала", res.File)
		}
		if len(f.analyzed) != 0 {
			t.Errorf("AnalyzeJob вызван %v, want нет (цели нет)", f.analyzed)
		}
	})
}

// Кросс-ревью этапа 2: kit_mid/kit_low тамов — тоже наборы для автоустановки.
func TestApplySoundPresetInstallsTomKits(t *testing.T) {
	f := psSetup(t, map[string]string{"kick": "0.5*lt(mod(t\\,0.5)\\,0.02)"})
	f.kits = []any{map[string]any{"name": "osdk/kick", "samples": 3.0}}
	p := psPreset(13, []yue.PresetSpec{
		{Stems: []string{"kick"}, Engine: []map[string]any{{"type": "sampler", "kit": "osdk/kick",
			"kit_mid": "midkit/tom", "kit_low": "lowkit/tom"}}},
	}, nil)
	psApply(t, f, p)
	if !reflect.DeepEqual(f.installs, []string{"midkit", "lowkit"}) {
		t.Errorf("InstallFxKit %q, want [midkit lowkit]", f.installs)
	}
}
