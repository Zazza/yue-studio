package studio

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yue-studio/internal/dsp"
)

// Тесты карточки internal-guitar-pedals, условия 1.2–1.4: кэш файлов воркера
// (Cache) и быстрое превью шагов на куске трека (Preview) — на весь трек и на
// дорожку. Написаны по карточке, без чтения реализации. Воркер — secFake
// (sections_test.go): считаются вызовы FetchAudio и MakeStems. Звук — синтетика
// 16 кГц моно; хелперы lavfi/decodeFile/slice/rms/key — из helpers_test.go и
// sections_test.go.
//
// Синтетика (pvDur = 8 с): other — шум с полосой до 6 кГц (без периодичности,
// поэтому сдвиг во времени виден однозначно), drums — тон 300 Гц;
// audio.flac = other + drums. Окно превью [2.5, 4.5]: до From больше 2 с —
// есть где «прогреть» эффект.

const (
	pvDur  = 8.0
	pvFrom = 2.5
	pvTo   = 4.5
	// максимальный хвост превью (карточка 1.3: «+ хвост ≤ 3 с»)
	pvMaxTail = 3.0
)

const (
	pvNoise = "anoisesrc=d=8:c=white:seed=11:a=0.25:r=16000,lowpass=f=6000"
	pvTone  = "aevalsrc=exprs='0.2*sin(2*PI*300*t)':d=8:s=16000"
)

// pvFiles — файлы трека на диске: audio = other + drums, стемы other/drums/bass/vocals.
func pvFiles(t *testing.T) map[string]string {
	t.Helper()
	needFFmpeg(t)
	dir := t.TempDir()
	return map[string]string{
		"audio.flac": lavfi(t, pvNoise+"[n];"+pvTone+"[d];[n][d]amix=inputs=2:normalize=0[out0]",
			filepath.Join(dir, "pv-audio.flac")),
		"stem-other.flac":  lavfi(t, pvNoise, filepath.Join(dir, "pv-other.flac")),
		"stem-drums.flac":  lavfi(t, pvTone, filepath.Join(dir, "pv-drums.flac")),
		"stem-bass.flac":   lavfi(t, aeval("0", pvDur), filepath.Join(dir, "pv-bass.flac")),
		"stem-vocals.flac": lavfi(t, aeval("0", pvDur), filepath.Join(dir, "pv-vocals.flac")),
	}
}

// pvSetup — фейковый воркер с треком parentID и всеми стемами.
func pvSetup(t *testing.T) (*secFake, map[string]string) {
	t.Helper()
	f := newSecFake()
	pf := pvFiles(t)
	put(f, parentID, pf)
	return f, pf
}

func pvPreview(t *testing.T, f *secFake, cache *Cache, spec PreviewSpec) *PreviewResult {
	t.Helper()
	res, err := Preview(context.Background(), f, cache, spec, t.TempDir())
	if err != nil {
		t.Fatalf("Preview(%+v): %v", spec, err)
	}
	if res == nil {
		t.Fatal("Preview: nil результат без ошибки")
	}
	return res
}

func pvSpec(stem string, steps ...dsp.Step) PreviewSpec {
	return PreviewSpec{JobID: parentID, Stem: stem, Steps: steps, From: pvFrom, To: pvTo}
}

func pvFetchCount(f *secFake, k string) int {
	n := 0
	for _, v := range f.fetched {
		if v == k {
			n++
		}
	}
	return n
}

func pvSecs(s []float32) float64 { return float64(len(s)) / sr }

// pvLag — сдвиг b относительно a в отсчётах (b[i+lag] ≈ a[i]), |lag| ≤ maxLag,
// по максимуму взаимной корреляции на первых n отсчётах.
func pvLag(a, b []float32, maxLag, n int) int {
	best, bestV := 0, math.Inf(-1)
	for lag := -maxLag; lag <= maxLag; lag++ {
		var s float64
		for i := maxLag; i < n-maxLag && i+lag < len(b) && i < len(a); i++ {
			s += float64(a[i]) * float64(b[i+lag])
		}
		if s > bestV {
			best, bestV = lag, s
		}
	}
	return best
}

