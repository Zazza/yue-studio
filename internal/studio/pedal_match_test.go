package studio

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"yue-studio/internal/dsp"
)

// Тесты карточки internal-guitar-pedals, условие 1.11: выравнивание громкости
// перегрузов (od-ts, fuzz-muff, dist-rat, fuzz-octave) на дорожке — в Preview и в
// RebuildSections громкость обработанной дорожки в окне ≈ исходной (±1.5 дБ) и
// для тихой (−25 дБ), и для громкой (−6 дБ) дорожки; крутилка level +6 дБ →
// дорожка громче исходной на 6 ± 1.5 дБ; у цепочек без перегруза (eq) выравнивания
// нет: eq +6 дБ → дорожка громче на ~6 дБ. Написаны по карточке, без чтения
// реализации.
//
// «Громкость» меряется как RMS дорожки в середине окна (вдали от краёв и фейдов).
// Обработанная дорожка восстанавливается из микса: микс_после − микс_до + исходная
// дорожка (по 1.4 / пересборке меняется только она) — так тест не зависит от соло (1.10).
//
// Синтетика 8 с: other («гитара») — синус 220 Гц с заданным RMS, drums — тихий
// синус 100 Гц (0.05); audio.flac = other + drums.

var pmDrives = []string{"od-ts", "fuzz-muff", "dist-rat", "fuzz-octave"}

const (
	pmDur      = 8.0
	pmQuietDb  = -25.0
	pmLoudDb   = -6.0
	pmTolDb    = 1.5
	pmRbFrom   = 2.0
	pmRbTo     = 6.0
	pmDrumExpr = "0.05*sin(2*PI*100*t)"
)

// pmSetup — фейковый воркер: дорожка other — синус 220 Гц с RMS rmsDb дБFS.
func pmSetup(t *testing.T, rmsDb float64) (*secFake, map[string]string) {
	t.Helper()
	needFFmpeg(t)
	amp := math.Pow(10, rmsDb/20) * math.Sqrt2
	gtr := fmt.Sprintf("%.6f*sin(2*PI*220*t)", amp)
	dir := t.TempDir()
	pf := map[string]string{
		"audio.flac":       lavfi(t, aeval(gtr+"+"+pmDrumExpr, pmDur), filepath.Join(dir, "pm-audio.flac")),
		"stem-other.flac":  lavfi(t, aeval(gtr, pmDur), filepath.Join(dir, "pm-other.flac")),
		"stem-drums.flac":  lavfi(t, aeval(pmDrumExpr, pmDur), filepath.Join(dir, "pm-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", pmDur), filepath.Join(dir, "pm-bass.flac")),
		"stem-vocals.flac": lavfi(t, aeval("0", pmDur), filepath.Join(dir, "pm-vocals.flac")),
	}
	f := newSecFake()
	put(f, parentID, pf)
	return f, pf
}

// pmMaxParams — все крутилки цепочки на максимум, кроме level (0 дБ): самый
// сильный перегруз, какой даёт педаль.
func pmMaxParams(t *testing.T, chain string) map[string]float64 {
	t.Helper()
	c := dsp.ByID(chain)
	if c == nil {
		t.Fatalf("нет цепочки %s", chain)
	}
	p := map[string]float64{}
	for _, q := range c.Params {
		p[q.ID] = q.Max
	}
	p["level"] = 0
	return p
}

// pmLevelParams — значения по умолчанию, level = gain дБ.
func pmLevelParams(t *testing.T, chain string, gain float64) map[string]float64 {
	t.Helper()
	c := dsp.ByID(chain)
	if c == nil {
		t.Fatalf("нет цепочки %s", chain)
	}
	p := map[string]float64{}
	hasLevel := false
	for _, q := range c.Params {
		p[q.ID] = q.Default
		hasLevel = hasLevel || q.ID == "level"
	}
	if !hasLevel {
		t.Fatalf("у %s нет крутилки level (громкость, дБ)", chain)
	}
	p["level"] = gain
	return p
}

// pmAdd — a − b + c поотсчётно (по короткой длине).
func pmAdd(a, b, c []float32) []float32 {
	n := min(len(a), len(b), len(c))
	r := make([]float32, n)
	for i := range r {
		r[i] = a[i] - b[i] + c[i]
	}
	return r
}

