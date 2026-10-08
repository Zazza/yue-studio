package studio

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Тесты карточки internal-own-track, этап 4 «Синты», условие 25 (ТК49): запись движка
// с Add (SectionSpec.Add, json "add") — обработанный кусок кладётся ПОВЕРХ трека,
// исходная дорожка не вычитается; дорожка mix разрешена только с Add (синт не требует
// разделения). Запрос к воркеру для mix: ApplyFx(source mix, output solo, preview, pad).
// Написаны по карточке и контракту задачи, без чтения реализации.
//
// Способ — как в engine_test.go: фейк engFake отдаёт известный «кусок» (1000 Гц первую
// секунду, дальше 1500 Гц) файлом от начала трека (pad); по тонам дорожек родителя
// (vocals 3000, other 500, bass 200 Гц) видно, вычтено что-то из трека или нет.

const exprBass200 = "0.3*sin(2*PI*200*t)"

// synthChain — цепочка synth как из JSON (числа — float64).
func synthChain() []map[string]any {
	return []map[string]any{{
		"type":  "synth",
		"notes": []any{map[string]any{"t": 2.5, "d": 1.0, "midi": []any{69.0}, "vel": 1.0}},
		"osc1":  4.0,
	}}
}

// addSetup — родитель: трек = vocals 3000 + other 500 + bass 200 Гц, все дорожки файлами.
func addSetup(t *testing.T) (*engFake, []float32) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	pf := map[string]string{
		"audio.flac":       lavfi(t, aeval(exprA3000+"+"+exprB500+"+"+exprBass200, engDur), filepath.Join(dir, "a-audio.flac")),
		"stem-vocals.flac": lavfi(t, aeval(exprA3000, engDur), filepath.Join(dir, "a-vocals.flac")),
		"stem-other.flac":  lavfi(t, aeval(exprB500, engDur), filepath.Join(dir, "a-other.flac")),
		"stem-bass.flac":   lavfi(t, aeval(exprBass200, engDur), filepath.Join(dir, "a-bass.flac")),
		"stem-drums.flac":  lavfi(t, aeval("0", engDur), filepath.Join(dir, "a-drums.flac")),
	}
	put(f, parentID, pf)
	return newEngFake(t, f), decodeFile(t, pf["audio.flac"])
}

func addSpec(stems []string, add bool) SectionSpec {
	return SectionSpec{ChildID: 0, From: 2, To: 6, Stems: stems, Engine: synthChain(), Add: add}
}

// ТК49: Add с Engine на mix → один ApplyFx у родителя: source mix, output solo, preview, pad.
func TestRebuildSectionsAddMixRequest(t *testing.T) {
	f, _ := addSetup(t)
	engRun(t, f, addSpec([]string{"mix"}, true))
	if len(f.calls) != 1 {
		t.Fatalf("ApplyFx вызван %d раз, want 1", len(f.calls))
	}
	c := f.calls[0]
	if c.id != parentID {
		t.Errorf("ApplyFx у джобы %d, want %d (родитель)", c.id, parentID)
	}
	r := c.req
	// кросс-ревью s4: без Add воркер отдаёт «трек + (синт − кусок)» — края окна и хвост удваивали трек
	if !r.Add {
		t.Errorf("запрос без Add: воркер вернул бы трек вместе с партией")
	}
	if r.Source != "mix" || r.Output != "solo" || !r.Preview || !r.Pad {
		t.Errorf("FxRequest source=%q output=%q preview=%v pad=%v, want mix/solo/true/true",
			r.Source, r.Output, r.Preview, r.Pad)
	}
	if r.From == nil || *r.From > 2 || *r.From < 2-engFade-1e-9 {
		t.Errorf("FxRequest.From = %v, want окно записи (From = 2, не раньше From − фейд)", deref(r.From))
	}
	if r.To == nil || !near(*r.To, 6, 1e-9) {
		t.Errorf("FxRequest.To = %v, want 6", deref(r.To))
	}
	if len(r.Chain) != 1 || r.Chain[0]["type"] != "synth" {
		t.Errorf("FxRequest.Chain = %v, want цепочку записи (synth)", r.Chain)
	}
	// дорожки mix у воркера нет и быть не должно: её не скачивают и не разделяют
	if contains(f.fetched, key(parentID, "stem-mix.flac")) {
		t.Errorf("скачивалась stem-mix.flac: %v", f.fetched)
	}
	if f.stemsCalls[parentID] != 0 {
		t.Errorf("MakeStems вызван %d раз, want 0 (синт не требует разделения)", f.stemsCalls[parentID])
	}
}

