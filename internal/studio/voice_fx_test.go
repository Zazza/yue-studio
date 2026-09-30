package studio

import (
	"math"
	"path/filepath"
	"testing"
)

// Тесты «эффекта на голос»: голосовая цепочка (Voice) на стеме vocals —
// громкость обработанного голоса выравнивается по RMS исходного (перегруз
// сжимает и громчит), сверху дБ спеки; группа не меняется. Фейковый воркер
// и хелперы — из sections_test.go, «ремонтные» цепочки без выравнивания —
// в stem_fx_test.go.
//
// Синтетика: голос (vocals) — тоны 1000 Гц (в полосе мегофона 400–3000) и
// 200 Гц (ниже полосы — мегафон его срезает), группа (other) — тон 500 Гц.

const (
	ampVoice1k  = 0.3
	ampVoice200 = 0.2
	exprVoice   = "0.3*sin(2*PI*1000*t)+0.2*sin(2*PI*200*t)"
	exprOther5  = "0.3*sin(2*PI*500*t)"
)

// voiceFxSetup — родитель: audio.flac = голос + группа, стемы vocals/other,
// drums/bass — тишина.
func voiceFxSetup(t *testing.T) (*secFake, []float32) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	pf := map[string]string{
		"audio.flac":       lavfi(t, aeval(exprVoice+"+"+exprOther5, fxDur), filepath.Join(dir, "vfx-audio.flac")),
		"stem-vocals.flac": lavfi(t, aeval(exprVoice, fxDur), filepath.Join(dir, "vfx-vocals.flac")),
		"stem-other.flac":  lavfi(t, aeval(exprOther5, fxDur), filepath.Join(dir, "vfx-other.flac")),
		"stem-drums.flac":  lavfi(t, aeval("0", fxDur), filepath.Join(dir, "vfx-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", fxDur), filepath.Join(dir, "vfx-bass.flac")),
	}
	put(f, parentID, pf)
	return f, decodeFile(t, pf["audio.flac"])
}

func voiceFxSpec(db float64) SectionSpec {
	return SectionSpec{ChildID: 0, From: 0, To: 0, Stems: []string{"vocals"},
		Chain: "megaphone", Params: map[string]float64{}, Db: db}
}

// voice200Drop — на сколько дБ тон 200 Гц (вне полосы мегофона) тише в выходе,
// чем в базе: положительное — срезан.
func voice200Drop(t *testing.T, out, base []float32, from, to float64) float64 {
	t.Helper()
	b := toneAmp(base, 200, from, to)
	if b <= 0 {
		t.Fatal("нулевая амплитуда 200 Гц в базе")
	}
	return db(toneAmp(out, 200, from, to)) - db(b)
}

// Карточка: мегафон на весь голос — уровень тона в полосе ≈ исходному
// (выравнивание RMS: перегруз громчит), тон ниже полосы срезан ≥ 12 дБ,
// группа не тронута.
func TestRebuildSectionsVoiceChainLevelMatch(t *testing.T) {
	f, base := voiceFxSetup(t)
	run(t, f, voiceFxSpec(0))
	out := uploadedOnly(t, f)

	if d := db(toneAmp(out, 1000, 1, 7)) - db(toneAmp(base, 1000, 1, 7)); math.Abs(d) > 2 {
		t.Errorf("голос 1000 Гц (в полосе): %+.1f дБ к исходному, want ±2 (RMS-выравнивание)", d)
	}
	if d := voice200Drop(t, out, base, 1, 7); d > -12 {
		t.Errorf("голос 200 Гц (ниже полосы): %+.1f дБ, want ≤ −12 (мегафон срезает)", d)
	}
	if d := db(toneAmp(out, 500, 1, 7)) - db(toneAmp(base, 500, 1, 7)); math.Abs(d) > 0.5 {
		t.Errorf("группа 500 Гц: %+.1f дБ, want ±0.5 (не трогается)", d)
	}
}

