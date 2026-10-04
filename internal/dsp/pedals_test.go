package dsp

import (
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Тесты карточки internal-guitar-pedals, этап 3 (условия 3.1–3.3): гитарные педали.
// Написаны по карточке, без чтения реализации: проверяется звук на синтетике через
// граф цепочки (Run / ffmpeg с тем же графом), а не текст графа.
//
// ID крутилок педалей в контракте карточки не названы — карточка говорит
// «перегруз», «тон», «громкость», «частота», «скорость». Крутилка ищется по ID из
// списка привычных имён, затем по подписи (Label); не нашлась — тест падает с
// понятным сообщением (это пробел контракта, а не повод подгонять тест).
//
// Хелперы nc* — из newchains_test.go, needFFmpeg/decode/toneAmpAt/segRMS — из
// inserts_test.go, dbfs/hasParam — из chains_test.go, ncGainProfile/ncMeanAbsDiff —
// из modfilter_test.go, ncStepLevels/ncStepDiff — из dynamics_test.go.

// pdPedalIDs — новые цепочки-педали карточки 3.1 с названиями из карточки.
var pdPedalIDs = []struct{ id, name string }{
	{"od-ts", "Овердрайв (Tube Screamer)"},
	{"fuzz-muff", "Фузз (Big Muff)"},
	{"dist-rat", "Дисторшн (RAT)"},
	{"fuzz-octave", "Октавный фузз"},
	{"boost", "Бустер"},
	{"univibe", "Uni-Vibe"},
	{"ringmod", "Кольцевой модулятор"},
	{"noise-gate", "Гейт от шума"},
	{"comp-pedal", "Компрессор (педаль)"},
	{"cab", "Кабинет"},
}

// pdOldPedalIDs — существующие цепочки, которые тоже получают флаг Pedal (3.1).
var pdOldPedalIDs = []string{
	"chorus", "flanger", "phaser", "tremolo", "autowah", "octaver", "delay",
	"reverb-room", "reverb-hall", "reverb-plate", "reverb-spring", "eq",
}

// pdDriveIDs — перегрузы (3.2): «перегруз», «тон», «громкость».
var pdDriveIDs = []string{"od-ts", "fuzz-muff", "dist-rat"}

// pdKnob — найти крутилку цепочки по одному из ID, иначе по подстроке подписи.
func pdKnob(c *Chain, ids, labels []string) *Param {
	for _, id := range ids {
		for i := range c.Params {
			if c.Params[i].ID == id {
				return &c.Params[i]
			}
		}
	}
	for _, sub := range labels {
		for i := range c.Params {
			if strings.Contains(strings.ToLower(c.Params[i].Label), sub) {
				return &c.Params[i]
			}
		}
	}
	return nil
}

func pdMustKnob(t *testing.T, c *Chain, what string, ids, labels []string) *Param {
	t.Helper()
	p := pdKnob(c, ids, labels)
	if p == nil {
		var have []string
		for _, q := range c.Params {
			have = append(have, q.ID+" «"+q.Label+"»")
		}
		t.Fatalf("%s: нет крутилки «%s» (искал ID %v или подпись с %v); есть: %v",
			c.ID, what, ids, labels, have)
	}
	return p
}

func pdDriveKnob(t *testing.T, c *Chain) *Param {
	return pdMustKnob(t, c, "перегруз",
		[]string{"drive", "gain", "fuzz", "sustain", "dist", "distortion", "overdrive"},
		[]string{"перегруз", "драйв", "сустейн", "фузз", "дисторш", "drive", "gain"})
}

func pdToneKnob(t *testing.T, c *Chain) *Param {
	return pdMustKnob(t, c, "тон", []string{"tone", "filter"}, []string{"тон", "фильтр", "tone"})
}

func pdLevelKnob(t *testing.T, c *Chain) *Param {
	return pdMustKnob(t, c, "громкость",
		[]string{"level", "volume", "vol", "out", "output"},
		[]string{"громкость", "уровень", "level", "volume"})
}

// pdRunF32 — прогнать вход через граф цепочки ffmpeg'ом с выходом в float-WAV:
// у flac/wav по умолчанию целые отсчёты, они обрезают всё выше 0 дБFS, и пик
// выше нуля было бы не увидеть. Возвращает два канала с частотой ncSR.
func pdRunF32(t *testing.T, c *Chain, in string, p map[string]float64) (l, r []float32) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.wav")
	if o, err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-i", in, "-filter_complex", c.FilterGraph(p), "-map", "[out]",
		"-c:a", "pcm_f32le", out).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %s %v: %v %s", c.ID, p, err, o)
	}
	return ncDecode2(t, out, ncSR)
}

