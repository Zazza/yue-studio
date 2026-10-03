package studio

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты RebuildSections — по контракту «+ инструмент/приём»: кусок трека
// перерендерен моделью целой группой, в треке меняются ТОЛЬКО выбранные стемы
// (drums/bass/other; vocals — никогда) в окне [From−FadeIn, To+FadeOut].
// База — audio.flac родителя, стемы — stem-<name>.flac родителя и ребёнка;
// нет стема — один MakeStems(id) и повторное скачивание; результат —
// overdub-inst-<ChildID последней спеки>.flac у родителя.
//
// Хелперы звука (lavfi, decodeBytes, slice, rms, needFFmpeg, key, keys, contains)
// — из inserts_test.go того же пакета.

var stemNames = []string{"drums", "bass", "other", "vocals"}

// secFake — фейковый воркер: только внешние границы (скачивание, стемы, загрузка).
// Остальные методы yue.Service не реализованы: их вызов — паника (нарушение контракта).
type secFake struct {
	yue.Service
	files       map[string]string           // "id/file" → путь на диске
	afterStems  map[int64]map[string]string // что появляется у джобы после MakeStems(id)
	stemsCalls  map[int64]int
	uploads     map[string][]byte
	labels      map[string]string // подпись варианта по имени файла
	uploadedTo  []int64
	fetched     []string
	stemsFailed bool // MakeStems возвращает ошибку
}

func newSecFake() *secFake {
	return &secFake{
		files:      map[string]string{},
		afterStems: map[int64]map[string]string{},
		stemsCalls: map[int64]int{},
		uploads:    map[string][]byte{},
		labels:     map[string]string{},
	}
}

func (f *secFake) FetchAudio(_ context.Context, id int64, file string) (io.ReadCloser, string, error) {
	k := key(id, file)
	f.fetched = append(f.fetched, k)
	p, ok := f.files[k]
	if !ok {
		return nil, "", &yue.StatusError{Code: 404, Msg: "404 not found: " + k}
	}
	r, err := os.Open(p)
	if err != nil {
		return nil, "", err
	}
	return r, file, nil
}

func (f *secFake) MakeStems(_ context.Context, id int64) (map[string]any, error) {
	f.stemsCalls[id]++
	if f.stemsFailed {
		return nil, errors.New("stems failed")
	}
	for name, p := range f.afterStems[id] {
		f.files[key(id, name)] = p
	}
	return map[string]any{"ok": true}, nil
}

func (f *secFake) UploadDsp(_ context.Context, id int64, fname, label string, data []byte) (*yue.DspVariant, error) {
	f.uploadedTo = append(f.uploadedTo, id)
	f.uploads[fname] = append([]byte(nil), data...)
	f.labels[fname] = label
	return &yue.DspVariant{File: fname}, nil
}

// --- синтетика (16 кГц моно) ---

const (
	trackDur = 12.0 // длина трека родителя, с
	clickAmp = 0.5  // щелчок drums (20 мс каждые 0.5 с)
	amp440   = 0.3  // other родителя
	amp660   = 0.15 // other ребёнка — вдвое тише
)

// выражения aevalsrc (запятые экранированы для lavfi)
const (
	exprClicks = "0.5*lt(mod(t\\,0.5)\\,0.02)"
	expr440    = "0.3*sin(2*PI*440*t)"
	expr660    = "0.15*sin(2*PI*660*t)"
)

func aeval(expr string, dur float64) string {
	return fmt.Sprintf("aevalsrc=exprs='%s':d=%g:s=16000", expr, dur)
}

// parentFiles — база и 4 стема родителя: drums = щелчки, other = синус 440, bass/vocals — тишина.
func parentFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	return map[string]string{
		"audio.flac":       lavfi(t, aeval(expr440+"+"+exprClicks, trackDur), filepath.Join(dir, "p-audio.flac")),
		"stem-drums.flac":  lavfi(t, aeval(exprClicks, trackDur), filepath.Join(dir, "p-drums.flac")),
		"stem-other.flac":  lavfi(t, aeval(expr440, trackDur), filepath.Join(dir, "p-other.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", trackDur), filepath.Join(dir, "p-bass.flac")),
		"stem-vocals.flac": lavfi(t, aeval("0", trackDur), filepath.Join(dir, "p-vocals.flac")),
	}
}

