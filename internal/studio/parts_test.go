package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-own-track, этап 8а, условие 68 (тест-кейсы ТК102, ТК103): партии-рецепты пресета.
// Написаны по карточке, без чтения реализации.
//
// Сигнатуры Go (выбраны test-author: карточка задаёт имена и смысл, не типы):
//
//	type PartOpts struct { Style string; Octave int; Sections []string }          // Sections nil/пусто — все такты
//	type PartNote struct { T, D float64; Midi []float64(или []int); Vel float64 } // json: t, d, midi, vel
//	func PartNotes(bars []yue.ChordBar, o PartOpts) []PartNote                    // порт synthPart.partNotes
//	type PercOpts struct { Pattern string; Sections []string; Swing, Accent float64 } // Accent передаётся явно
//	type PercHit struct { T, D, Vel float64 }                                     // json: t, d, vel
//	func PercHits(bars []yue.ChordBar, o PercOpts) []PercHit                      // порт percPart.percHits
//	func BarsFromBeat(g yue.BeatGrid, dur float64, shift int) []yue.ChordBar      // порт percPart.barsFromBeat
//
// Результаты сравниваются через JSON (поля t/d/midi/vel), поэтому тип Midi и точные имена полей Go свободны.
//
// ТК102 — паритет с JS: эталоны testdata/parts_golden.json генерирует frontend/src/partsGolden.test.js
// (WRITE_GOLDEN=1 npx vitest run src/partsGolden.test.js); сравнение ±1e-9. Опции в эталоне полные.
// Расхождение карточки и JS: у partNotes sections [] — «ни одного такта», условие 68 — «пусто — все»;
// эталон такой случай не содержит, Go проверяется отдельно по условию (TestPartNotesEmptySectionsAll).
//
// ТК103 — применение: yue.SoundPreset.Parts []yue.PresetPart (json "parts": kind, engine, style, octave,
// pattern, swing, accent, sections, place). Такты — ChordGrid трека; synth — ноты PartNotes, perc — удары
// PercHits; без аккордов perc — по BarsFromBeat(JobGrid, длина трека, 0), synth пропускается; ноты — в первый
// блок цепочки (поле notes); в пересборку — запись {stems [mix], add, engine, place}: виден как ApplyFx с
// source mix и Add. Нет нот — партия пропускается без ошибки. Наборы kit партий докачиваются (InstallFxKit).
// Воркер — psFake (presets_test.go) + ChordGrid/JobGrid: внешние границы, своё не подменяется.

// ---------- ТК102: паритет с JS ----------

type ppGoldenSynth struct {
	Name string         `json:"name"`
	Bars []yue.ChordBar `json:"bars"`
	Opts struct {
		Style    string   `json:"style"`
		Octave   int      `json:"octave"`
		Sections []string `json:"sections"`
	} `json:"opts"`
	Out []map[string]any `json:"out"`
}

type ppGoldenPerc struct {
	Name string         `json:"name"`
	Bars []yue.ChordBar `json:"bars"`
	Opts struct {
		Pattern  string   `json:"pattern"`
		Sections []string `json:"sections"`
		Swing    float64  `json:"swing"`
		Accent   float64  `json:"accent"`
	} `json:"opts"`
	Out []map[string]any `json:"out"`
}

type ppGoldenBeat struct {
	Name  string           `json:"name"`
	Grid  yue.BeatGrid     `json:"grid"`
	Dur   float64          `json:"dur"`
	Shift int              `json:"shift"`
	Out   []map[string]any `json:"out"`
}

type ppGolden struct {
	PartNotes    []ppGoldenSynth `json:"partNotes"`
	PercHits     []ppGoldenPerc  `json:"percHits"`
	BarsFromBeat []ppGoldenBeat  `json:"barsFromBeat"`
}

