package studio

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты карточки internal-own-track, этап 10, условия 86 и 86а (тест-кейсы ТК126, ТК126а): сверка по итогу.
// Написаны по карточке, без чтения реализации.
//
// Контракт: пресет с целями level_db после прохода (пересборка → … → версия-трек V) разделяет V
// (MakeStemsWith(V, "roformer")), меряет уровни целевых дорожек к треку так же, как presetLevels
// (rms_p95_db дорожки − сумма мощностей основных vocals/drums/bass/other из JobStems(V)); если хоть одна
// дальше 1 дБ от цели — исправляющий проход: у каждой целевой записи поправка −ошибка (за проход не больше
// ±6 дБ), поправки накапливаются (сумма не больше ±9 дБ на запись). Не больше двух исправляющих проходов
// (всего ≤ 3 пересборки). Каждая промежуточная версия удаляется (DeleteJob), ответ — последняя. Все в
// пределах 1 дБ — дальше не исправляется. Сбой разделения на любом шаге — ответ последняя готовая версия,
// без ошибки.
//
// Как видно: пресет — гитара (движок, level_db −10) и бас (без движка, level_db −10). Гейн вставки гитары —
// амплитуда тона обработанного куска (1000 Гц, prlFake) в пересборке к амплитуде куска; бас — тон 200 Гц
// пересборки к исходному треку. Пересборки — загрузки с «mix» в имени, по порядку; число проходов —
// число вызовов движка на гитару. Версии получают id 900, 901, 902 по порядку; у каждой своя ошибка гитары.
//
// Толкования: пресет без финала и мастера (условие говорит «после мастера» о месте сверки, не о его
// наличии); замеры версии фейк отдаёт только после её разделения; ошибка у баса — ровно 0; разделение
// последней версии после второго исправления не проверяется (условие его не требует).

const (
	vfTarget = -10.0
	vfFirst  = psChild // id первой версии (V1)
)

// vfUpload — загруженный файл (имя, байты) в порядке загрузки.
type vfUpload struct {
	name string
	data []byte
}

// vfFake — prlFake (замеры трека, кусок движка 1000 Гц) + разделение и замеры версий, версии с растущими id.
type vfFake struct {
	*prlFake
	guitarErr map[int64]float64 // версия → гитара к треку = цель + ошибка (нет в карте — у цели)
	splitFail map[int64]bool    // у каких версий MakeStemsWith отвечает ошибкой
	noMetric  map[string]bool   // у каких дорожек версий нет замера rms_p95_db (дорожка в списке есть)
	split     map[int64]bool
	splitAsks []int64 // у каких версий просили roformer
	ups       []vfUpload
	nextID    int64
}

func (f *vfFake) MakeStemsWith(ctx context.Context, id int64, model string) (map[string]any, error) {
	if id == psJobID {
		return f.prlFake.MakeStemsWith(ctx, id, model)
	}
	if model == "roformer" {
		f.splitAsks = append(f.splitAsks, id)
	}
	if f.splitFail[id] {
		return nil, errors.New("roformer: нет памяти")
	}
	f.split[id] = true
	return map[string]any{"ok": true}, nil
}

// JobStems версии — после разделения: основные vocals/drums/other −20, бас ровно у цели, гитара — цель + guitarErr.
func (f *vfFake) JobStems(ctx context.Context, id int64) ([]map[string]any, error) {
	if id == psJobID {
		return f.prlFake.JobStems(ctx, id)
	}
	if !f.split[id] {
		return nil, nil
	}
	p95 := vfVersionP95(f.guitarErr[id])
	var out []map[string]any
	for name, v := range p95 {
		file := "stem-" + name + ".flac"
		if f.noMetric[name] {
			out = append(out, map[string]any{"file": file, "name": name})
			continue
		}
		out = append(out, map[string]any{"file": file, "name": name, "metrics": map[string]any{
			"file": file, "metrics": map[string]any{"rms_p95_db": v, "rms_median_db": v - 6, "peak": 0.5}}})
	}
	return out, nil
}

