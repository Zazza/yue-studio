package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-own-track, этап 8а, условие 67 (тест-кейс ТК101): громкость дорожки по замеру.
// Написаны по карточке, без чтения реализации.
//
// Контракт: у записи пресета yue.PresetSpec.LevelDb *float64 (json level_db, −40…+6) — громкость дорожки
// к треку после применения. ApplySoundPreset: уровень дорожки = rms_p95_db её замера (JobStems:
// metrics.metrics.rms_p95_db) − сумма мощностей основных дорожек с замером (vocals, drums, bass, other);
// Db записи += clamp(level_db − уровень, −12, +12). Нет замера дорожки или основных — Db как в записи,
// без ошибки.
//
// Как видно Db пересборки: запись «только громкость» ({stems [X], level_db}, без обработки) меняет в
// треке дорожку X на Db дБ. У каждой дорожки свой тон (vocals 3000, other 500, bass 200, hh 5000 Гц),
// поэтому Db = амплитуда тона X в результате пересборки (dsp-preset-<id>-mix.flac) к амплитуде в
// исходном треке, в дБ; середина трека (2–8 с) — без краёв окна. Замеры (rms_p95_db) задаёт фейк
// независимо от звука — как числа воркера.
//
// Ожидания — по формуле условия 67: сумма основных при p95 vocals −20, drums −20, other −20, bass −30 —
// 10·lg(3·10⁻² + 10⁻³) = −15,086 (в ТК101 написано −14,95 — арифметическая неточность карточки);
// bass: уровень −14,914, level_db −10 → Db +4,914 (в пределах ТК101 «+4,95 ±0,05»);
// hh −40: уровень −24,914, level_db −28 → Db −3,086. В ТК101 написано «+3,05» — знак расходится с
// формулой условия 67 (−28 − (−24,91) < 0: дорожку надо сделать ТИШЕ); тест следует условию,
// расхождение вынесено в отчёт test-author.

const (
	lvDur   = 10.0
	lvVoc   = "0.1*sin(2*PI*3000*t)"
	lvOther = "0.1*sin(2*PI*500*t)"
	lvBass  = "0.05*sin(2*PI*200*t)"
	lvHH    = "0.05*sin(2*PI*5000*t)"
)

// lvFake — psFake (presets_test.go) + замеры громкости дорожек в списке JobStems, как у воркера.
type lvFake struct {
	*psFake
	p95 map[string]float64 // дорожка → rms_p95_db; нет в карте — у дорожки нет замера
}

func (f *lvFake) JobStems(ctx context.Context, id int64) ([]map[string]any, error) {
	list, err := f.psFake.JobStems(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, s := range list {
		name, _ := s["name"].(string)
		if v, ok := f.p95[name]; ok {
			s["metrics"] = map[string]any{"file": s["file"],
				"metrics": map[string]any{"rms_p95_db": v, "rms_median_db": v - 6, "peak": 0.5}}
		}
	}
	return list, nil
}

// lvSetup — трек: vocals 3000 + other 500 + bass 200 + drums (= hh 5000) Гц; дорожки и часть hh файлами.
func lvSetup(t *testing.T, p95 map[string]float64) (*lvFake, []float32) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	p := func(n string) string { return filepath.Join(dir, n) }
	files := map[string]string{
		"audio.flac":       lavfi(t, aeval(strings.Join([]string{lvVoc, lvOther, lvBass, lvHH}, "+"), lvDur), p("lv-audio.flac")),
		"stem-vocals.flac": lavfi(t, aeval(lvVoc, lvDur), p("lv-vocals.flac")),
		"stem-other.flac":  lavfi(t, aeval(lvOther, lvDur), p("lv-other.flac")),
		"stem-bass.flac":   lavfi(t, aeval(lvBass, lvDur), p("lv-bass.flac")),
		"stem-drums.flac":  lavfi(t, aeval(lvHH, lvDur), p("lv-drums.flac")),
		"stem-hh.flac":     lavfi(t, aeval(lvHH, lvDur), p("lv-hh.flac")),
	}
	sf := newSecFake()
	put(sf, psJobID, files)
	f := &psFake{
		engFake: newEngFake(t, sf),
		jobs: []yue.Job{{ID: psJobID, Title: psTitle, Status: "done", AudioFile: "audio.flac",
			DurationSec: lvDur}},
	}
	return &lvFake{psFake: f, p95: p95}, decodeFile(t, files["audio.flac"])
}

