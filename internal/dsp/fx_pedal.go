package dsp

import (
	"fmt"
	"math"
)

// Гитарные педали: перегрузы, бустер, Uni-Vibe, кольцевой модулятор, гейт,
// компрессор, кабинет. В отличие от настоящих педалей они обрабатывают уже
// записанную гитару (дорожку demucs): перегруз поверх перегруза грязнее, чем
// на чистом звукоснимателе, — подбирать на слух превью.

// pedalCeiling — потолок пиков на выходе перегрузов: клиппинг с громкостью
// выше 0 дБ иначе хрипит уже в файле
const pedalCeiling = "alimiter=limit=0.97:attack=1:release=20:level=disabled:latency=1"

func driveParams(drive, driveMax, tone float64) []Param {
	return []Param{
		{ID: "drive", Label: "перегруз, дБ", Min: 0, Max: driveMax, Step: 1, Default: drive},
		{ID: "tone", Label: "тон (верх), кГц", Min: 1, Max: 10, Step: 0.1, Default: tone},
		{ID: "level", Label: "громкость, дБ", Min: -12, Max: 6, Step: 0.5, Default: 0},
	}
}

// odTSGraph — Tube Screamer: низ срезан до клиппинга (плотный, не гудит),
// горб середины ~720 Гц, мягкий tanh-клип с передискретизацией, тон — срез верха.
func odTSGraph(p map[string]float64) string {
	return fmt.Sprintf("[0:a]highpass=f=720:p=1,equalizer=f=720:t=q:w=0.8:g=6,volume=%[1]gdB,"+
		"asoftclip=type=tanh:oversample=4,lowpass=f=%.0[2]f,lowshelf=g=4:f=200,volume=%[3]gdB,%[4]s[out]",
		p["drive"], p["tone"]*1000, p["level"]-6, pedalCeiling)
}

var muffParams = []Param{
	{ID: "drive", Label: "сустейн (перегруз), дБ", Min: 0, Max: 50, Step: 1, Default: 30},
	{ID: "tone", Label: "тон (0 — бас, 1 — верх)", Min: 0, Max: 1, Step: 0.05, Default: 0.5},
	{ID: "level", Label: "громкость, дБ", Min: -12, Max: 6, Step: 0.5, Default: 0},
}

// muffGraph — Big Muff: два каскада клиппинга (огромный сустейн), провал
// середины ~1 кГц, тон — наклон между басом и верхом.
func muffGraph(p map[string]float64) string {
	tilt := (p["tone"] - 0.5) * 12
	return fmt.Sprintf("[0:a]highpass=f=80,volume=%[1]gdB,asoftclip=type=atan:oversample=4,volume=6dB,"+
		"asoftclip=type=atan:oversample=4,equalizer=f=1000:t=q:w=0.7:g=-8,"+
		"lowshelf=g=%[2]g:f=500,highshelf=g=%[3]g:f=2000,lowpass=f=6000,volume=%[4]gdB,%[5]s[out]",
		p["drive"], -tilt, tilt, p["level"]-8, pedalCeiling)
}

// ratGraph — RAT: жёсткий клип, после — фильтр (срез верха): резкий, «пилящий».
func ratGraph(p map[string]float64) string {
	return fmt.Sprintf("[0:a]highpass=f=60,volume=%[1]gdB,asoftclip=type=hard:oversample=4,"+
		"lowpass=f=%.0[2]f,lowpass=f=%.0[2]f,volume=%[3]gdB,%[4]s[out]",
		p["drive"], p["tone"]*1000, p["level"]-6, pedalCeiling)
}

var octFuzzParams = []Param{
	{ID: "drive", Label: "фузз, дБ", Min: 0, Max: 40, Step: 1, Default: 24},
	{ID: "octave", Label: "октава вверх (доля)", Min: 0, Max: 1, Step: 0.05, Default: 0.7},
	{ID: "tone", Label: "тон (верх), кГц", Min: 1, Max: 10, Step: 0.1, Default: 5},
	{ID: "level", Label: "громкость, дБ", Min: -12, Max: 6, Step: 0.5, Default: 0},
}

// octFuzzGraph — октавный фузз (Octavia): выпрямление волны |x| удваивает
// частоту — октава вверх; подмешивается к звуку и перегружается. Перед aeval
// раскладка закреплена стерео: с c=same на моно-входе ffmpeg 7.1 падал
// (segfault), когда дальше в цепочке стоял стерео-эффект (набор «Хендрикс»).
func octFuzzGraph(p map[string]float64) string {
	o := p["octave"]
	return fmt.Sprintf(`[0:a]aformat=channel_layouts=stereo,aeval=exprs='%[1]g*val(ch)+%[2]g*2*abs(val(ch))':c=same,highpass=f=40,highpass=f=40,`+
		"volume=%[3]gdB,asoftclip=type=tanh:oversample=4,lowpass=f=%.0[4]f,volume=%[5]gdB,%[6]s[out]",
		1-o, o, p["drive"], p["tone"]*1000, p["level"]-6, pedalCeiling)
}

var boostParams = []Param{
	{ID: "gain", Label: "громкость, дБ", Min: 0, Max: 20, Step: 0.5, Default: 8},
	{ID: "bright", Label: "яркость (верх), дБ", Min: 0, Max: 6, Step: 0.5, Default: 2},
}

