package studio

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-own-track, этап 6, условия 44 и 44а (ТК72): мастер в пересборке.
// SectionSpec.Master (с Engine): после сборки и загрузки микса overdub-inst-N.flac —
// ApplyFx(родитель, source mix, file = этот микс, in_place, chain = цепочка мастера) на
// воркере; результат — тот же файл микса; одна активная запись мастера (две — ошибка);
// реестр только из мастера — микс = оригинал + мастер; окно мастера игнорируется;
// RebuildResult.Variant — ответ ApplyFx (метрики lufs, true_peak_db).
// 44а: после ApplyFx — ещё UploadDsp(overdub-premaster-N.flac) с тем же звуком, что ушёл в
// первую загрузку («микс без мастера» для «▶ стало / ▶ было»).
//
// Воркер — msFake: secFake (sections_test.go) + ApplyFx, который пишет порядок вызовов.
// Загрузки фейк хранит по имени (secFake.uploads): «на месте» воркер файл не меняет.

type msFake struct {
	*secFake
	log   []string // upload:<file>, fx:<file>
	calls []yue.FxRequest
	ids   []int64
	first map[string][]byte // байты первой загрузки каждого имени
	err   error             // ApplyFx отвечает ошибкой
}

func (f *msFake) WorkerConfig(context.Context) (map[string]any, error) {
	return map[string]any{"fx_preview": true}, nil
}

func (f *msFake) UploadDsp(ctx context.Context, id int64, fname, label string, data []byte) (*yue.DspVariant, error) {
	f.log = append(f.log, "upload:"+fname)
	if f.first == nil {
		f.first = map[string][]byte{}
	}
	if _, ok := f.first[fname]; !ok {
		f.first[fname] = append([]byte(nil), data...)
	}
	return f.secFake.UploadDsp(ctx, id, fname, label, data)
}

// ApplyFx — мастер «на месте»: тот же файл, метрики громкости пересчитаны воркером.
func (f *msFake) ApplyFx(_ context.Context, id int64, req yue.FxRequest) (*yue.DspVariant, error) {
	f.log = append(f.log, "fx:"+req.File)
	f.calls = append(f.calls, req)
	f.ids = append(f.ids, id)
	if f.err != nil {
		return nil, f.err
	}
	return &yue.DspVariant{File: req.File, Label: "микс · мастер",
		Metrics: map[string]any{"lufs": -14.0, "true_peak_db": -1.0}}, nil
}

func msChain() []map[string]any {
	return []map[string]any{
		{"type": "glue", "threshold_db": -18.0, "ratio": 2.0},
		{"type": "limiter", "ceiling_db": -1.0, "target_lufs": -14.0},
	}
}

// msMaster — запись мастера; окно задано нарочно — оно должно игнорироваться.
func msMaster() SectionSpec {
	return SectionSpec{ChildID: 0, Master: true, Engine: msChain(), From: 3, To: 5}
}

func msMute() SectionSpec {
	return SectionSpec{ChildID: 0, From: 2, To: 6, Stems: []string{"other"}, Db: -100}
}

func msSetup(t *testing.T) (*msFake, map[string]string) {
	t.Helper()
	sf, pf := secSetup(t)
	return &msFake{secFake: sf}, pf
}

// msWantLog — порядок 44/44а: микс → мастер на нём → микс без мастера.
func msWantLog(n string) []string {
	return []string{"upload:overdub-inst-" + n + ".flac", "fx:overdub-inst-" + n + ".flac",
		"upload:overdub-premaster-" + n + ".flac"}
}

