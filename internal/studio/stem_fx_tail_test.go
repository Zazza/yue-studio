package studio

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"yue-studio/internal/dsp"
)

// Тесты карточки internal-dsp-space, условия 10.2 и 10.3: эффект на дорожку
// (спека ChildID 0 с Chain) у цепочки с хвостом (TailSec > 0) — хвост звучит
// после To ещё TailSec, сухая дорожка после To не удваивается; цепочка с Key
// получает вторым входом стем Key родителя. Фейковый воркер и хелперы — из
// sections_test.go / helpers_test.go / stem_fx_test.go.
//
// Синтетика хвоста: голос (vocals) — тон 1000 Гц на 2.0–3.6 с (внутри окна 2–4 с)
// и тон 1500 Гц с 4.5 с (после To — эффекта на нём быть не должно); остальное
// (other) — тишина. Реверб на голосе в окне [2, 4]: после To слышен хвост тона
// 1000, тон 1500 звучит как в исходнике (не удвоен, без реверба).

const (
	tailDur  = 10.0
	tailExpr = "0.3*sin(2*PI*1000*t)*between(t\\,2\\,3.6)+0.3*sin(2*PI*1500*t)*gte(t\\,4.5)"
)

func tailSetup(t *testing.T) (*secFake, []float32) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	pf := map[string]string{
		"audio.flac":       lavfi(t, aeval(tailExpr, tailDur), filepath.Join(dir, "tl-audio.flac")),
		"stem-vocals.flac": lavfi(t, aeval(tailExpr, tailDur), filepath.Join(dir, "tl-vocals.flac")),
		"stem-other.flac":  lavfi(t, aeval("0", tailDur), filepath.Join(dir, "tl-other.flac")),
		"stem-drums.flac":  lavfi(t, aeval("0", tailDur), filepath.Join(dir, "tl-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", tailDur), filepath.Join(dir, "tl-bass.flac")),
	}
	put(f, parentID, pf)
	return f, decodeFile(t, pf["audio.flac"])
}

func hallParams() map[string]float64 {
	return map[string]float64{"size": 2, "predelay": 0, "wet": 1, "width": 0}
}

// Карточка 10.2: после To звучит хвост, сухая дорожка не удвоена; после
// To + TailSec (+ фейд) — исходный трек.
func TestRebuildSectionsStemChainTail(t *testing.T) {
	c := dsp.ByID("reverb-hall")
	if c == nil {
		t.Fatal("reverb-hall не найден")
	}
	tail := c.TailSec(hallParams())
	if tail <= 0 {
		t.Fatalf("reverb-hall TailSec = %v, want > 0", tail)
	}
	f, base := tailSetup(t)
	run(t, f, SectionSpec{ChildID: 0, From: 2, To: 4, Stems: []string{"vocals"},
		Chain: "reverb-hall", Params: hallParams()})
	out := uploadedOnly(t, f)

	if d := float64(len(out)) / sr; math.Abs(d-tailDur) > 0.05 {
		t.Errorf("длина выхода %.3f с, want %.0f ± 0.05", d, tailDur)
	}
	// хвост тона 1000 Гц после To (в исходнике там тишина)
	if a := toneAmp(out, 1000, 4.1, 4.4); a < 0.003 {
		t.Errorf("хвост после To: 1000 Гц на 4.1–4.4 с %.4f, want слышен (≥ 0.003 ≈ −40 дБ к тону)", a)
	}
	// сухая дорожка после To не удвоена и без эффекта: тон 1500 как в исходнике
	if d := db(toneAmp(out, 1500, 4.6, 5.0)) - db(0.3); math.Abs(d) > 1.5 {
		t.Errorf("1500 Гц после To (4.6–5.0 с): %+.1f дБ к исходному, want ±1.5 (не удвоен, эффект не на нём)", d)
	}
	// после To + TailSec + запас на фейд — исходный трек
	from := 4 + tail + 0.5
	if r := relDiffDb(out, base, from, tailDur); r > -30 {
		t.Errorf("после To+TailSec (%.1f с–конец) разница с исходным %.1f дБ, want ≤ −30", from, r)
	}
	// до From — исходный трек
	if r := relDiffDb(out, base, 0, 1.5); r > -40 {
		t.Errorf("до From разница с исходным %.1f дБ, want ≤ −40", r)
	}
}

// Карточка 10.3: цепочка с Key (ducking, ключ drums) получает вторым входом стем
// drums родителя: тон other в окне приседает после щелчков барабанов.
func TestRebuildSectionsKeyChainUsesParentStem(t *testing.T) {
	needFFmpeg(t)
	c := dsp.ByID("ducking")
	if c == nil || c.Key != "drums" {
		t.Fatalf("ducking: %+v, want цепочку с Key drums", c)
	}
	dir := t.TempDir()
	f := newSecFake()
	pf := map[string]string{
		"audio.flac":       lavfi(t, aeval(expr440+"+"+exprClicks, fxDur), filepath.Join(dir, "k-audio.flac")),
		"stem-other.flac":  lavfi(t, aeval(expr440, fxDur), filepath.Join(dir, "k-other.flac")),
		"stem-drums.flac":  lavfi(t, aeval(exprClicks, fxDur), filepath.Join(dir, "k-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", fxDur), filepath.Join(dir, "k-bass.flac")),
		"stem-vocals.flac": lavfi(t, aeval("0", fxDur), filepath.Join(dir, "k-vocals.flac")),
	}
	put(f, parentID, pf)
	base := decodeFile(t, pf["audio.flac"])
	run(t, f, SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"other"}, Chain: "ducking"})
	out := uploadedOnly(t, f)

	// окна 40 мс: целое число периодов 440 Гц нет, но сравниваются одинаковые окна
	// после щелчка и перед следующим — смещение оценки одно и то же
	for _, cl := range []float64{3.0, 3.5, 4.0, 4.5} {
		after := toneAmp(out, 440, cl+0.02, cl+0.06)
		before := toneAmp(out, 440, cl+0.42, cl+0.46)
		if d := db(before) - db(after); d < 6 {
			t.Errorf("щелчок drums %.1f с: тон other через 20–60 мс тише, чем перед следующим, на %.1f дБ, want ≥ 6", cl, d)
		}
	}
	if r := relDiffDb(out, base, 0, 1.5); r > -40 {
		t.Errorf("до From разница с исходным %.1f дБ, want ≤ −40", r)
	}
}

// Карточка 6.5: у трека нет стема-ключа — ошибка, ничего не загружено.
func TestRebuildSectionsKeyChainNoKeyStemIsError(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	put(f, parentID, map[string]string{
		"audio.flac":      lavfi(t, aeval(expr440, fxDur), filepath.Join(dir, "nk-audio.flac")),
		"stem-other.flac": lavfi(t, aeval(expr440, fxDur), filepath.Join(dir, "nk-other.flac")),
	})
	_, err := RebuildSections(context.Background(), f, parentID,
		[]SectionSpec{{ChildID: 0, From: 2, To: 6, Stems: []string{"other"}, Chain: "ducking"}})
	if err == nil {
		t.Fatal("нет stem-drums: want ошибку, got nil")
	}
	if len(f.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", keys(f.uploads))
	}
}