// lvMain — замеры ТК101: vocals, drums, other −20, bass −30 (+ hh −40).
func lvMain() map[string]float64 {
	return map[string]float64{"vocals": -20, "drums": -20, "other": -20, "bass": -30, "hh": -40}
}

func lvF(v float64) *float64 { return &v }

// lvSum — сумма мощностей основных дорожек ТК101, дБ (−15,086).
var lvSum = 10 * math.Log10(3*math.Pow(10, -2)+math.Pow(10, -3))

// lvApplied — Db, на который пересборка изменила дорожку с тоном hz (дБ к исходному треку).
func lvApplied(t *testing.T, f *lvFake, orig []float32, p yue.SoundPreset, hz float64) float64 {
	t.Helper()
	if _, err := ApplySoundPreset(context.Background(), f, psJobID, p); err != nil {
		t.Fatalf("ApplySoundPreset: %v", err)
	}
	name := fmt.Sprintf("dsp-preset-%d-mix.flac", p.ID)
	data, ok := f.uploads[name]
	if !ok {
		t.Fatalf("пересборки нет: загружено %v, want %s", keys(map[string][]byte(f.uploads)), name)
	}
	out := decodeBytes(t, data)
	a, ref := toneAmp(out, hz, 2, 8), toneAmp(orig, hz, 2, 8)
	if ref <= 0 || a <= 0 {
		t.Fatalf("тон %.0f Гц: в результате %.5f, в исходном %.5f", hz, a, ref)
	}
	return 20 * math.Log10(a/ref)
}

func lvPreset(id int64, specs ...yue.PresetSpec) yue.SoundPreset {
	return yue.SoundPreset{ID: id, Name: "Громкость", Specs: specs}
}

// ТК101: bass level_db −10 → Db = −10 − (−30 − сумма основных) = +4,91 ±0,05.
func TestPresetLevelDbBass(t *testing.T) {
	f, orig := lvSetup(t, lvMain())
	got := lvApplied(t, f, orig, lvPreset(31, yue.PresetSpec{Stems: []string{"bass"}, LevelDb: lvF(-10)}), 200)
	want := -10 - (-30 - lvSum)
	if math.Abs(got-want) > 0.05 {
		t.Errorf("бас изменён на %+.3f дБ, want %+.3f ±0,05 (level_db −10 при уровне баса %.2f)", got, want, -30-lvSum)
	}
	// остальные дорожки не тронуты
	if d := lvAppliedNoRun(t, f, orig, 31, 3000); math.Abs(d) > 0.05 {
		t.Errorf("голос изменён на %+.3f дБ, want 0 (запись только на бас)", d)
	}
}

// ТК101: Db записи задан — поправка складывается с ним: −2 + 4,91.
func TestPresetLevelDbAddsToRecordDb(t *testing.T) {
	f, orig := lvSetup(t, lvMain())
	got := lvApplied(t, f, orig, lvPreset(32, yue.PresetSpec{Stems: []string{"bass"}, LevelDb: lvF(-10), Db: -2}), 200)
	want := -2 + (-10 - (-30 - lvSum))
	if math.Abs(got-want) > 0.05 {
		t.Errorf("бас изменён на %+.3f дБ, want %+.3f ±0,05 (db −2 + поправка level_db)", got, want)
	}
}

