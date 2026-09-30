package mcp

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"yue-studio/internal/studio"
	"yue-studio/internal/yue"
)

// Тесты инструментов «голос по частям» (revoice_start / revoice_apply),
// rebuild_sections и vocal_contour — по спецификации задачи. Воркер —
// fakeService (внешняя граница); свои функции не мокаются.

func pid(v int64) *int64 { return &v }

// jsonArgs — аргументы так, как они приходят из JSON-RPC (числа — float64).
func jsonArgs(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// ---------- revoice_start ----------

func TestRevoiceStartUsesVoiceSourceOfVariant(t *testing.T) {
	s, fake := newTestServer(t)
	// версия 10 — вариант эффекта над треком 5: голос — от 5
	fake.jobs = []yue.Job{
		{ID: 7, Status: "done"},
		{ID: 10, Status: "done", Role: "variant", ParentID: pid(7)},
	}
	out, ok := call(t, s, "revoice_start", jsonArgs(t, `{"job_id":10,"from":12.5,"to":30,"abc":"X:1"}`))
	if !ok {
		t.Fatalf("revoice_start failed: %s", out)
	}
	// takes по умолчанию — 2
	if len(fake.continued) != 2 {
		t.Fatalf("ContinueJob вызван %d раз, want 2", len(fake.continued))
	}
	for i, c := range fake.continued {
		if c.JobID != 7 || c.From != 12.5 || c.Seed != 0 || c.Abc != "X:1" || c.StyleAdd != "" {
			t.Fatalf("continue[%d] = %+v, want {7 12.5 0 X:1 \"\"}", i, c)
		}
	}
	// в ответе — id всех дублей (500, 501) и id источника
	for _, want := range []string{"500", "501", "7"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ответ без %s: %s", want, out)
		}
	}
}

func TestRevoiceStartUsesExplicitVoiceSrc(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{
		{ID: 1, Status: "done"},
		{ID: 3, Status: "done"},
		{ID: 20, Status: "done", Role: "rebuild", ParentID: pid(1), VoiceSrc: pid(3)},
	}
	out, ok := call(t, s, "revoice_start", jsonArgs(t, `{"job_id":20,"from":0,"to":10,"takes":1}`))
	if !ok {
		t.Fatalf("revoice_start failed: %s", out)
	}
	if len(fake.continued) != 1 || fake.continued[0].JobID != 3 || fake.continued[0].From != 0 {
		t.Fatalf("continued = %+v, want один вызов от источника 3 с from=0", fake.continued)
	}
	if !strings.Contains(out, "500") || !strings.Contains(out, "3") {
		t.Fatalf("ответ: %s", out)
	}
}

func TestRevoiceStartGeneratedVersionIsOwnSource(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{
		{ID: 1, Status: "done"},
		{ID: 44, Status: "done", Role: "section", ParentID: pid(1)},
	}
	if out, ok := call(t, s, "revoice_start", jsonArgs(t, `{"job_id":44,"from":4,"to":8,"takes":4}`)); !ok {
		t.Fatalf("revoice_start failed: %s", out)
	}
	if len(fake.continued) != 4 {
		t.Fatalf("takes=4 → %d вызовов", len(fake.continued))
	}
	for _, c := range fake.continued {
		if c.JobID != 44 {
			t.Fatalf("источник %d, want 44", c.JobID)
		}
	}
}

func TestRevoiceStartSourceNotFound(t *testing.T) {
	cases := map[string][]yue.Job{
		// версии нет в библиотеке
		"нет версии": {{ID: 1, Status: "done"}},
		// вариант без родителя
		"вариант без родителя": {{ID: 10, Status: "done", Role: "variant"}},
		// родитель потерян
		"родитель потерян": {{ID: 10, Status: "done", Role: "variant", ParentID: pid(999)}},
		// цикл родителей
		"цикл": {
			{ID: 10, Status: "done", Role: "variant", ParentID: pid(11)},
			{ID: 11, Status: "done", Role: "variant", ParentID: pid(10)},
		},
	}
	for name, jobs := range cases {
		t.Run(name, func(t *testing.T) {
			s, fake := newTestServer(t)
			fake.jobs = jobs
			if out, ok := call(t, s, "revoice_start", jsonArgs(t, `{"job_id":10,"from":1,"to":5}`)); ok {
				t.Fatalf("ожидалась ошибка, ответ: %s", out)
			}
			if len(fake.continued) != 0 {
				t.Fatalf("ContinueJob не должен вызываться: %+v", fake.continued)
			}
		})
	}
}