// pvRelDb — RMS(got − ref) к RMS(ref) на отсчётах [0, n), дБ; сдвиг lag: got[i+lag] ↔ ref[i].
func pvRelDb(got, ref []float32, lag, n int) float64 {
	var e, eb float64
	for i := 0; i < n; i++ {
		j := i + lag
		if j < 0 || j >= len(got) || i >= len(ref) {
			continue
		}
		d := float64(got[j]) - float64(ref[i])
		e += d * d
		eb += float64(ref[i]) * float64(ref[i])
	}
	if e == 0 {
		return -200
	}
	return 10 * math.Log10(e/eb)
}

// pvSub — a − b поотсчётно (по короткой длине).
func pvSub(a, b []float32) []float32 {
	n := min(len(a), len(b))
	d := make([]float32, n)
	for i := range d {
		d[i] = a[i] - b[i]
	}
	return d
}

func pvDecode(t *testing.T, path string) []float32 {
	t.Helper()
	if path == "" {
		t.Fatal("пустой путь файла превью")
	}
	return decodeFile(t, path)
}

// ---------- 1.2 Cache ----------

// Карточка 1.2: первый Fetch качает с воркера, повторный для того же (job, name) —
// без обращения к воркеру и тот же путь; файл — копия файла воркера.
func TestCacheFetchOnceSamePath(t *testing.T) {
	f, pf := pvSetup(t)
	dir := t.TempDir()
	c := NewCache(dir)
	ctx := context.Background()

	p1, err := c.Fetch(ctx, f, parentID, "audio.flac")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if n := pvFetchCount(f, key(parentID, "audio.flac")); n != 1 {
		t.Fatalf("первый Fetch: обращений к воркеру %d, want 1", n)
	}
	want, _ := os.ReadFile(pf["audio.flac"])
	got, err := os.ReadFile(p1)
	if err != nil {
		t.Fatalf("файл кэша %s не читается: %v", p1, err)
	}
	if string(got) != string(want) {
		t.Errorf("файл кэша (%d байт) ≠ файлу воркера (%d байт)", len(got), len(want))
	}
	// карточка 1.6 (часть): файлы — в каталоге кэша
	if rel, err := filepath.Rel(dir, p1); err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("путь %s вне каталога кэша %s", p1, dir)
	}

	p2, err := c.Fetch(ctx, f, parentID, "audio.flac")
	if err != nil {
		t.Fatalf("повторный Fetch: %v", err)
	}
	if p2 != p1 {
		t.Errorf("повторный Fetch: путь %s, want тот же %s", p2, p1)
	}
	if n := pvFetchCount(f, key(parentID, "audio.flac")); n != 1 {
		t.Errorf("повторный Fetch обратился к воркеру: обращений %d, want 1", n)
	}
}

// Карточка 1.2: разные имена и разные джобы — разные записи кэша.
func TestCacheFetchDistinctKeys(t *testing.T) {
	f, pf := pvSetup(t)
	put(f, 2, map[string]string{"audio.flac": pf["stem-drums.flac"]})
	c := NewCache(t.TempDir())
	ctx := context.Background()
	a, err := c.Fetch(ctx, f, parentID, "audio.flac")
	if err != nil {
		t.Fatal(err)
	}
	o, err := c.Fetch(ctx, f, parentID, "stem-other.flac")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Fetch(ctx, f, 2, "audio.flac")
	if err != nil {
		t.Fatal(err)
	}
	if a == o || a == b || o == b {
		t.Errorf("пути должны различаться: %s %s %s", a, o, b)
	}
	if n := pvFetchCount(f, key(2, "audio.flac")); n != 1 {
		t.Errorf("джоба 2: обращений %d, want 1 (своя запись, не audio.flac джобы 1)", n)
	}
}

