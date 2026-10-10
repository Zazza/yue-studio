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

// Тесты карточки internal-own-track, этап 13в, условие 104 (тест-кейс ТК141, клиент):
// FxInstruments — GET /fx/instruments → [{id, name, base, group, stems, chain, extra}];
// FxInstrumentCreate — POST /fx/instruments → созданный;
// FxInstrumentUpdate — PUT /fx/instruments/{id} → обновлённый;
// FxInstrumentDelete — DELETE /fx/instruments/{id} → {ok}.
// Ошибки воркера (404 нет id, 422 проверка) доходят до вызывающего.
// Написаны по карточке, без чтения реализации.

const instrJSON = `{"id":7,"name":"Мой пэд","base":"synth-juno","group":"synth-pad","stems":["synth"],
	"chain":[{"type":"eq","highpass_hz":80}],"extra":{"style":"pad","octave":1}}`

func sampleInstrument() FxInstrument {
	return FxInstrument{
		Name:  "Мой пэд",
		Base:  "synth-juno",
		Group: "synth-pad",
		Stems: []string{"synth"},
		Chain: []map[string]any{{"type": "eq", "highpass_hz": 80.0}},
		Extra: map[string]any{"style": "pad", "octave": 1.0},
	}
}

func checkDecoded(t *testing.T, got *FxInstrument) {
	t.Helper()
	if got == nil {
		t.Fatal("результат nil")
	}
	want := sampleInstrument()
	want.ID = 7
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("разбор:\n got %+v\nwant %+v", *got, want)
	}
}

// тело запроса как map — проверяется по именам полей API воркера
func readBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Errorf("тело не JSON: %v (%s)", err, b)
	}
	return m
}

func TestFxInstrumentsList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/fx/instruments" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[` + instrJSON + `,{"id":9,"name":"Второй","base":"perc-hat8","group":"","stems":["perc"],"chain":[{"type":"perc","voice":1}],"extra":{}}]`))
	}))
	defer srv.Close()

	list, err := New(srv.URL).FxInstruments(context.Background())
	if err != nil {
		t.Fatalf("FxInstruments: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("инструментов %d, want 2: %+v", len(list), list)
	}
	checkDecoded(t, &list[0])
	if list[1].ID != 9 || list[1].Name != "Второй" || !reflect.DeepEqual(list[1].Stems, []string{"perc"}) {
		t.Errorf("инструмент 1: %+v", list[1])
	}
}

// Пустой список воркера — пустой результат без ошибки.
func TestFxInstrumentsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	list, err := New(srv.URL).FxInstruments(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}

func TestFxInstrumentCreatePostsBody(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/fx/instruments" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		got = readBody(t, r)
		_, _ = w.Write([]byte(instrJSON))
	}))
	defer srv.Close()

	res, err := New(srv.URL).FxInstrumentCreate(context.Background(), sampleInstrument())
	if err != nil {
		t.Fatalf("FxInstrumentCreate: %v", err)
	}
	checkDecoded(t, res)
	if got["name"] != "Мой пэд" || got["base"] != "synth-juno" || got["group"] != "synth-pad" {
		t.Errorf("поля запроса: %v", got)
	}
	if !reflect.DeepEqual(got["stems"], []any{"synth"}) {
		t.Errorf("stems: %v", got["stems"])
	}
	if !reflect.DeepEqual(got["chain"], []any{map[string]any{"type": "eq", "highpass_hz": 80.0}}) {
		t.Errorf("chain: %v", got["chain"])
	}
	if !reflect.DeepEqual(got["extra"], map[string]any{"style": "pad", "octave": 1.0}) {
		t.Errorf("extra: %v", got["extra"])
	}
	// у нового id нет (omitempty): воркер назначает сам
	if _, ok := got["id"]; ok {
		t.Errorf("в теле create есть id: %v", got)
	}
}

func TestFxInstrumentUpdatePutsByID(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/fx/instruments/7" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		got = readBody(t, r)
		_, _ = w.Write([]byte(instrJSON))
	}))
	defer srv.Close()

	res, err := New(srv.URL).FxInstrumentUpdate(context.Background(), 7, sampleInstrument())
	if err != nil {
		t.Fatalf("FxInstrumentUpdate: %v", err)
	}
	checkDecoded(t, res)
	if got["name"] != "Мой пэд" {
		t.Errorf("name: %v", got)
	}
	if !reflect.DeepEqual(got["chain"], []any{map[string]any{"type": "eq", "highpass_hz": 80.0}}) {
		t.Errorf("chain: %v", got["chain"])
	}
}

func TestFxInstrumentDeleteByID(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if r.Method != http.MethodDelete || r.URL.Path != "/fx/instruments/7" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	if err := New(srv.URL).FxInstrumentDelete(context.Background(), 7); err != nil {
		t.Fatalf("FxInstrumentDelete: %v", err)
	}
	if called != 1 {
		t.Errorf("запросов %d, want 1", called)
	}
}

// Условие 103/104: 404 (нет id) и 422 (проверка) доходят до вызывающего ошибкой.
func TestFxInstrumentWorkerErrors(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusUnprocessableEntity} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"detail":"bad"}`))
		}))
		c := New(srv.URL)
		ctx := context.Background()
		if res, err := c.FxInstrumentCreate(ctx, sampleInstrument()); err == nil {
			t.Errorf("create, статус %d: ошибки нет, результат %+v", code, res)
		}
		if res, err := c.FxInstrumentUpdate(ctx, 999, sampleInstrument()); err == nil {
			t.Errorf("update, статус %d: ошибки нет, результат %+v", code, res)
		}
		if err := c.FxInstrumentDelete(ctx, 999); err == nil {
			t.Errorf("delete, статус %d: ошибки нет", code)
		}
		srv.Close()
	}
}
