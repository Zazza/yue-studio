// Package dsp — пост-обработка ffmpeg-цепочками на стороне ПК.
package dsp

import (
	"fmt"
	"strings"
)

// Param — крутилка цепочки (значение по умолчанию = Default).
type Param struct {
	ID      string  `json:"id"`
	Label   string  `json:"label"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Step    float64 `json:"step"`
	Default float64 `json:"default"`
}

// Chain — пресет эффектов: параметры + шаблон filter_complex.
type Chain struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Note   string  `json:"note"`
	Params []Param `json:"params"`

	graph func(p map[string]float64) string
}

var wallParams = []Param{
	{ID: "exciter", Label: "эксайтер (песок верхов)", Min: 0, Max: 6, Step: 0.1, Default: 2.5},
	{ID: "wall", Label: "стена (ниже = монолитнее)", Min: 0.15, Max: 0.9, Step: 0.05, Default: 0.5},
	{ID: "noise", Label: "шумовое полотно 0.5–9к", Min: 0, Max: 0.3, Step: 0.005, Default: 0.09},
}

// wallGraph — highpass → эксайтер → крашер → лимитер-стена → подмес band-шума → финал.
// makeup у acompressor линейный (1–64), поэтому стену делает лимитер, а не компрессор.
func wallGraph(p map[string]float64) string {
	return fmt.Sprintf(
		"[0:a]highpass=f=55,"+
			"aexciter=amount=%.2f:drive=9:freq=2200:ceil=12000,"+
			"acrusher=bits=12:mix=0.25,"+
			"alimiter=limit=%.2f:attack=1:release=10:level=disabled[a];"+
			"anoisesrc=color=white:amplitude=%.3f:seed=42,highpass=f=500,lowpass=f=9000[n];"+
			"[a][n]amix=inputs=2:duration=first:normalize=0,"+
			"alimiter=limit=0.97:attack=1:release=20:level=disabled[out]",
		p["exciter"], p["wall"], p["noise"])
}

var tapeParams = []Param{
	{ID: "wow", Label: "wow (завывание ленты)", Min: 0, Max: 0.3, Step: 0.01, Default: 0.1},
	{ID: "hiss", Label: "шипение", Min: 0, Max: 0.1, Step: 0.002, Default: 0.018},
	{ID: "cut", Label: "срез верхов, кГц", Min: 5, Max: 16, Step: 0.5, Default: 9.5},
}

func tapeGraph(p map[string]float64) string {
	return fmt.Sprintf(
		"[0:a]vibrato=f=0.7:d=%.2f,highpass=f=60,lowpass=f=%.0f,"+
			"alimiter=limit=0.9:attack=5:release=50:level=disabled[a];"+
			"anoisesrc=color=pink:amplitude=%.3f:seed=7,highpass=f=40,lowpass=f=8500[n];"+
			"[a][n]amix=inputs=2:duration=first:normalize=0,"+
			"alimiter=limit=0.95:attack=5:release=50:level=disabled[out]",
		p["wow"], p["cut"]*1000, p["hiss"]) // cut — кГц (раньше уходил как Гц: срез на 10 Гц глушил трек)
}

var aliveParams = []Param{
	{ID: "wobble", Label: "микро-детюн (живость высоты)", Min: 0, Max: 0.2, Step: 0.005, Default: 0.06},
	{ID: "breath", Label: "дыхание громкости", Min: 0, Max: 0.4, Step: 0.01, Default: 0.12},
	{ID: "pump", Label: "насос компрессора", Min: 0.5, Max: 5, Step: 0.1, Default: 2.5},
	{ID: "grit", Label: "зерно верхов", Min: 0, Max: 3, Step: 0.1, Default: 1.0},
}

func aliveGraph(p map[string]float64) string {
	return fmt.Sprintf(
		"[0:a]vibrato=f=0.35:d=%.2f,tremolo=f=0.12:d=%.2f,"+
			"acompressor=threshold=0.12:ratio=%.1f:attack=12:release=220:makeup=1.4,"+
			"aexciter=amount=%.2f:drive=6:freq=2200:ceil=11000,"+
			"alimiter=limit=0.95:attack=2:release=25:level=disabled[out]",
		p["wobble"], p["breath"], p["pump"], p["grit"])
}

var crescParams = []Param{
	{ID: "start", Label: "с какой секунды расти", Min: 0, Max: 600, Step: 1, Default: 120},
	{ID: "ramp", Label: "за сколько секунд", Min: 1, Max: 120, Step: 1, Default: 30},
	{ID: "db", Label: "на сколько дБ громче", Min: 0, Max: 6, Step: 0.5, Default: 3},
}

// crescGraph — громкость плавно растёт на db дБ за ramp секунд с отметки
// start и держится до конца; лимитер ловит пики, чтобы подъём не хрипел.
// Длина трека фильтру не известна — отметку задаёт пользователь.
func crescGraph(p map[string]float64) string {
	return fmt.Sprintf(
		"[0:a]volume='if(lt(t,%[1]g),1,if(lt(t,%[1]g+%[2]g),pow(10,%[3]g*(t-%[1]g)/%[2]g/20),pow(10,%[3]g/20)))':eval=frame,"+
			"alimiter=limit=0.95:attack=2:release=50:level=disabled[out]",
		p["start"], p["ramp"], p["db"])
}

var tempoFromParams = []Param{
	{ID: "start", Label: "с какой секунды быстрее", Min: 0, Max: 600, Step: 0.1, Default: 120},
	{ID: "factor", Label: "во сколько раз быстрее", Min: 1, Max: 1.3, Step: 0.01, Default: 1.1},
}

// tempoFromGraph — с отметки start трек ускоряется в factor раз без смены
// высоты (atempo). YuE смену темпа посреди плана не исполняет (замер: приём
// «темп +10%» — бочка осталась 120 BPM), поэтому «разогнать финал» — обработкой.
// Стык — ровно на отметке: ставить на начало такта/секции.
func tempoFromGraph(p map[string]float64) string {
	return fmt.Sprintf(
		"[0:a]asplit=2[a][b];[a]atrim=0:%[1]g,asetpts=PTS-STARTPTS[x];"+
			"[b]atrim=start=%[1]g,asetpts=PTS-STARTPTS,atempo=%[2]g[y];"+
			"[x][y]concat=n=2:v=0:a=1[out]",
		p["start"], p["factor"])
}

var gapParams = []Param{
	{ID: "start", Label: "с какой секунды тишина", Min: 0, Max: 600, Step: 0.1, Default: 120},
	{ID: "dur", Label: "длина паузы, с", Min: 0.2, Max: 8, Step: 0.1, Default: 2},
	{ID: "fade", Label: "мягкость краёв, мс", Min: 5, Max: 300, Step: 5, Default: 40},
}

// gapGraph — полная пауза (все инструменты и голос) на dur секунд с отметки
// start: «управление эмоцией» перед сбивкой. Края — короткие рампы, без щелчка.
// Длина трека не меняется: пауза заменяет звук, а не вставляется.
func gapGraph(p map[string]float64) string {
	return fmt.Sprintf(
		"[0:a]volume='if(lt(t,%[1]g-%[3]g),1,if(lt(t,%[1]g),(%[1]g-t)/%[3]g,"+
			"if(lt(t,%[1]g+%[2]g),0,if(lt(t,%[1]g+%[2]g+%[3]g),(t-%[1]g-%[2]g)/%[3]g,1))))':eval=frame[out]",
		p["start"], p["dur"], p["fade"]/1000)
}

var breatheParams = []Param{
	{ID: "amount", Label: "сила (0 — выкл, 1 — заметно)", Min: 0, Max: 1.5, Step: 0.05, Default: 1},
}

// breatheGraph — «разжать» кирпич нейромикса: мягкий экспандер — тихие места
// чуть тише, громкие как были. Пережатое не восстановить, но разброс громкости
// растёт (замер на #212: LRA 6.3 → 7.3 LU при amount 1) без качания.
func breatheGraph(p map[string]float64) string {
	a := p["amount"]
	return fmt.Sprintf(
		"[0:a]compand=attacks=0.02:decays=0.3:points=-90/-90|-40/%.1f|-22/%.1f|-8/-8|0/-0.5:soft-knee=6[out]",
		-40-6*a, -22-2*a)
}

var gritParams = []Param{
	{ID: "drive", Label: "перегруз", Min: 1, Max: 5, Step: 0.1, Default: 2.4},
	{ID: "crush", Label: "биткраш (ломкость)", Min: 0, Max: 0.6, Step: 0.05, Default: 0.35},
	{ID: "grit", Label: "песок верхов", Min: 0, Max: 4, Step: 0.1, Default: 2.2},
}

func gritGraph(p map[string]float64) string {
	return fmt.Sprintf(
		"[0:a]volume=%.2f,alimiter=limit=0.55:attack=1:release=8:level=disabled,"+
			"acrusher=bits=10:mix=%.2f,"+
			"aexciter=amount=%.2f:drive=8:freq=2400:ceil=12000,"+
			"alimiter=limit=0.94:attack=1:release=15:level=disabled[out]",
		p["drive"], p["crush"], p["grit"])
}

var warpParams = []Param{
	{ID: "wow", Label: "варп (завывание)", Min: 0, Max: 0.4, Step: 0.01, Default: 0.22},
	{ID: "flutter", Label: "флаттер (дрожь)", Min: 0, Max: 0.3, Step: 0.01, Default: 0.12},
	{ID: "cut", Label: "срез верхов, кГц", Min: 3, Max: 12, Step: 0.5, Default: 7},
}

func warpGraph(p map[string]float64) string {
	return fmt.Sprintf(
		"[0:a]vibrato=f=0.5:d=%.2f,vibrato=f=4.5:d=%.2f,lowpass=f=%.0f,"+
			"alimiter=limit=0.9:attack=5:release=60:level=disabled[out]",
		p["wow"], p["flutter"], p["cut"]*1000) // cut — кГц
}

var chains = []Chain{
	{
		ID: "wall", Name: "Стена/шум/песок",
		Note:   "Монолит громкости, песок верхов, нойз-полотно.",
		Params: wallParams, graph: wallGraph,
	},
	{
		ID: "wall-lite", Name: "Лёгкая стена",
		Note: "Та же цепочка мягче: чуть плотнее и грязнее, без разрушения.",
		Params: []Param{
			{ID: "exciter", Label: "эксайтер (песок верхов)", Min: 0, Max: 6, Step: 0.1, Default: 1.2},
			{ID: "wall", Label: "стена (ниже = монолитнее)", Min: 0.15, Max: 0.9, Step: 0.05, Default: 0.7},
			{ID: "noise", Label: "шумовое полотно 0.5–9к", Min: 0, Max: 0.3, Step: 0.005, Default: 0.05},
		},
		graph: wallGraph,
	},
	{
		ID: "tape", Name: "Кассета",
		Note:   "Wow/флаттер, срез верхов, розовое шипение — домашняя лента.",
		Params: tapeParams, graph: tapeGraph,
	},
	{
		ID: "alive", Name: "Живость",
		Note:   "Микро-детюн, дыхание громкости, насос компрессора — из пластмассы в «играли руками».",
		Params: aliveParams, graph: aliveGraph,
	},
	{
		ID: "grit", Name: "Грязь/перегруз",
		Note:   "Сатурация в лимитере, биткраш, песок — гараж и ламповый хрип.",
		Params: gritParams, graph: gritGraph,
	},
	{
		ID: "breathe", Name: "Дыхание",
		Note:   "Разжать «кирпич» нейромикса: тихие места чуть тише, громкие как были — трек меньше утомляет.",
		Params: breatheParams, graph: breatheGraph,
	},
	{
		ID: "gap", Name: "Тишина",
		Note:   "Полная пауза на пару секунд с отметки — затишье перед сбивкой/припевом; края мягкие.",
		Params: gapParams, graph: gapGraph,
	},
	{
		ID: "tempo-from", Name: "Ускорить с отметки",
		Note:   "С выбранной секунды трек быстрее без смены высоты — разогнать финал (модель смену темпа в плане не исполняет).",
		Params: tempoFromParams, graph: tempoFromGraph,
	},
	{
		ID: "cresc", Name: "Громкость к концу",
		Note:   "Плавный подъём громкости с выбранной секунды — финал звучит крупнее; лимитер держит пики.",
		Params: crescParams, graph: crescGraph,
	},
	{
		ID: "warp", Name: "Варп-лента",
		Note:   "Глубокое завывание и дрожь, глухой верх — плёночный брак как приём.",
		Params: warpParams, graph: warpGraph,
	},
}

// All — все пресеты (для UI).
func All() []Chain {
	out := make([]Chain, len(chains))
	copy(out, chains)
	return out
}

func ByID(id string) *Chain {
	for i := range chains {
		if chains[i].ID == id {
			return &chains[i]
		}
	}
	return nil
}

// FilterGraph строит filter_complex: недостающие параметры берутся
// по умолчанию, значения зажимаются в диапазон крутилки.
func (c *Chain) FilterGraph(params map[string]float64) string {
	p := make(map[string]float64, len(c.Params))
	for _, prm := range c.Params {
		v, ok := params[prm.ID]
		if !ok {
			v = prm.Default
		}
		if v < prm.Min {
			v = prm.Min
		}
		if v > prm.Max {
			v = prm.Max
		}
		p[prm.ID] = v
	}
	return withFrom(c.graph(p), p[fromParamID])
}

// fromParamID — общий параметр «с какой секунды»: эффект включается с
// отметки (например, «жёстче с третьего припева»), до неё трек нетронут.
// Цепочки со своей отметкой (параметр start) его не получают.
const (
	fromParamID = "from"
	fromXfade   = 0.1 // переход на стыке, с — без щелчка
)

var fromParam = Param{ID: fromParamID, Label: "с какой секунды (0 — весь трек)", Min: 0, Max: 600, Step: 0.5, Default: 0}

func init() {
	for i := range chains {
		hasStart := false
		for _, prm := range chains[i].Params {
			hasStart = hasStart || prm.ID == "start"
		}
		if !hasStart {
			chains[i].Params = append(chains[i].Params, fromParam)
		}
	}
}

// withFrom — граф цепочки (вход [0:a], выход [out]) звучит только с отметки
// from: цепочка считается по всему треку, а на отметке сухой сигнал сменяется
// обработанным (линейный переход fromXfade по времени). Без обрезки: отметка за
// концом трека (или за концом фрагмента превью) оставляет звук нетронутым, а не
// роняет ffmpeg пустой веткой. Длина — по сухому сигналу.
func withFrom(graph string, from float64) string {
	if from <= 0 {
		return graph
	}
	half := fromXfade / 2
	wet := strings.Replace(graph, "[0:a]", "[from_b]", 1)
	wet = strings.TrimSuffix(wet, "[out]") + "[from_wet0]"
	return fmt.Sprintf("[0:a]asplit=2[from_dry0][from_b];%[1]s;"+
		"[from_dry0]volume='clip((%[2]g-t)/%[4]g,0,1)':eval=frame[from_dry];"+
		"[from_wet0]volume='clip((t-%[3]g)/%[4]g,0,1)':eval=frame[from_wet];"+
		"[from_dry][from_wet]amix=inputs=2:duration=first:normalize=0[out]",
		wet, from+half, from-half, fromXfade)
}

// Defaults — карта параметров по умолчанию (для UI).
func (c *Chain) Defaults() map[string]float64 {
	m := make(map[string]float64, len(c.Params))
	for _, p := range c.Params {
		m[p.ID] = p.Default
	}
	return m
}

// Span — фрагмент файла для Run: со startSec и длиной durSec (nil = весь файл).
// Для превью цепочки: секунды вместо всего трека.
type Span struct {
	StartSec float64
	DurSec   float64
}

// Run прогоняет файл через filter_complex (ffmpeg на ПК).
func Run(inPath, outPath, filterGraph string, span *Span) error {
	args := []string{"-y", "-hide_banner", "-loglevel", "error"}
	if span != nil {
		args = append(args, "-ss", fmt.Sprintf("%.1f", span.StartSec),
			"-t", fmt.Sprintf("%.1f", span.DurSec))
	}
	args = append(args, "-i", inPath, "-filter_complex", filterGraph, "-map", "[out]", outPath)
	cmd := ffmpegCmd(args)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		out := stderr.String()
		if len(out) > 500 {
			out = out[len(out)-500:]
		}
		return fmt.Errorf("ffmpeg: %v: %s", err, out)
	}
	return nil
}