// ТК101: поправка не больше ±12 дБ: level_db +6 → +12, level_db −40 → −12.
func TestPresetLevelDbClamp(t *testing.T) {
	for _, c := range []struct {
		level, want float64
	}{{6, 12}, {-40, -12}} {
		t.Run(fmt.Sprintf("level_db %+g", c.level), func(t *testing.T) {
			f, orig := lvSetup(t, lvMain())
			got := lvApplied(t, f, orig, lvPreset(33, yue.PresetSpec{Stems: []string{"bass"}, LevelDb: lvF(c.level)}), 200)
			if math.Abs(got-c.want) > 0.05 {
				t.Errorf("бас изменён на %+.3f дБ, want %+g (предел поправки)", got, c.want)
			}
		})
	}
}

// ТК101: у дорожки нет замера → Db как в записи, без ошибки.
func TestPresetLevelDbNoStemMetric(t *testing.T) {
	m := lvMain()
	delete(m, "bass")
	f, orig := lvSetup(t, m)
	got := lvApplied(t, f, orig, lvPreset(34, yue.PresetSpec{Stems: []string{"bass"}, LevelDb: lvF(-10), Db: -6}), 200)
	if math.Abs(got - -6) > 0.05 {
		t.Errorf("бас изменён на %+.3f дБ, want −6 (замера баса нет — db записи как есть)", got)
	}
}

// Условие 67: нет замера ни у одной основной дорожки → Db как в записи, без ошибки.
func TestPresetLevelDbNoMainMetrics(t *testing.T) {
	f, orig := lvSetup(t, map[string]float64{"hh": -40})
	got := lvApplied(t, f, orig, lvPreset(35, yue.PresetSpec{Stems: []string{"hh"}, LevelDb: lvF(-28)}), 5000)
	if math.Abs(got) > 0.05 {
		t.Errorf("hh изменён на %+.3f дБ, want 0 (основных замеров нет — db записи 0 как есть)", got)
	}
}

// ТК101: часть барабанов hh (замер −40) — та же сумма основных: level_db −28 → Db = −28 − (−40 − сумма) = −3,09.
func TestPresetLevelDbDrumPart(t *testing.T) {
	f, orig := lvSetup(t, lvMain())
	got := lvApplied(t, f, orig, lvPreset(36, yue.PresetSpec{Stems: []string{"hh"}, LevelDb: lvF(-28)}), 5000)
	want := -28 - (-40 - lvSum)
	if math.Abs(got-want) > 0.05 {
		t.Errorf("hh изменён на %+.3f дБ, want %+.3f ±0,05 (сумма основных %.2f — без частей барабанов)", got, want, lvSum)
	}
}

// Условие 69: поле level_db в JSON записи пресета (воркер ↔ приложение).
func TestPresetSpecLevelDbJSON(t *testing.T) {
	b, err := json.Marshal(yue.PresetSpec{Stems: []string{"bass"}, LevelDb: lvF(-8)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"level_db":-8`) {
		t.Errorf("JSON записи %s, want поле level_db: -8", b)
	}
	var sp yue.PresetSpec
	if err := json.Unmarshal([]byte(`{"stems":["hh"],"level_db":-28.5}`), &sp); err != nil {
		t.Fatal(err)
	}
	if sp.LevelDb == nil || *sp.LevelDb != -28.5 {
		t.Errorf("level_db из JSON = %v, want −28,5", sp.LevelDb)
	}
	var none yue.PresetSpec
	if err := json.Unmarshal([]byte(`{"stems":["hh"],"db":2}`), &none); err != nil {
		t.Fatal(err)
	}
	if none.LevelDb != nil {
		t.Errorf("level_db без поля = %v, want nil (нет цели — громкость не трогается)", *none.LevelDb)
	}
}

// lvAppliedNoRun — изменение тона hz в уже сделанной пересборке пресета id (дБ к исходному).
func lvAppliedNoRun(t *testing.T, f *lvFake, orig []float32, id int64, hz float64) float64 {
	t.Helper()
	out := decodeBytes(t, f.uploads[fmt.Sprintf("dsp-preset-%d-mix.flac", id)])
	return 20 * math.Log10(toneAmp(out, hz, 2, 8)/toneAmp(orig, hz, 2, 8))
}
