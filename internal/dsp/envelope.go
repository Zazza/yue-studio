package dsp

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// EnvPoint — точка линии громкости: секунда трека и громкость в дБ.
type EnvPoint struct {
	T  float64 `json:"t"`
	Db float64 `json:"db"`
}

// Пределы линии громкости: −30 дБ — «почти заглушить», +12 — потолок
// без клипа на типичном миксе; число точек ограничено длиной выражения ffmpeg.
const (
	EnvMinDb     = -30.0
	EnvMaxDb     = 12.0
	EnvMaxPoints = 200
	envFrame     = 1024 // сэмплов на пересчёт громкости
)

// NormalizeEnvelope — точки по порядку времени: невалидные (T < 0, NaN/Inf)
// отброшены, дБ зажаты в [EnvMinDb, EnvMaxDb], из точек с одинаковым T
// остаётся последняя по входу. Вход не меняется.
func NormalizeEnvelope(pts []EnvPoint) ([]EnvPoint, error) {
	bad := func(v float64) bool { return math.IsNaN(v) || math.IsInf(v, 0) }
	out := make([]EnvPoint, 0, len(pts))
	for _, p := range pts {
		if bad(p.T) || bad(p.Db) || p.T < 0 {
			continue
		}
		p.Db = math.Min(math.Max(p.Db, EnvMinDb), EnvMaxDb)
		out = append(out, p)
	}
	// устойчивая сортировка: среди равных T последняя по входу — последняя в срезе
	sort.SliceStable(out, func(i, j int) bool { return out[i].T < out[j].T })
	uniq := out[:0]
	for i, p := range out {
		if i+1 < len(out) && out[i+1].T == p.T {
			continue
		}
		uniq = append(uniq, p)
	}
	if len(uniq) == 0 {
		return nil, errors.New("линия громкости пуста: нужна хотя бы одна точка {t ≥ 0, db}")
	}
	if len(uniq) > EnvMaxPoints {
		return nil, fmt.Errorf("линия громкости: %d точек, максимум %d", len(uniq), EnvMaxPoints)
	}
	return uniq, nil
}

// EnvelopeGraph — filter_complex громкости по точкам (вход [0:a], выход [out]):
// между точками дБ меняются линейно, до первой точки — дБ первой, после
// последней — дБ последней. Точки — после NormalizeEnvelope. Выражение — сумма
// окон gte*lt, а не вложенные if: глубина разбора не растёт с числом точек.
// Громкость пересчитывается раз на кадр, поэтому кадры режутся по 1024 сэмпла
// (~23 мс на 44.1 кГц): кадр декодера FLAC — 4608 (~104 мс), на крутом спаде
// громкость шла ступенями ~3 дБ.
func EnvelopeGraph(pts []EnvPoint) string {
	if len(pts) == 0 {
		return "[0:a]anull[out]"
	}
	first, last := pts[0], pts[len(pts)-1]
	terms := []string{fmt.Sprintf("lt(t,%g)*(%g)", first.T, first.Db)}
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		terms = append(terms, fmt.Sprintf("gte(t,%g)*lt(t,%g)*(%g+(t-%g)*%g)",
			a.T, b.T, a.Db, a.T, (b.Db-a.Db)/(b.T-a.T)))
	}
	terms = append(terms, fmt.Sprintf("gte(t,%g)*(%g)", last.T, last.Db))
	return fmt.Sprintf("[0:a]asetnsamples=n=%d:p=0,volume='pow(10,(%s)/20)':eval=frame[out]", envFrame, strings.Join(terms, "+"))
}
