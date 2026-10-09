package studio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-own-track, этап 6, условие 45 (ТК73): мастер пресета звука.
// Применение: specs → final (ПК) → мастер на воркере (ApplyFx source mix, file = файл после
// final, in_place; файла-варианта нет — обычный рендер /fx) → VariantToTrack. target_lufs —
// блок limiter мастера: нет limiter — дописывается {limiter, target_lufs}; есть без target —
// получает target; шага level на ПК для цели нет.
//
// Воркер — msPresetFake: psFake (presets_test.go) + мастер. Превью движка (записи specs)
// уходят в psFake; вызов без превью — мастер: in_place отвечает тем же файлом, иначе —
// новым вариантом dsp-fx-mix-*.flac у джобы.

type msPresetFake struct {
	*psFake
	master []yue.FxRequest
	merr   error
}

func (f *msPresetFake) ApplyFx(ctx context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	if req.Preview {
		return f.psFake.ApplyFx(ctx, id, req)
	}
	f.log = append(f.log, "master:"+req.File)
	f.master = append(f.master, req)
	if f.merr != nil {
		return nil, f.merr
	}
	m := map[string]any{"lufs": -14.0, "true_peak_db": -1.0}
	if req.InPlace {
		return &yue.DspVariant{File: req.File, Metrics: m}, nil
	}
	name := fmt.Sprintf("dsp-fx-mix-%08x.flac", len(f.master))
	data, err := os.ReadFile(f.files[key(id, "audio.flac")])
	if err != nil {
		return nil, err
	}
	p := filepath.Join(f.dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return nil, err
	}
	f.files[key(id, name)] = p
	return &yue.DspVariant{File: name, Metrics: m}, nil
}

func msPresetSetup(t *testing.T) *msPresetFake {
	t.Helper()
	return &msPresetFake{psFake: psSetup(t, nil)}
}

// msApply — ApplySoundPreset с фейком, который перехватывает мастер.
func msApply(t *testing.T, f *msPresetFake, p yue.SoundPreset) *PresetResult {
	t.Helper()
	res, err := ApplySoundPreset(context.Background(), f, psJobID, p)
	if err != nil {
		t.Fatalf("ApplySoundPreset: %v", err)
	}
	if res == nil {
		t.Fatal("ApplySoundPreset: nil результат без ошибки")
	}
	return res
}

func msGlue() map[string]any {
	return map[string]any{"type": "glue", "threshold_db": -18.0, "ratio": 2.0}
}

