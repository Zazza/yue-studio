package studio

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"

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
var mutable = []string{"drums", "bass", "other", "vocals"}

// stemSet — скачанные стемы одной джобы (имя → путь)
type stemSet map[string]string

// RebuildSections — пересобрать трек со ВСЕМИ заменами с чистого оригинала:
// повторная пересборка поверх прошлого микса наслаивала бы замены и не давала
// менять громкость.
func RebuildSections(ctx context.Context, svc yue.Service, parentID int64, specs []SectionSpec) (*RebuildResult, error) {
	if len(specs) == 0 {
		return nil, errors.New("нет замен для пересборки")
	}
	dir, err := os.MkdirTemp("", fmt.Sprintf("yue-sections-%d-*", parentID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	base, err := FetchBase(ctx, svc, parentID, dir)
	if err != nil {
		return nil, fmt.Errorf("оригинал #%d: %w", parentID, err)
	}
	parent, err := fetchStems(ctx, svc, parentID, dir)
	if err != nil {
		return nil, err
	}
	for _, s := range specs {
		if (s.ChildID == 0 || s.Revoice) && slices.Contains(s.Stems, "vocals") && parent["vocals"] == "" {
			if parent["vocals"], err = FetchTemp(ctx, svc, parentID, "stem-vocals.flac", dir, tmpPattern); err != nil {
				return nil, fmt.Errorf("стем vocals #%d: %w", parentID, err)
			}
		}
	}

	inputs := []string{base}
	var ins []dsp.Insert
	reports := make([]InsertReport, 0, len(specs))
	for i, s := range specs {
		if s.ChildID == 0 && s.Chain != "" {
			fx, err := stemFxInserts(s, parent, dir, i, &inputs)
			if err != nil {
				return nil, err
			}
			ins = append(ins, fx...)
			reports = append(reports, InsertReport{Aligned: true})
			continue
		}
		if s.ChildID == 0 {
			ins = append(ins, muteInserts(s, parent, &inputs)...)
			reports = append(reports, InsertReport{Aligned: true})
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
		}
		reports = append(reports, rep)
	}

	out := dir + "/out.flac"
	if err := dsp.RunInputs(inputs, out, dsp.InsertsGraph(ins)); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	fname := fmt.Sprintf("overdub-inst-%d.flac", lastChild(specs))
	v, err := svc.UploadDsp(ctx, parentID, fname, data)
	if err != nil {
		return nil, err
	}
	return &RebuildResult{Variant: v, Inserts: reports}, nil
}

// stemFxInserts — эффект на дорожки трека в окне: дорожка целиком через
// цепочку (эффекты с памятью — эхо, компрессор — «разогреты» к окну), в трек
// ложится обработанная дорожка и та же исходная с обратным знаком, с фейдами.
func stemFxInserts(s SectionSpec, parent stemSet, dir string, idx int, inputs *[]string) ([]dsp.Insert, error) {
	chain := dsp.ByID(s.Chain)
	if chain == nil {
		return nil, fmt.Errorf("неизвестный эффект %q", s.Chain)
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
		fx := fmt.Sprintf("%s/fx-%d-%s.flac", dir, idx, name)
		if err := dsp.Run(parent[name], fx, chain.FilterGraph(s.Params), nil); err != nil {
			return nil, fmt.Errorf("эффект %s на %s: %w", s.Chain, name, err)
		}
		from := math.Max(0, s.From-fadeIn)
		dur := fxWindowForever
		if s.To > 0 {
			dur = s.To + fadeOut - from
		}
		*inputs = append(*inputs, fx, parent[name])
		out = append(out,
			dsp.Insert{AtSec: from, SkipSec: from, DurSec: dur, Gain: 1, FadeIn: fadeIn, FadeOut: fadeOut},
			dsp.Insert{AtSec: from, SkipSec: from, DurSec: dur, Gain: -1, FadeIn: fadeIn, FadeOut: fadeOut})
	}
	return out, nil
}

// fxWindowForever — «до конца трека» для окна эффекта (длиннее любой песни)
const fxWindowForever = 3600.0

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
		from, to := s.From-fadeIn, s.To+fadeOut
		*inputs = append(*inputs, parent[name])
		// KeepHighHz > 0 — меняется только низ дорожки: demucs относит к голосу
		// шумные тарелки, и заглушённая речь уносила их с собой (#258: верх −30 дБ
		// на месте речи, «дыры»)
		out = append(out, dsp.Insert{AtSec: from, SkipSec: from, DurSec: to - from, Gain: gain,
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
