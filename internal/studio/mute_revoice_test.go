package studio

import (
	"math"
	"os/exec"
	"path/filepath"
	"testing"
)

// Тесты карточки 1.4 (громкость дорожек, гейн новой дорожки после провала) и
// 1.6 (Revoice — «перепеть»). Фейковый воркер и синтетика — из sections_test.go,
// звуковые хелперы — из helpers_test.go.

// --- 1.4 muteInserts: «громкость дорожек» (ChildID 0) ---

func muteSpec(stems []string, db float64) SectionSpec {
	return SectionSpec{ChildID: 0, From: 4, To: 8, Stems: stems, Db: db}
}

// Гейн = 10^(Db/20) − 1 — добавка к уже звучащему стему; Db ≤ −60 → ровно −1.
func TestMuteInsertsGain(t *testing.T) {
	parent := stemSet{"drums": "/p/drums.flac"}
	for _, tc := range []struct {
		db, want, tol float64
	}{
		{6, math.Pow(10, 6.0/20) - 1, 1e-9},   // ≈ +0.995
		{-6, math.Pow(10, -6.0/20) - 1, 1e-9}, // ≈ −0.499
		{0, 0, 1e-12},
		{-59, math.Pow(10, -59.0/20) - 1, 1e-9}, // ещё не заглушение
		{-60, -1, 0},
		{-100, -1, 0},
	} {
		var inputs []string
		ins := muteInserts(muteSpec([]string{"drums"}, tc.db), parent, &inputs)
		if len(ins) != 1 {
			t.Fatalf("Db=%g: %d вставок, want 1", tc.db, len(ins))
		}
		if g := ins[0].Gain; math.Abs(g-tc.want) > tc.tol {
			t.Errorf("Db=%g: Gain=%.6f, want %.6f", tc.db, g, tc.want)
		}
	}
}

// По вставке на каждый стем спеки, которого есть у родителя, в порядке Stems;
// пути стемов родителя дописываются в inputs (уже лежащее там не трогается).
func TestMuteInsertsStemsAndInputs(t *testing.T) {
	parent := stemSet{"drums": "/p/drums.flac", "other": "/p/other.flac", "vocals": "/p/vocals.flac"}
	inputs := []string{"/p/audio.flac"}
	ins := muteInserts(muteSpec([]string{"other", "bass", "vocals", "drums"}, -100), parent, &inputs)

	want := []string{"/p/audio.flac", "/p/other.flac", "/p/vocals.flac", "/p/drums.flac"}
	if len(inputs) != len(want) {
		t.Fatalf("inputs=%v, want %v (bass у родителя нет — пропущен)", inputs, want)
	}
	for i := range want {
		if inputs[i] != want[i] {
			t.Errorf("inputs[%d]=%q, want %q", i, inputs[i], want[i])
		}
	}
	if len(ins) != 3 {
		t.Fatalf("%d вставок, want 3 (по одной на имеющийся стем)", len(ins))
	}
	for i, in := range ins {
		if in.Gain != -1 {
			t.Errorf("вставка %d: Gain=%v, want −1", i, in.Gain)
		}
	}
}

// Вставка — сам стем родителя на его же месте в треке и покрывает окно From..To.
func TestMuteInsertsCoverWindow(t *testing.T) {
	var inputs []string
	ins := muteInserts(muteSpec([]string{"drums"}, -100), stemSet{"drums": "/p/d.flac"}, &inputs)
	if len(ins) != 1 {
		t.Fatalf("%d вставок, want 1", len(ins))
	}
	in := ins[0]
	const eps = 1e-6
	if math.Abs(in.SkipSec-in.AtSec) > eps {
		t.Errorf("SkipSec=%v, AtSec=%v — стем родителя сдвинут относительно трека", in.SkipSec, in.AtSec)
	}
	if in.AtSec > 4+eps {
		t.Errorf("AtSec=%v, want ≤ From=4", in.AtSec)
	}
	if in.DurSec <= 0 || in.AtSec+in.DurSec < 8-eps {
		t.Errorf("AtSec+DurSec=%v, want ≥ To=8 (DurSec=%v)", in.AtSec+in.DurSec, in.DurSec)
	}
	if in.Tempo != 0 && in.Tempo != 1 {
		t.Errorf("Tempo=%v — стем родителя не растягивается", in.Tempo)
	}
}

