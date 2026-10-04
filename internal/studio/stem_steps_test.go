package studio

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"testing"

	"yue-studio/internal/dsp"
)

// Тесты карточки internal-guitar-pedals, условие 2.4: «Применить к треку» — одна
// спека пересборки (ChildID 0) со списком шагов SectionSpec.Steps вместо Chain;
// шаги идут ПОСЛЕДОВАТЕЛЬНО (выход шага — вход следующего), а не суммой разниц
// отдельных эффектов; выключенный шаг не влияет. Написаны по карточке, без чтения
// реализации. Фейковый воркер (secFake), fxSetup (голос 3000 Гц + остальное 500 Гц)
// и хелперы — из sections_test.go / stem_fx_test.go / helpers_test.go.

// stLevel — шаг «Громкость альбома» на gain дБ (тихий тон — до потолка далеко).
func stLevel(gain float64, off bool) dsp.Step {
	return dsp.Step{Chain: "level", Params: map[string]float64{"gain": gain}, Off: off}
}

// Карточка 2.4: два шага «−6 дБ» на other последовательно дают −12 дБ. Сумма
// разниц двух отдельных эффектов дала бы x + 2·(0.5x − x) = 0 — тишину; так
// отличается «последовательно» от «сумма разниц». Голос (3000) не тронут, вне
// окна — исходный трек; длина та же.
func TestRebuildSectionsStepsAreSequential(t *testing.T) {
	f, base := fxSetup(t)
	run(t, f, SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"other"},
		Steps: []dsp.Step{stLevel(-6, false), stLevel(-6, false)}})
	out := uploadedOnly(t, f)

	if d := float64(len(out)) / sr; math.Abs(d-fxDur) > 0.05 {
		t.Errorf("длина выхода %.3f с, want %.0f ± 0.05", d, fxDur)
	}
	if d := db(toneAmp(out, 500, 2.5, 5.5)) - db(fxAmpB); math.Abs(d-(-12)) > 1 {
		t.Errorf("500 Гц (other) в окне %+.1f дБ, want −12 ± 1 (шаги последовательно: −6 и ещё −6)", d)
	}
	if d := db(toneAmp(out, 3000, 2.5, 5.5)) - db(fxAmpA); math.Abs(d) > 0.5 {
		t.Errorf("3000 Гц (голос) в окне %+.1f дБ, want |Δ| ≤ 0.5 (чужая дорожка)", d)
	}
	for _, w := range [][2]float64{{0, 1.5}, {6.5, fxDur}} {
		if r := relDiffDb(out, base, w[0], w[1]); r > -40 {
			t.Errorf("вне окна %.1f–%.1f с разница с исходным %.1f дБ, want ≤ −40", w[0], w[1], r)
		}
	}
}

// Карточка 2.4: выключенный шаг (Off) не влияет: «−6 дБ» + выключенный «−6 дБ» = −6 дБ.
func TestRebuildSectionsStepsOffIgnored(t *testing.T) {
	f, _ := fxSetup(t)
	run(t, f, SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"other"},
		Steps: []dsp.Step{stLevel(-6, false), stLevel(-6, true)}})
	out := uploadedOnly(t, f)
	if d := db(toneAmp(out, 500, 2.5, 5.5)) - db(fxAmpB); math.Abs(d-(-6)) > 1 {
		t.Errorf("500 Гц (other) в окне %+.1f дБ, want −6 ± 1 (выключенный шаг не действует)", d)
	}
}

// Карточка 2.4: в окне трек = остальное + StepsGraph(steps)(дорожка). Эталон
// собирается из публичного API: дорожка прогоняется через граф StepsGraph
// (dsp.Run), к ней добавляется голос. Разница в середине окна (вдали от фейдов)
// ≤ −30 дБ к эталону. Шаги — вырез eq −12 дБ на 500 Гц и «громкость −6 дБ»:
// последовательно это 0.25·0.5 = 0.125x, а сумма разниц x + (0.25−1)x + (0.5−1)x =
// −0.25x — другой уровень и обратная фаза. Цепочки не зависят от частоты
// дискретизации (перегруз grit зависит — эталон на 16 кГц с ним не сходится уже
// у одиночной Chain, поэтому взят линейный пример).
func TestRebuildSectionsStepsMatchStepsGraph(t *testing.T) {
	f, _ := fxSetup(t)
	steps := []dsp.Step{
		{Chain: "eq", Params: map[string]float64{"mid": -12, "midf": 500}},
		stLevel(-6, false),
	}
	run(t, f, SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"other"}, Steps: steps})
	out := uploadedOnly(t, f)

	g, _, err := dsp.StepsGraph(steps)
	if err != nil {
		t.Fatalf("StepsGraph: %v", err)
	}
	dir := t.TempDir()
	wet := filepath.Join(dir, "other-wet.flac")
	if err := dsp.Run(f.files[key(parentID, "stem-other.flac")], wet, g, nil); err != nil {
		t.Fatalf("Run StepsGraph по дорожке: %v", err)
	}
	wetS := decodeFile(t, wet)
	voc := decodeFile(t, f.files[key(parentID, "stem-vocals.flac")])
	n := min(len(wetS), len(voc))
	want := make([]float32, n)
	for i := range want {
		want[i] = wetS[i] + voc[i]
	}
	if r := relDiffDb(out, want, 3, 5); r > -30 {
		t.Errorf("в окне (3–5 с) трек отличается от «голос + StepsGraph(other)» на %.1f дБ, want ≤ −30 (шаги последовательно)", r)
	}

	// и это не сумма разниц двух отдельных эффектов: эталон «сумма разниц» должен
	// отличаться от результата — иначе тест не различает два способа
	sumDiff := sumOfDiffs(t, f.files[key(parentID, "stem-other.flac")], steps, voc)
	if r := relDiffDb(sumDiff, want, 3, 5); r < -20 {
		t.Fatalf("синтетика не различает «последовательно» и «сумму разниц» (%.1f дБ) — поправь тест", r)
	}
}

