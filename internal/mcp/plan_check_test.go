package mcp

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты проверки изменённого плана (карточка internal-plan-check, условия 2 и 3):
// инструмент plan_check и проверка abc перед continue_job / revoice_start.
// Воркер — fakeService (внешняя граница).

// samplePlanCheck — ответ воркера: Vocal укорочен 16→14, один изменённый такт,
// предупреждение о потолке.
func samplePlanCheck() *yue.PlanCheck {
	return &yue.PlanCheck{
		Bars:     map[string][2]int{"Vocal": {16, 14}, "Bass": {16, 16}},
		Duration: [2]float64{64, 56},
		Changed: []yue.PlanChange{
			{Voice: "Vocal", Bar: 5, Start: 42.5, End: 45, Before: "d2 e2 f2 g2", After: "d'2 e'2 f'2 g'2"},
		},
		ChangedTotal: 1,
		Ceiling:      yue.PlanCeiling{Top: "d'", Ceiling: "f'", NewTop: "g'"},
		Warnings:     []string{"такт 5 выше потолка голоса (писк/фальцет)"},
	}
}

func lineWith(lines []string, subs ...string) int {
	for i, l := range lines {
		all := true
		for _, s := range subs {
			if !strings.Contains(l, s) {
				all = false
				break
			}
		}
		if all {
			return i
		}
	}
	return -1
}

// ---------- plan_check ----------

func TestPlanCheckPassesArgs(t *testing.T) {
	s, fake := newTestServer(t)
	fake.planCheckOut = samplePlanCheck()
	if out, ok := call(t, s, "plan_check", jsonArgs(t, `{"job_id":216,"abc":"X:1\nK:C\n","from":30.5}`)); !ok {
		t.Fatalf("plan_check failed: %s", out)
	}
	want := []planCheckCall{{216, "X:1\nK:C\n", 30.5}}
	if !reflect.DeepEqual(fake.planCheckCalls, want) {
		t.Fatalf("PlanCheck args = %+v, want %+v", fake.planCheckCalls, want)
	}
}

func TestPlanCheckWithoutFromPassesZero(t *testing.T) {
	s, fake := newTestServer(t)
	if out, ok := call(t, s, "plan_check", jsonArgs(t, `{"job_id":3,"abc":"X:1"}`)); !ok {
		t.Fatalf("plan_check failed: %s", out)
	}
	if len(fake.planCheckCalls) != 1 || fake.planCheckCalls[0] != (planCheckCall{3, "X:1", 0}) {
		t.Fatalf("PlanCheck args = %+v, want {3 X:1 0}", fake.planCheckCalls)
	}
}

