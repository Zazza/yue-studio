package studio

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"

	"yue-studio/internal/dsp"
	"yue-studio/internal/yue"
)

// Пресеты звука: рецепт обработки готового трека (правки дорожек на весь трек + финальная
// цепочка ffmpeg на весь микс) → новая версия-трек «<трек> · <пресет>». Применяет приложение
// (и MCP вручную): пересборка дорожек и ffmpeg живут на ПК, воркер хранит рецепты и статусы.

// PresetResult — версия, которую сделал пресет: ChildID — трек-версия, File — вариант-источник.
type PresetResult struct {
	ChildID int64
	File    string
}

// ApplySoundPreset — пресет на трек: дорожки (нет — разделение), наборы сэмплов, пересборка
// с правками «весь трек», финал на РЕЗУЛЬТАТЕ пересборки, версия-трек. Ошибка любого шага —
// ошибка с причиной; трек и прежние варианты не трогаются.
func ApplySoundPreset(ctx context.Context, svc yue.Service, jobID int64, p yue.SoundPreset) (*PresetResult, error) {
	job, err := findJob(ctx, svc, jobID)
	if err != nil {
		return nil, err
	}
	// части барабанов пересборка обрабатывает только движком: эффект/педали на них молча пропали бы
	for _, sp := range p.Specs {
		placeOnly := sp.Place != nil && sp.Chain == "" && len(sp.Steps) == 0 // место части — без обработки, можно
		if len(sp.Engine) == 0 && !placeOnly && slices.ContainsFunc(sp.Stems, func(n string) bool { return slices.Contains(drumParts, n) }) {
			return nil, fmt.Errorf("пресет «%s»: на частях барабанов (%s) — только цепочка движка", p.Name, strings.Join(sp.Stems, ", "))
		}
	}
	if err := ensurePresetStems(ctx, svc, jobID, p.Specs); err != nil {
		return nil, err
	}
	if err := ensurePresetKits(ctx, svc, p.Specs); err != nil {
		return nil, err
	}
	file := job.AudioFile
	if len(p.Specs) > 0 {
		specs := make([]SectionSpec, 0, len(p.Specs))
		for _, s := range p.Specs {
			specs = append(specs, presetSection(s))
		}
		res, err := rebuildSections(ctx, svc, jobID, specs, fmt.Sprintf("dsp-preset-%d-mix.flac", p.ID))
		if err != nil {
			return nil, fmt.Errorf("пересборка дорожек: %w", err)
		}
		file = res.Variant.File
	}
	if steps := activeSteps(p.Final); len(steps) > 0 {
		graph, _, err := dsp.StepsGraph(steps)
		if err != nil {
			return nil, fmt.Errorf("финал: %w", err)
		}
		v, err := RunGraph(ctx, svc, jobID, file, graph, fmt.Sprintf("dsp-preset-%d.flac", p.ID), p.Name)
		if err != nil {
			return nil, fmt.Errorf("финал: %w", err)
		}
		file = v.File
	}
	if master := presetMaster(p); len(master) > 0 {
		// мастер и громкость к цели — на воркере (истинный пик), на файле после финала: вариант
		// пресета — на месте, звук самого трека не трогается — обычный вариант движка
		req := yue.FxRequest{Source: "mix", Chain: master, Label: p.Name}
		if file != job.AudioFile {
			req.File, req.InPlace = file, true
		}
		v, err := svc.ApplyFx(ctx, jobID, req)
		if err != nil {
			return nil, fmt.Errorf("мастер: %w", err)
		}
		file = v.File
	}
	title := strings.TrimSpace(job.Title)
	if title == "" {
		title = fmt.Sprintf("#%d", jobID)
	}
	child, err := svc.VariantToTrack(ctx, jobID, file, title+" · "+p.Name, 0)
	if err != nil {
		return nil, fmt.Errorf("версия трека: %w", err)
	}
	return &PresetResult{ChildID: child, File: file}, nil
}

func findJob(ctx context.Context, svc yue.Service, id int64) (*yue.Job, error) {
	jobs, err := svc.Jobs(ctx)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		if jobs[i].ID == id {
			if jobs[i].AudioFile == "" {
				return nil, fmt.Errorf("job %d has no audio", id) // прежняя ошибка конвейера DSP (app_test)
			}
			return &jobs[i], nil
		}
	}
	return nil, fmt.Errorf("job %d has no audio (not found)", id)
}

