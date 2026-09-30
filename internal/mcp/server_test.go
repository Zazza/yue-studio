package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// fakeService — мок yue.Service для инструментного слоя MCP.
type fakeService struct {
	yue.Service
	url       string
	health    *yue.HealthInfo
	jobs      []yue.Job
	submitted []yue.SubmitParams
	canceled  []int64
	deleted   []int64
	voices    []yue.Voice
	voiceDel  []int64
	fetch     map[string]string // file -> content
	fetched   []string          // какие файлы запрашивались через FetchAudio

	// ContinueJob: записанные вызовы; id новых джоб — 500, 501, …
	continued []continueCall

	// VocalContour: настраиваемый ответ и записанные аргументы
	contour      *yue.VocalContour
	contourErr   error
	contourCalls []contourCall

	// VariantToTrack: записанные вызовы
	promoted []promoteCall

	// Волна студии: настраиваемые ответы job_peaks / job_spectrum
	peaksOut    map[string]any
	spectrumOut []byte

	// JobTones («найти свист»): настраиваемый ответ и записанные аргументы (со stem)
	tonesOut   []yue.Tone
	tonesCalls []tonesCall

	// UploadDsp: загруженные варианты (эффект на дорожку идёт через пересборку)
	uploads map[string][]byte
}

type tonesCall struct {
	ID       int64
	From, To float64
	Stem     string
}

type promoteCall struct {
	jobID       int64
	file, title string
	voiceSrc    int64
}

type continueCall struct {
	JobID    int64
	From     float64
	Seed     int64
	Abc      string
	StyleAdd string
}

type contourCall struct {
	ID       int64
	From, To float64
}

func (f *fakeService) Health(ctx context.Context) (*yue.HealthInfo, error) { return f.health, nil }
func (f *fakeService) Jobs(ctx context.Context) ([]yue.Job, error)         { return f.jobs, nil }

func (f *fakeService) Submit(ctx context.Context, p yue.SubmitParams) (int64, error) {
	f.submitted = append(f.submitted, p)
	return int64(len(f.submitted)), nil
}

func (f *fakeService) Cancel(ctx context.Context, id int64) (bool, error) {
	f.canceled = append(f.canceled, id)
	return true, nil
}

func (f *fakeService) DeleteJob(ctx context.Context, id int64) (bool, error) {
	f.deleted = append(f.deleted, id)
	return true, nil
}

func (f *fakeService) Voices(ctx context.Context) ([]yue.Voice, error) { return f.voices, nil }

func (f *fakeService) VoiceCreate(ctx context.Context, name string, jobID int64, params string, seed int64) (int64, error) {
	f.voices = append(f.voices, yue.Voice{ID: int64(len(f.voices)) + 1, Name: name,
		JobID: jobID, Params: params, Seed: seed, JobAlive: true, HasAudio: true})
	return int64(len(f.voices)), nil
}

func (f *fakeService) VoiceDelete(ctx context.Context, id int64) (bool, error) {
	f.voiceDel = append(f.voiceDel, id)
	return true, nil
}

func (f *fakeService) VariantToTrack(ctx context.Context, jobID int64, file, title string, voiceSrc int64) (int64, error) {
	f.promoted = append(f.promoted, promoteCall{jobID, file, title, voiceSrc})
	return 77, nil
}

func (f *fakeService) ContinueJob(ctx context.Context, jobID int64, fromSec float64, seed int64, abc, styleAdd string) (int64, error) {
	f.continued = append(f.continued, continueCall{jobID, fromSec, seed, abc, styleAdd})
	return 500 + int64(len(f.continued)) - 1, nil
}

func (f *fakeService) VocalContour(ctx context.Context, id int64, from, to float64) (*yue.VocalContour, error) {
	f.contourCalls = append(f.contourCalls, contourCall{id, from, to})
	if f.contourErr != nil {
		return nil, f.contourErr
	}
	return f.contour, nil
}

func (f *fakeService) JobTones(ctx context.Context, id int64, from, to float64, stem string) ([]yue.Tone, error) {
	f.tonesCalls = append(f.tonesCalls, tonesCall{id, from, to, stem})
	return f.tonesOut, nil
}

func (f *fakeService) UploadDsp(ctx context.Context, id int64, fname string, data []byte) (*yue.DspVariant, error) {
	if f.uploads == nil {
		f.uploads = map[string][]byte{}
	}
	f.uploads[fname] = append([]byte(nil), data...)
	return &yue.DspVariant{File: fname}, nil
}

func (f *fakeService) SetHead(ctx context.Context, jobID, headID int64) error { return nil }

func (f *fakeService) JobPeaks(ctx context.Context, id int64, file string, bins int) (map[string]any, error) {
	return f.peaksOut, nil
}

