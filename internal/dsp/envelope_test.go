package dsp

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// Тесты карточки internal-vocal-ride, условие 1: огибающая громкости.
// NormalizeEnvelope — чистка точек; EnvelopeGraph — граф для Run, громкость
// кусочно-линейная в дБ между точками, до первой/после последней — дБ крайней.
// Звук проверяется на синтетике (тон) через Run, а не по тексту графа.
// Хелперы (needFFmpeg, genIn, decode, segRMS, dbfs, secs) — из inserts_test.go/chains_test.go.

// sameEnv — точки равны побитово (NaN == NaN), чтобы сравнивать вход до/после.
func sameEnv(a, b []EnvPoint) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Float64bits(a[i].T) != math.Float64bits(b[i].T) ||
			math.Float64bits(a[i].Db) != math.Float64bits(b[i].Db) {
			return false
		}
	}
	return true
}

func normOK(t *testing.T, in []EnvPoint) []EnvPoint {
	t.Helper()
	got, err := NormalizeEnvelope(in)
	if err != nil {
		t.Fatalf("NormalizeEnvelope(%v): %v", in, err)
	}
	return got
}

func TestEnvelopeConstants(t *testing.T) {
	if EnvMinDb != -30 || EnvMaxDb != 12 || EnvMaxPoints != 200 {
		t.Errorf("константы: EnvMinDb=%v EnvMaxDb=%v EnvMaxPoints=%v, want −30, 12, 200",
			EnvMinDb, EnvMaxDb, EnvMaxPoints)
	}
}

