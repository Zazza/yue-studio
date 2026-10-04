package dsp

import (
	"fmt"
	"math"
	"strings"
)

// Время/глитч (реверс к отметке, статтер, остановка ленты) и лоуфай (винил).

// glitchEdge — короткий фейд на краях вырезанных кусков, с: без щелчков
const glitchEdge = 0.005

// muteWindow — выражение громкости: 0 в окне [a, b], 1 вне, края — рампы glitchEdge.
func muteWindow(a, b float64) string {
	return fmt.Sprintf("1-min(clip((t-%[1]g)/%[3]g,0,1),clip((%[2]g-t)/%[3]g,0,1))", a-glitchEdge, b+glitchEdge, glitchEdge)
}

var reverseParams = []Param{
	{ID: "start", Label: "отметка (реверс заканчивается здесь), с", Min: 0, Max: 600, Step: 0.05, Default: 120},
	{ID: "len", Label: "длина реверса, с", Min: 0.2, Max: 8, Step: 0.1, Default: 1.5},
	{ID: "src", Label: "что реверсировать: 0 — кусок до отметки, 1 — кусок после (реверс-тарелка)", Min: 0, Max: 1, Step: 1, Default: 1},
	{ID: "level", Label: "громкость реверса", Min: 0, Max: 1.5, Step: 0.05, Default: 0.8},
	{ID: "replace", Label: "0 — поверх звука, 1 — вместо", Min: 0, Max: 1, Step: 1, Default: 0},
}

// reverseGraph — в окне [start − len, start] звучит кусок задом наперёд:
// реверс-тарелка (кусок после отметки — удар тарелки «втягивается» к отметке)
// или реверс-хвост (тот же кусок до отметки). Длина трека не меняется.
func reverseGraph(p map[string]float64) string {
	start, n := p["start"], p["len"]
	a := math.Max(0, start-n)
	n = start - a
	if n <= 0 {
		return "[0:a]anull[out]"
	}
	src := a
	if p["src"] >= 0.5 {
		src = start
	}
	main := "anull"
	if p["replace"] >= 0.5 {
		main = fmt.Sprintf("asetnsamples=n=%d:p=0,volume='%s':eval=frame", gateSamples, muteWindow(a, start))
	}
	// apad до длины окна — кусок за концом трека не пустой (areverse и adelay
	// на пустом потоке роняют граф)
	return fmt.Sprintf("[0:a]asplit=2[rs_m0][rs_s];[rs_m0]%[1]s[rs_m];"+
		"[rs_s]atrim=start=%[2]g:duration=%[3]g,asetpts=PTS-STARTPTS,apad=whole_dur=%[3]g,atrim=duration=%[3]g,"+
		"areverse,afade=t=in:d=%[4]g,afade=t=out:st=%[5]g:d=%[4]g,volume=%[6]g,adelay=delays=%[7]g:all=1[rs_r];"+
		"[rs_m][rs_r]amix=inputs=2:duration=first:normalize=0[out]",
		main, src, n, glitchEdge, n-glitchEdge, p["level"], a*1000)
}

var stutterParams = []Param{
	{ID: "start", Label: "с какой секунды (на сильную долю)", Min: 0, Max: 600, Step: 0.005, Default: 120},
	{ID: "bpm", Label: "темп трека, BPM («найти сетку»)", Min: 40, Max: 240, Step: 0.5, Default: 120},
	{ID: "div", Label: "ударов на долю (2 — восьмые, 4 — шестнадцатые)", Min: 1, Max: 8, Step: 1, Default: 2},
	{ID: "count", Label: "сколько раз повторить кусок", Min: 2, Max: 16, Step: 1, Default: 4},
}

// stutterGraph — статтер: кусок длиной в удар сетки (60/(bpm·div)) с отметки
// повторяется count раз подряд вместо исходного звука; дальше трек идёт как
// был. Длина трека не меняется.
func stutterGraph(p map[string]float64) string {
	start := p["start"]
	period := 60 / (p["bpm"] * p["div"])
	count := int(math.Round(p["count"]))
	var b strings.Builder
	fmt.Fprintf(&b, "[0:a]asplit=2[st_m0][st_s];[st_m0]asetnsamples=n=%d:p=0,volume='%s':eval=frame[st_m];"+
		"[st_s]atrim=start=%[3]g:duration=%[4]g,asetpts=PTS-STARTPTS,apad=whole_dur=%[4]g,atrim=duration=%[4]g,"+
		"afade=t=in:d=%[5]g,afade=t=out:st=%[6]g:d=%[5]g,asplit=%[7]d",
		gateSamples, muteWindow(start, start+float64(count)*period), start, period, glitchEdge, period-glitchEdge, count)
	for k := 0; k < count; k++ {
		fmt.Fprintf(&b, "[st_c%d]", k)
	}
	mixIn := "[st_m]"
	for k := 0; k < count; k++ {
		fmt.Fprintf(&b, ";[st_c%[1]d]adelay=delays=%[2]g:all=1[st_p%[1]d]", k, (start+float64(k)*period)*1000)
		mixIn += fmt.Sprintf("[st_p%d]", k)
	}
	fmt.Fprintf(&b, ";%samix=inputs=%d:duration=first:normalize=0[out]", mixIn, count+1)
	return b.String()
}

