// Package studio — конвейеры экрана студии на стороне ПК (ffmpeg + воркер).
package studio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"yue-studio/internal/dsp"
	"yue-studio/internal/yue"
)

// InsertSpec — одна вклейка, как её хранит фронтенд.
type InsertSpec struct {
	ChildID int64   `json:"child_id"` // джоба-мини-рендер (её audio.flac — партия)
	From    float64 `json:"from"`     // окно трека, где партия должна звучать, с
	To      float64 `json:"to"`
	Lead    float64 `json:"lead"`     // сколько секунд партии звучит до From (такт контекста плана)
	BeatSec float64 `json:"beat_sec"` // длина доли по плану (60/Q); 0 → дефолт dsp
	Db      float64 `json:"db"`       // громкость относительно оригинала, дБ
}

// InsertReport — куда и как встала вклейка.
type InsertReport struct {
	ChildID  int64   `json:"child_id"`
	StartSec float64 `json:"start_sec"` // где в треке встало начало партии
	Aligned  bool    `json:"aligned"`   // true — подогнано по бочке; false — стоит по плану (From − Lead)
	Score    float64 `json:"score"`
	Gain     float64 `json:"gain"` // итоговый линейный гейн
}

// RebuildResult — новый вариант трека и отчёт по вклейкам.
type RebuildResult struct {
	Variant *yue.DspVariant `json:"variant"`
	Inserts []InsertReport  `json:"inserts"`
}

const (
	baseFile  = "audio.flac"
	drumsStem = "stem-drums.flac"
	// gainRate — частота анализа уровня (RMS) окна и партии
	gainRate = 16000
	// tmpPattern — файлы конвейера живут в своём temp-каталоге, имя не важно
	tmpPattern = "*.flac"
)

// RebuildInserts — пересобрать трек со ВСЕМИ вклейками с чистого оригинала:
// повторный микс поверх прошлого наслаивал бы партии и не давал менять
// громкость. Ритм — по стему бочки (demucs на воркере, секунды); стема нет
// и не получился — вклейки встают по плану (From − Lead), без ошибки.
func RebuildInserts(ctx context.Context, svc yue.Service, parentID int64, specs []InsertSpec) (*RebuildResult, error) {
	if len(specs) == 0 {
		return nil, errors.New("нет вклеек для пересборки")
	}
	dir, err := os.MkdirTemp("", fmt.Sprintf("yue-inserts-%d-*", parentID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	base, err := FetchTemp(ctx, svc, parentID, baseFile, dir, tmpPattern)
	if err != nil {
		return nil, fmt.Errorf("оригинал #%d: %w", parentID, err)
	}
	drums := drumsPath(ctx, svc, parentID, dir)

	inputs := []string{base}
	ins := make([]dsp.Insert, 0, len(specs))
	reports := make([]InsertReport, 0, len(specs))
	for _, s := range specs {
		party, err := FetchTemp(ctx, svc, s.ChildID, baseFile, dir, tmpPattern)
		if err != nil {
			return nil, fmt.Errorf("партия #%d: %w", s.ChildID, err)
		}
		p := dsp.Placement{StartSec: s.From - s.Lead, Ratio: 1}
		if drums != "" {
			if m, err := dsp.MeasureInsert(drums, party, s.From, s.To, s.Lead, s.BeatSec); err == nil {
				p = m
			}
			// ошибка замера — не повод терять вклейку: остаётся план
		}
		gain, err := insertGain(base, party, p, s)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, party)
		ins = append(ins, dsp.PlaceInsert(p, s.From, s.To, gain))
		reports = append(reports, InsertReport{ChildID: s.ChildID, StartSec: p.StartSec,
			Aligned: p.Aligned, Score: p.Score, Gain: gain})
	}

	out := dir + "/out.flac"
	if err := dsp.RunInputs(inputs, out, dsp.InsertsGraph(ins)); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	fname := fmt.Sprintf("overdub-inst-%d.flac", specs[len(specs)-1].ChildID)
	v, err := svc.UploadDsp(ctx, parentID, fname, data)
	if err != nil {
		return nil, err
	}
	return &RebuildResult{Variant: v, Inserts: reports}, nil
}

// drumsPath — стем бочки родителя во временном файле; нет — один раз
// просим воркер разложить трек. "" — опоры нет, вклейки по плану.
func drumsPath(ctx context.Context, svc yue.Service, parentID int64, dir string) string {
	if p, err := FetchTemp(ctx, svc, parentID, drumsStem, dir, tmpPattern); err == nil {
		return p
	}
	if _, err := svc.MakeStems(ctx, parentID); err != nil {
		return "" // demucs недоступен — вклейки по плану (Aligned=false в отчёте)
	}
	if p, err := FetchTemp(ctx, svc, parentID, drumsStem, dir, tmpPattern); err == nil {
		return p
	}
	return ""
}

// insertGain — гейн партии: её уровень в звучащем куске выравнивается по
// окну оригинала [From, To], сверху — дБ пользователя.
func insertGain(base, party string, p dsp.Placement, s InsertSpec) (float64, error) {
	dur := s.To - s.From
	ref, err := dsp.DecodeMono(base, gainRate, s.From, dur)
	if err != nil {
		return 0, err
	}
	ratio := p.Ratio
	if ratio <= 0 {
		ratio = 1
	}
	cand, err := dsp.DecodeMono(party, gainRate, max(0, (s.From-p.StartSec)*ratio), dur*ratio)
	if err != nil {
		return 0, err
	}
	return dsp.InsertGain(dsp.RMS(ref), dsp.RMS(cand), s.Db), nil
}

// FetchTemp скачивает артефакт джобы во временный файл каталога dir
// ("" — системный temp) по шаблону имени pattern; удаление — на вызывающем.
func FetchTemp(ctx context.Context, svc yue.Service, id int64, file, dir, pattern string) (string, error) {
	body, _, err := svc.FetchAudio(ctx, id, file)
	if err != nil {
		return "", err
	}
	defer func() { _ = body.Close() }()
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	_, cpErr := io.Copy(tmp, body)
	tmp.Close()
	if cpErr != nil {
		os.Remove(tmp.Name())
		return "", cpErr
	}
	return tmp.Name(), nil
}