// To = 0 — до конца трека (как у эффекта на дорожку и у dsp_apply): раньше
// окно выходило отрицательным, и пересборка молча ничего не меняла (#459).
func TestMuteInsertsToZeroMeansEnd(t *testing.T) {
	var inputs []string
	spec := SectionSpec{ChildID: 0, From: 0, To: 0, Stems: []string{"vocals"}, Db: -100}
	ins := muteInserts(spec, stemSet{"vocals": "/p/v.flac"}, &inputs)
	if len(ins) != 1 {
		t.Fatalf("%d вставок, want 1", len(ins))
	}
	if ins[0].DurSec < 600 {
		t.Errorf("DurSec=%v при To=0, want окно до конца трека (не короче любой песни)", ins[0].DurSec)
	}
	if ins[0].AtSec > 0 {
		t.Errorf("AtSec=%v, want ≤ From=0", ins[0].AtSec)
	}
}

// Краевые: пустой Stems, стемов у родителя нет — вставок нет, inputs не растут.
func TestMuteInsertsNothingToDo(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stems  []string
		parent stemSet
	}{
		{"пустой Stems", nil, stemSet{"drums": "/p/d.flac"}},
		{"нет у родителя", []string{"drums", "bass"}, stemSet{"other": "/p/o.flac"}},
	} {
		inputs := []string{"/p/audio.flac"}
		ins := muteInserts(muteSpec(tc.stems, -100), tc.parent, &inputs)
		if len(ins) != 0 || len(inputs) != 1 {
			t.Errorf("%s: вставок %d, inputs %v — want 0 и без изменений", tc.name, len(ins), inputs)
		}
	}
}

// uploadedOnly — единственная загрузка (имя файла у «громкости дорожек» карточкой не задано).
func uploadedOnly(t *testing.T, f *secFake) []float32 {
	t.Helper()
	if len(f.uploads) != 1 {
		t.Fatalf("загрузок %d, want 1: %v", len(f.uploads), keys(f.uploads))
	}
	for _, data := range f.uploads {
		return decodeBytes(t, data)
	}
	return nil
}

// Сквозной: ChildID 0, drums Db −100 — в окне щелчков нет, other на месте, вне окна база.
func TestRebuildSectionsMuteDrums(t *testing.T) {
	needFFmpeg(t)
	f := newSecFake()
	pf := parentFiles(t, t.TempDir())
	put(f, parentID, pf)
	run(t, f, muteSpec([]string{"drums"}, -100))
	out := uploadedOnly(t, f)
	base := decodeFile(t, pf["audio.flac"])

	if p := peak(out, 4.5, 7.5); p > 0.45 {
		t.Errorf("в окне пик %.3f — drums не заглушены (want < 0.45, только синус 0.3)", p)
	}
	if a := toneAmp(out, 440, 4.5, 7.5); math.Abs(a-amp440) > 0.03 {
		t.Errorf("440 Гц в окне %.4f, want ≈ %.2f (other не трогался)", a, amp440)
	}
	for _, w := range [][2]float64{{0, 3.5}, {8.5, trackDur}} {
		if r := diffRMS(out, base, w[0], w[1]); r > 0.01 {
			t.Errorf("вне окна %.1f–%.1f с выход отличается от базы: %.4f", w[0], w[1], r)
		}
	}
}