var tapeStopParams = []Param{
	{ID: "start", Label: "с какой секунды тормозить", Min: 0, Max: 600, Step: 0.05, Default: 120},
	{ID: "dur", Label: "за сколько секунд остановиться", Min: 0.3, Max: 4, Step: 0.1, Default: 1.5},
}

const (
	tapeStopSlices = 16    // лесенка скоростей: asetrate держит одну скорость на кусок
	tapeStopRate   = 48000 // частота кусков торможения: asetrate нужна известная частота
)

// tapeStopGraph — остановка ленты: с отметки скорость (и высота вместе с
// ней — как у ленты) падает от 1 до 0 за dur секунд и звук гаснет; после окна
// трек идёт с того же места, где шёл бы без эффекта (длина не меняется). Куски
// лесенки переводятся в tapeStopRate: asetrate меняет скорость только при
// известной частоте, а частоту входа граф не знает.
func tapeStopGraph(p map[string]float64) string {
	start, dur := p["start"], p["dur"]
	step := dur / tapeStopSlices // длина куска на выходе
	var b strings.Builder
	fmt.Fprintf(&b, "[0:a]asplit=2[ts_m0][ts_s0];[ts_m0]asetnsamples=n=%d:p=0,volume='%s':eval=frame[ts_m];"+
		"[ts_s0]aresample=%d,asplit=%d", gateSamples, muteWindow(start, start+dur), tapeStopRate, tapeStopSlices)
	for i := 0; i < tapeStopSlices; i++ {
		fmt.Fprintf(&b, "[ts_s%d]", i+1)
	}
	mixIn := "[ts_m]"
	src := start // где в исходнике начинается кусок
	for i := 0; i < tapeStopSlices; i++ {
		r := 1 - (float64(i)+0.5)/tapeStopSlices // скорость куска
		gain := math.Pow(1-float64(i)/tapeStopSlices, 2)
		fmt.Fprintf(&b, ";[ts_s%[1]d]atrim=start=%[2]g:duration=%[3]g,asetpts=PTS-STARTPTS,"+
			"asetrate=%[4]g,aresample=%[5]d,apad=whole_dur=%[6]g,atrim=duration=%[6]g,"+
			"afade=t=in:d=0.002,afade=t=out:st=%[7]g:d=0.002,volume=%[8]g,adelay=delays=%[9]g:all=1[ts_p%[1]d]",
			i+1, src, r*step, tapeStopRate*r, tapeStopRate, step, step-0.002, gain, (start+float64(i)*step)*1000)
		mixIn += fmt.Sprintf("[ts_p%d]", i+1)
		src += r * step
	}
	fmt.Fprintf(&b, ";%samix=inputs=%d:duration=first:normalize=0[out]", mixIn, tapeStopSlices+1)
	return b.String()
}

var vinylParams = []Param{
	{ID: "crackle", Label: "треск", Min: 0, Max: 1, Step: 0.05, Default: 0.4},
	{ID: "hiss", Label: "шум", Min: 0, Max: 0.05, Step: 0.001, Default: 0.008},
	{ID: "cut", Label: "верх, кГц", Min: 6, Max: 20, Step: 0.5, Default: 14},
}

// vinylGraph — пластинка: редкие щелчки (одиночные отсчёты, в среднем
// crackle·60 в секунду) и тихий шум поверх звука, верх чуть закрыт. Шум
// строится aeval на копии входа — частота и длина входа, без подбора частоты.
func vinylGraph(p map[string]float64) string {
	expr := fmt.Sprintf(`if(lt(random(0)\,%[1]g/s)\,(random(1)*2-1)*%[2]g\,0)+(random(2)*2-1)*%[3]g`,
		p["crackle"]*60, 0.2+0.4*p["crackle"], p["hiss"])
	cut := p["cut"] * 1000
	return fmt.Sprintf("[0:a]asplit=2[vn_d0][vn_s];[vn_d0]lowpass=f=%[1]g[vn_d];"+
		"[vn_s]aeval=exprs='%[2]s',highpass=f=700,lowpass=f=%[1]g[vn_n];"+
		"[vn_d][vn_n]amix=inputs=2:duration=first:normalize=0[out]", cut, expr)
}
