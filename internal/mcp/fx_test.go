package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-sound-engine, тест-кейс 15 (MCP): fx_apply передаёт
// цепочку в Service как есть и возвращает вариант; fx_assets — список захватов
// и IR; fx_asset_upload — локальный файл с ПК уходит в UploadFxAsset.
// Воркер — fxFake: общий fakeService (server_test.go) + методы движка.

type fxApplyCall struct {
	id  int64
	req yue.FxRequest
}

type fxUploadCall struct {
	kind, name string
	data       []byte
}

type fxFake struct {
	*fakeService
	applies  []fxApplyCall
	applyErr error
	assets   map[string]any
	uploads  []fxUploadCall
}

func (f *fxFake) ApplyFx(ctx context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	f.applies = append(f.applies, fxApplyCall{id, req})
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	return &yue.DspVariant{File: "dsp-fx-" + req.Source + "-0123abcd.flac", CreatedAt: "2026-10-06T12:00:00",
		Metrics: map[string]any{"lufs": -14.2}, Label: req.Label}, nil
}

func (f *fxFake) FxAssets(ctx context.Context) (map[string]any, error) {
	return f.assets, nil
}

func (f *fxFake) UploadFxAsset(ctx context.Context, kind, name string, data []byte) (map[string]any, error) {
	f.uploads = append(f.uploads, fxUploadCall{kind, name, append([]byte(nil), data...)})
	return map[string]any{"name": name, "kind": kind}, nil
}

// newFxServer — сервер со всеми инструментами (как newTestServer), воркер — fxFake.
func newFxServer(t *testing.T) (*Server, *fxFake) {
	t.Helper()
	s, base := newTestServer(t)
	base.jobs = []yue.Job{{ID: 466, Status: "done", AudioFile: "audio.flac"}}
	fake := &fxFake{fakeService: base}
	s.client = fake
	return s, fake
}

const fxChainJSON = `[
	{"type":"eq","highpass_hz":80,"bands":[{"freq_hz":3000,"gain_db":2.5,"q":1}]},
	{"type":"amp","model":"Plexi Lead.nam","input_db":-6},
	{"type":"reverb","wet":0.2}
]`

func TestFxApplyPassesChainAsIs(t *testing.T) {
	s, fake := newFxServer(t)
	out, ok := call(t, s, "fx_apply", jsonArgs(t, `{"job_id":466,"source":"vocals","chain":`+fxChainJSON+
		`,"from":10.5,"to":20,"output":"solo","label":"голос через Plexi"}`))
	if !ok {
		t.Fatalf("fx_apply failed: %s", out)
	}
	if len(fake.applies) != 1 {
		t.Fatalf("ApplyFx вызван %d раз", len(fake.applies))
	}
	c := fake.applies[0]
	if c.id != 466 || c.req.Source != "vocals" || c.req.Output != "solo" || c.req.Label != "голос через Plexi" {
		t.Errorf("аргументы: id=%d %+v", c.id, c.req)
	}
	if c.req.From == nil || *c.req.From != 10.5 || c.req.To == nil || *c.req.To != 20 {
		t.Errorf("окно: from=%v to=%v", c.req.From, c.req.To)
	}
	// цепочка как есть: сравниваем по JSON с тем, что передал агент
	var want, got any
	_ = json.Unmarshal([]byte(fxChainJSON), &want)
	raw, _ := json.Marshal(c.req.Chain)
	_ = json.Unmarshal(raw, &got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("цепочка изменена:\n got %s\nwant %s", raw, fxChainJSON)
	}
	// ответ — вариант с именем файла
	if !strings.Contains(out, "dsp-fx-vocals-0123abcd.flac") {
		t.Errorf("в ответе нет имени варианта: %s", out)
	}
}

// Без окна/output/label — в запрос они не придумываются.
func TestFxApplyOptionalOmitted(t *testing.T) {
	s, fake := newFxServer(t)
	out, ok := call(t, s, "fx_apply", jsonArgs(t, `{"job_id":466,"source":"mix","chain":[{"type":"comp"}]}`))
	if !ok {
		t.Fatalf("fx_apply failed: %s", out)
	}
	if len(fake.applies) != 1 {
		t.Fatalf("ApplyFx вызван %d раз", len(fake.applies))
	}
	r := fake.applies[0].req
	if r.From != nil || r.To != nil {
		t.Errorf("окно придумано: from=%v to=%v", r.From, r.To)
	}
	if r.Source != "mix" || len(r.Chain) != 1 || r.Chain[0]["type"] != "comp" {
		t.Errorf("запрос: %+v", r)
	}
}