// Сквозной: other +6 дБ — в окне 440 вдвое громче, вне окна как был; Db 0 — выход = база.
func TestRebuildSectionsMuteLouderAndZero(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	pf := parentFiles(t, dir)

	f := newSecFake()
	put(f, parentID, pf)
	run(t, f, muteSpec([]string{"other"}, 6))
	out := uploadedOnly(t, f)
	if a := toneAmp(out, 440, 4.5, 7.5); math.Abs(db(a)-db(amp440)-6) > 1 {
		t.Errorf("Db +6: 440 Гц в окне %.4f (%+.1f дБ к базе), want +6 ± 1", a, db(a)-db(amp440))
	}
	if a := toneAmp(out, 440, 1, 3); math.Abs(a-amp440) > 0.03 {
		t.Errorf("Db +6: 440 Гц вне окна %.4f, want ≈ %.2f", a, amp440)
	}

	f0 := newSecFake()
	put(f0, parentID, pf)
	run(t, f0, muteSpec([]string{"other"}, 0))
	base := decodeFile(t, pf["audio.flac"])
	if r := diffRMS(uploadedOnly(t, f0), base, 0, trackDur); r > 0.01 {
		t.Errorf("Db 0: выход отличается от базы, RMS разности %.4f", r)
	}
}

// --- 1.4 stemGain: опора громкости при провале в окне ---

// dipFiles — старая дорожка 16 с: тон 0.5 везде, кроме окна 8–12 с, где уровень inWin;
// новая — тон 0.25 длиной 8 с, в треке с 6 с (start).
func dipFiles(t *testing.T, inWin string) (oldPath, newPath string) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	oldExpr := "if(between(t\\,8\\,12)\\," + inWin + "\\,0.5)*sin(2*PI*440*t)"
	oldPath = lavfi(t, aeval(oldExpr, 16), filepath.Join(dir, "old.flac"))
	newPath = lavfi(t, aeval("0.25*sin(2*PI*440*t)", 8), filepath.Join(dir, "new.flac"))
	return
}

var dipSpec = SectionSpec{ChildID: 7, From: 8, To: 12, Lead: 2, BeatSec: 0.5, Stems: []string{"drums"}}

func gainOf(t *testing.T, oldP, newP string, s SectionSpec, accent bool) float64 {
	t.Helper()
	g, err := stemGain(oldP, newP, 6, s, accent)
	if err != nil {
		t.Fatalf("stemGain: %v", err)
	}
	return g
}

// Окно без провала: новая дорожка выравнивается по старой в окне (0.25 → 0.5: ×2), сверху Db.
func TestStemGainMatchesWindow(t *testing.T) {
	oldP, newP := dipFiles(t, "0.5")
	for _, tc := range []struct{ db, want float64 }{{0, 2}, {6, 2 * math.Pow(10, 6.0/20)}, {-6, 2 * math.Pow(10, -6.0/20)}} {
		s := dipSpec
		s.Db = tc.db
		if g := gainOf(t, oldP, newP, s, false); math.Abs(db(g)-db(tc.want)) > 1 {
			t.Errorf("Db=%g: gain %.3f, want ≈ %.3f", tc.db, g, tc.want)
		}
	}
}

// Карточка: в окне старой дорожки тишина → опора — уровень перед окном: gain не ~0,
// новая дорожка выходит примерно на уровень до окна (0.25 × gain ≈ 0.5).
func TestStemGainSilentWindowUsesLevelBefore(t *testing.T) {
	oldP, newP := dipFiles(t, "0")
	for _, accent := range []bool{false, true} {
		g := gainOf(t, oldP, newP, dipSpec, accent)
		if g < 0.1 {
			t.Fatalf("accent=%v: gain %.4f — сбивка после провала почти беззвучна", accent, g)
		}
		if d := db(0.25*g) - db(0.5); math.Abs(d) > 2 {
			t.Errorf("accent=%v: новая дорожка %+.1f дБ к уровню до окна, want ≈ 0 ± 2", accent, d)
		}
	}
}