func ppLoadGolden(t *testing.T) ppGolden {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "parts_golden.json"))
	if err != nil {
		t.Fatalf("эталоны: %v (сгенерировать: cd frontend && WRITE_GOLDEN=1 npx vitest run src/partsGolden.test.js)", err)
	}
	var g ppGolden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("эталоны: %v", err)
	}
	if len(g.PartNotes) == 0 || len(g.PercHits) == 0 || len(g.BarsFromBeat) == 0 {
		t.Fatalf("эталоны пусты: notes %d, hits %d, bars %d", len(g.PartNotes), len(g.PercHits), len(g.BarsFromBeat))
	}
	return g
}

// ppGeneric — значение Go как JSON-объекты (числа float64): для сравнения с эталоном.
func ppGeneric(t *testing.T, v any) []map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("результат %s — не список объектов: %v", b, err)
	}
	return out
}

// ppSame — got совпадает с want по полям fields: числа ±1e-9, списки чисел поэлементно, строки точно.
func ppSame(got, want []map[string]any, fields []string) string {
	if len(got) != len(want) {
		return fmt.Sprintf("элементов %d, want %d", len(got), len(want))
	}
	for i := range want {
		for _, k := range fields {
			if msg := ppSameValue(got[i][k], want[i][k]); msg != "" {
				return fmt.Sprintf("[%d].%s: %s (got %v, want %v)", i, k, msg, got[i], want[i])
			}
		}
	}
	return ""
}

func ppSameValue(g, w any) string {
	switch wv := w.(type) {
	case float64:
		gv, ok := g.(float64)
		if !ok || math.Abs(gv-wv) > 1e-9 {
			return "число"
		}
	case []any:
		gv, ok := g.([]any)
		if !ok || len(gv) != len(wv) {
			return "длина списка"
		}
		for j := range wv {
			if msg := ppSameValue(gv[j], wv[j]); msg != "" {
				return msg
			}
		}
	default:
		if !reflect.DeepEqual(g, w) {
			return "значение"
		}
	}
	return ""
}

// ТК102: PartNotes = synthPart.partNotes на эталонах (стили, октавы, секции, трудные аккорды и такты).
func TestPartNotesGolden(t *testing.T) {
	for _, c := range ppLoadGolden(t).PartNotes {
		t.Run(c.Name, func(t *testing.T) {
			got := PartNotes(c.Bars, PartOpts{Style: c.Opts.Style, Octave: c.Opts.Octave, Sections: c.Opts.Sections})
			if msg := ppSame(ppGeneric(t, got), c.Out, []string{"t", "d", "midi", "vel"}); msg != "" {
				t.Error(msg)
			}
		})
	}
}

// ТК102: PercHits = percPart.percHits на эталонах (рисунки, swing, accent, секции, трудные такты).
func TestPercHitsGolden(t *testing.T) {
	for _, c := range ppLoadGolden(t).PercHits {
		t.Run(c.Name, func(t *testing.T) {
			got := PercHits(c.Bars, PercOpts{Pattern: c.Opts.Pattern, Sections: c.Opts.Sections,
				Swing: c.Opts.Swing, Accent: c.Opts.Accent})
			if msg := ppSame(ppGeneric(t, got), c.Out, []string{"t", "d", "vel"}); msg != "" {
				t.Error(msg)
			}
		})
	}
}

// ТК102: BarsFromBeat = percPart.barsFromBeat на эталонах (неполный последний такт, shift, пределы).
func TestBarsFromBeatGolden(t *testing.T) {
	for _, c := range ppLoadGolden(t).BarsFromBeat {
		t.Run(c.Name, func(t *testing.T) {
			got := BarsFromBeat(c.Grid, c.Dur, c.Shift)
			if msg := ppSame(ppGeneric(t, got), c.Out, []string{"start", "end", "section"}); msg != "" {
				t.Error(msg)
			}
		})
	}
}

