package dsp

import (
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
)

// Тесты карточки internal-guitar-pedals, условие 1.1 (StepsGraph — цепочка шагов
// педалборда в одном графе для Run). Написаны по карточке, без чтения реализации:
// сравнивается звук на синтетике (Run), а не текст графа.
//
// Эталоны собираются из существующего API: один шаг — Run(ByID(chain).FilterGraph(p));
// два шага — Run первого в промежуточный файл, затем Run второго по нему.
// Хелперы (needFFmpeg, ncGen, ncChain, ncDecode2, ncDiffDb) — из inserts_test.go /
// newchains_test.go.

const stSR = 16000

// stIn — вход 4 с: тон 220 Гц пачками (0.5 с звук, 0.5 с тишина) — у реверба и
// перегруза есть что менять, у хвоста — где звучать.
func stIn(t *testing.T) string {
	t.Helper()
	return ncGen(t, "0.4*sin(2*PI*220*t)*lt(mod(t\\,1)\\,0.5)", 4, stSR)
}

// stRunGraph — прогнать файл через граф, вернуть путь выхода.
func stRunGraph(t *testing.T, in, graph string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.flac")
	if err := Run(in, out, graph, nil); err != nil {
		t.Fatalf("Run: %v\nграф: %s", err, graph)
	}
	return out
}

// stRunSteps — граф StepsGraph(steps) через Run; ошибка StepsGraph — Fatal.
func stRunSteps(t *testing.T, in string, steps []Step) string {
	t.Helper()
	g, _, err := StepsGraph(steps)
	if err != nil {
		t.Fatalf("StepsGraph(%+v): %v", steps, err)
	}
	return stRunGraph(t, in, g)
}

// stRunChain — эталон одного шага: Run(ByID(id).FilterGraph(p)).
func stRunChain(t *testing.T, in, id string, p map[string]float64) string {
	t.Helper()
	return stRunGraph(t, in, ncChain(t, id).FilterGraph(p))
}

// stDiffDb — разница двух файлов (по каждому каналу, худший), дБ к эталону ref,
// на длине входа [0, dur].
func stDiffDb(t *testing.T, got, ref string, dur float64) float64 {
	t.Helper()
	gl, gr := ncDecode2(t, got, stSR)
	rl, rr := ncDecode2(t, ref, stSR)
	return math.Max(ncDiffDb(gl, rl, stSR, 0, dur), ncDiffDb(gr, rr, stSR, 0, dur))
}

var (
	stGrit   = map[string]float64{"drive": 3, "crush": 0.3, "grit": 2}
	stHall   = map[string]float64{"size": 1.5, "predelay": 0, "wet": 0.6, "width": 1}
	stRoom   = map[string]float64{"size": 0.8, "predelay": 10, "wet": 0.5}
	stEqDark = map[string]float64{"high": -12, "low": 6}
)

// Контракт: json-теги Step — chain, params/off опускаются, если пусты.
func TestStepJSON(t *testing.T) {
	b, err := json.Marshal(Step{Chain: "eq"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"chain":"eq"}` {
		t.Errorf("json Step{Chain:eq} = %s, want {\"chain\":\"eq\"}", b)
	}
	var s Step
	if err := json.Unmarshal([]byte(`{"chain":"grit","params":{"drive":2},"off":true}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.Chain != "grit" || s.Params["drive"] != 2 || !s.Off {
		t.Errorf("json → Step: %+v, want grit drive=2 off", s)
	}
}

// Карточка 1.1: один шаг ≡ звук ByID(chain).FilterGraph(params) (разница ≤ −50 дБ).
func TestStepsGraphSingleStepEqualsChain(t *testing.T) {
	needFFmpeg(t)
	in := stIn(t)
	cases := []struct {
		id string
		p  map[string]float64
	}{
		{"grit", stGrit},
		{"reverb-hall", stHall},
		{"eq", stEqDark},
		{"delay", map[string]float64{"bpm": 120, "div": 2, "feedback": 0.5}},
		{"chorus", nil}, // параметры по умолчанию
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			got := stRunSteps(t, in, []Step{{Chain: c.id, Params: c.p}})
			ref := stRunChain(t, in, c.id, c.p)
			if d := stDiffDb(t, got, ref, 4); d > -50 {
				t.Errorf("один шаг %s: разница с цепочкой %.1f дБ, want ≤ −50", c.id, d)
			}
		})
	}
}

