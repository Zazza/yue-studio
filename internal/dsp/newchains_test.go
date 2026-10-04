package dsp

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/cmplx"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// Тесты карточки internal-dsp-space, общие условия 0.1–0.6 и контракт 10.1
// (TailSec, Key) по всем новым цепочкам. Тесты написаны по карточке, без чтения
// реализации: проверяется звук на синтетике (Run/RunInputs), а не текст графа.
//
// Общие хелперы новых цепочек (префикс nc) — здесь; хелперы старых тестов
// (needFFmpeg, lavfi, decode, decodeAt, toneAmpAt, segRMS, dbfs, genIn, runChain,
// secs, diffSeg, hasParam) — из inserts_test.go и chains_test.go.

// ncSR — частота для проверок с верхом (> 5 кГц): 16 кГц для них мало.
const ncSR = 44100

// ncNewIDs — все новые цепочки карточки (transient убрана из задачи, карточка 6.2).
var ncNewIDs = []string{
	"reverb-room", "reverb-hall", "reverb-plate", "reverb-spring",
	"delay", "width", "haas",
	"chorus", "flanger", "phaser", "tremolo",
	"eq", "sweep", "autowah",
	"fade", "multiband", "ducking",
	"pitch", "octaver",
	"reverse", "stutter", "tape-stop",
	"vinyl",
}

// ncStereoIDs — стерео-цепочки (условие 0.6).
var ncStereoIDs = []string{"reverb-room", "reverb-hall", "reverb-plate", "reverb-spring", "delay", "width", "haas"}

// ncNoTailIDs — новые цепочки без хвоста: TailSec = 0 (условие 10.1).
var ncNoTailIDs = []string{
	"width", "haas", "chorus", "flanger", "phaser", "tremolo", "eq", "sweep", "autowah",
	"fade", "multiband", "ducking", "pitch", "octaver",
	"reverse", "stutter", "tape-stop", "vinyl",
}

// ncChain — цепочка по ID; нет — Fatal.
func ncChain(t *testing.T, id string) *Chain {
	t.Helper()
	c := ByID(id)
	if c == nil {
		t.Fatalf("ByID(%q) = nil, цепочка должна существовать", id)
	}
	return c
}

// ncFile — сгенерировать файл из источника lavfi (src может быть графом) в flac.
func ncFile(t *testing.T, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "in.flac")
	lavfi(t, src, p)
	return p
}

// ncGen — моно-вход с частотой rate из выражения aevalsrc (запятые экранированы).
func ncGen(t *testing.T, expr string, dur float64, rate int) string {
	t.Helper()
	return ncFile(t, fmt.Sprintf("aevalsrc=exprs='%s':d=%g:s=%d", expr, dur, rate))
}

// ncNoise — белый шум (seed фиксирован — детерминирован) длиной noiseDur, затем
// тишина до total; моно, частота rate.
func ncNoise(t *testing.T, amp, noiseDur, total float64, rate int) string {
	t.Helper()
	return ncFile(t, fmt.Sprintf("anoisesrc=d=%g:c=white:seed=7:a=%g:r=%d,apad=whole_dur=%g",
		noiseDur, amp, rate, total))
}

// ncRun — прогнать файл через цепочку id, вернуть путь выхода.
func ncRun(t *testing.T, id, in string, p map[string]float64) string {
	t.Helper()
	c := ncChain(t, id)
	out := filepath.Join(t.TempDir(), "out.flac")
	if err := Run(in, out, c.FilterGraph(p), nil); err != nil {
		t.Fatalf("Run %s %v: %v", id, p, err)
	}
	return out
}

// ncDecode2 — декодировать файл в два канала float32 с частотой rate
// (моно-файл даёт два одинаковых канала).
func ncDecode2(t *testing.T, path string, rate int) (l, r []float32) {
	t.Helper()
	raw, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", path, "-f", "f32le", "-ac", "2", "-ar", strconv.Itoa(rate), "-").Output()
	if err != nil {
		t.Fatalf("decode2 %s: %v", path, err)
	}
	n := len(raw) / 8
	l, r = make([]float32, n), make([]float32, n)
	for i := 0; i < n; i++ {
		l[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*8:]))
		r[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*8+4:]))
	}
	return l, r
}

// ncDecode2AF — как ncDecode2, но с фильтром ffmpeg -af перед декодированием.
func ncDecode2AF(t *testing.T, path string, rate int, af string) (l, r []float32) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "af.flac")
	if out, err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-i", path, "-af", af, p).CombinedOutput(); err != nil {
		t.Fatalf("af %q: %v %s", af, err, out)
	}
	return ncDecode2(t, p, rate)
}