// Условие 68: sections пусто — все такты (как nil), и у синта, и у перкуссии.
func TestPartNotesEmptySectionsAll(t *testing.T) {
	bars := ppBars()
	all := PartNotes(bars, PartOpts{Style: "pad"})
	empty := PartNotes(bars, PartOpts{Style: "pad", Sections: []string{}})
	if len(all) != 4 || !reflect.DeepEqual(ppGeneric(t, empty), ppGeneric(t, all)) {
		t.Errorf("sections [] → %d нот, nil → %d, want одинаково и 4 (по ноте-аккорду на такт)", len(empty), len(all))
	}
	ha, he := PercHits(bars, PercOpts{Pattern: "fours", Accent: 1}), PercHits(bars, PercOpts{Pattern: "fours", Sections: []string{}, Accent: 1})
	if len(ha) != 16 || len(he) != 16 {
		t.Errorf("perc fours: nil → %d, [] → %d ударов, want 16 (4 такта × 4 доли)", len(ha), len(he))
	}
}

// ---------- ТК103: партии пресета в пересборке ----------

const ppKitInstalled = "ppkit" // набор, который «есть» на воркере

// ppBars — ChordGrid ТК103: 4 такта по 2 с, такты 3–4 — припев.
func ppBars() []yue.ChordBar {
	return []yue.ChordBar{
		{Start: 0, End: 2, Chord: "Am", Section: "verse"},
		{Start: 2, End: 4, Chord: "F", Section: "verse"},
		{Start: 4, End: 6, Chord: "C", Section: "chorus"},
		{Start: 6, End: 8, Chord: "G", Section: "chorus"},
	}
}

// ppFake — psFake + сетка аккордов и долей трека.
type ppFake struct {
	*psFake
	chords    *yue.ChordGrid
	beat      *yue.BeatGrid
	gridCalls int
}

func (f *ppFake) ChordGrid(context.Context, int64) (*yue.ChordGrid, error) { return f.chords, nil }

func (f *ppFake) JobGrid(context.Context, int64, float64, float64) (*yue.BeatGrid, error) {
	f.gridCalls++
	return f.beat, nil
}

// ppSetup — трек длиной dur (vocals 3000 + other 500 Гц, bass/drums — тишина) с аккордами bars.
func ppSetup(t *testing.T, dur float64, bars []yue.ChordBar) *ppFake {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	p := func(n string) string { return filepath.Join(dir, n) }
	files := map[string]string{
		"audio.flac":       lavfi(t, aeval(exprPs3k+"+"+exprPs500, dur), p("pp-audio.flac")),
		"stem-vocals.flac": lavfi(t, aeval(exprPs3k, dur), p("pp-vocals.flac")),
		"stem-other.flac":  lavfi(t, aeval(exprPs500, dur), p("pp-other.flac")),
		"stem-drums.flac":  lavfi(t, aeval("0", dur), p("pp-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", dur), p("pp-bass.flac")),
	}
	sf := newSecFake()
	put(sf, psJobID, files)
	f := &psFake{
		engFake: newEngFake(t, sf),
		jobs: []yue.Job{{ID: psJobID, Title: psTitle, Status: "done", AudioFile: "audio.flac",
			DurationSec: dur}},
		kits: []any{map[string]any{"name": ppKitInstalled + "/clap", "samples": 2.0}},
	}
	return &ppFake{psFake: f, chords: &yue.ChordGrid{BPM: 120, Bars: bars},
		beat: &yue.BeatGrid{BPM: 120, Offset: 0, Strength: 0.9, Source: "drums"}}
}

// ppPreset — пресет из одних партий: JSON как у воркера (поля частей — по карточке).
func ppPreset(t *testing.T, id int64, partsJSON string) yue.SoundPreset {
	t.Helper()
	var p yue.SoundPreset
	src := fmt.Sprintf(`{"id": %d, "name": "Партии", "specs": [], "final": [], "master": [], "parts": %s}`, id, partsJSON)
	if err := json.Unmarshal([]byte(src), &p); err != nil {
		t.Fatalf("пресет: %v", err)
	}
	if len(ppPartsOf(p)) == 0 {
		t.Fatalf("parts из JSON не прочитаны: %s", partsJSON)
	}
	return p
}

