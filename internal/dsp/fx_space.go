package dsp

import (
	"fmt"
	"math"
	"strings"
)

// Пространство: реверб, дилей в темп, стерео-ширина, удвоение Хааса — против
// «плоскости» нейромикса, где из пространства было только короткое эхо.

// reverbKind — характер реверба: из чего собран синтетический импульс.
type reverbKind struct {
	hp      float64 // срез низа хвоста, Гц (гулкий низ мутит микс)
	shape   string  // множитель затухающего шума в выражении aeval (t — секунды импульса)
	bandTop float64 // > 0 — верх полосы импульса, Гц (пружина — узкая полоса)
}

var reverbKinds = map[string]reverbKind{
	// комната — плотные ранние отражения в первые десятки мс
	"room": {hp: 120, shape: "(1+2*exp(-t/0.02))"},
	// зал — хвост нарастает ~30 мс (диффузия), потом долго гаснет
	"hall": {hp: 100, shape: "(1-exp(-t/0.03))"},
	// плейт — сразу плотный и яркий, без ранних отражений
	"plate": {hp: 180, shape: "1"},
	// пружина — «дребезг»: отражения каждые 33 мс в узкой полосе
	"spring": {hp: 250, shape: "(1+0.8*cos(2*PI*t/0.033))", bandTop: 4500},
}

func reverbParams(sizeMin, sizeMax, size, predelay, tone float64) []Param {
	return []Param{
		{ID: "size", Label: "хвост, с", Min: sizeMin, Max: sizeMax, Step: 0.1, Default: size},
		{ID: "predelay", Label: "предзадержка, мс", Min: 0, Max: 150, Step: 1, Default: predelay},
		{ID: "tone", Label: "верх хвоста, кГц", Min: 1, Max: 16, Step: 0.5, Default: tone},
		{ID: "wet", Label: "громкость хвоста (сухой не убавляется)", Min: 0, Max: 1, Step: 0.05, Default: 0.3},
		{ID: "width", Label: "ширина хвоста (0 — моно)", Min: 0, Max: 1, Step: 0.05, Default: 1},
	}
}

// reverbGraph — свёртка (afir) с синтетическим импульсом: затухающий шум
// −60 дБ за size секунд, свой у каждого канала (L и R — разные зёрна, иначе
// хвост моно). Импульс строится aeval на копии самого входа — у него частота
// и каналы входа: aevalsrc с фиксированной частотой заставлял ffmpeg
// пересэмплировать весь трек. irnorm=2 — нормировка по энергии: дефолтная
// (1) давала хвост на −46 дБ (замер на тоне 1 с). В свёртку идёт моно-сумма
// входа: afir сворачивает каналы по отдельности, и на стерео-входе хвост
// оставался стерео даже при ширине 0 (кросс-ревью: корреляция 0.02). Ширину
// хвоста задаёт только импульс. Сухой сигнал не трогается, хвост
// подмешивается с громкостью wet после предзадержки.
func reverbGraph(kind string) func(p map[string]float64) string {
	k := reverbKinds[kind]
	return func(p map[string]float64) string {
		size, w := p["size"], p["width"]
		env := strings.ReplaceAll(fmt.Sprintf("exp(-6.9*t/%g)*%s", size, k.shape), ",", `\,`)
		// правый канал — смесь своего шума (зерно st(1,7)) и левого: у каждого
		// канала свои переменные, поэтому random(0) в правом повторяет левый.
		// Ширина — в самом выражении: pan после фильтров импульса ломал срез
		// верха (замер: доля > 5 кГц в хвосте −21.7 дБ с pan, −53 без него)
		exprs := fmt.Sprintf(`(random(0)*2-1)*%[1]s|if(eq(n\,0)\,st(1\,7)\,0)\;(%[2]g*(random(1)*2-1)+%[3]g*(random(0)*2-1))*%[1]s`,
			env, w, 1-w)
		band := fmt.Sprintf("highpass=f=%g,lowpass=f=%g,lowpass=f=%g", k.hp, p["tone"]*1000, p["tone"]*1000)
		if k.bandTop > 0 {
			band += fmt.Sprintf(",lowpass=f=%g", k.bandTop)
		}
		return fmt.Sprintf("[0:a]aformat=channel_layouts=stereo,asplit=3[rv_d][rv_x][rv_s];"+
			"[rv_s]apad=whole_dur=%[1]g,atrim=duration=%[1]g,aeval=exprs='%[2]s':c=stereo,%[3]s[rv_ir];"+
			"[rv_x]pan=stereo|c0=0.5*c0+0.5*c1|c1=0.5*c0+0.5*c1[rv_m];[rv_m][rv_ir]afir=irnorm=2,adelay=delays=%[4]g:all=1,volume=%[5]g[rv_w];"+
			"[rv_d][rv_w]amix=inputs=2:duration=first:normalize=0[out]",
			size, exprs, band, p["predelay"], p["wet"])
	}
}

func reverbTail(p map[string]float64) float64 { return p["size"] + p["predelay"]/1000 }

var delayParams = []Param{
	{ID: "bpm", Label: "темп трека, BPM («найти сетку»)", Min: 40, Max: 240, Step: 0.5, Default: 120},
	{ID: "div", Label: "доля: 1 — 1/4, 2 — 1/8, 3 — 1/8 с точкой, 4 — 1/16, 5 — 1/4 с точкой", Min: 1, Max: 5, Step: 1, Default: 3},
	{ID: "feedback", Label: "обратная связь (громкость следующего повтора)", Min: 0, Max: 0.9, Step: 0.05, Default: 0.45},
	{ID: "pingpong", Label: "пинг-понг L/R (0 — нет, 1 — да)", Min: 0, Max: 1, Step: 1, Default: 1},
	{ID: "cut", Label: "верх повторов, кГц", Min: 1, Max: 16, Step: 0.5, Default: 6},
	{ID: "wet", Label: "громкость повторов", Min: 0, Max: 1, Step: 0.05, Default: 0.5},
}

