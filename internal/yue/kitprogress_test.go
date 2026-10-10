package yue

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Тесты карточки internal-own-track, этап 14б, условие 117 (тест-кейс ТК145, клиент):
// FxKitProgress — GET /fx/kits/progress; ответ {name, part, done, total, bytes} — как есть;
// нет установки — {} (пустой ответ без ошибки); 500 — ошибка. Написаны по карточке, без чтения реализации.

func TestFxKitProgressGetsProgress(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"name":"vsco-violin","part":"pizz","done":7,"total":31,"bytes":12345678}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL).FxKitProgress(context.Background())
	if err != nil {
		t.Fatalf("FxKitProgress: %v", err)
	}
	if method != http.MethodGet || path != "/fx/kits/progress" {
		t.Errorf("запрос: %s %s, want GET /fx/kits/progress", method, path)
	}
	if got["name"] != "vsco-violin" || got["part"] != "pizz" || got["done"] != float64(7) ||
		got["total"] != float64(31) || got["bytes"] != float64(12345678) {
		t.Errorf("ответ: %v", got)
	}
}

func TestFxKitProgressIdleIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL).FxKitProgress(context.Background())
	if err != nil {
		t.Fatalf("FxKitProgress: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("нет установки — want пусто, got %v", got)
	}
}

func TestFxKitProgressServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).FxKitProgress(context.Background()); err == nil {
		t.Fatal("want ошибку на 500")
	}
}