// ncChannels — число каналов файла (ffprobe).
func ncChannels(t *testing.T, path string) int {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=channels", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("ffprobe %s: %q", path, out)
	}
	return n
}

// ncSeg — отсчёты на [from,to] с при частоте rate.
func ncSeg(s []float32, rate int, from, to float64) []float32 {
	a, b := int(from*float64(rate)), int(to*float64(rate))
	if a < 0 {
		a = 0
	}
	if b > len(s) {
		b = len(s)
	}
	if a >= b {
		return nil
	}
	return s[a:b]
}

func ncRMS(s []float32, rate int, from, to float64) float64 {
	seg := ncSeg(s, rate, from, to)
	if len(seg) == 0 {
		return 0
	}
	return RMS(seg)
}

func ncPeak(s []float32, rate int, from, to float64) float64 {
	var m float64
	for _, v := range ncSeg(s, rate, from, to) {
		m = math.Max(m, math.Abs(float64(v)))
	}
	return m
}

// ncDiffDb — RMS разности a−b на [from,to] относительно RMS b, дБ (−200 — совпадают).
func ncDiffDb(a, b []float32, rate int, from, to float64) float64 {
	x, y := ncSeg(a, rate, from, to), ncSeg(b, rate, from, to)
	n := min(len(x), len(y))
	if n == 0 {
		return math.Inf(1)
	}
	var e, eb float64
	for i := 0; i < n; i++ {
		d := float64(x[i]) - float64(y[i])
		e += d * d
		eb += float64(y[i]) * float64(y[i])
	}
	if e == 0 {
		return -200
	}
	return 10 * math.Log10(e/eb)
}

// ncCorr — коэффициент корреляции Пирсона двух отрезков одинаковой длины.
func ncCorr(a, b []float32) float64 {
	n := min(len(a), len(b))
	if n == 0 {
		return 0
	}
	var ma, mb float64
	for i := 0; i < n; i++ {
		ma += float64(a[i])
		mb += float64(b[i])
	}
	ma /= float64(n)
	mb /= float64(n)
	var sab, saa, sbb float64
	for i := 0; i < n; i++ {
		x, y := float64(a[i])-ma, float64(b[i])-mb
		sab += x * y
		saa += x * x
		sbb += y * y
	}
	if saa == 0 || sbb == 0 {
		return 0
	}
	return sab / math.Sqrt(saa*sbb)
}

// ncXcorrLag — лаг (в отсчётах, |лаг| ≤ maxLag) максимума взаимной корреляции:
// положительный — b отстаёт от a (b[i+lag] ≈ a[i]).
func ncXcorrLag(a, b []float32, maxLag int) int {
	best, bestV := 0, math.Inf(-1)
	for lag := -maxLag; lag <= maxLag; lag++ {
		var s float64
		for i := maxLag; i+maxLag < len(a) && i+maxLag < len(b); i++ {
			s += float64(a[i]) * float64(b[i+lag])
		}
		if s > bestV {
			best, bestV = lag, s
		}
	}
	return best
}

// ncFFT — БПФ (радикс-2, длина — степень двойки).
func ncFFT(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := 0; k < size/2; k++ {
				u, v := x[start+k], x[start+k+size/2]*wk
				x[start+k], x[start+k+size/2] = u+v, u-v
				wk *= w
			}
		}
	}
}

// ncSpectrum — спектр мощности отрезка [from,to] (окно Ханна, дополнение нулями
// до степени двойки); возвращает мощности бинов и шаг частоты бина, Гц.
func ncSpectrum(s []float32, rate int, from, to float64) (pw []float64, binHz float64) {
	seg := ncSeg(s, rate, from, to)
	n := 1
	for n < len(seg) {
		n <<= 1
	}
	x := make([]complex128, n)
	for i, v := range seg {
		w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(len(seg)))
		x[i] = complex(float64(v)*w, 0)
	}
	ncFFT(x)
	pw = make([]float64, n/2)
	for i := range pw {
		pw[i] = real(x[i])*real(x[i]) + imag(x[i])*imag(x[i])
	}
	return pw, float64(rate) / float64(n)
}

// ncBand — энергия спектра в полосе [lo,hi] Гц.
func ncBand(pw []float64, binHz, lo, hi float64) float64 {
	var e float64
	for i, p := range pw {
		if f := float64(i) * binHz; f >= lo && f <= hi {
			e += p
		}
	}
	return e
}

