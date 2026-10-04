// Package dsp — пост-обработка ffmpeg-цепочками на стороне ПК.
package dsp

import (
	"fmt"
	"math"
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
	// Voice — цепочка для дорожки голоса (мегафон, телефон, перегруз, слэпбэк):
	// примочки «как у Джека Уайта или Летова». UI такой цепочке автоматически
	// выбирает дорожку «голос»; применение на весь микс остаётся возможным
	Voice bool `json:"voice,omitempty"`
	// Key — дорожка-ключ (стем трека, напр. "drums"): граф читает её вторым
	// входом [1:a]. Такая цепочка работает только эффектом на дорожку — у
	// всего трека стемов-ключей нет
	Key string `json:"key,omitempty"`
	// Pedal — гитарная педаль: цепочка видна в палитре блока «Педали» (и в общем
	// списке эффектов, как все)
	Pedal bool `json:"pedal,omitempty"`
	// match — громкость обработанной дорожки выравнивается по исходной (RMS в
	// окне), крутилка level — поправка сверху. Перегрузы клиппингом выводят
	// любую дорожку почти на полную шкалу: гитара −15 дБ после фузза становилась
	// громче всего микса и клиппировала его (набор «Гранж» на #331: −8 → −2.2 LUFS)
	match bool

	graph func(p map[string]float64) string
	// tail — сколько секунд эффект звучит после конца звука (реверб, дилей);
	// nil — хвоста нет
	tail func(p map[string]float64) float64
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

var dropParams = []Param{
	{ID: "start", Label: "с какой секунды тормозить", Min: 0, Max: 600, Step: 0.1, Default: 120},
	{ID: "dur", Label: "сколько секунд тормозить (исходного звука)", Min: 0.5, Max: 10, Step: 0.1, Default: 3},
	{ID: "slow", Label: "темп в конце торможения (1 — без замедления)", Min: 0.5, Max: 1, Step: 0.05, Default: 0.7},
	{ID: "gap", Label: "тишина после, с", Min: 0, Max: 8, Step: 0.1, Default: 2},
	{ID: "rise", Label: "нарастание после тишины, с (0 — сразу полный звук)", Min: 0, Max: 30, Step: 0.5, Default: 6},
	{ID: "floor", Label: "откуда нарастать, дБ", Min: -40, Max: -6, Step: 1, Default: -30},
}

// dropSlices — на сколько кусков режется торможение: atempo держит один темп
// на кусок, поэтому плавное замедление — лесенка из коротких кусков.
const dropSlices = 8

// dropXfade — перекрёстный переход между соседними кусками торможения, с.
// Встык (concat) куски давали слышные швы: atempo обрабатывает каждый кусок
// отдельно, и на границе фаза волны рвётся (прослушка драйва: «склейка слышится»).
const dropXfade = 0.03

// dropGraph — «провал»: с отметки start кусок dur секунд звучит всё медленнее
// (темп от 1 до slow, без смены высоты) и затихает до нуля; затем вставляется
// тишина gap секунд, и остаток трека идёт в обычном темпе, нарастая за rise
// секунд от floor дБ до полной громкости. Трек удлиняется на растяжку и паузу.
// Куски торможения и начало трека сшиты перекрёстными переходами dropXfade:
// каждый кусок начинается чуть раньше своей границы (на dropXfade выходного
// звука), так что длина результата та же, что при склейке встык.
// Ставить start на начало такта: стык торможения — ровно на отметке.
func dropGraph(p map[string]float64) string {
	start, dur, slow, gap, rise, floor := p["start"], p["dur"], p["slow"], p["gap"], p["rise"], p["floor"]
	var b strings.Builder
	fmt.Fprintf(&b, "[0:a]asplit=%d", dropSlices+2)
	for i := 0; i < dropSlices+2; i++ {
		fmt.Fprintf(&b, "[s%d]", i)
	}
	// apad=whole_dur — пустой кусок (отметка за концом трека, start=0) дополняется
	// тишиной до длины перехода: acrossfade на пустом входе роняет ffmpeg;
	// обычные куски длиннее перехода и не меняются
	fmt.Fprintf(&b, ";[s0]atrim=0:%g,asetpts=PTS-STARTPTS,apad=whole_dur=%g[h]", start, dropXfade)
	step := dur / dropSlices
	for i := 0; i < dropSlices; i++ {
		// темп куска — по его середине; громкость линейно от 1 до 0 по всему окну
		tempo := 1 - (1-slow)*(float64(i)+0.5)/dropSlices
		a0, a1 := 1-float64(i)/dropSlices, 1-float64(i+1)/dropSlices
		from, to := start+float64(i)*step, start+float64(i+1)*step
		// захлёст в начале куска: dropXfade выходного звука = dropXfade·tempo исходного
		lead := math.Min(dropXfade*tempo, from)
		pre := lead / tempo           // захлёст в секундах выходного звука
		outLen := (to - from) / tempo // длина куска после растяжки
		fade := fmt.Sprintf("volume='%g+(%g)*clip((t-%g)/%g,0,1)':eval=frame", a0, a1-a0, pre, outLen)
		fmt.Fprintf(&b, ";[s%d]atrim=%g:%g,asetpts=PTS-STARTPTS,atempo=%g,asetnsamples=n=%d:p=0,%s,apad=whole_dur=%g[d%d]",
			i+1, from-lead, to, tempo, envFrame, fade, dropXfade, i)
	}
	fmt.Fprintf(&b, ";[s%d]atrim=start=%g,asetpts=PTS-STARTPTS", dropSlices+1, start+dur)
	if rise > 0 {
		fmt.Fprintf(&b, ",asetnsamples=n=%d:p=0,volume='if(lt(t,%[2]g),pow(10,(%[3]g)*(1-t/%[2]g)/20),1)':eval=frame",
			envFrame, rise, floor)
	}
	// пауза — задержкой хвоста, а не apad последнего куска: apad на пустом куске
	// (отметка за концом трека) роняет весь граф ffmpeg
	if gap > 0 {
		fmt.Fprintf(&b, ",adelay=delays=%g:all=1", gap*1000)
	}
	b.WriteString("[t]")
	// начало трека и куски торможения — перекрёстными переходами; хвост после
	// торможения стыкуется встык: на стыке звук уже затих до нуля
	prev := "h"
	for i := 0; i < dropSlices; i++ {
		fmt.Fprintf(&b, ";[%s][d%d]acrossfade=d=%g:c1=tri:c2=tri[c%d]", prev, i, dropXfade, i)
		prev = fmt.Sprintf("c%d", i)
	}
	fmt.Fprintf(&b, ";[%s][t]concat=n=2:v=0:a=1[out]", prev)
	return b.String()
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

var masterParams = []Param{
	{ID: "drive", Label: "перегруз (громкость)", Min: 1, Max: 3, Step: 0.1, Default: 1.5},
	{ID: "grit", Label: "песок верхов", Min: 0, Max: 3, Step: 0.1, Default: 1.3},
	{ID: "breath", Label: "разжатие тишины (0 — выкл)", Min: 0, Max: 1.5, Step: 0.05, Default: 1.5},
}

// masterGraph — «мастеринг одним кликом»: разжать кирпич нейромикса (экспандер
// как у «Дыхания»), поднять громкость перегрузом и присыпать песком верхов —
// классический цикл гейн → сатурация → песок → лимитер, но одним проходом.
// Песок — treble-подъём: гармоники даёт клиппинг лимитера, а aexciter в этой
// сборке ffmpeg (9.0.2) подтягивает любой уровень к потолку (замер: вход
// −30 дБ → выход 0 дБ) и убивает разжатие. Замер на #173: RMS −18.5 → −15.4 дБ,
// куплет +1.9 / припев +2.0 дБ, пик ≤ 0.94, без клипа.
func masterGraph(p map[string]float64) string {
	a := p["breath"]
	return fmt.Sprintf(
		"[0:a]compand=attacks=0.02:decays=0.3:points=-90/-90|-40/%.1f|-22/%.1f|-8/-8|0/-0.5:soft-knee=6,"+
			"volume=%.2f,alimiter=limit=0.55:attack=1:release=8:level=disabled,"+
			"treble=g=%.1f:f=3500,"+
			"alimiter=limit=0.94:attack=1:release=15:level=disabled[out]",
		-40-6*a, -22-2*a, p["drive"], p["grit"]*2)
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

var dewhistleParams = []Param{
	{ID: "freq", Label: "частота свиста, Гц (найти — «найти свист»)", Min: 1000, Max: 16000, Step: 5, Default: 5000},
	{ID: "freq2", Label: "ещё тон, Гц (0 — нет)", Min: 0, Max: 16000, Step: 5, Default: 0},
	{ID: "freq3", Label: "ещё тон, Гц (0 — нет)", Min: 0, Max: 16000, Step: 5, Default: 0},
	{ID: "depth", Label: "глубина выреза, дБ", Min: 6, Max: 40, Step: 1, Default: 30},
	{ID: "width", Label: "ширина выреза, Гц", Min: 10, Max: 400, Step: 5, Default: 60},
	{ID: "harmonics", Label: "гармоники (1 — только сам тон)", Min: 1, Max: 3, Step: 1, Default: 1},
	{ID: "start", Label: "с какой секунды", Min: 0, Max: 600, Step: 0.5, Default: 0},
	{ID: "end", Label: "по какую секунду (0 — до конца)", Min: 0, Max: 600, Step: 0.5, Default: 0},
}

// dewhistleXfade — переход сухой↔вырезанный на краях окна, с (без щелчка)
const dewhistleXfade = 0.1

// dewhistleGraph — «Убрать свист»: узкие вырезы (equalizer, ширина в Гц) на
// частоте тона и его гармониках ниже 20 кГц; только в окне start…end — вне окна
// звук сухой, переход по времени (как withFrom). Модель иногда рождает узкий
// «свист» в гитарах/синтах (#245: 5265 Гц, до +40 дБ над соседями в стеме).
func dewhistleGraph(p map[string]float64) string {
	// свист бывает не один (#252: после 5265 Гц остались 3526 и 4430) — до трёх
	// тонов за проход; 0 — тон не задан
	var notch []string
	for _, base := range []float64{p["freq"], p["freq2"], p["freq3"]} {
		if base <= 0 {
			continue
		}
		for k := 1; k <= int(math.Round(p["harmonics"])); k++ {
			if f := base * float64(k); f < 20000 {
				notch = append(notch, fmt.Sprintf("equalizer=f=%g:t=h:w=%g:g=%g", f, p["width"], -p["depth"]))
			}
		}
	}
	chain := strings.Join(notch, ",")
	start, end := p["start"], p["end"]
	if start <= 0 && end <= 0 {
		return "[0:a]" + chain + "[out]"
	}
	half := dewhistleXfade / 2
	// доля вырезанного сигнала: 0 → 1 на start, 1 → 0 на end (end 0 — до конца)
	gate := fmt.Sprintf("clip((t-%g)/%g,0,1)", start-half, dewhistleXfade)
	if start <= 0 {
		gate = "1"
	}
	if end > 0 {
		gate = fmt.Sprintf("min(%s,clip((%g-t)/%g,0,1))", gate, end+half, dewhistleXfade)
	}
	return fmt.Sprintf("[0:a]asplit=2[dw_dry0][dw_wet0];[dw_wet0]%[1]s,volume='%[2]s':eval=frame[dw_wet];"+
		"[dw_dry0]volume='1-%[2]s':eval=frame[dw_dry];"+
		"[dw_dry][dw_wet]amix=inputs=2:duration=first:normalize=0[out]", chain, gate)
}

var softenParams = []Param{
	{ID: "amount", Label: "сила (0 — выкл, 1 — сильно)", Min: 0, Max: 1, Step: 0.05, Default: 0.5},
	{ID: "max", Label: "предел ослабления (0–1)", Min: 0, Max: 1, Step: 0.05, Default: 0.5},
	{ID: "freq", Label: "с каких частот (0 — ниже, 1 — только верх)", Min: 0, Max: 1, Step: 0.05, Default: 0.5},
}

// softenGraph — «Смягчить звон»: де-эссер ffmpeg — прижимает верх (шипящие,
// звон) только в моменты, когда он выпирает; тело голоса не трогает. Для
// дорожки голоса (эффект на дорожку): на общем миксе глушит и тарелки.
func softenGraph(p map[string]float64) string {
	return fmt.Sprintf("[0:a]deesser=i=%g:m=%g:f=%g[out]", p["amount"], p["max"], p["freq"])
}

var megaphoneParams = []Param{
	{ID: "lo", Label: "низ полосы, Гц", Min: 250, Max: 800, Step: 10, Default: 400},
	{ID: "hi", Label: "верх полосы, кГц", Min: 2, Max: 5, Step: 0.1, Default: 3},
	{ID: "drive", Label: "насыщение", Min: 1, Max: 8, Step: 0.1, Default: 5},
	{ID: "ring", Label: "звон рупора", Min: 0, Max: 12, Step: 0.5, Default: 6},
	{ID: "echo", Label: "короткое эхо", Min: 0, Max: 1, Step: 0.05, Default: 0.5},
}

// megaphoneGraph — голос сквозь рупор: узкая полоса, ЗВОН рупора (резонанс
// ~1.8 кГц — без него полосный голос звучит глухим картоном), перегруз,
// короткое эхо «помещения». Полоса стоит и до перегруза (в клип уходит только
// полосный сигнал), и после — клиппинг/биткраш рождают широкополосные гармоники,
// динамик мегофона их не играет (после эха ещё один lowpass — эхо до него
// доходит уже с гармониками). Насыщение — клиппинг в лимитере с attack 0.1 мс
// (обычный лимитер с атакой в миллисекунду на ровном тоне — просто гейн).
func megaphoneGraph(p map[string]float64) string {
	ring := ""
	if p["ring"] > 0.01 {
		ring = fmt.Sprintf("equalizer=f=1800:t=q:w=1.4:g=%g,", p["ring"])
	}
	echo := ""
	if p["echo"] > 0.01 {
		echo = fmt.Sprintf(",aecho=in_gain=1:out_gain=1:delays=55|110:decays=%.2f|%.2f",
			p["echo"], p["echo"]*0.5)
	}
	return fmt.Sprintf("[0:a]highpass=f=%[1]g,highpass=f=%[1]g,lowpass=f=%[2]g,lowpass=f=%[2]g,"+
		"%[5]svolume=%[3]g,"+
		"alimiter=limit=0.3:attack=0.1:release=5:level=disabled,acrusher=bits=10:mix=0.3,"+
		"highpass=f=%[1]g,highpass=f=%[1]g,highpass=f=%[1]g,lowpass=f=%[2]g,lowpass=f=%[2]g,lowpass=f=%[2]g%[4]s,"+
		"lowpass=f=%[2]g,alimiter=limit=0.9:attack=1:release=15:level=disabled[out]",
		p["lo"], p["hi"]*1000, p["drive"], echo, ring)
}

var phoneParams = []Param{
	{ID: "lo", Label: "низ полосы, Гц", Min: 200, Max: 600, Step: 10, Default: 420},
	{ID: "hi", Label: "верх полосы, кГц", Min: 2, Max: 4, Step: 0.1, Default: 2.6},
	{ID: "drive", Label: "хрип трубки", Min: 1, Max: 6, Step: 0.1, Default: 3},
}

// phoneGraph — голос из телефонной трубки: полоса УЖЕ мегафонной (до ~2.6 кГц),
// гнусавый резонанс ~1 кГц, сухо — без эха и биткраша, в отличие от мегофона.
func phoneGraph(p map[string]float64) string {
	return fmt.Sprintf("[0:a]highpass=f=%[1]g,highpass=f=%[1]g,lowpass=f=%[2]g,lowpass=f=%[2]g,"+
		"equalizer=f=1000:t=q:w=1.4:g=4,volume=%[3]g,"+
		"alimiter=limit=0.35:attack=0.1:release=5:level=disabled,"+
		"highpass=f=%[1]g,highpass=f=%[1]g,highpass=f=%[1]g,lowpass=f=%[2]g,lowpass=f=%[2]g,lowpass=f=%[2]g,"+
		"alimiter=limit=0.85:attack=1:release=12:level=disabled[out]",
		p["lo"], p["hi"]*1000, p["drive"])
}

var voiceDriveParams = []Param{
	{ID: "drive", Label: "перегруз, дБ", Min: 0, Max: 30, Step: 1, Default: 22},
	{ID: "mid", Label: "плотность середины (1.5 кГц), дБ", Min: 0, Max: 10, Step: 0.5, Default: 5},
	{ID: "low", Label: "срез низа, Гц", Min: 100, Max: 400, Step: 10, Default: 200},
	{ID: "tape", Label: "верх плёнки, кГц", Min: 5, Max: 12, Step: 0.5, Default: 8.5},
	{ID: "noise", Label: "шум ленты", Min: 0, Max: 0.05, Step: 0.0025, Default: 0.005},
}

// voiceDriveGraph — летовский перегруз голоса, подогнан по эталону (замер
// стема голоса «Гражданской обороны», 2026-10-01): грязь — это ПЛОТНАЯ
// ПЕРЕГРУЖЕННАЯ СЕРЕДИНА и верх, закрытый плёнкой, а не шипящий песок.
// Эталон: <300 Гц −15 дБ, 300–1к −2, 1–4к −5, >4к −17 дБ к сумме, центр ~1 кГц.
// Прежняя цепочка (эксайтер после клипа) давала >4к −5 дБ и центр 2.5 кГц —
// «не тот перегруз». Здесь: срез низа, подъём середины, мягкий клип tanh с
// передискретизацией (без цифрового скрежета), плёночный срез верха —
// на нашем голосе: −14 / −1.9 / −5.3 / −17.3 дБ, центр 900 Гц.
func voiceDriveGraph(p map[string]float64) string {
	return fmt.Sprintf("[0:a]highpass=f=%[1]g,highpass=f=%[1]g,equalizer=f=1500:t=q:w=1:g=%[2]g,"+
		"volume=%[3]gdB,asoftclip=type=tanh:oversample=4,lowpass=f=%[4]g,volume=-%[3]gdB[a];"+
		"anoisesrc=color=pink:amplitude=%[5]g:seed=11,highpass=f=40,lowpass=f=%[4]g[n];"+
		"[a][n]amix=inputs=2:duration=first:normalize=0,"+
		"alimiter=limit=0.92:attack=1:release=15:level=disabled[out]",
		p["low"], p["mid"], p["drive"], p["tape"]*1000, p["noise"])
}

var slapbackParams = []Param{
	{ID: "delay", Label: "задержка повтора, мс", Min: 60, Max: 160, Step: 5, Default: 100},
	{ID: "echo", Label: "громкость повтора", Min: 0, Max: 1, Step: 0.05, Default: 0.6},
}

// slapbackGraph — одиночное эхо (рокабилли, Джек Уайт): сухой голос и один
// повтор через delay мс с ослаблением echo.
func slapbackGraph(p map[string]float64) string {
	return fmt.Sprintf("[0:a]aecho=in_gain=1:out_gain=1:delays=%[1]g:decays=%[2]g[out]",
		p["delay"], p["echo"])
}

var gateParams = []Param{
	{ID: "bpm", Label: "темп трека, BPM", Min: 40, Max: 240, Step: 0.5, Default: 120},
	{ID: "div", Label: "ударов на долю (2 — восьмые, 4 — шестнадцатые)", Min: 1, Max: 8, Step: 1, Default: 4},
	{ID: "duty", Label: "доля открытого звука в ударе", Min: 0.1, Max: 0.9, Step: 0.05, Default: 0.5},
	{ID: "depth", Label: "глубина (1 — полная тишина между ударами)", Min: 0, Max: 1, Step: 0.05, Default: 0.9},
	{ID: "smooth", Label: "мягкость краёв, мс", Min: 1, Max: 30, Step: 1, Default: 5},
	{ID: "offset", Label: "сетка: время любой сильной доли, с («найти сетку»)", Min: 0, Max: 600, Step: 0.005, Default: 0},
}

// gateSamples — длина аудиокадра для гейта: volume с eval=frame считает
// выражение раз на кадр, а кадр ffmpeg по умолчанию ~1024 сэмпла (21 мс на
// 48 кГц) — на шестнадцатых в 138 BPM (109 мс) край ступенькой. 64 сэмпла — ~1 мс;
// p=0 — последний кадр не дополняется тишиной (иначе выход длиннее входа).
const gateSamples = 64

// gateGraph — «Ритм-гейт» (транс-гейт): громкость открывается на duty каждого
// удара сетки (bpm × div ударов в минуту, сетка от offset) и закрывается до
// 1−depth между ними; края — линейные рампы smooth мс. Тянущийся звук (пэд,
// гитара с сустейном) становится пульсирующим синт-ритмом на тех же аккордах.
func gateGraph(p map[string]float64) string {
	period := 60 / (p["bpm"] * p["div"]) // длина удара, с
	// shift — целое число ударов не меньше offset: t−offset+shift ≥ 0, и mod не
	// уходит в минус в начале трека; фаза сетки от прибавки целых ударов не меняется
	shift := period * math.Ceil(p["offset"]/period)
	phase := fmt.Sprintf("mod(t-%g+%g,%g)/%g", p["offset"], shift, period, period) // 0..1 внутри удара
	ramp := math.Min(p["smooth"]/1000/period, p["duty"]/2)                         // длина края в долях удара
	// open: 0 вне окна [0, duty], 1 внутри, линейно на краях
	open := fmt.Sprintf("clip(min(%[1]s/%[2]g,(%[3]g-%[1]s)/%[2]g),0,1)", phase, ramp, p["duty"])
	return fmt.Sprintf("[0:a]asetnsamples=n=%d:p=0,volume='1-%g*(1-%s)':eval=frame[out]", gateSamples, p["depth"], open)
}

var levelParams = []Param{
	{ID: "gain", Label: "громкость, дБ (+ громче, − тише)", Min: -12, Max: 12, Step: 0.1, Default: 0},
	{ID: "ceiling", Label: "потолок пиков, дБ", Min: -3, Max: -0.1, Step: 0.1, Default: -1.5},
}

// levelGraph — «Громкость альбома»: только сдвиг громкости и ограничитель пиков
// на потолке ceiling — без перегруза, песка и разжатия, тембр и динамика те же.
// Для выравнивания треков альбома к одной громкости (LUFS): пики выше потолка
// при сжатии в mp3/стриминге хрипят, ограничитель трогает только их. latency=1 —
// задержка упреждения ограничителя компенсируется: звук не сдвигается (тест поймал 5 мс).
func levelGraph(p map[string]float64) string {
	return fmt.Sprintf("[0:a]volume=%gdB,alimiter=limit=%g:attack=5:release=50:level=disabled:latency=1[out]",
		p["gain"], math.Pow(10, p["ceiling"]/20))
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
		ID: "master", Name: "Мастеринг",
		Note: "«Как настоящая пластинка»: разжимает кирпич, поднимает громкость перегрузом, " +
			"добавляет песок верхов — одним проходом. В разделе эффектов есть кнопка применения одним кликом.",
		Params: masterParams, graph: masterGraph,
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
		ID: "drop", Name: "Провал",
		Note: "С отметки трек замедляется и затихает, пауза, затем дальше в обычном темпе нарастает из тишины — " +
			"«провал → разгон» перед финальным припевом. Трек удлиняется на растяжку и паузу.",
		Params: dropParams, graph: dropGraph,
	},
	{
		ID: "cresc", Name: "Громкость к концу",
		Note:   "Плавный подъём громкости с выбранной секунды — финал звучит крупнее; лимитер держит пики.",
		Params: crescParams, graph: crescGraph,
	},
	{
		ID: "soften", Name: "Смягчить звон",
		Note: "Де-эссер: прижимает звонкие и шипящие места голоса, тело голоса не трогает. " +
			"Лучше на дорожку «голос», а не на весь трек (иначе притихнут и тарелки).",
		Params: softenParams, graph: softenGraph,
	},
	{
		ID: "dewhistle", Name: "Убрать свист",
		Note: "Узкий вырез частоты свиста/писка (и гармоник) в выбранном окне — музыка рядом почти не меняется. " +
			"Частоту подскажет «найти свист».",
		Params: dewhistleParams, graph: dewhistleGraph,
	},
	{
		ID: "warp", Name: "Варп-лента",
		Note:   "Глубокое завывание и дрожь, глухой верх — плёночный брак как приём.",
		Params: warpParams, graph: warpGraph,
	},
	{
		ID: "level", Name: "Громкость альбома",
		Note: "Только громкость на заданные дБ и ограничитель пиков на потолке — без перегруза, " +
			"песка и разжатия: выровнять треки альбома к одной громкости, тембр и динамика те же.",
		Params: levelParams, graph: levelGraph,
	},
	{
		ID: "gate", Name: "Ритм-гейт",
		Note: "Громкость открывается и закрывается в такт (bpm × удары на долю, сетка от сдвига): " +
			"тянущийся звук — пэд, гитара с сустейном — становится пульсирующим синт-ритмом на тех же " +
			"аккордах. Ставить на дорожку (обычно «прочее») в окне.",
		Params: gateParams, graph: gateGraph,
	},
	{
		ID: "megaphone", Name: "Мегафон",
		Note: "Голос сквозь рупор: узкая полоса, перегруз, короткое эхо помещения (Джек Уайт). " +
			"Цепочка для дорожки «голос» — дорожка выбирается сама.",
		Params: megaphoneParams, graph: megaphoneGraph, Voice: true,
	},
	{
		ID: "phone", Name: "Телефон",
		Note: "Голос из телефонной трубки: полоса ещё уже, сухо, лёгкий хрип. " +
			"Цепочка для дорожки «голос» — дорожка выбирается сама.",
		Params: phoneParams, graph: phoneGraph, Voice: true,
	},
	{
		ID: "voice-drive", Name: "Перегруз голоса",
		Note: "Лоуфай-перегруз голоса: клиппинг, биткраш, глухой верх, шум ленты (Летов). " +
			"Цепочка для дорожки «голос» — дорожка выбирается сама.",
		Params: voiceDriveParams, graph: voiceDriveGraph, Voice: true,
	},
	{
		ID: "slapback", Name: "Слэпбэк",
		Note: "Одиночное эхо 80–120 мс — рокабилли/Джек Уайт: голос с повтором. " +
			"Цепочка для дорожки «голос» — дорожка выбирается сама.",
		Params: slapbackParams, graph: slapbackGraph, Voice: true,
	},
	{
		ID: "reverb-room", Name: "Реверб: комната",
		Note: "Небольшое помещение: плотные ранние отражения, короткий хвост — инструмент «в комнате», а не в вакууме. " +
			"На голос — эффектом на дорожку «голос».",
		Params: reverbParams(0.3, 1.5, 0.6, 10, 8), graph: reverbGraph("room"), tail: reverbTail, Pedal: true,
	},
	{
		ID: "reverb-hall", Name: "Реверб: зал",
		Note:   "Большой зал: хвост нарастает и долго гаснет — объём и глубина для медленных частей и голоса.",
		Params: reverbParams(1, 6, 2.5, 30, 6), graph: reverbGraph("hall"), tail: reverbTail, Pedal: true,
	},
	{
		ID: "reverb-plate", Name: "Реверб: плейт",
		Note:   "Студийная пластина: сразу плотный яркий хвост без отражений — классика для голоса и малого барабана.",
		Params: reverbParams(0.5, 4, 1.6, 5, 12), graph: reverbGraph("plate"), tail: reverbTail, Pedal: true,
	},
	{
		ID: "reverb-spring", Name: "Реверб: пружина",
		Note:   "Гитарный пружинный ревер: узкая полоса и металлический «дребезг» — сёрф, рокабилли, даб.",
		Params: reverbParams(0.5, 3, 1.2, 0, 4.5), graph: reverbGraph("spring"), tail: reverbTail, Pedal: true,
	},
	{
		ID: "delay", Name: "Дилей в темп",
		Note: "Повторы в долю трека (1/4, 1/8, 1/8 с точкой…), затухают с обратной связью, пинг-понг между каналами, " +
			"верх повторов срезан. Темп подскажет «найти сетку».",
		Params: delayParams, graph: delayGraph, tail: delayTail, Pedal: true,
	},
	{
		ID: "width", Name: "Стерео-ширина",
		Note:   "Шире или уже стереобазы через середину/бока; низ остаётся в центре. 0 — моно.",
		Params: widthParams, graph: widthGraph,
	},
	{
		ID: "haas", Name: "Удвоение (Хаас)",
		Note:   "Один канал позже на 10–30 мс: звук один, но широкий — «двойной» голос или гитара.",
		Params: haasParams, graph: haasGraph,
	},
	{
		ID: "chorus", Name: "Хорус",
		Note:   "Несколько слегка «плывущих» копий: звук шире и гуще, как несколько исполнителей.",
		Params: chorusParams, graph: chorusGraph, Pedal: true,
	},
	{
		ID: "flanger", Name: "Фленжер",
		Note:   "Гребёнка, которая ездит по спектру: «реактивный» свист на гитарах, тарелках, синтах.",
		Params: flangerParams, graph: flangerGraph, Pedal: true,
	},
	{
		ID: "phaser", Name: "Фэйзер",
		Note:   "Мягкое «качание» провалов спектра — психоделия 70-х на клавишах и гитаре.",
		Params: phaserParams, graph: phaserGraph, Pedal: true,
	},
	{
		ID: "tremolo", Name: "Тремоло",
		Note:   "Плавное синусное качание громкости (без краёв, в отличие от Ритм-гейта) — винтажный усилитель.",
		Params: tremoloParams, graph: tremoloGraph, Pedal: true,
	},
	{
		ID: "eq", Name: "Эквалайзер",
		Note:   "Полки низа и верха, колокол середины: поправить тембр дорожки или трека. 0 дБ — полоса выключена.",
		Params: eqParams, graph: eqGraph, Pedal: true,
	},
	{
		ID: "sweep", Name: "Свип фильтра",
		Note: "С отметки частота среза едет за N секунд — «разгон перед припевом» (срез низа вверх) или " +
			"«уход в подушку» (срез верха вниз). После окна звук снова сухой или держит конечную частоту.",
		Params: sweepParams, graph: sweepGraph,
	},
	{
		ID: "autowah", Name: "Авто-вау",
		Note:   "Полосовой фильтр качается между двумя частотами — «вау-вау» на гитаре или клавишах (качание, без слежения за громкостью).",
		Params: autowahParams, graph: autowahGraph, Pedal: true,
	},
	{
		ID: "fade", Name: "Нарастание/затухание",
		Note:   "Плавное нарастание с начала трека и/или затухание с отметки до тишины.",
		Params: fadeParams, graph: fadeGraph,
	},
	{
		ID: "multiband", Name: "Многополосный компрессор",
		Note:   "Три полосы со своим компрессором: плотнее и ровнее, громкий бас не прижимает голос и тарелки.",
		Params: multibandParams, graph: multibandGraph,
	},
	{
		ID: "ducking", Name: "Ducking от барабанов",
		Note: "Дорожка приседает на каждом ударе барабанов — «качающий» микс, бочка пробивается. " +
			"Только эффектом на дорожку (обычно «прочее» или бас): ключ — барабаны трека.",
		Params: duckingParams, graph: duckingGraph, Key: duckingKey,
	},
	{
		ID: "pitch", Name: "Транспонирование",
		Note:   "Выше или ниже на полутоны без смены темпа (rubberband), форманты голоса сохраняются.",
		Params: pitchParams, graph: pitchGraph,
	},
	{
		ID: "octaver", Name: "Октавер",
		Note:   "Подмешивает копию на октаву ниже и/или выше — толще бас, «органный» голос.",
		Params: octaverParams, graph: octaverGraph, Pedal: true,
	},
	{
		ID: "reverse", Name: "Реверс к отметке",
		Note: "Перед отметкой звучит кусок задом наперёд: реверс-тарелка, «втягивающаяся» в удар, " +
			"или реверс-хвост. Длина трека не меняется.",
		Params: reverseParams, graph: reverseGraph,
	},
	{
		ID: "stutter", Name: "Статтер",
		Note:   "С отметки один удар сетки повторяется N раз подряд — электронное «заикание» перед сбивкой.",
		Params: stutterParams, graph: stutterGraph,
	},
	{
		ID: "tape-stop", Name: "Остановка ленты",
		Note:   "С отметки звук замедляется и опускается вниз до остановки, как выключенный магнитофон; потом трек идёт дальше.",
		Params: tapeStopParams, graph: tapeStopGraph,
	},
	{
		ID: "vinyl", Name: "Винил",
		Note:   "Треск и щелчки пластинки, тихий шум, чуть закрытый верх.",
		Params: vinylParams, graph: vinylGraph,
	},
	{
		ID: "od-ts", Name: "Овердрайв (Tube Screamer)",
		Note:   "Тёплый перегруз с горбом середины: гитара выходит вперёд и не гудит на низах — блюз, классик-рок, соло.",
		Params: driveParams(18, 40, 3.5), graph: odTSGraph, Pedal: true, match: true,
	},
	{
		ID: "fuzz-muff", Name: "Фузз (Big Muff)",
		Note:   "Толстый фузз с огромным сустейном и провалом середины — стена гитар, шугейз, стоунер.",
		Params: muffParams, graph: muffGraph, Pedal: true, match: true,
	},
	{
		ID: "dist-rat", Name: "Дисторшн (RAT)",
		Note:   "Жёсткий резкий дисторшн, «фильтр» срезает верх — панк, гранж, нойз.",
		Params: driveParams(28, 45, 4), graph: ratGraph, Pedal: true, match: true,
	},
	{
		ID: "fuzz-octave", Name: "Октавный фузз",
		Note:   "Фузз с октавой вверх (как Octavia у Хендрикса): звенящий, «синтезаторный» на соло.",
		Params: octFuzzParams, graph: octFuzzGraph, Pedal: true, match: true,
	},
	{
		ID: "boost", Name: "Бустер",
		Note:   "Чистый подъём громкости и чуть верха; перед перегрузом — плотнее и злее.",
		Params: boostParams, graph: boostGraph, Pedal: true,
	},
	{
		ID: "univibe", Name: "Uni-Vibe",
		Note:   "Медленное «вязкое» качание фэйзера — Хендрикс, Гилмор, психоделия.",
		Params: univibeParams, graph: univibeGraph, Pedal: true,
	},
	{
		ID: "ringmod", Name: "Кольцевой модулятор",
		Note:   "Звук умножается на тон: вместо нот — металлические суммы и разности, «робот», колокола.",
		Params: ringmodParams, graph: ringmodGraph, Pedal: true,
	},
	{
		ID: "noise-gate", Name: "Гейт от шума",
		Note:   "Между нотами — тишина: убирает гул и шум перегруза в паузах, ноты целы.",
		Params: noiseGateParams, graph: noiseGateGraph, Pedal: true,
	},
	{
		ID: "comp-pedal", Name: "Компрессор (педаль)",
		Note:   "Тихие и громкие ноты ровнее, ноты тянутся дольше — кантри, фанк, чистые партии.",
		Params: compPedalParams, graph: compPedalGraph, Pedal: true,
	},
	{
		ID: "cab", Name: "Кабинет",
		Note:   "Гитарный динамик: срезает низ и верх, которых нет у кабинета, — перегруз звучит «гитарно», а не жужжит.",
		Params: cabParams, graph: cabGraph, Pedal: true,
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
	p := c.values(params)
	g := c.graph(p)
	if mix, ok := p[mixParamID]; ok {
		g = dryWet(g, mix)
	}
	return withFrom(g, p[fromParamID])
}

// TailSec — хвост эффекта после конца звука, с (0 — без хвоста): эффект на
// дорожку в окне продлевает обработанную дорожку на хвост.
func (c *Chain) TailSec(params map[string]float64) float64 {
	if c.tail == nil {
		return 0
	}
	return c.tail(c.values(params))
}

// values — параметры цепочки: недостающие по умолчанию, зажатые в диапазон.
func (c *Chain) values(params map[string]float64) map[string]float64 {
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
	return p
}

// fromParamID — общий параметр «с какой секунды»: эффект включается с
// отметки (например, «жёстче с третьего припева»), до неё трек нетронут.
// Цепочки со своей отметкой (параметр start) его не получают.
const (
	fromParamID = "from"
	fromXfade   = 0.1 // переход на стыке, с — без щелчка
)

var fromParam = Param{ID: fromParamID, Label: "с какой секунды (0 — весь трек)", Min: 0, Max: 600, Step: 0.5, Default: 0}

// mixParamID — параметр «сухой/обработанный»: доля эффекта в финальном микше
// (0 — сухой звук, 1 — только обработанный). Голосовые цепочки получают общий,
// остальные (хорус, удвоение…) объявляют свой с удобным дефолтом.
const mixParamID = "mix"

var mixParam = Param{ID: mixParamID, Label: "сухой/обработанный (доля эффекта)", Min: 0, Max: 1, Step: 0.05, Default: 1}

func init() {
	for i := range chains {
		hasStart, hasMix := false, false
		for _, prm := range chains[i].Params {
			hasStart = hasStart || prm.ID == "start"
			hasMix = hasMix || prm.ID == mixParamID
		}
		if !hasStart {
			chains[i].Params = append(chains[i].Params, fromParam)
		}
		if chains[i].Voice && !hasMix {
			chains[i].Params = append(chains[i].Params, mixParam)
		}
	}
}

// dryWet — доля эффекта (mix): линейный микс сухого
// сигнала и графа цепочки. mix ≥ 1 — только обработанный сигнал, граф без
// изменений; mix = 0 — сухой голос, эффект выключен (шум/эхо внутри графа
// гасятся вместе с веткой).
func dryWet(graph string, mix float64) string {
	if mix >= 1 {
		return graph
	}
	wet := strings.Replace(graph, "[0:a]", "[vx_b]", 1)
	wet = strings.TrimSuffix(wet, "[out]") + "[vx_w0]"
	return fmt.Sprintf("[0:a]%[4]sasplit=2[vx_d][vx_b];%[1]s;"+
		"[vx_d]volume=%.2f[vx_dry];[vx_w0]volume=%.2f[vx_wet];"+
		"[vx_dry][vx_wet]amix=inputs=2:duration=first:normalize=0[out]",
		wet, 1-mix, mix, stereoDry(graph))
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
	return fmt.Sprintf("[0:a]%[5]sasplit=2[from_dry0][from_b];%[1]s;"+
		"[from_dry0]volume='clip((%[2]g-t)/%[4]g,0,1)':eval=frame[from_dry];"+
		"[from_wet0]volume='clip((t-%[3]g)/%[4]g,0,1)':eval=frame[from_wet];"+
		"[from_dry][from_wet]amix=inputs=2:duration=first:normalize=0[out]",
		wet, from+half, from-half, fromXfade, stereoDry(graph))
}

// stereoUpmix — начало графа стерео-цепочки (реверб, дилей, ширина, Хаас):
// моно-вход разводится на два канала.
const stereoUpmix = "aformat=channel_layouts=stereo,"

// stereoDry — сухой ветке стерео-цепочки тоже два канала: amix берёт
// раскладку первого входа, и моно-сухой сводил стерео-эффект в моно (Хаас
// на моно-входе давал моно, mix переставал быть линейным).
func stereoDry(graph string) string {
	if strings.HasPrefix(graph, "[0:a]"+stereoUpmix) {
		return stereoUpmix
	}
	return ""
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
