package dsp

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
)

// Тесты карточки internal-own-track, этап 10, условие 83 (тест-кейс ТК124): уровень громких мест файла.
// Написаны по карточке, без чтения реализации.
//
// Контракт: LoudLevelDb(path) (float64, error) — как rms_p95_db воркера: моно 22 050 Гц, RMS кадрами 2048
// с шагом 512, 95-й перцентиль, дБFS.
//
// Толкования (карточка не уточняет):
//   - моно — среднее каналов (как librosa.load(mono=True) воркера): одинаковые каналы дают тот же уровень;
//   - «5 % громко → около уровня громкого»: на границе громкого куска кадры частично громкие, поэтому
//     допуск −6…+0,5 дБ к уровню громкого (для librosa-подобного счёта ≈ −3,7); средняя мощность (≈ −13 к
//     громкому) и медиана (≈ −40) в него не попадают;
//   - 2 % громко — 95-й перцентиль приходится на тихие кадры: уровень заметно ниже громкого (не максимум);
//   - тишина — конечное число не выше −80 дБ, без ошибки; нет файла — ошибка.

// llGen — файл из выражения aevalsrc (exprs — каналы через |), частота rate, длина dur.
func llGen(t *testing.T, name, exprs string, dur float64, rate int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	lavfi(t, fmt.Sprintf("aevalsrc=exprs='%s':d=%g:s=%d", exprs, dur, rate), p)
	return p
}

func llLevel(t *testing.T, path string) float64 {
	t.Helper()
	got, err := LoudLevelDb(path)
	if err != nil {
		t.Fatalf("LoudLevelDb(%s): %v", filepath.Base(path), err)
	}
	if math.IsNaN(got) || math.IsInf(got, 0) {
		t.Fatalf("LoudLevelDb(%s) = %v, want конечное число", filepath.Base(path), got)
	}
	return got
}

func llSineDb(a float64) float64 { return 20 * math.Log10(a/math.Sqrt2) }

// ТК124: синус постоянной амплитуды A → 20·lg(A/√2) ± 0,3 дБ (разные частоты дискретизации и амплитуды).
func TestLoudLevelDbSine(t *testing.T) {
	needFFmpeg(t)
	for _, c := range []struct {
		a    float64
		rate int
	}{{0.5, 44100}, {0.1, 48000}, {0.02, 16000}} {
		t.Run(fmt.Sprintf("A=%g@%d", c.a, c.rate), func(t *testing.T) {
			p := llGen(t, "sine.flac", fmt.Sprintf("%g*sin(2*PI*440*t)", c.a), 5, c.rate)
			got, want := llLevel(t, p), llSineDb(c.a)
			if math.Abs(got-want) > 0.3 {
				t.Errorf("уровень синуса A=%g: %.2f дБ, want %.2f ±0,3", c.a, got, want)
			}
		})
	}
}

// ТК124 (моно): стерео с одинаковыми каналами — тот же уровень, что у одного канала.
func TestLoudLevelDbStereoSameAsMono(t *testing.T) {
	needFFmpeg(t)
	e := "0.2*sin(2*PI*330*t)"
	p := llGen(t, "stereo.wav", e+"|"+e, 4, 44100)
	got, want := llLevel(t, p), llSineDb(0.2)
	if math.Abs(got-want) > 0.3 {
		t.Errorf("стерео (каналы одинаковые): %.2f дБ, want %.2f ±0,3 (моно — среднее каналов)", got, want)
	}
}

// ТК124: 5 % громко (A), 95 % тихо (A/100) → около уровня громкого (95-й перцентиль, не среднее).
func TestLoudLevelDbLoudFivePercent(t *testing.T) {
	needFFmpeg(t)
	const a = 0.5
	// 20 с, громко 1 с (10–11 с)
	expr := fmt.Sprintf("(%g*between(t\\,10\\,11)+%g*(1-between(t\\,10\\,11)))*sin(2*PI*440*t)", a, a/100)
	p := llGen(t, "five.flac", expr, 20, 44100)
	got, loud := llLevel(t, p), llSineDb(a)
	if got < loud-6 || got > loud+0.5 {
		t.Errorf("5 %% громко: %.2f дБ, want около громкого %.2f (−6…+0,5); тихое %.2f", got, loud, llSineDb(a/100))
	}
}

// Условие 83 (95-й перцентиль, а не максимум): громко лишь 2 % → уровень заметно ниже громкого.
func TestLoudLevelDbNotPeak(t *testing.T) {
	needFFmpeg(t)
	const a = 0.5
	// 20 с, громко 0,4 с (10–10,4 с)
	expr := fmt.Sprintf("(%g*between(t\\,10\\,10.4)+%g*(1-between(t\\,10\\,10.4)))*sin(2*PI*440*t)", a, a/100)
	p := llGen(t, "two.flac", expr, 20, 44100)
	got, loud := llLevel(t, p), llSineDb(a)
	if got > loud-10 {
		t.Errorf("2 %% громко: %.2f дБ, want ниже громкого %.2f хотя бы на 10 дБ (перцентиль, не пик)", got, loud)
	}
}

// ТК124: тишина → очень низкое конечное значение, без NaN, паники и ошибки.
func TestLoudLevelDbSilence(t *testing.T) {
	needFFmpeg(t)
	p := llGen(t, "silence.flac", "0", 3, 44100)
	if got := llLevel(t, p); got > -80 {
		t.Errorf("тишина: %.2f дБ, want ≤ −80", got)
	}
}

// Краевой случай: файла нет → ошибка.
func TestLoudLevelDbMissingFile(t *testing.T) {
	needFFmpeg(t)
	if _, err := LoudLevelDb(filepath.Join(t.TempDir(), "nope.flac")); err == nil {
		t.Error("LoudLevelDb несуществующего файла: ошибки нет, want ошибка")
	}
}