// pdMono — среднее двух каналов.
func pdMono(l, r []float32) []float32 {
	m := make([]float32, min(len(l), len(r)))
	for i := range m {
		m[i] = (l[i] + r[i]) / 2
	}
	return m
}

// pdRun — Run цепочки с параметрами, выход моно с частотой ncSR.
func pdRun(t *testing.T, id, in string, p map[string]float64) []float32 {
	t.Helper()
	return pdMono(ncDecode2(t, ncRun(t, id, in, p), ncSR))
}

// --- 3.1 ---

// Карточка 3.1: новые педали есть в dsp.All(), с флагом Pedal и названием из карточки.
func TestPedalChainsRegistered(t *testing.T) {
	inAll := map[string]*Chain{}
	for _, c := range All() {
		c := c
		inAll[c.ID] = &c
	}
	for _, pc := range pdPedalIDs {
		c := ByID(pc.id)
		if c == nil {
			t.Errorf("ByID(%q) = nil, педаль должна существовать", pc.id)
			continue
		}
		if inAll[pc.id] == nil {
			t.Errorf("%s нет в dsp.All() (не видна в DSP)", pc.id)
		}
		if !c.Pedal {
			t.Errorf("%s: Pedal = false, want true (видна в палитре педалей)", pc.id)
		}
		if c.Name != pc.name {
			t.Errorf("%s: Name = %q, want %q", pc.id, c.Name, pc.name)
		}
		if c.Key != "" {
			t.Errorf("%s: Key = %q, want пусто (педаль на одну дорожку)", pc.id, c.Key)
		}
	}
}

// Карточка 3.1: существующие chorus/flanger/phaser/tremolo/autowah/octaver/delay/
// reverb-*/eq тоже с флагом Pedal.
func TestOldChainsMarkedPedal(t *testing.T) {
	for _, id := range pdOldPedalIDs {
		c := ByID(id)
		if c == nil {
			t.Errorf("ByID(%q) = nil", id)
			continue
		}
		if !c.Pedal {
			t.Errorf("%s: Pedal = false, want true (карточка 3.1)", id)
		}
	}
}

