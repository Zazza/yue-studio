package studio

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-own-track, этап 6, условие 40 (ТК68, ТК78а): место дорожки в стерео
// в пересборке. SectionSpec.Place {pan, width}:
//   (а) у записи-добавления (Add) — матрица на её вставки;
//   (б) запись «место»: ChildID 0, одна дорожка (не mix), без engine/chain/steps/envelope —
//       итог дорожки = M·(её звук после всех правок); Db у неё не применяется; две на одну
//       дорожку — ошибка;
//   (в) подпись «место: <дорожка> 30 % вправо, ширина 1,4».
//
// Звук — стерео 16 кГц с L = R (на моно-входе панорама не проверяется): vocals — 3000 Гц 0,3,
// other — 500 Гц 0,3, drums/bass — тишина; трек = vocals + other. Матрица pan 1 на L = R = o:
// слева 0, справа o·√2. Воркер — plFake: secFake (скачивание/загрузка) + движок, который
// отдаёт «обработанный кусок» — стерео-тон 1000 Гц 0,2 (L = R) от From превью до конца файла.

const (
	plDur   = 10.0
	plAmp   = 0.3 // тоны дорожек
	plChunk = 0.2 // тон обработанного куска
)

// plFake — воркер для пересборки со стерео-звуком: только внешние границы.
type plFake struct {
	*secFake
	t     *testing.T
	dir   string
	calls []yue.FxRequest
}

func (f *plFake) WorkerConfig(context.Context) (map[string]any, error) {
	return map[string]any{"fx_preview": true}, nil
}

// ApplyFx — превью движка: файл от начала трека (pad), до From — тишина, дальше тон 1000 Гц.
func (f *plFake) ApplyFx(_ context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	f.calls = append(f.calls, req)
	if req.From == nil || req.To == nil {
		return nil, &yue.StatusError{Code: 422, Msg: "preview needs from/to"}
	}
	name := fmt.Sprintf("preview-fx-%08x.flac", len(f.calls))
	dur := *req.To + req.Fade + 1
	expr := fmt.Sprintf("%g*sin(2*PI*1000*t)*gte(t\\,%g)", plChunk, *req.From)
	p := plGen(f.t, expr, dur, filepath.Join(f.dir, name))
	f.files[key(id, name)] = p
	return &yue.DspVariant{File: name, DurationSec: dur}, nil
}

// plGen — стерео flac 16 кГц, оба канала — expr.
func plGen(t *testing.T, expr string, dur float64, path string) string {
	t.Helper()
	src := fmt.Sprintf("aevalsrc=exprs='%[1]s|%[1]s':d=%[2]g:s=16000", expr, dur)
	out, err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", src, "-c:a", "flac", "-sample_fmt", "s32", "-bits_per_raw_sample", "24", path).CombinedOutput()
	if err != nil {
		t.Fatalf("gen %q: %v %s", src, err, out)
	}
	return path
}

// plSetup — родитель: стерео-трек и дорожки (L = R).
func plSetup(t *testing.T) *plFake {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	p := func(n string) string { return filepath.Join(dir, n) }
	v := fmt.Sprintf("%g*sin(2*PI*3000*t)", plAmp)
	o := fmt.Sprintf("%g*sin(2*PI*500*t)", plAmp)
	sf := newSecFake()
	put(sf, parentID, map[string]string{
		"audio.flac":       plGen(t, v+"+"+o, plDur, p("pl-audio.flac")),
		"stem-vocals.flac": plGen(t, v, plDur, p("pl-vocals.flac")),
		"stem-other.flac":  plGen(t, o, plDur, p("pl-other.flac")),
		"stem-drums.flac":  plGen(t, "0", plDur, p("pl-drums.flac")),
		"stem-bass.flac":   plGen(t, "0", plDur, p("pl-bass.flac")),
	})
	return &plFake{secFake: sf, t: t, dir: dir}
}

// plDecode2 — загруженные байты → два канала float32 16 кГц.
func plDecode2(t *testing.T, data []byte) (l, r []float32) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "up.flac")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", p, "-f", "f32le", "-ac", "2", "-ar", "16000", "-").Output()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	n := len(raw) / 8
	l, r = make([]float32, n), make([]float32, n)
	for i := 0; i < n; i++ {
		l[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*8:]))
		r[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*8+4:]))
	}
	return l, r
}

