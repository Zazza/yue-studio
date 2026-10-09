package studio

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"yue-studio/internal/dsp"
	"yue-studio/internal/yue"
)

// SectionSpec — замена дорожек трека в окне на дорожки перерендера куска.
// Модель рендерит кусок целой группой (барабаны держат грув), а в треке
// меняются только нужные стемы: остальное звучит как было, голос — всегда
// родной. Замена целого куска давала слышный шов («другой дубль»), наложение
// одиночной партии — чужой грув и лишний голос (замеры в истории задачи).
type SectionSpec struct {
	// рендер куска группой; 0 — «громкость дорожек»: дорожки Stems родителя
	// (можно и vocals) в окне меняются на Db без рендера: −100 — заглушить
	// («только барабаны»), +6 — громче («барабаны громче» в заходе после провала)
	ChildID int64    `json:"child_id"`
	From    float64  `json:"from"` // окно трека, где меняются дорожки, с
	To      float64  `json:"to"`
	Lead    float64  `json:"lead"`     // секунд плана в рендере до From (такт контекста)
	BeatSec float64  `json:"beat_sec"` // длина доли по плану; 0 → defaultBeatSec
	Stems   []string `json:"stems"`    // drums/bass/other; vocals игнорируется
	Db      float64  `json:"db"`       // громкость новой дорожки относительно старой, дБ
	FadeIn  float64  `json:"fade_in"`  // плавный вход, заканчивается в From; 0 → доля
	FadeOut float64  `json:"fade_out"` // плавный выход, начинается в To; 0 → доля
	// Revoice — «перепеть»: разрешить замену голоса (Stems ["vocals"]) — новый
	// рендер той же песни с другим текстом/голосом ложится на минус трека.
	// Без флага голос не заменяется никогда.
	Revoice bool `json:"revoice"`
	// KeepHighHz > 0 — старая дорожка вычитается только ниже этой частоты:
	// у сбивки хэт и тарелки оригинала остаются (прослушка: без этого трек «глохнет»)
	KeepHighHz float64 `json:"keep_high_hz"`
	// Chain (+ Params) при ChildID 0 — эффект на дорожку: DSP-цепочка
	// применяется к дорожкам Stems трека в окне [From, To) (To ≤ 0 — до конца);
	// в трек добавляется разница «обработанная − исходная» дорожка, поэтому
	// остальное не меняется даже при утечках demucs (звон голоса на #254:
	// обработка микса глушила и гитары)
	Chain  string             `json:"chain,omitempty"`
	Params map[string]float64 `json:"params,omitempty"`
	// Steps при ChildID 0 — вместо Chain цепочка эффектов по порядку (педалборд):
	// выход шага — вход следующего, а не сумма разниц отдельных эффектов
	Steps []dsp.Step `json:"steps,omitempty"`
	// Envelope при ChildID 0 — линия громкости дорожек Stems по всему треку
	// (точки время → дБ, между ними линейно в дБ); как у эффекта, в трек
	// добавляется разница «дорожка с линией − дорожка»
	Envelope []dsp.EnvPoint `json:"envelope,omitempty"`
	// Engine при ChildID 0 — цепочка звукового движка воркера (JSON-блоки, как у
	// fx_apply) на дорожки Stems в окне: считается на воркере, в трек — разница
	// «обработанная − исходная», как у Chain/Steps
	Engine []map[string]any `json:"engine,omitempty"`
	// Add у записи Engine — добавить обработанный кусок поверх трека, исходную дорожку не вычитать
	// (синт-партия по аккордам); дорожка "mix" (весь трек как вход, без разделения) — только с Add
	Add bool `json:"add,omitempty"`
	// Place — место в стерео. У записи-добавления (Add) — место её партии. Без Engine/Chain/Steps/
	// Envelope при ChildID 0 и одной дорожке — запись «место дорожки»: матрица ложится на все правки
	// этой дорожки и на разницу «исходная → на месте» — итог дорожки M·(звук после всех правок)
	Place *yue.Place `json:"place,omitempty"`
	// Master у записи Engine — мастер: цепочка движка на весь собранный микс на воркере (одна на трек)
	Master bool `json:"master,omitempty"`
}

// isPlace — запись «место дорожки»
func (s SectionSpec) isPlace() bool {
	return s.Place != nil && s.ChildID == 0 && !s.Add && !s.Master && len(s.Engine) == 0 && s.Chain == "" &&
		len(s.Steps) == 0 && len(s.Envelope) == 0
}

// RebuildResult — новый вариант трека и отчёт по заменам.
type RebuildResult struct {
	Variant *yue.DspVariant `json:"variant"`
	Inserts []InsertReport  `json:"inserts"`
}

// InsertReport — куда встал рендер куска.
type InsertReport struct {
	ChildID  int64   `json:"child_id"`
	StartSec float64 `json:"start_sec"` // где в треке начало рендера
	Aligned  bool    `json:"aligned"`   // true — по бочке; false — по плану (From − Lead)
	Score    float64 `json:"score"`
	Gain     float64 `json:"gain"` // гейн первой заменённой дорожки
}

