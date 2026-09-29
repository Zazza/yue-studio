package studio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"yue-studio/internal/yue"
)

// Тесты RebuildInserts написаны по контракту задачи «вклейки инструментов»:
// база — всегда audio.flac родителя, партии — audio.flac детей, опора ритма —
// stem-drums.flac родителя (нет — один вызов MakeStems, не вышло — по плану),
// результат — overdub-inst-<ChildID последней спеки>.flac.

const parentID int64 = 1

// fakeSvc — фейковый воркер: только внешние границы (скачивание, стемы, загрузка).
// Остальные методы yue.Service не реализованы: их вызов — паника, т.е. нарушение контракта.
type fakeSvc struct {
	yue.Service
	files         map[string]string // "id/file" → путь к файлу на диске
	stemsCalls    int
	makeStemsAdds bool   // MakeStems кладёт stem-drums.flac родителю
	stemPath      string // какой файл положить при makeStemsAdds
	uploads       map[string][]byte
	fetched       []string // все попытки FetchAudio, "id/file"
}

func newFake() *fakeSvc {
	return &fakeSvc{files: map[string]string{}, uploads: map[string][]byte{}}
}

func key(id int64, file string) string { return fmt.Sprintf("%d/%s", id, file) }

func (f *fakeSvc) FetchAudio(_ context.Context, id int64, file string) (io.ReadCloser, string, error) {
	k := key(id, file)
	f.fetched = append(f.fetched, k)
	p, ok := f.files[k]
	if !ok {
		return nil, "", errors.New("404 not found: " + k)
	}
	r, err := os.Open(p)
	if err != nil {
		return nil, "", err
	}
	return r, file, nil
}

func (f *fakeSvc) MakeStems(_ context.Context, id int64) (map[string]any, error) {
	f.stemsCalls++
	if f.makeStemsAdds && id == parentID {
		f.files[key(parentID, "stem-drums.flac")] = f.stemPath
		return map[string]any{"ok": true}, nil
	}
	return nil, errors.New("stems failed")
}

func (f *fakeSvc) UploadDsp(_ context.Context, id int64, fname string, data []byte) (*yue.DspVariant, error) {
	if id != parentID {
		return nil, fmt.Errorf("upload to unexpected job %d", id)
	}
	f.uploads[fname] = append([]byte(nil), data...)
	return &yue.DspVariant{File: fname}, nil
}

// --- звук через ffmpeg ---

const sr = 16000

func needFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

// lavfi — сгенерировать файл (16 кГц моно) из источника lavfi.
func lavfi(t *testing.T, src, path string) string {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", src, "-ac", "1", "-ar", "16000", path).CombinedOutput()
	if err != nil {
		t.Fatalf("lavfi %q: %v %s", src, err, out)
	}
	return path
}