// Карточка 1.1: два шага — звук = второй(первый(вход)).
func TestStepsGraphTwoStepsIsComposition(t *testing.T) {
	needFFmpeg(t)
	in := stIn(t)
	pairs := [][2]Step{
		{{Chain: "grit", Params: stGrit}, {Chain: "reverb-hall", Params: stHall}},
		{{Chain: "reverb-hall", Params: stHall}, {Chain: "grit", Params: stGrit}},
		{{Chain: "eq", Params: stEqDark}, {Chain: "delay", Params: map[string]float64{"div": 4}}},
	}
	for _, pr := range pairs {
		t.Run(pr[0].Chain+"→"+pr[1].Chain, func(t *testing.T) {
			got := stRunSteps(t, in, []Step{pr[0], pr[1]})
			mid := stRunChain(t, in, pr[0].Chain, pr[0].Params)
			ref := stRunChain(t, mid, pr[1].Chain, pr[1].Params)
			if d := stDiffDb(t, got, ref, 4); d > -50 {
				t.Errorf("%s→%s: разница с второй(первый(вход)) %.1f дБ, want ≤ −50",
					pr[0].Chain, pr[1].Chain, d)
			}
		})
	}
}

// Карточка 1.1: порядок важен — перегруз→реверб ≠ реверб→перегруз (разница > −30 дБ).
func TestStepsGraphOrderMatters(t *testing.T) {
	needFFmpeg(t)
	in := stIn(t)
	grit := Step{Chain: "grit", Params: stGrit}
	hall := Step{Chain: "reverb-hall", Params: stHall}
	a := stRunSteps(t, in, []Step{grit, hall})
	b := stRunSteps(t, in, []Step{hall, grit})
	if d := stDiffDb(t, a, b, 4); d <= -30 {
		t.Errorf("перегруз→реверб vs реверб→перегруз: разница %.1f дБ, want > −30 (порядок шагов важен)", d)
	}
}

// Карточка 1.1: выключенный шаг (Off) = его нет — в звуке и в хвосте.
func TestStepsGraphOffStepIsAbsent(t *testing.T) {
	needFFmpeg(t)
	in := stIn(t)
	ref := stRunChain(t, in, "grit", stGrit)
	for name, steps := range map[string][]Step{
		"выкл после": {{Chain: "grit", Params: stGrit}, {Chain: "reverb-hall", Params: stHall, Off: true}},
		"выкл до":    {{Chain: "reverb-hall", Params: stHall, Off: true}, {Chain: "grit", Params: stGrit}},
		"выкл между": {{Chain: "eq", Params: stEqDark, Off: true}, {Chain: "grit", Params: stGrit},
			{Chain: "delay", Off: true}},
	} {
		t.Run(name, func(t *testing.T) {
			_, tail, err := StepsGraph(steps)
			if err != nil {
				t.Fatalf("StepsGraph: %v", err)
			}
			if tail != 0 {
				t.Errorf("хвост %.3f с, want 0: выключенный реверб/дилей хвоста не даёт (у grit хвоста нет)", tail)
			}
			got := stRunSteps(t, in, steps)
			if d := stDiffDb(t, got, ref, 4); d > -50 {
				t.Errorf("с выключенным шагом разница с одним grit %.1f дБ, want ≤ −50", d)
			}
		})
	}
}