// партии ТК103: synth pad только в припеве (+ eq после синта), perc backbeat на все такты; наборы — новые
const ppPartsTK103 = `[
 {"kind": "synth", "engine": [{"type": "synth", "osc1": 4, "kit": "pianokit/piano"}, {"type": "eq", "highpass_hz": 200}],
  "style": "pad", "sections": ["chorus"]},
 {"kind": "perc", "engine": [{"type": "perc", "voice": 0, "kit": "clapkit/clap"}], "pattern": "backbeat"}
]`

// ppPartsOf — партии пресета: поле SoundPreset.Parts типа []yue.PresetPart (условие 69).
func ppPartsOf(p yue.SoundPreset) []yue.PresetPart { return p.Parts }

type ppNote struct {
	T    float64   `json:"t"`
	D    float64   `json:"d"`
	Midi []float64 `json:"midi"`
	Vel  float64   `json:"vel"`
}

// ppCall — запрос ApplyFx партии kind (первый блок цепочки synth|perc); nil — не было.
func ppCall(t *testing.T, f *ppFake, kind string) *yue.FxRequest {
	t.Helper()
	var found *yue.FxRequest
	for i := range f.calls {
		r := f.calls[i].req
		if len(r.Chain) > 0 && r.Chain[0]["type"] == kind {
			if found != nil {
				t.Errorf("партия %s ушла в ApplyFx дважды", kind)
			}
			found = &f.calls[i].req
		}
	}
	return found
}

func ppNotes(t *testing.T, r *yue.FxRequest) []ppNote {
	t.Helper()
	b, err := json.Marshal(r.Chain[0]["notes"])
	if err != nil {
		t.Fatal(err)
	}
	var out []ppNote
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("notes первого блока %s: %v", b, err)
	}
	return out
}

func ppCheckMixAdd(t *testing.T, what string, r *yue.FxRequest) {
	t.Helper()
	if r.Source != "mix" || !r.Add {
		t.Errorf("%s: ApplyFx source=%q add=%v, want запись пересборки {stems [mix], add true}", what, r.Source, r.Add)
	}
}

func ppApply(t *testing.T, f *ppFake, p yue.SoundPreset) {
	t.Helper()
	if _, err := ApplySoundPreset(context.Background(), f, psJobID, p); err != nil {
		t.Fatalf("ApplySoundPreset: %v", err)
	}
}

// ТК103: трек с ChordGrid из 4 тактов (2 — припев): synth pad — ноты только в тактах припева,
// perc backbeat — 4×2 удара; обе — записи пересборки на mix с add; наборы партий докачаны.
func TestPresetPartsWithChords(t *testing.T) {
	f := ppSetup(t, 8, ppBars())
	ppApply(t, f, ppPreset(t, 41, ppPartsTK103))

	syn := ppCall(t, f, "synth")
	if syn == nil {
		t.Fatalf("synth-партии нет в пересборке: вызовы ApplyFx %d", len(f.calls))
	}
	ppCheckMixAdd(t, "synth", syn)
	if len(syn.Chain) != 2 || syn.Chain[1]["type"] != "eq" || syn.Chain[0]["osc1"] != 4.0 {
		t.Errorf("цепочка synth %v, want цепочку рецепта (synth osc1 4 → eq) с нотами в первом блоке", syn.Chain)
	}
	// pad в припеве: C (4–6 с) и G (6–8 с), C4-регистр, vel 0,8 — как synthPart.partNotes
	want := []ppNote{{T: 4, D: 2, Midi: []float64{60, 64, 67}, Vel: 0.8}, {T: 6, D: 2, Midi: []float64{62, 67, 71}, Vel: 0.8}}
	if got := ppNotes(t, syn); !reflect.DeepEqual(got, want) {
		t.Errorf("ноты synth %+v, want %+v (только такты chorus)", got, want)
	}

	perc := ppCall(t, f, "perc")
	if perc == nil {
		t.Fatalf("perc-партии нет в пересборке: вызовы ApplyFx %d", len(f.calls))
	}
	ppCheckMixAdd(t, "perc", perc)
	hits := ppNotes(t, perc)
	if len(hits) != 8 {
		t.Fatalf("ударов perc %d, want 8 (4 такта × 2 доли backbeat)", len(hits))
	}
	for i, h := range hits {
		if wt := float64(i)*1 + 0.5; math.Abs(h.T-wt) > 1e-9 || math.Abs(h.D-0.5) > 1e-9 {
			t.Errorf("удар %d: t %.3f d %.3f, want t %.1f d 0,5 (доли 2 и 4 такта)", i, h.T, h.D, wt)
		}
	}
	if f.gridCalls != 0 {
		t.Errorf("JobGrid вызван %d раз, want 0 (такты есть в ChordGrid)", f.gridCalls)
	}

	// наборы партий докачаны, по разу на набор
	slices.Sort(f.installs)
	if !reflect.DeepEqual(f.installs, []string{"clapkit", "pianokit"}) {
		t.Errorf("InstallFxKit %q, want [clapkit pianokit] (наборы партий)", f.installs)
	}
}