// Ошибка воркера (422 с причиной) — ошибка инструмента с той же причиной.
func TestFxApplyWorkerErrorIsToolError(t *testing.T) {
	s, fake := newFxServer(t)
	fake.applyErr = errors.New("422: unknown block type: fuzz")
	out, ok := call(t, s, "fx_apply", jsonArgs(t, `{"job_id":466,"source":"mix","chain":[{"type":"fuzz"}]}`))
	if ok {
		t.Fatalf("ждали ошибку, ответ: %s", out)
	}
	if !strings.Contains(out, "fuzz") {
		t.Errorf("в ошибке нет причины: %s", out)
	}
}

func TestFxAssetsReturnsList(t *testing.T) {
	s, fake := newFxServer(t)
	fake.assets = map[string]any{
		"amps": []any{map[string]any{"name": "Plexi Lead.nam", "latency": 3}},
		"irs":  []any{map[string]any{"name": "room.wav", "sr": 48000, "seconds": 0.5}},
	}
	out, ok := call(t, s, "fx_assets", map[string]any{})
	if !ok {
		t.Fatalf("fx_assets failed: %s", out)
	}
	for _, w := range []string{"Plexi Lead.nam", "room.wav"} {
		if !strings.Contains(out, w) {
			t.Errorf("в списке нет %q: %s", w, out)
		}
	}
}

func TestFxAssetUploadSendsLocalFile(t *testing.T) {
	s, fake := newFxServer(t)
	data := []byte(`{"architecture":"WaveNet","weights":[1,2,3]}`)
	p := filepath.Join(t.TempDir(), "Plexi Lead.nam")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"kind": "amp", "path": p})
	out, ok := call(t, s, "fx_asset_upload", jsonArgs(t, string(args)))
	if !ok {
		t.Fatalf("fx_asset_upload failed: %s", out)
	}
	if len(fake.uploads) != 1 {
		t.Fatalf("UploadFxAsset вызван %d раз", len(fake.uploads))
	}
	u := fake.uploads[0]
	if u.kind != "amp" || u.name != "Plexi Lead.nam" || string(u.data) != string(data) {
		t.Errorf("загрузка: kind=%q name=%q data=%q", u.kind, u.name, u.data)
	}
	if !strings.Contains(out, "Plexi Lead.nam") {
		t.Errorf("в ответе нет имени: %s", out)
	}
}

// Файла нет — ошибка, на воркер ничего не ушло.
func TestFxAssetUploadMissingFile(t *testing.T) {
	s, fake := newFxServer(t)
	args, _ := json.Marshal(map[string]any{"kind": "ir", "path": filepath.Join(t.TempDir(), "nope.wav")})
	out, ok := call(t, s, "fx_asset_upload", jsonArgs(t, string(args)))
	if ok {
		t.Fatalf("ждали ошибку, ответ: %s", out)
	}
	if len(fake.uploads) != 0 {
		t.Errorf("загружено при ошибке: %v", fake.uploads)
	}
}

// Тесты карточки internal-instruments-page, тест-кейс 9 (MCP): fx_apply передаёт
// preview; fx_blocks — описание блоков из встроенной копии worker/fx_blocks.json;
// fx_presets — готовые цепочки (встроенный fx_presets.json из frontend/src/fxPresets.js).

var fxBlockTypes = []string{"gate", "eq", "comp", "drive", "amp", "cab", "reverb", "delay"}

func TestFxApplyPassesPreview(t *testing.T) {
	s, fake := newFxServer(t)
	out, ok := call(t, s, "fx_apply", jsonArgs(t, `{"job_id":466,"source":"guitar","chain":[{"type":"reverb"}],`+
		`"from":20,"to":35,"output":"solo","preview":true}`))
	if !ok {
		t.Fatalf("fx_apply preview failed: %s", out)
	}
	if len(fake.applies) != 1 {
		t.Fatalf("ApplyFx вызван %d раз", len(fake.applies))
	}
	r := fake.applies[0].req
	if !r.Preview {
		t.Errorf("preview=true не передан: %+v", r)
	}
	if r.From == nil || *r.From != 20 || r.To == nil || *r.To != 35 || r.Output != "solo" {
		t.Errorf("окно/output превью: %+v", r)
	}
}