// Карточка 1.2: Invalidate(job) — следующий Fetch качает заново; чужая джоба не тронута.
func TestCacheInvalidate(t *testing.T) {
	f, pf := pvSetup(t)
	put(f, 2, map[string]string{"audio.flac": pf["stem-drums.flac"]})
	c := NewCache(t.TempDir())
	ctx := context.Background()
	for _, id := range []int64{parentID, 2} {
		if _, err := c.Fetch(ctx, f, id, "audio.flac"); err != nil {
			t.Fatal(err)
		}
	}
	c.Invalidate(parentID)
	if _, err := c.Fetch(ctx, f, parentID, "audio.flac"); err != nil {
		t.Fatalf("Fetch после Invalidate: %v", err)
	}
	if n := pvFetchCount(f, key(parentID, "audio.flac")); n != 2 {
		t.Errorf("после Invalidate: обращений к воркеру %d, want 2 (качает заново)", n)
	}
	if _, err := c.Fetch(ctx, f, 2, "audio.flac"); err != nil {
		t.Fatal(err)
	}
	if n := pvFetchCount(f, key(2, "audio.flac")); n != 1 {
		t.Errorf("Invalidate(1) сбросил джобу 2: обращений %d, want 1", n)
	}
}

// Краевой случай: файла на воркере нет — ошибка; Invalidate джобы без записей — не паника.
func TestCacheFetchMissingIsError(t *testing.T) {
	f := newSecFake()
	c := NewCache(t.TempDir())
	c.Invalidate(99)
	if p, err := c.Fetch(context.Background(), f, 99, "audio.flac"); err == nil {
		t.Errorf("нет файла на воркере: want ошибку, got путь %q", p)
	}
}

// ---------- 1.3 Preview, весь трек ----------

// Карточка 1.3: Dry — кусок трека [From, To] (сдвиг ≤ 5 мс, разница ≤ −50 дБ);
// длины Wet и Dry равны (±10 мс) и = To − From (хвоста нет) ± 0.05 с.
func TestPreviewWholeTrackDry(t *testing.T) {
	f, pf := pvSetup(t)
	res := pvPreview(t, f, NewCache(t.TempDir()), pvSpec("", dsp.Step{Chain: "eq",
		Params: map[string]float64{"high": -12}}))
	if res.From != pvFrom || res.To != pvTo {
		t.Errorf("окно результата [%v, %v], want [%v, %v]", res.From, res.To, pvFrom, pvTo)
	}
	dry, wet := pvDecode(t, res.Dry), pvDecode(t, res.Wet)
	src := slice(decodeFile(t, pf["audio.flac"]), pvFrom, pvDur)

	n := int((pvTo - pvFrom) * sr)
	lag := pvLag(src, dry, int(0.010*sr), n)
	if math.Abs(float64(lag))/sr > 0.005 {
		t.Errorf("Dry сдвинут относительно трека на %.1f мс, want ≤ 5", float64(lag)/sr*1000)
	}
	if d := pvRelDb(dry, src, lag, n-int(0.01*sr)); d > -50 {
		t.Errorf("Dry vs трек [%.1f, %.1f]: разница %.1f дБ, want ≤ −50", pvFrom, pvTo, d)
	}
	want := pvTo - pvFrom
	if d := pvSecs(dry); math.Abs(d-want) > 0.05 {
		t.Errorf("длина Dry %.3f с, want %.2f ± 0.05 (To − From, хвоста нет)", d, want)
	}
	if d := pvSecs(wet) - pvSecs(dry); math.Abs(d) > 0.010 {
		t.Errorf("длины Wet %.3f и Dry %.3f различаются на %.0f мс, want ≤ 10", pvSecs(wet), pvSecs(dry), d*1000)
	}
}