// presetMaster — цепочка мастера пресета с громкостью к цели: target_lufs ложится в ограничитель
// (есть без цели — получает её; своя цель ограничителя важнее), нет ограничителя — дописывается.
// Копия: пресет не меняется.
func presetMaster(p yue.SoundPreset) []map[string]any {
	out := make([]map[string]any, 0, len(p.Master)+1)
	limiter := -1
	for _, b := range p.Master {
		c := make(map[string]any, len(b))
		for k, v := range b {
			c[k] = v
		}
		if c["type"] == "limiter" {
			limiter = len(out)
		}
		out = append(out, c)
	}
	if p.TargetLUFS == nil {
		return out
	}
	if limiter < 0 {
		return append(out, map[string]any{"type": "limiter", "target_lufs": *p.TargetLUFS})
	}
	if t, _ := out[limiter]["target_lufs"].(float64); t == 0 {
		out[limiter]["target_lufs"] = *p.TargetLUFS
	}
	return out
}

// presetSection — правка пресета → запись пересборки: эффект на дорожки на весь трек
func presetSection(s yue.PresetSpec) SectionSpec {
	sec := SectionSpec{Stems: slices.Clone(s.Stems), Db: s.Db, Engine: s.Engine, Chain: s.Chain, Params: s.Params,
		Place: s.Place}
	for _, st := range s.Steps {
		sec.Steps = append(sec.Steps, dsp.Step{Chain: st.Chain, Params: st.Params, Off: st.Off})
	}
	return sec
}

func activeSteps(final []yue.PresetStep) []dsp.Step {
	var out []dsp.Step
	for _, st := range final {
		if !st.Off {
			out = append(out, dsp.Step{Chain: st.Chain, Params: st.Params})
		}
	}
	return out
}

func trackStems(ctx context.Context, svc yue.Service, id int64) (map[string]bool, error) {
	list, err := svc.JobStems(ctx, id)
	if err != nil {
		var se *yue.StatusError
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			return map[string]bool{}, nil // дорожек ещё нет
		}
		return nil, err
	}
	have := map[string]bool{}
	for _, s := range list {
		if n, ok := s["name"].(string); ok {
			have[n] = true
		}
	}
	return have, nil
}

func missingStems(need []string, have map[string]bool) []string {
	var out []string
	for _, n := range need {
		if !have[n] {
			out = append(out, n)
		}
	}
	return out
}

// ensurePresetStems — все дорожки правок есть у трека; нет — разделение (части барабанов даёт
// только RoFormer), после него всё ещё нет — ошибка с именами дорожек.
func ensurePresetStems(ctx context.Context, svc yue.Service, id int64, specs []yue.PresetSpec) error {
	var need []string
	for _, s := range specs {
		for _, n := range s.Stems {
			if !slices.Contains(need, n) {
				need = append(need, n)
			}
		}
	}
	if len(need) == 0 {
		return nil
	}
	have, err := trackStems(ctx, svc, id)
	if err != nil {
		return err
	}
	miss := missingStems(need, have)
	if len(miss) == 0 {
		return nil
	}
	parts := slices.ContainsFunc(miss, func(n string) bool { return slices.Contains(drumParts, n) })
	if parts {
		_, err = svc.MakeStemsWith(ctx, id, "roformer")
	} else {
		_, err = svc.MakeStems(ctx, id)
	}
	if err != nil {
		return fmt.Errorf("разделение на дорожки: %w", err)
	}
	if have, err = trackStems(ctx, svc, id); err != nil {
		return err
	}
	if miss = missingStems(need, have); len(miss) > 0 {
		return fmt.Errorf("нет дорожек: %s (нужно разделение RoFormer)", strings.Join(miss, ", "))
	}
	return nil
}

// ensurePresetKits — наборы сэмплов цепочек движка (kit/kit_open/kit_mid/kit_low у sampler, kit у bass и perc:
// «<набор>/<часть>»), которых нет на воркере, — установить по разу на набор
func ensurePresetKits(ctx context.Context, svc yue.Service, specs []yue.PresetSpec) error {
	var need []string
	for _, s := range specs {
		for _, b := range s.Engine {
			for _, k := range []string{"kit", "kit_open", "kit_mid", "kit_low"} {
				if v, _ := b[k].(string); v != "" && !slices.Contains(need, v) {
					need = append(need, v)
				}
			}
		}
	}
	if len(need) == 0 {
		return nil
	}
	assets, err := svc.FxAssets(ctx)
	if err != nil {
		return fmt.Errorf("наборы сэмплов: %w", err)
	}
	have := map[string]bool{}
	if list, ok := assets["kits"].([]any); ok {
		for _, k := range list {
			if m, ok := k.(map[string]any); ok {
				if n, ok := m["name"].(string); ok {
					have[n] = true
				}
			}
		}
	}
	var installed []string
	for _, k := range need {
		kit, _, _ := strings.Cut(k, "/")
		if have[k] || slices.Contains(installed, kit) {
			continue
		}
		if _, err := svc.InstallFxKit(ctx, kit); err != nil {
			return fmt.Errorf("набор %s: %w", kit, err)
		}
		installed = append(installed, kit)
	}
	return nil
}