// childFiles — рендер куска длиной dur, начинающийся в треке в From−Lead (кратно 0.5 с,
// поэтому щелчки ребёнка совпадают со щелчками трека). drumsExpr — выражение для drums.
func childFiles(t *testing.T, dir string, id int64, dur float64, drumsExpr string) map[string]string {
	t.Helper()
	p := func(n string) string { return filepath.Join(dir, fmt.Sprintf("c%d-%s.flac", id, n)) }
	return map[string]string{
		"audio.flac":       lavfi(t, aeval(expr660+"+"+drumsExpr, dur), p("audio")),
		"stem-drums.flac":  lavfi(t, aeval(drumsExpr, dur), p("drums")),
		"stem-other.flac":  lavfi(t, aeval(expr660, dur), p("other")),
		"stem-bass.flac":   lavfi(t, aeval("0", dur), p("bass")),
		"stem-vocals.flac": lavfi(t, aeval("0", dur), p("vocals")),
	}
}

func put(f *secFake, id int64, files map[string]string) {
	for name, p := range files {
		f.files[key(id, name)] = p
	}
}

// secSetup — родитель со всеми стемами, ребёнок 7 (рендер 8 с с трека 2 с) со всеми стемами.
func secSetup(t *testing.T) (*secFake, map[string]string) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	pf := parentFiles(t, dir)
	put(f, parentID, pf)
	put(f, 7, childFiles(t, dir, 7, 8, exprClicks))
	return f, pf
}

// спека кейса 1: окно 4–8, рендер начат за 2 с до From (т.е. в 2 с трека)
func spec7(stems []string, db float64) SectionSpec {
	return SectionSpec{ChildID: 7, From: 4, To: 8, Lead: 2, BeatSec: 0.5,
		Stems: stems, Db: db, FadeIn: 0.1, FadeOut: 0.1}
}

func decodeFile(t *testing.T, path string) []float32 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return decodeBytes(t, data)
}

// toneAmp — амплитуда синуса частоты hz на [from,to] (одна точка ДПФ, как Гёрцель).
// Окна берутся целыми секундами: 440 и 660 Гц — целое число периодов, взаимно не
// протекают; гармоники щелчков (каждые 0.5 с, 20 мс) на этих частотах < 0.001.
func toneAmp(s []float32, hz, from, to float64) float64 {
	seg := slice(s, from, to)
	if len(seg) == 0 {
		return 0
	}
	w := 2 * math.Pi * hz / sr
	var re, im float64
	for i, v := range seg {
		re += float64(v) * math.Cos(w*float64(i))
		im -= float64(v) * math.Sin(w*float64(i))
	}
	return 2 * math.Hypot(re, im) / float64(len(seg))
}

func db(x float64) float64 { return 20 * math.Log10(x) }

func peak(s []float32, from, to float64) float64 {
	var m float64
	for _, v := range slice(s, from, to) {
		if a := math.Abs(float64(v)); a > m {
			m = a
		}
	}
	return m
}

// diffRMS — RMS разности двух сигналов на [from,to].
func diffRMS(a, b []float32, from, to float64) float64 {
	x, y := slice(a, from, to), slice(b, from, to)
	n := len(x)
	if len(y) < n {
		n = len(y)
	}
	if n == 0 {
		return math.Inf(1)
	}
	d := make([]float32, n)
	for i := 0; i < n; i++ {
		d[i] = x[i] - y[i]
	}
	return rms(d)
}

// assertClicks — на [from,to] щелчки drums на месте и одинарные: в каждом щелчке пик
// ≈ 0.5 + синус (≤ 0.36), между щелчками — только синус (< 0.45). Удвоенный щелчок дал бы > 1.
func assertClicks(t *testing.T, out []float32, from, to float64, what string) {
	t.Helper()
	for c := math.Ceil(from/0.5) * 0.5; c+0.5 <= to; c += 0.5 {
		if p := peak(out, c, c+0.02); p < 0.6 || p > 0.95 {
			t.Errorf("%s: щелчок на %.1f с пик %.3f, want 0.6..0.95", what, c, p)
		}
		if p := peak(out, c+0.1, c+0.4); p > 0.45 {
			t.Errorf("%s: между щелчками %.1f–%.1f с пик %.3f, want < 0.45", what, c+0.1, c+0.4, p)
		}
	}
}

func run(t *testing.T, f *secFake, specs ...SectionSpec) *RebuildResult {
	t.Helper()
	res, err := RebuildSections(context.Background(), f, parentID, specs)
	if err != nil {
		t.Fatalf("RebuildSections: %v", err)
	}
	return res
}

func secUploaded(t *testing.T, f *secFake, name string) []float32 {
	t.Helper()
	data, ok := f.uploads[name]
	if !ok {
		t.Fatalf("не загружен %q; загружены: %v", name, keys(f.uploads))
	}
	return decodeBytes(t, data)
}

