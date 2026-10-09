package dsp

import (
	"fmt"
	"math"
)

// пределы места дорожки в стерео
const (
	PanMin, PanMax     = -1.0, 1.0
	WidthMin, WidthMax = 0.0, 2.0
)

// PlaceMatrix — место звука в стерео матрицей 2×2 {a, b, c, d}: L' = a·L + b·R, R' = c·L + d·R.
// Сначала ширина через середину/бока (0 — моно, 1 — как есть, 2 — бока вдвое громче), затем
// панорама складыванием канала: при p > 0 левый канал уходит в правый (θ = p·π/2), g = 1/√(1+sin θ)
// держит мощность коррелированного (моно) звука — у края он не громче, чем в центре. Матрица
// линейна: применённая к каждой правке дорожки и к разнице «исходная → M·исходная» даёт M·итог.
// pan 0, width 1 — ровно единичная.
func PlaceMatrix(pan, width float64) ([4]float64, error) {
	if math.IsNaN(pan) || pan < PanMin || pan > PanMax {
		return [4]float64{}, fmt.Errorf("панорама %g вне %g…%g", pan, PanMin, PanMax)
	}
	if math.IsNaN(width) || width < WidthMin || width > WidthMax {
		return [4]float64{}, fmt.Errorf("ширина %g вне %g…%g", width, WidthMin, WidthMax)
	}
	w := [4]float64{(1 + width) / 2, (1 - width) / 2, (1 - width) / 2, (1 + width) / 2}
	if pan == 0 {
		return w, nil
	}
	th := math.Abs(pan) * math.Pi / 2
	g := 1 / math.Sqrt(1+math.Sin(th))
	// p > 0: L' = g·cos θ·L, R' = g·(R + sin θ·L); p < 0 — зеркально
	p := [4]float64{g * math.Cos(th), 0, g * math.Sin(th), g}
	if pan < 0 {
		p = [4]float64{g, g * math.Sin(th), 0, g * math.Cos(th)}
	}
	return MulMatrix(p, w), nil
}

// MulMatrix — произведение матриц 2×2 a·b (сначала b, потом a).
func MulMatrix(a, b [4]float64) [4]float64 {
	return [4]float64{
		a[0]*b[0] + a[1]*b[2], a[0]*b[1] + a[1]*b[3],
		a[2]*b[0] + a[3]*b[2], a[2]*b[1] + a[3]*b[3],
	}
}