const (
	// defaultBeatSec — доля при 120 BPM, если план не передал темп
	defaultBeatSec = 0.5
	// gainRate — частота анализа уровня (RMS) дорожек
	gainRate = 16000
	// engineGridSec — шаг окна движка: 10 мс = 441 сэмпл при 44,1 кГц и 480 при 48 кГц
	engineGridSec = 0.01
	// tmpPattern — файлы конвейера живут в своём temp-каталоге, имя не важно
	tmpPattern = "*.flac"
	// опора громкости для «молчащего» окна: уровень старой дорожки перед окном
	// (длиной с окно + запас); окно тише него в 4 раза (−12 дБ) — молчит
	ctxExtraSec       = 4.0
	silentWindowRatio = 0.25
	// muteFadeSec — края «громкости дорожек» по умолчанию: коротко, без щелчка
	muteFadeSec = 0.05
	// muteDb — ниже этого Db дорожка глушится полностью
	muteDb = -60.0
)

// swappable — дорожки, которые можно менять; голос не заменяется никогда
// (заглушить его можно — режим ChildID 0)
var swappable = []string{"drums", "bass", "other"}

// mutable — дорожки, которые можно заглушить
var mutable = []string{"drums", "bass", "other", "vocals", "guitar", "piano"}

// drumParts — части барабанов (DrumSep, только RoFormer): внутри «барабанов», в сумму не входят
var drumParts = []string{"kick", "snare", "toms", "hh", "ride", "crash"}

// engineStems — дорожки, которые меняет цепочка движка: и части барабанов (замена ударов sampler)
var engineStems = append(append([]string{}, mutable...), drumParts...)

// detailStems — подробные дорожки (6-стемная модель воркера): гитара и клавиши
// внутри «прочего». Заменять их нельзя (у куска своих нет), но эффект,
// громкость и глушение — как у основных: в трек идёт «дорожка − исходная»
var detailStems = []string{"guitar", "piano"}

// stemSet — скачанные стемы одной джобы (имя → путь)
type stemSet map[string]string

// RebuildSections — пересобрать трек со ВСЕМИ заменами с чистого оригинала:
// повторная пересборка поверх прошлого микса наслаивала бы замены и не давала
// менять громкость.
func RebuildSections(ctx context.Context, svc yue.Service, parentID int64, specs []SectionSpec) (*RebuildResult, error) {
	return rebuildSections(ctx, svc, parentID, specs, "")
}

