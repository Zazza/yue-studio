package studio

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// Тесты карточки internal-splice, условия 1–2: Splice — новая версия из кусков
// аудио версий (audio.flac каждой джобы), куски [From, To) по порядку (To ≤ 0 —
// до конца), соседние сшиты переходом crossfade (≤ 0 → 0.05 с), GainDb — громкость
// куска; итог — вариант splice-<n>.flac у baseID. Ошибки — без загрузки.
// Фейковый воркер (secFake) и звуковые хелперы — из sections_test.go / helpers_test.go.
//
// Синтетика (16 кГц моно, 6 с, амплитуда 0.3):
//   A (джоба 11) — 440 Гц до 4 с, 550 Гц с 4 с (метка: кусок A 4–6 обязан быть 550);
//   B (джоба 12) — 770 Гц до 2 с, 660 Гц с 2 с (метка: кусок B 2–5 обязан быть 660).
// Окна анализа — целые секунды внутри кусков, в стороне от швов.

const (
	spliceBase int64 = 100
	spliceA    int64 = 11
	spliceB    int64 = 12
	spliceDur        = 6.0
	spliceAmp        = 0.3
)

const (
	exprSpliceA = "0.3*sin(2*PI*(440+110*gte(t\\,4))*t)"
	exprSpliceB = "0.3*sin(2*PI*(770-110*gte(t\\,2))*t)"
)

func spliceSetup(t *testing.T) *secFake {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	put(f, spliceA, map[string]string{"audio.flac": lavfi(t, aeval(exprSpliceA, spliceDur), filepath.Join(dir, "a.flac"))})
	put(f, spliceB, map[string]string{"audio.flac": lavfi(t, aeval(exprSpliceB, spliceDur), filepath.Join(dir, "b.flac"))})
	return f
}

// spliceOK — Splice без ошибки; ровно одна загрузка — к baseID, имя splice-*.flac,
// возвращённый вариант — этот файл. Возвращает декодированный итог.
func spliceOK(t *testing.T, f *secFake, parts []SplicePart, xf float64) []float32 {
	t.Helper()
	v, err := Splice(context.Background(), f, spliceBase, parts, xf)
	if err != nil {
		t.Fatalf("Splice: %v", err)
	}
	if len(f.uploads) != 1 {
		t.Fatalf("загрузок %d, want 1: %v", len(f.uploads), keys(f.uploads))
	}
	if len(f.uploadedTo) != 1 || f.uploadedTo[0] != spliceBase {
		t.Errorf("загружено к джобам %v, want [%d] (baseID)", f.uploadedTo, spliceBase)
	}
	var name string
	for n := range f.uploads {
		name = n
	}
	if !strings.HasPrefix(name, "dsp-splice-") || !strings.HasSuffix(name, ".flac") {
		t.Errorf("имя варианта %q, want splice-<n>.flac", name)
	}
	if v == nil || v.File != name {
		t.Errorf("вернулся вариант %+v, want File=%q", v, name)
	}
	return uploadedOnly(t, f)
}

func durOf(s []float32) float64 { return float64(len(s)) / sr }

// assertOnlyTone — на [from,to] звучит тон want с амплитудой ≈ amp (±0.5 дБ),
// остальные тоны синтетики ≈ 0.
func assertOnlyTone(t *testing.T, out []float32, from, to, want, amp float64, what string) {
	t.Helper()
	if a := toneAmp(out, want, from, to); math.Abs(db(a)-db(amp)) > 0.5 {
		t.Errorf("%s: %.0f Гц на %.2f–%.2f с = %.4f, want ≈ %.2f", what, want, from, to, a, amp)
	}
	for _, hz := range []float64{440, 550, 660, 770} {
		if hz == want {
			continue
		}
		if a := toneAmp(out, hz, from, to); a > 0.01 {
			t.Errorf("%s: на %.2f–%.2f с есть %.0f Гц (%.4f), want только %.0f", what, from, to, hz, a, want)
		}
	}
}

// Кейс карточки: [A 0–3, B 2–5, A 4–6], crossfade 0.05 → длина 3+3+2 − 2·0.05 = 7.9;
// в середине первого куска только 440, второго — только 660 (B с 2 с, не с 0),
// третьего — только 550 (A с 4 с). Внутри кусков уровень исходный.
func TestSpliceLengthAndPieceContent(t *testing.T) {
	f := spliceSetup(t)
	out := spliceOK(t, f, []SplicePart{
		{JobID: spliceA, From: 0, To: 3},
		{JobID: spliceB, From: 2, To: 5},
		{JobID: spliceA, From: 4, To: 6},
	}, 0.05)

	if d := durOf(out); math.Abs(d-7.9) > 0.05 {
		t.Errorf("длина %.3f с, want 7.9 ± 0.05 (3+3+2 − 2·0.05)", d)
	}
	// кусок 1 в итоге: 0–3; кусок 2: 2.95–5.95; кусок 3: 5.9–7.9
	assertOnlyTone(t, out, 1, 2, 440, spliceAmp, "кусок 1 (A 0–3)")
	assertOnlyTone(t, out, 3.95, 4.95, 660, spliceAmp, "кусок 2 (B 2–5)")
	assertOnlyTone(t, out, 6.4, 7.4, 550, spliceAmp, "кусок 3 (A 4–6)")
}

