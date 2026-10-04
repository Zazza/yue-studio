package studio

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// Тесты карточки «дорожки гитары и клавиш»: помимо 4 основных стемов у трека
// есть «подробные» stem-guitar.flac и stem-piano.flac — уточнение внутри other,
// в сумму трека не входят. Пересборка (заглушка/громкость/эффект/линия
// громкости) работает с ними как с обычной дорожкой. Фейковый воркер (secFake)
// и синтетика — из sections_test.go/stem_fx_test.go, звуковые хелперы — из
// helpers_test.go.
//
// Синтетика (fxDur = 8 с): подробная дорожка — тон 3000 Гц; other = 500 Гц +
// тот же тон 3000 Гц (гитара/клавиши живут внутри «прочего»); голос, барабаны,
// бас — тишина; audio.flac = 500 + 3000 (сумма ОСНОВНЫХ стемов). Значит,
// «трек минус дорожка гитары» = только 500 Гц.

// detailFiles — файлы родителя: основные стемы и, если withDetail, stem-<detail>.flac.
func detailFiles(t *testing.T, detail string, withDetail bool) (base map[string]string, detailPath string) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	mix := exprB500 + "+" + exprA3000
	base = map[string]string{
		"audio.flac":       lavfi(t, aeval(mix, fxDur), filepath.Join(dir, "d-audio.flac")),
		"stem-other.flac":  lavfi(t, aeval(mix, fxDur), filepath.Join(dir, "d-other.flac")),
		"stem-vocals.flac": lavfi(t, aeval("0", fxDur), filepath.Join(dir, "d-vocals.flac")),
		"stem-drums.flac":  lavfi(t, aeval("0", fxDur), filepath.Join(dir, "d-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", fxDur), filepath.Join(dir, "d-bass.flac")),
	}
	detailPath = lavfi(t, aeval(exprA3000, fxDur), filepath.Join(dir, "d-"+detail+".flac"))
	if withDetail {
		base["stem-"+detail+".flac"] = detailPath
	}
	return base, detailPath
}

// detailSetup — родитель со всеми основными стемами и подробной дорожкой detail.
func detailSetup(t *testing.T, detail string) (*secFake, []float32) {
	t.Helper()
	pf, _ := detailFiles(t, detail, true)
	f := newSecFake()
	put(f, parentID, pf)
	return f, decodeFile(t, pf["audio.flac"])
}

func muteDetail(detail string) SectionSpec {
	return SectionSpec{ChildID: 0, From: 2, To: 4, Stems: []string{detail}, Db: -100}
}

// assertDetailRemovedInWindow — в окне 2–4 с тон дорожки (3000) снят, 500 Гц на месте,
// вне окна выход = база.
func assertDetailRemovedInWindow(t *testing.T, out, base []float32, what string) {
	t.Helper()
	if d := float64(len(out)) / sr; math.Abs(d-fxDur) > 0.05 {
		t.Errorf("%s: длина выхода %.3f с, want %.0f ± 0.05 (длина базы)", what, d, fxDur)
	}
	if a := toneAmp(out, 3000, 2.5, 3.5); a > 0.01 {
		t.Errorf("%s: 3000 Гц (дорожка) в окне %.4f, want ≈ 0 (трек минус дорожка)", what, a)
	}
	if d := db(toneAmp(out, 500, 2.5, 3.5)) - db(fxAmpB); math.Abs(d) > 0.5 {
		t.Errorf("%s: 500 Гц в окне изменился на %+.2f дБ, want |Δ| ≤ 0.5 (прочие дорожки не трогаются)", what, d)
	}
	for _, w := range [][2]float64{{0, 1.5}, {4.5, fxDur}} {
		if r := relDiffDb(out, base, w[0], w[1]); r > -40 {
			t.Errorf("%s: вне окна %.1f–%.1f с разница с базой %.1f дБ, want ≤ −40", what, w[0], w[1], r)
		}
	}
}