// --- тест-кейсы карточки ---

// Кейс 1: Stems ["other"] — в окне 440 заменён на 660, вне окна — база; drums не тронуты;
// начало рендера в треке — From−Lead.
func TestRebuildSectionsReplacesOnlyChosenStemInWindow(t *testing.T) {
	f, pf := secSetup(t)
	res := run(t, f, spec7([]string{"other"}, 0))
	out := secUploaded(t, f, "overdub-inst-7.flac")
	base := decodeFile(t, pf["audio.flac"])

	if d := float64(len(out)) / sr; math.Abs(d-trackDur) > 0.05 {
		t.Errorf("длина выхода %.3f с, want %.0f ± 0.05 (длина базы)", d, trackDur)
	}
	// в окне: есть 660, нет 440
	if a := toneAmp(out, 660, 4.5, 7.5); a < 0.15 {
		t.Errorf("660 Гц в окне: амплитуда %.4f, want ≥ 0.15 (партия ребёнка звучит)", a)
	}
	if a := toneAmp(out, 440, 4.5, 7.5); a > 0.03 {
		t.Errorf("440 Гц в окне: амплитуда %.4f, want < 0.03 (стем родителя вычтен)", a)
	}
	// вне окна: есть 440, нет 660
	for _, w := range [][2]float64{{1, 3}, {9, 11}} {
		if a := toneAmp(out, 440, w[0], w[1]); math.Abs(a-amp440) > 0.03 {
			t.Errorf("440 Гц на %.0f–%.0f с: %.4f, want ≈ %.2f", w[0], w[1], a, amp440)
		}
		if a := toneAmp(out, 660, w[0], w[1]); a > 0.01 {
			t.Errorf("660 Гц на %.0f–%.0f с: %.4f, want ≈ 0 (вне окна ребёнка нет)", w[0], w[1], a)
		}
	}
	// вне окна [From−FadeIn, To+FadeOut] выход = база
	for _, w := range [][2]float64{{0, 3.85}, {8.15, trackDur}} {
		if r := diffRMS(out, base, w[0], w[1]); r > 0.01 {
			t.Errorf("на %.2f–%.2f с выход отличается от базы: RMS разности %.4f", w[0], w[1], r)
		}
	}
	// drums не трогались — щелчки одинарные по всему треку (кроме зон фейда)
	assertClicks(t, out, 0.5, 3.5, "до окна")
	assertClicks(t, out, 4.5, 7.5, "в окне")
	assertClicks(t, out, 8.5, 11.5, "после окна")

	if res == nil || len(res.Inserts) != 1 {
		t.Fatalf("Inserts = %+v, want 1 отчёт", res)
	}
	r := res.Inserts[0]
	if r.ChildID != 7 {
		t.Errorf("Report.ChildID=%d, want 7", r.ChildID)
	}
	if math.Abs(r.StartSec-2) > 0.03 {
		t.Errorf("Report.StartSec=%.4f, want 2 ± 0.03 (From−Lead)", r.StartSec)
	}
}

// Кейс 2: громкость — стем ребёнка выравнивается по RMS стема родителя в окне, сверху Db.
func TestRebuildSectionsLevelMatchAndDb(t *testing.T) {
	for _, tc := range []struct {
		db, wantDb float64
	}{{0, 0}, {6, 6}} {
		t.Run(fmt.Sprintf("Db=%+g", tc.db), func(t *testing.T) {
			f, _ := secSetup(t)
			run(t, f, spec7([]string{"other"}, tc.db))
			out := secUploaded(t, f, "overdub-inst-7.flac")
			in660 := toneAmp(out, 660, 4.5, 7.5)
			out440 := toneAmp(out, 440, 1, 3)
			if in660 <= 0 || out440 <= 0 {
				t.Fatalf("нулевая амплитуда: 660 в окне %.4f, 440 вне окна %.4f", in660, out440)
			}
			// ребёнок вдвое тише (−6 дБ) — без выравнивания разница была бы −6 + Db
			if d := db(in660) - db(out440); math.Abs(d-tc.wantDb) > 1.5 {
				t.Errorf("660 в окне относительно 440 вне окна: %+.2f дБ, want %+.0f ± 1.5", d, tc.wantDb)
			}
		})
	}
}

