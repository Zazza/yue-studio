package dsp

// Placement — куда и с какой скоростью ставить партию в трек.
type Placement struct {
	StartSec float64 // момент трека, где звучит начало партии (после растяжения); может быть < 0
	Ratio    float64 // скорость партии (atempo)
	Score    float64 // надёжность совпадения; < AlignMinScore — подгонки не было
	Aligned  bool    // false — совпадение ненадёжно, стоит по плану (from − lead) без растяжения
}

const (
	alignRate    = 16000 // частота анализа огибающей
	alignTailSec = 1.0   // окно оригинала захватывает секунду после выделения
	// defaultBeatSec — доля при 120 BPM (дефолт диалекта), если темп не передан
	defaultBeatSec = 0.5
	// темп партии и трека — из одного Q: плана, и держат его оба (замер на
	// моторике 120: сетка колокольчика стабильна 16 с). Оценка скорости по
	// живому миксу шумит на ±1% — растяжение вносило бы ошибку, а не убирало.
	// Поэтому вклейка подгоняется только сдвигом; скорость меряет yue-align.
	insertMinRatio = 1
	insertMaxRatio = 1
)

// MeasureInsert — подгонка мини-рендера (partyPath) к окну [from, to] трека.
// lead — сколько плана партии звучит до from (такт контекста, sliceLeadSec):
// по плану начало партии стоит на from − lead. Трек и партия держат сетку
// плана до такта (замерено), поэтому поиск — в пределах четверти доли
// (beatSec/4) вокруг плана: на полудоле восьмые хэта/гитары давали уверенный
// ложный ответ (±240 мс, валидация в тасклоге задачи).
func MeasureInsert(trackPath, partyPath string, from, to, lead, beatSec float64) (Placement, error) {
	return measureInsert(trackPath, partyPath, from, to, lead, beatSec, insertMinRatio, insertMaxRatio)
}

// MeasureInsertTempo — то же с поиском скорости партии в [minRatio, maxRatio] (диагностика).
func MeasureInsertTempo(trackPath, partyPath string, from, to, lead, beatSec, minRatio, maxRatio float64) (Placement, error) {
	return measureInsert(trackPath, partyPath, from, to, lead, beatSec, minRatio, maxRatio)
}

func measureInsert(trackPath, partyPath string, from, to, lead, beatSec, minRatio, maxRatio float64) (Placement, error) {
	planned := from - lead
	fallback := Placement{StartSec: planned, Ratio: 1}
	refStart := max(0, planned-alignDefaultShift)
	ref, err := DecodeMono(trackPath, alignRate, refStart, to+alignTailSec-refStart)
	if err != nil {
		return fallback, err
	}
	// партия — только та часть, что ляжет в окно: рендер куска модель не
	// останавливает на длине плана, хвост (другой материал) сбивает атаки
	cand, err := DecodeMono(partyPath, alignRate, 0, to+alignTailSec-planned)
	if err != nil {
		return fallback, err
	}
	if beatSec <= 0 {
		beatSec = defaultBeatSec
	}
	a := AlignAudio(ref, cand, alignRate, AlignOpts{ExpectOffsetSec: planned - refStart,
		MaxShiftSec: beatSec / 4, MinRatio: minRatio, MaxRatio: maxRatio})
	fallback.Score = a.Score
	if a.Score < AlignMinScore {
		return fallback, nil
	}
	return Placement{StartSec: refStart + a.OffsetSec, Ratio: a.Ratio, Score: a.Score, Aligned: true}, nil
}

// PlaceInsert — вклейка партии так, чтобы звучал только кусок [from, to] трека.
func PlaceInsert(p Placement, from, to, gain float64) Insert {
	ratio := p.Ratio
	if ratio <= 0 {
		ratio = 1
	}
	in := Insert{AtSec: p.StartSec, Tempo: ratio, Gain: gain}
	if p.StartSec < from {
		// начало партии раньше окна: отрезаем исходные секунды до from
		in.SkipSec = (from - p.StartSec) * ratio
		in.AtSec = from
	}
	in.DurSec = max(0, to-in.AtSec)
	return in
}
