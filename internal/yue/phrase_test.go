package yue

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// Тесты карточки internal-own-track, этап 13, условия 95–96 (тест-кейс ТК135, клиент):
// FxPhrases — GET /fx/phrases → [{id, family, name{ru,en}, bpm, cycle_sec}];
// FxPhrase — POST /fx/phrase {phrase, tempo, chain, stems, bypass} → {file, cycle_sec, clipped};
// FetchPhraseAudio — GET /fx/phrase/files/{file} → тело WAV.
// Написаны по карточке, без чтения реализации.

func TestFxPhrasesListsCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/fx/phrases" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[
			{"id":"gtr-arp","family":"guitar","name":{"ru":"Перебор","en":"Arpeggio"},"bpm":96,"cycle_sec":5},
			{"id":"bass-root","family":"bass","name":{"ru":"Бас","en":"Bass"},"bpm":120,"cycle_sec":2.5}
		]`))
	}))
	defer srv.Close()

	list, err := New(srv.URL).FxPhrases(context.Background())
	if err != nil {
		t.Fatalf("FxPhrases: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("фраз %d, want 2: %+v", len(list), list)
	}
	g, b := list[0], list[1]
	if g.ID != "gtr-arp" || g.Family != "guitar" || g.BPM != 96 || g.CycleSec != 5 {
		t.Errorf("фраза 0: %+v", g)
	}
	if !reflect.DeepEqual(g.Name, map[string]string{"ru": "Перебор", "en": "Arpeggio"}) {
		t.Errorf("name: %v", g.Name)
	}
	if b.ID != "bass-root" || b.Family != "bass" || b.BPM != 120 || b.CycleSec != 2.5 {
		t.Errorf("фраза 1: %+v", b)
	}
}

func TestFxPhrasePostsJSONAndDecodes(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/fx/phrase" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("тело не JSON: %v (%s)", err, body)
		}
		_, _ = w.Write([]byte(`{"file":"phrase-0123abcd.wav","cycle_sec":6.6667,"clipped":true}`))
	}))
	defer srv.Close()

	chain := []map[string]any{
		{"type": "amp", "model": "Plexi Lead.nam", "input_db": -6.0},
		{"type": "delay", "time_ms": 300.0, "feedback": 0.5},
	}
	res, err := New(srv.URL).FxPhrase(context.Background(), FxPhraseReq{
		Phrase: "gtr-arp", Tempo: 0.75, Chain: chain, Stems: []string{"guitar"}, Bypass: true,
	})
	if err != nil {
		t.Fatalf("FxPhrase: %v", err)
	}
	if res == nil || res.File != "phrase-0123abcd.wav" || res.CycleSec != 6.6667 || !res.Clipped {
		t.Fatalf("результат: %+v", res)
	}
	if got["phrase"] != "gtr-arp" || got["tempo"] != 0.75 || got["bypass"] != true {
		t.Errorf("поля запроса: %v", got)
	}
	if !reflect.DeepEqual(got["stems"], []any{"guitar"}) {
		t.Errorf("stems: %v", got["stems"])
	}
	// цепочка уходит как есть
	wantRaw, _ := json.Marshal(chain)
	var want any
	_ = json.Unmarshal(wantRaw, &want)
	if !reflect.DeepEqual(got["chain"], want) {
		t.Errorf("цепочка изменена:\n got %v\nwant %v", got["chain"], want)
	}
}

// Ответ без clipped (не пришлось ужимать) → Clipped=false.
func TestFxPhraseNotClipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"file":"phrase-x.wav","cycle_sec":4,"clipped":false}`))
	}))
	defer srv.Close()
	res, err := New(srv.URL).FxPhrase(context.Background(), FxPhraseReq{Phrase: "drums-rock", Tempo: 1})
	if err != nil {
		t.Fatalf("FxPhrase: %v", err)
	}
	if res.Clipped || res.CycleSec != 4 || res.File != "phrase-x.wav" {
		t.Errorf("результат: %+v", res)
	}
}

// Условие 95: ошибки воркера (404 неизвестная фраза, 422 темп, 503 движок) доходят до вызывающего.
func TestFxPhraseWorkerErrors(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"detail":"bad"}`))
		}))
		res, err := New(srv.URL).FxPhrase(context.Background(), FxPhraseReq{Phrase: "nope", Tempo: 2})
		srv.Close()
		if err == nil {
			t.Errorf("статус %d: ошибки нет, результат %+v", code, res)
		}
	}
}

func TestFetchPhraseAudioGetsFile(t *testing.T) {
	const wav = "RIFF....WAVEfmt fake-bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/fx/phrase/files/phrase-0123abcd.wav" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte(wav))
	}))
	defer srv.Close()

	rc, err := New(srv.URL).FetchPhraseAudio(context.Background(), "phrase-0123abcd.wav")
	if err != nil {
		t.Fatalf("FetchPhraseAudio: %v", err)
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)
	if string(b) != wav {
		t.Errorf("тело %q, want %q", b, wav)
	}
}

// Файл не из кэша (404) и имя с «/» — ошибка, а не пустое тело.
func TestFetchPhraseAudioErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	for _, f := range []string{"missing.wav", "../x", "a/b.wav"} {
		rc, err := New(srv.URL).FetchPhraseAudio(context.Background(), f)
		if err == nil {
			_ = rc.Close()
			t.Errorf("%q: ошибки нет", f)
		}
	}
}