// G1: заглушка дорожки guitar в окне — итог = база минус дорожка гитары в окне;
// остальные дорожки не трогаются; файл есть — MakeStems не нужен.
func TestDetailRebuildMuteGuitar(t *testing.T) {
	f, base := detailSetup(t, "guitar")
	run(t, f, muteDetail("guitar"))
	assertDetailRemovedInWindow(t, uploadedOnly(t, f), base, "guitar −100 дБ")
	if n := f.stemsCalls[parentID]; n != 0 {
		t.Errorf("stem-guitar.flac есть, а MakeStems вызван %d раз, want 0", n)
	}
	if !contains(f.fetched, key(parentID, "stem-guitar.flac")) {
		t.Errorf("stem-guitar.flac не скачивался; скачаны: %v", f.fetched)
	}
}

// G1 (симметрично для клавиш): заглушка piano.
func TestDetailRebuildMutePiano(t *testing.T) {
	f, base := detailSetup(t, "piano")
	run(t, f, muteDetail("piano"))
	assertDetailRemovedInWindow(t, uploadedOnly(t, f), base, "piano −100 дБ")
}

// G2: эффект (цепочка из dsp — «Убрать свист» на 3000 Гц) на Stems ["piano"]
// применяется в окне — результат в окне отличается от базы (3000 ослаблен), 500 Гц
// не тронут; вне окна — база.
func TestDetailRebuildChainOnPiano(t *testing.T) {
	f, base := detailSetup(t, "piano")
	run(t, f, SectionSpec{ChildID: 0, From: 2, To: 4, Stems: []string{"piano"},
		Chain: "dewhistle", Params: notch3000()})
	out := uploadedOnly(t, f)

	if r := relDiffDb(out, base, 2.5, 3.5); r < -20 {
		t.Errorf("в окне выход почти равен базе (%.1f дБ) — эффект на piano не применён", r)
	}
	if d := db(toneAmp(out, 3000, 2.5, 3.5)) - db(fxAmpA); d > -15 {
		t.Errorf("3000 Гц (клавиши) в окне изменился на %+.1f дБ, want ≤ −15", d)
	}
	if d := db(toneAmp(out, 500, 2.5, 3.5)) - db(fxAmpB); math.Abs(d) > 0.5 {
		t.Errorf("500 Гц в окне изменился на %+.2f дБ, want |Δ| ≤ 0.5", d)
	}
	for _, w := range [][2]float64{{0, 1.5}, {4.5, fxDur}} {
		if r := relDiffDb(out, base, w[0], w[1]); r > -40 {
			t.Errorf("вне окна %.1f–%.1f с разница с базой %.1f дБ, want ≤ −40", w[0], w[1], r)
		}
	}
}

// G3: stem-guitar.flac нет, основные стемы есть → MakeStems(id) ровно один раз,
// затем дорожка скачивается и применяется.
func TestDetailRebuildMakesStemsForMissingGuitar(t *testing.T) {
	pf, guitar := detailFiles(t, "guitar", false)
	f := newSecFake()
	put(f, parentID, pf)
	f.afterStems[parentID] = map[string]string{"stem-guitar.flac": guitar}

	run(t, f, muteDetail("guitar"))
	if n := f.stemsCalls[parentID]; n != 1 {
		t.Errorf("MakeStems(родитель) вызван %d раз, want ровно 1", n)
	}
	assertDetailRemovedInWindow(t, uploadedOnly(t, f), decodeFile(t, pf["audio.flac"]), "guitar после MakeStems")
}

// G3: после MakeStems дорожки гитары всё равно нет — ошибка с понятным текстом
// (называет дорожку), ничего не загружено; не молчаливый пропуск. MakeStems упал —
// тоже ошибка без загрузки.
func TestDetailRebuildGuitarUnavailableIsError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failed bool
	}{{"MakeStems без гитары", false}, {"MakeStems ошибка", true}} {
		t.Run(tc.name, func(t *testing.T) {
			pf, _ := detailFiles(t, "guitar", false)
			f := newSecFake()
			f.stemsFailed = tc.failed
			put(f, parentID, pf)

			_, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{muteDetail("guitar")})
			if err == nil {
				t.Fatal("ошибки нет, want ошибку: дорожки гитары нет и после MakeStems")
			}
			// понятный текст обязателен, когда MakeStems прошёл, а дорожки нет;
			// при сбое самого MakeStems достаточно его ошибки
			if msg := strings.ToLower(err.Error()); !tc.failed && !strings.Contains(msg, "guitar") && !strings.Contains(msg, "гитар") {
				t.Errorf("текст ошибки %q не называет дорожку (guitar/гитара)", err.Error())
			}
			if n := f.stemsCalls[parentID]; n != 1 {
				t.Errorf("MakeStems(родитель) вызван %d раз, want ровно 1", n)
			}
			if len(f.uploads) != 0 {
				t.Errorf("при ошибке загружено: %v", keys(f.uploads))
			}
		})
	}
}

