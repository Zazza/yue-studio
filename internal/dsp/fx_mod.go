package dsp

import (
	"fmt"
	"math"
	"strings"
)

// Модуляция (хорус, фленжер, фэйзер, тремоло) и фильтры (эквалайзер, свип,
// авто-вау).

var chorusParams = []Param{
	{ID: "depth", Label: "глубина, мс", Min: 1, Max: 10, Step: 0.5, Default: 3},
	{ID: "rate", Label: "скорость, Гц", Min: 0.1, Max: 3, Step: 0.05, Default: 0.6},
	{ID: "mix", Label: "сухой/обработанный (доля эффекта)", Min: 0, Max: 1, Step: 0.05, Default: 0.5},
}

// chorusGraph — три голоса хоруса с разной задержкой и скоростью: один
// голос звучит как вибрато, несколько — как «несколько исполнителей».
func chorusGraph(p map[string]float64) string {
	r, d := p["rate"], p["depth"]
	return fmt.Sprintf("[0:a]chorus=0.7:0.9:20|27|34:0.5|0.4|0.35:%g|%g|%g:%g|%g|%g[out]",
		r, r*1.3, r*0.7, d, d, d)
}

var flangerParams = []Param{
	{ID: "delay", Label: "базовая задержка, мс", Min: 0, Max: 10, Step: 0.5, Default: 2},
	{ID: "depth", Label: "глубина, мс", Min: 0, Max: 10, Step: 0.5, Default: 2},
	{ID: "rate", Label: "скорость, Гц", Min: 0.1, Max: 2, Step: 0.05, Default: 0.3},
	{ID: "regen", Label: "обратная связь, %", Min: -95, Max: 95, Step: 5, Default: 30},
	{ID: "mix", Label: "сухой/обработанный (доля эффекта)", Min: 0, Max: 1, Step: 0.05, Default: 0.5},
}

func flangerGraph(p map[string]float64) string {
	return fmt.Sprintf("[0:a]flanger=delay=%g:depth=%g:regen=%g:speed=%g[out]",
		p["delay"], p["depth"], p["regen"], p["rate"])
}

var phaserParams = []Param{
	{ID: "rate", Label: "скорость, Гц", Min: 0.1, Max: 2, Step: 0.05, Default: 0.5},
	{ID: "depth", Label: "глубина", Min: 0.1, Max: 0.9, Step: 0.05, Default: 0.5},
	{ID: "mix", Label: "сухой/обработанный (доля эффекта)", Min: 0, Max: 1, Step: 0.05, Default: 0.6},
}

func phaserGraph(p map[string]float64) string {
	return fmt.Sprintf("[0:a]aphaser=in_gain=0.6:out_gain=0.9:delay=3:decay=%g:speed=%g:type=t[out]",
		p["depth"], p["rate"])
}

var tremoloParams = []Param{
	{ID: "rate", Label: "скорость, Гц", Min: 0.5, Max: 20, Step: 0.1, Default: 5},
	{ID: "depth", Label: "глубина (1 — до тишины)", Min: 0, Max: 1, Step: 0.05, Default: 0.5},
}

// tremoloGraph — плавное синусное качание громкости (в отличие от Ритм-гейта
// без краёв): громкость между 1−depth и 1.
func tremoloGraph(p map[string]float64) string {
	if p["depth"] <= 0 {
		return "[0:a]anull[out]"
	}
	return fmt.Sprintf("[0:a]tremolo=f=%g:d=%g[out]", p["rate"], p["depth"])
}

var eqParams = []Param{
	{ID: "low", Label: "низ, дБ", Min: -12, Max: 12, Step: 0.5, Default: 0},
	{ID: "lowf", Label: "низ: частота полки, Гц", Min: 40, Max: 400, Step: 5, Default: 120},
	{ID: "mid", Label: "середина, дБ", Min: -12, Max: 12, Step: 0.5, Default: 0},
	{ID: "midf", Label: "середина: частота, Гц", Min: 200, Max: 8000, Step: 10, Default: 1000},
	{ID: "midq", Label: "середина: добротность (больше — уже)", Min: 0.3, Max: 4, Step: 0.1, Default: 1},
	{ID: "high", Label: "верх, дБ", Min: -12, Max: 12, Step: 0.5, Default: 0},
	{ID: "highf", Label: "верх: частота полки, Гц", Min: 2000, Max: 16000, Step: 100, Default: 8000},
}

// eqGraph — полки низа/верха и колокол середины; полоса с 0 дБ не ставится
// вовсе, все нули — звук без изменений. Наклон полок 1 (самый крутой без
// выброса): с дефолтным 0.5 полка −6 дБ на 8 кГц давала на 12 кГц лишь −4.6 дБ.
func eqGraph(p map[string]float64) string {
	var f []string
	if p["low"] != 0 {
		f = append(f, fmt.Sprintf("lowshelf=g=%g:f=%g:t=s:w=1", p["low"], p["lowf"]))
	}
	if p["mid"] != 0 {
		f = append(f, fmt.Sprintf("equalizer=f=%g:t=q:w=%g:g=%g", p["midf"], p["midq"], p["mid"]))
	}
	if p["high"] != 0 {
		f = append(f, fmt.Sprintf("highshelf=g=%g:f=%g:t=s:w=1", p["high"], p["highf"]))
	}
	if len(f) == 0 {
		return "[0:a]anull[out]"
	}
	return "[0:a]" + strings.Join(f, ",") + "[out]"
}