// ncBandDb — энергия полосы [lo,hi] на [from,to], дБ (относительная шкала).
func ncBandDb(s []float32, rate int, from, to, lo, hi float64) float64 {
	pw, bin := ncSpectrum(s, rate, from, to)
	return 10 * math.Log10(ncBand(pw, bin, lo, hi)+1e-30)
}

// ncHighShareDb — доля энергии выше f Гц в отрезке, дБ.
func ncHighShareDb(s []float32, rate int, from, to, f float64) float64 {
	pw, bin := ncSpectrum(s, rate, from, to)
	return 10 * math.Log10((ncBand(pw, bin, f, float64(rate)/2)+1e-30)/(ncBand(pw, bin, 0, float64(rate)/2)+1e-30))
}

// ncAutocorrPeak — лаг (с) максимума автокорреляции s на [lo,hi] с (через БПФ).
func ncAutocorrPeak(s []float32, rate int, lo, hi float64) float64 {
	n := 1
	for n < 2*len(s) {
		n <<= 1
	}
	x := make([]complex128, n)
	var m float64
	for _, v := range s {
		m += float64(v)
	}
	m /= float64(len(s))
	for i, v := range s {
		x[i] = complex(float64(v)-m, 0)
	}
	ncFFT(x)
	for i := range x {
		x[i] = complex(real(x[i])*real(x[i])+imag(x[i])*imag(x[i]), 0)
	}
	// обратное БПФ через сопряжение
	for i := range x {
		x[i] = cmplx.Conj(x[i])
	}
	ncFFT(x)
	a, b := int(lo*float64(rate)), int(hi*float64(rate))
	best, bestV := a, math.Inf(-1)
	for k := a; k <= b && k < len(s); k++ {
		if v := real(x[k]); v > bestV {
			best, bestV = k, v
		}
	}
	return float64(best) / float64(rate)
}

// ncZCFreq — частота тона по переходам через ноль на [from,to].
func ncZCFreq(s []float32, rate int, from, to float64) float64 {
	seg := ncSeg(s, rate, from, to)
	if len(seg) < 2 {
		return 0
	}
	first, last, n := -1, -1, 0
	for i := 1; i < len(seg); i++ {
		if seg[i-1] < 0 && seg[i] >= 0 {
			if first < 0 {
				first = i
			}
			last = i
			n++
		}
	}
	if n < 2 {
		return 0
	}
	return float64(n-1) * float64(rate) / float64(last-first)
}

func ncHasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// ncParamsForRun — параметры для прогона цепочки в общих тестах: отметки (start/from)
// ставятся на at, чтобы эффект действительно звучал внутри короткого входа.
func ncParamsForRun(c *Chain, at float64) map[string]float64 {
	p := map[string]float64{}
	if hasParam(c, "start") {
		p["start"] = at
	}
	if hasParam(c, "from") {
		p["from"] = at
	}
	return p
}

// ncRunAny — прогнать цепочку; цепочке с ключом вторым входом идёт key (если пуст — сам вход).
func ncRunAny(t *testing.T, c *Chain, in, key string, p map[string]float64) (string, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.flac")
	if c.Key != "" {
		if key == "" {
			key = in
		}
		return out, RunInputs([]string{in, key}, out, c.FilterGraph(p))
	}
	return out, Run(in, out, c.FilterGraph(p), nil)
}

// --- 0.1 ---

// Карточка 0.1: каждая новая цепочка в dsp.All(), ByID находит; Name/Note/Label — на русском.
func TestNewChainsRegisteredRussian(t *testing.T) {
	inAll := map[string]bool{}
	for _, c := range All() {
		inAll[c.ID] = true
	}
	for _, id := range ncNewIDs {
		c := ByID(id)
		if c == nil {
			t.Errorf("ByID(%q) = nil", id)
			continue
		}
		if c.ID != id {
			t.Errorf("ByID(%q).ID = %q", id, c.ID)
		}
		if !inAll[id] {
			t.Errorf("%s нет в dsp.All()", id)
		}
		if !ncHasCyrillic(c.Name) {
			t.Errorf("%s: Name %q — не на русском", id, c.Name)
		}
		if !ncHasCyrillic(c.Note) {
			t.Errorf("%s: Note %q — не на русском", id, c.Note)
		}
		for _, p := range c.Params {
			if !ncHasCyrillic(p.Label) {
				t.Errorf("%s: Label крутилки %s %q — не на русском", id, p.ID, p.Label)
			}
		}
	}
}

// --- 0.2 ---