func TestPlanCheckPrintsSummary(t *testing.T) {
	s, fake := newTestServer(t)
	fake.planCheckOut = samplePlanCheck()
	out, ok := call(t, s, "plan_check", jsonArgs(t, `{"job_id":216,"abc":"X:1"}`))
	if !ok {
		t.Fatalf("plan_check failed: %s", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// такты по голосам: было → стало
	if lineWith(lines, "Vocal", "16", "14") < 0 {
		t.Fatalf("нет строки тактов Vocal 16→14:\n%s", out)
	}
	if !strings.Contains(out, "Bass") {
		t.Fatalf("нет голоса Bass в сводке тактов:\n%s", out)
	}
	// потолок: верх исходного, потолок, верх нового — в одной строке
	if lineWith(lines, "d'", "f'", "g'") < 0 {
		t.Fatalf("нет строки потолка (d' / f' / g'):\n%s", out)
	}
	// изменённый такт: время начала и голос
	if lineWith(lines, "42.5", "Vocal") < 0 {
		t.Fatalf("нет строки изменённого такта (42.5, Vocal):\n%s", out)
	}
	// предупреждение с пометкой ⚠
	if lineWith(lines, "⚠", "такт 5 выше потолка голоса") < 0 {
		t.Fatalf("нет предупреждения с ⚠:\n%s", out)
	}
}

func TestPlanCheckOneLinePerChangedBar(t *testing.T) {
	s, fake := newTestServer(t)
	pc := samplePlanCheck()
	pc.Changed = []yue.PlanChange{
		{Voice: "Vocal", Bar: 3, Start: 17.25, End: 19.5, Before: "a", After: "b"},
		{Voice: "Bass", Bar: 9, Start: 83.75, End: 86, Before: "C", After: "D"},
	}
	pc.ChangedTotal = 2
	fake.planCheckOut = pc
	out, ok := call(t, s, "plan_check", jsonArgs(t, `{"job_id":216,"abc":"X:1"}`))
	if !ok {
		t.Fatalf("plan_check failed: %s", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	a, b := lineWith(lines, "17.25", "Vocal"), lineWith(lines, "83.75", "Bass")
	if a < 0 || b < 0 || a == b {
		t.Fatalf("ожидалось по отдельной строке на такт (a=%d b=%d):\n%s", a, b, out)
	}
}

func TestPlanCheckNoChangesNoWarnings(t *testing.T) {
	s, fake := newTestServer(t)
	fake.planCheckOut = &yue.PlanCheck{
		Bars:     map[string][2]int{"Vocal": {16, 16}},
		Duration: [2]float64{64, 64},
		Ceiling:  yue.PlanCeiling{Top: "d'", Ceiling: "f'", NewTop: "d'"},
	}
	out, ok := call(t, s, "plan_check", jsonArgs(t, `{"job_id":216,"abc":"X:1"}`))
	if !ok {
		t.Fatalf("plan_check failed: %s", out)
	}
	if strings.Contains(out, "⚠") {
		t.Fatalf("без предупреждений не должно быть ⚠:\n%s", out)
	}
}

func TestPlanCheckPropagatesClientError(t *testing.T) {
	s, fake := newTestServer(t)
	fake.planCheckErr = errors.New("HTTP 422: abc: пустой план")
	out, ok := call(t, s, "plan_check", jsonArgs(t, `{"job_id":216,"abc":""}`))
	if ok {
		t.Fatalf("ошибка клиента проглочена: %s", out)
	}
	if !strings.Contains(out, "пустой план") {
		t.Fatalf("текст ошибки потерян: %s", out)
	}
}

// ---------- continue_job с abc ----------

func TestContinueJobWithAbcChecksPlanAndPrintsWarnings(t *testing.T) {
	s, fake := newTestServer(t)
	fake.planCheckOut = samplePlanCheck()
	out, ok := call(t, s, "continue_job", jsonArgs(t, `{"job_id":216,"from_sec":40,"abc":"X:1\nK:C"}`))
	if !ok {
		t.Fatalf("continue_job failed: %s", out)
	}
	if len(fake.planCheckCalls) != 1 || fake.planCheckCalls[0] != (planCheckCall{216, "X:1\nK:C", 40}) {
		t.Fatalf("PlanCheck args = %+v, want {216 X:1\\nK:C 40}", fake.planCheckCalls)
	}
	// предупреждения не блокируют постановку
	if len(fake.continued) != 1 || fake.continued[0].JobID != 216 || fake.continued[0].Abc != "X:1\nK:C" {
		t.Fatalf("ContinueJob = %+v, want один вызов для 216 с тем же abc", fake.continued)
	}
	if !strings.Contains(out, "500") {
		t.Fatalf("в ответе нет id поставленной джобы: %s", out)
	}
	if lineWith(strings.Split(out, "\n"), "⚠", "такт 5 выше потолка голоса") < 0 {
		t.Fatalf("в ответе нет предупреждения проверки:\n%s", out)
	}
}

func TestContinueJobPlanCheckErrorBlocksSubmit(t *testing.T) {
	s, fake := newTestServer(t)
	fake.planCheckErr = &yue.StatusError{Code: 422, Msg: "HTTP 422: abc: не разобран"}
	out, ok := call(t, s, "continue_job", jsonArgs(t, `{"job_id":216,"from_sec":40,"abc":"мусор"}`))
	if ok {
		t.Fatalf("ожидалась ошибка, ответ: %s", out)
	}
	if !strings.Contains(out, "не разобран") {
		t.Fatalf("текст ошибки потерян: %s", out)
	}
	if len(fake.continued) != 0 {
		t.Fatalf("ContinueJob не должен вызываться: %+v", fake.continued)
	}
}

func TestContinueJobWithoutAbcSkipsPlanCheck(t *testing.T) {
	s, fake := newTestServer(t)
	out, ok := call(t, s, "continue_job", jsonArgs(t, `{"job_id":216,"from_sec":40}`))
	if !ok {
		t.Fatalf("continue_job failed: %s", out)
	}
	if len(fake.planCheckCalls) != 0 {
		t.Fatalf("без abc проверка не нужна: %+v", fake.planCheckCalls)
	}
	if len(fake.continued) != 1 {
		t.Fatalf("ContinueJob вызван %d раз, want 1", len(fake.continued))
	}
}

// ---------- revoice_start с abc ----------

func TestRevoiceStartWithAbcChecksPlanOfVoiceSource(t *testing.T) {
	s, fake := newTestServer(t)
	// версия 10 — вариант над треком 7: продолжение идёт от 7, проверяется план 7
	fake.jobs = []yue.Job{
		{ID: 7, Status: "done"},
		{ID: 10, Status: "done", Role: "variant", ParentID: pid(7)},
	}
	fake.planCheckOut = samplePlanCheck()
	out, ok := call(t, s, "revoice_start", jsonArgs(t, `{"job_id":10,"from":12.5,"to":30,"abc":"X:1"}`))
	if !ok {
		t.Fatalf("revoice_start failed: %s", out)
	}
	if len(fake.planCheckCalls) == 0 {
		t.Fatal("PlanCheck не вызван")
	}
	for i, c := range fake.planCheckCalls {
		if c != (planCheckCall{7, "X:1", 12.5}) {
			t.Fatalf("PlanCheck[%d] = %+v, want {7 X:1 12.5}", i, c)
		}
	}
	if len(fake.continued) != 2 {
		t.Fatalf("предупреждения не блокируют: ContinueJob вызван %d раз, want 2", len(fake.continued))
	}
	if lineWith(strings.Split(out, "\n"), "⚠", "такт 5 выше потолка голоса") < 0 {
		t.Fatalf("в ответе нет предупреждения проверки:\n%s", out)
	}
}

func TestRevoiceStartPlanCheckErrorBlocksSubmit(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{{ID: 7, Status: "done"}}
	fake.planCheckErr = &yue.StatusError{Code: 422, Msg: "HTTP 422: abc: не разобран"}
	out, ok := call(t, s, "revoice_start", jsonArgs(t, `{"job_id":7,"from":4,"to":8,"abc":"мусор"}`))
	if ok {
		t.Fatalf("ожидалась ошибка, ответ: %s", out)
	}
	if !strings.Contains(out, "не разобран") {
		t.Fatalf("текст ошибки потерян: %s", out)
	}
	if len(fake.continued) != 0 {
		t.Fatalf("ContinueJob не должен вызываться: %+v", fake.continued)
	}
}

func TestRevoiceStartWithoutAbcSkipsPlanCheck(t *testing.T) {
	s, fake := newTestServer(t)
	fake.jobs = []yue.Job{{ID: 7, Status: "done"}}
	if out, ok := call(t, s, "revoice_start", jsonArgs(t, `{"job_id":7,"from":4,"to":8,"takes":1}`)); !ok {
		t.Fatalf("revoice_start failed: %s", out)
	}
	if len(fake.planCheckCalls) != 0 {
		t.Fatalf("без abc проверка не нужна: %+v", fake.planCheckCalls)
	}
}

// ---------- тип yue.PlanCheck: JSON-контракт воркера ----------

func TestPlanCheckJSONTags(t *testing.T) {
	var c yue.PlanCheck
	err := json.Unmarshal([]byte(`{"bars":{"Vocal":[16,14]},"duration":[64,56],
		"changed":[{"voice":"Vocal","bar":5,"start":42.5,"end":45,"before":"a","after":"b"}],
		"changed_total":70,
		"ceiling":{"top":"d'","ceiling":"f'","new_top":"g'"},
		"warnings":["w"]}`), &c)
	if err != nil {
		t.Fatal(err)
	}
	want := yue.PlanCheck{
		Bars:         map[string][2]int{"Vocal": {16, 14}},
		Duration:     [2]float64{64, 56},
		Changed:      []yue.PlanChange{{Voice: "Vocal", Bar: 5, Start: 42.5, End: 45, Before: "a", After: "b"}},
		ChangedTotal: 70,
		Ceiling:      yue.PlanCeiling{Top: "d'", Ceiling: "f'", NewTop: "g'"},
		Warnings:     []string{"w"},
	}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("decoded %+v, want %+v", c, want)
	}
}

// Старый воркер без plan_check (404) не блокирует продолжение: джоба ставится,
// в ответе — «проверка плана недоступна»; 422 (битый план) — блокирует.
func TestContinueJobPlanCheckUnavailableStillSubmits(t *testing.T) {
	s, fake := newTestServer(t)
	fake.planCheckErr = &yue.StatusError{Code: 404, Msg: "yue /jobs/5/plan_check: 404 Not Found"}
	out, ok := call(t, s, "continue_job", jsonArgs(t, `{"job_id":5,"from_sec":10,"abc":"X:1"}`))
	if !ok || len(fake.continued) != 1 || !strings.Contains(out, "проверка плана недоступна") {
		t.Fatalf("404 проверки: ok=%v continued=%d out=%s", ok, len(fake.continued), out)
	}
	fake.continued = nil
	fake.planCheckErr = &yue.StatusError{Code: 422, Msg: "yue /jobs/5/plan_check: 422: abc"}
	if out, ok := call(t, s, "continue_job", jsonArgs(t, `{"job_id":5,"from_sec":10,"abc":"X:1"}`)); ok || len(fake.continued) != 0 {
		t.Fatalf("422 проверки должен блокировать: ok=%v continued=%d out=%s", ok, len(fake.continued), out)
	}
}

// Пустые поля сводки — «—»: у голоса нет нот, такта нет во времени нового плана.
func TestPlanCheckDashesForEmpty(t *testing.T) {
	s, fake := newTestServer(t)
	fake.planCheckOut = &yue.PlanCheck{Bars: map[string][2]int{"Vocal": {4, 3}}, ChangedTotal: 1,
		Changed: []yue.PlanChange{{Voice: "Vocal", Bar: 4, Before: "c4c4c4c4"}}}
	out, ok := call(t, s, "plan_check", jsonArgs(t, `{"job_id":1,"abc":"X:1"}`))
	if !ok {
		t.Fatal(out)
	}
	if !strings.Contains(out, "верх было —, потолок —, верх стало —") || !strings.Contains(out, "— с  Vocal такт 4: c4c4c4c4 → —") {
		t.Errorf("нужны «—» вместо пустого:\n%s", out)
	}
}