func (f *vfFake) VariantToTrack(_ context.Context, jobID int64, file, title string, _ int64) (int64, error) {
	f.log = append(f.log, "track:"+file)
	f.promoted = append(f.promoted, psPromote{jobID, file, title})
	id, parent := f.nextID, jobID
	f.nextID++
	f.jobs = append(f.jobs, yue.Job{ID: id, ParentID: &parent, Title: title, Status: "done", AudioFile: file,
		DurationSec: lvDur})
	return id, nil
}

func (f *vfFake) UploadDsp(ctx context.Context, id int64, fname, label string, data []byte) (*yue.DspVariant, error) {
	f.ups = append(f.ups, vfUpload{fname, append([]byte(nil), data...)})
	return f.prlFake.UploadDsp(ctx, id, fname, label, data)
}

// vfVersionP95 — замеры дорожек V1: сумма основных S; бас − S = цель; гитара − S = цель + guitarErr.
func vfVersionP95(guitarErr float64) map[string]float64 {
	others := 3 * math.Pow(10, -2) // vocals, drums, other по −20
	r := math.Pow(10, vfTarget/10)
	bass := r * others / (1 - r)
	sum := 10 * math.Log10(others+bass)
	return map[string]float64{"vocals": -20, "drums": -20, "other": -20,
		"bass": 10 * math.Log10(bass), "guitar": sum + vfTarget + guitarErr}
}

func vfSetup(t *testing.T, guitarErr map[int64]float64) (*vfFake, []float32) {
	t.Helper()
	pf, orig := prlSetup(t)
	pf.files[key(psJobID, "stem-guitar.flac")] = lavfi(t, aeval("0.05*sin(2*PI*700*t)", lvDur),
		filepath.Join(t.TempDir(), "vf-guitar.flac"))
	pf.p95["guitar"] = prlSineDb(0.05)
	return &vfFake{prlFake: pf, guitarErr: guitarErr, splitFail: map[int64]bool{},
		noMetric: map[string]bool{}, split: map[int64]bool{}, nextID: vfFirst}, orig
}

func vfPreset(id int64) yue.SoundPreset { return vfPresetDb(id, 0) }

// vfPresetDb — пресет, у записи гитары громкость «на этот раз» db (поле Db записи).
func vfPresetDb(id int64, db float64) yue.SoundPreset {
	return lvPreset(id,
		yue.PresetSpec{Stems: []string{"guitar"}, Engine: engChain(), LevelDb: lvF(vfTarget), Db: db},
		yue.PresetSpec{Stems: []string{"bass"}, LevelDb: lvF(vfTarget)},
	)
}

// vfPass — гейны прохода: гитара (вставка куска) и бас (к исходному треку), дБ.
type vfPass struct{ guitar, bass float64 }

func (f *vfFake) passes(t *testing.T, orig []float32) []vfPass {
	t.Helper()
	var out []vfPass
	for _, u := range f.ups {
		if !strings.Contains(u.name, "mix") {
			continue
		}
		s := decodeBytes(t, u.data)
		out = append(out, vfPass{
			guitar: 20 * math.Log10(toneAmp(s, prlChunkHz, 2, 8)/prlChunkAmp),
			bass:   20 * math.Log10(toneAmp(s, 200, 2, 8)/toneAmp(orig, 200, 2, 8)),
		})
	}
	return out
}

func (f *vfFake) guitarRuns() int {
	n := 0
	for _, c := range f.calls {
		if c.req.Source == "guitar" {
			n++
		}
	}
	return n
}