// plRun — пересборка; единственный загруженный микс → каналы.
func plRun(t *testing.T, f *plFake, specs ...SectionSpec) (l, r []float32) {
	t.Helper()
	if _, err := RebuildSections(context.Background(), f, parentID, specs); err != nil {
		t.Fatalf("RebuildSections: %v", err)
	}
	if len(f.uploads) != 1 {
		t.Fatalf("загрузок %d, want 1: %v", len(f.uploads), keys(f.uploads))
	}
	for _, data := range f.uploads {
		l, r = plDecode2(t, data)
	}
	return l, r
}

func plPlace(stem string, pan, width float64) SectionSpec {
	return SectionSpec{ChildID: 0, Stems: []string{stem}, Place: &yue.Place{Pan: pan, Width: width}}
}

// plTone — амплитуда тона hz: want > 0 — ±0,1 дБ; want = 0 — не громче ref − 40 дБ.
func plTone(t *testing.T, what string, s []float32, hz, from, to, want, ref float64) {
	t.Helper()
	got := toneAmp(s, hz, from, to)
	if want == 0 {
		if got > ref*0.01 {
			t.Errorf("%s: %g Гц амплитуда %.5f (%.1f дБ к %.2f), want ≤ −40 дБ", what, hz, got, db(got/ref), ref)
		}
		return
	}
	if d := db(got / want); math.Abs(d) > 0.1 {
		t.Errorf("%s: %g Гц амплитуда %.5f, want %.5f ± 0,1 дБ (%.2f дБ)", what, hz, got, want, d)
	}
}

// ТК68 / усл. 40б: запись «место» other pan 1 без других правок — гитары только справа:
// левый = база − other (500 Гц слева нет), правый = база + (√2 − 1)·other; голос не тронут.
func TestRebuildSectionsPlaceOnlyRight(t *testing.T) {
	f := plSetup(t)
	l, r := plRun(t, f, plPlace("other", 1, 1))
	if d := float64(len(l)) / sr; math.Abs(d-plDur) > 0.05 {
		t.Errorf("длина выхода %.3f с, want %.0f ± 0.05", d, plDur)
	}
	plTone(t, "левый, other", l, 500, 1, 9, 0, plAmp)
	plTone(t, "правый, other", r, 500, 1, 9, plAmp*math.Sqrt2, plAmp)
	plTone(t, "левый, голос", l, 3000, 1, 9, plAmp, plAmp)
	plTone(t, "правый, голос", r, 3000, 1, 9, plAmp, plAmp)
}

// Усл. 40б: Db у записи «место» не применяется — результат тот же, что при Db 0.
func TestRebuildSectionsPlaceIgnoresDb(t *testing.T) {
	f := plSetup(t)
	sp := plPlace("other", 1, 1)
	sp.Db = -6
	l, r := plRun(t, f, sp)
	plTone(t, "левый, other", l, 500, 1, 9, 0, plAmp)
	plTone(t, "правый, other (Db −6 не применён)", r, 500, 1, 9, plAmp*math.Sqrt2, plAmp)
}

// ТК68: запись движка на other (замена в окне 2–6) + место pan 1 — в окне вклад other =
// M·(новая дорожка): кусок 1000 Гц только справа ×√2, исходная other (500 Гц) не возвращается
// ни в одном канале (≤ −40 дБ); вне окна other — справа ×√2. Порядок записей не важен.
func TestRebuildSectionsPlaceOverEngine(t *testing.T) {
	eng := SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"other"}, Engine: engChain()}
	for name, specs := range map[string][]SectionSpec{
		"движок, потом место": {eng, plPlace("other", 1, 1)},
		"место, потом движок": {plPlace("other", 1, 1), eng},
	} {
		t.Run(name, func(t *testing.T) {
			f := plSetup(t)
			l, r := plRun(t, f, specs...)
			plTone(t, "окно, левый, исходная other", l, 500, 3, 5, 0, plAmp)
			plTone(t, "окно, правый, исходная other", r, 500, 3, 5, 0, plAmp)
			plTone(t, "окно, левый, новая дорожка", l, 1000, 3, 5, 0, plChunk)
			plTone(t, "окно, правый, новая дорожка", r, 1000, 3, 5, plChunk*math.Sqrt2, plChunk)
			plTone(t, "вне окна, левый, other", l, 500, 7, 9, 0, plAmp)
			plTone(t, "вне окна, правый, other", r, 500, 7, 9, plAmp*math.Sqrt2, plAmp)
			plTone(t, "окно, левый, голос", l, 3000, 3, 5, plAmp, plAmp)
		})
	}
}