// Кейс 3а: Stems ["vocals"] — голос всегда родной, выход = база.
func TestRebuildSectionsVocalsIgnored(t *testing.T) {
	f, pf := secSetup(t)
	run(t, f, spec7([]string{"vocals"}, 0))
	out := secUploaded(t, f, "overdub-inst-7.flac")
	base := decodeFile(t, pf["audio.flac"])
	if d := float64(len(out)) / sr; math.Abs(d-trackDur) > 0.05 {
		t.Errorf("длина выхода %.3f с, want %.0f ± 0.05", d, trackDur)
	}
	if r := diffRMS(out, base, 0, trackDur); r > 0.01 {
		t.Errorf("Stems [vocals]: выход отличается от базы, RMS разности %.4f", r)
	}
	if a := toneAmp(out, 660, 4.5, 7.5); a > 0.01 {
		t.Errorf("Stems [vocals]: в окне 660 Гц %.4f — ребёнок попал в трек", a)
	}
}

// Кейс 3б: Stems ["drums"], у ребёнка drums в окне тихие → в окне щелчков нет, 440 на месте.
func TestRebuildSectionsDrumsReplacedBySilence(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	put(f, parentID, parentFiles(t, dir))
	put(f, 7, childFiles(t, dir, 7, 8, "0"))

	run(t, f, spec7([]string{"drums"}, 0))
	out := secUploaded(t, f, "overdub-inst-7.flac")

	if p := peak(out, 4.5, 7.5); p > 0.45 {
		t.Errorf("в окне пик %.3f — щелчки drums не вычтены (want < 0.45, только синус 0.3)", p)
	}
	if a := toneAmp(out, 440, 4.5, 7.5); math.Abs(db(a)-db(amp440)) > 1.5 {
		t.Errorf("440 Гц в окне %.4f, want ≈ %.2f (other не трогался)", a, amp440)
	}
	if a := toneAmp(out, 660, 4.5, 7.5); a > 0.01 {
		t.Errorf("660 Гц в окне %.4f — other ребёнка попал в трек, хотя Stems=[drums]", a)
	}
	assertClicks(t, out, 0.5, 3.5, "до окна")
	assertClicks(t, out, 8.5, 11.5, "после окна")
}

// Кейс 4: у ребёнка нет стемов → MakeStems(ребёнок) ровно 1 раз, стемы скачиваются снова → успех.
func TestRebuildSectionsMakesChildStemsOnce(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	put(f, parentID, parentFiles(t, dir))
	cf := childFiles(t, dir, 7, 8, exprClicks)
	f.files[key(7, "audio.flac")] = cf["audio.flac"]
	f.afterStems[7] = map[string]string{}
	for _, n := range stemNames {
		f.afterStems[7]["stem-"+n+".flac"] = cf["stem-"+n+".flac"]
	}

	run(t, f, spec7([]string{"other"}, 0))
	if f.stemsCalls[7] != 1 {
		t.Errorf("MakeStems(7) вызван %d раз, want 1", f.stemsCalls[7])
	}
	if f.stemsCalls[parentID] != 0 {
		t.Errorf("MakeStems(родитель) вызван %d раз при готовых стемах, want 0", f.stemsCalls[parentID])
	}
	n := 0
	for _, k := range f.fetched {
		if k == key(7, "stem-other.flac") {
			n++
		}
	}
	// порядок проб стемов до MakeStems — деталь реализации; контракт: после
	// разделения стем скачан и вклеен (проверка 660 Гц ниже)
	if n < 1 {
		t.Errorf("stem-other.flac ребёнка не скачан после MakeStems")
	}
	out := secUploaded(t, f, "overdub-inst-7.flac")
	if a := toneAmp(out, 660, 4.5, 7.5); a < 0.15 {
		t.Errorf("660 Гц в окне %.4f — стем ребёнка после MakeStems не вклеен", a)
	}
}

// Кейс 4б: у родителя нет стемов → MakeStems(родитель) ровно 1 раз → успех.
func TestRebuildSectionsMakesParentStemsOnce(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	pf := parentFiles(t, dir)
	f.files[key(parentID, "audio.flac")] = pf["audio.flac"]
	f.afterStems[parentID] = map[string]string{}
	for _, n := range stemNames {
		f.afterStems[parentID]["stem-"+n+".flac"] = pf["stem-"+n+".flac"]
	}
	put(f, 7, childFiles(t, dir, 7, 8, exprClicks))

	run(t, f, spec7([]string{"other"}, 0))
	if f.stemsCalls[parentID] != 1 {
		t.Errorf("MakeStems(родитель) вызван %d раз, want 1", f.stemsCalls[parentID])
	}
	if _, ok := f.uploads["overdub-inst-7.flac"]; !ok {
		t.Errorf("не загружен overdub-inst-7.flac; загрузки: %v", keys(f.uploads))
	}
}