func msNum(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

// msCheckLimiter — блок limiter с target_lufs = want.
func msCheckLimiter(t *testing.T, b map[string]any, want float64) {
	t.Helper()
	if b["type"] != "limiter" {
		t.Errorf("блок %v, want limiter", b)
		return
	}
	if v, ok := msNum(b["target_lufs"]); !ok || v != want {
		t.Errorf("limiter.target_lufs = %v, want %v", b["target_lufs"], want)
	}
}

// msOneMaster — ровно один вызов мастера.
func msOneMaster(t *testing.T, f *msPresetFake) yue.FxRequest {
	t.Helper()
	if len(f.master) != 1 {
		t.Fatalf("мастер на воркере вызван %d раз, want 1; лог %v", len(f.master), f.log)
	}
	return f.master[0]
}

// ТК73: target −13, master [] → ApplyFx с цепочкой [{limiter, target_lufs −13}] на файле после
// final, in_place, source mix; версия — из того же файла; потолок limiter (если задан) −1.
func TestApplySoundPresetTargetIsWorkerLimiter(t *testing.T) {
	f := msPresetSetup(t)
	p := psPreset(61, []yue.PresetSpec{psVocalCut()}, psWidth())
	p.TargetLUFS = psTarget(-13)
	res := msApply(t, f, p)

	final := fmt.Sprintf("dsp-preset-%d.flac", p.ID)
	r := msOneMaster(t, f)
	if r.Source != "mix" || r.File != final || !r.InPlace || r.Preview {
		t.Errorf("мастер source=%q file=%q in_place=%v preview=%v, want mix/%s/true/false",
			r.Source, r.File, r.InPlace, r.Preview, final)
	}
	if len(r.Chain) != 1 {
		t.Fatalf("цепочка мастера %v, want [{limiter, target_lufs −13}]", r.Chain)
	}
	msCheckLimiter(t, r.Chain[0], -13)
	if c, ok := r.Chain[0]["ceiling_db"]; ok {
		if v, _ := msNum(c); v != -1 {
			t.Errorf("limiter.ceiling_db = %v, want −1 (потолок цели −1 dBTP)", c)
		}
	}
	up, ms, tr := psIndex(f.log, "upload:"+final), psIndex(f.log, "master:"+final), psIndex(f.log, "track:"+final)
	if up < 0 || ms < up || tr < ms {
		t.Errorf("порядок %v, want upload → master → track по %s", f.log, final)
	}
	if len(f.promoted) != 1 || f.promoted[0].file != final || res.File != final {
		t.Errorf("версия из %q (promoted %+v), want из %s", res.File, f.promoted, final)
	}
}

// ТК73: шага level на ПК для цели больше нет — вариант финала с целью звучит так же, как без
// цели (±0,1 дБ); замер громкости микса на ПК не нужен (его отсутствие — не ошибка).
func TestApplySoundPresetTargetNoLevelStep(t *testing.T) {
	base := msPresetSetup(t)
	pb := psPreset(62, []yue.PresetSpec{psVocalCut()}, psWidth())
	msApply(t, base, pb)
	ref := psFinalAmp(t, base.psFake, pb.ID)

	f := msPresetSetup(t)
	f.uploadMetrics = map[string]any{"lufs": -25.0} // по прежнему условию 11 дало бы level +12 дБ
	p := psPreset(62, []yue.PresetSpec{psVocalCut()}, psWidth())
	p.TargetLUFS = psTarget(-13)
	msApply(t, f, p)
	if got := psDb(psFinalAmp(t, f.psFake, p.ID), ref); !near(got, 0, 0.1) {
		t.Errorf("вариант финала с целью %.2f дБ к варианту без цели, want 0 (level на ПК не строится)", got)
	}

	g := msPresetSetup(t) // замеров нет вовсе
	q := psPreset(63, []yue.PresetSpec{psVocalCut()}, nil)
	q.TargetLUFS = psTarget(-13)
	msApply(t, g, q)
	if r := msOneMaster(t, g); r.File != fmt.Sprintf("dsp-preset-%d-mix.flac", q.ID) || !r.InPlace {
		t.Errorf("мастер на %q in_place=%v, want файл пересборки in_place (финала нет)", r.File, r.InPlace)
	}
	if _, ok := g.uploads[fmt.Sprintf("dsp-preset-%d.flac", q.ID)]; ok {
		t.Errorf("загружен вариант финала при пустом final — значит, цель всё ещё строит шаг на ПК")
	}
}

// ТК73: master [glue] + target −13 → [glue, limiter target −13].
func TestApplySoundPresetMasterPlusTarget(t *testing.T) {
	f := msPresetSetup(t)
	p := psPreset(64, []yue.PresetSpec{psVocalCut()}, psWidth())
	p.Master = []map[string]any{msGlue()}
	p.TargetLUFS = psTarget(-13)
	msApply(t, f, p)
	r := msOneMaster(t, f)
	if len(r.Chain) != 2 || !reflect.DeepEqual(r.Chain[0], msGlue()) {
		t.Fatalf("цепочка %v, want [glue, limiter]", r.Chain)
	}
	msCheckLimiter(t, r.Chain[1], -13)
}

// ТК73: master с limiter без target + target −12 → у limiter target −12, прочие поля целы,
// второй limiter не дописан.
func TestApplySoundPresetMasterLimiterGetsTarget(t *testing.T) {
	f := msPresetSetup(t)
	p := psPreset(65, []yue.PresetSpec{psVocalCut()}, psWidth())
	p.Master = []map[string]any{msGlue(), {"type": "limiter", "ceiling_db": -2.0, "release_ms": 80.0}}
	p.TargetLUFS = psTarget(-12)
	msApply(t, f, p)
	r := msOneMaster(t, f)
	if len(r.Chain) != 2 {
		t.Fatalf("цепочка %v, want [glue, limiter] (без второго limiter)", r.Chain)
	}
	lim := r.Chain[1]
	msCheckLimiter(t, lim, -12)
	if v, _ := msNum(lim["ceiling_db"]); v != -2 {
		t.Errorf("limiter.ceiling_db = %v, want −2 (как в пресете)", lim["ceiling_db"])
	}
	if v, _ := msNum(lim["release_ms"]); v != 80 {
		t.Errorf("limiter.release_ms = %v, want 80 (как в пресете)", lim["release_ms"])
	}
}

// Усл. 45: master без target — цепочка как есть, на варианте финала in_place.
func TestApplySoundPresetMasterWithoutTarget(t *testing.T) {
	f := msPresetSetup(t)
	p := psPreset(66, []yue.PresetSpec{psVocalCut()}, psWidth())
	p.Master = []map[string]any{msGlue()}
	msApply(t, f, p)
	r := msOneMaster(t, f)
	if !reflect.DeepEqual(r.Chain, []map[string]any{msGlue()}) {
		t.Errorf("цепочка %v, want [glue] как в пресете", r.Chain)
	}
	if r.File != fmt.Sprintf("dsp-preset-%d.flac", p.ID) || !r.InPlace {
		t.Errorf("мастер на %q in_place=%v, want вариант финала in_place", r.File, r.InPlace)
	}
}

// ТК73: без target и master пуст → ApplyFx не зовётся.
func TestApplySoundPresetNoMasterNoTargetNoFx(t *testing.T) {
	f := msPresetSetup(t)
	p := psPreset(67, []yue.PresetSpec{psVocalCut()}, psWidth())
	res := msApply(t, f, p)
	if len(f.master) != 0 || len(f.calls) != 0 {
		t.Errorf("ApplyFx вызван (мастер %d, превью %d), want ни разу", len(f.master), len(f.calls))
	}
	if final := fmt.Sprintf("dsp-preset-%d.flac", p.ID); res.File != final {
		t.Errorf("версия из %q, want %s", res.File, final)
	}
}

// Усл. 45: файла-варианта нет (ни specs, ни final) → обычный рендер /fx звука трека (без file
// и in_place), версия — из ответа.
func TestApplySoundPresetMasterOnOriginalRendersVariant(t *testing.T) {
	f := msPresetSetup(t)
	p := psPreset(68, nil, nil)
	p.TargetLUFS = psTarget(-14)
	res := msApply(t, f, p)
	r := msOneMaster(t, f)
	if r.Source != "mix" || r.File != "" || r.InPlace || r.Preview {
		t.Errorf("мастер source=%q file=%q in_place=%v preview=%v, want mix/«»/false/false",
			r.Source, r.File, r.InPlace, r.Preview)
	}
	if len(r.Chain) != 1 {
		t.Fatalf("цепочка %v, want [limiter]", r.Chain)
	}
	msCheckLimiter(t, r.Chain[0], -14)
	want := "dsp-fx-mix-00000001.flac"
	if res.File != want || len(f.promoted) != 1 || f.promoted[0].file != want {
		t.Errorf("версия из %q (promoted %+v), want из %s", res.File, f.promoted, want)
	}
}

// ТК73: ошибка ApplyFx мастера → ошибка, VariantToTrack не вызван.
func TestApplySoundPresetMasterErrorNoTrack(t *testing.T) {
	f := msPresetSetup(t)
	f.merr = errors.New("limiter: нет ffmpeg")
	p := psPreset(69, []yue.PresetSpec{psVocalCut()}, psWidth())
	p.TargetLUFS = psTarget(-13)
	if _, err := ApplySoundPreset(context.Background(), f, psJobID, p); err == nil {
		t.Fatal("ошибка мастера: want ошибку применения")
	}
	if len(f.promoted) != 0 {
		t.Errorf("VariantToTrack вызван: %+v", f.promoted)
	}
}

// ТК79в / усл. 45б: пресет с записью «место» части барабанов (hh, pan 0,3) применяется без
// ошибки и без движка (место — не обработка); пересборка с Place: хэт (6000 Гц) в миксе
// правее — справа громче, чем слева (M·(h, h) при pan 0,3: L ≈ 0,74·h, R ≈ 1,2·h).
func TestApplySoundPresetDrumPartPlace(t *testing.T) {
	const hh = "0.2*sin(2*PI*6000*t)"
	f := &msPresetFake{psFake: psSetup(t, nil)}
	// стерео-звук (L = R, как в place_test.go): на моно-треке панорама не видна; хэт звучит и
	// в самом треке (часть барабанов — внутри микса)
	dir := t.TempDir()
	for name, expr := range map[string]string{
		"audio.flac": exprPs3k + "+" + exprPs500 + "+" + hh, "stem-vocals.flac": exprPs3k,
		"stem-other.flac": exprPs500, "stem-drums.flac": hh, "stem-bass.flac": "0", "stem-hh.flac": hh,
	} {
		f.files[key(psJobID, name)] = plGen(t, expr, psDur, filepath.Join(dir, name))
	}
	p := psPreset(70, []yue.PresetSpec{{Stems: []string{"hh"}, Place: &yue.Place{Pan: 0.3, Width: 1}}}, nil)
	res := msApply(t, f, p)
	if len(f.calls) != 0 || len(f.master) != 0 {
		t.Errorf("ApplyFx вызван (превью %d, мастер %d), want ни разу — место без движка", len(f.calls), len(f.master))
	}
	mix := fmt.Sprintf("dsp-preset-%d-mix.flac", p.ID)
	data := f.uploads[mix]
	if len(data) == 0 {
		t.Fatalf("пересборка %s не загружена: %v", mix, keys(f.uploads))
	}
	l, r := plDecode2(t, data)
	al, ar := toneAmp(l, 6000, 2, 8), toneAmp(r, 6000, 2, 8)
	if ar <= 0 || db(ar/al) < 1 {
		t.Errorf("хэт слева %.4f, справа %.4f — want справа громче ≥ 1 дБ (место pan 0,3 не применено)", al, ar)
	}
	if res.File != mix || len(f.promoted) != 1 {
		t.Errorf("версия из %q (promoted %+v), want из %s", res.File, f.promoted, mix)
	}
}