// decodeBytes — декодировать загруженные байты в моно float32 16 кГц.
func decodeBytes(t *testing.T, data []byte) []float32 {
	t.Helper()
	p := filepath.Join(t.TempDir(), "uploaded.flac")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", p, "-f", "f32le", "-ac", "1", "-ar", "16000", "-").Output()
	if err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	s := make([]float32, len(raw)/4)
	for i := range s {
		s[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return s
}

// clicks — партия dur с тишины с короткими всплесками (20 мс, 0.8) в моменты at.
func clicks(dur float64, at ...float64) string {
	var parts []string
	for _, a := range at {
		parts = append(parts, fmt.Sprintf("between(t\\,%.3f\\,%.3f)", a, a+0.02))
	}
	cond := strings.Join(parts, "+")
	return fmt.Sprintf("aevalsrc=exprs='if(%s\\,0.8\\,0)':d=%g:s=16000", cond, dur)
}

// quietBase — база dur с тихого белого шума (≈ −60 дБ): окно оригинала не пустое,
// значит выравнивание по RMS даёт конечный положительный гейн.
func quietBase(dur float64) string {
	return fmt.Sprintf("anoisesrc=r=16000:a=0.001:c=white:d=%g", dur)
}

func slice(s []float32, from, to float64) []float32 {
	a, b := int(from*sr), int(to*sr)
	if a < 0 {
		a = 0
	}
	if b > len(s) {
		b = len(s)
	}
	if a >= b {
		return nil
	}
	return s[a:b]
}

func rms(s []float32) float64 {
	if len(s) == 0 {
		return 0
	}
	var e float64
	for _, v := range s {
		e += float64(v) * float64(v)
	}
	return math.Sqrt(e / float64(len(s)))
}

// clickThreshold — порог «щелчок»: в 6 раз выше RMS фона выхода на заведомо
// пустом от партий участке [from,to]. Порог считается по самому выходу, поэтому
// не зависит от того, как реализация нормирует микс; пик шума ≈ 1.7·RMS, а
// щелчок после выравнивания по RMS — в десятки раз выше RMS фона.
func clickThreshold(t *testing.T, out []float32, from, to float64) float64 {
	t.Helper()
	bg := rms(slice(out, from, to))
	if bg == 0 {
		t.Fatalf("фон выхода на [%.1f,%.1f] нулевой — база потеряна", from, to)
	}
	return 6 * bg
}

// firstAbove — момент (с) первого отсчёта |x| > thr в [from,to]; -1 — нет.
func firstAbove(s []float32, thr, from, to float64) float64 {
	seg := slice(s, from, to)
	for i, v := range seg {
		if math.Abs(float64(v)) > thr {
			return from + float64(i)/sr
		}
	}
	return -1
}

// setup — родитель с базой 10 с тихого шума; стема нет и MakeStems его не даёт.
func setup(t *testing.T) (*fakeSvc, string) {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	f := newFake()
	f.files[key(parentID, "audio.flac")] = lavfi(t, quietBase(10), filepath.Join(dir, "base.flac"))
	return f, dir
}

func uploaded(t *testing.T, f *fakeSvc, name string) []float32 {
	t.Helper()
	data, ok := f.uploads[name]
	if !ok {
		var got []string
		for k := range f.uploads {
			got = append(got, k)
		}
		t.Fatalf("не загружен %q; загружены: %v", name, got)
	}
	return decodeBytes(t, data)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// --- тест-кейсы карточки ---

// Кейс 1: база — всегда audio.flac родителя, никогда не прошлый микс overdub-inst-*.
func TestRebuildInsertsBaseIsAlwaysOriginal(t *testing.T) {
	f, dir := setup(t)
	// прошлый микс — громкий синус: если его взять базой, фон выхода станет громким
	f.files[key(parentID, "overdub-inst-5.flac")] = lavfi(t,
		"sine=frequency=440:duration=10:sample_rate=16000", filepath.Join(dir, "old.flac"))
	f.files[key(7, "audio.flac")] = lavfi(t, clicks(4, 1.5), filepath.Join(dir, "p7.flac"))

	_, err := RebuildInserts(context.Background(), f, parentID,
		[]InsertSpec{{ChildID: 7, From: 4, To: 7, Lead: 1, BeatSec: 0.5}})
	if err != nil {
		t.Fatalf("RebuildInserts: %v", err)
	}
	if !contains(f.fetched, key(parentID, "audio.flac")) {
		t.Errorf("база audio.flac не скачивалась: %v", f.fetched)
	}
	for _, k := range f.fetched {
		if strings.Contains(k, "overdub-inst") {
			t.Errorf("скачан прошлый микс %q — база должна быть чистым оригиналом", k)
		}
	}
	out := uploaded(t, f, "overdub-inst-7.flac")
	// фон до вклейки — тихий шум (RMS ≈ 0.0006), а не синус 1/8 (RMS ≈ 0.088)
	if r := rms(slice(out, 0, 2.5)); r > 0.01 {
		t.Errorf("фон выхода RMS=%.4f — похоже, база взята не из audio.flac", r)
	}
}

// Кейс 2: имя результата — по ChildID последней спеки.
func TestRebuildInsertsResultNamedByLastSpec(t *testing.T) {
	f, dir := setup(t)
	f.files[key(7, "audio.flac")] = lavfi(t, clicks(4, 1.5), filepath.Join(dir, "p7.flac"))
	f.files[key(9, "audio.flac")] = lavfi(t, clicks(4, 1.5), filepath.Join(dir, "p9.flac"))

	res, err := RebuildInserts(context.Background(), f, parentID, []InsertSpec{
		{ChildID: 7, From: 2, To: 4, Lead: 1, BeatSec: 0.5},
		{ChildID: 9, From: 6, To: 8, Lead: 1, BeatSec: 0.5},
	})
	if err != nil {
		t.Fatalf("RebuildInserts: %v", err)
	}
	if len(f.uploads) != 1 {
		t.Errorf("ожидалась одна загрузка, было %d", len(f.uploads))
	}
	if _, ok := f.uploads["overdub-inst-9.flac"]; !ok {
		t.Errorf("не загружен overdub-inst-9.flac; загрузки: %v", keys(f.uploads))
	}
	if res == nil || res.Variant == nil || res.Variant.File != "overdub-inst-9.flac" {
		t.Errorf("Variant = %+v, want File=overdub-inst-9.flac", res)
	}
}

func keys(m map[string][]byte) []string {
	var r []string
	for k := range m {
		r = append(r, k)
	}
	return r
}

// Кейс 3: без стема партия встаёт по плану — начало на From−Lead; длина выхода = длине базы.
func TestRebuildInsertsPlanPlacementWithoutStem(t *testing.T) {
	f, dir := setup(t)
	f.files[key(7, "audio.flac")] = lavfi(t, clicks(4, 1.5), filepath.Join(dir, "p7.flac"))

	res, err := RebuildInserts(context.Background(), f, parentID,
		[]InsertSpec{{ChildID: 7, From: 4, To: 7, Lead: 1, BeatSec: 0.5}})
	if err != nil {
		t.Fatalf("RebuildInserts: %v", err)
	}
	if f.stemsCalls != 1 {
		t.Errorf("MakeStems вызван %d раз, want ровно 1", f.stemsCalls)
	}
	out := uploaded(t, f, "overdub-inst-7.flac")
	if d := float64(len(out)) / sr; math.Abs(d-10) > 0.05 {
		t.Errorf("длина выхода %.3f с, want 10 ± 0.05 (длина базы)", d)
	}
	thr := clickThreshold(t, out, 0, 2.5)
	// начало партии = From−Lead = 3, щелчок партии на 1.5 → 4.5 с трека
	if at := firstAbove(out, thr, 0, 10); math.Abs(at-4.5) > 0.015 {
		t.Errorf("щелчок в выходе на %.4f с, want 4.5 ± 0.015", at)
	}
	if res == nil || len(res.Inserts) != 1 {
		t.Fatalf("Inserts = %+v, want 1 отчёт", res)
	}
	r := res.Inserts[0]
	if r.ChildID != 7 {
		t.Errorf("Report.ChildID=%d, want 7", r.ChildID)
	}
	if r.Aligned {
		t.Errorf("Report.Aligned=true без стема — должно стоять по плану")
	}
	if math.Abs(r.StartSec-3) > 0.015 {
		t.Errorf("Report.StartSec=%.4f, want 3 (From−Lead)", r.StartSec)
	}
	if !(r.Gain > 0) {
		t.Errorf("Report.Gain=%v, want > 0", r.Gain)
	}
}

// Кейс 3б: стема нет, MakeStems его сделал → стем скачивается повторно, MakeStems — ровно один раз.
func TestRebuildInsertsMakesStemsOnceAndRefetches(t *testing.T) {
	f, dir := setup(t)
	f.makeStemsAdds = true
	f.stemPath = lavfi(t, clicks(10, 0.5, 1.5, 2.5, 3.5, 4.5, 5.5, 6.5, 7.5, 8.5),
		filepath.Join(dir, "drums.flac"))
	f.files[key(7, "audio.flac")] = lavfi(t, clicks(4, 0.5, 1.5, 2.5, 3.5), filepath.Join(dir, "p7.flac"))

	if _, err := RebuildInserts(context.Background(), f, parentID,
		[]InsertSpec{{ChildID: 7, From: 4, To: 7, Lead: 1, BeatSec: 0.5}}); err != nil {
		t.Fatalf("RebuildInserts: %v", err)
	}
	if f.stemsCalls != 1 {
		t.Errorf("MakeStems вызван %d раз, want 1", f.stemsCalls)
	}
	n := 0
	for _, k := range f.fetched {
		if k == key(parentID, "stem-drums.flac") {
			n++
		}
	}
	if n < 2 {
		t.Errorf("stem-drums.flac запрошен %d раз — после MakeStems стем должен браться снова", n)
	}
}

// Кейс 4: звучит только окно [From,To] — часть партии до From в трек не попадает.
func TestRebuildInsertsOnlyWindowSounds(t *testing.T) {
	f, dir := setup(t)
	// щелчок 0.5 с партии → 3.5 с трека (до From=4) — вырезан;
	// щелчок 2.0 с партии → 5.0 с трека (в окне) — звучит (контроль, что вклейка вообще есть)
	f.files[key(7, "audio.flac")] = lavfi(t, clicks(4, 0.5, 2.0), filepath.Join(dir, "p7.flac"))

	if _, err := RebuildInserts(context.Background(), f, parentID,
		[]InsertSpec{{ChildID: 7, From: 4, To: 7, Lead: 1, BeatSec: 0.5}}); err != nil {
		t.Fatalf("RebuildInserts: %v", err)
	}
	out := uploaded(t, f, "overdub-inst-7.flac")
	thr := clickThreshold(t, out, 0, 2.5)
	if at := firstAbove(out, thr, 0, 4.0); at >= 0 {
		t.Errorf("до From=4.0 в выходе звук партии на %.4f с — должно быть только окно", at)
	}
	if at := firstAbove(out, thr, 4.0, 10); math.Abs(at-5.0) > 0.015 {
		t.Errorf("щелчок окна на %.4f с, want 5.0 ± 0.015", at)
	}
}

// Кейс 5: Db поверх выравнивания по RMS — −6 дБ даёт гейн ×0.501 от 0 дБ.
func TestRebuildInsertsDbScalesGain(t *testing.T) {
	gain := func(db float64) float64 {
		f, dir := setup(t)
		f.files[key(7, "audio.flac")] = lavfi(t, clicks(4, 1.5, 2.5), filepath.Join(dir, "p7.flac"))
		res, err := RebuildInserts(context.Background(), f, parentID,
			[]InsertSpec{{ChildID: 7, From: 4, To: 7, Lead: 1, BeatSec: 0.5, Db: db}})
		if err != nil {
			t.Fatalf("RebuildInserts(Db=%g): %v", db, err)
		}
		if res == nil || len(res.Inserts) != 1 {
			t.Fatalf("Inserts = %+v, want 1 отчёт", res)
		}
		return res.Inserts[0].Gain
	}
	g0, g6 := gain(0), gain(-6)
	if !(g0 > 0) || !(g6 > 0) {
		t.Fatalf("Gain должны быть > 0: Db0=%v Db-6=%v", g0, g6)
	}
	if ratio := g6 / g0; math.Abs(ratio-0.501) > 0.01 {
		t.Errorf("Gain(-6)/Gain(0)=%.4f, want 0.501 ± 0.01", ratio)
	}
}

// Кейс 6: две спеки — обе партии в выходе на своих местах.
func TestRebuildInsertsTwoSpecsBothPresent(t *testing.T) {
	f, dir := setup(t)
	f.files[key(7, "audio.flac")] = lavfi(t, clicks(4, 1.5), filepath.Join(dir, "p7.flac"))
	f.files[key(9, "audio.flac")] = lavfi(t, clicks(4, 1.5), filepath.Join(dir, "p9.flac"))

	res, err := RebuildInserts(context.Background(), f, parentID, []InsertSpec{
		{ChildID: 7, From: 2, To: 4, Lead: 1, BeatSec: 0.5}, // старт 1 → щелчок 2.5
		{ChildID: 9, From: 6, To: 8, Lead: 1, BeatSec: 0.5}, // старт 5 → щелчок 6.5
	})
	if err != nil {
		t.Fatalf("RebuildInserts: %v", err)
	}
	out := uploaded(t, f, "overdub-inst-9.flac")
	thr := clickThreshold(t, out, 8.5, 10)
	if at := firstAbove(out, thr, 0, 5); math.Abs(at-2.5) > 0.015 {
		t.Errorf("щелчок партии 7 на %.4f с, want 2.5 ± 0.015", at)
	}
	if at := firstAbove(out, thr, 5, 10); math.Abs(at-6.5) > 0.015 {
		t.Errorf("щелчок партии 9 на %.4f с, want 6.5 ± 0.015", at)
	}
	if res == nil || len(res.Inserts) != 2 {
		t.Fatalf("Inserts = %+v, want 2 отчёта", res)
	}
	if res.Inserts[0].ChildID != 7 || res.Inserts[1].ChildID != 9 {
		t.Errorf("порядок отчётов %d,%d, want 7,9", res.Inserts[0].ChildID, res.Inserts[1].ChildID)
	}
}

// Кейс 7: пустой specs — ошибка, ничего не загружено.
func TestRebuildInsertsEmptySpecs(t *testing.T) {
	f := newFake()
	for _, specs := range [][]InsertSpec{nil, {}} {
		if _, err := RebuildInserts(context.Background(), f, parentID, specs); err == nil {
			t.Errorf("specs=%v: ошибки нет, want ошибка", specs)
		}
	}
	if len(f.uploads) != 0 {
		t.Errorf("UploadDsp вызван при пустом specs: %v", keys(f.uploads))
	}
}

// Кейс 8: у ребёнка нет audio.flac — ошибка, ничего не загружено.
func TestRebuildInsertsMissingChildAudio(t *testing.T) {
	f, _ := setup(t)
	if _, err := RebuildInserts(context.Background(), f, parentID,
		[]InsertSpec{{ChildID: 7, From: 4, To: 7, Lead: 1, BeatSec: 0.5}}); err == nil {
		t.Fatal("ошибки нет, want ошибка скачивания партии")
	}
	if len(f.uploads) != 0 {
		t.Errorf("UploadDsp вызван при ошибке партии: %v", keys(f.uploads))
	}
}

// Кейс 8б: у родителя нет audio.flac — ошибка (база не скачалась).
func TestRebuildInsertsMissingBase(t *testing.T) {
	needFFmpeg(t)
	f := newFake()
	f.files[key(7, "audio.flac")] = lavfi(t, clicks(4, 1.5), filepath.Join(t.TempDir(), "p7.flac"))
	if _, err := RebuildInserts(context.Background(), f, parentID,
		[]InsertSpec{{ChildID: 7, From: 4, To: 7, Lead: 1, BeatSec: 0.5}}); err == nil {
		t.Fatal("ошибки нет, want ошибка скачивания базы")
	}
	if len(f.uploads) != 0 {
		t.Errorf("UploadDsp вызван без базы: %v", keys(f.uploads))
	}
}

// Кейс 9: стем уже есть — MakeStems не вызывается, стем скачивается.
func TestRebuildInsertsExistingStemNoMakeStems(t *testing.T) {
	f, dir := setup(t)
	f.files[key(parentID, "stem-drums.flac")] = lavfi(t,
		clicks(10, 0.5, 1.5, 2.5, 3.5, 4.5, 5.5, 6.5, 7.5, 8.5), filepath.Join(dir, "drums.flac"))
	f.files[key(7, "audio.flac")] = lavfi(t, clicks(4, 0.5, 1.5, 2.5, 3.5), filepath.Join(dir, "p7.flac"))

	res, err := RebuildInserts(context.Background(), f, parentID,
		[]InsertSpec{{ChildID: 7, From: 4, To: 7, Lead: 1, BeatSec: 0.5}})
	if err != nil {
		t.Fatalf("RebuildInserts: %v", err)
	}
	if f.stemsCalls != 0 {
		t.Errorf("MakeStems вызван %d раз при готовом стеме, want 0", f.stemsCalls)
	}
	if !contains(f.fetched, key(parentID, "stem-drums.flac")) {
		t.Errorf("стем не скачивался: %v", f.fetched)
	}
	if res == nil || len(res.Inserts) != 1 {
		t.Fatalf("Inserts = %+v, want 1 отчёт", res)
	}
	out := uploaded(t, f, "overdub-inst-7.flac")
	if d := float64(len(out)) / sr; math.Abs(d-10) > 0.05 {
		t.Errorf("длина выхода %.3f с, want 10 ± 0.05", d)
	}
}