func (f *fakeService) JobSpectrum(ctx context.Context, id int64, file string) ([]byte, error) {
	return f.spectrumOut, nil
}

func (f *fakeService) DspVariantDelete(ctx context.Context, id int64, fname string) (bool, error) {
	return true, nil
}

func (f *fakeService) FetchAudio(ctx context.Context, id int64, file string) (io.ReadCloser, string, error) {
	f.fetched = append(f.fetched, file)
	if c, ok := f.fetch[file]; ok {
		return io.NopCloser(strings.NewReader(c)), "application/octet-stream", nil
	}
	return nil, "", errNotFound
}

func (f *fakeService) GetURL() string  { return f.url }
func (f *fakeService) SetURL(u string) { f.url = u }

var errNotFound = &notFoundErr{}

type notFoundErr struct{}

func (*notFoundErr) Error() string { return "not found" }

func newTestServer(t *testing.T) (*Server, *fakeService) {
	t.Helper()
	fake := &fakeService{url: "http://w:8091"}
	s := NewServer(fake, t.TempDir())
	RegisterWorkflowTools(s)
	RegisterStudioTools(s)
	RegisterLibraryTools(s)
	RegisterInstallTools(s)
	return s, fake
}

// call имитирует tools/call через диспетчер (без stdio).
func call(t *testing.T, s *Server, name string, args map[string]any) (string, bool) {
	t.Helper()
	var tools Tool
	s.mu.RLock()
	tools = s.tools[name]
	s.mu.RUnlock()
	if tools.Name == "" {
		t.Fatalf("tool %q not registered", name)
	}
	out, err := tools.Handler(s, args)
	if err != nil {
		return err.Error(), false
	}
	return out, true
}

func TestSubmitBuildsParams(t *testing.T) {
	s, fake := newTestServer(t)
	out, ok := call(t, s, "submit", map[string]any{
		"style": "doom metal, slow", "lyrics": "[Instrumental]", "title": "тёмное", "seed": 7,
	})
	if !ok {
		t.Fatalf("submit failed: %s", out)
	}
	if len(fake.submitted) != 1 {
		t.Fatalf("submitted %d", len(fake.submitted))
	}
	p := fake.submitted[0]
	if p.Style != "doom metal, slow" || p.Lyrics != "[Instrumental]" || p.Seed != 7 || p.Title != "тёмное" {
		t.Fatalf("params: %+v", p)
	}
	// cot по умолчанию full
	if p.Cot != "full" {
		t.Fatalf("cot = %q", p.Cot)
	}
}

func TestSubmitFanSeedsAndTitles(t *testing.T) {
	s, fake := newTestServer(t)
	out, ok := call(t, s, "submit", map[string]any{
		"style": "punk", "lyrics": "[Instrumental]", "seed": 10, "n": 3, "title": "x",
	})
	if !ok {
		t.Fatalf("submit failed: %s", out)
	}
	if len(fake.submitted) != 3 {
		t.Fatalf("submitted %d", len(fake.submitted))
	}
	for i, p := range fake.submitted {
		if p.Seed != 10+int64(i) {
			t.Fatalf("seed[%d] = %d", i, p.Seed)
		}
		if want := "x [1/3]"[:0] + strings.Replace("x [i/3]", "i", string(rune('1'+i)), 1); p.Title != want {
			t.Fatalf("title[%d] = %q, want %q", i, p.Title, want)
		}
	}
}