// Кейс карточки: gain_db ±6 у куска → его уровень на ~столько же дБ выше/ниже (±1),
// соседний кусок без gain_db не меняется.
func TestSpliceGainDb(t *testing.T) {
	for _, g := range []float64{6, -6} {
		f := spliceSetup(t)
		out := spliceOK(t, f, []SplicePart{
			{JobID: spliceA, From: 0, To: 3},
			{JobID: spliceA, From: 0, To: 3, GainDb: g},
		}, 0.05)
		plain := toneAmp(out, 440, 1, 2)
		gained := toneAmp(out, 440, 4, 5) // кусок 2: 2.95–5.95
		if math.Abs(db(plain)-db(spliceAmp)) > 0.5 {
			t.Errorf("gain %+.0f: кусок без gain_db %.4f, want ≈ %.2f (не тронут)", g, plain, spliceAmp)
		}
		if d := db(gained) - db(plain); math.Abs(d-g) > 1 {
			t.Errorf("gain %+.0f: кусок громче соседнего на %+.2f дБ, want %+.0f ± 1", g, d, g)
		}
	}
}

// Кейс карточки: To = 0 — кусок до конца файла: A 2–(конец 6) → длина 4 с,
// в конце — 550 (A после 4 с).
func TestSpliceToZeroTillEnd(t *testing.T) {
	f := spliceSetup(t)
	out := spliceOK(t, f, []SplicePart{{JobID: spliceA, From: 2, To: 0}}, 0.05)
	if d := durOf(out); math.Abs(d-4) > 0.05 {
		t.Errorf("длина %.3f с, want 4 ± 0.05 (A с 2 с до конца 6 с)", d)
	}
	assertOnlyTone(t, out, 0.5, 1.5, 440, spliceAmp, "A 2.5–3.5")
	assertOnlyTone(t, out, 2.5, 3.5, 550, spliceAmp, "A 4.5–5.5 (хвост до конца)")
}

// Условие 1: crossfade ≤ 0 → по умолчанию 0.05 с: [A 0–3, B 2–5] → 5.95 с.
func TestSpliceDefaultCrossfade(t *testing.T) {
	for _, xf := range []float64{0, -1} {
		f := spliceSetup(t)
		out := spliceOK(t, f, []SplicePart{
			{JobID: spliceA, From: 0, To: 3},
			{JobID: spliceB, From: 2, To: 5},
		}, xf)
		if d := durOf(out); math.Abs(d-5.95) > 0.05 {
			t.Errorf("crossfade %g: длина %.3f с, want 5.95 ± 0.05 (переход по умолчанию 0.05)", xf, d)
		}
	}
}

// Условие 2 на другом переходе: crossfade 1 с → 3+3 − 1 = 5 с.
func TestSpliceCustomCrossfadeLength(t *testing.T) {
	f := spliceSetup(t)
	out := spliceOK(t, f, []SplicePart{
		{JobID: spliceA, From: 0, To: 3},
		{JobID: spliceB, From: 2, To: 5},
	}, 1)
	if d := durOf(out); math.Abs(d-5) > 0.05 {
		t.Errorf("длина %.3f с, want 5 ± 0.05 (3+3 − 1)", d)
	}
}

// Кейсы карточки: ошибки — пустые parts, To ≤ From (To > 0), неизвестная джоба;
// при ошибке ничего не загружено.
func TestSpliceErrorsWithoutUpload(t *testing.T) {
	cases := []struct {
		name  string
		parts []SplicePart
	}{
		{"parts nil", nil},
		{"parts пустой", []SplicePart{}},
		{"to < from", []SplicePart{{JobID: spliceA, From: 3, To: 2}}},
		{"to == from", []SplicePart{{JobID: spliceA, From: 3, To: 3}}},
		{"плохой кусок после хорошего", []SplicePart{
			{JobID: spliceA, From: 0, To: 3}, {JobID: spliceB, From: 5, To: 1}}},
		{"неизвестная джоба", []SplicePart{
			{JobID: spliceA, From: 0, To: 3}, {JobID: 99, From: 0, To: 2}}},
	}
	for _, tc := range cases {
		f := spliceSetup(t)
		v, err := Splice(context.Background(), f, spliceBase, tc.parts, 0.05)
		if err == nil {
			t.Errorf("%s: want ошибку, got nil (вариант %+v)", tc.name, v)
		}
		if len(f.uploads) != 0 || len(f.uploadedTo) != 0 {
			t.Errorf("%s: при ошибке загружено %v", tc.name, keys(f.uploads))
		}
	}
}