// Без preview (и при preview=false) — обычный вариант.
func TestFxApplyPreviewDefaultFalse(t *testing.T) {
	s, fake := newFxServer(t)
	for _, args := range []string{
		`{"job_id":466,"source":"mix","chain":[{"type":"comp"}]}`,
		`{"job_id":466,"source":"mix","chain":[{"type":"comp"}],"preview":false}`,
	} {
		if out, ok := call(t, s, "fx_apply", jsonArgs(t, args)); !ok {
			t.Fatalf("fx_apply failed: %s", out)
		}
	}
	for i, c := range fake.applies {
		if c.req.Preview {
			t.Errorf("вызов %d: preview придуман: %+v", i, c.req)
		}
	}
}

func TestFxBlocksListsAllTypesFromWorkerSource(t *testing.T) {
	s, _ := newFxServer(t)
	out, ok := call(t, s, "fx_blocks", map[string]any{})
	if !ok {
		t.Fatalf("fx_blocks failed: %s", out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("ответ не JSON-объект: %v (%s)", err, out)
	}
	for _, typ := range fxBlockTypes {
		b, _ := got[typ].(map[string]any)
		if b == nil {
			t.Errorf("нет блока %q", typ)
			continue
		}
		if ps, _ := b["params"].([]any); len(ps) == 0 {
			t.Errorf("у блока %q пустые params", typ)
		}
	}
	// содержимое — то же, что в источнике воркера (одна правда на всех клиентов)
	raw, err := os.ReadFile(filepath.Join("..", "..", "worker", "fx_blocks.json"))
	if err != nil {
		t.Fatalf("нет источника worker/fx_blocks.json: %v", err)
	}
	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("источник не JSON: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fx_blocks отличается от worker/fx_blocks.json")
	}
}

func TestFxPresetsReturnsList(t *testing.T) {
	s, _ := newFxServer(t)
	out, ok := call(t, s, "fx_presets", map[string]any{})
	if !ok {
		t.Fatalf("fx_presets failed: %s", out)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("ответ не JSON-список: %v (%s)", err, out)
	}
	if len(got) == 0 {
		t.Fatal("список пресетов пуст")
	}
	known := map[string]bool{}
	for _, typ := range fxBlockTypes {
		known[typ] = true
	}
	ids := map[string]bool{}
	for i, p := range got {
		id, _ := p["id"].(string)
		if id == "" || ids[id] {
			t.Errorf("пресет %d: пустой или повторный id %q", i, id)
		}
		ids[id] = true
		chain, _ := p["chain"].([]any)
		if len(chain) == 0 {
			t.Errorf("пресет %q: пустая цепочка", id)
		}
		for _, b := range chain {
			m, _ := b.(map[string]any)
			if typ, _ := m["type"].(string); !known[typ] {
				t.Errorf("пресет %q: неизвестный блок %v", id, m["type"])
			}
		}
	}
	// тот же набор, что встроенная копия fx_presets.json (make mcp-data из fxPresets.js)
	raw, err := os.ReadFile("fx_presets.json")
	if err != nil {
		t.Fatalf("нет internal/mcp/fx_presets.json (make mcp-data): %v", err)
	}
	var want []map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("fx_presets.json не JSON-список: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fx_presets отличается от встроенного fx_presets.json")
	}
}

// Решение кросс-ревью internal-instruments-page: ответ fx_apply в режиме превью
// несёт duration_sec и clipped от воркера (страница и агент видят длину куска
// и перегруз). Воркер — fxPreviewFake, отдающий вариант превью.

type fxPreviewFake struct {
	*fxFake
}

func (f *fxPreviewFake) ApplyFx(ctx context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	f.applies = append(f.applies, fxApplyCall{id, req})
	return &yue.DspVariant{File: "preview-fx-0123abcd.flac", DurationSec: 18.5, Clipped: true}, nil
}

func TestFxApplyReturnsDurationAndClipped(t *testing.T) {
	s, base := newFxServer(t)
	s.client = &fxPreviewFake{fxFake: base}
	out, ok := call(t, s, "fx_apply", jsonArgs(t, `{"job_id":466,"source":"guitar","chain":[{"type":"reverb"}],`+
		`"from":20,"to":35,"output":"solo","preview":true}`))
	if !ok {
		t.Fatalf("fx_apply preview failed: %s", out)
	}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`"file"\s*:\s*"preview-fx-0123abcd\.flac"`),
		regexp.MustCompile(`"duration_sec"\s*:\s*18\.5\b`),
		regexp.MustCompile(`"clipped"\s*:\s*true\b`),
	} {
		if !re.MatchString(out) {
			t.Errorf("в ответе fx_apply нет %s: %s", re, out)
		}
	}
}
