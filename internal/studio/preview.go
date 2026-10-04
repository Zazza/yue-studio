package studio

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"yue-studio/internal/dsp"
	"yue-studio/internal/yue"
)

// PreviewSpec — что послушать: эффекты Steps на весь трек (Stem "") или на
// одну дорожку, в окне трека [From, To).
type PreviewSpec struct {
	JobID int64
	Stem  string
	Steps []dsp.Step
	From  float64
	To    float64
}

// PreviewResult — «стало» (Wet) и «было» (Dry): куски одной длины с одного
// места трека, файлы на ПК. From/To — окно трека, DurSec — длина кусков
// (окно + хвост эффекта). У превью на дорожку ещё соло: обработанная и
// исходная дорожка без остального микса — в плотном миксе (стена гитар) звук
// педали маскируется, соло слышно без помех (замер #331: тремоло на гитаре
// −17 дБ к миксу — в миксе на слух «ничего не меняется»).
type PreviewResult struct {
	Wet     string  `json:"wet"`
	Dry     string  `json:"dry"`
	WetSolo string  `json:"wet_solo,omitempty"`
	DrySolo string  `json:"dry_solo,omitempty"`
	From    float64 `json:"from"`
	To      float64 `json:"to"`
	DurSec  float64 `json:"dur_sec"`
}

const (
	// previewWarmSec — звук до окна, на котором эффект «прогревается» и
	// отрезается: у компрессора, эха и реверба на первой секунде иначе провал
	previewWarmSec = 2.0
	// previewMaxTail — сколько хвоста реверба/дилея слышно после окна, с
	previewMaxTail = 3.0
	previewRate    = 16000 // частота анализа уровня голосовой цепочки
)