// ТК103: трек без аккордов (ChordGrid пуст, JobGrid 120 BPM offset 0, длина 8 с) — synth пропущена,
// perc — по 4 тактам BarsFromBeat; ошибки нет.
func TestPresetPartsWithoutChords(t *testing.T) {
	f := ppSetup(t, 8, nil)
	ppApply(t, f, ppPreset(t, 42, ppPartsTK103))

	if r := ppCall(t, f, "synth"); r != nil {
		t.Errorf("synth-партия ушла в пересборку (%d нот), want пропуск: у трека нет аккордов", len(ppNotes(t, r)))
	}
	perc := ppCall(t, f, "perc")
	if perc == nil {
		t.Fatalf("perc-партии нет в пересборке, want удары по сетке долей")
	}
	ppCheckMixAdd(t, "perc", perc)
	hits := ppNotes(t, perc)
	if len(hits) != 8 {
		t.Fatalf("ударов perc %d, want 8 (BarsFromBeat: 4 такта по 2 с × 2 доли backbeat)", len(hits))
	}
	for i, h := range hits {
		if wt := float64(i) + 0.5; math.Abs(h.T-wt) > 1e-9 {
			t.Errorf("удар %d в %.3f с, want %.1f", i, h.T, wt)
		}
	}
	if f.gridCalls == 0 {
		t.Errorf("JobGrid не вызван, want сетка долей для тактов без аккордов")
	}
}

// Условие 68: нет нот (секция, которой нет в песне) — партия пропускается, не ошибка; другая партия идёт.
func TestPresetPartsNoNotesSkipped(t *testing.T) {
	f := ppSetup(t, 8, ppBars())
	ppApply(t, f, ppPreset(t, 43, `[
 {"kind": "synth", "engine": [{"type": "synth"}], "style": "arp", "sections": ["solo"]},
 {"kind": "perc", "engine": [{"type": "perc", "voice": 1, "kit": "`+ppKitInstalled+`/clap"}], "pattern": "fours", "sections": ["chorus"]}
]`))
	if r := ppCall(t, f, "synth"); r != nil {
		t.Errorf("synth без нот ушла в пересборку, want пропуск")
	}
	perc := ppCall(t, f, "perc")
	if perc == nil {
		t.Fatal("perc-партии нет в пересборке")
	}
	if hits := ppNotes(t, perc); len(hits) != 8 || math.Abs(hits[0].T-4) > 1e-9 {
		t.Errorf("perc fours в припеве: %d ударов (первый %+v), want 8 с 4 с", len(hits), hits)
	}
	if len(f.installs) != 0 {
		t.Errorf("InstallFxKit %q, want ни одного (набор уже есть)", f.installs)
	}
}