// Карточка 1.3: Wet — тот же кусок через шаги. Шаги без изменений (eq с нулями) —
// Wet = Dry (≤ −50 дБ); шаги с изменениями — Wet ≈ StepsGraph по Dry и ≠ Dry.
func TestPreviewWholeTrackWetIsStepsOverDry(t *testing.T) {
	f, _ := pvSetup(t)
	cache := NewCache(t.TempDir())
	n := int((pvTo - pvFrom) * sr)

	t.Run("eq с нулями", func(t *testing.T) {
		res := pvPreview(t, f, cache, pvSpec("", dsp.Step{Chain: "eq"}))
		dry, wet := pvDecode(t, res.Dry), pvDecode(t, res.Wet)
		if d := pvRelDb(wet, dry, 0, n); d > -50 {
			t.Errorf("eq с нулями: Wet vs Dry %.1f дБ, want ≤ −50", d)
		}
	})

	t.Run("тёмный eq + перегруз", func(t *testing.T) {
		steps := []dsp.Step{{Chain: "eq", Params: map[string]float64{"high": -12, "low": 6}},
			{Chain: "grit", Params: map[string]float64{"drive": 2}}}
		res := pvPreview(t, f, cache, pvSpec("", steps...))
		dry, wet := pvDecode(t, res.Dry), pvDecode(t, res.Wet)
		g, _, err := dsp.StepsGraph(steps)
		if err != nil {
			t.Fatal(err)
		}
		refPath := filepath.Join(t.TempDir(), "ref.flac")
		if err := dsp.Run(res.Dry, refPath, g, nil); err != nil {
			t.Fatal(err)
		}
		ref := decodeFile(t, refPath)
		// первые 0.1 с пропускаются: у фильтров и лимитеров своё «разгонное» состояние
		skip := int(0.1 * sr)
		if d := pvRelDb(wet[skip:], ref[skip:], 0, n-skip); d > -30 {
			t.Errorf("Wet vs шаги по Dry: разница %.1f дБ, want ≤ −30", d)
		}
		if d := pvRelDb(wet, dry, 0, n); d <= -20 {
			t.Errorf("Wet почти = Dry (%.1f дБ): шаги не применены", d)
		}
	})
}

// Карточка 1.3: хвост — длины Wet и Dry = To − From + min(хвост, 3) ± 0.05 с, между
// собой ±10 мс; хвост — сумма TailSec включённых шагов.
func TestPreviewWholeTrackTailLength(t *testing.T) {
	f, _ := pvSetup(t)
	cache := NewCache(t.TempDir())
	cases := []struct {
		name  string
		steps []dsp.Step
	}{
		{"зал 1.5 с", []dsp.Step{{Chain: "reverb-hall", Params: map[string]float64{"size": 1.5, "predelay": 0}}}},
		{"зал + комната > 3 с — обрезка до 3", []dsp.Step{
			{Chain: "reverb-hall", Params: map[string]float64{"size": 2.5, "predelay": 0}},
			{Chain: "reverb-room", Params: map[string]float64{"size": 1.5, "predelay": 0}}}},
		{"зал выключен — хвоста нет", []dsp.Step{{Chain: "grit"},
			{Chain: "reverb-hall", Params: map[string]float64{"size": 2}, Off: true}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, tail, err := dsp.StepsGraph(c.steps)
			if err != nil {
				t.Fatal(err)
			}
			res := pvPreview(t, f, cache, pvSpec("", c.steps...))
			dry, wet := pvDecode(t, res.Dry), pvDecode(t, res.Wet)
			want := pvTo - pvFrom + math.Min(tail, pvMaxTail)
			if d := pvSecs(dry); math.Abs(d-want) > 0.05 {
				t.Errorf("длина Dry %.3f с, want %.3f ± 0.05 (хвост %.2f)", d, want, tail)
			}
			if d := pvSecs(wet); math.Abs(d-want) > 0.05 {
				t.Errorf("длина Wet %.3f с, want %.3f ± 0.05 (хвост %.2f)", d, want, tail)
			}
			if d := pvSecs(wet) - pvSecs(dry); math.Abs(d) > 0.010 {
				t.Errorf("Wet и Dry различаются на %.0f мс, want ≤ 10", d*1000)
			}
		})
	}
}