// Preview — кусок трека с эффектами и без: быстрая проба перед применением.
// На дорожку — как пересборка: в кусок трека ложится «обработанная − исходная»
// дорожка, остальные не меняются. Трек и стемы — из кэша на ПК. Стемов нет —
// один раз просим воркер разделить трек.
func Preview(ctx context.Context, svc yue.Service, cache *Cache, spec PreviewSpec, outDir string) (*PreviewResult, error) {
	if spec.To <= spec.From || spec.From < 0 {
		return nil, fmt.Errorf("окно превью %.2f–%.2f с: конец должен быть позже начала", spec.From, spec.To)
	}
	if spec.Stem != "" && !slices.Contains(mutable, spec.Stem) {
		return nil, fmt.Errorf("неизвестная дорожка %q", spec.Stem)
	}
	graph, tail, err := dsp.StepsGraph(spec.Steps)
	if err != nil {
		return nil, err
	}
	tail = math.Min(tail, previewMaxTail)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	base, err := cache.Base(ctx, svc, spec.JobID)
	if err != nil {
		return nil, fmt.Errorf("звук трека #%d: %w", spec.JobID, err)
	}
	win := spec.To - spec.From
	res := &PreviewResult{
		Wet: filepath.Join(outDir, "wet.flac"), Dry: filepath.Join(outDir, "dry.flac"),
		From: spec.From, To: spec.To, DurSec: win + tail,
	}
	// кусок для обработки: с прогревом до окна; у эффектов с секундой трека —
	// с начала трека, чтобы их отметки и сетка остались на своих местах
	segStart := math.Max(0, spec.From-previewWarmSec)
	if dsp.StepsTimed(spec.Steps) {
		segStart = 0
	}
	lead := spec.From - segStart
	tmp, err := os.MkdirTemp(outDir, "work-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if err := dsp.Excerpt(base, res.Dry, spec.From, res.DurSec); err != nil {
		return nil, fmt.Errorf("кусок трека: %w", err)
	}
	baseSeg := filepath.Join(tmp, "base.flac")
	if err := dsp.Excerpt(base, baseSeg, segStart, lead+res.DurSec); err != nil {
		return nil, fmt.Errorf("кусок трека: %w", err)
	}
	cut := fmt.Sprintf("atrim=start=%.6f,asetpts=PTS-STARTPTS", lead)
	if spec.Stem == "" {
		if err := dsp.Run(baseSeg, res.Wet, withOut(graph, "[pv_w]")+";[pv_w]"+cut+"[out]", nil); err != nil {
			return nil, fmt.Errorf("эффект: %w", err)
		}
		return res, nil
	}

	stem, err := previewStem(ctx, svc, cache, spec.JobID, spec.Stem)
	if err != nil {
		return nil, err
	}
	stemSeg := filepath.Join(tmp, "stem.flac")
	if err := dsp.Excerpt(stem, stemSeg, segStart, lead+res.DurSec); err != nil {
		return nil, fmt.Errorf("кусок дорожки: %w", err)
	}
	fx := filepath.Join(tmp, "fx.flac")
	if err := dsp.Run(stemSeg, fx, graph, nil); err != nil {
		return nil, fmt.Errorf("эффект на %s: %w", spec.Stem, err)
	}
	gain := 1.0
	if match, extraDb := dsp.StepsMatch(spec.Steps); match {
		// голосовая примочка или перегруз: громкость обработанной дорожки в окне —
		// по исходной, крутилка «громкость» перегруза — сверху
		ref, err := dsp.DecodeMono(stemSeg, previewRate, lead, win)
		if err != nil {
			return nil, err
		}
		wet, err := dsp.DecodeMono(fx, previewRate, lead, win)
		if err != nil {
			return nil, err
		}
		gain = dsp.InsertGain(dsp.RMS(ref), dsp.RMS(wet), extraDb)
	}
	mix := fmt.Sprintf("[1:a]volume=%g[pv_f];[2:a]volume=-1[pv_s];"+
		"[0:a][pv_f][pv_s]amix=inputs=3:duration=first:normalize=0,%s[out]", gain, cut)
	if err := dsp.RunInputs([]string{baseSeg, fx, stemSeg}, res.Wet, mix); err != nil {
		return nil, fmt.Errorf("микс превью: %w", err)
	}
	res.WetSolo, res.DrySolo = filepath.Join(outDir, "wet_solo.flac"), filepath.Join(outDir, "dry_solo.flac")
	if err := dsp.Run(fx, res.WetSolo, fmt.Sprintf("[0:a]volume=%g,%s[out]", gain, cut), nil); err != nil {
		return nil, fmt.Errorf("соло дорожки: %w", err)
	}
	if err := dsp.Run(stemSeg, res.DrySolo, "[0:a]"+cut+"[out]", nil); err != nil {
		return nil, fmt.Errorf("соло дорожки: %w", err)
	}
	return res, nil
}

// previewStem — дорожка из кэша; у трека нет стемов (или старые, без гитары)
// — один раз просим воркер разделить трек и качаем заново.
func previewStem(ctx context.Context, svc yue.Service, cache *Cache, jobID int64, stem string) (string, error) {
	name := "stem-" + stem + ".flac"
	p, err := cache.Fetch(ctx, svc, jobID, name)
	if err == nil {
		return p, nil
	}
	if !isNotFound(err) {
		return "", err
	}
	if _, err := svc.MakeStems(ctx, jobID); err != nil {
		return "", fmt.Errorf("стемы #%d: %w", jobID, err)
	}
	cache.Invalidate(jobID) // разделение переписало все стемы трека
	if p, err = cache.Fetch(ctx, svc, jobID, name); err != nil {
		if isNotFound(err) {
			return "", errors.New("дорожка " + stem + " не выделилась (воркер без 6-стемной модели?)")
		}
		return "", err
	}
	return p, nil
}

// withOut — граф с выходом out вместо [out] (для склейки с хвостом графа).
func withOut(graph, out string) string {
	return strings.TrimSuffix(graph, "[out]") + out
}