// sumOfDiffs — «неправильный» эталон: other + Σ(шаг_i(other) − other) + голос.
func sumOfDiffs(t *testing.T, other string, steps []dsp.Step, voc []float32) []float32 {
	t.Helper()
	src := decodeFile(t, other)
	acc := append([]float32(nil), src...)
	for i, s := range steps {
		c := dsp.ByID(s.Chain)
		if c == nil {
			t.Fatalf("нет цепочки %s", s.Chain)
		}
		p := filepath.Join(t.TempDir(), "s.flac")
		if err := dsp.Run(other, p, c.FilterGraph(s.Params), nil); err != nil {
			t.Fatalf("шаг %d: %v", i, err)
		}
		w := decodeFile(t, p)
		for j := range acc {
			if j < len(w) {
				acc[j] += w[j] - src[j]
			}
		}
	}
	for j := range acc {
		if j < len(voc) {
			acc[j] += voc[j]
		}
	}
	return acc
}

// Карточка 2.4 (+ контракт): дорожка «гитара» — подробный стем, шаги работают и на
// нём (дорожка по умолчанию у педалей — guitar).
func TestRebuildSectionsStepsOnGuitar(t *testing.T) {
	f, _ := detailSetup(t, "guitar") // гитара — 3000 Гц внутри other, other = 500 + 3000
	run(t, f, SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"guitar"},
		Steps: []dsp.Step{stLevel(-6, false), stLevel(-6, false)}})
	out := uploadedOnly(t, f)
	if d := db(toneAmp(out, 3000, 2.5, 5.5)) - db(fxAmpA); math.Abs(d-(-12)) > 1 {
		t.Errorf("3000 Гц (гитара) в окне %+.1f дБ, want −12 ± 1", d)
	}
	if d := db(toneAmp(out, 500, 2.5, 5.5)) - db(fxAmpB); math.Abs(d) > 0.5 {
		t.Errorf("500 Гц (не гитара) в окне %+.1f дБ, want |Δ| ≤ 0.5", d)
	}
}

// Неизвестная цепочка в шагах — ошибка, ничего не загружено.
func TestRebuildSectionsStepsUnknownChainIsError(t *testing.T) {
	f, _ := fxSetup(t)
	_, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{{ChildID: 0, From: 2, To: 6,
		Stems: []string{"other"}, Steps: []dsp.Step{{Chain: "no-such-chain"}}}})
	if err == nil {
		t.Fatal("неизвестная цепочка в шагах: want ошибку, got nil")
	}
	if len(f.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", keys(f.uploads))
	}
}

// Контракт: SectionSpec.Steps — json `steps,omitempty`.
func TestSectionSpecStepsJSON(t *testing.T) {
	var sp SectionSpec
	if err := json.Unmarshal([]byte(`{"child_id":0,"from":2,"to":4,"stems":["guitar"],
		"steps":[{"chain":"od-ts","params":{"drive":5}},{"chain":"reverb-room","off":true}]}`), &sp); err != nil {
		t.Fatal(err)
	}
	want := []dsp.Step{{Chain: "od-ts", Params: map[string]float64{"drive": 5}}, {Chain: "reverb-room", Off: true}}
	if !reflect.DeepEqual(sp.Steps, want) {
		t.Errorf("steps → Steps=%+v, want %+v", sp.Steps, want)
	}
	b, err := json.Marshal(SectionSpec{ChildID: 0, From: 1, To: 2, Stems: []string{"other"}, Db: -100})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["steps"]; ok {
		t.Errorf("спека без шагов сериализуется со steps (omitempty): %s", b)
	}
}
