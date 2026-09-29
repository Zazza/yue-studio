package dsp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// Выравнивание вклейки по ритму оригинала. Модель исполняет мини-план
// «примерно»: темп плывёт на проценты, начало сдвинуто на такт контекста.
// Подгоняем по моментам атак (рост энергии кадра): перебор скорости партии
// и сдвига вокруг ожидаемого — ffmpeg + Go, без DSP-библиотек.

// Alignment — как подогнать партию (cand) под окно оригинала (ref).
type Alignment struct {
	OffsetSec float64 // задержка партии (уже после растяжения) относительно начала ref; может быть < 0
	Ratio     float64 // скорость проигрывания партии для atempo: 1.02 = на 2% быстрее
	Score     float64 // нормированная взвешенная доля атак партии, попавших на атаки ref, [0..1]
}

// AlignOpts — границы поиска (нули → дефолты).
type AlignOpts struct {
	ExpectOffsetSec    float64 // ожидаемый сдвиг, поиск вокруг него
	MaxShiftSec        float64 // радиус поиска сдвига; 0 → alignDefaultShift
	MinRatio, MaxRatio float64 // диапазон скорости; 0 → alignDefaultMinRatio..MaxRatio
}

const (
	// AlignMinScore — ниже совпадение ненадёжно: вклейка идёт по плану, UI предупреждает.
	// Абсолютные значения на живом миксе малы (колокольчик по бочке ~0.15 при
	// случайном фоне ~0.04), надёжность держит проверка согласованности половин.
	AlignMinScore = 0.1
	// MaxInsertGain — потолок гейна выравнивания (~+18 дБ): тихий рендер не раздуваем в шум.
	MaxInsertGain = 8.0
	silenceRMS    = 1e-6

	alignHopSec          = 0.005 // шаг огибающей: 5 мс — точность сдвига до интерполяции
	alignDefaultShift    = 2.0   // ±такт на 120 BPM
	alignDefaultMinRatio = 0.94
	alignDefaultMaxRatio = 1.06
	alignRatioStep       = 0.0025
	// штраф за удаление от ожидаемого сдвига (доля Score на весь радиус)
	alignDistPenalty  = 0.05
	alignRatioPenalty = 0.5 // −0.03 Score за 6% темпа
	alignKeepTempo    = 0.8 // скорость 1, если она даёт ≥ 80% лучшего совпадения
	alignCandPeakFrac = 0.25
	alignRefPeakFrac  = 0.5
	alignPeakGapSec   = 0.05
	alignHitSec       = 0.02 // допуск попадания атаки (критерий карточки — 30 мс)
	alignMinPeaks     = 4    // меньше атак в окне — совпадение не оценить
)

// RMS — среднеквадратичный уровень; пустой срез → 0.
func RMS(x []float32) float64 {
	if len(x) == 0 {
		return 0
	}
	var s float64
	for _, v := range x {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(x)))
}

// InsertGain — гейн партии: её RMS выравнивается по окну оригинала, сверху db
// (дБ относительно оригинала); потолок MaxInsertGain. Тишину партии не
// раздуваем (→ 0); в тихом окне оригинала (вступление из тишины) выравнивать
// не по чему — только db.
func InsertGain(refRMS, candRMS, db float64) float64 {
	if candRMS < silenceRMS {
		return 0
	}
	user := math.Pow(10, db/20)
	if refRMS < silenceRMS {
		return math.Min(user, MaxInsertGain)
	}
	return math.Min(refRMS/candRMS*user, MaxInsertGain)
}

// onsetEnvelope — положительный прирост лог-энергии кадров (hop = alignHopSec),
// центрированный: тишина и ровный шум дают ~0, атаки — пики.
func onsetEnvelope(x []float32, rate int) []float64 {
	hop := int(float64(rate) * alignHopSec)
	if hop < 1 || len(x) < 2*hop {
		return nil
	}
	n := len(x)/hop - 1
	le := make([]float64, n)
	for i := 0; i < n; i++ {
		var e float64
		for _, v := range x[i*hop : i*hop+2*hop] {
			e += float64(v) * float64(v)
		}
		le[i] = math.Log10(e/float64(2*hop) + 1e-8)
	}
	env := make([]float64, n)
	for i := 1; i < n; i++ {
		if d := le[i] - le[i-1]; d > 0 {
			env[i] = d
		}
	}
	var mean float64
	for _, v := range env {
		mean += v
	}
	mean /= float64(n)
	for i := range env {
		env[i] -= mean
	}
	return env
}

// peak — сильная атака: кадр и сила относительно максимума окна (0..1].
type peak struct{ at, w float64 }

// peakTimes — локальные максимумы огибающей выше frac от максимума,
// не чаще alignPeakGapSec. Сила нужна: на ровном груве моменты атак
// повторяются каждую долю, различают сдвиг акценты.
func peakTimes(env []float64, frac float64) []peak {
	mx := 0.0
	for _, v := range env {
		mx = max(mx, v)
	}
	if mx <= 0 {
		return nil
	}
	gap := alignPeakGapSec / alignHopSec
	var out []peak
	for i := 1; i+1 < len(env); i++ {
		if env[i] < frac*mx || env[i] < env[i-1] || env[i] < env[i+1] {
			continue
		}
		if n := len(out); n > 0 && float64(i)-out[n-1].at < gap {
			continue
		}
		out = append(out, peak{float64(i), env[i] / mx})
	}
	return out
}