// Карточка 1.1: хвост = сумма TailSec включённых шагов.
func TestStepsGraphTailIsSumOfEnabled(t *testing.T) {
	hallP := map[string]float64{"size": 2, "predelay": 40}
	delayP := map[string]float64{"bpm": 100, "div": 1, "feedback": 0.6}
	hall, room, delay := ncChain(t, "reverb-hall"), ncChain(t, "reverb-room"), ncChain(t, "delay")
	wantAll := hall.TailSec(hallP) + delay.TailSec(delayP)
	if hall.TailSec(hallP) <= 0 || delay.TailSec(delayP) <= 0 {
		t.Fatalf("у reverb-hall/delay TailSec должен быть > 0: %v %v", hall.TailSec(hallP), delay.TailSec(delayP))
	}
	cases := []struct {
		name  string
		steps []Step
		want  float64
	}{
		{"реверб + дилей", []Step{{Chain: "reverb-hall", Params: hallP}, {Chain: "grit"},
			{Chain: "delay", Params: delayP}}, wantAll},
		{"дилей выкл", []Step{{Chain: "reverb-hall", Params: hallP},
			{Chain: "delay", Params: delayP, Off: true}}, hall.TailSec(hallP)},
		{"реверб дважды", []Step{{Chain: "reverb-room", Params: stRoom},
			{Chain: "reverb-room", Params: stRoom}}, 2 * room.TailSec(stRoom)},
		{"без хвостов", []Step{{Chain: "grit"}, {Chain: "eq", Params: stEqDark}}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, tail, err := StepsGraph(c.steps)
			if err != nil {
				t.Fatalf("StepsGraph: %v", err)
			}
			if math.Abs(tail-c.want) > 1e-9 {
				t.Errorf("хвост %.4f с, want %.4f (сумма TailSec включённых шагов)", tail, c.want)
			}
		})
	}
}

// Карточка 1.1: одинаковые цепочки дважды подряд работают (метки графа не конфликтуют)
// и звучат как цепочка, применённая дважды.
func TestStepsGraphSameChainTwice(t *testing.T) {
	needFFmpeg(t)
	in := stIn(t)
	for _, c := range []struct {
		id string
		p  map[string]float64
	}{
		{"reverb-room", stRoom}, // граф с метками asplit/amix
		{"delay", map[string]float64{"div": 4, "feedback": 0.3}},
		{"eq", stEqDark},
	} {
		t.Run(c.id, func(t *testing.T) {
			got := stRunSteps(t, in, []Step{{Chain: c.id, Params: c.p}, {Chain: c.id, Params: c.p}})
			mid := stRunChain(t, in, c.id, c.p)
			ref := stRunChain(t, mid, c.id, c.p)
			if d := stDiffDb(t, got, ref, 4); d > -50 {
				t.Errorf("%s дважды: разница с двукратным прогоном %.1f дБ, want ≤ −50", c.id, d)
			}
		})
	}
}

// Карточка 1.1 и контракт: Key-цепочка — ошибка; неизвестная цепочка — ошибка;
// ни одного включённого шага — ошибка.
func TestStepsGraphErrors(t *testing.T) {
	cases := map[string][]Step{
		"key-цепочка":         {{Chain: "grit"}, {Chain: "ducking"}},
		"неизвестная цепочка": {{Chain: "no-such-chain"}},
		"пустой список":       {},
		"nil":                 nil,
		"все шаги выключены":  {{Chain: "grit", Off: true}, {Chain: "reverb-hall", Off: true}},
	}
	if c := ByID("ducking"); c == nil || c.Key == "" {
		t.Fatalf("ducking должна быть key-цепочкой: %+v", c)
	}
	for name, steps := range cases {
		t.Run(name, func(t *testing.T) {
			if g, _, err := StepsGraph(steps); err == nil {
				t.Errorf("StepsGraph(%+v): want ошибку, got граф %q", steps, g)
			}
		})
	}
}
