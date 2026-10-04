package dsp

import (
	"fmt"
	"math"
	"strings"
)

// Динамика (нарастание/затухание, многополосный компрессор,
// ducking от барабанов) и высота (транспонирование, октавер).

var fadeParams = []Param{
	{ID: "in", Label: "нарастание с начала, с (0 — нет)", Min: 0, Max: 30, Step: 0.5, Default: 0},
	{ID: "start", Label: "затухание с секунды (0 — нет)", Min: 0, Max: 600, Step: 0.5, Default: 0},
	{ID: "out", Label: "длина затухания, с", Min: 0.5, Max: 30, Step: 0.5, Default: 8},
}

// fadeGraph — нарастание с начала трека и затухание с отметки start (длина
// трека графу неизвестна, отметку задаёт пользователь); после затухания —
// тишина. Кривая — квадрат доли: линейная громкость на слух «висит» в начале
// нарастания и обрывается в конце затухания.
func fadeGraph(p map[string]float64) string {
	in, start, out := p["in"], p["start"], p["out"]
	if in <= 0 && start <= 0 {
		return "[0:a]anull[out]"
	}
	gain := "1"
	if in > 0 {
		gain = fmt.Sprintf("pow(clip(t/%g,0,1),2)", in)
	}
	if start > 0 {
		gain = fmt.Sprintf("%s*pow(clip((%g-t)/%g,0,1),2)", gain, start+out, out)
	}
	return fmt.Sprintf("[0:a]asetnsamples=n=%d:p=0,volume='%s':eval=frame[out]", gateSamples, gain)
}

var multibandParams = []Param{
	{ID: "amount", Label: "плотность (0 — выкл)", Min: 0, Max: 1, Step: 0.05, Default: 0.5},
	{ID: "x1", Label: "раздел низ/середина, Гц", Min: 80, Max: 500, Step: 10, Default: 200},
	{ID: "x2", Label: "раздел середина/верх, Гц", Min: 1500, Max: 6000, Step: 100, Default: 3000},
}

// multibandGraph — три полосы (разделы Линквица–Райли 4-го порядка, как у
// ширины) и свой компрессор в каждой: громкий бас не прижимает голос и
// тарелки. Порог −30 дБ, степень 1…6 по amount; подъём на половину сжатия
// пика, лимитер ловит остальное. mcompand не годится: верх последней полосы
// должен быть ниже половины частоты дискретизации, а она графу неизвестна.
func multibandGraph(p map[string]float64) string {
	a := p["amount"]
	if a <= 0 {
		return "[0:a]anull[out]"
	}
	ratio := 1 + 5*a
	makeup := math.Pow(10, 15*(1-1/ratio)/20)
	comp := fmt.Sprintf("acompressor=threshold=0.0316:ratio=%g:attack=10:release=150:makeup=%g", ratio, makeup)
	x1, x2 := p["x1"], p["x2"]
	return fmt.Sprintf("[0:a]asplit=3[mb_l][mb_m][mb_h];"+
		"[mb_l]lowpass=f=%[1]g,lowpass=f=%[1]g,%[3]s[mb_lo];"+
		"[mb_m]highpass=f=%[1]g,highpass=f=%[1]g,lowpass=f=%[2]g,lowpass=f=%[2]g,%[3]s[mb_mid];"+
		"[mb_h]highpass=f=%[2]g,highpass=f=%[2]g,%[3]s[mb_hi];"+
		"[mb_lo][mb_mid][mb_hi]amix=inputs=3:duration=first:normalize=0,"+
		"alimiter=limit=0.97:attack=1:release=20:level=disabled:latency=1[out]", x1, x2, comp)
}

var duckingParams = []Param{
	{ID: "threshold", Label: "порог ключа (ниже — чаще приседает)", Min: 0.01, Max: 0.5, Step: 0.01, Default: 0.08},
	{ID: "ratio", Label: "глубина (степень сжатия)", Min: 1, Max: 20, Step: 0.5, Default: 6},
	{ID: "attack", Label: "атака, мс", Min: 1, Max: 50, Step: 1, Default: 5},
	{ID: "release", Label: "восстановление, мс", Min: 20, Max: 800, Step: 10, Default: 200},
}

