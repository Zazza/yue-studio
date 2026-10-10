package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-own-track, этап 13в, условие 104 (тест-кейс ТК141, MCP):
// инструмент fx_instruments (action list|create|update|delete) — свои инструменты воркера.
// Воркер — instrFake поверх fakeService (server_test.go).
// Допущение: аргументы инструмента названы как поля API воркера (id, name, base, group, stems, chain, extra);
// chain и stems передаются массивами JSON. Написаны по карточке, без чтения реализации.

type instrFake struct {
	*fakeService
	listed  int
	created []yue.FxInstrument
	updated []instrUpdate
	removed []int64
}

type instrUpdate struct {
	id int64
	in yue.FxInstrument
}

func (f *instrFake) FxInstruments(ctx context.Context) ([]yue.FxInstrument, error) {
	f.listed++
	return []yue.FxInstrument{
		{ID: 3, Name: "Мой перегруз", Base: "guitar-drive", Group: "guitar-drive", Stems: []string{"guitar"},
			Chain: []map[string]any{{"type": "drive", "gain_db": 12.0}}},
		{ID: 5, Name: "Мой пэд", Base: "synth-juno", Group: "synth-pad", Stems: []string{"synth"},
			Chain: []map[string]any{{"type": "synth", "osc1": 2.0}}},
	}, nil
}

func (f *instrFake) FxInstrumentCreate(ctx context.Context, in yue.FxInstrument) (*yue.FxInstrument, error) {
	f.created = append(f.created, in)
	out := in
	out.ID = 42
	return &out, nil
}

func (f *instrFake) FxInstrumentUpdate(ctx context.Context, id int64, in yue.FxInstrument) (*yue.FxInstrument, error) {
	f.updated = append(f.updated, instrUpdate{id, in})
	out := in
	out.ID = id
	return &out, nil
}

func (f *instrFake) FxInstrumentDelete(ctx context.Context, id int64) error {
	f.removed = append(f.removed, id)
	return nil
}

func newInstrServer(t *testing.T) (*Server, *instrFake) {
	t.Helper()
	s, base := newTestServer(t)
	fake := &instrFake{fakeService: base}
	s.client = fake
	return s, fake
}

func sameJSON(t *testing.T, got any, want string) bool {
	t.Helper()
	b, _ := json.Marshal(got)
	var g, w any
	_ = json.Unmarshal(b, &g)
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(g, w)
}

// ТК141: инструмент fx_instruments зарегистрирован и описан.
func TestFxInstrumentsToolRegistered(t *testing.T) {
	s, _ := newTestServer(t)
	s.mu.RLock()
	tool, ok := s.tools["fx_instruments"]
	s.mu.RUnlock()
	if !ok || tool.Name == "" {
		t.Fatal("инструмент fx_instruments не зарегистрирован")
	}
	if strings.TrimSpace(tool.Description) == "" {
		t.Error("у fx_instruments нет описания")
	}
}

// ТК141: action list зовёт FxInstruments, id и имена видны в выводе.
func TestFxInstrumentsToolList(t *testing.T) {
	s, fake := newInstrServer(t)
	out, ok := call(t, s, "fx_instruments", map[string]any{"action": "list"})
	if !ok {
		t.Fatalf("fx_instruments list failed: %s", out)
	}
	if fake.listed == 0 {
		t.Error("FxInstruments не вызван")
	}
	for _, s := range []string{"Мой перегруз", "Мой пэд"} {
		if !strings.Contains(out, s) {
			t.Errorf("в выводе нет %q: %s", s, out)
		}
	}
	if len(fake.created)+len(fake.updated)+len(fake.removed) != 0 {
		t.Error("list изменил данные")
	}
}

// ТК141: create передаёт name/chain/stems (и base/group) воркеру; id созданного — в выводе.
func TestFxInstrumentsToolCreate(t *testing.T) {
	s, fake := newInstrServer(t)
	chain := `[{"type":"eq","highpass_hz":80},{"type":"delay","time_ms":375,"wet":0.25}]`
	out, ok := call(t, s, "fx_instruments", jsonArgs(t,
		`{"action":"create","name":"Мой пэд","base":"synth-juno","group":"synth-pad","stems":["synth","other"],"chain":`+chain+`}`))
	if !ok {
		t.Fatalf("fx_instruments create failed: %s", out)
	}
	if len(fake.created) != 1 {
		t.Fatalf("FxInstrumentCreate вызван %d раз, want 1", len(fake.created))
	}
	in := fake.created[0]
	if in.Name != "Мой пэд" || in.Base != "synth-juno" || in.Group != "synth-pad" {
		t.Errorf("поля: %+v", in)
	}
	if !reflect.DeepEqual(in.Stems, []string{"synth", "other"}) {
		t.Errorf("stems: %v", in.Stems)
	}
	if !sameJSON(t, in.Chain, chain) {
		t.Errorf("цепочка %v, want %s", in.Chain, chain)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("в выводе нет id созданного: %s", out)
	}
}

// update по id: имя доходит до воркера с нужным id.
func TestFxInstrumentsToolUpdate(t *testing.T) {
	s, fake := newInstrServer(t)
	out, ok := call(t, s, "fx_instruments", jsonArgs(t, `{"action":"update","id":5,"name":"Новое имя"}`))
	if !ok {
		t.Fatalf("fx_instruments update failed: %s", out)
	}
	if len(fake.updated) != 1 {
		t.Fatalf("FxInstrumentUpdate вызван %d раз, want 1", len(fake.updated))
	}
	if u := fake.updated[0]; u.id != 5 || u.in.Name != "Новое имя" {
		t.Errorf("update: %+v", u)
	}
}

// delete по id.
func TestFxInstrumentsToolDelete(t *testing.T) {
	s, fake := newInstrServer(t)
	out, ok := call(t, s, "fx_instruments", jsonArgs(t, `{"action":"delete","id":5}`))
	if !ok {
		t.Fatalf("fx_instruments delete failed: %s", out)
	}
	if !reflect.DeepEqual(fake.removed, []int64{5}) {
		t.Errorf("удалены %v, want [5]", fake.removed)
	}
}

// Неизвестное действие — ошибка, воркер не трогается.
func TestFxInstrumentsToolUnknownAction(t *testing.T) {
	s, fake := newInstrServer(t)
	if out, ok := call(t, s, "fx_instruments", map[string]any{"action": "explode"}); ok {
		t.Errorf("неизвестное действие прошло: %s", out)
	}
	if len(fake.created)+len(fake.updated)+len(fake.removed) != 0 {
		t.Error("неизвестное действие изменило данные")
	}
}