// G4: VolumeEnvelope принимает дорожку guitar: её тон идёт по огибающей, 500 Гц не меняется.
func TestDetailVolumeEnvelopeGuitar(t *testing.T) {
	f, _ := detailSetup(t, "guitar")
	v, err := VolumeEnvelope(context.Background(), f, parentID, "guitar", rampPts)
	if err != nil {
		t.Fatalf("VolumeEnvelope guitar: %v (want: дорожка допустима)", err)
	}
	if v == nil {
		t.Fatal("вариант nil")
	}
	out := uploadedOnly(t, f)
	if d := db(toneAmp(out, 3000, 0, 1)) - db(fxAmpA); math.Abs(d) > 0.5 {
		t.Errorf("гитара 0–1 с: %+.2f дБ, want ≈ 0", d)
	}
	if d := db(toneAmp(out, 3000, 4, 7)) - db(fxAmpA); math.Abs(d-(-12)) > 0.5 {
		t.Errorf("гитара 4–7 с: %+.2f дБ, want −12 ± 0.5", d)
	}
	for _, w := range [][2]float64{{0, 1}, {4, 7}} {
		if d := db(toneAmp(out, 500, w[0], w[1])) - db(fxAmpB); math.Abs(d) > 0.5 {
			t.Errorf("500 Гц %.0f–%.0f с: %+.2f дБ, want |Δ| ≤ 0.5", w[0], w[1], d)
		}
	}
}

// G4 (симметрично): piano тоже допустимая дорожка линии громкости.
func TestDetailVolumeEnvelopePiano(t *testing.T) {
	f, _ := detailSetup(t, "piano")
	if _, err := VolumeEnvelope(context.Background(), f, parentID, "piano", rampPts); err != nil {
		t.Fatalf("VolumeEnvelope piano: %v (want: дорожка допустима)", err)
	}
	if len(f.uploads) != 1 {
		t.Errorf("загрузок %d, want 1", len(f.uploads))
	}
}

// G4: неизвестная дорожка — ошибка, ничего не загружено, MakeStems не зовётся.
func TestDetailVolumeEnvelopeUnknownStem(t *testing.T) {
	f, _ := detailSetup(t, "guitar")
	for _, stem := range []string{"flute", "../x", "Guitar"} {
		if _, err := VolumeEnvelope(context.Background(), f, parentID, stem, rampPts); err == nil {
			t.Errorf("stem %q: want ошибку, got nil", stem)
		}
	}
	if len(f.uploads) != 0 {
		t.Errorf("при ошибке загружено: %v", keys(f.uploads))
	}
	if n := f.stemsCalls[parentID]; n != 0 {
		t.Errorf("на неизвестной дорожке MakeStems вызван %d раз, want 0", n)
	}
}

// G5: подпись пересборки называет дорожку по-русски: guitar → «гитара»
// (не общее «гитары/синты» дорожки other), piano → «клавиши».
func TestDetailRebuildLabel(t *testing.T) {
	for _, tc := range []struct {
		detail, want string
		spec         SectionSpec
	}{
		{"guitar", "гитара", muteDetail("guitar")},
		{"piano", "клавиши", muteDetail("piano")},
		{"piano", "клавиши", SectionSpec{ChildID: 0, From: 2, To: 4, Stems: []string{"piano"},
			Chain: "dewhistle", Params: notch3000()}},
	} {
		t.Run(tc.detail+"/"+tc.spec.Chain, func(t *testing.T) {
			f, _ := detailSetup(t, tc.detail)
			got := mixLabel(t, f, tc.spec)
			if !strings.Contains(got, tc.want) {
				t.Errorf("подпись %q, want содержит %q", got, tc.want)
			}
			if strings.Contains(got, "гитары/синты") {
				t.Errorf("подпись %q называет дорожку other вместо %s", got, tc.detail)
			}
		})
	}
}
