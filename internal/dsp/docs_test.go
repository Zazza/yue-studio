package dsp

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// effectsDoc — справочник эффектов для пользователя: каждая цепочка там описана
// строкой с её ID в виде (`id`).
const effectsDoc = "../../docs/effects.md"

// Новая цепочка без описания в docs/effects.md — тест падает: справочник не
// отстаёт от кода. Число цепочек в тексте («Всего N цепочки») — тоже.
func TestEffectsDocListsEveryChain(t *testing.T) {
	raw, err := os.ReadFile(effectsDoc)
	if err != nil {
		t.Fatalf("справочник эффектов: %v", err)
	}
	doc := string(raw)
	for _, c := range All() {
		if !strings.Contains(doc, "(`"+c.ID+"`)") {
			t.Errorf("цепочка %s («%s») не описана в docs/effects.md — добавь строку с (`%s`)", c.ID, c.Name, c.ID)
		}
	}
	if want := fmt.Sprintf("Всего %d цепоч", len(All())); !strings.Contains(doc, want) {
		t.Errorf("в docs/effects.md нет «%s…»: число цепочек в тексте устарело", want)
	}
}