// Провал ≥ 12 дБ (−20 дБ) — тоже «молчит»: опора до окна.
func TestStemGainDeepDipUsesLevelBefore(t *testing.T) {
	oldP, newP := dipFiles(t, "0.05")
	g := gainOf(t, oldP, newP, dipSpec, false)
	if d := db(0.25*g) - db(0.5); math.Abs(d) > 2 {
		t.Errorf("провал −20 дБ: новая дорожка %+.1f дБ к уровню до окна, want ≈ 0 ± 2", d)
	}
}

// Провал меньше 12 дБ (−6 дБ) без accent — опора остаётся окно: новая на уровне окна.
func TestStemGainShallowDipKeepsWindow(t *testing.T) {
	oldP, newP := dipFiles(t, "0.25")
	g := gainOf(t, oldP, newP, dipSpec, false)
	if d := db(0.25*g) - db(0.25); math.Abs(d) > 1.5 {
		t.Errorf("провал −6 дБ: новая дорожка %+.1f дБ к уровню окна, want ≈ 0 ± 1.5", d)
	}
}

// accent: опора не тише бита перед окном — при провале −6 дБ новая на уровне до окна.
func TestStemGainAccentNotQuieterThanBefore(t *testing.T) {
	oldP, newP := dipFiles(t, "0.25")
	g := gainOf(t, oldP, newP, dipSpec, true)
	if d := db(0.25*g) - db(0.5); math.Abs(d) > 1.5 {
		t.Errorf("accent, провал −6 дБ: новая дорожка %+.1f дБ к уровню до окна, want ≈ 0 ± 1.5", d)
	}
	// accent не делает тише, если в окне громче, чем до него
	oldL, newL := dipFiles(t, "1")
	g = gainOf(t, oldL, newL, dipSpec, true)
	if d := db(0.25*g) - db(1); math.Abs(d) > 1.5 {
		t.Errorf("accent, окно громче: новая дорожка %+.1f дБ к уровню окна, want ≈ 0 ± 1.5", d)
	}
}

// --- 1.6 Revoice ---

const (
	exprVoxParent = "0.3*sin(2*PI*550*t)"  // голос родителя
	exprVoxChild  = "0.15*sin(2*PI*770*t)" // голос нового рендера
)

// voxSetup — как secSetup, но голос слышен: у родителя 550 Гц (в базе тоже), у ребёнка 770 Гц.
func voxSetup(t *testing.T) (*secFake, map[string]string) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	pf := parentFiles(t, dir)
	pf["audio.flac"] = lavfi(t, aeval(expr440+"+"+exprClicks+"+"+exprVoxParent, trackDur), filepath.Join(dir, "pv-audio.flac"))
	pf["stem-vocals.flac"] = lavfi(t, aeval(exprVoxParent, trackDur), filepath.Join(dir, "pv-vocals.flac"))
	put(f, parentID, pf)
	cf := childFiles(t, dir, 7, 8, exprClicks)
	cf["stem-vocals.flac"] = lavfi(t, aeval(exprVoxChild, 8), filepath.Join(dir, "cv-vocals.flac"))
	put(f, 7, cf)
	return f, pf
}