// Карточка 0.2: у цепочек без своего start есть общий from; до отметки from звук не тронут.
func TestNewChainsFromKeepsAudioBefore(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, "0.3*sin(2*PI*440*t)+0.1*sin(2*PI*3000*t)", 4, chSR)
	src := decode(t, in)
	for _, id := range ncNewIDs {
		t.Run(id, func(t *testing.T) {
			c := ncChain(t, id)
			if hasParam(c, "start") {
				if hasParam(c, "from") {
					t.Errorf("%s: есть и start, и from", id)
				}
				t.Skip("своя отметка start — from не положен")
			}
			if !hasParam(c, "from") {
				t.Fatalf("%s: нет ни start, ни from", id)
			}
			out, err := ncRunAny(t, c, in, "", map[string]float64{"from": 2})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got := decode(t, out)
			if r := ncDiffDb(got, src, chSR, 0, 1.9); r > -40 {
				t.Errorf("до from (0–1.9 с) выход отличается от входа: %.1f дБ, want ≤ −40", r)
			}
		})
	}
}

// --- 0.3 ---

// Карточка 0.3: mix 0 → выход = сухой вход (разница ≤ −50 дБ); mix 1 → только
// обработанный. dryWet линеен, поэтому отличие от сухого при mix 1 вдвое (6 дБ)
// больше, чем при mix 0.5.
func TestNewChainsMixDryWet(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, "0.2*sin(2*PI*220*t)+0.2*sin(2*PI*1330*t)+0.1*sin(2*PI*3170*t)", 3, chSR)
	srcL, srcR := ncDecode2(t, in, chSR)
	tested := 0
	for _, id := range ncNewIDs {
		c := ByID(id)
		if c == nil || !hasParam(c, "mix") {
			continue
		}
		tested++
		t.Run(id, func(t *testing.T) {
			run := func(mix float64) ([]float32, []float32) {
				p := ncParamsForRun(c, 0)
				p["mix"] = mix
				out, err := ncRunAny(t, c, in, "", p)
				if err != nil {
					t.Fatalf("Run mix=%v: %v", mix, err)
				}
				return ncDecode2(t, out, chSR)
			}
			l0, r0 := run(0)
			if d := ncDiffDb(l0, srcL, chSR, 0.05, 2.95); d > -50 {
				t.Errorf("mix 0: L отличается от сухого на %.1f дБ, want ≤ −50", d)
			}
			if d := ncDiffDb(r0, srcR, chSR, 0.05, 2.95); d > -50 {
				t.Errorf("mix 0: R отличается от сухого на %.1f дБ, want ≤ −50", d)
			}
			// оба канала вместе: у стерео-цепочки (haas) эффект может быть в одном канале
			both := func(l, r []float32) []float32 {
				return append(append([]float32{}, ncSeg(l, chSR, 0.2, 2.8)...), ncSeg(r, chSR, 0.2, 2.8)...)
			}
			dry := both(srcL, srcR)
			n := float64(len(dry))
			d1, dh := ncDiffDb(both(run(1)), dry, 1, 0, n), ncDiffDb(both(run(0.5)), dry, 1, 0, n)
			if d1 < -30 {
				t.Fatalf("mix 1: выход почти сухой (%.1f дБ) — эффекта нет", d1)
			}
			if r := d1 - dh; r < 4.5 || r > 7.5 {
				t.Errorf("mix 1 vs 0.5: отличие от сухого %+.1f дБ, want ≈ +6 (линейный микс, mix 1 — без сухого)", r)
			}
		})
	}
	if tested == 0 {
		t.Error("ни у одной новой цепочки нет mix (карточка: chorus/flanger/phaser/autowah/haas)")
	}
}

// --- 0.4 ---

// Карточка 0.4: длина выхода = длине входа ±0.05 с у всех новых цепочек (хвост
// реверба/дилея за концом трека отрезается).
func TestNewChainsKeepLength(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, "0.4*sin(2*PI*440*t)*lt(t\\,2)", 3, chSR)
	for _, id := range ncNewIDs {
		t.Run(id, func(t *testing.T) {
			c := ncChain(t, id)
			p := ncParamsForRun(c, 0.5)
			// самый длинный хвост / самые длинные окна
			for _, prm := range c.Params {
				switch prm.ID {
				case "size", "feedback", "len", "dur", "count":
					p[prm.ID] = prm.Max
				}
			}
			out, err := ncRunAny(t, c, in, "", p)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if d := secs(decode(t, out)); math.Abs(d-3) > 0.05 {
				t.Errorf("длина выхода %.3f с, want 3 ± 0.05", d)
			}
		})
	}
}