// Усл. 40б: матрица и на заглушение той же дорожки — в окне заглушения other нет ни слева,
// ни справа (без матрицы на вычитании слева осталось бы −other), вне окна — справа ×√2.
func TestRebuildSectionsPlaceOverMute(t *testing.T) {
	f := plSetup(t)
	mute := SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"other"}, Db: -100}
	l, r := plRun(t, f, mute, plPlace("other", 1, 1))
	plTone(t, "окно, левый, other", l, 500, 3, 5, 0, plAmp)
	plTone(t, "окно, правый, other", r, 500, 3, 5, 0, plAmp)
	plTone(t, "вне окна, левый, other", l, 500, 7, 9, 0, plAmp)
	plTone(t, "вне окна, правый, other", r, 500, 7, 9, plAmp*math.Sqrt2, plAmp)
}

// ТК68 / усл. 40а: запись-добавление (Add, дорожка mix) с Place pan −1 — партия только слева
// ×√2; трек под ней не тронут.
func TestRebuildSectionsAddWithPlace(t *testing.T) {
	f := plSetup(t)
	add := SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"mix"}, Engine: engChain(), Add: true,
		Place: &yue.Place{Pan: -1, Width: 1}}
	l, r := plRun(t, f, add)
	plTone(t, "окно, левый, партия", l, 1000, 3, 5, plChunk*math.Sqrt2, plChunk)
	plTone(t, "окно, правый, партия", r, 1000, 3, 5, 0, plChunk)
	plTone(t, "окно, левый, other трека", l, 500, 3, 5, plAmp, plAmp)
	plTone(t, "окно, правый, other трека", r, 500, 3, 5, plAmp, plAmp)
}

// ТК78а: добавление на дорожке bass с Place pan 1 + запись «место» bass pan −1 — вклад партии =
// M_place·M_add·партия. Партия L = R = c: M_add (pan 1) → (0, c·√2); M_place (pan −1) → (c, 0).
// Только M_add дал бы (0, c·√2), только M_place — (c·√2, 0): слева ровно c, справа ≈ 0.
func TestRebuildSectionsAddPlaceComposedWithStemPlace(t *testing.T) {
	for name, order := range map[string]bool{"добавление, потом место": false, "место, потом добавление": true} {
		t.Run(name, func(t *testing.T) {
			f := plSetup(t)
			add := SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"bass"}, Engine: engChain(), Add: true,
				Place: &yue.Place{Pan: 1, Width: 1}}
			specs := []SectionSpec{add, plPlace("bass", -1, 1)}
			if order {
				specs = []SectionSpec{specs[1], specs[0]}
			}
			l, r := plRun(t, f, specs...)
			plTone(t, "окно, левый, партия (M_place·M_add)", l, 1000, 3, 5, plChunk, plChunk)
			plTone(t, "окно, правый, партия", r, 1000, 3, 5, 0, plChunk)
			plTone(t, "окно, левый, other трека", l, 500, 3, 5, plAmp, plAmp)
			plTone(t, "окно, правый, other трека", r, 500, 3, 5, plAmp, plAmp)
		})
	}
}

// ТК68: две записи «место» на одну дорожку → ошибка, ничего не загружено.
func TestRebuildSectionsTwoPlacesSameStemIsError(t *testing.T) {
	f := plSetup(t)
	_, err := RebuildSections(context.Background(), f, parentID,
		[]SectionSpec{plPlace("other", 1, 1), plPlace("other", -0.5, 1.2)})
	if err == nil {
		t.Fatal("две записи «место» на other: want ошибку")
	}
	if len(f.uploads) != 0 {
		t.Errorf("загружено %v, want ничего", keys(f.uploads))
	}
}

