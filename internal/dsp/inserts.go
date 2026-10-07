package dsp

import (
	"bytes"
	"cmp"
	"fmt"
	"math"
	"strconv"
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
		if inPlace(in) {
			writeInPlace(&b, in, i+1)
			labels += fmt.Sprintf("[p%d]", i+1)
			continue
		}
		fmt.Fprintf(&b, "[%d:a]", i+1)
		if in.SkipSec > 0 {
			// то же значение, что у adelay ниже (%g, без округления до мс): иначе
			// начало куска и задержка расходились на сэмплы, и «вычитание» на
			// некруглой секунде делало звук громче (ревью: остаток 0.45 при 0.3)
			fmt.Fprintf(&b, "atrim=start=%s,asetpts=PTS-STARTPTS,", ffNum(in.SkipSec))
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
		ms := max(in.AtSec, 0) * 1000 // дробные мс: ffmpeg округляет до сэмпла так же, как atrim
		fmt.Fprintf(&b, "adelay=delays=%s:all=1,volume=%.3f[p%d];", ffNum(ms), in.Gain, i+1)
		labels += fmt.Sprintf("[p%d]", i+1)
	}
	fmt.Fprintf(&b, "%samix=inputs=%d:duration=first:normalize=0[out]", labels, len(ins)+1)
	return b.String()
}

// inPlace — вставка той же дорожки на её же место (заглушение, вычитание исходной,
// эффект на дорожку): кусок с SkipSec берётся и кладётся в AtSec == SkipSec, без растяжения.
func inPlace(in Insert) bool {
	return in.SkipSec == in.AtSec && (in.Tempo <= 0 || in.Tempo == 1)
}

// writeInPlace — вставка «на месте» без atrim/adelay: дорожка целиком, окно — afade прямо
// на её шкале времени (до начала и после конца afade даёт тишину). atrim округляет время к
// ближайшему сэмплу, а adelay отбрасывает дробь: на времени с дробным числом сэмплов
// (44,1 кГц, некруглые мс) кусок вставал на сэмпл раньше базы, и «вычитание» оставляло звук
// громче исходного (+3 дБ на белом шуме, кросс-ревью internal-studio-engine). afade без
// сдвига, так же быстр и округляет начало как воркер (round). Окно [AtSec, AtSec+DurSec)
// (DurSec 0 — до конца), края — FadeIn/FadeOut; край без фейда — спад hardEdgeSec.
func writeInPlace(b *strings.Builder, in Insert, n int) {
	at := max(in.AtSec, 0)
	// частотной маске (afftfilt) весь трек не нужен — она дорогая: кусок от origin (сетка
	// lowpassGridSec, с запасом под окно FFT). На место он встаёт не через adelay (тот
	// ошибается на сэмпл: 123200 мс при 44,1 кГц — 5433119 вместо 5433120), а склейкой с
	// тишиной до origin: оба куска режет atrim по одному времени — стык точен до сэмпла
	origin := 0.0
	if in.LowpassHz > 0 {
		origin = max(0, math.Floor((at-lowpassPadSec)/lowpassGridSec)*lowpassGridSec)
	}
	if origin > 0 {
		fmt.Fprintf(b, "[%[1]d:a]asplit=2[sa%[1]d][sb%[1]d];[sa%[1]d]atrim=end=%[2]s,volume=0[za%[1]d];[sb%[1]d]"+
			"atrim=start=%[2]s,asetpts=PTS-STARTPTS,", n, ffNum(origin))
	} else {
		fmt.Fprintf(b, "[%d:a]", n)
	}
	if in.LowpassHz > 0 {
		if in.DurSec > 0 {
			fmt.Fprintf(b, "atrim=duration=%s,", ffNum(at+in.DurSec-origin+lowpassPadSec))
		}
		fmt.Fprintf(b, "apad=pad_len=%[1]d,afftfilt=real='re*lte(b*sr/%[2]d\\,%.0[3]f)':imag='im*lte(b*sr/%[2]d\\,%.0[3]f)'"+
			":win_size=%[2]d:overlap=0.75,atrim=start_sample=%[1]d,asetpts=PTS-STARTPTS,",
			fftLatency, fftWin, in.LowpassHz)
	}
	rel := at - origin
	if fadeIn := cmp.Or(in.FadeIn, hardEdgeSec); rel > 0 || in.FadeIn > 0 {
		fmt.Fprintf(b, "afade=t=in:st=%s:d=%s,", ffNum(rel), ffNum(fadeIn))
	}
	if in.DurSec > 0 {
		fadeOut := hardEdgeSec
		if in.FadeOut > 0 && in.DurSec > in.FadeOut {
			fadeOut = in.FadeOut
		}
		fmt.Fprintf(b, "afade=t=out:st=%s:d=%s,", ffNum(max(rel+in.DurSec-fadeOut, 0)), ffNum(fadeOut))
	}
	if origin > 0 {
		fmt.Fprintf(b, "volume=%.3f[wb%[2]d];[za%[2]d][wb%[2]d]concat=n=2:v=0:a=1[p%[2]d];", in.Gain, n)
		return
	}
	fmt.Fprintf(b, "volume=%.3f[p%d];", in.Gain, n)
}

// ffNum — число для опций ffmpeg без экспоненты: «5e-05» он как время не разбирает
// (пересборка с коротким окном падала целиком); точность — как у %g, но в записи 0.00005
func ffNum(x float64) string {
	return strconv.FormatFloat(x, 'f', -1, 64)
}

// частотная маска вставки «на месте»: запас до окна под окно FFT (fftWin при 44,1 кГц ≈ 0,09 с)
// и шаг сетки начала куска (10 мс = 441 и 480 сэмплов)
const (
	lowpassPadSec  = 0.2
	lowpassGridSec = 0.01
)

// hardEdgeSec — край окна вставки «на месте» без фейда: afade нужна длительность,
// 0,1 мс (4–5 сэмплов) на слух — резкий край
const hardEdgeSec = 0.0001

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

// WindowGraph — граф для Run: звук только в окне вставки — линейный подъём
// [from, from+fadeIn], полная громкость до to, линейный спад [to, to+fadeOut],
// вне окна тишина. Та же форма, что у afade в InsertsGraph (кривая tri), и
// посэмплово (aeval): окно эффекта с хвостом вычитается из трека той же
// вставкой без остатка сухого звука.
func WindowGraph(from, fadeIn, to, fadeOut float64) string {
	up := fmt.Sprintf("gte(t\\,%g)", from)
	if fadeIn > 0 {
		up = fmt.Sprintf("clip((t-%g)/%g\\,0\\,1)", from, fadeIn)
	}
	down := fmt.Sprintf("lt(t\\,%g)", to)
	if fadeOut > 0 {
		down = fmt.Sprintf("clip((%g-t)/%g\\,0\\,1)", to+fadeOut, fadeOut)
	}
	return fmt.Sprintf("[0:a]aeval=exprs='val(ch)*%s*%s':c=same[out]", up, down)
}