// Условие 68: октава и accent из рецепта доходят до нот/ударов (octave −1 у pulse, accent 0 — все .9).
func TestPresetPartsOptionsApplied(t *testing.T) {
	f := ppSetup(t, 8, ppBars())
	ppApply(t, f, ppPreset(t, 44, `[
 {"kind": "synth", "engine": [{"type": "synth"}], "style": "pulse", "octave": -1, "sections": ["verse"]},
 {"kind": "perc", "engine": [{"type": "perc"}], "pattern": "eighths", "accent": 0, "swing": 0.2, "sections": []}
]`))
	syn := ppCall(t, f, "synth")
	if syn == nil {
		t.Fatal("synth-партии нет в пересборке")
	}
	notes := ppNotes(t, syn)
	// pulse: основной тон восьмыми, регистр C2 (36) − октава: Am → A1 (33), F → F1 (29); 2 такта × 8
	if len(notes) != 16 || !reflect.DeepEqual(notes[0].Midi, []float64{33}) || !reflect.DeepEqual(notes[8].Midi, []float64{29}) {
		t.Errorf("pulse octave −1 в куплете: %d нот, первая %+v, девятая %+v; want 16, midi 33 и 29", len(notes),
			ppAt(notes, 0), ppAt(notes, 8))
	}
	perc := ppCall(t, f, "perc")
	if perc == nil {
		t.Fatal("perc-партии нет в пересборке")
	}
	hits := ppNotes(t, perc)
	if len(hits) != 32 {
		t.Fatalf("eighths на 4 такта: %d ударов, want 32 (sections [] — все такты)", len(hits))
	}
	for i, h := range hits {
		if math.Abs(h.Vel-0.9) > 1e-9 {
			t.Errorf("удар %d vel %.3f, want 0,9 (accent 0 — ровно)", i, h.Vel)
			break
		}
	}
	// swing 0,2: вторая восьмая доли позже на 0,2 × 0,25 с
	if math.Abs(hits[1].T-(0.25+0.05)) > 1e-9 {
		t.Errorf("вторая восьмая в %.4f с, want 0,30 (swing 0,2)", hits[1].T)
	}
}

func ppAt(n []ppNote, i int) any {
	if i < len(n) {
		return n[i]
	}
	return nil
}

// Условие 68/69: поля партии в JSON (воркер ↔ приложение) читаются в yue.PresetPart.
func TestPresetPartJSON(t *testing.T) {
	p := ppPreset(t, 45, `[{"kind": "perc", "engine": [{"type": "perc"}], "pattern": "offbeat", "swing": 0.3,
 "accent": 0.5, "sections": ["chorus"], "place": {"pan": -0.4}},
 {"kind": "synth", "engine": [{"type": "synth"}], "style": "drone", "octave": -2}]`)
	if len(p.Parts) != 2 {
		t.Fatalf("партий %d, want 2", len(p.Parts))
	}
	b, err := json.Marshal(p.Parts)
	if err != nil {
		t.Fatal(err)
	}
	var back []map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	pc, sy := back[0], back[1]
	if pc["kind"] != "perc" || pc["pattern"] != "offbeat" || pc["swing"] != 0.3 || pc["accent"] != 0.5 ||
		!reflect.DeepEqual(pc["sections"], []any{"chorus"}) {
		t.Errorf("perc после JSON туда-обратно %v", pc)
	}
	if pl, _ := pc["place"].(map[string]any); pl == nil || pl["pan"] != -0.4 || pl["width"] != 1.0 {
		t.Errorf("place perc %v, want pan −0,4, width 1 (умолчание места)", pc["place"])
	}
	if sy["kind"] != "synth" || sy["style"] != "drone" || sy["octave"] != -2.0 {
		t.Errorf("synth после JSON туда-обратно %v", sy)
	}
	if e, _ := sy["engine"].([]any); len(e) != 1 {
		t.Errorf("engine synth %v, want цепочку рецепта", sy["engine"])
	}
}

// ppChordErrFake — ppFake, у которого ChordGrid отвечает ошибкой err (внешняя граница: API воркера).
type ppChordErrFake struct {
	*ppFake
	err error
}

func (f *ppChordErrFake) ChordGrid(context.Context, int64) (*yue.ChordGrid, error) { return nil, f.err }