// ТК68: запись «место» на mix → ошибка, ничего не загружено.
func TestRebuildSectionsPlaceOnMixIsError(t *testing.T) {
	f := plSetup(t)
	_, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{plPlace("mix", 1, 1)})
	if err == nil {
		t.Fatal("место на mix: want ошибку")
	}
	if len(f.uploads) != 0 {
		t.Errorf("загружено %v, want ничего", keys(f.uploads))
	}
}

// Усл. 39–40: место вне пределов (pan 2, width 3) → ошибка, ничего не загружено.
func TestRebuildSectionsPlaceOutOfRangeIsError(t *testing.T) {
	for _, p := range []yue.Place{{Pan: 2, Width: 1}, {Pan: 0, Width: 3}} {
		f := plSetup(t)
		_, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{plPlace("other", p.Pan, p.Width)})
		if err == nil {
			t.Errorf("место %+v: want ошибку", p)
		}
		if len(f.uploads) != 0 {
			t.Errorf("место %+v: загружено %v, want ничего", p, keys(f.uploads))
		}
	}
}

// Усл. 40в: подпись пересборки — «место: <дорожка> 30 % вправо, ширина 1,4».
func TestRebuildSectionsPlaceLabel(t *testing.T) {
	f := plSetup(t)
	plRun(t, f, plPlace("other", 0.3, 1.4))
	var label string
	for _, l := range f.labels {
		label = l
	}
	if !strings.Contains(label, "место: ") || !strings.Contains(label, "30 % вправо, ширина 1,4") {
		t.Errorf("подпись %q, want «место: <дорожка> 30 %% вправо, ширина 1,4»", label)
	}
}