// hitMask — «попадание» в атаку оригинала: её сила на самой атаке, линейно
// к 0 за alignHitSec.
func hitMask(peaks []peak, n int) []float64 {
	w := alignHitSec / alignHopSec
	m := make([]float64, n)
	for _, p := range peaks {
		for u := max(0, int(p.at-w)); u <= min(n-1, int(p.at+w)); u++ {
			m[u] = max(m[u], p.w*(1-math.Abs(float64(u)-p.at)/(w+1)))
		}
	}
	return m
}

// hitScore — взвешенная доля атак партии, попавших на атаки оригинала при
// скорости ratio и сдвиге lag (кадры). Считаются атаки внутри окна оригинала.
func hitScore(mask []float64, cand []peak, ratio float64, lag int) float64 {
	var s, tot float64
	n := 0
	for _, p := range cand {
		u := int(math.Round(p.at/ratio)) + lag
		if u < 0 || u >= len(mask) {
			continue
		}
		s += p.w * mask[u]
		tot += p.w
		n++
	}
	if n < alignMinPeaks {
		return 0
	}
	return s / tot
}

// AlignAudio ищет скорость и сдвиг партии: событие cand в момент t (секунды
// исходной партии) должно оказаться в ref в момент OffsetSec + t/Ratio.
// Сравниваются сильные атаки, а не огибающие целиком: у редкой партии
// (колокольчик, струнные) с плотным миксом мало общего по энергии, а по
// моментам нот — много. Score — взвешенная силой доля атак партии, попавших на атаки ref.
func AlignAudio(ref, cand []float32, rate int, o AlignOpts) Alignment {
	best := Alignment{OffsetSec: o.ExpectOffsetSec, Ratio: 1}
	if o.MaxShiftSec <= 0 {
		o.MaxShiftSec = alignDefaultShift
	}
	if o.MinRatio <= 0 {
		o.MinRatio = alignDefaultMinRatio
	}
	if o.MaxRatio <= 0 {
		o.MaxRatio = alignDefaultMaxRatio
	}
	re := onsetEnvelope(ref, rate)
	cp := peakTimes(onsetEnvelope(cand, rate), alignCandPeakFrac)
	if len(re) == 0 || len(cp) == 0 {
		return best
	}
	// в плотном миксе атака есть каждые ~60 мс — любая нота «попадёт»;
	// опора — только сильные удары (бочка/малый), на них держится грув
	mask := hitMask(peakTimes(re, alignRefPeakFrac), len(re))
	expect := int(math.Round(o.ExpectOffsetSec / alignHopSec))
	radius := int(math.Ceil(o.MaxShiftSec / alignHopSec))
	search := func(peaks []peak, ratios []float64) (float64, int, float64) {
		bestAdj, bestR, bestLag, bestSc := math.Inf(-1), 1.0, expect, 0.0
		for _, r := range ratios {
			for d := -radius; d <= radius; d++ {
				sc := hitScore(mask, peaks, r, expect+d)
				// на ровном груве совпадения повторяются каждую долю — берём
				// ближний к ожидаемому и скорость ближе к 1
				adj := sc - alignDistPenalty*math.Abs(float64(d))/float64(radius+1) - alignRatioPenalty*math.Abs(r-1)
				if adj > bestAdj {
					bestAdj, bestR, bestLag, bestSc = adj, r, expect+d, sc
				}
			}
		}
		return bestR, bestLag, bestSc
	}
	var ratios []float64
	for r := o.MinRatio; r <= o.MaxRatio+1e-9; r += alignRatioStep {
		ratios = append(ratios, r)
	}
	r, lag, sc := search(cp, ratios)
	if sc <= 0 {
		return best
	}
	// растяжение — только при явном выигрыше: на живом миксе оценка скорости
	// шумит на ±1%, а трек и партия играют по одному Q: плана
	if r != 1 {
		if _, l1, s1 := search(cp, []float64{1}); s1 >= alignKeepTempo*sc {
			r, lag, sc = 1, l1, s1
		}
	}
	// проверка согласованности: половины партии выравниваются независимо
	// (при найденной скорости) и должны указать на тот же сдвиг. Случайный
	// пик совпадений (шум, размытые струнные) половины не повторяют.
	mid := len(cp) / 2
	tol := int(math.Round(alignHitSec / alignHopSec))
	for _, half := range [][]peak{cp[:mid], cp[mid:]} {
		_, l, hs := search(half, []float64{r})
		if hs <= 0 || abs(l-lag) > tol {
			return Alignment{OffsetSec: o.ExpectOffsetSec, Ratio: 1}
		}
	}
	return Alignment{OffsetSec: float64(lag) * alignHopSec, Ratio: r, Score: sc}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// DecodeMono — окно файла в моно float32 с частотой rate (ffmpeg → f32le).
func DecodeMono(path string, rate int, fromSec, durSec float64) ([]float32, error) {
	args := []string{"-hide_banner", "-loglevel", "error"}
	if fromSec > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", fromSec))
	}
	if durSec > 0 {
		args = append(args, "-t", fmt.Sprintf("%.3f", durSec))
	}
	args = append(args, "-i", path, "-f", "f32le", "-ac", "1", "-ar", fmt.Sprint(rate), "pipe:1")
	cmd := ffmpegCmd(args)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg decode: %w: %s", err, stderr.String())
	}
	x := make([]float32, out.Len()/4)
	if err := binary.Read(&out, binary.LittleEndian, x); err != nil {
		return nil, err
	}
	return x, nil
}