// Кейс 5: стемов нет, MakeStems не помог (ошибка или стемы так и не появились) → ошибка, загрузки нет.
func TestRebuildSectionsStemsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failed bool
	}{{"MakeStems ошибка", true}, {"MakeStems без стемов", false}} {
		t.Run(tc.name, func(t *testing.T) {
			needFFmpeg(t)
			dir := t.TempDir()
			f := newSecFake()
			f.stemsFailed = tc.failed
			put(f, parentID, parentFiles(t, dir))
			f.files[key(7, "audio.flac")] = childFiles(t, dir, 7, 8, exprClicks)["audio.flac"]

			_, err := RebuildSections(context.Background(), f, parentID,
				[]SectionSpec{spec7([]string{"other"}, 0)})
			if err == nil {
				t.Fatal("ошибки нет, want ошибка: стемов ребёнка нет")
			}
			if f.stemsCalls[7] != 1 {
				t.Errorf("MakeStems(7) вызван %d раз, want ровно 1", f.stemsCalls[7])
			}
			if len(f.uploads) != 0 {
				t.Errorf("UploadDsp вызван при ошибке стемов: %v", keys(f.uploads))
			}
		})
	}
}

// Кейс 6: имя результата — по ChildID последней спеки, загрузка одна и родителю,
// отчёты — по одному на спеку в порядке спек; база — audio.flac родителя, не прошлый микс.
func TestRebuildSectionsResultNamedByLastSpec(t *testing.T) {
	f, _ := secSetup(t)
	dir := t.TempDir()
	// прошлый микс — громкий 1 кГц: взятый базой, он был бы слышен вне окон
	f.files[key(parentID, "overdub-inst-5.flac")] = lavfi(t,
		aeval("0.5*sin(2*PI*1000*t)", trackDur), filepath.Join(dir, "old.flac"))
	// ребёнок 9: рендер 4 с, начат в треке в 8 (From 9, Lead 1)
	put(f, 9, childFiles(t, dir, 9, 4, exprClicks))

	res := run(t, f,
		SectionSpec{ChildID: 7, From: 4, To: 6, Lead: 2, BeatSec: 0.5, Stems: []string{"other"}, FadeIn: 0.1, FadeOut: 0.1},
		SectionSpec{ChildID: 9, From: 9, To: 11, Lead: 1, BeatSec: 0.5, Stems: []string{"other"}, FadeIn: 0.1, FadeOut: 0.1},
	)
	if len(f.uploads) != 1 {
		t.Errorf("ожидалась одна загрузка, было %d: %v", len(f.uploads), keys(f.uploads))
	}
	for _, id := range f.uploadedTo {
		if id != parentID {
			t.Errorf("загрузка в джобу %d, want родителю %d", id, parentID)
		}
	}
	if res == nil || res.Variant == nil || res.Variant.File != "overdub-inst-9.flac" {
		t.Errorf("Variant = %+v, want File=overdub-inst-9.flac", res)
	}
	if res == nil || len(res.Inserts) != 2 {
		t.Fatalf("Inserts = %+v, want 2 отчёта", res)
	}
	if res.Inserts[0].ChildID != 7 || res.Inserts[1].ChildID != 9 {
		t.Errorf("порядок отчётов %d,%d, want 7,9", res.Inserts[0].ChildID, res.Inserts[1].ChildID)
	}
	if !contains(f.fetched, key(parentID, "audio.flac")) {
		t.Errorf("база audio.flac не скачивалась: %v", f.fetched)
	}
	for _, k := range f.fetched {
		if strings.Contains(k, "overdub-inst") {
			t.Errorf("скачан прошлый микс %q — база должна быть audio.flac", k)
		}
	}
	out := secUploaded(t, f, "overdub-inst-9.flac")
	if a := toneAmp(out, 1000, 1, 3); a > 0.01 {
		t.Errorf("1 кГц прошлого микса в выходе: %.4f", a)
	}
	// обе замены на своих местах
	for _, w := range [][2]float64{{4.5, 5.5}, {9.5, 10.5}} {
		if a := toneAmp(out, 660, w[0], w[1]); a < 0.15 {
			t.Errorf("660 Гц на %.1f–%.1f с: %.4f — замена не на месте", w[0], w[1], a)
		}
	}
	if a := toneAmp(out, 440, 7, 8); math.Abs(a-amp440) > 0.03 {
		t.Errorf("между окнами 440 Гц %.4f, want ≈ %.2f", a, amp440)
	}
}