// Контракт: To ≤ From — ошибка; пустые шаги — ошибка (StepsGraph: ни одного включённого).
func TestPreviewBadSpecIsError(t *testing.T) {
	f, _ := pvSetup(t)
	cache := NewCache(t.TempDir())
	eq := []dsp.Step{{Chain: "eq"}}
	cases := map[string]PreviewSpec{
		"To = From":          {JobID: parentID, Steps: eq, From: 3, To: 3},
		"To < From":          {JobID: parentID, Steps: eq, From: 4, To: 3},
		"без шагов":          {JobID: parentID, From: pvFrom, To: pvTo},
		"все выкл":           {JobID: parentID, Steps: []dsp.Step{{Chain: "eq", Off: true}}, From: pvFrom, To: pvTo},
		"key-цепочка":        {JobID: parentID, Steps: []dsp.Step{{Chain: "ducking"}}, From: pvFrom, To: pvTo},
		"нет цепочки":        {JobID: parentID, Steps: []dsp.Step{{Chain: "no-such"}}, From: pvFrom, To: pvTo},
		"нет трека":          {JobID: 77, Steps: eq, From: pvFrom, To: pvTo},
		"To = From, дорожка": {JobID: parentID, Stem: "other", Steps: eq, From: 3, To: 3},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if res, err := Preview(context.Background(), f, cache, spec, t.TempDir()); err == nil {
				t.Errorf("Preview(%+v): want ошибку, got %+v", spec, res)
			}
		})
	}
}

// ---------- 1.4 Preview, дорожка ----------

// Карточка 1.4: шаг без изменений (громкость 0 дБ / eq с нулями) на дорожке —
// вычитание без остатка: Wet = Dry ≤ −50 дБ; Dry — кусок трека.
func TestPreviewStemNeutralStepNulls(t *testing.T) {
	f, pf := pvSetup(t)
	cache := NewCache(t.TempDir())
	n := int((pvTo - pvFrom) * sr)
	src := slice(decodeFile(t, pf["audio.flac"]), pvFrom, pvDur)
	for name, step := range map[string]dsp.Step{
		"eq с нулями":    {Chain: "eq"},
		"громкость 0 дБ": {Chain: "level", Params: map[string]float64{"gain": 0}},
	} {
		t.Run(name, func(t *testing.T) {
			res := pvPreview(t, f, cache, pvSpec("other", step))
			dry, wet := pvDecode(t, res.Dry), pvDecode(t, res.Wet)
			if d := pvRelDb(wet, dry, 0, n); d > -50 {
				t.Errorf("Wet vs Dry %.1f дБ, want ≤ −50 (дорожка без изменений — вычитание без остатка)", d)
			}
			if d := pvRelDb(dry, src, 0, n); d > -50 {
				t.Errorf("Dry vs трек [%.1f, %.1f]: %.1f дБ, want ≤ −50 (Dry — кусок трека)", pvFrom, pvTo, d)
			}
		})
	}
}

// Карточка 1.4: Wet − Dry ≈ обработанная − исходная дорожка (меняется только она).
func TestPreviewStemChangesOnlyThisStem(t *testing.T) {
	f, pf := pvSetup(t)
	params := map[string]float64{"high": -12, "highf": 2000, "low": 6}
	res := pvPreview(t, f, NewCache(t.TempDir()), pvSpec("other", dsp.Step{Chain: "eq", Params: params}))
	dry, wet := pvDecode(t, res.Dry), pvDecode(t, res.Wet)

	// эталон: eq по всей дорожке other, кусок [From, To]
	refPath := filepath.Join(t.TempDir(), "other-eq.flac")
	if err := dsp.Run(pf["stem-other.flac"], refPath, dsp.ByID("eq").FilterGraph(params), nil); err != nil {
		t.Fatal(err)
	}
	other := decodeFile(t, pf["stem-other.flac"])
	want := pvSub(slice(decodeFile(t, refPath), pvFrom, pvTo), slice(other, pvFrom, pvTo))
	got := pvSub(wet, dry)
	n := int((pvTo - pvFrom) * sr)
	if rms(want) < 0.01 {
		t.Fatalf("эталонная разница слишком мала (%.4f) — синтетика не годится", rms(want))
	}
	if d := pvRelDb(got, want, 0, n); d > -30 {
		t.Errorf("Wet − Dry vs (обработанная − исходная other): разница %.1f дБ, want ≤ −30", d)
	}
	// тон drums (300 Гц) не тронут: его амплитуда в Wet как в Dry
	if d := db(toneAmp(wet, 300, 0, 2)) - db(toneAmp(dry, 300, 0, 2)); math.Abs(d) > 0.5 {
		t.Errorf("drums (300 Гц) в Wet изменился на %+.2f дБ, want ±0.5 — эффект только на other", d)
	}
}

