package studio

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"testing"
)

// Тесты карточки internal-stem-fx, условие 1: спека «громкость дорожек» (ChildID 0)
// с Chain (+Params) — DSP-эффект применяется к дорожкам Stems трека в окне
// [From, To) (To ≤ 0 — до конца); в трек добавляется разница «обработанная −
// исходная» дорожка в окне (с фейдами на краях); остальные дорожки и звук вне
// окна не меняются. Фейковый воркер (secFake) и синтетика — из sections_test.go,
// звуковые хелперы — из helpers_test.go / mute_revoice_test.go.
//
// Синтетика: голос (vocals) — тон 3000 Гц, остальное (other) — тон 500 Гц,
// трек = их сумма. Вырез — цепочка «Убрать свист» (dewhistle) на 3000 Гц; её
// собственные start/end = 0, окно задаёт спека. Проверки внутри окна берутся
// с отступом 0.5 с от краёв (фейды), вне окна — тоже с отступом 0.5 с.

const (
	fxDur  = 8.0
	fxAmpA = 0.3 // стем A (vocals), 3000 Гц
	fxAmpB = 0.3 // стем B (other), 500 Гц
)

const (
	exprA3000 = "0.3*sin(2*PI*3000*t)"
	exprB500  = "0.3*sin(2*PI*500*t)"
)

// fxSetup — родитель: audio.flac = A+B, стемы vocals=A, other=B, drums/bass — тишина.
func fxSetup(t *testing.T) (*secFake, []float32) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	pf := map[string]string{
		"audio.flac":       lavfi(t, aeval(exprA3000+"+"+exprB500, fxDur), filepath.Join(dir, "fx-audio.flac")),
		"stem-vocals.flac": lavfi(t, aeval(exprA3000, fxDur), filepath.Join(dir, "fx-vocals.flac")),
		"stem-other.flac":  lavfi(t, aeval(exprB500, fxDur), filepath.Join(dir, "fx-other.flac")),
		"stem-drums.flac":  lavfi(t, aeval("0", fxDur), filepath.Join(dir, "fx-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", fxDur), filepath.Join(dir, "fx-bass.flac")),
	}
	put(f, parentID, pf)
	return f, decodeFile(t, pf["audio.flac"])
}

// notch3000 — параметры выреза: 3000 Гц глубиной 30 дБ, без гармоник, на весь вход
// (start/end = 0) — окно задаёт спека, не цепочка.
func notch3000() map[string]float64 {
	return map[string]float64{"freq": 3000, "depth": 30, "width": 60, "harmonics": 1, "start": 0, "end": 0}
}

func fxSpec(from, to float64) SectionSpec {
	return SectionSpec{ChildID: 0, From: from, To: to, Stems: []string{"vocals"},
		Chain: "dewhistle", Params: notch3000()}
}

// relDiffDb — разница выход − база на [from,to] относительно уровня базы, дБ.
func relDiffDb(out, base []float32, from, to float64) float64 {
	d := diffRMS(out, base, from, to)
	b := rms(slice(base, from, to))
	if d <= 0 {
		return -200
	}
	return 20 * math.Log10(d/b)
}

// Кейс карточки: в окне 2–4 с тон стема A (3000) ослаблен ≥ 15 дБ, тон стема B (500)
// не изменился (≤ 0.5 дБ); вне окна трек = исходный (разница ≤ −40 дБ); длина та же.
func TestRebuildSectionsStemChainInWindow(t *testing.T) {
	f, base := fxSetup(t)
	run(t, f, fxSpec(2, 4))
	out := uploadedOnly(t, f)

	if d := float64(len(out)) / sr; math.Abs(d-fxDur) > 0.05 {
		t.Errorf("длина выхода %.3f с, want %.0f ± 0.05 (длина базы)", d, fxDur)
	}
	if d := db(toneAmp(out, 3000, 2.5, 3.5)) - db(fxAmpA); d > -15 {
		t.Errorf("3000 Гц (стем A) в окне изменился на %+.1f дБ, want ≤ −15", d)
	}
	if d := db(toneAmp(out, 500, 2.5, 3.5)) - db(fxAmpB); math.Abs(d) > 0.5 {
		t.Errorf("500 Гц (стем B) в окне изменился на %+.1f дБ, want |Δ| ≤ 0.5 (чужая дорожка не трогается)", d)
	}
	for _, w := range [][2]float64{{0, 1.5}, {4.5, fxDur}} {
		if r := relDiffDb(out, base, w[0], w[1]); r > -40 {
			t.Errorf("вне окна %.1f–%.1f с разница с исходным %.1f дБ, want ≤ −40", w[0], w[1], r)
		}
		if a := toneAmp(out, 3000, w[0], w[1]); math.Abs(db(a)-db(fxAmpA)) > 0.5 {
			t.Errorf("3000 Гц вне окна %.1f–%.1f с: %.4f, want ≈ %.2f", w[0], w[1], a, fxAmpA)
		}
	}
}