// RunGraph — граф ffmpeg на файл трека → вариант fname с подписью label ("" — по имени файла).
// src "" — звук самого трека. Общий конвейер приложения, MCP и пресетов звука.
func RunGraph(ctx context.Context, svc yue.Service, jobID int64, src, graph, fname, label string) (*yue.DspVariant, error) {
	if src == "" {
		job, err := findJob(ctx, svc, jobID)
		if err != nil {
			return nil, err
		}
		src = job.AudioFile
	}
	dir, err := os.MkdirTemp("", fmt.Sprintf("yue-graph-%d-*", jobID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	in, err := FetchTemp(ctx, svc, jobID, src, dir, "in-*"+fileExt(src))
	if err != nil {
		return nil, err
	}
	out := in + ".out.flac"
	if err := dsp.Run(in, out, graph, nil); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	return svc.UploadDsp(ctx, jobID, fname, label, data)
}

func fileExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

// PresetRunner — авто-применение пресетов, выбранных при создании трека: приложение зовёт Tick
// раз в несколько секунд. Видеокарта занята (есть queued/running) — ждёт; иначе берёт первый
// pending у первой готовой не черновой джобы, захватывает (409 — другой уже взял), применяет и
// пишет итог. Один пресет за тик: пересборка и финал занимают минуты.
type PresetRunner struct {
	Svc   yue.Service
	Apply func(ctx context.Context, jobID int64, p yue.SoundPreset) (*PresetResult, error)
}

func (r *PresetRunner) Tick(ctx context.Context) {
	jobs, err := r.Svc.Jobs(ctx)
	if err != nil {
		log.Printf("пресеты звука: список треков: %v", err)
		return
	}
	var job *yue.Job
	var pid int64
	for i := range jobs {
		if jobs[i].Status == "queued" || jobs[i].Status == "running" {
			return // очередь видеокарты занята — пресеты подождут
		}
	}
	slices.SortFunc(jobs, func(a, b yue.Job) int { return cmp.Compare(a.ID, b.ID) })
	for i := range jobs {
		j := &jobs[i]
		if j.Status != "done" || j.Draft {
			continue
		}
		for _, sp := range j.SoundPresets {
			if sp.Status == "pending" {
				job, pid = j, sp.ID
				break
			}
		}
		if job != nil {
			break
		}
	}
	if job == nil {
		return
	}
	presets, err := r.Svc.SoundPresets(ctx)
	if err != nil {
		log.Printf("пресеты звука: список пресетов: %v", err)
		return
	}
	if _, err := r.Svc.SoundPresetState(ctx, job.ID, pid, yue.JobPreset{Status: "running"}); err != nil {
		log.Printf("пресеты звука: захват #%d/%d: %v", job.ID, pid, err) // 409 — уже взял другой
		return
	}
	var preset *yue.SoundPreset
	for i := range presets {
		if presets[i].ID == pid {
			preset = &presets[i]
			break
		}
	}
	if preset == nil {
		r.finish(ctx, job.ID, pid, yue.JobPreset{Status: "error", Error: "пресет удалён"})
		return
	}
	apply := r.Apply
	if apply == nil {
		apply = func(ctx context.Context, id int64, p yue.SoundPreset) (*PresetResult, error) {
			return ApplySoundPreset(ctx, r.Svc, id, p)
		}
	}
	res, err := apply(ctx, job.ID, *preset)
	if err != nil {
		msg := err.Error()
		if len([]rune(msg)) > 480 {
			msg = string([]rune(msg)[:480]) + "…"
		}
		r.finish(ctx, job.ID, pid, yue.JobPreset{Status: "error", Error: msg})
		return
	}
	r.finish(ctx, job.ID, pid, yue.JobPreset{Status: "done", ChildID: res.ChildID})
}

func (r *PresetRunner) finish(ctx context.Context, jobID, pid int64, st yue.JobPreset) {
	if _, err := r.Svc.SoundPresetState(ctx, jobID, pid, st); err != nil {
		log.Printf("пресеты звука: итог #%d/%d: %v", jobID, pid, err)
	}
}