// pmPreviewGain — изменение громкости дорожки other превью шага, дБ.
func pmPreviewGain(t *testing.T, f *secFake, pf map[string]string, step dsp.Step) float64 {
	t.Helper()
	res := pvPreview(t, f, NewCache(t.TempDir()), pvSpec("other", step))
	wet, dry := pvDecode(t, res.Wet), pvDecode(t, res.Dry)
	src := slice(decodeFile(t, pf["stem-other.flac"]), pvFrom, pvDur)
	proc := pmAdd(wet, dry, src)
	// середина окна [From+0.3, To−0.3]
	a, b := 0.3, pvTo-pvFrom-0.3
	return db(rms(slice(proc, a, b))) - db(rms(slice(src, a, b)))
}

// pmRebuildGain — изменение громкости дорожки other после пересборки с шагом, дБ.
func pmRebuildGain(t *testing.T, f *secFake, pf map[string]string, step dsp.Step) float64 {
	t.Helper()
	run(t, f, SectionSpec{ChildID: 0, From: pmRbFrom, To: pmRbTo, Stems: []string{"other"},
		Steps: []dsp.Step{step}})
	out := uploadedOnly(t, f)
	base := decodeFile(t, pf["audio.flac"])
	src := decodeFile(t, pf["stem-other.flac"])
	proc := pmAdd(out, base, src)
	a, b := pmRbFrom+0.5, pmRbTo-0.5
	return db(rms(slice(proc, a, b))) - db(rms(slice(src, a, b)))
}

type pmPath struct {
	name string
	gain func(t *testing.T, f *secFake, pf map[string]string, step dsp.Step) float64
}

var pmPaths = []pmPath{{"Preview", pmPreviewGain}, {"RebuildSections", pmRebuildGain}}

// Карточка 1.11: перегруз на тихой (−25 дБ) и громкой (−6 дБ) дорожке — громкость
// обработанной дорожки в окне = исходной ± 1.5 дБ; и при крутилках по умолчанию,
// и при всех крутилках на максимуме (level 0).
func TestPedalDriveLoudnessMatched(t *testing.T) {
	for _, path := range pmPaths {
		for _, lvl := range []float64{pmQuietDb, pmLoudDb} {
			for _, chain := range pmDrives {
				for pname, params := range map[string]map[string]float64{
					"по умолчанию": pmLevelParams(t, chain, 0),
					"на максимуме": pmMaxParams(t, chain),
				} {
					name := fmt.Sprintf("%s/%+.0fдБ/%s/%s", path.name, lvl, chain, pname)
					t.Run(name, func(t *testing.T) {
						f, pf := pmSetup(t, lvl)
						d := path.gain(t, f, pf, dsp.Step{Chain: chain, Params: params})
						if math.Abs(d) > pmTolDb {
							t.Errorf("громкость дорожки изменилась на %+.2f дБ, want 0 ± %.1f (выравнивание перегруза)", d, pmTolDb)
						}
					})
				}
			}
		}
	}
}

// Карточка 1.11: крутилка level +6 дБ поверх выравнивания — дорожка громче исходной
// на 6 ± 1.5 дБ. Только тихая дорожка: у громкой (−6 дБ RMS) +6 дБ — это синус с
// пиком +3 дБFS, а пик перегруза ≤ 0 дБFS (карточка 3.2) — условие для неё
// невыполнимо одновременно с 3.2.
func TestPedalDriveLevelKnobOverMatch(t *testing.T) {
	for _, path := range pmPaths {
		for _, chain := range pmDrives {
			t.Run(path.name+"/"+chain, func(t *testing.T) {
				f, pf := pmSetup(t, pmQuietDb)
				d := path.gain(t, f, pf, dsp.Step{Chain: chain, Params: pmLevelParams(t, chain, 6)})
				if math.Abs(d-6) > pmTolDb {
					t.Errorf("level +6: дорожка громче на %+.2f дБ, want +6 ± %.1f", d, pmTolDb)
				}
			})
		}
	}
}

// Карточка 1.11: у цепочки без перегруза выравнивания нет — eq +6 дБ (колокол на
// частоте тона 220 Гц) делает дорожку громче на ~6 дБ (± 1.5), а не возвращает её
// к исходной громкости.
func TestPedalEqNotMatched(t *testing.T) {
	eq := dsp.Step{Chain: "eq", Params: map[string]float64{"mid": 6, "midf": 220, "midq": 1}}
	for _, path := range pmPaths {
		t.Run(path.name, func(t *testing.T) {
			f, pf := pmSetup(t, pmQuietDb)
			d := path.gain(t, f, pf, eq)
			if math.Abs(d-6) > pmTolDb {
				t.Errorf("eq +6 дБ: дорожка громче на %+.2f дБ, want +6 ± %.1f (у eq выравнивания нет)", d, pmTolDb)
			}
		})
	}
}
