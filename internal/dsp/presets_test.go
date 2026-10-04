package dsp

import (
	"encoding/json"
	"strings"
	"testing"
)

// Тесты карточки internal-guitar-pedals, условия 2.5 и 4.1: наборы педалей
// dsp.Presets() — {id, name, note, steps}; ссылаются только на существующие цепочки,
// без цепочек с ключом; каждый набор собирается в граф (StepsGraph). Написаны по
// карточке, без чтения реализации.

// Карточка 2.5: список наборов не пуст; id уникальны и не пусты; у каждого есть
// name и note и хотя бы один шаг.
func TestPresetsListWellFormed(t *testing.T) {
	ps := Presets()
	if len(ps) == 0 {
		t.Fatal("Presets() пуст, want хотя бы один набор")
	}
	seen := map[string]bool{}
	for i, p := range ps {
		if strings.TrimSpace(p.ID) == "" {
			t.Errorf("набор #%d: пустой id", i)
		}
		if seen[p.ID] {
			t.Errorf("id %q повторяется", p.ID)
		}
		seen[p.ID] = true
		if strings.TrimSpace(p.Name) == "" {
			t.Errorf("набор %q: пустое name", p.ID)
		}
		if strings.TrimSpace(p.Note) == "" {
			t.Errorf("набор %q: пустое note", p.ID)
		}
		if len(p.Steps) == 0 {
			t.Errorf("набор %q: нет шагов", p.ID)
		}
	}
}

// Карточка 4.1: наборы ссылаются только на существующие цепочки и не на цепочки
// с Key (шаги идут на одну дорожку/трек, второго входа нет); параметры шага —
// крутилки этой цепочки.
func TestPresetsReferenceExistingChains(t *testing.T) {
	for _, p := range Presets() {
		for j, s := range p.Steps {
			c := ByID(s.Chain)
			if c == nil {
				t.Errorf("набор %q, шаг %d: цепочки %q нет", p.ID, j, s.Chain)
				continue
			}
			if c.Key != "" {
				t.Errorf("набор %q, шаг %d: цепочка %q с ключом %q — в наборе нельзя", p.ID, j, s.Chain, c.Key)
			}
			for k := range s.Params {
				if !hasParam(c, k) {
					t.Errorf("набор %q, шаг %d (%s): параметра %q у цепочки нет", p.ID, j, s.Chain, k)
				}
			}
		}
	}
}

// Карточка 2.5/4.1: каждый набор собирается в граф без ошибки.
func TestPresetsBuildGraph(t *testing.T) {
	for _, p := range Presets() {
		g, _, err := StepsGraph(p.Steps)
		if err != nil {
			t.Errorf("набор %q: StepsGraph: %v", p.ID, err)
			continue
		}
		if g == "" {
			t.Errorf("набор %q: пустой граф", p.ID)
		}
	}
}

// Карточка 2.5/4.1: наборы реально звучат — граф проходит через ffmpeg (Run).
func TestPresetsRunOnSine(t *testing.T) {
	needFFmpeg(t)
	in, _ := genIn(t, "0.3*sin(2*PI*220*t)", 2)
	for _, p := range Presets() {
		t.Run(p.ID, func(t *testing.T) {
			g, _, err := StepsGraph(p.Steps)
			if err != nil {
				t.Fatalf("StepsGraph: %v", err)
			}
			out := stRunGraph(t, in, g)
			if d := secs(decode(t, out)); d < 1.9 {
				t.Errorf("выход %.2f с, want ≥ длины входа (2 с)", d)
			}
		})
	}
}

// Контракт: json-теги Preset — id/name/note/steps.
func TestPresetJSON(t *testing.T) {
	b, err := json.Marshal(Preset{ID: "x", Name: "Икс", Note: "заметка",
		Steps: []Step{{Chain: "od-ts"}}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "name", "note", "steps"} {
		if _, ok := m[k]; !ok {
			t.Errorf("json Preset без ключа %q: %s", k, b)
		}
	}
}
