package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"yue-studio/internal/dsp"
	"yue-studio/internal/studio"
	"yue-studio/internal/yue"
)

// окно превью по умолчанию, как было у dsp_preview: кусок с 20-й секунды
const mcpPreviewFrom, mcpPreviewTo = 20.0, 35.0

// fxCache — кэш трека/стемов MCP-сервера для превью (живёт с процессом).
func (s *Server) fxCache() *studio.Cache {
	s.fxOnce.Do(func() {
		s.fxDir = filepath.Join(os.TempDir(), fmt.Sprintf("yue-mcp-fx-%d", os.Getpid()))
		s.fx = studio.NewCache(filepath.Join(s.fxDir, "files"))
	})
	return s.fx
}

// fxPreview — превью шагов на куске трека (как в приложении), «стало» — вариантом
// на воркер: агенту нужны метрики, а их считает воркер.
func (s *Server) fxPreview(jobID int64, stem string, steps []dsp.Step, from, to float64, solo bool) (*yue.DspVariant, error) {
	if to <= from {
		from, to = mcpPreviewFrom, mcpPreviewTo
	}
	cache := s.fxCache()
	dir := filepath.Join(s.fxDir, "previews", fmt.Sprint(jobID))
	res, err := studio.Preview(context.Background(), s.client, cache, studio.PreviewSpec{
		JobID: jobID, Stem: stem, Steps: steps, From: from, To: to}, dir)
	if err != nil {
		return nil, err
	}
	wet := res.Wet
	if solo {
		if res.WetSolo == "" {
			return nil, errors.New("solo — только с stem (дорожка без остального микса)")
		}
		wet = res.WetSolo
	}
	data, err := os.ReadFile(wet)
	if err != nil {
		return nil, err
	}
	name := "steps"
	if len(steps) == 1 {
		name = steps[0].Chain
	}
	label := fmt.Sprintf("превью %s %.0f–%.0f с", name, from, to)
	if stem != "" {
		label += " · " + stem
	}
	if solo {
		label += " (соло)"
	}
	return s.client.UploadDsp(context.Background(), jobID, "dsp-preview-"+name+".flac", label, data)
}

// argSteps — цепочка эффектов из аргументов: steps [{chain, params, off}] или
// одна цепочка chain + params. Ничего — ошибка.
func argSteps(args map[string]any) ([]dsp.Step, error) {
	steps, err := parseSteps(args["steps"])
	if err != nil || len(steps) > 0 {
		return steps, err
	}
	if chain := argString(args, "chain"); chain != "" {
		return []dsp.Step{{Chain: chain, Params: argNumMap(args, "params")}}, nil
	}
	return nil, errors.New("нужен chain (одна цепочка) или steps (цепочка по порядку) — см. dsp_chains")
}

// parseSteps — массив шагов [{chain, params, off}] из JSON-аргумента; нет — nil.
func parseSteps(raw any) ([]dsp.Step, error) {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return nil, nil
	}
	out := make([]dsp.Step, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("steps[%d]: нужен объект {chain, params, off}", i)
		}
		st := dsp.Step{Chain: argString(m, "chain"), Params: argNumMap(m, "params")}
		st.Off, _ = m["off"].(bool)
		if st.Chain == "" {
			return nil, fmt.Errorf("steps[%d]: нет chain", i)
		}
		out = append(out, st)
	}
	return out, nil
}