// rebuildSections — пересборка; fname "" — overdub-inst-<последняя вклейка>.flac (микс студии), иначе
// свой файл (пресет звука не должен перезаписывать «микс с правками» студии)
func rebuildSections(ctx context.Context, svc yue.Service, parentID int64, specs []SectionSpec, fname string) (*RebuildResult, error) {
	if len(specs) == 0 {
		return nil, errors.New("нет замен для пересборки")
	}
	label := rebuildLabel(specs)
	all := specs // и записи «место»: их дорожки (части барабанов) тоже скачиваются
	dir, err := os.MkdirTemp("", fmt.Sprintf("yue-sections-%d-*", parentID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	specs, master, places, err := splitMix(specs)
	if err != nil {
		return nil, err
	}
	needStems := len(places) > 0
	for _, sp := range specs {
		engineAdd := sp.Add && len(sp.Engine) > 0
		if slices.Contains(sp.Stems, "mix") && !engineAdd {
			return nil, errors.New("дорожка mix — только для добавления цепочкой движка (add): вычесть весь трек нельзя")
		}
		if !engineAdd || len(sp.Stems) != 1 || sp.Stems[0] != "mix" {
			needStems = true
		}
	}
	base, err := FetchBase(ctx, svc, parentID, dir)
	if err != nil {
		return nil, fmt.Errorf("оригинал #%d: %w", parentID, err)
	}
	// только добавления поверх всего трека (синт) — разделение на дорожки не нужно
	parent := stemSet{}
	if needStems {
		if parent, err = fetchStems(ctx, svc, parentID, dir); err != nil {
			return nil, err
		}
	}
	for _, s := range all { // и место голоса: его вставка — исходная дорожка vocals
		if (s.ChildID == 0 || s.Revoice) && slices.Contains(s.Stems, "vocals") && parent["vocals"] == "" {
			if parent["vocals"], err = FetchTemp(ctx, svc, parentID, "stem-vocals.flac", dir, tmpPattern); err != nil {
				return nil, fmt.Errorf("стем vocals #%d: %w", parentID, err)
			}
		}
	}
	if err := fetchDetailStems(ctx, svc, parentID, all, parent, dir); err != nil {
		return nil, err
	}

	inputs := []string{base}
	var ins []dsp.Insert
	var tags []string // дорожка каждой вставки: на неё ложится место дорожки
	reports := make([]InsertReport, 0, len(specs))
	for i, s := range specs {
		if s.ChildID == 0 {
			// правка без рендера — по дорожке за раз: так у каждой вставки известна её дорожка
			var got []dsp.Insert
			for _, name := range s.Stems {
				one := s
				one.Stems = []string{name}
				var part []dsp.Insert
				switch {
				case s.Chain != "" || len(s.Steps) > 0:
					part, err = stemFxInserts(one, parent, dir, i, &inputs)
				case len(s.Engine) > 0:
					part, err = engineInserts(ctx, svc, parentID, one, parent, base, dir, i, &inputs)
				case len(s.Envelope) > 0:
					part, err = envelopeInserts(one, parent, dir, i, &inputs)
				default:
					part = muteInserts(one, parent, &inputs)
				}
				if err != nil {
					return nil, err
				}
				if s.Add && s.Place != nil {
					m, _ := dsp.PlaceMatrix(s.Place.Pan, s.Place.Width) // проверено в splitMix
					for k := range part {
						part[k].Matrix = &m
					}
				}
				for range part {
					tags = append(tags, name)
				}
				got = append(got, part...)
			}
			rep := InsertReport{Aligned: true}
			if len(got) > 0 && (s.Chain != "" || len(s.Steps) > 0 || len(s.Engine) > 0) {
				rep.Gain = got[0].Gain // гейн первой обработанной дорожки (выравнивание + дБ)
			}
			ins = append(ins, got...)
			reports = append(reports, rep)
			continue
		}
		child, err := fetchStems(ctx, svc, s.ChildID, dir)
		if err != nil {
			return nil, err
		}
		if s.Revoice && slices.Contains(s.Stems, "vocals") {
			if child["vocals"], err = FetchTemp(ctx, svc, s.ChildID, "stem-vocals.flac", dir, tmpPattern); err != nil {
				return nil, fmt.Errorf("стем vocals #%d: %w", s.ChildID, err)
			}
		}
		beat := s.BeatSec
		if beat <= 0 {
			beat = defaultBeatSec
		}
		fadeIn, fadeOut := s.FadeIn, s.FadeOut
		if fadeIn <= 0 {
			fadeIn = beat
		}
		if fadeOut <= 0 {
			fadeOut = beat
		}
		// выравнивание по бочке вместе с тактом контекста: в сбивке своей
		// бочки может не быть, контекст перед окном — обычный бит
		p, err := dsp.MeasureInsert(parent["drums"], child["drums"], s.From-s.Lead, s.To, 0, beat)
		if err != nil {
			p = dsp.Placement{StartSec: s.From - s.Lead, Ratio: 1}
		}
		p.Ratio = 1
		rep := InsertReport{ChildID: s.ChildID, StartSec: p.StartSec, Aligned: p.Aligned, Score: p.Score}
		first := true
		for _, name := range s.Stems {
			if !slices.Contains(swappable, name) && (!s.Revoice || name != "vocals") {
				continue
			}
			gain, err := stemGain(parent[name], child[name], p.StartSec, s, name == "drums")
			if err != nil {
				return nil, err
			}
			if first {
				rep.Gain, first = gain, false
			}
			from, to := s.From-fadeIn, s.To+fadeOut
			// новая дорожка: кусок рендера, звучащий в [from, to] трека
			add := dsp.PlaceInsert(p, from, to, gain)
			add.FadeIn, add.FadeOut = fadeIn, fadeOut
			// старая дорожка: тот же кусок оригинала с обратным знаком
			sub := dsp.Insert{AtSec: from, SkipSec: from, DurSec: to - from, Gain: -1,
				FadeIn: fadeIn, FadeOut: fadeOut, LowpassHz: s.KeepHighHz}
			inputs = append(inputs, child[name], parent[name])
			ins = append(ins, add, sub)
			tags = append(tags, name, name)
		}
		reports = append(reports, rep)
	}
	ins, inputs = placeStems(ins, tags, inputs, places, parent)

	out := dir + "/out.flac"
	if err := dsp.RunInputs(inputs, out, dsp.InsertsGraph(ins)); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	if fname == "" {
		fname = fmt.Sprintf("overdub-inst-%d.flac", lastChild(specs))
	}
	v, err := svc.UploadDsp(ctx, parentID, fname, label, data)
	if err != nil {
		return nil, err
	}
	if master != nil {
		// мастер — на воркере, на весь собранный микс, в тот же файл: студия играет его по имени
		if v, err = svc.ApplyFx(ctx, parentID, yue.FxRequest{Source: "mix", File: v.File, InPlace: true,
			Chain: master.Engine}); err != nil {
			return nil, fmt.Errorf("мастер: %w", err)
		}
		// микс без мастера — для «было/стало» правки мастера в студии (иначе мастер лёг бы дважды)
		if _, err := svc.UploadDsp(ctx, parentID, PremasterFile(fname), label+" · без мастера", data); err != nil {
			return nil, fmt.Errorf("микс без мастера: %w", err)
		}
	}
	return &RebuildResult{Variant: v, Inserts: reports}, nil
}

// PremasterFile — «микс без мастера» рядом с миксом студии: overdub-inst-N.flac → overdub-premaster-N.flac
func PremasterFile(mix string) string {
	return strings.Replace(mix, "overdub-inst-", "overdub-premaster-", 1)
}

// splitMix — из записей пересборки отделить мастер (одна активная) и места дорожек (по одному на
// дорожку); остальные записи — как были. Место только у записи-добавления или своей записью.
func splitMix(specs []SectionSpec) (rest []SectionSpec, master *SectionSpec, places map[string][4]float64, err error) {
	places = map[string][4]float64{}
	for i, s := range specs {
		if s.Place != nil {
			if _, err := dsp.PlaceMatrix(s.Place.Pan, s.Place.Width); err != nil {
				return nil, nil, nil, fmt.Errorf("место: %w", err)
			}
		}
		switch {
		case s.Master:
			if len(s.Engine) == 0 {
				return nil, nil, nil, errors.New("мастер без цепочки движка")
			}
			if master != nil {
				return nil, nil, nil, errors.New("у трека две записи мастера — оставьте одну")
			}
			master = &specs[i]
		case s.isPlace():
			if len(s.Stems) != 1 || !slices.Contains(engineStems, s.Stems[0]) {
				return nil, nil, nil, fmt.Errorf("место — одной дорожке трека (не %v)", s.Stems)
			}
			name := s.Stems[0]
			if _, dup := places[name]; dup {
				return nil, nil, nil, fmt.Errorf("у дорожки %s два места — оставьте одно", name)
			}
			places[name], _ = dsp.PlaceMatrix(s.Place.Pan, s.Place.Width)
		case s.Place != nil && !s.Add:
			return nil, nil, nil, errors.New("место — у партии-добавления или своей записью дорожки, не у замены")
		default:
			rest = append(rest, s)
		}
	}
	return rest, master, places, nil
}

// placeParent — дорожка, внутри которой часть: части барабанов — в drums, гитара и клавиши — в other
func placeParent(name string) string {
	switch {
	case slices.Contains(drumParts, name):
		return "drums"
	case slices.Contains(detailStems, name):
		return "other"
	}
	return ""
}

// placeStems — место дорожки: матрица на каждую вставку этой дорожки (вклейки, вычитания, эффекты,
// громкость) и вставка исходной дорожки целиком с матрицей M − I. Матрица линейна, поэтому итог
// дорожки — M·(исходная + все правки): правки не теряются, исходная не возвращается.
func placeStems(ins []dsp.Insert, tags, inputs []string, places map[string][4]float64, parent stemSet) ([]dsp.Insert, []string) {
	for k, name := range tags {
		// правка части (бочка, гитара) — внутри своей дорожки (барабаны, «прочее»): место дорожки ложится и
		// на неё; своё место части — сначала (M_дорожки·M_части)
		m, ok := places[name]
		if pm, has := places[placeParent(name)]; has {
			if ok {
				m = dsp.MulMatrix(pm, m)
			} else {
				m, ok = pm, true
			}
		}
		if !ok {
			continue
		}
		if ins[k].Matrix != nil {
			m = dsp.MulMatrix(m, *ins[k].Matrix)
		}
		ins[k].Matrix = &m
	}
	names := make([]string, 0, len(places))
	for name := range places {
		names = append(names, name)
	}
	slices.Sort(names) // порядок входов ffmpeg — один и тот же от пересборки к пересборке
	for _, name := range names {
		if parent[name] == "" {
			continue
		}
		m := places[name]
		d := [4]float64{m[0] - 1, m[1], m[2], m[3] - 1}
		inputs = append(inputs, parent[name])
		ins = append(ins, dsp.Insert{Gain: 1, Matrix: &d})
	}
	return ins, inputs
}

// stemLabels — дорожки по-русски, как в студии
var stemLabels = map[string]string{"vocals": "голос", "drums": "барабаны", "bass": "бас", "other": "гитары/синты",
	"guitar": "гитара", "piano": "клавиши"}

// rebuildLabel — что сделано в пересборке, для подписи микса в списках:
// «Перегруз голоса · голос + вклейка #191 · барабаны 1:20–1:28». По имени
// файла (overdub-inst-0) этого не понять, а MCP реестр вклеек студии не ведёт.
func rebuildLabel(specs []SectionSpec) string {
	parts := make([]string, 0, len(specs))
	for _, s := range specs {
		var what string
		switch {
		case s.Master:
			what = "мастер: " + strings.TrimPrefix(EngineLabel(s.Engine), "Движок: ")
		case s.ChildID > 0:
			what = fmt.Sprintf("вклейка #%d", s.ChildID)
		case len(s.Engine) > 0:
			what = EngineLabel(s.Engine)
		case len(s.Steps) > 0:
			what = StepsLabel(s.Steps)
		case s.Chain != "":
			what = s.Chain
			if c := dsp.ByID(s.Chain); c != nil {
				what = c.Name
			}
		case len(s.Envelope) > 0:
			what = "линия громкости"
		case s.Db <= muteDb:
			what = "заглушить"
		default:
			what = fmt.Sprintf("громкость %+g дБ", s.Db)
		}
		stems := make([]string, 0, len(s.Stems))
		for _, n := range s.Stems {
			stems = append(stems, cmp.Or(stemLabels[n], n))
		}
		label := what + " · " + strings.Join(stems, ", ")
		if s.isPlace() {
			label = "место: " + strings.Join(stems, ", ") + " " + placeLabel(*s.Place)
		}
		if win := labelWindow(s.From, s.To); win != "" {
			label += " " + win
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, " + ")
}

// placeLabel — «30 % вправо, ширина 1,4», «по центру»
func placeLabel(p yue.Place) string {
	var side string
	switch {
	case p.Pan > 0:
		side = fmt.Sprintf("%.0f %% вправо", p.Pan*100)
	case p.Pan < 0:
		side = fmt.Sprintf("%.0f %% влево", -p.Pan*100)
	default:
		side = "по центру"
	}
	if p.Width != 1 {
		side += ", ширина " + strings.Replace(strconv.FormatFloat(p.Width, 'f', -1, 64), ".", ",", 1)
	}
	return side
}

// labelWindow — «1:20–1:28», «с 1:20»; весь трек — пусто
func labelWindow(from, to float64) string {
	mmss := func(sec float64) string { return fmt.Sprintf("%d:%02d", int(sec)/60, int(sec)%60) }
	switch {
	case to > 0:
		return mmss(from) + "–" + mmss(to)
	case from > 0:
		return "с " + mmss(from)
	}
	return ""
}

// stemFxInserts — эффект на дорожки трека в окне: дорожка целиком через
// цепочку (эффекты с памятью — эхо, компрессор — «разогреты» к окну), в трек
// ложится обработанная дорожка и та же исходная с обратным знаком, с фейдами.
// Громкость обработанной дорожки выравнивается по RMS исходной в окне, сверху
// дБ из спеки (перегруз/клиппинг сжимает и громчит — без выравнивания голос
// рвёт микс). Эффект — одна цепочка (Chain) или шаги по порядку (Steps).
func stemFxInserts(s SectionSpec, parent stemSet, dir string, idx int, inputs *[]string) ([]dsp.Insert, error) {
	fx, err := planFx(s)
	if err != nil {
		return nil, err
	}
	fadeIn, fadeOut := s.FadeIn, s.FadeOut
	if fadeIn <= 0 {
		fadeIn = muteFadeSec
	}
	if fadeOut <= 0 {
		fadeOut = muteFadeSec
	}
	var out []dsp.Insert
	for _, name := range s.Stems {
		if !slices.Contains(mutable, name) || parent[name] == "" {
			continue
		}
		from, dur := insertWindow(s, fadeIn, fadeOut)
		// хвост (реверб, дилей) звучит после окна: цепочка получает дорожку,
		// уже обрезанную по окну с теми же фейдами, что у вычитаемой исходной, —
		// после To в обработанной остаётся только хвост, сухая не удваивается
		src := parent[name]
		tail := 0.0
		if s.To > 0 {
			tail = fx.tail
		}
		if tail > 0 {
			src = fmt.Sprintf("%s/win-%d-%s.flac", dir, idx, name)
			if err := dsp.Run(parent[name], src, dsp.WindowGraph(from, fadeIn, s.To, fadeOut), nil); err != nil {
				return nil, fmt.Errorf("окно эффекта %s на %s: %w", fx.name, name, err)
			}
		}
		out1 := fmt.Sprintf("%s/fx-%d-%s.flac", dir, idx, name)
		if err := runFx(fx, src, parent, out1); err != nil {
			return nil, fmt.Errorf("эффект %s на %s: %w", fx.name, name, err)
		}
		gain, err := fxGain(fx.voice, fx.extraDb, parent[name], out1, s)
		if err != nil {
			return nil, fmt.Errorf("уровень эффекта %s на %s: %w", fx.name, name, err)
		}
		wet := dsp.Insert{AtSec: from, SkipSec: from, DurSec: dur, Gain: gain, FadeIn: fadeIn, FadeOut: fadeOut}
		if tail > 0 {
			// вход уже с фейдами окна; короткий спад — в самом конце хвоста
			wet = dsp.Insert{AtSec: from, SkipSec: from, DurSec: dur + tail, Gain: gain, FadeOut: muteFadeSec}
		}
		*inputs = append(*inputs, out1, parent[name])
		out = append(out, wet,
			dsp.Insert{AtSec: from, SkipSec: from, DurSec: dur, Gain: -1, FadeIn: fadeIn, FadeOut: fadeOut})
	}
	return out, nil
}

// engineInserts — цепочка звукового движка на дорожки в окне. Дорожка считается на
// воркере превью (кусок окна + хвост реверба/дилея): вход окна с линейными краями
// muteFadeSec — той же формы, что у вычитаемой исходной дорожки (dsp.WindowGraph у
// ffmpeg-эффекта), поэтому края сходятся без щелчка. В трек: кусок +1·дБ с начала
// окна и исходная дорожка −1 в окне с фейдами. Громкость не выравнивается: уровни —
// в блоках движка (усилитель и перегруз сами приводят к входу).
func engineInserts(ctx context.Context, svc yue.Service, parentID int64, s SectionSpec, parent stemSet,
	base, dir string, idx int, inputs *[]string) ([]dsp.Insert, error) {
	// старый воркер (без превью) молча вернул бы дорожку целиком — она легла бы со сдвигом
	if cfg, err := svc.WorkerConfig(ctx); err != nil {
		return nil, fmt.Errorf("движок: настройки воркера: %w", err)
	} else if ok, _ := cfg["fx_preview"].(bool); !ok {
		return nil, errors.New("движок: воркер не умеет превью движка — обновите воркер (make worker)")
	}
	f := muteFadeSec
	from := math.Max(0, s.From-f)
	to := s.To
	if to <= 0 {
		// длина по звуку, а не по заголовку: у импортированных трек — mp3/wav
		pcm, err := dsp.DecodeMono(base, gainRate, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("движок: длина трека: %w", err)
		}
		if to = float64(len(pcm)) / gainRate; to <= 0 {
			return nil, errors.New("движок: не узнать длину трека для окна «до конца»")
		}
	}
	// окно — на сетке engineGridSec: там время — целое число сэмплов и при 44,1, и при 48 кГц.
	// На некруглом времени ffmpeg (atrim округляет к ближайшему сэмплу, adelay отбрасывает
	// дробь) и воркер (round) ставят кусок и вычитаемую дорожку на разные сэмплы — под
	// обработанным звуком остаётся сухая дорожка (кросс-ревью: остаток громче оригинала);
	// конец — вниз, чтобы не выйти за длину трека
	from = math.Round(from/engineGridSec) * engineGridSec
	to = math.Floor(to/engineGridSec) * engineGridSec
	if to <= from {
		return nil, fmt.Errorf("движок: пустое окно %g–%g", s.From, s.To)
	}
	var out []dsp.Insert
	for _, name := range s.Stems {
		mixAdd := name == "mix" && s.Add
		if !mixAdd && (!slices.Contains(engineStems, name) || parent[name] == "") {
			continue
		}
		fromV, toV := from, to
		v, err := svc.ApplyFx(ctx, parentID, yue.FxRequest{Source: name, Chain: s.Engine, From: &fromV, To: &toV,
			Output: "solo", Preview: true, Fade: f, Pad: true, Add: s.Add})
		if err != nil {
			return nil, fmt.Errorf("движок на %s: %w", name, err)
		}
		if !strings.HasPrefix(v.File, "preview-fx-") {
			return nil, fmt.Errorf("движок на %s: воркер вернул %s вместо куска превью — обновите воркер", name, v.File)
		}
		// воркер этапа 3 поле pad молча отбрасывает: кусок пришёл бы с начала окна и лёг на 0:00,
		// а окно дорожки вычлось бы — дыра без ошибки. С pad файл от начала трека — не короче to
		if v.DurationSec+engineGridSec < to {
			return nil, fmt.Errorf("движок на %s: воркер вернул кусок без начала трека (%.2f с при окне до %.2f с) — обновите воркер",
				name, v.DurationSec, to)
		}
		wet, err := FetchTemp(ctx, svc, parentID, v.File, dir, fmt.Sprintf("engine-%d-%s-*.flac", idx, name))
		if err != nil {
			return nil, fmt.Errorf("движок на %s: кусок %s: %w", name, v.File, err)
		}
		// кусок от начала трека (Pad): на место без adelay — тем же отсчётом, что дорожка
		*inputs = append(*inputs, wet)
		out = append(out, dsp.Insert{Gain: math.Pow(10, s.Db/20)})
		if s.Add {
			continue // добавление: исходная дорожка остаётся как есть
		}
		*inputs = append(*inputs, parent[name])
		out = append(out, dsp.Insert{AtSec: from, SkipSec: from, DurSec: to + f - from, Gain: -1, FadeIn: f, FadeOut: f})
	}
	return out, nil
}

// EngineLabel — «Движок: gate → eq → amp» (типы блоков по порядку).
func EngineLabel(chain []map[string]any) string {
	names := make([]string, 0, len(chain))
	for _, b := range chain {
		if t, ok := b["type"].(string); ok {
			names = append(names, t)
		}
	}
	return "Движок: " + strings.Join(names, " → ")
}

// fxPlan — эффект записи пересборки: граф ffmpeg, хвост, голосовой ли
// (выравнивание громкости), дорожка-ключ (только у одиночной цепочки).
type fxPlan struct {
	name    string
	graph   string
	tail    float64
	voice   bool    // выравнивать громкость по исходной дорожке
	extraDb float64 // поправка сверху (крутилки «громкость» перегрузов)
	key     string
}

func planFx(s SectionSpec) (fxPlan, error) {
	if len(s.Steps) > 0 {
		g, tail, err := dsp.StepsGraph(s.Steps)
		if err != nil {
			return fxPlan{}, err
		}
		match, extraDb := dsp.StepsMatch(s.Steps)
		return fxPlan{name: StepsLabel(s.Steps), graph: g, tail: tail, voice: match, extraDb: extraDb}, nil
	}
	chain := dsp.ByID(s.Chain)
	if chain == nil {
		return fxPlan{}, fmt.Errorf("неизвестный эффект %q", s.Chain)
	}
	match, extraDb := dsp.StepsMatch([]dsp.Step{{Chain: s.Chain, Params: s.Params}})
	return fxPlan{name: s.Chain, graph: chain.FilterGraph(s.Params), tail: chain.TailSec(s.Params),
		voice: match, extraDb: extraDb, key: chain.Key}, nil
}

// StepsLabel — «Фузз (Big Muff) → Хорус → Реверб: зал» (выключенные пропущены).
func StepsLabel(steps []dsp.Step) string {
	var names []string
	for _, st := range steps {
		if st.Off {
			continue
		}
		name := st.Chain
		if c := dsp.ByID(st.Chain); c != nil {
			name = c.Name
		}
		names = append(names, name)
	}
	return strings.Join(names, " → ")
}

// runFx — эффект на дорожку src; цепочка с ключом (ducking) получает вторым
// входом дорожку-ключ трека.
func runFx(fx fxPlan, src string, parent stemSet, out string) error {
	if fx.key == "" {
		return dsp.Run(src, out, fx.graph, nil)
	}
	key := parent[fx.key]
	if key == "" {
		return fmt.Errorf("нет дорожки-ключа %q", fx.key)
	}
	return dsp.RunInputs([]string{src, key}, out, fx.graph)
}

// envelopeInserts — линия громкости на дорожки Stems: дорожка целиком через
// EnvelopeGraph, в трек — она же минус исходная (остальное не меняется).
func envelopeInserts(s SectionSpec, parent stemSet, dir string, idx int, inputs *[]string) ([]dsp.Insert, error) {
	pts, err := dsp.NormalizeEnvelope(s.Envelope)
	if err != nil {
		return nil, err
	}
	var out []dsp.Insert
	for _, name := range s.Stems {
		if !slices.Contains(mutable, name) || parent[name] == "" {
			continue
		}
		env := fmt.Sprintf("%s/env-%d-%s.flac", dir, idx, name)
		if err := dsp.Run(parent[name], env, dsp.EnvelopeGraph(pts), nil); err != nil {
			return nil, fmt.Errorf("громкость по линии на %s: %w", name, err)
		}
		*inputs = append(*inputs, env, parent[name])
		out = append(out, dsp.Insert{Gain: 1}, dsp.Insert{Gain: -1})
	}
	return out, nil
}

// fxGain — гейн обработанной дорожки. У голосовых цепочек-примочек её RMS
// в окне [From, To) выравнивается по исходной дорожке, сверху дБ пользователя
// (перегруз/клиппинг сжимает и громчит). «Ремонтные» цепочки (вырез свиста,
// де-эссер) уровень дорожки менять не должны: вырез почти всего сигнала
// «добрал» бы гейном до исходного уровня — там только дБ.
func fxGain(voice bool, extraDb float64, oldPath, fxPath string, s SectionSpec) (float64, error) {
	if !voice {
		return math.Pow(10, s.Db/20), nil
	}
	dur := s.To - s.From
	if dur <= 0 {
		dur = 0 // до конца
	}
	ref, err := dsp.DecodeMono(oldPath, gainRate, s.From, dur)
	if err != nil {
		return 0, err
	}
	wet, err := dsp.DecodeMono(fxPath, gainRate, s.From, dur)
	if err != nil {
		return 0, err
	}
	return dsp.InsertGain(dsp.RMS(ref), dsp.RMS(wet), s.Db+extraDb), nil
}

// fxWindowForever — «до конца трека» для окна эффекта (длиннее любой песни)
const fxWindowForever = 3600.0

// insertWindow — окно вставки дорожки с запасом на фейды: начало (не раньше
// 0) и длительность; To ≤ 0 — до конца трека (раньше в «громкости дорожки»
// окно при to=0 выходило отрицательным, и пересборка молча ничего не делала).
func insertWindow(s SectionSpec, fadeIn, fadeOut float64) (from, dur float64) {
	from = math.Max(0, s.From-fadeIn)
	if s.To <= 0 {
		return from, fxWindowForever
	}
	return from, s.To + fadeOut - from
}

// muteInserts — громкость дорожек родителя в окне: к треку добавляется сама
// дорожка с гейном 10^(Db/20)−1 (−1 при Db ≤ −60 — заглушить), края с фейдами.
func muteInserts(s SectionSpec, parent stemSet, inputs *[]string) []dsp.Insert {
	gain := -1.0
	if s.Db > muteDb {
		gain = math.Pow(10, s.Db/20) - 1
	}
	fadeIn, fadeOut := s.FadeIn, s.FadeOut
	if fadeIn <= 0 {
		fadeIn = muteFadeSec
	}
	if fadeOut <= 0 {
		fadeOut = muteFadeSec
	}
	var out []dsp.Insert
	for _, name := range s.Stems {
		if !slices.Contains(mutable, name) || parent[name] == "" {
			continue
		}
		from, dur := insertWindow(s, fadeIn, fadeOut)
		*inputs = append(*inputs, parent[name])
		// KeepHighHz > 0 — меняется только низ дорожки: demucs относит к голосу
		// шумные тарелки, и заглушённая речь уносила их с собой (#258: верх −30 дБ
		// на месте речи, «дыры»)
		out = append(out, dsp.Insert{AtSec: from, SkipSec: from, DurSec: dur, Gain: gain,
			FadeIn: fadeIn, FadeOut: fadeOut, LowpassHz: s.KeepHighHz})
	}
	return out
}

// fetchStems — стемы drums/bass/other джобы; нет — один раз просим воркер
// разложить трек (demucs, секунды). Без стемов замена невозможна — ошибка.
func fetchStems(ctx context.Context, svc yue.Service, id int64, dir string) (stemSet, error) {
	get := func() (stemSet, error) {
		set := stemSet{}
		for _, name := range swappable {
			p, err := FetchTemp(ctx, svc, id, "stem-"+name+".flac", dir, tmpPattern)
			if err != nil {
				return nil, err
			}
			set[name] = p
		}
		return set, nil
	}
	if set, err := get(); err == nil {
		return set, nil
	}
	if _, err := svc.MakeStems(ctx, id); err != nil {
		return nil, fmt.Errorf("стемы #%d: %w", id, err)
	}
	set, err := get()
	if err != nil {
		return nil, fmt.Errorf("стемы #%d после разделения: %w", id, err)
	}
	return set, nil
}

// fetchDetailStems — гитара/клавиши родителя, если их просят записи без
// куска (эффект, громкость). Стемы, сделанные до 6-стемной модели, их не
// имеют: один раз просим воркер разложить трек заново. Нет и после — ошибка,
// а не молчаливый пропуск (эффект «применился», а звук тот же)
func fetchDetailStems(ctx context.Context, svc yue.Service, id int64, specs []SectionSpec, parent stemSet, dir string) error {
	var need []string
	for _, s := range specs {
		if s.ChildID != 0 {
			continue
		}
		for _, n := range s.Stems {
			part := (len(s.Engine) > 0 || s.isPlace()) && slices.Contains(drumParts, n) // части — движку и месту
			if (slices.Contains(detailStems, n) || part) && !slices.Contains(need, n) {
				need = append(need, n)
			}
		}
	}
	get := func() error {
		for _, n := range need {
			if parent[n] != "" {
				continue
			}
			p, err := FetchTemp(ctx, svc, id, "stem-"+n+".flac", dir, tmpPattern)
			if err != nil {
				return err
			}
			parent[n] = p
		}
		return nil
	}
	if len(need) == 0 || get() == nil {
		return nil
	}
	if _, err := svc.MakeStems(ctx, id); err != nil {
		return fmt.Errorf("стемы #%d: %w", id, err)
	}
	if err := get(); err != nil {
		hint := "воркер без 6-стемной модели?"
		for _, n := range need {
			if slices.Contains(drumParts, n) { // части барабанов — только у RoFormer
				hint = "части барабанов есть только у разделения RoFormer: настройки → «Разделение на дорожки» или make_stems model=roformer"
				break
			}
		}
		return fmt.Errorf("дорожка %s #%d не выделилась (%s): %w", strings.Join(need, "/"), id, hint, err)
	}
	return nil
}

// stemGain — гейн новой дорожки: её уровень в окне выравнивается по старой
// (перерендер часто тише — «основной трек как будто стал тише»), сверху дБ.
// Если старая дорожка в окне молчит (провал), опора — её уровень перед окном:
// иначе сбивка после провала выравнивалась под тишину и пропадала (замер:
// гейн ×0.001).
// accent (барабаны): опора — не тише бита перед окном: сбивка — акцент, а в
// окне перед припевом у песни часто свой провал, и дробь выравнивалась под
// полупустое место (прослушка: «барабаны тихие, а должны быть акцентом»).
func stemGain(oldPath, newPath string, start float64, s SectionSpec, accent bool) (float64, error) {
	dur := s.To - s.From
	ref, err := dsp.DecodeMono(oldPath, gainRate, s.From, dur)
	if err != nil {
		return 0, err
	}
	refRMS := dsp.RMS(ref)
	ctxFrom := max(0, s.From-dur-ctxExtraSec)
	if ctx, err := dsp.DecodeMono(oldPath, gainRate, ctxFrom, s.From-ctxFrom); err == nil {
		c := dsp.RMS(ctx)
		if refRMS < c*silentWindowRatio || (accent && refRMS < c) {
			refRMS = c
		}
	}
	cand, err := dsp.DecodeMono(newPath, gainRate, max(0, s.From-start), dur)
	if err != nil {
		return 0, err
	}
	return dsp.InsertGain(refRMS, dsp.RMS(cand), s.Db), nil
}

// lastChild — номер последнего рендера для имени файла результата
// (спеки «заглушить» с ChildID 0 пропускаются; только они — 0)
func lastChild(specs []SectionSpec) int64 {
	for i := len(specs) - 1; i >= 0; i-- {
		if specs[i].ChildID != 0 {
			return specs[i].ChildID
		}
	}
	return 0
}