// boostGraph — бустер: чистый подъём громкости и чуть верха; пики держит
// лимитер (громче он «дожимает» звук — как бустер в перегруженный усилитель).
func boostGraph(p map[string]float64) string {
	return fmt.Sprintf("[0:a]volume=%gdB,highshelf=g=%g:f=3000,%s[out]", p["gain"], p["bright"], pedalCeiling)
}

var univibeParams = []Param{
	{ID: "rate", Label: "скорость, Гц", Min: 0.5, Max: 8, Step: 0.1, Default: 2},
	{ID: "depth", Label: "глубина", Min: 0, Max: 1, Step: 0.05, Default: 0.6},
	{ID: "mix", Label: "сухой/обработанный (1 — вибрато, 0.5 — хорус)", Min: 0, Max: 1, Step: 0.05, Default: 0.6},
}

// univibeGraph — Uni-Vibe (Хендрикс, Гилмор): медленно качающиеся провалы
// фэйзера с синусной кривой плюс лёгкое качание громкости.
func univibeGraph(p map[string]float64) string {
	d := p["depth"]
	return fmt.Sprintf("[0:a]aphaser=in_gain=0.7:out_gain=0.85:delay=4:decay=%.2f:speed=%g:type=s,"+
		"tremolo=f=%[2]g:d=%.2[3]f[out]", 0.3+0.5*d, p["rate"], 0.01+0.15*d)
}

var ringmodParams = []Param{
	{ID: "freq", Label: "частота модуляции, Гц", Min: 20, Max: 2000, Step: 5, Default: 440},
	{ID: "mix", Label: "сухой/обработанный (доля эффекта)", Min: 0, Max: 1, Step: 0.05, Default: 0.8},
}

// ringmodGraph — кольцевой модулятор: звук умножается на синус — вместо нот
// суммы и разности частот, металлический «робот».
func ringmodGraph(p map[string]float64) string {
	return fmt.Sprintf(`[0:a]aformat=channel_layouts=stereo,aeval=exprs='val(ch)*sin(2*PI*%g*t)':c=same,volume=3dB[out]`, p["freq"])
}

var noiseGateParams = []Param{
	{ID: "threshold", Label: "порог, дБ (тише — глушится)", Min: -80, Max: -20, Step: 1, Default: -50},
	{ID: "release", Label: "закрытие, мс", Min: 10, Max: 500, Step: 10, Default: 100},
}

// noiseGateGraph — гейт от шума: между нотами (тише порога) — тишина,
// ноты целы. В отличие от Ритм-гейта — по громкости, а не в такт.
func noiseGateGraph(p map[string]float64) string {
	return fmt.Sprintf("[0:a]agate=threshold=%g:range=0.0005:ratio=20:attack=1:release=%g:knee=2[out]",
		math.Pow(10, p["threshold"]/20), p["release"])
}

var compPedalParams = []Param{
	{ID: "sustain", Label: "сустейн (сила сжатия)", Min: 0, Max: 1, Step: 0.05, Default: 0.5},
	{ID: "attack", Label: "атака, мс (больше — щелчок медиатора проходит)", Min: 1, Max: 50, Step: 1, Default: 10},
	{ID: "level", Label: "громкость, дБ", Min: -12, Max: 12, Step: 0.5, Default: 0},
}

// compPedalGraph — компрессор-педаль (кантри, фанк): тихие и громкие ноты
// ровнее, ноты тянутся дольше. Порог −10…−40 дБ и степень 2…10 по сустейну,
// подъём — половина сжатия на 0 дБ.
func compPedalGraph(p map[string]float64) string {
	a := p["sustain"]
	thrDb := -10 - 30*a
	ratio := 2 + 8*a
	makeup := math.Min(64, math.Pow(10, -thrDb*(1-1/ratio)*0.5/20))
	return fmt.Sprintf("[0:a]acompressor=threshold=%g:ratio=%g:attack=%g:release=200:makeup=%g:knee=4,"+
		"volume=%gdB,%s[out]", math.Pow(10, thrDb/20), ratio, p["attack"], makeup, p["level"], pedalCeiling)
}

var cabParams = []Param{
	{ID: "low", Label: "срез низа, Гц", Min: 50, Max: 200, Step: 5, Default: 90},
	{ID: "high", Label: "верх динамика, кГц", Min: 3, Max: 8, Step: 0.1, Default: 5},
	{ID: "mid", Label: "середина «коробки», дБ", Min: -6, Max: 6, Step: 0.5, Default: 0},
}

// cabGraph — кабинет (гитарный динамик): низ срезан, выше «верха динамика»
// резкий спад до −30 дБ за 2 кГц (FIR с нулевой фазой — динамик 12" не играет
// верх, именно он делает перегруз «гитарным», а не жужжащим).
func cabGraph(p map[string]float64) string {
	hi := p["high"] * 1000
	g := fmt.Sprintf(`if(lt(f\,%[1]g)\,0\,if(lt(f\,%[2]g)\,-30*(f-%[1]g)/2000\,-30))`, hi-1000, hi+1000)
	mid := ""
	if p["mid"] != 0 {
		mid = fmt.Sprintf(",equalizer=f=1500:t=q:w=1:g=%g", p["mid"])
	}
	return fmt.Sprintf("[0:a]highpass=f=%g,highpass=f=%g,firequalizer=gain='%s':zero_phase=on%s[out]",
		p["low"], p["low"], g, mid)
}
