package dsp

import (
	"bytes"
	"fmt"
	"strings"
)

// Insert — одна вклейка партии в трек.
type Insert struct {
	AtSec   float64 // момент трека, где начинает звучать партия (после отрезки SkipSec)
	SkipSec float64 // сколько отрезать с начала партии (секунды исходной партии, до растяжения)
	DurSec  float64 // сколько звучит в треке (после растяжения); 0 = до конца партии
	Tempo   float64 // atempo; 0 или 1 = без растяжения; вне [0.5, 2] — зажимается
	Gain    float64 // линейный гейн; отрицательный — вычитание (замена дорожки)
	FadeIn  float64 // плавный вход звучащего куска, с; 0 — без фейда
	FadeOut float64 // плавный выход, с (нужен DurSec); 0 — без фейда
	// LowpassHz > 0 — кусок проходит фильтр нижних частот: при вычитании
	// старых барабанов их верх (хэт, тарелки) остаётся в треке
	LowpassHz float64
}

// окно FFT-маски «только низ» и её задержка (окно × перекрытие 0.75), сэмплов
const (
	fftWin     = 4096
	fftLatency = fftWin * 3 / 4
)

// пределы одного фильтра atempo в ffmpeg
const (
	minAtempo = 0.5
	maxAtempo = 2.0
)

// InsertsGraph — filter_complex: вход 0 = база, входы 1..n = партии по порядку.
// Все вклейки накладываются на чистую базу разом: пересборка с новыми гейнами
// не наслаивает прошлые миксы. amix normalize=0 — иначе он делит громкость на
// число входов; длина — по базе.
func InsertsGraph(ins []Insert) string {
	if len(ins) == 0 {
		return "[0:a]anull[out]"
	}
	var b strings.Builder
	labels := "[0:a]"
	for i, in := range ins {
		fmt.Fprintf(&b, "[%d:a]", i+1)
		if in.SkipSec > 0 {
			fmt.Fprintf(&b, "atrim=start=%.3f,asetpts=PTS-STARTPTS,", in.SkipSec)
		}
		if in.Tempo > 0 && in.Tempo != 1 {
			t := min(max(in.Tempo, minAtempo), maxAtempo)
			fmt.Fprintf(&b, "atempo=%.4f,", t)
		}
		if in.DurSec > 0 {
			fmt.Fprintf(&b, "atrim=duration=%.3f,", in.DurSec)
		}
		if in.LowpassHz > 0 {
			// низ дорожки без сдвига фазы: маска по частотам в FFT; обычный
			// lowpass сдвигал фазу, и вычитание «низа» оставляло ~20% речи и
			// задевало верх (#258). Задержка afftfilt (окно × перекрытие) снята
			// обрезкой, хвост — дополнен тишиной
			fmt.Fprintf(&b, "apad=pad_len=%[1]d,afftfilt=real='re*lte(b*sr/%[2]d\\,%.0[3]f)':imag='im*lte(b*sr/%[2]d\\,%.0[3]f)'"+
				":win_size=%[2]d:overlap=0.75,atrim=start_sample=%[1]d,asetpts=PTS-STARTPTS,",
				fftLatency, fftWin, in.LowpassHz)
		}
		if in.FadeIn > 0 {
			fmt.Fprintf(&b, "afade=t=in:st=0:d=%.3f,", in.FadeIn)
		}
		if in.FadeOut > 0 && in.DurSec > in.FadeOut {
			fmt.Fprintf(&b, "afade=t=out:st=%.3f:d=%.3f,", in.DurSec-in.FadeOut, in.FadeOut)
		}
		ms := int(max(in.AtSec, 0) * 1000)
		fmt.Fprintf(&b, "adelay=delays=%d:all=1,volume=%.3f[p%d];", ms, in.Gain, i+1)
		labels += fmt.Sprintf("[p%d]", i+1)
	}
	fmt.Fprintf(&b, "%samix=inputs=%d:duration=first:normalize=0[out]", labels, len(ins)+1)
	return b.String()
}

// RunInputs — ffmpeg с несколькими входами (база + партии) через filter_complex.
func RunInputs(inputs []string, outPath, graph string) error {
	args := []string{"-y", "-hide_banner", "-loglevel", "error"}
	for _, p := range inputs {
		args = append(args, "-i", p)
	}
	args = append(args, "-filter_complex", graph, "-map", "[out]", outPath)
	cmd := ffmpegCmd(args)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, stderr.String())
	}
	return nil
}
