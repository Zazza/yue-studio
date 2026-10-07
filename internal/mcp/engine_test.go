package mcp

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-studio-engine, тест-кейс 8 (MCP, условие 7):
// rebuild_sections принимает в записи поле engine (массив блоков движка) →
// SectionSpec.Engine как есть; описание инструмента это говорит. Воркер — fxFake
// (fx_test.go: fakeService + ApplyFx), звук — синтетика ffmpeg (fxLavfi/fxDecode
// из stem_fx_test.go).

const engineSpecsJSON = `[
	{"from":20,"to":35,"stems":["other"],"engine":[
		{"type":"amp","model":"JCM2000.nam","input_db":-6},
		{"type":"cab","cutoff_hz":7000},
		{"type":"eq","bands":[{"freq_hz":3000,"gain_db":2.5,"q":1}]}]},
	{"from":1,"to":2,"stems":["drums"],"db":-100}
]`

// engineChainOf — цепочка из JSON первой записи (как её видит декодер: числа float64).
func engineChainOf(t *testing.T) []map[string]any {
	t.Helper()
	var specs []map[string]any
	if err := json.Unmarshal([]byte(engineSpecsJSON), &specs); err != nil {
		t.Fatal(err)
	}
	var chain []map[string]any
	for _, b := range specs[0]["engine"].([]any) {
		chain = append(chain, b.(map[string]any))
	}
	return chain
}

// ТК8: engine записи → SectionSpec.Engine как есть (вложенные bands тоже);
// запись без engine — Engine пустой, остальное как раньше.
func TestParseSectionSpecsEngine(t *testing.T) {
	var raw any
	if err := json.Unmarshal([]byte(engineSpecsJSON), &raw); err != nil {
		t.Fatal(err)
	}
	got, err := parseSectionSpecs(raw)
	if err != nil {
		t.Fatalf("parseSectionSpecs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("спек %d, want 2: %+v", len(got), got)
	}
	if want := engineChainOf(t); !reflect.DeepEqual(got[0].Engine, want) {
		t.Errorf("Engine = %v, want как в JSON %v", got[0].Engine, want)
	}
	if got[0].ChildID != 0 || got[0].From != 20 || got[0].To != 35 ||
		!reflect.DeepEqual(got[0].Stems, []string{"other"}) || got[0].Chain != "" {
		t.Errorf("запись движка: %+v, want child 0, 20–35, [other], без chain", got[0])
	}
	if len(got[1].Engine) != 0 || got[1].Db != -100 {
		t.Errorf("запись без engine: %+v, want Engine пустой, Db −100", got[1])
	}
}

// Краевой: engine не массив объектов — ошибка разбора (не молча без эффекта).
func TestParseSectionSpecsEngineNotArrayIsError(t *testing.T) {
	for _, bad := range []string{
		`[{"from":1,"to":2,"stems":["other"],"engine":"amp"}]`,
		`[{"from":1,"to":2,"stems":["other"],"engine":{"type":"amp"}}]`,
		`[{"from":1,"to":2,"stems":["other"],"engine":["amp"]}]`,
	} {
		var raw any
		if err := json.Unmarshal([]byte(bad), &raw); err != nil {
			t.Fatal(err)
		}
		if got, err := parseSectionSpecs(raw); err == nil {
			t.Errorf("%s: want ошибку, got %+v", bad, got)
		}
	}
}

// Описание rebuild_sections называет поле engine.
func TestRebuildSectionsDescribesEngine(t *testing.T) {
	s, _ := newTestServer(t)
	s.mu.RLock()
	tool := s.tools["rebuild_sections"]
	s.mu.RUnlock()
	b, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tool.Description+string(b), "engine") {
		t.Errorf("в описании rebuild_sections нет поля engine: %s / %s", tool.Description, b)
	}
}