// Карточка 1.4: эффект «прогрет» с 2 с до From — у цепочки с памятью (реверб)
// нет провала громкости в начале окна: уровень эффекта (Wet − Dry) в первые
// 0.2 с не ниже уровня в середине окна больше чем на 1.5 дБ.
func TestPreviewStemEffectIsWarm(t *testing.T) {
	f, _ := pvSetup(t)
	step := dsp.Step{Chain: "reverb-hall", Params: map[string]float64{"size": 2, "predelay": 0, "wet": 1, "width": 0}}
	res := pvPreview(t, f, NewCache(t.TempDir()), pvSpec("other", step))
	eff := pvSub(pvDecode(t, res.Wet), pvDecode(t, res.Dry))
	head, mid := rms(slice(eff, 0, 0.2)), rms(slice(eff, 1.0, 2.0))
	if mid <= 0 {
		t.Fatal("эффекта нет: Wet = Dry в середине окна")
	}
	if d := db(head) - db(mid); d < -1.5 {
		t.Errorf("начало окна: эффект тише середины на %.1f дБ, want ≥ −1.5 (реверб прогрет до From)", -d)
	}
}

// Карточка 1.4: стемов нет — один раз MakeStems, затем превью по появившимся стемам.
func TestPreviewStemMakesStemsOnce(t *testing.T) {
	needFFmpeg(t)
	f := newSecFake()
	pf := pvFiles(t)
	f.files[key(parentID, "audio.flac")] = pf["audio.flac"]
	f.afterStems[parentID] = map[string]string{}
	for name, p := range pf {
		if name != "audio.flac" {
			f.afterStems[parentID][name] = p
		}
	}
	res := pvPreview(t, f, NewCache(t.TempDir()), pvSpec("other", dsp.Step{Chain: "eq"}))
	if f.stemsCalls[parentID] != 1 {
		t.Errorf("MakeStems вызван %d раз, want 1", f.stemsCalls[parentID])
	}
	if res.Wet == "" || res.Dry == "" {
		t.Errorf("после MakeStems превью без файлов: %+v", res)
	}
}

// Карточка 1.4: стемов нет и MakeStems их не дал — ошибка, MakeStems ровно один раз.
func TestPreviewStemNoStemsIsError(t *testing.T) {
	for name, failed := range map[string]bool{"стемы не появились": false, "MakeStems с ошибкой": true} {
		t.Run(name, func(t *testing.T) {
			needFFmpeg(t)
			f := newSecFake()
			f.stemsFailed = failed
			f.files[key(parentID, "audio.flac")] = pvFiles(t)["audio.flac"]
			res, err := Preview(context.Background(), f, NewCache(t.TempDir()),
				pvSpec("other", dsp.Step{Chain: "eq"}), t.TempDir())
			if err == nil {
				t.Errorf("нет стема other: want ошибку, got %+v", res)
			}
			if f.stemsCalls[parentID] != 1 {
				t.Errorf("MakeStems вызван %d раз, want 1", f.stemsCalls[parentID])
			}
		})
	}
}

// Карточка 1.2 в превью: повторное превью того же трека (тёплый кэш) не качает
// файлы с воркера заново.
func TestPreviewUsesCache(t *testing.T) {
	f, _ := pvSetup(t)
	cache := NewCache(t.TempDir())
	pvPreview(t, f, cache, pvSpec("other", dsp.Step{Chain: "eq"}))
	before := len(f.fetched)
	pvPreview(t, f, cache, pvSpec("other", dsp.Step{Chain: "grit"}))
	if after := len(f.fetched); after != before {
		t.Errorf("повторное превью скачало ещё %d файлов: %v, want 0 (тёплый кэш)", after-before, f.fetched[before:])
	}
}
