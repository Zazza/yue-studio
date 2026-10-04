package mcp

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-dsp-space, условия 6.5 и 10.5 (MCP): цепочка с ключом
// (ducking, Key drums) не применяется на весь трек — понятная ошибка до скачивания
// и ffmpeg; dsp_chains отдаёт новые цепочки и поле key; описания упоминают key.

// Карточка 6.5: dsp_apply ducking без stem — ошибка «только эффект на дорожку», до
// ffmpeg (ничего не скачано и не загружено).
func TestDspApplyKeyChainWholeTrackIsError(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{{ID: 5, Status: "done", AudioFile: "audio.flac"}}
	fake.fetch = map[string]string{"audio.flac": "not-audio"}
	out, ok := call(t, s, "dsp_apply", jsonArgs(t, `{"job_id":5,"chain":"ducking"}`))
	if ok {
		t.Fatalf("ducking на весь трек: want ошибку, ответ: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "дорожк") {
		t.Errorf("ошибка непонятна (нет «дорожк…»): %s", out)
	}
	if len(fake.fetched) != 0 {
		t.Errorf("до ошибки скачано %v — проверка должна быть до ffmpeg", fake.fetched)
	}
	if len(fake.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", fake.uploads)
	}
}

// Карточка 10.5: dsp_chains отдаёт новые цепочки (dsp.All) и key у ducking.
func TestDspChainsListsNewChainsAndKey(t *testing.T) {
	s, _ := newTestServer(t)
	out, ok := call(t, s, "dsp_chains", map[string]any{})
	if !ok {
		t.Fatalf("dsp_chains: %s", out)
	}
	for _, id := range []string{"reverb-hall", "delay", "haas", "eq", "sweep", "ducking", "pitch", "vinyl"} {
		if !strings.Contains(out, `"`+id+`"`) {
			t.Errorf("dsp_chains без цепочки %s", id)
		}
	}
	if !regexp.MustCompile(`"key":\s*"drums"`).MatchString(out) {
		t.Errorf("dsp_chains: у ducking нет \"key\":\"drums\"")
	}
}

// Карточка 10.5: описания dsp_chains и dsp_apply упоминают key-цепочки.
func TestDspToolsDescribeKeyChains(t *testing.T) {
	s, _ := newTestServer(t)
	for _, name := range []string{"dsp_chains", "dsp_apply"} {
		s.mu.RLock()
		tool := s.tools[name]
		s.mu.RUnlock()
		// описание инструмента или его параметров (stem у dsp_apply)
		if text := tool.Description + fmt.Sprint(tool.InputSchema); !strings.Contains(text, "key") {
			t.Errorf("%s: описание не упоминает key-цепочки: %s", name, tool.Description)
		}
	}
}