// Карточка: дБ спеки — поверх выравнивания: +6 дБ даёт +6 к тону в полосе,
// группа как была.
func TestRebuildSectionsVoiceChainDbOnTop(t *testing.T) {
	f, base := voiceFxSetup(t)
	run(t, f, voiceFxSpec(6))
	out := uploadedOnly(t, f)

	if d := db(toneAmp(out, 1000, 1, 7)) - db(toneAmp(base, 1000, 1, 7)); d < 4 || d > 8 {
		t.Errorf("голос 1000 Гц с Db=+6: %+.1f дБ к исходному, want 4..8", d)
	}
	if d := db(toneAmp(out, 500, 1, 7)) - db(toneAmp(base, 500, 1, 7)); math.Abs(d) > 0.5 {
		t.Errorf("группа 500 Гц при Db=+6 голоса: %+.1f дБ, want ±0.5", d)
	}
}

// Карточка: окно [From, To) — эффект только в нём: вне окна трек = исходный.
func TestRebuildSectionsVoiceChainWindow(t *testing.T) {
	f, base := voiceFxSetup(t)
	sp := voiceFxSpec(0)
	sp.From, sp.To = 3, 6
	run(t, f, sp)
	out := uploadedOnly(t, f)

	for _, w := range [][2]float64{{0, 2.4}, {6.6, fxDur}} {
		if r := relDiffDb(out, base, w[0], w[1]); r > -40 {
			t.Errorf("вне окна %.1f–%.1f с разница с исходным %.1f дБ, want ≤ −40", w[0], w[1], r)
		}
	}
	if d := voice200Drop(t, out, base, 3.5, 5.5); d > -12 {
		t.Errorf("в окне 200 Гц: %+.1f дБ, want ≤ −12 (эффект работает)", d)
	}
	if d := voice200Drop(t, out, base, 0, 2.4); math.Abs(d) > 0.5 {
		t.Errorf("вне окна 200 Гц: %+.1f дБ, want ±0.5 (эффект не вышел за окно)", d)
	}
}

// Карточка: гейн выравнивания в отчёте — приглушающий (перегруз громчит),
// не 1 и не потолок; общий RMS окна ≈ исходному.
func TestRebuildSectionsVoiceChainGainReported(t *testing.T) {
	f, base := voiceFxSetup(t)
	sp := voiceFxSpec(0)
	sp.From, sp.To = 2, 6
	res := run(t, f, sp)
	if len(res.Inserts) == 0 || res.Inserts[0].Gain <= 0 || res.Inserts[0].Gain >= 1 {
		t.Errorf("Gain отчёта = %+v, want 0..1 (перегруз громчит — гейн приглушающий)", res.Inserts)
	}
	out := uploadedOnly(t, f)
	if d := 20 * math.Log10(rms(slice(out, 2.5, 5.5))/rms(slice(base, 2.5, 5.5))); math.Abs(d) > 2.5 {
		t.Errorf("RMS окна %+.1f дБ к исходному, want ±2.5", d)
	}
}

// Карточка: та же цепочка на дорожке группы работает и там: мегафон на other —
// 500 Гц (в полосе) обработан и выровнен, голос не тронут.
func TestRebuildSectionsVoiceChainOnOtherStem(t *testing.T) {
	f, base := voiceFxSetup(t)
	run(t, f, SectionSpec{ChildID: 0, From: 0, To: 0, Stems: []string{"other"},
		Chain: "megaphone", Params: map[string]float64{}})
	out := uploadedOnly(t, f)

	if d := db(toneAmp(out, 1000, 1, 7)) - db(toneAmp(base, 1000, 1, 7)); math.Abs(d) > 0.5 {
		t.Errorf("голос при эффекте на other: %+.1f дБ, want ±0.5", d)
	}
	if d := db(toneAmp(out, 200, 1, 7)) - db(toneAmp(base, 200, 1, 7)); math.Abs(d) > 0.5 {
		t.Errorf("тон 200 Гц голоса при эффекте на other: %+.1f дБ, want ±0.5", d)
	}
	if d := db(toneAmp(out, 500, 1, 7)) - db(toneAmp(base, 500, 1, 7)); math.Abs(d) > 2.5 {
		t.Errorf("группа 500 Гц (обработана): %+.1f дБ к исходному, want ±2.5 (выравнивание)", d)
	}
}
