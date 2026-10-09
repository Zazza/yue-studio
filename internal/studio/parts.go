package studio

// Партии-рецепты пресетов звука: ноты синта по аккордам и удары перкуссии по тактам — порт чистой логики
// фронта (frontend/src/synthPart.js partNotes, frontend/src/percPart.js percHits/barsFromBeat). Паритет —
// эталоны testdata/parts_golden.json, их пишет vitest (partsGolden.test.js).

import (
	"math"
	"regexp"
	"slices"
	"strings"

	"yue-studio/internal/yue"
)

var (
	chordRoot = map[string]int{"C": 0, "D": 2, "E": 4, "F": 5, "G": 7, "A": 9, "B": 11}
	chordQual = map[string][]int{"": {4, 7}, "maj": {4, 7}, "m": {3, 7}, "min": {3, 7}, "dim": {3, 6}, "aug": {4, 8},
		"7": {4, 7, 10}, "maj7": {4, 7, 11}, "m7": {3, 7, 10}, "sus2": {2, 7}, "sus4": {5, 7}, "5": {7}}
	chordRe = regexp.MustCompile(`^([A-G])([#b]?)(maj7|maj|min|m7|m|dim|aug|sus2|sus4|7|5)?(?:/[A-G][#b]?)?$`)
	// нижняя нота регистра стиля: C4 или C2 (pulse — синт-бас)
	partBase = map[string]int{"pad": 60, "arp": 60, "drone": 60, "pulse": 36}
)

// chordPcs — «Dm» → [2 5 9] (основной тон, терция, квинта, …); нераспознанный — nil.
func chordPcs(name string) []int {
	m := chordRe.FindStringSubmatch(strings.TrimSpace(name))
	if m == nil {
		return nil
	}
	acc := map[string]int{"#": 1, "b": -1}[m[2]]
	root := (chordRoot[m[1]] + acc + 12) % 12
	out := []int{root}
	for _, i := range chordQual[m[3]] {
		out = append(out, (root+i)%12)
	}
	return out
}

// PartOpts — стиль партии синта (pad|arp|pulse|drone), октава −2…2, части песни (пусто — все такты).
type PartOpts struct {
	Style    string
	Octave   int
	Sections []string
}

// PartNote — нота партии (секунды трека).
type PartNote struct {
	T    float64 `json:"t"`
	D    float64 `json:"d"`
	Midi []int   `json:"midi"`
	Vel  float64 `json:"vel"`
}

func inSections(sections []string, s string) bool {
	return len(sections) == 0 || slices.Contains(sections, s)
}

// PartNotes — ноты партии синта по тактам (как synthPart.partNotes; пустой Sections — все такты).
func PartNotes(bars []yue.ChordBar, o PartOpts) []PartNote {
	base, ok := partBase[o.Style]
	if !ok {
		base = 60
	}
	base += 12 * max(-2, min(2, o.Octave))
	inReg := func(pc int) int { return base + pc }
	var out []PartNote
	var drone *PartNote
	droneChord := ""
	flush := func() {
		if drone != nil {
			out = append(out, *drone)
			drone = nil
		}
	}
	for _, b := range bars {
		pcs := chordPcs(b.Chord)
		n := b.End - b.Start
		if pcs == nil || !inSections(o.Sections, b.Section) || !(n > 0) {
			flush()
			continue
		}
		triad := make([]int, 0, 3)
		for _, pc := range pcs[:min(3, len(pcs))] {
			triad = append(triad, inReg(pc))
		}
		slices.Sort(triad)
		switch o.Style {
		case "arp":
			for i := range 8 {
				out = append(out, PartNote{T: b.Start + float64(i)*n/8, D: n / 8, Midi: []int{triad[i%len(triad)]}, Vel: velAlt(i, 0.65, 0.8)})
			}
		case "pulse":
			for i := range 8 {
				out = append(out, PartNote{T: b.Start + float64(i)*n/8, D: n / 8, Midi: []int{inReg(pcs[0])}, Vel: velAlt(i, 0.7, 0.85)})
			}
		case "drone":
			midi := []int{inReg(pcs[0]), inReg((pcs[0] + 7) % 12)}
			slices.Sort(midi)
			if drone != nil && droneChord == b.Chord && math.Abs(drone.T+drone.D-b.Start) < 1e-6 {
				drone.D += n
			} else {
				flush()
				drone, droneChord = &PartNote{T: b.Start, D: n, Midi: midi, Vel: 0.75}, b.Chord
			}
			continue
		default: // pad — аккорд целиком (с септимой, если есть)
			midi := make([]int, 0, len(pcs))
			for _, pc := range pcs {
				midi = append(midi, inReg(pc))
			}
			slices.Sort(midi)
			out = append(out, PartNote{T: b.Start, D: n, Midi: midi, Vel: 0.8})
		}
		flush()
	}
	flush()
	return out
}