// msCheckPremaster — «микс без мастера» — тот же звук, что ушёл в первую загрузку микса.
func msCheckPremaster(t *testing.T, f *msFake, n string) {
	t.Helper()
	mix, pre := f.first["overdub-inst-"+n+".flac"], f.first["overdub-premaster-"+n+".flac"]
	if len(pre) == 0 {
		t.Fatalf("overdub-premaster-%s.flac не загружен; лог %v", n, f.log)
	}
	if bytes.Equal(mix, pre) {
		return
	}
	a, b := decodeBytes(t, mix), decodeBytes(t, pre)
	if len(a) != len(b) || diffRMS(a, b, 0, float64(len(a))/sr) > 1e-4 {
		t.Errorf("overdub-premaster-%s.flac — не тот звук, что ушёл в первую загрузку микса", n)
	}
}

// msCheckMasterCall — ровно один ApplyFx: у родителя, source mix, file, in_place, цепочка мастера,
// не превью, без окна (весь трек).
func msCheckMasterCall(t *testing.T, f *msFake, file string) {
	t.Helper()
	if len(f.calls) != 1 {
		t.Fatalf("ApplyFx вызван %d раз, want 1 (мастер); лог %v", len(f.calls), f.log)
	}
	r := f.calls[0]
	if f.ids[0] != parentID {
		t.Errorf("ApplyFx у джобы %d, want %d", f.ids[0], parentID)
	}
	if r.Source != "mix" || r.File != file || !r.InPlace || r.Preview {
		t.Errorf("FxRequest source=%q file=%q in_place=%v preview=%v, want mix/%s/true/false",
			r.Source, r.File, r.InPlace, r.Preview, file)
	}
	if !reflect.DeepEqual(r.Chain, msChain()) {
		t.Errorf("FxRequest.Chain = %v, want цепочка мастера %v", r.Chain, msChain())
	}
	if (r.From != nil && *r.From != 0) || r.To != nil {
		t.Errorf("окно мастера from=%v to=%v, want весь трек (окно записи игнорируется)", deref(r.From), deref(r.To))
	}
}

// ТК72: громкость other + мастер — порядок UploadDsp(overdub-inst-0) → ApplyFx(тот же файл,
// in_place, chain мастера, source mix) → UploadDsp(overdub-premaster-0); результат — файл микса
// с метриками ответа ApplyFx.
func TestRebuildSectionsMasterOrderAndResult(t *testing.T) {
	f, _ := msSetup(t)
	res, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{msMute(), msMaster()})
	if err != nil {
		t.Fatalf("RebuildSections: %v", err)
	}
	const mix = "overdub-inst-0.flac"
	if want := msWantLog("0"); !reflect.DeepEqual(f.log, want) {
		t.Errorf("порядок вызовов %v, want %v", f.log, want)
	}
	msCheckMasterCall(t, f, mix)
	msCheckPremaster(t, f, "0")
	if res == nil || res.Variant == nil {
		t.Fatalf("результат %+v, want вариант", res)
	}
	if res.Variant.File != mix {
		t.Errorf("Variant.File = %q, want %s (тот же файл микса)", res.Variant.File, mix)
	}
	if lufs, _ := res.Variant.Metrics["lufs"].(float64); lufs != -14 {
		t.Errorf("Metrics.lufs = %v, want −14 (ответ ApplyFx)", res.Variant.Metrics["lufs"])
	}
	if tp, _ := res.Variant.Metrics["true_peak_db"].(float64); tp != -1 {
		t.Errorf("Metrics.true_peak_db = %v, want −1 (ответ ApplyFx)", res.Variant.Metrics["true_peak_db"])
	}
	// микс собран ffmpeg без мастера, но с остальными правками: other (440) в окне заглушён
	out := decodeBytes(t, f.first[mix])
	if a := toneAmp(out, 440, 3, 5); a > 0.01 {
		t.Errorf("в окне заглушения 440 Гц = %.4f, want тишина (правка other потерялась)", a)
	}
	if a := toneAmp(out, 440, 8, 10); a < 0.25 {
		t.Errorf("вне окна 440 Гц = %.4f, want ≈ 0.3", a)
	}
}