// ТК49: в графе пересборки только +кусок — тоны трека в окне на месте, кусок звучит поверх.
func TestRebuildSectionsAddMixOnlyPlusChunk(t *testing.T) {
	f, base := addSetup(t)
	out := engRun(t, f, addSpec([]string{"mix"}, true))
	if d := float64(len(out)) / sr; !near(d, engDur, 0.05) {
		t.Errorf("длина выхода %.3f с, want %.0f ± 0.05", d, engDur)
	}
	// без вычитания: все дорожки трека в окне на прежнем уровне
	for _, hz := range []float64{3000, 500, 200} {
		if a := toneAmp(out, hz, 2.1, 5.9); !near(a, ampTone, 0.02) {
			t.Errorf("%.0f Гц в окне 2.1–5.9 с: %.4f, want ≈ %.2f (add: исходное не вычитается)", hz, a, ampTone)
		}
	}
	// +кусок: начало (1000 Гц) и продолжение (1500 Гц) звучат в окне
	if a := toneAmp(out, 1000, 2.1, 2.9); !near(a, engAmp, 0.03) {
		t.Errorf("кусок (1000 Гц) на 2.1–2.9 с: %.4f, want ≈ %.2f", a, engAmp)
	}
	if a := toneAmp(out, 1500, 3.2, 6.0); !near(a, engAmp, 0.03) {
		t.Errorf("кусок (1500 Гц) на 3.2–6.0 с: %.4f, want ≈ %.2f", a, engAmp)
	}
	// до окна — исходный трек
	if r := relDiffDb(out, base, 0, 1.9); r > -40 {
		t.Errorf("до окна разница с исходным %.1f дБ, want ≤ −40", r)
	}
}

// ТК49: mix без Add → ошибка пересборки, к воркеру не ходили, ничего не загружено.
func TestRebuildSectionsMixWithoutAddFails(t *testing.T) {
	f, _ := addSetup(t)
	_, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{addSpec([]string{"mix"}, false)})
	if err == nil {
		t.Fatal("mix без add: want ошибку, got nil")
	}
	if !strings.Contains(err.Error(), "mix") {
		t.Errorf("ошибка %q не называет дорожку mix", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("ApplyFx вызван %d раз, want 0", len(f.calls))
	}
	if len(f.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", keys(f.uploads))
	}
}

// ТК49: Add на дорожке bass → ApplyFx(source bass), +кусок, исходный bass НЕ вычтен.
func TestRebuildSectionsAddOnBassNoSubtract(t *testing.T) {
	f, _ := addSetup(t)
	out := engRun(t, f, addSpec([]string{"bass"}, true))
	if len(f.calls) != 1 || f.calls[0].req.Source != "bass" {
		t.Fatalf("ApplyFx: %+v, want 1 вызов для bass", f.calls)
	}
	if a := toneAmp(out, 200, 2.1, 5.9); !near(a, ampTone, 0.02) {
		t.Errorf("200 Гц (bass) в окне %.4f, want ≈ %.2f (add: без −bass)", a, ampTone)
	}
	if a := toneAmp(out, 1500, 3.2, 6.0); !near(a, engAmp, 0.03) {
		t.Errorf("кусок (1500 Гц) на 3.2–6.0 с: %.4f, want ≈ %.2f", a, engAmp)
	}
}

// Контроль: та же запись на bass без Add — по-прежнему замена (−bass): тест выше
// отличает add от обычной записи движка.
func TestRebuildSectionsBassWithoutAddSubtracts(t *testing.T) {
	f, _ := addSetup(t)
	out := engRun(t, f, addSpec([]string{"bass"}, false))
	if a := toneAmp(out, 200, 2.5, 5.5); a > 0.01 {
		t.Errorf("200 Гц (bass) в окне %.4f, want ≈ 0 (без add исходная дорожка вычитается)", a)
	}
}

// Поле add приходит из фронта/MCP по JSON; без add в JSON его нет (omitempty).
func TestSectionSpecAddJSON(t *testing.T) {
	var sp SectionSpec
	if err := json.Unmarshal([]byte(`{"child_id":0,"from":2,"to":6,"stems":["mix"],
		"engine":[{"type":"synth"}],"add":true}`), &sp); err != nil {
		t.Fatal(err)
	}
	if !sp.Add {
		t.Errorf("add:true → Add = false")
	}
	data, err := json.Marshal(SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"bass"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"add"`) {
		t.Errorf("без Add в JSON есть поле add: %s", data)
	}
}