// Контракт: Chain.Pedal — json `pedal,omitempty`.
func TestChainPedalJSON(t *testing.T) {
	c := ByID("od-ts")
	if c == nil {
		t.Fatal("od-ts не найдена")
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"pedal":true`) {
		t.Errorf("json od-ts без \"pedal\":true: %s", b)
	}
	// «Убрать свист» — ремонт голоса, не гитарная педаль (в списке 3.1 её нет)
	w := ByID("dewhistle")
	if w == nil {
		t.Fatal("dewhistle не найдена")
	}
	if w.Pedal {
		t.Fatal("dewhistle — не педаль (нет в списке 3.1), Pedal должен быть false")
	}
	b, _ = json.Marshal(w)
	if strings.Contains(string(b), `"pedal"`) {
		t.Errorf("json dewhistle содержит pedal (omitempty): %s", b)
	}
}

// --- 3.2 ---

const pdF = 220.0 // тон «струны» для перегрузов

func pdToneIn(t *testing.T, amp float64) string {
	t.Helper()
	return ncGen(t, fmt.Sprintf("%g*sin(2*PI*%g*t)", amp, pdF), 3, ncSR)
}

// Карточка 3.2: больше «перегруза» → больше гармоник: доля энергии выше 2f у тона f
// растёт (порог ≥ 3 дБ между минимумом и максимумом крутилки — выбран тестом,
// карточка говорит «растёт»).
func TestOverdriveMoreDriveMoreHarmonics(t *testing.T) {
	needFFmpeg(t)
	in := pdToneIn(t, 0.3)
	for _, id := range pdDriveIDs {
		t.Run(id, func(t *testing.T) {
			c := ncChain(t, id)
			k := pdDriveKnob(t, c)
			lo := pdRun(t, id, in, map[string]float64{k.ID: k.Min})
			hi := pdRun(t, id, in, map[string]float64{k.ID: k.Max})
			// «выше 2f»: сама 2f не считается, граница — 2.5f
			sLo := ncHighShareDb(lo, ncSR, 0.5, 2.5, 2.5*pdF)
			sHi := ncHighShareDb(hi, ncSR, 0.5, 2.5, 2.5*pdF)
			if sHi-sLo < 3 {
				t.Errorf("%s=%g: доля выше 2f %.1f дБ, при %s=%g: %.1f дБ — want рост ≥ 3 дБ",
					k.ID, k.Max, sHi, k.ID, k.Min, sLo)
			}
		})
	}
}

// Карточка 3.2: «тон» ниже → меньше верха: доля энергии выше 2 кГц при тоне в
// минимуме меньше, чем в максимуме (порог ≥ 3 дБ выбран тестом).
func TestOverdriveToneDarkens(t *testing.T) {
	needFFmpeg(t)
	in := pdToneIn(t, 0.3)
	for _, id := range pdDriveIDs {
		t.Run(id, func(t *testing.T) {
			c := ncChain(t, id)
			k := pdToneKnob(t, c)
			dark := pdRun(t, id, in, map[string]float64{k.ID: k.Min})
			bright := pdRun(t, id, in, map[string]float64{k.ID: k.Max})
			sd := ncHighShareDb(dark, ncSR, 0.5, 2.5, 2000)
			sb := ncHighShareDb(bright, ncSR, 0.5, 2.5, 2000)
			if sb-sd < 3 {
				t.Errorf("%s: доля выше 2 кГц при %s=%g %.1f дБ, при %s=%g %.1f дБ — want темнее ≥ 3 дБ",
					id, k.ID, k.Min, sd, k.ID, k.Max, sb)
			}
		})
	}
}

// Карточка 3.2: «громкость» — уровень выхода: максимум крутилки громче минимума
// (порог ≥ 6 дБ выбран тестом: крутилка с меньшим ходом бесполезна), середина — между.
func TestOverdriveLevelSetsOutput(t *testing.T) {
	needFFmpeg(t)
	in := pdToneIn(t, 0.3)
	for _, id := range pdDriveIDs {
		t.Run(id, func(t *testing.T) {
			c := ncChain(t, id)
			k := pdLevelKnob(t, c)
			mid := (k.Min + k.Max) / 2
			lv := func(v float64) float64 {
				return dbfs(ncRMS(pdRun(t, id, in, map[string]float64{k.ID: v}), ncSR, 0.5, 2.5))
			}
			lo, md, hi := lv(k.Min), lv(mid), lv(k.Max)
			if hi-lo < 6 {
				t.Errorf("%s: RMS при %s=%g %.1f дБFS, при %g %.1f дБFS — want разница ≥ 6 дБ",
					id, k.ID, k.Max, hi, k.Min, lo)
			}
			if !(lo < md && md < hi) {
				t.Errorf("%s: уровень не растёт с крутилкой: min %.1f, mid %.1f, max %.1f дБFS", id, lo, md, hi)
			}
		})
	}
}

// Карточка 3.2: пик ≤ 0 дБFS даже при громком входе и всех крутилках в максимуме.
// Выход — float-WAV (иначе целочисленный формат сам обрезал бы пик и тест был бы
// пустым); допуск 0.01 дБ на округление.
func TestOverdrivePeakNotAboveZeroDbfs(t *testing.T) {
	needFFmpeg(t)
	in := pdToneIn(t, 0.9)
	for _, id := range pdDriveIDs {
		t.Run(id, func(t *testing.T) {
			c := ncChain(t, id)
			p := map[string]float64{}
			for _, q := range c.Params {
				p[q.ID] = q.Max
			}
			// отметки времени (start/from), если есть, — в начало: эффект должен звучать
			for _, k := range []string{"start", "from"} {
				if hasParam(c, k) {
					p[k] = 0
				}
			}
			l, r := pdRunF32(t, c, in, p)
			pk := math.Max(ncPeak(l, ncSR, 0, 3), ncPeak(r, ncSR, 0, 3))
			if pk == 0 {
				t.Fatalf("%s: выход — тишина", id)
			}
			if db := dbfs(pk); db > 0.01 {
				t.Errorf("%s: всё на максимуме — пик %.2f дБFS, want ≤ 0", id, db)
			}
		})
	}
}

// --- 3.3 ---

// Карточка 3.3: октавный фузз — у тона f появляется 2f не тише −20 дБ к f.
func TestFuzzOctaveAddsOctave(t *testing.T) {
	needFFmpeg(t)
	const f = 440.0
	in := ncGen(t, fmt.Sprintf("0.3*sin(2*PI*%g*t)", f), 3, ncSR)
	out := pdRun(t, "fuzz-octave", in, nil)
	a1, a2 := toneAmpAt(out, ncSR, f, 0.5, 2.5), toneAmpAt(out, ncSR, 2*f, 0.5, 2.5)
	if a1 == 0 && a2 == 0 {
		t.Fatal("выход — тишина")
	}
	if d := dbfs(a2) - dbfs(a1); d < -20 {
		t.Errorf("2f (%g Гц) %.1f дБ к f, want ≥ −20", 2*f, d)
	}
}

// Карточка 3.3: кольцевой модулятор (mix 1) — у тона f появляются f±fm, сам f
// подавлен ≥ 20 дБ к входу. Боковые не тише −20 дБ к исходному f (порог выбран
// тестом: у идеального кольцевого они по −6 дБ).
func TestRingmodSidebands(t *testing.T) {
	needFFmpeg(t)
	const f = 1000.0
	c := ncChain(t, "ringmod")
	k := pdMustKnob(t, c, "частота модуляции",
		[]string{"freq", "fm", "hz", "carrier", "mod"}, []string{"частот", "гц"})
	fm := math.Min(math.Max(300, k.Min), k.Max)
	if fm >= f {
		t.Fatalf("ringmod: частота модуляции %g не ниже тона %g — тест не построить", fm, f)
	}
	if !hasParam(c, "mix") {
		t.Fatalf("ringmod: нет крутилки mix (карточка: «с mix 1»)")
	}
	in := ncGen(t, fmt.Sprintf("0.3*sin(2*PI*%g*t)", f), 3, ncSR)
	src := pdMono(ncDecode2(t, in, ncSR)) // тем же путём, что и выход (pdRun)
	out := pdRun(t, "ringmod", in, map[string]float64{k.ID: fm, "mix": 1})
	ref := dbfs(toneAmpAt(src, ncSR, f, 0.5, 2.5))
	if d := dbfs(toneAmpAt(out, ncSR, f, 0.5, 2.5)) - ref; d > -20 {
		t.Errorf("f %g Гц: %+.1f дБ к входу, want подавлен ≥ 20 дБ", f, d)
	}
	for _, sb := range []float64{f - fm, f + fm} {
		if d := dbfs(toneAmpAt(out, ncSR, sb, 0.5, 2.5)) - ref; d < -20 {
			t.Errorf("боковая %g Гц (f±fm, fm=%g): %.1f дБ к исходному f, want ≥ −20", sb, fm, d)
		}
	}
}

// Карточка 3.3: Uni-Vibe — спектр качается во времени: на белом шуме АЧХ в окне
// 0–0.5 с отличается от окна через полпериода качания (≥ 1 дБ в среднем — как у
// chorus/flanger/phaser).
func TestUnivibeSpectrumMoves(t *testing.T) {
	needFFmpeg(t)
	c := ncChain(t, "univibe")
	k := pdMustKnob(t, c, "скорость", []string{"rate", "speed"}, []string{"скорост", "гц"})
	rate := 0.5 // период 2 с, полпериода — 1 с
	if rate < k.Min || rate > k.Max {
		t.Fatalf("univibe: скорость %g вне диапазона %g–%g — выбери другие окна", rate, k.Min, k.Max)
	}
	p := map[string]float64{k.ID: rate}
	if hasParam(c, "mix") {
		p["mix"] = 1
	}
	in := ncNoise(t, 0.3, 3, 3, chSR)
	src := decode(t, in)
	out := decode(t, ncRun(t, "univibe", in, p))
	a, b := ncGainProfile(out, src, 0, 0.5), ncGainProfile(out, src, 1, 1.5)
	if d := ncMeanAbsDiff(a, b); d < 1 {
		t.Errorf("АЧХ в окнах 0–0.5 и 1–1.5 с отличается в среднем на %.2f дБ, want ≥ 1 (спектр качается)", d)
	}
}

// Карточка 3.3: гейт от шума — между нотами тишина < −50 дБ (к уровню нот), ноты
// целы (±1 дБ — порог выбран тестом). Вход: ноты 440 Гц по 0.5 с через 1 с и
// постоянный фон (гул 1234 Гц) на −40 дБ к нотам. Паузы меряются с отступом 0.4 с
// от конца ноты (время отпускания гейта), ноты — с отступом 50 мс от начала.
func TestNoiseGateSilencesBetweenNotes(t *testing.T) {
	needFFmpeg(t)
	const expr = "0.3*sin(2*PI*440*t)*lt(mod(t\\,1.5)\\,0.5)+0.003*sin(2*PI*1234*t)"
	in := ncGen(t, expr, 4.5, ncSR)
	src := pdMono(ncDecode2(t, in, ncSR)) // тем же путём, что и выход (pdRun)
	out := pdRun(t, "noise-gate", in, nil)
	for _, n := range []float64{1.5, 3.0} { // первую ноту не берём — прогрев
		note := dbfs(ncRMS(src, ncSR, n+0.05, n+0.45))
		if d := dbfs(ncRMS(out, ncSR, n+0.05, n+0.45)) - note; math.Abs(d) > 1 {
			t.Errorf("нота %.1f с: %+.1f дБ к исходной, want ±1 (ноты целы)", n, d)
		}
		gap := dbfs(ncRMS(out, ncSR, n+0.9, n+1.4))
		if d := gap - note; d > -50 {
			t.Errorf("пауза после ноты %.1f с: %.1f дБ к ноте, want < −50 (тишина)", n, d)
		}
	}
}

// Карточка 3.3: компрессор-педаль — разница громких и тихих нот сжимается. Вход —
// тон 1000 Гц: секунды −30 и −6 дБFS по очереди; перепад на выходе меньше входного
// (порог ≥ 3 дБ выбран тестом, у multiband — ≥ 6 дБ при amount 1).
func TestCompPedalSquashesDynamics(t *testing.T) {
	needFFmpeg(t)
	in := ncGen(t, ncStepLevels, 4, chSR)
	src := decode(t, in)
	out := decode(t, ncRun(t, "comp-pedal", in, nil))
	if d := ncStepDiff(src) - ncStepDiff(out); d < 3 {
		t.Errorf("перепад тихих и громких нот: вход %.1f дБ, выход %.1f дБ — want сжатие ≥ 3 дБ",
			ncStepDiff(src), ncStepDiff(out))
	}
}

// Карточка 3.3: кабинет — на белом шуме энергия выше 6 кГц падает ≥ 12 дБ, ниже
// 4 кГц меняется ≤ 3 дБ.
func TestCabCutsHighs(t *testing.T) {
	needFFmpeg(t)
	in := ncNoise(t, 0.2, 3, 3, ncSR)
	src := pdMono(ncDecode2(t, in, ncSR))
	out := pdRun(t, "cab", in, nil)
	if d := ncBandDb(out, ncSR, 0.5, 2.5, 6000, ncSR/2) - ncBandDb(src, ncSR, 0.5, 2.5, 6000, ncSR/2); d > -12 {
		t.Errorf("энергия выше 6 кГц %+.1f дБ, want ≤ −12", d)
	}
	if d := ncBandDb(out, ncSR, 0.5, 2.5, 20, 4000) - ncBandDb(src, ncSR, 0.5, 2.5, 20, 4000); math.Abs(d) > 3 {
		t.Errorf("энергия ниже 4 кГц %+.1f дБ, want |Δ| ≤ 3", d)
	}
}