// Договорённость интерфейса: place без width в JSON — ширина 1 (как есть); явный 0 — 0 (моно).
func TestSectionSpecPlaceJSONDefaultWidth(t *testing.T) {
	var s SectionSpec
	if err := json.Unmarshal([]byte(`{"child_id":0,"stems":["other"],"place":{"pan":0.3}}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.Place == nil || s.Place.Pan != 0.3 || s.Place.Width != 1 {
		t.Errorf("place без width = %+v, want {Pan 0.3 Width 1}", s.Place)
	}
	var z SectionSpec
	if err := json.Unmarshal([]byte(`{"stems":["other"],"place":{"pan":0,"width":0}}`), &z); err != nil {
		t.Fatal(err)
	}
	if z.Place == nil || z.Place.Width != 0 {
		t.Errorf("place с width 0 = %+v, want Width 0", z.Place)
	}
	var none SectionSpec
	if err := json.Unmarshal([]byte(`{"stems":["other"],"db":-6}`), &none); err != nil {
		t.Fatal(err)
	}
	if none.Place != nil {
		t.Errorf("запись без place: Place = %+v, want nil", none.Place)
	}
}

// --- ТК79 (усл. 40г): место голоса и место барабанов поверх правок их частей ---

// ТК79а: единственная запись «место» vocals pan 1 — голос только справа ×√2, слева ≈ 0;
// гитары (other) не тронуты.
func TestRebuildSectionsPlaceVocalsOnly(t *testing.T) {
	f := plSetup(t)
	l, r := plRun(t, f, plPlace("vocals", 1, 1))
	plTone(t, "левый, голос", l, 3000, 1, 9, 0, plAmp)
	plTone(t, "правый, голос", r, 3000, 1, 9, plAmp*math.Sqrt2, plAmp)
	plTone(t, "левый, other", l, 500, 1, 9, plAmp, plAmp)
	plTone(t, "правый, other", r, 500, 1, 9, plAmp, plAmp)
}

// plDrAmp — тоны частей барабанов: тише прочих, чтобы правый канал (×√2) не упирался в 0 дБFS.
const plDrAmp = 0.05

// plDrumSetup — plSetup, где барабаны звучат: drums = бочка 200 Гц + хэт 6000 Гц (по 0,05),
// части kick (200 Гц) и hh (6000 Гц) — отдельными дорожками (внутри drums); трек = голос +
// other + drums.
func plDrumSetup(t *testing.T) *plFake {
	t.Helper()
	f := plSetup(t)
	p := func(n string) string { return filepath.Join(f.dir, n) }
	v := fmt.Sprintf("%g*sin(2*PI*3000*t)", plAmp)
	o := fmt.Sprintf("%g*sin(2*PI*500*t)", plAmp)
	k := fmt.Sprintf("%g*sin(2*PI*200*t)", plDrAmp)
	h := fmt.Sprintf("%g*sin(2*PI*6000*t)", plDrAmp)
	put(f.secFake, parentID, map[string]string{
		"audio.flac":      plGen(t, v+"+"+o+"+"+k+"+"+h, plDur, p("pld-audio.flac")),
		"stem-drums.flac": plGen(t, k+"+"+h, plDur, p("pld-drums.flac")),
		"stem-kick.flac":  plGen(t, k, plDur, p("pld-kick.flac")),
		"stem-hh.flac":    plGen(t, h, plDur, p("pld-hh.flac")),
	})
	return f
}

// ТК79б: движок (sampler) на kick — замена в окне 2–6, + «место» drums pan 1 → слева нет ни
// барабанов (200 и 6000 Гц), ни новой бочки (1000 Гц куска) — ≤ −40 дБ; справа новая бочка
// ×√2 (своего места у kick нет: M_drums·I). Порядок записей не важен.
func TestRebuildSectionsDrumsPlaceOverKickEngine(t *testing.T) {
	sampler := []map[string]any{{"type": "sampler", "kit": "osdk/kick"}}
	eng := SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"kick"}, Engine: sampler}
	for name, specs := range map[string][]SectionSpec{
		"движок, потом место": {eng, plPlace("drums", 1, 1)},
		"место, потом движок": {plPlace("drums", 1, 1), eng},
	} {
		t.Run(name, func(t *testing.T) {
			f := plDrumSetup(t)
			l, r := plRun(t, f, specs...)
			plTone(t, "окно, левый, новая бочка", l, 1000, 3, 5, 0, plChunk)
			plTone(t, "окно, правый, новая бочка", r, 1000, 3, 5, plChunk*math.Sqrt2, plChunk)
			plTone(t, "окно, левый, исходная бочка", l, 200, 3, 5, 0, plDrAmp)
			plTone(t, "окно, правый, исходная бочка", r, 200, 3, 5, 0, plDrAmp)
			plTone(t, "окно, левый, хэт", l, 6000, 3, 5, 0, plDrAmp)
			plTone(t, "окно, правый, хэт", r, 6000, 3, 5, plDrAmp*math.Sqrt2, plDrAmp)
			plTone(t, "вне окна, левый, бочка", l, 200, 7, 9, 0, plDrAmp)
			plTone(t, "вне окна, правый, бочка", r, 200, 7, 9, plDrAmp*math.Sqrt2, plDrAmp)
			plTone(t, "окно, левый, голос", l, 3000, 3, 5, plAmp, plAmp)
		})
	}
}

// ТК79е / усл. 39а: моно-трек (импорт) + «место» other pan 1 → выход стерео; база приведена к
// стерео копией канала (L = R, без −3 дБ): голос в обоих каналах на прежнем уровне 0,3 ±0,1 дБ,
// other слева ≈ 0, справа ×√2.
func TestRebuildSectionsPlaceOnMonoTrack(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	p := func(n string) string { return filepath.Join(dir, n) }
	v := fmt.Sprintf("%g*sin(2*PI*3000*t)", plAmp)
	o := fmt.Sprintf("%g*sin(2*PI*500*t)", plAmp)
	sf := newSecFake()
	put(sf, parentID, map[string]string{ // lavfi (helpers_test.go) пишет моно 16 кГц
		"audio.flac":       lavfi(t, aeval(v+"+"+o, plDur), p("m-audio.flac")),
		"stem-vocals.flac": lavfi(t, aeval(v, plDur), p("m-vocals.flac")),
		"stem-other.flac":  lavfi(t, aeval(o, plDur), p("m-other.flac")),
		"stem-drums.flac":  lavfi(t, aeval("0", plDur), p("m-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", plDur), p("m-bass.flac")),
	})
	f := &plFake{secFake: sf, t: t, dir: dir}
	l, r := plRun(t, f, plPlace("other", 1, 1))
	for _, data := range f.uploads {
		up := filepath.Join(t.TempDir(), "up.flac")
		if err := os.WriteFile(up, data, 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0",
			"-show_entries", "stream=channels", "-of", "csv=p=0", up).Output()
		if err != nil {
			t.Fatalf("ffprobe: %v", err)
		}
		if ch := strings.TrimSpace(string(out)); ch != "2" {
			t.Errorf("каналов в выходе %s, want 2 (стерео)", ch)
		}
	}
	plTone(t, "левый, other", l, 500, 1, 9, 0, plAmp)
	plTone(t, "правый, other", r, 500, 1, 9, plAmp*math.Sqrt2, plAmp)
	plTone(t, "левый, голос", l, 3000, 1, 9, plAmp, plAmp)
	plTone(t, "правый, голос", r, 3000, 1, 9, plAmp, plAmp)
}

// plMonoFake — моно-воркер: превью движка — моно-кусок (тон 1000 Гц plChunk от From).
type plMonoFake struct{ *plFake }

func (f *plMonoFake) ApplyFx(_ context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	f.calls = append(f.calls, req)
	if req.From == nil || req.To == nil {
		return nil, &yue.StatusError{Code: 422, Msg: "preview needs from/to"}
	}
	name := fmt.Sprintf("preview-fx-%08x.flac", len(f.calls))
	dur := *req.To + req.Fade + 1
	expr := fmt.Sprintf("%g*sin(2*PI*1000*t)*gte(t\\,%g)", plChunk, *req.From)
	f.files[key(id, name)] = lavfi(f.t, aeval(expr, dur), filepath.Join(f.dir, name)) // моно
	return &yue.DspVariant{File: name, DurationSec: dur}, nil
}

// ТК79ж / усл. 39б: моно-трек и моно-дорожки (голос 3000, other 500, drums 200 Гц — по 0,2),
// «место» other pan 1 + заглушение drums (−100) + добавление на mix (моно-кусок 1000 Гц):
// барабанов нет (≤ −40 дБ), кусок звучит в обоих каналах на своём уровне ±0,1 дБ (копия канала,
// без −3 дБ), голос — тоже; other слева ≈ 0, справа ×√2.
func TestRebuildSectionsPlaceMonoOtherInsertsStereoCopy(t *testing.T) {
	needFFmpeg(t)
	const a = 0.2
	dir := t.TempDir()
	p := func(n string) string { return filepath.Join(dir, n) }
	v := fmt.Sprintf("%g*sin(2*PI*3000*t)", a)
	o := fmt.Sprintf("%g*sin(2*PI*500*t)", a)
	d := fmt.Sprintf("%g*sin(2*PI*200*t)", a)
	sf := newSecFake()
	put(sf, parentID, map[string]string{
		"audio.flac":       lavfi(t, aeval(v+"+"+o+"+"+d, plDur), p("mg-audio.flac")),
		"stem-vocals.flac": lavfi(t, aeval(v, plDur), p("mg-vocals.flac")),
		"stem-other.flac":  lavfi(t, aeval(o, plDur), p("mg-other.flac")),
		"stem-drums.flac":  lavfi(t, aeval(d, plDur), p("mg-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", plDur), p("mg-bass.flac")),
	})
	f := &plMonoFake{&plFake{secFake: sf, t: t, dir: dir}}
	mute := SectionSpec{ChildID: 0, From: 1, To: 9, Stems: []string{"drums"}, Db: -100}
	add := SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"mix"}, Engine: engChain(), Add: true}
	if _, err := RebuildSections(context.Background(), f, parentID,
		[]SectionSpec{plPlace("other", 1, 1), mute, add}); err != nil {
		t.Fatalf("RebuildSections: %v", err)
	}
	if len(f.uploads) != 1 {
		t.Fatalf("загрузок %d, want 1: %v", len(f.uploads), keys(f.uploads))
	}
	var l, r []float32
	for _, data := range f.uploads {
		l, r = plDecode2(t, data)
	}
	plTone(t, "левый, барабаны", l, 200, 2, 8, 0, a)
	plTone(t, "правый, барабаны", r, 200, 2, 8, 0, a)
	plTone(t, "левый, кусок", l, 1000, 3, 5, plChunk, plChunk)
	plTone(t, "правый, кусок", r, 1000, 3, 5, plChunk, plChunk)
	plTone(t, "левый, голос", l, 3000, 2, 8, a, a)
	plTone(t, "правый, голос", r, 3000, 2, 8, a, a)
	plTone(t, "левый, other", l, 500, 2, 8, 0, a)
	plTone(t, "правый, other", r, 500, 2, 8, a*math.Sqrt2, a)
}