// Точки приходят из фронта/MCP по JSON как {"t":…,"db":…}.
func TestEnvPointJSON(t *testing.T) {
	var p []EnvPoint
	if err := json.Unmarshal([]byte(`[{"t":1.5,"db":-3}]`), &p); err != nil {
		t.Fatal(err)
	}
	if len(p) != 1 || p[0] != (EnvPoint{T: 1.5, Db: -3}) {
		t.Errorf("разбор: %+v, want [{T:1.5 Db:-3}]", p)
	}
	raw, err := json.Marshal(EnvPoint{T: 2, Db: 6})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"t":2,"db":6}` {
		t.Errorf("сериализация %s, want {\"t\":2,\"db\":6}", raw)
	}
}

// Условие: сортировка по T.
func TestNormalizeEnvelopeSorts(t *testing.T) {
	got := normOK(t, []EnvPoint{{T: 3, Db: -1}, {T: 1, Db: 2}, {T: 2, Db: 0}})
	want := []EnvPoint{{T: 1, Db: 2}, {T: 2, Db: 0}, {T: 3, Db: -1}}
	if !sameEnv(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Условие: T<0 и NaN/Inf в T или Db — отбрасываются; T = 0 — допустимая точка.
func TestNormalizeEnvelopeDropsInvalid(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	got := normOK(t, []EnvPoint{
		{T: -1, Db: -6},
		{T: nan, Db: -6},
		{T: inf, Db: -6},
		{T: math.Inf(-1), Db: -6},
		{T: 1, Db: nan},
		{T: 2, Db: inf},
		{T: 3, Db: math.Inf(-1)},
		{T: 0, Db: -3},
		{T: 4, Db: 1},
	})
	want := []EnvPoint{{T: 0, Db: -3}, {T: 4, Db: 1}}
	if !sameEnv(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Условие: Db зажимается в [EnvMinDb, EnvMaxDb]; граничные значения — как есть.
func TestNormalizeEnvelopeClampsDb(t *testing.T) {
	got := normOK(t, []EnvPoint{{T: 1, Db: 50}, {T: 2, Db: -100}, {T: 3, Db: 12}, {T: 4, Db: -30}, {T: 5, Db: -7.5}})
	want := []EnvPoint{{T: 1, Db: 12}, {T: 2, Db: -30}, {T: 3, Db: 12}, {T: 4, Db: -30}, {T: 5, Db: -7.5}}
	if !sameEnv(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Условие: одинаковое T — остаётся последняя по входу.
func TestNormalizeEnvelopeDuplicateTKeepsLast(t *testing.T) {
	got := normOK(t, []EnvPoint{{T: 1, Db: -3}, {T: 2, Db: 0}, {T: 1, Db: -6}})
	want := []EnvPoint{{T: 1, Db: -6}, {T: 2, Db: 0}}
	if !sameEnv(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// дубль после сортировки тоже по порядку входа, а не по позиции после сортировки
	got = normOK(t, []EnvPoint{{T: 5, Db: 3}, {T: 1, Db: 0}, {T: 5, Db: -9}, {T: 5, Db: 4}})
	want = []EnvPoint{{T: 1, Db: 0}, {T: 5, Db: 4}}
	if !sameEnv(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Условие: пусто (в т.ч. после чистки) — ошибка.
func TestNormalizeEnvelopeEmptyIsError(t *testing.T) {
	cases := map[string][]EnvPoint{
		"nil":           nil,
		"пустой срез":   {},
		"всё невалидно": {{T: -1, Db: 0}, {T: math.NaN(), Db: 0}, {T: 1, Db: math.Inf(1)}},
	}
	for name, in := range cases {
		if got, err := NormalizeEnvelope(in); err == nil {
			t.Errorf("%s: want ошибку, got %v", name, got)
		}
	}
}

// Условие: больше EnvMaxPoints — ошибка; ровно EnvMaxPoints — можно.
func TestNormalizeEnvelopeMaxPoints(t *testing.T) {
	mk := func(n int) []EnvPoint {
		p := make([]EnvPoint, n)
		for i := range p {
			p[i] = EnvPoint{T: float64(i) * 0.5, Db: -1}
		}
		return p
	}
	if got, err := NormalizeEnvelope(mk(EnvMaxPoints)); err != nil || len(got) != EnvMaxPoints {
		t.Errorf("%d точек: len=%d err=%v, want все без ошибки", EnvMaxPoints, len(got), err)
	}
	if _, err := NormalizeEnvelope(mk(EnvMaxPoints + 1)); err == nil {
		t.Errorf("%d точек: want ошибку", EnvMaxPoints+1)
	}
}

// Условие: вход не мутируется (порядок, значения, невалидные точки — на месте).
func TestNormalizeEnvelopeDoesNotMutateInput(t *testing.T) {
	in := []EnvPoint{{T: 3, Db: 50}, {T: -1, Db: 0}, {T: 1, Db: math.NaN()}, {T: 1, Db: -100}, {T: 2, Db: 0}, {T: 3, Db: -2}}
	orig := append([]EnvPoint(nil), in...)
	got := normOK(t, in)
	if !sameEnv(in, orig) {
		t.Errorf("вход изменён: %v, было %v", in, orig)
	}
	// результат не делит память со входом
	if len(got) > 0 {
		got[0].Db = 999
		if !sameEnv(in, orig) {
			t.Errorf("результат делит память со входом: %v", in)
		}
	}
}

// --- граф через ffmpeg ---

// envRun — прогнать вход через EnvelopeGraph(pts), вернуть выход 16 кГц моно.
func envRun(t *testing.T, in string, pts []EnvPoint) []float32 {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.flac")
	if err := Run(in, out, EnvelopeGraph(pts), nil); err != nil {
		t.Fatalf("Run EnvelopeGraph(%v): %v", pts, err)
	}
	return decode(t, out)
}

// relDb — уровень выхода к уровню входа на [from,to], дБ.
func relDb(out, src []float32, from, to float64) float64 {
	return dbfs(segRMS(out, from, to)) - dbfs(segRMS(src, from, to))
}

// Контракт графа для Run: вход [0:a], выход [out].
func TestEnvelopeGraphLabels(t *testing.T) {
	g := EnvelopeGraph([]EnvPoint{{T: 1, Db: 0}, {T: 3, Db: -12}})
	for _, want := range []string{"[0:a]", "[out]"} {
		if !strings.Contains(g, want) {
			t.Errorf("%q нет в графе: %s", want, g)
		}
	}
}

// Кейс карточки: {1:0},{3:−12} на постоянном тоне — до 1 с исходный уровень,
// в 2 с ≈ −6 дБ (линейно в дБ; линейно по амплитуде дало бы ≈ −4), после 3 с ≈ −12;
// длина выхода = длине входа.
func TestEnvelopeGraphRampInDb(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.2*sin(2*PI*1000*t)", 5)
	out := envRun(t, in, []EnvPoint{{T: 1, Db: 0}, {T: 3, Db: -12}})

	if d := secs(out) - secs(src); math.Abs(d) > 0.02 {
		t.Errorf("длина выхода %.3f с, входа %.3f — want равны", secs(out), secs(src))
	}
	if d := relDb(out, src, 0.2, 0.8); math.Abs(d) > 0.5 {
		t.Errorf("до первой точки %+.2f дБ, want ≈ 0 (дБ первой точки)", d)
	}
	if d := relDb(out, src, 1.95, 2.05); math.Abs(d-(-6)) > 1 {
		t.Errorf("в 2 с %+.2f дБ, want −6 ± 1 (линейно в дБ)", d)
	}
	if d := relDb(out, src, 3.5, 4.5); math.Abs(d-(-12)) > 0.5 {
		t.Errorf("после последней точки %+.2f дБ, want −12 ± 0.5", d)
	}
}

// Условие: до первой точки — дБ первой (не 0): {2:−6},{4:0} — начало трека −6 дБ.
func TestEnvelopeGraphBeforeFirstPointUsesFirstDb(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.2*sin(2*PI*1000*t)", 5)
	out := envRun(t, in, []EnvPoint{{T: 2, Db: -6}, {T: 4, Db: 0}})
	if d := relDb(out, src, 0.2, 1.8); math.Abs(d-(-6)) > 0.5 {
		t.Errorf("до первой точки %+.2f дБ, want −6 ± 0.5", d)
	}
	if d := relDb(out, src, 2.95, 3.05); math.Abs(d-(-3)) > 1 {
		t.Errorf("в 3 с %+.2f дБ, want −3 ± 1", d)
	}
	if d := relDb(out, src, 4.2, 4.9); math.Abs(d) > 0.5 {
		t.Errorf("после последней точки %+.2f дБ, want ≈ 0", d)
	}
}

// Кейс карточки: одна точка +6 — весь трек +6 дБ, длина та же.
func TestEnvelopeGraphSinglePointWholeTrack(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.1*sin(2*PI*1000*t)", 5)
	out := envRun(t, in, []EnvPoint{{T: 2.5, Db: 6}})
	if d := secs(out) - secs(src); math.Abs(d) > 0.02 {
		t.Errorf("длина выхода %.3f с, входа %.3f — want равны", secs(out), secs(src))
	}
	for _, w := range [][2]float64{{0.1, 0.9}, {2, 3}, {4, 4.9}} {
		if d := relDb(out, src, w[0], w[1]); math.Abs(d-6) > 0.5 {
			t.Errorf("%.1f–%.1f с: %+.2f дБ, want +6 ± 0.5", w[0], w[1], d)
		}
	}
}

// Условие: без «пилы» — шаг громкости не грубее ~25 мс. На спаде 12 дБ за 2 с
// шаг 25 мс даёт ошибку ≤ 0.15 дБ; в каждом окне 20 мс уровень следует прямой
// в дБ с точностью 0.4 дБ (грубые ступени или пила вылезут за допуск).
func TestEnvelopeGraphSmoothRamp(t *testing.T) {
	needFFmpeg(t)
	in, src := genIn(t, "0.2*sin(2*PI*1000*t)", 4)
	out := envRun(t, in, []EnvPoint{{T: 1, Db: 0}, {T: 3, Db: -12}})
	bad := 0
	for a := 1.1; a+0.02 <= 2.9; a += 0.02 {
		mid := a + 0.01
		want := -12 * (mid - 1) / 2
		if d := relDb(out, src, a, a+0.02); math.Abs(d-want) > 0.4 {
			if bad < 5 {
				t.Errorf("окно %.2f–%.2f с: %+.2f дБ, want %+.2f ± 0.4", a, a+0.02, d, want)
			}
			bad++
		}
	}
	if bad > 0 {
		t.Errorf("окон вне прямой: %d", bad)
	}
}

// Ревью: настоящие треки — FLAC 44.1 кГц стерео, кадр декодера 4608 сэмплов
// (~104 мс); громкость «раз на кадр» давала ступени ~3 дБ на крутом спаде.
// Спад 30 дБ за 1 с: в каждом окне 10 мс уровень на прямой в дБ ± 1 дБ.
func TestEnvelopeGraphSmoothOnTrackFlac(t *testing.T) {
	needFFmpeg(t)
	in := filepath.Join(t.TempDir(), "in.flac")
	lavfi(t, "aevalsrc=exprs='0.2*sin(2*PI*1000*t)|0.2*sin(2*PI*1000*t)':d=3:s=44100", in)
	src := decode(t, in)
	out := envRun(t, in, []EnvPoint{{T: 1, Db: 0}, {T: 2, Db: -30}})
	bad := 0
	for a := 1.05; a+0.01 <= 1.95; a += 0.01 {
		want := -30 * (a + 0.005 - 1)
		if d := relDb(out, src, a, a+0.01); math.Abs(d-want) > 1 {
			if bad < 5 {
				t.Errorf("окно %.2f–%.2f с: %+.2f дБ, want %+.2f ± 1 (ступени громкости)", a, a+0.01, d, want)
			}
			bad++
		}
	}
	if bad > 0 {
		t.Errorf("окон вне прямой: %d", bad)
	}
}
