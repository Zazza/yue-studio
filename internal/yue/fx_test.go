package yue

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// Тесты карточки internal-sound-engine, условие 4 (клиент воркера):
// ApplyFx — POST /jobs/{id}/fx с цепочкой как есть; FxAssets — GET /fx/assets;
// UploadFxAsset — POST /fx/assets?kind=&name= с сырыми байтами.

func TestApplyFxPostsChainAsIs(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/jobs/466/fx" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("тело не JSON: %v (%s)", err, b)
		}
		_, _ = w.Write([]byte(`{"file":"dsp-fx-vocals-0123abcd.flac","created_at":"2026-10-06T12:00:00",` +
			`"metrics":{"lufs":-14.2},"label":"голос через Plexi"}`))
	}))
	defer srv.Close()

	chain := []map[string]any{
		{"type": "eq", "highpass_hz": 80.0, "bands": []any{map[string]any{"freq_hz": 3000.0, "gain_db": 2.5, "q": 1.0}}},
		{"type": "amp", "model": "Plexi Lead.nam", "input_db": -6.0},
		{"type": "reverb", "wet": 0.2},
	}
	from, to := 10.5, 20.0
	v, err := New(srv.URL).ApplyFx(context.Background(), 466, FxRequest{
		Source: "vocals", Chain: chain, From: &from, To: &to, Output: "solo", Label: "голос через Plexi",
	})
	if err != nil {
		t.Fatalf("ApplyFx: %v", err)
	}
	if v == nil || v.File != "dsp-fx-vocals-0123abcd.flac" || v.Label != "голос через Plexi" || v.Metrics["lufs"] != -14.2 {
		t.Fatalf("вариант: %+v", v)
	}
	if got["source"] != "vocals" || got["output"] != "solo" || got["label"] != "голос через Plexi" {
		t.Errorf("поля запроса: %v", got)
	}
	if got["from"] != 10.5 || got["to"] != 20.0 {
		t.Errorf("окно: from=%v to=%v", got["from"], got["to"])
	}
	// цепочка уходит как есть: тот же JSON, что передан
	wantRaw, _ := json.Marshal(chain)
	var want any
	_ = json.Unmarshal(wantRaw, &want)
	if !reflect.DeepEqual(got["chain"], want) {
		t.Errorf("цепочка изменена:\n got %v\nwant %v", got["chain"], want)
	}
}

// Без окна и output — полей нет в теле (воркер берёт весь трек и output=mix).
func TestApplyFxOmitsEmptyOptional(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"file":"dsp-fx-mix-89abcdef.flac","created_at":"x","metrics":null}`))
	}))
	defer srv.Close()

	v, err := New(srv.URL).ApplyFx(context.Background(), 3, FxRequest{
		Source: "mix", Chain: []map[string]any{{"type": "comp"}},
	})
	if err != nil {
		t.Fatalf("ApplyFx: %v", err)
	}
	if v.File != "dsp-fx-mix-89abcdef.flac" {
		t.Fatalf("file = %q", v.File)
	}
	for _, k := range []string{"from", "to", "output", "label"} {
		if _, ok := got[k]; ok {
			t.Errorf("пустое поле %q ушло в тело: %v", k, got)
		}
	}
	if got["source"] != "mix" {
		t.Errorf("source = %v", got["source"])
	}
}

// Нулевое начало окна — не «нет окна»: from=0 уходит в тело.
func TestApplyFxSendsZeroFrom(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"file":"dsp-fx-bass-00000000.flac","created_at":"x","metrics":null}`))
	}))
	defer srv.Close()

	zero, to := 0.0, 5.0
	if _, err := New(srv.URL).ApplyFx(context.Background(), 3, FxRequest{
		Source: "bass", Chain: []map[string]any{{"type": "eq"}}, From: &zero, To: &to,
	}); err != nil {
		t.Fatalf("ApplyFx: %v", err)
	}
	if v, ok := got["from"]; !ok || v != 0.0 {
		t.Errorf("from=0 потерян: %v", got)
	}
}

// Ошибка воркера (422 с причиной) доходит до вызывающего.
func TestApplyFxErrorCarriesReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":"unknown block type: fuzz"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).ApplyFx(context.Background(), 1, FxRequest{
		Source: "mix", Chain: []map[string]any{{"type": "fuzz"}},
	})
	if err == nil {
		t.Fatal("ждали ошибку")
	}
	if !strings.Contains(err.Error(), "fuzz") {
		t.Errorf("в ошибке нет причины: %v", err)
	}
}

func TestFxAssetsGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/fx/assets" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"amps":[{"name":"Plexi Lead.nam","latency":3}],` +
			`"irs":[{"name":"room.wav","sr":48000,"seconds":0.5}]}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL).FxAssets(context.Background())
	if err != nil {
		t.Fatalf("FxAssets: %v", err)
	}
	amps, _ := got["amps"].([]any)
	irs, _ := got["irs"].([]any)
	if len(amps) != 1 || len(irs) != 1 {
		t.Fatalf("список: %v", got)
	}
	if a, _ := amps[0].(map[string]any); a["name"] != "Plexi Lead.nam" {
		t.Errorf("amps[0] = %v", amps[0])
	}
}

func TestUploadFxAssetSendsKindNameAndBytes(t *testing.T) {
	var kind, name string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/fx/assets" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		kind, name = r.URL.Query().Get("kind"), r.URL.Query().Get("name")
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"name":"Plexi Lead & Co.nam","kind":"amp"}`))
	}))
	defer srv.Close()

	data := []byte(`{"architecture":"WaveNet","weights":[1,2,3]}`)
	got, err := New(srv.URL).UploadFxAsset(context.Background(), "amp", "Plexi Lead & Co.nam", data)
	if err != nil {
		t.Fatalf("UploadFxAsset: %v", err)
	}
	// имя с пробелом и & доходит целиком — экранируется в query
	if kind != "amp" || name != "Plexi Lead & Co.nam" {
		t.Errorf("query: kind=%q name=%q", kind, name)
	}
	if string(body) != string(data) {
		t.Errorf("тело: %q, want %q", body, data)
	}
	if got["name"] != "Plexi Lead & Co.nam" || got["kind"] != "amp" {
		t.Errorf("ответ: %v", got)
	}
}

// Тесты карточки internal-instruments-page, тест-кейс 8: режим превью движка.
// FxRequest.Preview уходит в тело только при true; ответ превью
// {file, duration_sec, clipped} декодируется в вариант (поле File).

func TestApplyFxPreviewSentOnlyWhenTrue(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/jobs/7/fx" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		got = nil
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("тело не JSON: %v (%s)", err, b)
		}
		_, _ = w.Write([]byte(`{"file":"preview-fx-0123abcd.flac","duration_sec":18.0,"clipped":false}`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	from, to := 20.0, 35.0

	v, err := c.ApplyFx(context.Background(), 7, FxRequest{
		Source: "guitar", Chain: []map[string]any{{"type": "reverb"}}, From: &from, To: &to, Preview: true,
	})
	if err != nil {
		t.Fatalf("ApplyFx preview: %v", err)
	}
	if got["preview"] != true {
		t.Errorf("preview=true не ушёл в тело: %v", got)
	}
	if got["from"] != 20.0 || got["to"] != 35.0 {
		t.Errorf("окно превью: from=%v to=%v", got["from"], got["to"])
	}
	if v == nil || v.File != "preview-fx-0123abcd.flac" {
		t.Fatalf("ответ превью: %+v", v)
	}

	// без Preview — поля нет вовсе (обычный вариант этапа 2)
	if _, err := c.ApplyFx(context.Background(), 7, FxRequest{
		Source: "guitar", Chain: []map[string]any{{"type": "reverb"}},
	}); err != nil {
		t.Fatalf("ApplyFx: %v", err)
	}
	if _, ok := got["preview"]; ok {
		t.Errorf("preview=false ушёл в тело: %v", got)
	}
}

// Решение кросс-ревью internal-instruments-page: ответ превью
// {file, duration_sec, clipped} не теряет duration_sec и clipped.
func TestApplyFxPreviewKeepsDurationAndClipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"file":"preview-fx-89abcdef.flac","duration_sec":18.5,"clipped":true}`))
	}))
	defer srv.Close()
	from, to := 20.0, 35.0
	v, err := New(srv.URL).ApplyFx(context.Background(), 7, FxRequest{
		Source: "guitar", Chain: []map[string]any{{"type": "reverb"}}, From: &from, To: &to, Preview: true,
	})
	if err != nil {
		t.Fatalf("ApplyFx preview: %v", err)
	}
	if v == nil || v.File != "preview-fx-89abcdef.flac" {
		t.Fatalf("ответ превью: %+v", v)
	}
	if v.DurationSec != 18.5 {
		t.Errorf("duration_sec потерян: %v", v.DurationSec)
	}
	if !v.Clipped {
		t.Errorf("clipped=true потерян: %+v", v)
	}
}