// Сквозной: rebuild_sections с engine доходит до воркера — ApplyFx у трека с
// цепочкой как есть, на дорожку записи, solo + preview; вариант загружен.
func TestRebuildSectionsEngineCallsWorker(t *testing.T) {
	needFF(t)
	s, fake := newFxServer(t)
	fake.jobs = []yue.Job{{ID: 466, Status: "done", AudioFile: "audio.flac"}}
	const a3000, b500 = "0.3*sin(2*PI*3000*t)", "0.3*sin(2*PI*500*t)"
	fake.fetch = map[string]string{
		"audio.flac":       fxLavfi(t, a3000+"+"+b500, 8),
		"stem-vocals.flac": fxLavfi(t, a3000, 8),
		"stem-other.flac":  fxLavfi(t, b500, 8),
		"stem-drums.flac":  fxLavfi(t, "0", 8),
		"stem-bass.flac":   fxLavfi(t, "0", 8),
		// «обработанный кусок» — под именем, которое отдаёт fxFake.ApplyFx для превью
		// с pad — от начала трека, длина трека
		"preview-fx-0123abcd.flac": fxLavfi(t, "0.2*sin(2*PI*1000*t)", 8),
	}
	chain := `[{"type":"amp","model":"JCM2000.nam"},{"type":"cab"}]`
	out, ok := call(t, s, "rebuild_sections", jsonArgs(t,
		`{"job_id":466,"specs":[{"from":2,"to":5,"stems":["other"],"engine":`+chain+`}]}`))
	if !ok {
		t.Fatalf("rebuild_sections с engine: %s", out)
	}
	if len(fake.applies) != 1 {
		t.Fatalf("ApplyFx вызван %d раз, want 1", len(fake.applies))
	}
	a := fake.applies[0]
	var want []map[string]any
	_ = json.Unmarshal([]byte(chain), &want)
	if a.id != 466 || a.req.Source != "other" || a.req.Output != "solo" || !a.req.Preview {
		t.Errorf("ApplyFx(%d, source=%q output=%q preview=%v), want 466/other/solo/true",
			a.id, a.req.Source, a.req.Output, a.req.Preview)
	}
	if !reflect.DeepEqual(a.req.Chain, want) {
		t.Errorf("цепочка у воркера %v, want как в engine %v", a.req.Chain, want)
	}
	if len(fake.fakeService.uploads) != 1 {
		t.Errorf("загрузок %d, want 1 (вариант пересборки)", len(fake.fakeService.uploads))
	}
}

// Условие 8 через MCP: воркер отказал (движок выключен) — инструмент падает с
// причиной, ничего не загружено.
func TestRebuildSectionsEngineWorkerErrorIsError(t *testing.T) {
	needFF(t)
	s, fake := newFxServer(t)
	fake.applyErr = errors.New("fx engine disabled")
	fake.fetch = map[string]string{
		"audio.flac":      fxLavfi(t, "0.3*sin(2*PI*500*t)", 4),
		"stem-other.flac": fxLavfi(t, "0.3*sin(2*PI*500*t)", 4),
		"stem-drums.flac": fxLavfi(t, "0", 4),
		"stem-bass.flac":  fxLavfi(t, "0", 4),
	}
	out, ok := call(t, s, "rebuild_sections", jsonArgs(t,
		`{"job_id":466,"specs":[{"from":1,"to":2,"stems":["other"],"engine":[{"type":"reverb"}]}]}`))
	if ok {
		t.Fatalf("воркер отказал: want ошибку, ответ: %s", out)
	}
	if !strings.Contains(out, "fx engine disabled") {
		t.Errorf("ошибка %q без причины воркера", out)
	}
	if len(fake.fakeService.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", fake.fakeService.uploads)
	}
}

// Условия 2/8 через MCP: воркер без превью движка (/config без fx_preview) —
// rebuild_sections падает с текстом про обновление воркера, ApplyFx не вызван,
// ничего не загружено.
func TestRebuildSectionsEngineOldWorkerIsError(t *testing.T) {
	needFF(t)
	s, fake := newFxServer(t)
	fake.cfg = map[string]any{"ollama_url": "http://llm:11434"}
	fake.fetch = map[string]string{
		"audio.flac":      fxLavfi(t, "0.3*sin(2*PI*500*t)", 4),
		"stem-other.flac": fxLavfi(t, "0.3*sin(2*PI*500*t)", 4),
		"stem-drums.flac": fxLavfi(t, "0", 4),
		"stem-bass.flac":  fxLavfi(t, "0", 4),
	}
	out, ok := call(t, s, "rebuild_sections", jsonArgs(t,
		`{"job_id":466,"specs":[{"from":1,"to":2,"stems":["other"],"engine":[{"type":"reverb"}]}]}`))
	if ok {
		t.Fatalf("воркер без fx_preview: want ошибку, ответ: %s", out)
	}
	if low := strings.ToLower(out); !strings.Contains(low, "воркер") || !strings.Contains(low, "обнов") {
		t.Errorf("ошибка %q — want текст про обновление воркера", out)
	}
	if len(fake.applies) != 0 {
		t.Errorf("ApplyFx вызван %d раз, want 0", len(fake.applies))
	}
	if len(fake.fakeService.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", fake.fakeService.uploads)
	}
}