func TestRevoiceStartTakesOutOfRange(t *testing.T) {
	for _, takes := range []int{5, 10, -1} {
		s, fake := newTestServer(t)
		fake.jobs = []yue.Job{{ID: 5, Status: "done"}}
		args := jsonArgs(t, `{"job_id":5,"from":1,"to":5,"takes":`+strconv.Itoa(takes)+`}`)
		if out, ok := call(t, s, "revoice_start", args); ok {
			t.Fatalf("takes=%d: ожидалась ошибка, ответ: %s", takes, out)
		}
		if len(fake.continued) != 0 {
			t.Fatalf("takes=%d: ContinueJob вызван %d раз", takes, len(fake.continued))
		}
	}
}

// ---------- revoice_apply ----------

func TestRevoiceApplyRejectsTakeNotDone(t *testing.T) {
	for _, status := range []string{"queued", "running", "error", "canceled"} {
		t.Run(status, func(t *testing.T) {
			s, fake := newTestServer(t)
			fake.jobs = []yue.Job{
				{ID: 5, Status: "done"},
				{ID: 501, Status: status, Role: "continue", ParentID: pid(5)},
			}
			out, ok := call(t, s, "revoice_apply", jsonArgs(t, `{"job_id":5,"take_id":501,"from":10,"to":20}`))
			if ok {
				t.Fatalf("дубль в статусе %q принят: %s", status, out)
			}
			// пересборка не начиналась: ни одного скачивания аудио
			if len(fake.fetched) != 0 {
				t.Fatalf("пересборка вызвана: fetched %v", fake.fetched)
			}
		})
	}
}

func TestRevoiceApplyRejectsMissingTake(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{{ID: 5, Status: "done"}}
	if out, ok := call(t, s, "revoice_apply", jsonArgs(t, `{"job_id":5,"take_id":777,"from":10,"to":20}`)); ok {
		t.Fatalf("несуществующий дубль принят: %s", out)
	}
	if len(fake.fetched) != 0 {
		t.Fatalf("пересборка вызвана: fetched %v", fake.fetched)
	}
}

// ---------- rebuild_sections / parseSectionSpecs ----------