// delayDivs — длина доли дилея в четвертях: 1/4, 1/8, 1/8 с точкой, 1/16, 1/4 с точкой.
var delayDivs = []float64{1, 0.5, 0.75, 0.25, 1.5}

const (
	delayMaxTaps = 16   // больше повторов не раскладываем
	delayFloor   = 0.01 // повтор тише 1 % (−40 дБ) уже не слышен
)

// delayTaps — длина задержки, с, и число повторов до порога delayFloor.
func delayTaps(p map[string]float64) (d float64, n int) {
	i := min(max(int(math.Round(p["div"]))-1, 0), len(delayDivs)-1)
	d = 60 / p["bpm"] * delayDivs[i]
	n = 1
	for g := p["wet"] * p["feedback"]; n < delayMaxTaps && g >= delayFloor; g *= p["feedback"] {
		n++
	}
	return d, n
}

// delayGraph — дилей в темп: обратной связи в графе ffmpeg нет (петли
// запрещены), поэтому повторы разложены явно — k-й через k·d с громкостью
// wet·feedback^(k−1). Повторы из моно-суммы, верх срезан; пинг-понг —
// нечётные в левом канале, чётные в правом.
func delayGraph(p map[string]float64) string {
	d, n := delayTaps(p)
	var b strings.Builder
	fmt.Fprintf(&b, "[0:a]aformat=channel_layouts=stereo,asplit=2[dl_d][dl_s];"+
		"[dl_s]pan=mono|c0=0.5*c0+0.5*c1,lowpass=f=%g,asplit=%d", p["cut"]*1000, n)
	for k := 1; k <= n; k++ {
		fmt.Fprintf(&b, "[dl_t%d]", k)
	}
	mixIn := "[dl_d]"
	for k := 1; k <= n; k++ {
		pan := "c0=c0|c1=c0"
		if p["pingpong"] >= 0.5 {
			pan = "c0=c0|c1=0*c0"
			if k%2 == 0 {
				pan = "c0=0*c0|c1=c0"
			}
		}
		fmt.Fprintf(&b, ";[dl_t%[1]d]adelay=delays=%[2]g:all=1,volume=%[3]g,pan=stereo|%[4]s[dl_p%[1]d]",
			k, float64(k)*d*1000, p["wet"]*math.Pow(p["feedback"], float64(k-1)), pan)
		mixIn += fmt.Sprintf("[dl_p%d]", k)
	}
	fmt.Fprintf(&b, ";%samix=inputs=%d:duration=first:normalize=0[out]", mixIn, n+1)
	return b.String()
}

func delayTail(p map[string]float64) float64 {
	d, n := delayTaps(p)
	return d * float64(n)
}

var widthParams = []Param{
	{ID: "width", Label: "ширина (0 — моно, 1 — как есть, 2 — шире)", Min: 0, Max: 2, Step: 0.05, Default: 1.4},
	{ID: "bass", Label: "моно ниже, Гц (0 — нет)", Min: 0, Max: 300, Step: 10, Default: 120},
}

// widthGraph — ширина через середину/бока (M/S): L' = ½(1+w)·L + ½(1−w)·R и
// зеркально; w = 0 — моно, 1 — без изменений, 2 — бока вдвое громче. Низ ниже
// bass сводится в моно (бас в стороны мутит и «гуляет»): разделение
// Линквица–Райли 4-го порядка (два прохода по 2 полюса) — сумма полос ровная.
func widthGraph(p map[string]float64) string {
	w := p["width"]
	ms := fmt.Sprintf("pan=stereo|c0=%[1]g*c0+%[2]g*c1|c1=%[2]g*c0+%[1]g*c1", (1+w)/2, (1-w)/2)
	if p["bass"] <= 0 {
		return "[0:a]aformat=channel_layouts=stereo," + ms + "[out]"
	}
	return fmt.Sprintf("[0:a]aformat=channel_layouts=stereo,asplit=2[wd_l][wd_h];"+
		"[wd_l]lowpass=f=%[1]g,lowpass=f=%[1]g,pan=stereo|c0=0.5*c0+0.5*c1|c1=0.5*c0+0.5*c1[wd_lo];"+
		"[wd_h]highpass=f=%[1]g,highpass=f=%[1]g,%[2]s[wd_hi];"+
		"[wd_lo][wd_hi]amix=inputs=2:duration=first:normalize=0[out]", p["bass"], ms)
}

var haasParams = []Param{
	{ID: "delay", Label: "задержка второго канала, мс", Min: 10, Max: 30, Step: 1, Default: 18},
	{ID: "side", Label: "какой канал позже (0 — правый, 1 — левый)", Min: 0, Max: 1, Step: 1, Default: 0},
	{ID: "mix", Label: "сухой/удвоенный (доля эффекта)", Min: 0, Max: 1, Step: 0.05, Default: 0.7},
}

// haasGraph — удвоение Хааса: моно-сумма в оба канала, один из них позже на
// 10–30 мс — ухо слышит один источник, но широкий («двойной» голос/гитара).
func haasGraph(p map[string]float64) string {
	ms := p["delay"]
	delays := fmt.Sprintf("0|%g", ms)
	if p["side"] >= 0.5 {
		delays = fmt.Sprintf("%g|0", ms)
	}
	return fmt.Sprintf("[0:a]aformat=channel_layouts=stereo,pan=stereo|c0=0.5*c0+0.5*c1|c1=0.5*c0+0.5*c1,"+
		"adelay=delays=%s[out]", delays)
}