// Кейс карточки: To = 0 — эффект до конца трека; до From трек исходный.
func TestRebuildSectionsStemChainToZeroTillEnd(t *testing.T) {
	f, base := fxSetup(t)
	run(t, f, fxSpec(2, 0))
	out := uploadedOnly(t, f)

	for _, w := range [][2]float64{{2.5, 4.5}, {5, fxDur - 0.5}} {
		if d := db(toneAmp(out, 3000, w[0], w[1])) - db(fxAmpA); d > -15 {
			t.Errorf("To=0: 3000 Гц на %.1f–%.1f с изменился на %+.1f дБ, want ≤ −15", w[0], w[1], d)
		}
		if d := db(toneAmp(out, 500, w[0], w[1])) - db(fxAmpB); math.Abs(d) > 0.5 {
			t.Errorf("To=0: 500 Гц на %.1f–%.1f с изменился на %+.1f дБ, want |Δ| ≤ 0.5", w[0], w[1], d)
		}
	}
	if r := relDiffDb(out, base, 0, 1.5); r > -40 {
		t.Errorf("To=0: до From разница с исходным %.1f дБ, want ≤ −40", r)
	}
}

// Кейс карточки: неизвестная цепочка — ошибка, ничего не загружено.
func TestRebuildSectionsStemChainUnknownIsError(t *testing.T) {
	f, _ := fxSetup(t)
	sp := fxSpec(2, 4)
	sp.Chain = "no-such-chain"
	if _, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{sp}); err == nil {
		t.Fatal("неизвестная цепочка: want ошибку, got nil")
	}
	if len(f.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", keys(f.uploads))
	}
}

// Кейс карточки: вместе с обычной заглушкой в одном вызове — применены обе:
// в 2–4 с вырезан 3000 (голос), в 5–7 с заглушён other (500), остальное на месте.
func TestRebuildSectionsStemChainWithMute(t *testing.T) {
	f, base := fxSetup(t)
	mute := SectionSpec{ChildID: 0, From: 5, To: 7, Stems: []string{"other"}, Db: -100}
	run(t, f, fxSpec(2, 4), mute)
	out := uploadedOnly(t, f)

	if d := db(toneAmp(out, 3000, 2.5, 3.5)) - db(fxAmpA); d > -15 {
		t.Errorf("эффект: 3000 Гц в 2–4 с изменился на %+.1f дБ, want ≤ −15", d)
	}
	if d := db(toneAmp(out, 500, 2.5, 3.5)) - db(fxAmpB); math.Abs(d) > 0.5 {
		t.Errorf("эффект: 500 Гц в 2–4 с изменился на %+.1f дБ, want |Δ| ≤ 0.5", d)
	}
	if a := toneAmp(out, 500, 5.5, 6.5); a > 0.01 {
		t.Errorf("заглушка: 500 Гц в 5–7 с %.4f, want ≈ 0 (other заглушён)", a)
	}
	if a := toneAmp(out, 3000, 5.5, 6.5); math.Abs(db(a)-db(fxAmpA)) > 0.5 {
		t.Errorf("заглушка: 3000 Гц в 5–7 с %.4f, want ≈ %.2f (голос вне окна эффекта не тронут)", a, fxAmpA)
	}
	for _, w := range [][2]float64{{0, 1.5}, {7.5, fxDur}} {
		if r := relDiffDb(out, base, w[0], w[1]); r > -40 {
			t.Errorf("вне окон %.1f–%.1f с разница с исходным %.1f дБ, want ≤ −40", w[0], w[1], r)
		}
	}
}

// Поля приходят из фронта/MCP по JSON под именами chain и params.
func TestSectionSpecChainParamsJSON(t *testing.T) {
	var sp SectionSpec
	err := json.Unmarshal([]byte(`{"child_id":0,"from":2,"to":4,"stems":["vocals"],
		"chain":"dewhistle","params":{"freq":2638,"depth":15}}`), &sp)
	if err != nil {
		t.Fatal(err)
	}
	if sp.Chain != "dewhistle" {
		t.Errorf("chain → Chain=%q, want dewhistle", sp.Chain)
	}
	if want := map[string]float64{"freq": 2638, "depth": 15}; !reflect.DeepEqual(sp.Params, want) {
		t.Errorf("params → Params=%v, want %v", sp.Params, want)
	}
}