// Кейс 6б: пустой specs — ошибка, ничего не загружено.
func TestRebuildSectionsEmptySpecs(t *testing.T) {
	f := newSecFake()
	for _, specs := range [][]SectionSpec{nil, {}} {
		if _, err := RebuildSections(context.Background(), f, parentID, specs); err == nil {
			t.Errorf("specs=%v: ошибки нет, want ошибка", specs)
		}
	}
	if len(f.uploads) != 0 {
		t.Errorf("UploadDsp вызван при пустом specs: %v", keys(f.uploads))
	}
}

// --- KeepHighHz: у старой дорожки вычитается только низ (сбивка вместо барабанов) ---

// hiSR — частота для синтетики с 9000 Гц: при 16 кГц он выше Найквиста (8 кГц).
const hiSR = 44100

// lavfiHi — сгенерировать файл 44.1 кГц моно из источника lavfi.
func lavfiHi(t *testing.T, src, path string) string {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", src, "-ac", "1", "-ar", strconv.Itoa(hiSR), path).CombinedOutput()
	if err != nil {
		t.Fatalf("lavfi %q: %v %s", src, err, out)
	}
	return path
}

func aevalHi(expr string, dur float64) string {
	return fmt.Sprintf("aevalsrc=exprs='%s':d=%g:s=%d", expr, dur, hiSR)
}

