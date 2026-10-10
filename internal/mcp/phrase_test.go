package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-own-track, этап 13, условие 96 (тест-кейс ТК135, MCP):
// инструмент fx_phrase — список фраз / расчёт фразы с цепочкой → file, cycle_sec.
// Воркер — phraseFake поверх fakeService (server_test.go).
// Допущение: аргументы инструмента названы как поля API воркера (phrase, tempo, chain, stems,
// bypass); без phrase — список фраз. Написаны по карточке, без чтения реализации.

type phraseFake struct {
	*fakeService
	listed int
	reqs   []yue.FxPhraseReq
}

func (f *phraseFake) FxPhrases(ctx context.Context) ([]yue.FxPhrase, error) {
	f.listed++
	return []yue.FxPhrase{
		{ID: "gtr-arp", Family: "guitar", Name: map[string]string{"ru": "Перебор", "en": "Arpeggio"}, BPM: 96, CycleSec: 5},
		{ID: "drums-rock", Family: "drums", Name: map[string]string{"ru": "Рок", "en": "Rock"}, BPM: 120, CycleSec: 4},
	}, nil
}

func (f *phraseFake) FxPhrase(ctx context.Context, req yue.FxPhraseReq) (*yue.FxPhraseResult, error) {
	f.reqs = append(f.reqs, req)
	return &yue.FxPhraseResult{File: "phrase-0123abcd.wav", CycleSec: 6.25, Clipped: false}, nil
}

func newPhraseServer(t *testing.T) (*Server, *phraseFake) {
	t.Helper()
	s, base := newTestServer(t)
	fake := &phraseFake{fakeService: base}
	s.client = fake
	return s, fake
}

// ТК135: инструмент fx_phrase в списке инструментов.
func TestFxPhraseToolRegistered(t *testing.T) {
	s, _ := newTestServer(t)
	s.mu.RLock()
	tool, ok := s.tools["fx_phrase"]
	s.mu.RUnlock()
	if !ok || tool.Name == "" {
		t.Fatal("инструмент fx_phrase не зарегистрирован")
	}
	if strings.TrimSpace(tool.Description) == "" {
		t.Error("у fx_phrase нет описания")
	}
}

// Без фразы — список фраз воркера (id видны в выводе).
func TestFxPhraseToolListsPhrases(t *testing.T) {
	s, fake := newPhraseServer(t)
	out, ok := call(t, s, "fx_phrase", map[string]any{})
	if !ok {
		t.Fatalf("fx_phrase failed: %s", out)
	}
	if fake.listed == 0 {
		t.Error("FxPhrases не вызван")
	}
	for _, id := range []string{"gtr-arp", "drums-rock"} {
		if !strings.Contains(out, id) {
			t.Errorf("в выводе нет фразы %q: %s", id, out)
		}
	}
}

// С фразой и цепочкой — расчёт: запрос доходит до воркера, в выводе file и cycle_sec.
func TestFxPhraseToolRenders(t *testing.T) {
	s, fake := newPhraseServer(t)
	chain := `[{"type":"delay","time_ms":300,"feedback":0.5}]`
	out, ok := call(t, s, "fx_phrase", jsonArgs(t,
		`{"phrase":"gtr-arp","tempo":0.8,"chain":`+chain+`,"stems":["guitar"]}`))
	if !ok {
		t.Fatalf("fx_phrase failed: %s", out)
	}
	if len(fake.reqs) != 1 {
		t.Fatalf("FxPhrase вызван %d раз, want 1", len(fake.reqs))
	}
	r := fake.reqs[0]
	if r.Phrase != "gtr-arp" || r.Tempo != 0.8 || !reflect.DeepEqual(r.Stems, []string{"guitar"}) {
		t.Errorf("запрос %+v", r)
	}
	gotChain, _ := json.Marshal(r.Chain)
	var g, w any
	_ = json.Unmarshal(gotChain, &g)
	_ = json.Unmarshal([]byte(chain), &w)
	if !reflect.DeepEqual(g, w) {
		t.Errorf("цепочка %s, want %s", gotChain, chain)
	}
	if !strings.Contains(out, "phrase-0123abcd.wav") || !strings.Contains(out, "6.25") {
		t.Errorf("в выводе нет file/cycle_sec: %s", out)
	}
}