func TestParseSectionSpecsFromJSON(t *testing.T) {
	var raw any
	if err := json.Unmarshal([]byte(`[
		{"child_id":12,"from":10.5,"to":20,"lead":2,"beat_sec":0.5,"stems":["drums","bass"],
		 "db":-3,"fade_in":0.25,"fade_out":0.75,"keep_high_hz":6000,"revoice":false},
		{"child_id":13,"from":30,"to":40,"stems":["vocals"],"revoice":true}
	]`), &raw); err != nil {
		t.Fatal(err)
	}
	got, err := parseSectionSpecs(raw)
	if err != nil {
		t.Fatalf("parseSectionSpecs: %v", err)
	}
	want := []studio.SectionSpec{
		{ChildID: 12, From: 10.5, To: 20, Lead: 2, BeatSec: 0.5, Stems: []string{"drums", "bass"},
			Db: -3, FadeIn: 0.25, FadeOut: 0.75, KeepHighHz: 6000},
		// отсутствующие поля — нули
		{ChildID: 13, From: 30, To: 40, Stems: []string{"vocals"}, Revoice: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("specs:\n got %+v\nwant %+v", got, want)
	}
}

func TestParseSectionSpecsVolumeOnlySpec(t *testing.T) {
	// child_id не задан → 0 («громкость дорожек»); db −100 — заглушить
	var raw any
	_ = json.Unmarshal([]byte(`[{"from":1,"to":2,"stems":["vocals"],"db":-100}]`), &raw)
	got, err := parseSectionSpecs(raw)
	if err != nil {
		t.Fatalf("parseSectionSpecs: %v", err)
	}
	if len(got) != 1 || got[0].ChildID != 0 || got[0].Db != -100 || got[0].From != 1 || got[0].To != 2 ||
		!reflect.DeepEqual(got[0].Stems, []string{"vocals"}) {
		t.Fatalf("spec: %+v", got)
	}
}

func TestParseSectionSpecsRejectsNonArray(t *testing.T) {
	for name, raw := range map[string]any{
		"nil":    nil,
		"string": "[]",
		"object": map[string]any{"child_id": float64(1)},
		"number": float64(3),
	} {
		if _, err := parseSectionSpecs(raw); err == nil {
			t.Fatalf("%s: ожидалась ошибка", name)
		}
	}
}

func TestRebuildSectionsEmptySpecsIsError(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{{ID: 5, Status: "done"}}
	for name, args := range map[string]string{
		"пустой массив": `{"job_id":5,"specs":[]}`,
		"нет specs":     `{"job_id":5}`,
		"не массив":     `{"job_id":5,"specs":"drums"}`,
	} {
		if out, ok := call(t, s, "rebuild_sections", jsonArgs(t, args)); ok {
			t.Fatalf("%s: ожидалась ошибка, ответ: %s", name, out)
		}
	}
	if len(fake.fetched) != 0 {
		t.Fatalf("пересборка вызвана: fetched %v", fake.fetched)
	}
}

// ---------- vocal_contour ----------

func TestVocalContourPrintsSummaryAndBars(t *testing.T) {
	s, fake := newTestServer(t)
	fake.contour = &yue.VocalContour{
		MedianHz: 146.8, LowHz: 98, HighHz: 220,
		Bars: []yue.ContourBar{
			{Index: 0, Start: 12, End: 14, Notes: []string{"D3", "F3", "A3", "-"}},
			{Index: 1, Start: 14, End: 16, Notes: []string{"G2", "G2", "C3", "D3"}},
		},
	}
	out, ok := call(t, s, "vocal_contour", jsonArgs(t, `{"job_id":9,"from":12,"to":16}`))
	if !ok {
		t.Fatalf("vocal_contour failed: %s", out)
	}
	if len(fake.contourCalls) != 1 || fake.contourCalls[0] != (contourCall{9, 12, 16}) {
		t.Fatalf("VocalContour args: %+v", fake.contourCalls)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		t.Fatalf("ожидались сводка + 2 такта, got %d строк:\n%s", len(lines), out)
	}
	// первая строка — медиана и диапазон
	for _, want := range []string{"146.8", "98", "220"} {
		if !strings.Contains(lines[0], want) {
			t.Fatalf("сводка без %s: %q", want, lines[0])
		}
	}
	// по строке на такт, в порядке тактов: время начала и ноты
	bar0, bar1 := -1, -1
	for i, l := range lines[1:] {
		if strings.Contains(l, "12") && strings.Contains(l, "D3") && strings.Contains(l, "F3") && strings.Contains(l, "A3") {
			bar0 = i
		}
		if strings.Contains(l, "14") && strings.Contains(l, "G2") && strings.Contains(l, "C3") {
			bar1 = i
		}
	}
	if bar0 < 0 || bar1 < 0 || bar0 >= bar1 {
		t.Fatalf("строки тактов не найдены/не по порядку (bar0=%d bar1=%d):\n%s", bar0, bar1, out)
	}
}

func TestVocalContourWholeTrackPassesZeros(t *testing.T) {
	s, fake := newTestServer(t)
	fake.contour = &yue.VocalContour{}
	if out, ok := call(t, s, "vocal_contour", jsonArgs(t, `{"job_id":3}`)); !ok {
		t.Fatalf("vocal_contour failed: %s", out)
	}
	if len(fake.contourCalls) != 1 || fake.contourCalls[0] != (contourCall{3, 0, 0}) {
		t.Fatalf("VocalContour args: %+v", fake.contourCalls)
	}
}

func TestVocalContourPropagatesClientError(t *testing.T) {
	s, fake := newTestServer(t)
	fake.contourErr = errors.New("no vocals stem")
	out, ok := call(t, s, "vocal_contour", jsonArgs(t, `{"job_id":3}`))
	if ok {
		t.Fatalf("ошибка клиента проглочена: %s", out)
	}
	if !strings.Contains(out, "no vocals stem") {
		t.Fatalf("текст ошибки потерян: %s", out)
	}
}

// ---------- тип yue.VocalContour: JSON-контракт воркера ----------

func TestVocalContourJSONTags(t *testing.T) {
	var c yue.VocalContour
	err := json.Unmarshal([]byte(`{"median_hz":150,"low_hz":100,"high_hz":200,
		"bars":[{"index":2,"start":4.5,"end":6.5,"notes":["C3","E3"]}]}`), &c)
	if err != nil {
		t.Fatal(err)
	}
	want := yue.VocalContour{MedianHz: 150, LowHz: 100, HighHz: 200,
		Bars: []yue.ContourBar{{Index: 2, Start: 4.5, End: 6.5, Notes: []string{"C3", "E3"}}}}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("decoded %+v, want %+v", c, want)
	}
}