// decodeHi — декодировать загруженные байты в моно float32 44.1 кГц.
func decodeHi(t *testing.T, data []byte) []float32 {
	t.Helper()
	p := filepath.Join(t.TempDir(), "uploaded-hi.flac")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", p, "-f", "f32le", "-ac", "1", "-ar", strconv.Itoa(hiSR), "-").Output()
	if err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	s := make([]float32, len(raw)/4)
	for i := range s {
		s[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return s
}

// toneAmpHi — амплитуда синуса hz на [from,to] с для сигнала 44.1 кГц.
func toneAmpHi(s []float32, hz, from, to float64) float64 {
	a, b := int(from*hiSR), int(to*hiSR)
	if b > len(s) {
		b = len(s)
	}
	if a >= b {
		return 0
	}
	w := 2 * math.Pi * hz / hiSR
	var re, im float64
	for i, v := range s[a:b] {
		re += float64(v) * math.Cos(w*float64(i))
		im -= float64(v) * math.Sin(w*float64(i))
	}
	return 2 * math.Hypot(re, im) / float64(b-a)
}

const exprDrumsLoHi = "0.3*sin(2*PI*200*t)+0.3*sin(2*PI*9000*t)"

// keepHighSetup — родитель: drums = 200 + 9000 Гц (по 0.3), other = 440, база = drums + other;
// ребёнок 7 (рендер 8 с с трека 2 с): drums — тишина.
func keepHighSetup(t *testing.T) *secFake {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	p := func(n string) string { return filepath.Join(dir, n) }
	put(f, parentID, map[string]string{
		"audio.flac":       lavfiHi(t, aevalHi(exprDrumsLoHi+"+"+expr440, trackDur), p("p-audio.flac")),
		"stem-drums.flac":  lavfiHi(t, aevalHi(exprDrumsLoHi, trackDur), p("p-drums.flac")),
		"stem-other.flac":  lavfiHi(t, aevalHi(expr440, trackDur), p("p-other.flac")),
		"stem-bass.flac":   lavfiHi(t, aevalHi("0", trackDur), p("p-bass.flac")),
		"stem-vocals.flac": lavfiHi(t, aevalHi("0", trackDur), p("p-vocals.flac")),
	})
	put(f, 7, map[string]string{
		"audio.flac":       lavfiHi(t, aevalHi(expr660, 8), p("c7-audio.flac")),
		"stem-drums.flac":  lavfiHi(t, aevalHi("0", 8), p("c7-drums.flac")),
		"stem-other.flac":  lavfiHi(t, aevalHi(expr660, 8), p("c7-other.flac")),
		"stem-bass.flac":   lavfiHi(t, aevalHi("0", 8), p("c7-bass.flac")),
		"stem-vocals.flac": lavfiHi(t, aevalHi("0", 8), p("c7-vocals.flac")),
	})
	return f
}

// KeepHighHz 6000: в окне низ старых drums (200) вычтен, верх (9000) остался, other на месте.
// Новая тишина выравнивается гейном 0 — это нормально: добавлять нечего.
func TestRebuildSectionsKeepHighHzKeepsHighs(t *testing.T) {
	f := keepHighSetup(t)
	sp := spec7([]string{"drums"}, 0)
	sp.KeepHighHz = 6000
	run(t, f, sp)
	data, ok := f.uploads["overdub-inst-7.flac"]
	if !ok {
		t.Fatalf("не загружен overdub-inst-7.flac; загружены: %v", keys(f.uploads))
	}
	out := decodeHi(t, data)

	if d := float64(len(out)) / hiSR; math.Abs(d-trackDur) > 0.05 {
		t.Errorf("длина выхода %.3f с, want %.0f ± 0.05 (длина базы)", d, trackDur)
	}
	if a := toneAmpHi(out, 200, 4.5, 7.5); a > 0.05 {
		t.Errorf("200 Гц в окне %.4f, want < 0.05 (низ старых drums вычтен)", a)
	}
	// Допуск по верху шире ±0.06: фазовый сдвиг 2-полюсного lowpass даёт ≈0.37
	// (проверено ffmpeg на 44.1 кГц); полное вычитание дало бы < 0.06.
	if a := toneAmpHi(out, 9000, 4.5, 7.5); a < 0.24 || a > 0.42 {
		t.Errorf("9000 Гц в окне %.4f, want 0.24..0.42 (верх старых drums остаётся)", a)
	}
	if a := toneAmpHi(out, 440, 4.5, 7.5); math.Abs(a-amp440) > 0.03 {
		t.Errorf("440 Гц в окне %.4f, want ≈ %.2f (other не трогался)", a, amp440)
	}
	if a := toneAmpHi(out, 660, 4.5, 7.5); a > 0.01 {
		t.Errorf("660 Гц в окне %.4f — other ребёнка попал в трек, хотя Stems=[drums]", a)
	}
	// вне окна всё на месте
	for _, w := range [][2]float64{{1, 3}, {9, 11}} {
		for _, hz := range []float64{200, 9000, 440} {
			if a := toneAmpHi(out, hz, w[0], w[1]); math.Abs(a-0.3) > 0.03 {
				t.Errorf("%.0f Гц на %.0f–%.0f с: %.4f, want ≈ 0.3 (вне окна база)", hz, w[0], w[1], a)
			}
		}
	}
}

// KeepHighHz 0 — старая дорожка вычитается целиком: в окне нет ни 200, ни 9000.
func TestRebuildSectionsNoKeepHighHzRemovesAll(t *testing.T) {
	f := keepHighSetup(t)
	run(t, f, spec7([]string{"drums"}, 0))
	data, ok := f.uploads["overdub-inst-7.flac"]
	if !ok {
		t.Fatalf("не загружен overdub-inst-7.flac; загружены: %v", keys(f.uploads))
	}
	out := decodeHi(t, data)
	for _, hz := range []float64{200, 9000} {
		if a := toneAmpHi(out, hz, 4.5, 7.5); a > 0.05 {
			t.Errorf("KeepHighHz=0: %.0f Гц в окне %.4f, want < 0.05 (drums вычтены целиком)", hz, a)
		}
	}
	if a := toneAmpHi(out, 440, 4.5, 7.5); math.Abs(a-amp440) > 0.03 {
		t.Errorf("440 Гц в окне %.4f, want ≈ %.2f (other не трогался)", a, amp440)
	}
}

// Поле приходит из фронта по JSON под именем keep_high_hz.
func TestSectionSpecKeepHighHzJSON(t *testing.T) {
	var sp SectionSpec
	if err := json.Unmarshal([]byte(`{"child_id":7,"keep_high_hz":6000}`), &sp); err != nil {
		t.Fatal(err)
	}
	if sp.KeepHighHz != 6000 {
		t.Errorf("keep_high_hz → KeepHighHz=%v, want 6000", sp.KeepHighHz)
	}
}

// --- подпись микса (label для UploadDsp) ---
//
// Микс загружается с человеческой подписью: по каждой спеке «что · каким
// дорожкам» + окно (" M:SS–M:SS", " с M:SS", весь трек — ничего), спеки
// через " + ". Дорожки по-русски: vocals→голос, drums→барабаны, bass→бас,
// other→гитары/синты.

// labelSetup — родитель длиной dur: голос — тон 3000 Гц, other — 500 Гц,
// drums — щелчки, bass — тишина; audio.flac — их сумма.
func labelSetup(t *testing.T, dur float64) *secFake {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	f := newSecFake()
	put(f, parentID, map[string]string{
		"audio.flac":       lavfi(t, aeval(exprA3000+"+"+exprB500+"+"+exprClicks, dur), filepath.Join(dir, "l-audio.flac")),
		"stem-vocals.flac": lavfi(t, aeval(exprA3000, dur), filepath.Join(dir, "l-vocals.flac")),
		"stem-other.flac":  lavfi(t, aeval(exprB500, dur), filepath.Join(dir, "l-other.flac")),
		"stem-drums.flac":  lavfi(t, aeval(exprClicks, dur), filepath.Join(dir, "l-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", dur), filepath.Join(dir, "l-bass.flac")),
	})
	return f
}

// mixLabel — подпись единственного загруженного микса.
func mixLabel(t *testing.T, f *secFake, specs ...SectionSpec) string {
	t.Helper()
	run(t, f, specs...)
	if len(f.labels) != 1 {
		t.Fatalf("загрузок с подписью %d, want 1: %v", len(f.labels), f.labels)
	}
	for _, l := range f.labels {
		return l
	}
	return ""
}

func driveVocals(from, to float64) SectionSpec {
	return SectionSpec{ChildID: 0, From: from, To: to, Stems: []string{"vocals"}, Chain: "voice-drive"}
}

// Пример карточки: «Перегруз голоса» на голос по всему треку — окно не пишется.
func TestRebuildSectionsLabelEffectWholeTrack(t *testing.T) {
	f := labelSetup(t, 4)
	if got := mixLabel(t, f, driveVocals(0, 0)); got != "Перегруз голоса · голос" {
		t.Errorf("подпись %q, want %q", got, "Перегруз голоса · голос")
	}
}

// Окно с концом — " M:SS–M:SS" (минуты без ведущего нуля, секунды двумя цифрами).
func TestRebuildSectionsLabelEffectWindow(t *testing.T) {
	f := labelSetup(t, 90)
	want := "Перегруз голоса · голос 1:20–1:28"
	if got := mixLabel(t, f, driveVocals(80, 88)); got != want {
		t.Errorf("подпись %q, want %q", got, want)
	}
}

// To ≤ 0 при From > 0 — «с M:SS» (до конца трека).
func TestRebuildSectionsLabelEffectFromTillEnd(t *testing.T) {
	f := labelSetup(t, 70)
	want := "Перегруз голоса · голос с 1:05"
	if got := mixLabel(t, f, driveVocals(65, 0)); got != want {
		t.Errorf("подпись %q, want %q", got, want)
	}
}

// Заглушка (Db ≤ −60) нескольких дорожек — «заглушить», дорожки через ", ".
func TestRebuildSectionsLabelMute(t *testing.T) {
	f := labelSetup(t, 6)
	sp := SectionSpec{ChildID: 0, From: 2, To: 4, Stems: []string{"drums", "bass"}, Db: -100}
	want := "заглушить · барабаны, бас 0:02–0:04"
	if got := mixLabel(t, f, sp); got != want {
		t.Errorf("подпись %q, want %q", got, want)
	}
}

// Ровно −60 дБ — тоже «заглушить» (граница включительно).
func TestRebuildSectionsLabelMuteBoundary(t *testing.T) {
	f := labelSetup(t, 6)
	sp := SectionSpec{ChildID: 0, From: 2, To: 4, Stems: []string{"drums"}, Db: -60}
	if got := mixLabel(t, f, sp); got != "заглушить · барабаны 0:02–0:04" {
		t.Errorf("подпись %q", got)
	}
}

// Другая громкость — «громкость +N дБ», знак плюса пишется всегда.
func TestRebuildSectionsLabelGain(t *testing.T) {
	f := labelSetup(t, 6)
	sp := SectionSpec{ChildID: 0, From: 2, To: 4, Stems: []string{"other"}, Db: 6}
	want := "громкость +6 дБ · гитары/синты 0:02–0:04"
	if got := mixLabel(t, f, sp); got != want {
		t.Errorf("подпись %q, want %q", got, want)
	}
}

// Несколько спек — через " + " в порядке спек: эффект на голос и заглушка барабанов.
func TestRebuildSectionsLabelCombined(t *testing.T) {
	f := labelSetup(t, 8)
	mute := SectionSpec{ChildID: 0, From: 5, To: 7, Stems: []string{"drums"}, Db: -100}
	want := "Перегруз голоса · голос 0:01–0:03 + заглушить · барабаны 0:05–0:07"
	if got := mixLabel(t, f, driveVocals(1, 3), mute); got != want {
		t.Errorf("подпись %q, want %q", got, want)
	}
}

// Вклейка отрендеренного куска (ChildID > 0) — «вклейка #<id>».
func TestRebuildSectionsLabelInsert(t *testing.T) {
	f, _ := secSetup(t)
	want := "вклейка #7 · гитары/синты 0:04–0:08"
	if got := mixLabel(t, f, spec7([]string{"other"}, 0)); got != want {
		t.Errorf("подпись %q, want %q", got, want)
	}
}