func TestDeleteRequiresConfirm(t *testing.T) {
	s, fake := newTestServer(t)
	out, ok := call(t, s, "delete_job", map[string]any{"job_id": 5})
	if ok {
		t.Fatal("delete without confirm should fail")
	}
	if !strings.Contains(out, "подтверждение") {
		t.Fatalf("message: %s", out)
	}
	if len(fake.deleted) != 0 {
		t.Fatal("no delete expected")
	}
	out, ok = call(t, s, "delete_job", map[string]any{"job_id": 5, "confirm": true})
	if !ok {
		t.Fatalf("delete failed: %s", out)
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != 5 {
		t.Fatalf("deleted: %v", fake.deleted)
	}
}

func TestCancelRequiresConfirm(t *testing.T) {
	s, fake := newTestServer(t)
	if _, ok := call(t, s, "cancel", map[string]any{"job_id": 1}); ok {
		t.Fatal("cancel without confirm should fail")
	}
	if _, ok := call(t, s, "cancel", map[string]any{"job_id": 1, "confirm": true}); !ok {
		t.Fatal("cancel with confirm should succeed")
	}
	if len(fake.canceled) != 1 {
		t.Fatalf("canceled: %v", fake.canceled)
	}
}

func TestVoiceDeleteRequiresConfirm(t *testing.T) {
	s, fake := newTestServer(t)
	if _, ok := call(t, s, "voice_delete", map[string]any{"voice_id": 3}); ok {
		t.Fatal("voice_delete without confirm should fail")
	}
	if len(fake.voiceDel) != 0 {
		t.Fatal("no delete expected")
	}
	if _, ok := call(t, s, "voice_delete", map[string]any{"voice_id": 3, "confirm": true}); !ok {
		t.Fatal("voice_delete with confirm should succeed")
	}
	if len(fake.voiceDel) != 1 || fake.voiceDel[0] != 3 {
		t.Fatalf("voiceDel: %v", fake.voiceDel)
	}
}

func TestVoiceCreateSavesParamsAndSeed(t *testing.T) {
	s, fake := newTestServer(t)
	out, ok := call(t, s, "voice_create", map[string]any{
		"name": "бархатный хрип", "job_id": 7,
		"params": `{"register":"male-low","rough":2}`, "seed": 42,
	})
	if !ok {
		t.Fatalf("voice_create failed: %s", out)
	}
	if len(fake.voices) != 1 {
		t.Fatalf("voices: %+v", fake.voices)
	}
	v := fake.voices[0]
	if v.Name != "бархатный хрип" || v.JobID != 7 || v.Seed != 42 {
		t.Fatalf("voice: %+v", v)
	}
	if !strings.Contains(v.Params, "male-low") {
		t.Fatalf("params: %s", v.Params)
	}
	// сохранённый голос виден в списке
	out, ok = call(t, s, "voices_list", nil)
	if !ok || !strings.Contains(out, "бархатный хрип") {
		t.Fatalf("voices_list: %s", out)
	}
}

func TestArtifactsPicksBestFile(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{{ID: 9, AudioFile: "audio.flac", Mp3File: "audio.mp3"}}
	fake.fetch = map[string]string{"audio.mp3": "mp3-bytes"}
	out, ok := call(t, s, "artifacts", map[string]any{"job_id": 9})
	if !ok {
		t.Fatalf("artifacts failed: %s", out)
	}
	if !strings.Contains(out, "yue-9-audio.mp3") {
		t.Fatalf("out: %s", out)
	}
}

func TestRenderAbcForcesMelodyCot(t *testing.T) {
	s, fake := newTestServer(t)
	_, ok := call(t, s, "render_abc", map[string]any{
		"abc": "X:1", "style": "blues", "cot": "off",
	})
	if !ok {
		t.Fatal("render_abc failed")
	}
	if fake.submitted[0].Cot != "melody" || fake.submitted[0].Abc != "X:1" {
		t.Fatalf("params: %+v", fake.submitted[0])
	}
}

func TestLibraryDataEmbedded(t *testing.T) {
	// spec: данные библиотеки вшиты в бинарник и непусты
	if groups := loadStyleGroups(); len(groups) < 20 {
		t.Fatalf("style groups = %d (ожидались встроенные + пресеты)", len(groups))
	}
	opts := loadSlotOptions()
	for _, slot := range []string{"genre", "vocals", "production"} {
		if len(opts[slot]) == 0 {
			t.Fatalf("slot %s пуст", slot)
		}
	}
}

// ---------- протокол (stdio JSON-RPC) ----------

func rpcRoundTrip(t *testing.T, s *Server, reqs []string) []string {
	t.Helper()
	var in bytes.Buffer
	for _, r := range reqs {
		in.WriteString(r + "\n")
	}
	var out bytes.Buffer
	if err := s.Serve(&in, &out); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestProtocolHandshakeAndToolsList(t *testing.T) {
	s, _ := newTestServer(t)
	lines := rpcRoundTrip(t, s, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	})
	if len(lines) != 2 {
		t.Fatalf("responses: %d", len(lines))
	}
	var init struct {
		Result struct {
			ProtocolVersion string                `json:"protocolVersion"`
			ServerInfo      struct{ Name string } `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &init); err != nil {
		t.Fatal(err)
	}
	if init.Result.ProtocolVersion == "" || init.Result.ServerInfo.Name != "yue-studio" {
		t.Fatalf("init: %+v", init)
	}
	var list struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &list); err != nil {
		t.Fatal(err)
	}
	// spec: полный рабочий набор — флоу, студия, библиотека, установка
	var have = map[string]bool{}
	for _, tl := range list.Result.Tools {
		have[tl.Name] = true
	}
	for _, want := range []string{
		"status", "jobs", "submit", "plan", "render_abc", "cancel", "delete_job", "artifacts",
		"transcribe", "job_score", "job_preview",
		"config_get", "config_set", "dsp_chains", "dsp_apply", "dsp_preview", "dsp_variants",
		"analyze_job", "make_stems", "make_minus", "overdub", "import_track",
		"job_peaks", "job_spectrum",
		"variant_track", "dsp_variant_delete",
		"corpus_list", "corpus_create", "corpus_add_tracks", "corpus_build", "corpus_get",
		"voices_list", "voice_create", "voice_delete",
		"styles", "slot_options", "doctor", "install_worker", "install_app",
	} {
		if !have[want] {
			t.Fatalf("tool %q не зарегистрирован", want)
		}
	}
}

func TestProtocolToolsCall(t *testing.T) {
	s, fake := newTestServer(t)
	fake.health = &yue.HealthInfo{Status: "ok", ModelLoaded: true}
	lines := rpcRoundTrip(t, s, []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"status","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"no-such-tool","arguments":{}}}`,
	})
	if len(lines) != 2 {
		t.Fatalf("responses: %d", len(lines))
	}
	var ok struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(lines[0]), &ok)
	if !strings.Contains(ok.Result.Content[0].Text, "model_loaded") {
		t.Fatalf("status text: %s", ok.Result.Content[0].Text)
	}
	if !strings.Contains(lines[1], "unknown tool") {
		t.Fatalf("unknown tool response: %s", lines[1])
	}
}