// 44а: без мастера «микса без мастера» нет — одна загрузка, ApplyFx не зовётся.
func TestRebuildSectionsNoMasterNoPremaster(t *testing.T) {
	f, _ := msSetup(t)
	if _, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{msMute()}); err != nil {
		t.Fatalf("RebuildSections: %v", err)
	}
	if want := []string{"upload:overdub-inst-0.flac"}; !reflect.DeepEqual(f.log, want) {
		t.Errorf("порядок вызовов %v, want %v", f.log, want)
	}
}

// ТК72 / усл. 44: имя микса — overdub-inst-N.flac по последней вклейке; мастер — на нём же.
func TestRebuildSectionsMasterOnChildMix(t *testing.T) {
	f, _ := msSetup(t)
	if _, err := RebuildSections(context.Background(), f, parentID,
		[]SectionSpec{spec7([]string{"other"}, 0), msMaster()}); err != nil {
		t.Fatalf("RebuildSections: %v", err)
	}
	if want := msWantLog("7"); !reflect.DeepEqual(f.log, want) {
		t.Errorf("порядок вызовов %v, want %v", f.log, want)
	}
	msCheckMasterCall(t, f, "overdub-inst-7.flac")
	msCheckPremaster(t, f, "7")
}

// ТК72: реестр только из мастера — загружается оригинал, мастер на нём.
func TestRebuildSectionsMasterOnly(t *testing.T) {
	f, pf := msSetup(t)
	if _, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{msMaster()}); err != nil {
		t.Fatalf("RebuildSections: %v", err)
	}
	const mix = "overdub-inst-0.flac"
	if want := msWantLog("0"); !reflect.DeepEqual(f.log, want) {
		t.Errorf("порядок вызовов %v, want %v", f.log, want)
	}
	if len(f.first[mix]) == 0 {
		t.Fatalf("микс %s не загружен: %v", mix, keys(f.uploads))
	}
	out := decodeBytes(t, f.first[mix])
	base := decodeFile(t, pf["audio.flac"])
	if d := diffRMS(out, base, 0.5, trackDur-0.5); d > 0.001 {
		t.Errorf("загруженный микс отличается от оригинала: RMS разницы %.5f, want ≈ 0", d)
	}
	msCheckMasterCall(t, f, mix)
	msCheckPremaster(t, f, "0")
}

// ТК72: две активные записи мастера → ошибка без UploadDsp и без ApplyFx.
func TestRebuildSectionsTwoMastersIsError(t *testing.T) {
	f, _ := msSetup(t)
	_, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{msMaster(), msMute(), msMaster()})
	if err == nil {
		t.Fatal("две записи мастера: want ошибку")
	}
	if len(f.log) != 0 || len(f.uploads) != 0 {
		t.Errorf("вызовы %v, загрузки %v — want ничего", f.log, keys(f.uploads))
	}
}

// Усл. 44: мастер считается на воркере — его ошибка — ошибка пересборки.
func TestRebuildSectionsMasterFxErrorIsError(t *testing.T) {
	f, _ := msSetup(t)
	f.err = errors.New("limiter: target_lufs вне пределов")
	if _, err := RebuildSections(context.Background(), f, parentID, []SectionSpec{msMute(), msMaster()}); err == nil {
		t.Fatal("ApplyFx мастера с ошибкой: want ошибку пересборки")
	}
}

// Усл. 44: мастер — «с Engine»: запись мастера без цепочки → ошибка, ничего не загружено.
func TestRebuildSectionsMasterWithoutEngineIsError(t *testing.T) {
	f, _ := msSetup(t)
	_, err := RebuildSections(context.Background(), f, parentID,
		[]SectionSpec{msMute(), {ChildID: 0, Master: true}})
	if err == nil {
		t.Fatal("мастер без цепочки: want ошибку")
	}
	if len(f.log) != 0 {
		t.Errorf("вызовы %v, want ничего", f.log)
	}
}
