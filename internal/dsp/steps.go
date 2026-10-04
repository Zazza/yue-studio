package dsp

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Step — одна педаль в цепочке: цепочка, её крутилки и выключатель.
type Step struct {
	Chain  string             `json:"chain"`
	Params map[string]float64 `json:"params,omitempty"`
	Off    bool               `json:"off,omitempty"`
}

// graphLabel — метка потока в графе ffmpeg: [0:a], [out], [rv_d]…
var graphLabel = regexp.MustCompile(`\[([A-Za-z0-9_:]+)\]`)

// StepsGraph — включённые шаги по порядку, как педали на полу: выход шага —
// вход следующего (а не сумма разниц от исходника, как у отдельных эффектов на
// дорожку). Графы цепочек склеиваются в один filter_complex: внутренние метки
// каждого шага получают префикс s<i>_, поэтому одна цепочка может стоять
// дважды. Хвост — сумма хвостов шагов. Цепочка с ключом (ducking) в
// последовательность не берётся: второго входа у педали нет.
func StepsGraph(steps []Step) (graph string, tail float64, err error) {
	var parts []string
	n := 0
	for _, s := range steps {
		if s.Off {
			continue
		}
		c := ByID(s.Chain)
		if c == nil {
			return "", 0, fmt.Errorf("неизвестный эффект %q", s.Chain)
		}
		if c.Key != "" {
			return "", 0, fmt.Errorf("эффект %q с дорожкой-ключом не ставится в цепочку педалей", c.Name)
		}
		in := "[0:a]"
		if n > 0 {
			in = fmt.Sprintf("[s%d_out]", n-1)
		}
		pre := fmt.Sprintf("s%d_", n)
		g := graphLabel.ReplaceAllStringFunc(c.FilterGraph(s.Params), func(m string) string {
			switch name := m[1 : len(m)-1]; name {
			case "0:a":
				return in
			case "out":
				return "[" + pre + "out]"
			default:
				return "[" + pre + name + "]"
			}
		})
		parts = append(parts, g)
		tail += c.TailSec(s.Params)
		n++
	}
	if n == 0 {
		return "", 0, errors.New("нет включённых эффектов")
	}
	// выход последнего шага — [out] всего графа
	last := fmt.Sprintf("[s%d_out]", n-1)
	parts[n-1] = strings.TrimSuffix(parts[n-1], last) + "[out]"
	return strings.Join(parts, ";"), tail, nil
}

// StepsMatch — выравнивать ли громкость обработанной дорожки по исходной: в
// цепочке есть голосовая примочка или перегруз (match). extraDb — поправка
// сверху: сумма крутилок «громкость» (level) у перегрузов — после выравнивания
// внутри графа она бы потерялась.
func StepsMatch(steps []Step) (match bool, extraDb float64) {
	for _, s := range steps {
		c := ByID(s.Chain)
		if s.Off || c == nil || (!c.Voice && !c.match) {
			continue
		}
		match = true
		if c.match {
			extraDb += c.values(s.Params)["level"]
		}
	}
	return match, extraDb
}

// timedParams — крутилки с секундой трека: такой цепочке нужен звук с начала
// трека, иначе её отметка сдвинется на начало куска (свип с 120-й секунды на
// куске 115–130 сработал бы на 5-й секунде куска не там, где надо).
var timedParams = []string{"start", "in", "offset"}

// StepsTimed — есть ли в цепочке шаг, привязанный к секунде трека: своя
// отметка (свип, реверс, статтер, затухание), сетка гейта или «с какой секунды».
func StepsTimed(steps []Step) bool {
	for _, s := range steps {
		c := ByID(s.Chain)
		if s.Off || c == nil {
			continue
		}
		if s.Params[fromParamID] > 0 {
			return true
		}
		for _, p := range c.Params {
			for _, id := range timedParams {
				if p.ID == id {
					return true
				}
			}
		}
	}
	return false
}

// Excerpt — кусок файла [fromSec, fromSec+durSec) без обработки, с точностью до
// сэмпла (Run берёт отметку с шагом 0,1 с — для превью «было/стало» мало).
func Excerpt(inPath, outPath string, fromSec, durSec float64) error {
	return Run(inPath, outPath, fmt.Sprintf("[0:a]atrim=start=%.6f:duration=%.6f,asetpts=PTS-STARTPTS[out]",
		fromSec, durSec), nil)
}