func TestProtocolPingAndNotifications(t *testing.T) {
	s, _ := newTestServer(t)
	lines := rpcRoundTrip(t, s, []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":9,"method":"ping"}`,
	})
	if len(lines) != 1 || !strings.Contains(lines[0], `"id":9`) {
		t.Fatalf("ping responses: %v", lines)
	}
}

// Схема аргументов каждого инструмента — объект с properties-объектом (не null):
// иначе Claude Code отвергает tools/list целиком.
func TestToolSchemasHaveObjectProperties(t *testing.T) {
	s := NewServer(&fakeService{}, t.TempDir())
	RegisterWorkflowTools(s)
	RegisterStudioTools(s)
	RegisterLibraryTools(s)
	RegisterInstallTools(s)
	for _, name := range s.toolOrder {
		raw, err := json.Marshal(s.tools[name].InputSchema)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var sc struct {
			Type       string          `json:"type"`
			Properties json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &sc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if sc.Type != "object" || len(sc.Properties) == 0 || sc.Properties[0] != '{' {
			t.Errorf("%s: схема %s — нужен type object и properties-объект", name, raw)
		}
	}
}

// Версия после «перепеть»/пересборки: файл варианта, название (своё или по
// умолчанию) и источник голоса доходят до VariantToTrack как есть.
func TestPromoteVersion(t *testing.T) {
	f := &fakeService{}
	if id, err := promoteVersion(f, 214, "overdub-inst-243.flac", "", "по умолчанию", 243); err != nil || id != 77 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	if _, err := promoteVersion(f, 214, "overdub-inst-5.flac", "моё", "по умолчанию", 0); err != nil {
		t.Fatal(err)
	}
	want := []promoteCall{{214, "overdub-inst-243.flac", "по умолчанию", 243}, {214, "overdub-inst-5.flac", "моё", 0}}
	if !reflect.DeepEqual(f.promoted, want) {
		t.Errorf("вызовы %+v, want %+v", f.promoted, want)
	}
}

// Волна студии: job_peaks отдаёт JSON воркера как есть (агент ищет провалы
// числами), job_spectrum кладёт PNG в каталог загрузок и возвращает путь.
func TestJobPeaksTool(t *testing.T) {
	s, fake := newTestServer(t)
	fake.peaksOut = map[string]any{"_v": 1, "file": "audio.flac", "bins": 2400,
		"duration_sec": 210.0, "peaks": [][]float64{{-0.5, 0.5}}}
	out, ok := call(t, s, "job_peaks", map[string]any{"job_id": 204})
	if !ok {
		t.Fatalf("job_peaks: %s", out)
	}
	if !strings.Contains(out, `"bins"`) || !strings.Contains(out, `"duration_sec"`) {
		t.Fatalf("job_peaks должен вернуть bins/duration_sec: %s", out)
	}
}

func TestJobSpectrumToolSavesPNG(t *testing.T) {
	s, fake := newTestServer(t)
	fake.spectrumOut = []byte("\x89PNG-fake")
	out, ok := call(t, s, "job_spectrum", map[string]any{"job_id": 204})
	if !ok {
		t.Fatalf("job_spectrum: %s", out)
	}
	if !strings.Contains(out, "сохранено: ") {
		t.Fatalf("job_spectrum должен вернуть путь: %s", out)
	}
	path := strings.TrimPrefix(out, "сохранено: ")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(b) != "\x89PNG-fake" {
		t.Fatalf("png round-trip: %q", b)
	}
}
