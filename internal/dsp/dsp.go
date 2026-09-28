// Package dsp — пост-обработка ffmpeg-цепочками на стороне ПК.
package dsp

import (
	"bytes"
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
		p["wow"], p["cut"], p["hiss"])
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
		p["wow"], p["flutter"], p["cut"])
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
	return c.graph(p)
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

// VocalsOverGraph — родной вокал (стем) поверх нового аккомпанемента:
// length по аккомпанементу (длиннее), вокал в полный уровень. Для вокальных
// треков: овердаб-ререндер с текстом удваивает голос, микс двух исполнений
// на быстром груве расходится по таймингу — держим голос родным.
func VocalsOverGraph() string {
	return "[1:a]volume=1.0[vc];[0:a][vc]amix=inputs=2:duration=longest:normalize=0[out]"
}

// MixUnderGraph — filter_complex для вклейки партии в оригинал: партия
// задерживается до atSec, приглушается и подмешивается без нормализации
// (amix normalize=0 — иначе он делит громкость на число входов).
// durSec > 0 обрезает партию по окну выделения: рендер куска моделью
// не останавливается на длине плана и может раздуться на минуты.
func MixUnderGraph(atSec, durSec, gain float64) string {
	if atSec < 0 {
		atSec = 0
	}
	ms := int(atSec * 1000)
	trim := ""
	if durSec > 0 {
		trim = fmt.Sprintf("atrim=duration=%.3f,", durSec)
	}
	return fmt.Sprintf(
		"[1:a]%sadelay=%d|%d,volume=%.2f[du];[0:a][du]amix=inputs=2:duration=first:normalize=0[out]",
		trim, ms, ms, gain)
}

// RunTwoInputs — ffmpeg с двумя входами (микс партии под оригинал).
func RunTwoInputs(basePath, partyPath, outPath, filterGraph string) error {
	args := []string{"-y", "-hide_banner", "-loglevel", "error",
		"-i", basePath, "-i", partyPath, "-filter_complex", filterGraph, "-map", "[out]", outPath}
	cmd := ffmpegCmd(args)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, stderr.String())
	}
	return nil
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
