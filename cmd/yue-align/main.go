// yue-align — замер синхронности вклейки: где партия (мини-рендер) совпадает
// по атакам с окном трека и насколько с ним расходятся способы вклейки.
//
//	yue-align -track audio.flac -party child.flac -from 32 -to 40 -lead 2 -beat 0.5
//
// Печатает найденные сдвиг/скорость/score и ошибку (мс) на краях окна для
// «наивной» вклейки (начало партии в from) и для подгонки (сдвиг+темп).
package main

import (
	"flag"
	"fmt"
	"os"

	"yue-studio/internal/dsp"
)

func main() {
	track := flag.String("track", "", "трек (оригинал)")
	party := flag.String("party", "", "партия (мини-рендер)")
	from := flag.Float64("from", 0, "начало окна, с")
	to := flag.Float64("to", 0, "конец окна, с")
	lead := flag.Float64("lead", 0, "сколько плана партии звучит до from (sliceLeadSec), с")
	beat := flag.Float64("beat", 0.5, "длина доли по плану (60/Q), с")
	tempo := flag.Float64("tempo", 0, "искать и скорость партии в ±tempo (0.03 = ±3%); 0 — только сдвиг, как в приложении")
	flag.Parse()
	if *track == "" || *party == "" || *to <= *from {
		flag.Usage()
		os.Exit(2)
	}
	a, err := dsp.MeasureInsert(*track, *party, *from, *to, *lead, *beat)
	if *tempo > 0 {
		a, err = dsp.MeasureInsertTempo(*track, *party, *from, *to, *lead, *beat, 1-*tempo, 1+*tempo)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// ошибка отображения «событие партии t → трек» против найденного
	errAt := func(start, ratio, trackT float64) float64 {
		t := (trackT - a.StartSec) * a.Ratio // момент партии, который ДОЛЖЕН звучать в trackT
		got := start + t/ratio
		return (got - trackT) * 1000
	}
	fmt.Printf("найдено: начало партии в треке %.3f с, скорость %.4f, score %.2f, подгонка %v\n", a.StartSec, a.Ratio, a.Score, a.Aligned)
	fmt.Printf("наивно (партия с from=%.2f): ошибка %+.0f мс в начале окна, %+.0f мс в конце\n",
		*from, errAt(*from, 1, *from), errAt(*from, 1, *to))
	fmt.Printf("с учётом такта контекста (from−lead): %+.0f / %+.0f мс\n",
		errAt(*from-*lead, 1, *from), errAt(*from-*lead, 1, *to))
	fmt.Printf("подгонка (сдвиг+темп): %+.0f / %+.0f мс\n",
		errAt(a.StartSec, a.Ratio, *from), errAt(a.StartSec, a.Ratio, *to))
}