// duckingKey — дорожка-ключ ducking: барабаны трека
const duckingKey = "drums"

// duckingGraph — сайдчейн-компрессор: дорожка (вход 0, обычно «прочее» или
// бас) приседает на каждом ударе ключа (вход 1 — барабаны) — «качающий» микс,
// бочка пробивается. sidechaincompress на конце теряет кусок дорожки (замер:
// 2.23 из 3 с, 178.6 из 180 с), поэтому дорожка и ключ продлены тишиной, а
// длину возвращает amix duration=first с беззвучной копией дорожки первой.
func duckingGraph(p map[string]float64) string {
	return fmt.Sprintf("[0:a]aformat=channel_layouts=stereo,asplit=2[dk_x0][dk_z0];"+
		"[dk_x0]apad=pad_dur=%[5]g[dk_x];[dk_z0]volume=0[dk_z];"+
		"[1:a]aformat=channel_layouts=stereo,apad[dk_k];"+
		"[dk_x][dk_k]sidechaincompress=threshold=%[1]g:ratio=%[2]g:attack=%[3]g:release=%[4]g[dk_c];"+
		"[dk_z][dk_c]amix=inputs=2:duration=first:normalize=0[out]",
		p["threshold"], p["ratio"], p["attack"], p["release"], duckingPad)
}

// duckingPad — запас тишины за концом дорожки для sidechaincompress, с
const duckingPad = 2.0

var pitchParams = []Param{
	{ID: "semis", Label: "полутоны (+ выше, − ниже)", Min: -12, Max: 12, Step: 1, Default: 1},
}

// pitchGraph — транспонирование без смены темпа (rubberband; форманты
// сохраняются — голос не «бурундучит»). Нет rubberband в сборке ffmpeg —
// ffmpeg падает с понятной ошибкой (docs/limitations.md).
func pitchGraph(p map[string]float64) string {
	s := math.Round(p["semis"])
	if s == 0 {
		return "[0:a]anull[out]"
	}
	return fmt.Sprintf("[0:a]rubberband=pitch=%.6f:formant=preserved[out]", math.Pow(2, s/12))
}

var octaverParams = []Param{
	{ID: "down", Label: "октава вниз", Min: 0, Max: 1, Step: 0.05, Default: 0.5},
	{ID: "up", Label: "октава вверх", Min: 0, Max: 1, Step: 0.05, Default: 0},
}

// octaverRB — настройки rubberband октавера: длинное окно, качество высоты,
// сглаженные атаки. С дефолтными тон на октаву ниже «размазывался» (замер на
// 440 Гц: компонента 220 Гц −23.6 дБ при −18.1 у чистого тона) — подслой
// звучал на 8 дБ тише крутилки. Атаки подслою не нужны.
const octaverRB = "window=long:pitchq=quality:transients=smooth"

// octaverGraph — к сухому подмешиваются копии на октаву ниже и выше (rubberband).
func octaverGraph(p map[string]float64) string {
	var branches []string
	if p["down"] > 0 {
		branches = append(branches, fmt.Sprintf("rubberband=pitch=0.5:"+octaverRB+",volume=%g", p["down"]))
	}
	if p["up"] > 0 {
		branches = append(branches, fmt.Sprintf("rubberband=pitch=2:"+octaverRB+",volume=%g", p["up"]))
	}
	if len(branches) == 0 {
		return "[0:a]anull[out]"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[0:a]asplit=%d[oc_d]", len(branches)+1)
	for i := range branches {
		fmt.Fprintf(&b, "[oc_s%d]", i)
	}
	mixIn := "[oc_d]"
	for i, br := range branches {
		fmt.Fprintf(&b, ";[oc_s%[1]d]%[2]s[oc_b%[1]d]", i, br)
		mixIn += fmt.Sprintf("[oc_b%d]", i)
	}
	fmt.Fprintf(&b, ";%samix=inputs=%d:duration=first:normalize=0[out]", mixIn, len(branches)+1)
	return b.String()
}