// Карточка 0.4 (краевой): короткий вход (0.1 с) не роняет ffmpeg, длина та же.
func TestNewChainsShortInput(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, "0.3*sin(2*PI*440*t)", 0.1, chSR)
	for _, id := range ncNewIDs {
		t.Run(id, func(t *testing.T) {
			c := ncChain(t, id)
			out, err := ncRunAny(t, c, in, "", nil)
			if err != nil {
				t.Fatalf("короткий вход: Run: %v", err)
			}
			if d := secs(decode(t, out)); math.Abs(d-0.1) > 0.05 {
				t.Errorf("длина выхода %.3f с, want 0.1 ± 0.05", d)
			}
		})
	}
}

// Карточка 0.4 (краевой): отметка (start/from) за концом входа не роняет ffmpeg, длина та же.
func TestNewChainsMarkBeyondEnd(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, "0.3*sin(2*PI*440*t)", 2, chSR)
	for _, id := range ncNewIDs {
		t.Run(id, func(t *testing.T) {
			c := ncChain(t, id)
			out, err := ncRunAny(t, c, in, "", ncParamsForRun(c, 100))
			if err != nil {
				t.Fatalf("отметка за концом: Run: %v", err)
			}
			if d := secs(decode(t, out)); math.Abs(d-2) > 0.05 {
				t.Errorf("длина выхода %.3f с, want 2 ± 0.05", d)
			}
		})
	}
}

// --- 0.5 ---

// Карточка 0.5: значения вне диапазона зажимаются — граф тот же, что на границе.
func TestNewChainsClampOutOfRange(t *testing.T) {
	for _, id := range ncNewIDs {
		c := ByID(id)
		if c == nil {
			continue // отсутствие ловит TestNewChainsRegisteredRussian
		}
		over, atMax, under, atMin := map[string]float64{}, map[string]float64{}, map[string]float64{}, map[string]float64{}
		for _, p := range c.Params {
			over[p.ID], atMax[p.ID] = p.Max+1000, p.Max
			under[p.ID], atMin[p.ID] = p.Min-1000, p.Min
		}
		if a, b := c.FilterGraph(over), c.FilterGraph(atMax); a != b {
			t.Errorf("%s: выше максимума граф отличается от графа на максимуме", id)
		}
		if a, b := c.FilterGraph(under), c.FilterGraph(atMin); a != b {
			t.Errorf("%s: ниже минимума граф отличается от графа на минимуме", id)
		}
	}
}

// --- 0.6 ---

// Карточка 0.6: стерео-цепочки на моно-входе работают, выход стерео.
func TestNewStereoChainsOnMonoInput(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, "0.3*sin(2*PI*440*t)", 2, chSR)
	for _, id := range ncStereoIDs {
		t.Run(id, func(t *testing.T) {
			out := ncRun(t, id, in, nil)
			if n := ncChannels(t, out); n != 2 {
				t.Errorf("каналов на выходе %d, want 2 (стерео)", n)
			}
		})
	}
}

// --- 10.1 ---

// Карточка 10.1: у цепочек без хвоста TailSec = 0 (и у старых цепочек).
func TestTailSecZeroWithoutTail(t *testing.T) {
	for _, id := range append(append([]string{}, ncNoTailIDs...), "wall") {
		c := ByID(id)
		if c == nil {
			continue
		}
		if v := c.TailSec(nil); v != 0 {
			t.Errorf("%s: TailSec(defaults) = %v, want 0 (хвоста нет)", id, v)
		}
	}
}

// Карточка 10.1: Key — стем-ключ; у ducking — «drums», у остальных пусто; json key,omitempty.
func TestChainKey(t *testing.T) {
	d := ByID("ducking")
	if d == nil {
		t.Fatal("ducking не найден")
	}
	if d.Key != "drums" {
		t.Errorf("ducking.Key = %q, want drums", d.Key)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"key":"drums"`) {
		t.Errorf("json ducking без \"key\":\"drums\": %s", b)
	}
	for _, id := range ncNewIDs {
		if c := ByID(id); c != nil && id != "ducking" && c.Key != "" {
			t.Errorf("%s: Key = %q, want пусто (ключ только у ducking)", id, c.Key)
		}
	}
	if c := ByID("reverb-hall"); c != nil {
		b, _ := json.Marshal(c)
		if strings.Contains(string(b), `"key"`) {
			t.Errorf("json reverb-hall содержит key (omitempty): %s", b)
		}
	}
}