// Revoice: голос трека в окне заменён голосом рендера (ChildID > 0), остальное нетронуто.
func TestRebuildSectionsRevoiceReplacesVocals(t *testing.T) {
	f, _ := voxSetup(t)
	sp := spec7([]string{"vocals"}, 0)
	sp.Revoice = true
	run(t, f, sp)
	out := secUploaded(t, f, "overdub-inst-7.flac")

	if a := toneAmp(out, 770, 4.5, 7.5); a < 0.15 {
		t.Errorf("770 Гц (новый голос) в окне %.4f, want ≥ 0.15", a)
	}
	if a := toneAmp(out, 550, 4.5, 7.5); a > 0.03 {
		t.Errorf("550 Гц (старый голос) в окне %.4f, want < 0.03 (заменён)", a)
	}
	if a := toneAmp(out, 440, 4.5, 7.5); math.Abs(a-amp440) > 0.03 {
		t.Errorf("440 Гц в окне %.4f, want ≈ %.2f (other не трогался)", a, amp440)
	}
	for _, w := range [][2]float64{{1, 3}, {9, 11}} {
		if a := toneAmp(out, 550, w[0], w[1]); math.Abs(a-0.3) > 0.03 {
			t.Errorf("550 Гц на %.0f–%.0f с %.4f, want ≈ 0.3 (вне окна голос родной)", w[0], w[1], a)
		}
		if a := toneAmp(out, 770, w[0], w[1]); a > 0.01 {
			t.Errorf("770 Гц на %.0f–%.0f с %.4f, want ≈ 0", w[0], w[1], a)
		}
	}
}

// Без Revoice голос не заменяется: выход = база, нового голоса нет.
func TestRebuildSectionsNoRevoiceKeepsVocals(t *testing.T) {
	f, pf := voxSetup(t)
	run(t, f, spec7([]string{"vocals"}, 0))
	out := secUploaded(t, f, "overdub-inst-7.flac")
	base := decodeFile(t, pf["audio.flac"])
	if r := diffRMS(out, base, 0, trackDur); r > 0.01 {
		t.Errorf("без Revoice выход отличается от базы: RMS разности %.4f", r)
	}
	if a := toneAmp(out, 770, 4.5, 7.5); a > 0.01 {
		t.Errorf("без Revoice в окне 770 Гц %.4f — голос рендера попал в трек", a)
	}
}

// Громкость дорожки с KeepHighHz меняет только низ: заглушить речь в голосе и
// оставить тарелки, которые demucs отнёс к голосу (#258: «дыры» на месте речи).
func TestRebuildSectionsMuteKeepHigh(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	pf := parentFiles(t, dir)
	voc := "0.3*sin(2*PI*500*t)+0.3*sin(2*PI*6000*t)"
	pf["audio.flac"] = lavfi(t, aeval(expr440+"+"+exprClicks+"+"+voc, trackDur), filepath.Join(dir, "kh-audio.flac"))
	pf["stem-vocals.flac"] = lavfi(t, aeval(voc, trackDur), filepath.Join(dir, "kh-vocals.flac"))
	f := newSecFake()
	put(f, parentID, pf)
	spec := muteSpec([]string{"vocals"}, -100)
	spec.KeepHighHz = 3000
	run(t, f, spec)
	out := uploadedOnly(t, f)
	if a := toneAmp(out, 500, 4.5, 7.5); a > 0.03 {
		t.Errorf("500 Гц (речь) в окне %.4f — должен быть заглушён", a)
	}
	if a := toneAmp(out, 6000, 4.5, 7.5); math.Abs(a-0.3) > 0.06 {
		t.Errorf("6000 Гц (тарелки) в окне %.4f, want ≈ 0.3 — верх должен остаться", a)
	}
}

// Импортированный трек: звук лежит как audio.mp3 (#257) — пересборка берёт его,
// а не падает на отсутствующем audio.flac.
func TestRebuildSectionsImportedMp3Base(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	pf := parentFiles(t, dir)
	mp3 := filepath.Join(dir, "base.mp3")
	if out, err := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-i", pf["audio.flac"], mp3).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	delete(pf, "audio.flac")
	pf["audio.mp3"] = mp3
	f := newSecFake()
	put(f, parentID, pf)
	run(t, f, muteSpec([]string{"drums"}, -100))
	if a := toneAmp(uploadedOnly(t, f), 440, 4.5, 7.5); math.Abs(a-amp440) > 0.05 {
		t.Errorf("440 Гц в окне %.4f, want ≈ %.2f — база из audio.mp3", a, amp440)
	}
}