func vfAsked(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func vfApply(t *testing.T, f *vfFake, p yue.SoundPreset) *PresetResult {
	t.Helper()
	res, err := ApplySoundPreset(context.Background(), f, psJobID, p)
	if err != nil {
		t.Fatalf("ApplySoundPreset: %v", err)
	}
	if res == nil {
		t.Fatal("ApplySoundPreset: nil без ошибки")
	}
	return res
}

// vfCheck — проходы: гитара в проходе k (k ≥ 2) ниже первого на shifts[k−2] ±0,1, бас как в первом;
// удалены ровно версии deleted (трек и ответ — нет), ответ — child.
func vfCheck(t *testing.T, f *vfFake, orig []float32, res *PresetResult, shifts []float64, deleted []int64, child int64) {
	t.Helper()
	want := len(shifts) + 1
	if n := f.guitarRuns(); n != want {
		t.Fatalf("проходов (движок на гитару) %d, want %d", n, want)
	}
	ps := f.passes(t, orig)
	if len(ps) != want {
		t.Fatalf("пересборок (загрузок mix) %d, want %d: %v", len(ps), want, f.log)
	}
	for k, sh := range shifts {
		if d := ps[k+1].guitar - ps[0].guitar; math.Abs(d-sh) > 0.1 {
			t.Errorf("гитара в проходе %d: %+.2f дБ к первому (%.2f → %.2f), want %+.2f ±0,1",
				k+2, d, ps[0].guitar, ps[k+1].guitar, sh)
		}
		if d := ps[k+1].bass - ps[0].bass; math.Abs(d) > 0.1 {
			t.Errorf("бас в проходе %d: %+.2f дБ к первому, want 0 ±0,1 (бас у цели)", k+2, d)
		}
	}
	var wantDel []string
	for _, id := range deleted {
		wantDel = append(wantDel, fmt.Sprintf("job %d", id))
	}
	var gotDel []string
	for _, d := range f.deleted {
		if strings.HasPrefix(d, "job ") {
			gotDel = append(gotDel, d)
		}
	}
	if fmt.Sprint(vfSorted(gotDel)) != fmt.Sprint(vfSorted(wantDel)) {
		t.Errorf("удалены версии %v, want %v (промежуточные; трек и ответ остаются)", gotDel, wantDel)
	}
	if res.ChildID != child {
		t.Errorf("ChildID = %d, want %d (последняя версия)", res.ChildID, child)
	}
}

func vfSorted(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func vfRun(t *testing.T, id int64, errs map[int64]float64, fail ...int64) (*vfFake, []float32, *PresetResult) {
	t.Helper()
	f, orig := vfSetup(t, errs)
	for _, v := range fail {
		f.splitFail[v] = true
	}
	res := vfApply(t, f, vfPreset(id))
	if !vfAsked(f.splitAsks, vfFirst) {
		t.Errorf("первая версия не разделена RoFormer для сверки: просили %v", f.splitAsks)
	}
	return f, orig, res
}

// ТК126: гитара +3,6, после исправления у цели → 2 прохода, гитара −3,6, удалена V1, ответ V2.
func TestPresetVerifySecondPass(t *testing.T) {
	f, orig, res := vfRun(t, 51, map[int64]float64{vfFirst: 3.6})
	if !vfAsked(f.splitAsks, vfFirst+1) {
		t.Errorf("вторая версия не сверена (разделение не запрошено): %v", f.splitAsks)
	}
	vfCheck(t, f, orig, res, []float64{-3.6}, []int64{vfFirst}, vfFirst+1)
}

// ТК126/86: ошибка 9 → поправка прохода зажата 6; после неё у цели → 2 прохода.
func TestPresetVerifyClamp(t *testing.T) {
	f, orig, res := vfRun(t, 52, map[int64]float64{vfFirst: 9})
	vfCheck(t, f, orig, res, []float64{-6}, []int64{vfFirst}, vfFirst+1)
}

// ТК126а: +3,6 → +1,6 → третий проход: гитара = первый − 5,2; удалены V1 и V2; ответ V3.
func TestPresetVerifyThirdPass(t *testing.T) {
	f, orig, res := vfRun(t, 55, map[int64]float64{vfFirst: 3.6, vfFirst + 1: 1.6})
	vfCheck(t, f, orig, res, []float64{-3.6, -5.2}, []int64{vfFirst, vfFirst + 1}, vfFirst+2)
}

// ТК126а: +9 и +9 (и +9 у третьей) → 2-й проход −6, 3-й — сумма зажата −9; четвёртого прохода нет.
func TestPresetVerifySumClamp(t *testing.T) {
	f, orig, res := vfRun(t, 56, map[int64]float64{vfFirst: 9, vfFirst + 1: 9, vfFirst + 2: 9})
	vfCheck(t, f, orig, res, []float64{-6, -9}, []int64{vfFirst, vfFirst + 1}, vfFirst+2)
}

// ТК126а: промах остаётся после двух исправлений → всё равно ≤ 3 пересборок.
func TestPresetVerifyNoFourthPass(t *testing.T) {
	f, _, _ := vfRun(t, 57, map[int64]float64{vfFirst: 3, vfFirst + 1: 3, vfFirst + 2: 3, vfFirst + 3: 3})
	if n := f.guitarRuns(); n > 3 {
		t.Errorf("проходов %d, want ≤ 3 (не больше двух исправлений)", n)
	}
	if n := len(f.promoted); n > 3 {
		t.Errorf("версий-треков %d, want ≤ 3", n)
	}
}

// ТК126: все у цели (гитара +0,5 ≤ 1 дБ) → одна пересборка, ничего не удалено, ответ — первая версия.
func TestPresetVerifyAllOnTarget(t *testing.T) {
	f, orig, res := vfRun(t, 53, map[int64]float64{vfFirst: 0.5})
	vfCheck(t, f, orig, res, nil, nil, vfFirst)
}

// ТК126: сбой разделения первой версии → ответ первая версия, без ошибки, без второй пересборки.
func TestPresetVerifySplitError(t *testing.T) {
	f, orig, res := vfRun(t, 54, map[int64]float64{vfFirst: 3.6}, vfFirst)
	vfCheck(t, f, orig, res, nil, nil, vfFirst)
}

// ТК126а: сбой разделения второй версии → ответ вторая версия без ошибки, удалена только первая.
func TestPresetVerifySplitErrorSecond(t *testing.T) {
	f, orig, res := vfRun(t, 58, map[int64]float64{vfFirst: 3.6, vfFirst + 1: 1.6}, vfFirst+1)
	if !vfAsked(f.splitAsks, vfFirst+1) {
		t.Errorf("разделение второй версии не запрошено: %v", f.splitAsks)
	}
	vfCheck(t, f, orig, res, []float64{-3.6}, []int64{vfFirst}, vfFirst+1)
}

// ТК127 / 86б: level_db −10 и db +3 — цель сверки −7. Гитара версии на −7 → у цели, второго прохода нет.
func TestPresetVerifyRespectsDbOnTarget(t *testing.T) {
	f, orig := vfSetup(t, map[int64]float64{vfFirst: 3}) // гитара −10 + 3 = −7
	res := vfApply(t, f, vfPresetDb(61, 3))
	vfCheck(t, f, orig, res, nil, nil, vfFirst)
}

// ТК127 / 86б: level_db −10 и db +3, гитара версии на −10 → промах −3 к цели −7 → поправка +3 (не 0).
func TestPresetVerifyRespectsDbCorrects(t *testing.T) {
	f, orig := vfSetup(t, map[int64]float64{vfFirst: 0, vfFirst + 1: 3}) // −10, после исправления −7
	res := vfApply(t, f, vfPresetDb(62, 3))
	vfCheck(t, f, orig, res, []float64{3}, []int64{vfFirst}, vfFirst+1)
}

// ТК127 / 86в: нет замера у основной vocals (или у целевой гитары) при промахе гитары +3,6 →
// без поправок: одна пересборка, ничего не удалено, ответ — первая версия.
func TestPresetVerifyIncompleteMetrics(t *testing.T) {
	for _, stem := range []string{"vocals", "guitar"} {
		t.Run("нет замера "+stem, func(t *testing.T) {
			f, orig := vfSetup(t, map[int64]float64{vfFirst: 3.6, vfFirst + 1: 0})
			f.noMetric[stem] = true
			res := vfApply(t, f, vfPreset(63))
			vfCheck(t, f, orig, res, nil, nil, vfFirst)
		})
	}
}