func velAlt(i int, odd, even float64) float64 {
	if i%2 == 1 {
		return odd
	}
	return even
}

// PercOpts — рисунок (fours|eighths|sixteenths|backbeat|offbeat), части песни (пусто — все), свинг 0…0,5,
// акцент 0…1 (1 — полный рисунок силы).
type PercOpts struct {
	Pattern  string
	Sections []string
	Swing    float64
	Accent   float64
}

// PercHit — удар партии перкуссии (секунды трека).
type PercHit struct {
	T   float64 `json:"t"`
	D   float64 `json:"d"`
	Vel float64 `json:"vel"`
}

type percPattern struct {
	cells int
	on    func(i int) bool
}

var percPatterns = map[string]percPattern{
	"fours":      {4, func(int) bool { return true }},
	"eighths":    {8, func(int) bool { return true }},
	"sixteenths": {16, func(int) bool { return true }},
	"backbeat":   {4, func(i int) bool { return i%2 == 1 }},
	"offbeat":    {8, func(i int) bool { return i%2 == 1 }},
}

const percStrong = 0.9 // сила доли

func percBaseVel(i, cells int) float64 {
	per := cells / 4
	k := i % per
	if k == 0 {
		return percStrong
	}
	if per == 4 && k%2 == 1 {
		return 0.4
	}
	return 0.6
}

// PercHits — удары перкуссии по тактам (как percPart.percHits).
func PercHits(bars []yue.ChordBar, o PercOpts) []PercHit {
	pat, ok := percPatterns[o.Pattern]
	if !ok {
		pat = percPatterns["eighths"]
	}
	sw := max(0, min(0.5, o.Swing))
	ac := max(0, min(1, o.Accent))
	var out []PercHit
	for _, b := range bars {
		n := b.End - b.Start
		if !(n > 0) || !inSections(o.Sections, b.Section) {
			continue
		}
		cell := n / float64(pat.cells)
		for i := range pat.cells {
			if !pat.on(i) {
				continue
			}
			late := 0.0
			if pat.cells > 4 && i%2 == 1 {
				late = sw * cell
			}
			vel := percStrong - ac*(percStrong-percBaseVel(i, pat.cells))
			out = append(out, PercHit{T: b.Start + float64(i)*cell + late, D: cell, Vel: math.Round(vel*1000) / 1000})
		}
	}
	return out
}

// BarsFromBeat — такты без плана по темпу и доле (как percPart.barsFromBeat): по 4 доли от offset + shift
// долей до конца трека (последний — неполный), section "".
func BarsFromBeat(g yue.BeatGrid, dur float64, shift int) []yue.ChordBar {
	if !(g.BPM > 0) || !(dur > 0) {
		return nil
	}
	n := 240 / g.BPM
	k0 := max(0, min(3, shift))
	o := max(0, g.Offset) + float64(k0)*n/4
	var out []yue.ChordBar
	for k := 0; dur-(o+float64(k)*n) > 1e-6; k++ {
		out = append(out, yue.ChordBar{Start: o + float64(k)*n, End: math.Min(o+float64(k+1)*n, dur)})
	}
	return out
}