var sweepParams = []Param{
	{ID: "start", Label: "с какой секунды", Min: 0, Max: 600, Step: 0.1, Default: 120},
	{ID: "dur", Label: "за сколько секунд", Min: 0.5, Max: 60, Step: 0.5, Default: 8},
	{ID: "type", Label: "0 — срез верха (НЧ-фильтр), 1 — срез низа (ВЧ-фильтр)", Min: 0, Max: 1, Step: 1, Default: 1},
	{ID: "f0", Label: "частота в начале, Гц", Min: 20, Max: 20000, Step: 10, Default: 20},
	{ID: "f1", Label: "частота в конце, Гц", Min: 20, Max: 20000, Step: 10, Default: 2000},
	{ID: "hold", Label: "после свипа: 0 — вернуть сухой звук, 1 — держать конечную частоту", Min: 0, Max: 1, Step: 1, Default: 0},
}

const (
	sweepStep     = 0.02 // шаг команд свипа, с — ступенек частоты не слышно
	sweepMaxSteps = 400  // на длинном свипе шаг растёт: строка команд не раздувается
	sweepXfade    = 0.05 // переход сухой↔фильтр на краях окна, с
)

// sweepGraph — свип фильтра с отметки: частота среза идёт от f0 к f1 за dur
// секунд по экспоненте (равномерно на слух). У фильтров ffmpeg нет частоты
// от времени — частоту меняет asendcmd командами с шагом sweepStep. Фильтр
// звучит только в окне свипа (с hold — и после), вне окна звук сухой.
// Типично — «разгон перед припевом»: срез низа 20 → 2000 Гц и сухой удар.
func sweepGraph(p map[string]float64) string {
	start, dur, f0, f1 := p["start"], p["dur"], p["f0"], p["f1"]
	kind := "lowpass"
	if p["type"] >= 0.5 {
		kind = "highpass"
	}
	n := min(sweepMaxSteps, int(math.Ceil(dur/sweepStep)))
	var cmds []string
	for i := 0; i <= n; i++ {
		x := float64(i) / float64(n)
		f := f0 * math.Pow(f1/f0, x)
		cmds = append(cmds, fmt.Sprintf(`%.3f %[2]s@sw1 f %.0[3]f\, %[2]s@sw2 f %.0[3]f`, start+x*dur, kind, f))
	}
	half := sweepXfade / 2
	gate := fmt.Sprintf("clip((t-%g)/%g,0,1)", start-half, sweepXfade)
	if p["hold"] < 0.5 {
		gate = fmt.Sprintf("min(%s,clip((%g-t)/%g,0,1))", gate, start+dur+half, sweepXfade)
	}
	return fmt.Sprintf("[0:a]asplit=2[sw_d0][sw_w0];"+
		"[sw_w0]asendcmd=c='%[1]s',%[2]s@sw1=f=%.0[3]f,%[2]s@sw2=f=%.0[3]f,"+
		"asetnsamples=n=%[5]d:p=0,volume='%[4]s':eval=frame[sw_w];"+
		"[sw_d0]asetnsamples=n=%[5]d:p=0,volume='1-%[4]s':eval=frame[sw_d];"+
		"[sw_d][sw_w]amix=inputs=2:duration=first:normalize=0[out]",
		strings.Join(cmds, `\;`), kind, f0, gate, gateSamples)
}

var autowahParams = []Param{
	{ID: "rate", Label: "скорость качания, Гц", Min: 0.2, Max: 8, Step: 0.1, Default: 2},
	{ID: "lo", Label: "нижняя точка, Гц", Min: 300, Max: 1000, Step: 10, Default: 400},
	{ID: "hi", Label: "верхняя точка, Гц", Min: 1000, Max: 4000, Step: 50, Default: 2200},
	{ID: "mix", Label: "сухой/обработанный (доля эффекта)", Min: 0, Max: 1, Step: 0.05, Default: 0.8},
}

// autowahBands — сколько полосовых фильтров в «гребёнке» авто-вау
const autowahBands = 6

// autowahGraph — авто-вау качанием (LFO): частоту полосового фильтра от
// времени ffmpeg не меняет, поэтому стоит гребёнка из autowahBands полос
// lo…hi (геометрически), и громкость каждой — треугольное окно вокруг
// текущего положения «педали» pos(t) = (N−1)·(½ − ½·cos 2π·rate·t): соседние
// полосы перетекают друг в друга. Слежения за громкостью (настоящего
// «авто») нет — в ffmpeg нет такого фильтра.
func autowahGraph(p map[string]float64) string {
	pos := fmt.Sprintf("%d*(0.5-0.5*cos(2*PI*%g*t))", autowahBands-1, p["rate"])
	var b strings.Builder
	fmt.Fprintf(&b, "[0:a]asetnsamples=n=%d:p=0,asplit=%d", gateSamples, autowahBands)
	for k := 0; k < autowahBands; k++ {
		fmt.Fprintf(&b, "[aw_s%d]", k)
	}
	mixIn := ""
	for k := 0; k < autowahBands; k++ {
		f := p["lo"] * math.Pow(p["hi"]/p["lo"], float64(k)/(autowahBands-1))
		// ×2 — полосовой фильтр с q 2.5 срезает почти всё, вау тише сухого
		fmt.Fprintf(&b, ";[aw_s%[1]d]bandpass=f=%.0[2]f:t=q:w=2.5,volume='2*max(0,1-abs(%[3]s-%[1]d))':eval=frame[aw_b%[1]d]",
			k, f, pos)
		mixIn += fmt.Sprintf("[aw_b%d]", k)
	}
	fmt.Fprintf(&b, ";%samix=inputs=%d:duration=first:normalize=0[out]", mixIn, autowahBands)
	return b.String()
}