// ТК103а (условие 68а): ChordGrid → 404 (у трека нет плана) — это «аккордов нет», не ошибка пресета:
// synth пропущена, perc — по сетке долей JobGrid.
func TestPresetPartsChordGrid404NoChords(t *testing.T) {
	f := &ppChordErrFake{ppFake: ppSetup(t, 8, ppBars()),
		err: &yue.StatusError{Code: 404, Msg: "yue /chords: 404 Not Found: no plan"}}
	if _, err := ApplySoundPreset(context.Background(), f, psJobID, ppPreset(t, 45, ppPartsTK103)); err != nil {
		t.Fatalf("ApplySoundPreset при ChordGrid 404: %v, want без ошибки (404 = аккордов нет)", err)
	}
	if r := ppCall(t, f.ppFake, "synth"); r != nil {
		t.Errorf("synth-партия ушла в пересборку (%d нот), want пропуск: аккордов нет", len(ppNotes(t, r)))
	}
	perc := ppCall(t, f.ppFake, "perc")
	if perc == nil {
		t.Fatalf("perc-партии нет в пересборке, want удары по сетке долей")
	}
	ppCheckMixAdd(t, "perc", perc)
	if hits := ppNotes(t, perc); len(hits) != 8 {
		t.Errorf("ударов perc %d, want 8 (BarsFromBeat: 4 такта × 2 доли backbeat)", len(hits))
	}
	if f.gridCalls == 0 {
		t.Errorf("JobGrid не вызван, want сетка долей при 404 ChordGrid")
	}
}

// ТК103а (условие 68а): прочие ошибки ChordGrid (500) — ошибка ApplySoundPreset, как раньше.
func TestPresetPartsChordGrid500Error(t *testing.T) {
	f := &ppChordErrFake{ppFake: ppSetup(t, 8, ppBars()),
		err: &yue.StatusError{Code: 500, Msg: "yue /chords: 500 Internal Server Error: boom"}}
	if _, err := ApplySoundPreset(context.Background(), f, psJobID, ppPreset(t, 46, ppPartsTK103)); err == nil {
		t.Fatal("ApplySoundPreset при ChordGrid 500 без ошибки, want ошибку")
	}
}

// ppKitFailFake — ppFake, у которого установка набора badkit падает (внешняя граница: API воркера);
// попытки установки записываются в installs обёрнутого фейка.
type ppKitFailFake struct {
	*ppFake
}

func (f *ppKitFailFake) InstallFxKit(ctx context.Context, name string) (map[string]any, error) {
	if name == "badkit" {
		f.installs = append(f.installs, name)
		return nil, &yue.StatusError{Code: 502, Msg: "yue /fx/kits: 502 Bad Gateway: download failed"}
	}
	return f.ppFake.InstallFxKit(ctx, name)
}

// ТК103в (условие 68б): synth-партия только в секции solo (такой нет) с набором badkit, установка которого
// падает, + perc-партия: пропущенная партия набор не качает и пресет не роняет; perc идёт в пересборку.
func TestPresetPartsSkippedPartKitNotInstalled(t *testing.T) {
	f := &ppKitFailFake{ppFake: ppSetup(t, 8, ppBars())}
	p := ppPreset(t, 47, `[
 {"kind": "synth", "engine": [{"type": "synth", "kit": "badkit/x"}], "style": "pad", "sections": ["solo"]},
 {"kind": "perc", "engine": [{"type": "perc", "voice": 0}], "pattern": "backbeat"}
]`)
	if _, err := ApplySoundPreset(context.Background(), f, psJobID, p); err != nil {
		t.Fatalf("ApplySoundPreset: %v, want без ошибки (партия без нот свой набор не качает)", err)
	}
	if slices.Contains(f.installs, "badkit") {
		t.Errorf("InstallFxKit %q: badkit качался, want нет (synth-партия пропущена — нет нот)", f.installs)
	}
	if r := ppCall(t, f.ppFake, "synth"); r != nil {
		t.Errorf("synth без нот ушла в пересборку, want пропуск")
	}
	perc := ppCall(t, f.ppFake, "perc")
	if perc == nil {
		t.Fatal("perc-партии нет в пересборке")
	}
	ppCheckMixAdd(t, "perc", perc)
}
