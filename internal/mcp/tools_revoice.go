package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"yue-studio/internal/dsp"
	"yue-studio/internal/studio"
	"yue-studio/internal/yue"
)

// Инструменты слоя над моделью, которых нет у YuE: пересборка дорожек
// (вклейки, громкость, «перепеть»), «перепеть с места» по частям и сверка
// высоты голоса с планом. Те же механизмы, что в студии приложения.

const (
	revoiceTakesDefault = 2
	revoiceTakesMax     = 4
)

func registerRevoiceTools(s *Server) {
	registerToneTools(s)
	registerPlanTools(s)
	registerSpliceTools(s)
	s.Register(Tool{
		Name: "rebuild_sections",
		Description: "Пересборка дорожек трека с чистого оригинала (как студия): вклейки куском (child_id — рендер " +
			"куска, stems — какие дорожки заменить), громкость дорожек без рендера (child_id 0, db; −100 — заглушить, " +
			"можно vocals), «перепеть» (revoice: stems [vocals] — голос из рендера child_id). Ответ — файл варианта и " +
			"отчёт по заменам (встала по бочке/по плану). as_track — сразу версией-треком под «📎».",
		InputSchema: props(map[string]any{
			"job_id": prop("ID трека (версии), в котором меняются дорожки", "integer"),
			"specs": map[string]any{"type": "array", "description": "замены: {child_id, from, to (0 — до конца трека), lead?, beat_sec?, " +
				"stems, db?, fade_in?, fade_out?, keep_high_hz?, revoice?, chain?, params?, steps?, envelope?, engine?, add?} — chain/params: эффект на " +
				"дорожки stems в окне (голосовые цепочки — с выравниванием громкости по исходной дорожке, db сверху); " +
				"steps [{chain, params, off}] вместо chain — цепочка эффектов по порядку (педали, dsp_presets). " +
				"envelope [{t, db}] при child_id 0 — линия громкости дорожек stems по всему треку (как volume_envelope). engine [{type, …}] при child_id 0 — цепочка звукового движка воркера (как fx_apply, блоки — fx_blocks, готовые — fx_presets) на дорожки stems в окне (и на части барабанов kick/snare/toms/hh/ride/crash — только при дорожках RoFormer; замена ударов — блок sampler): считается на воркере, звук не сдвигается, хвост реверба/дилея/сэмплов звучит после to. add true у записи engine — добавить кусок поверх трека, исходную дорожку не вычитать; stems [\"mix\"] (без разделения) — только с add: так ложится синт-партия (блок synth с notes в секундах трека, аккорды — chord_grid). " +
				"stems: drums/bass/other/vocals; при child_id 0 ещё guitar/piano — гитара и клавиши внутри other " +
				"(заменить куском их нельзя)", "items": map[string]any{"type": "object"}},
			"as_track":  prop("сделать вариант версией-треком", "boolean"),
			"title":     prop("название версии (as_track)", "string"),
			"voice_src": prop("ID рендера, чей голос подставлен (as_track после «перепеть»)", "integer"),
		}, "job_id", "specs"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			specs, err := parseSectionSpecs(args["specs"])
			if err != nil {
				return "", err
			}
			if len(specs) == 0 {
				return "", errors.New("specs пуст — нечего пересобирать")
			}
			jobID := argInt(args, "job_id")
			res, err := studio.RebuildSections(context.Background(), s.client, jobID, specs)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "вариант %s у трека #%d\n", res.Variant.File, jobID)
			for _, r := range res.Inserts {
				how := "по плану"
				if r.Aligned {
					how = "по бочке"
				}
				fmt.Fprintf(&b, "  #%d: с %.2f с, %s (score %.2f, gain %.2f)\n", r.ChildID, r.StartSec, how, r.Score, r.Gain)
			}
			if argBool(args, "as_track") {
				id, err := promoteVersion(s.client, jobID, res.Variant.File, argString(args, "title"),
					fmt.Sprintf("версия #%d · пересборка", jobID), argInt(args, "voice_src"))
				if err != nil {
					return "", err
				}
				fmt.Fprintf(&b, "версия-трек #%d", id)
			}
			return b.String(), nil
		},
	})

	s.Register(Tool{
		Name: "revoice_start",
		Description: "«Перепеть с места», шаг 1: голос части [from, to) версии поётся заново — продолжения от " +
			"ИСТОЧНИКА голоса версии (voice_src / сам сгенерированный трек / по цепочке родителей) с отметки from. " +
			"abc — изменённый план источника (мелодия голоса в части: модель поёт её нота в ноту; не выше потолка " +
			"голоса — верх мелодии плана + 2 ступени). Ответ — id дублей; когда они done — revoice_apply.",
		InputSchema: props(map[string]any{
			"job_id": prop("ID версии трека", "integer"),
			"from":   prop("начало части, с", "number"),
			"to":     prop("конец части, с (лучше в паузе голоса)", "number"),
			"abc":    prop("изменённый план источника (необязательно)", "string"),
			"takes":  prop("сколько дублей, 1–4 (по умолчанию 2)", "integer"),
		}, "job_id", "from", "to"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			takes := int64(revoiceTakesDefault)
			if _, ok := args["takes"]; ok {
				takes = argInt(args, "takes")
			}
			if takes < 1 || takes > revoiceTakesMax {
				return "", fmt.Errorf("takes: от 1 до %d", revoiceTakesMax)
			}
			ctx := context.Background()
			jobs, err := s.client.Jobs(ctx)
			if err != nil {
				return "", err
			}
			byID := make(map[int64]yue.Job, len(jobs))
			for _, j := range jobs {
				byID[j.ID] = j
			}
			job, ok := byID[argInt(args, "job_id")]
			if !ok {
				return "", fmt.Errorf("трек #%d не найден", argInt(args, "job_id"))
			}
			src := studio.VoiceSource(job, byID)
			if src == 0 {
				return "", fmt.Errorf("источник голоса трека #%d не найден", job.ID)
			}
			from, abc := argFloat(args, "from"), argString(args, "abc")
			// план правится для источника голоса — проверяем против его плана
			note, err := planNote(s, src, abc, from)
			if err != nil {
				return "", err
			}
			ids := make([]string, 0, takes)
			for k := int64(0); k < takes; k++ {
				id, err := s.client.ContinueJob(ctx, src, from, 0, abc, "")
				if err != nil {
					return "", err
				}
				ids = append(ids, fmt.Sprintf("#%d", id))
			}
			return fmt.Sprintf("источник голоса #%d; дубли %s (с %.2f с). Когда done — revoice_apply {job_id: %d, "+
				"take_id, from: %g, to: %g}", src, strings.Join(ids, ", "), from, job.ID, from, argFloat(args, "to")) + note, nil
		},
	})

	s.Register(Tool{
		Name: "revoice_apply",
		Description: "«Перепеть с места», шаг 2: голос готового дубля подставляется в версию ТОЛЬКО в окне [from, to) " +
			"(музыка и голос вне части прежние) → новая версия-трек под «📎» с voice_src = дубль.",
		InputSchema: props(map[string]any{
			"job_id":   prop("ID версии трека", "integer"),
			"take_id":  prop("ID дубля из revoice_start", "integer"),
			"from":     prop("начало части, с", "number"),
			"to":       prop("конец части, с", "number"),
			"beat_sec": prop("длина доли, с (по умолчанию по плану 120 BPM)", "number"),
			"title":    prop("название версии", "string"),
		}, "job_id", "take_id", "from", "to"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			ctx := context.Background()
			jobID, take := argInt(args, "job_id"), argInt(args, "take_id")
			jobs, err := s.client.Jobs(ctx)
			if err != nil {
				return "", err
			}
			status := ""
			for _, j := range jobs {
				if j.ID == take {
					status = j.Status
				}
			}
			if status != "done" {
				if status == "" {
					status = "не найден"
				}
				return "", fmt.Errorf("дубль #%d не готов (%s)", take, status)
			}
			from, to := argFloat(args, "from"), argFloat(args, "to")
			res, err := studio.RebuildSections(ctx, s.client, jobID,
				[]studio.SectionSpec{studio.RevoiceSpec(take, from, to, argFloat(args, "beat_sec"))})
			if err != nil {
				return "", err
			}
			// источник голоса новой версии — дубль (его голос подставлен)
			id, err := promoteVersion(s.client, jobID, res.Variant.File, argString(args, "title"),
				fmt.Sprintf("версия #%d · голос %.0f–%.0f с (дубль #%d)", jobID, from, to, take), take)
			if err != nil {
				return "", err
			}
			r := res.Inserts[0]
			return fmt.Sprintf("версия-трек #%d (голос дубля #%d в %.2f–%.2f с; сдвиг %.3f с, по бочке: %v)",
				id, take, from, to, r.StartSec, r.Aligned), nil
		},
	})

	s.Register(Tool{
		Name: "vocal_contour",
		Description: "Высота голоса по тактам плана (стем vocals, нужен make_stems): ноты по четвертям такта " +
			"(«D4», «·» — нет голоса) + медиана/диапазон Гц. Сверка «спето ли по плану» и не ушёл ли голос в писк.",
		InputSchema: props(map[string]any{
			"job_id": prop("ID трека", "integer"),
			"from":   prop("с какой секунды (по умолчанию 0)", "number"),
			"to":     prop("до какой секунды (0 — до конца)", "number"),
		}, "job_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			c, err := s.client.VocalContour(context.Background(), argInt(args, "job_id"), argFloat(args, "from"), argFloat(args, "to"))
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "голос: медиана %.1f Гц, диапазон %.1f–%.1f Гц\n", c.MedianHz, c.LowHz, c.HighHz)
			for _, bar := range c.Bars {
				fmt.Fprintf(&b, "%6.1f с  такт %d: %s\n", bar.Start, bar.Index, strings.Join(bar.Notes, " "))
			}
			return b.String(), nil
		},
	})
}

// ---------- склейка кусков версий ----------

func registerSpliceTools(s *Server) {
	s.Register(Tool{
		Name: "splice",
		Description: "Склеить куски версий в новую версию-трек (по порядку, с переходом crossfade): вернуть вырезанный " +
			"проигрыш, собрать лучшие куски дублей. Резать по границам тактов одного исполнения (job_score) — шва не слышно.",
		InputSchema: props(map[string]any{
			"job_id": prop("ID версии, к которой прикрепить результат (станет её вложением «📎»)", "integer"),
			"parts": map[string]any{"type": "array", "description": "куски: {job_id, from, to (0 — до конца), gain_db?}",
				"items": map[string]any{"type": "object"}},
			"crossfade": prop("переход между кусками, с (по умолчанию 0.05)", "number"),
			"title":     prop("название версии", "string"),
		}, "job_id", "parts"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			raw, ok := args["parts"].([]any)
			if !ok || len(raw) == 0 {
				return "", errors.New("parts: нужен непустой массив кусков")
			}
			parts := make([]studio.SplicePart, 0, len(raw))
			for i, it := range raw {
				m, ok := it.(map[string]any)
				if !ok {
					return "", fmt.Errorf("parts[%d]: нужен объект", i)
				}
				parts = append(parts, studio.SplicePart{JobID: argInt(m, "job_id"), From: argFloat(m, "from"),
					To: argFloat(m, "to"), GainDb: argFloat(m, "gain_db")})
			}
			ctx := context.Background()
			jobs, err := s.client.Jobs(ctx)
			if err != nil {
				return "", err
			}
			byID := make(map[int64]yue.Job, len(jobs))
			for _, j := range jobs {
				byID[j.ID] = j
			}
			baseID := argInt(args, "job_id")
			base, ok := byID[baseID]
			if !ok {
				return "", fmt.Errorf("трек #%d не найден", baseID)
			}
			for _, p := range parts {
				if _, ok := byID[p.JobID]; !ok {
					return "", fmt.Errorf("трек #%d не найден", p.JobID)
				}
			}
			v, err := studio.Splice(ctx, s.client, baseID, parts, argFloat(args, "crossfade"))
			if err != nil {
				return "", err
			}
			id, err := promoteVersion(s.client, baseID, v.File, argString(args, "title"),
				fmt.Sprintf("версия #%d · склейка", baseID), studio.VoiceSource(base, byID))
			if err != nil {
				return "", err
			}
			dur := 0.0
			if d, ok := v.Metrics["duration_sec"].(float64); ok {
				dur = d
			}
			return fmt.Sprintf("версия-трек #%d, длина %d:%02d", id, int(dur)/60, int(dur)%60), nil
		},
	})
}

// ---------- проверка изменённого плана ----------

func registerPlanTools(s *Server) {
	s.Register(Tool{
		Name: "plan_check",
		Description: "Проверить изменённый план (ABC) до генерации: что изменилось относительно плана трека " +
			"(такты по голосам, время), потолок голоса (верх мелодии + 2 ступени — выше модель пищит), " +
			"правки до отметки from (продолжение их не сыграет). То же делают continue_job/revoice_start с abc.",
		InputSchema: props(map[string]any{
			"job_id": prop("ID трека, чей план правится (для revoice — источник голоса)", "integer"),
			"abc":    prop("изменённый план", "string"),
			"from":   prop("отметка продолжения, с (необязательно)", "number"),
		}, "job_id", "abc"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			pc, err := s.client.PlanCheck(context.Background(), argInt(args, "job_id"), argString(args, "abc"), argFloat(args, "from"))
			if err != nil {
				return "", err
			}
			return formatPlanCheck(pc), nil
		},
	})
}

// planNote — проверка плана перед продолжением: пустой abc — без проверки;
// ошибка (битый план) — не ставим; иначе — сводка в конец ответа.
func planNote(s *Server, jobID int64, abc string, from float64) (string, error) {
	if strings.TrimSpace(abc) == "" {
		return "", nil
	}
	pc, err := s.client.PlanCheck(context.Background(), jobID, abc, from)
	var se *yue.StatusError
	if errors.As(err, &se) && se.Code == http.StatusUnprocessableEntity {
		return "", fmt.Errorf("план не принят: %w", err) // битый план — не ставим
	}
	if err != nil {
		// проверка недоступна (старый воркер без plan_check, нет плана, сбой) —
		// продолжение ставим, но честно говорим, что план не проверен
		return "\n⚠ проверка плана недоступна: " + err.Error(), nil
	}
	return "\n" + formatPlanCheck(pc), nil
}

// dash — «—» вместо пустого значения (нет нот у голоса, такта нет в плане)
func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// formatPlanCheck — сводка проверки плана для человека/агента.
func formatPlanCheck(pc *yue.PlanCheck) string {
	var b strings.Builder
	voices := make([]string, 0, len(pc.Bars))
	for v := range pc.Bars {
		voices = append(voices, v)
	}
	sort.Strings(voices)
	parts := make([]string, 0, len(voices))
	for _, v := range voices {
		n := pc.Bars[v]
		parts = append(parts, fmt.Sprintf("%s %d→%d", v, n[0], n[1]))
	}
	fmt.Fprintf(&b, "такты: %s; длина плана %.1f→%.1f с\n", strings.Join(parts, ", "), pc.Duration[0], pc.Duration[1])
	fmt.Fprintf(&b, "голос: верх было %s, потолок %s, верх стало %s\n",
		dash(pc.Ceiling.Top), dash(pc.Ceiling.Ceiling), dash(pc.Ceiling.NewTop))
	fmt.Fprintf(&b, "изменено тактов: %d\n", pc.ChangedTotal)
	for _, c := range pc.Changed {
		at := "      —"
		if c.Start != 0 || c.End != 0 {
			at = fmt.Sprintf("%7.2f", c.Start)
		}
		fmt.Fprintf(&b, "  %s с  %s такт %d: %s → %s\n", at, c.Voice, c.Bar, dash(c.Before), dash(c.After))
	}
	for _, w := range pc.Warnings {
		fmt.Fprintf(&b, "⚠ %s\n", w)
	}
	return b.String()
}

// ---------- «Убрать свист» ----------

func registerToneTools(s *Server) {
	s.Register(Tool{
		Name: "chord_grid",
		Description: "Аккорды и секции плана трека по ТАКТАМ ЗВУКА (не по времени плана — темп плана и звука расходятся): " +
			"{bpm, bars [{start, end, chord, section}]} в секундах трека. Для синт-партии: ноты по аккордам → блок движка " +
			"synth (notes [{t, d, midi[], vel}], t — секунды трека) → rebuild_sections запись {child_id: 0, stems: [\"mix\"], " +
			"add: true, engine: [synth…, эффекты…]}. Нет плана — сначала transcribe_job.",
		InputSchema: props(map[string]any{"job_id": prop("ID трека", "integer")}, "job_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			g, err := s.client.ChordGrid(context.Background(), argInt(args, "job_id"))
			if err != nil {
				return "", err
			}
			return toJSON(g), nil
		},
	})

	s.Register(Tool{
		Name: "beat_grid",
		Description: "Сетка долей трека для эффектов в такт: темп (BPM) и время сильной доли в окне [from, to) — " +
			"готовые bpm и offset для dsp chain «gate» («Ритм-гейт»). По дорожке барабанов, если сделан make_stems, " +
			"иначе по миксу. Окно — лучше то место, где будет эффект. strength < 0,1 — сетки по сути нет.",
		InputSchema: props(map[string]any{
			"job_id": prop("ID трека", "integer"),
			"from":   prop("с какой секунды (по умолчанию 0)", "number"),
			"to":     prop("до какой секунды (0 — до конца)", "number"),
		}, "job_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			g, err := s.client.JobGrid(context.Background(), argInt(args, "job_id"), argFloat(args, "from"), argFloat(args, "to"))
			if err != nil {
				return "", err
			}
			src := "по миксу"
			if g.Source == "drums" {
				src = "по барабанам"
			}
			return fmt.Sprintf("bpm %.2f, сильная доля %.3f с (offset для gate), попадание %.2f, %s", g.BPM, g.Offset, g.Strength, src), nil
		},
	})

	s.Register(Tool{
		Name: "find_tones",
		Description: "Узкие устойчивые тона («свист», писк) в миксе трека в окне [from, to): частота и насколько " +
			"выше окрестности, дБ; самый заметный первым. Частоты — в dsp_apply chain «dewhistle» (freq, freq2, freq3, start, end).",
		InputSchema: props(map[string]any{
			"job_id": prop("ID трека", "integer"),
			"from":   prop("с какой секунды (по умолчанию 0)", "number"),
			"to":     prop("до какой секунды (0 — до конца)", "number"),
			"stem":   prop("дорожка: vocals / drums / bass / other / guitar / piano (пусто — весь микс; нужен make_stems)", "string"),
		}, "job_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			tones, err := s.client.JobTones(context.Background(), argInt(args, "job_id"), argFloat(args, "from"),
				argFloat(args, "to"), argString(args, "stem"))
			if err != nil {
				return "", err
			}
			if len(tones) == 0 {
				return "узких тонов не найдено", nil
			}
			var b strings.Builder
			for _, t := range tones {
				fmt.Fprintf(&b, "%.1f Гц — на %.1f дБ выше окрестности\n", t.Hz, t.ProminenceDb)
			}
			return b.String(), nil
		},
	})
}

// promoteVersion — вариант пересборки → версия-трек под «📎»; пустое название —
// dflt; voiceSrc > 0 — чей голос подставлен (для следующих «перепеть»).
func promoteVersion(c yue.Service, jobID int64, file, title, dflt string, voiceSrc int64) (int64, error) {
	if title == "" {
		title = dflt
	}
	return c.VariantToTrack(context.Background(), jobID, file, title, voiceSrc)
}

// argNumMap — объект чисел из аргументов MCP ({param_id: число}); нет — nil.
func argNumMap(args map[string]any, key string) map[string]float64 {
	raw, ok := args[key].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]float64, len(raw))
	for k, v := range raw {
		if f, ok := v.(float64); ok {
			out[k] = f
		}
	}
	return out
}

// parseSectionSpecs — спеки пересборки из JSON-аргументов MCP (числа — float64).
func parseSectionSpecs(raw any) ([]studio.SectionSpec, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, errors.New("specs: нужен массив объектов")
	}
	out := make([]studio.SectionSpec, 0, len(list))
	for i, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("specs[%d]: нужен объект", i)
		}
		steps, err := parseSteps(m["steps"])
		if err != nil {
			return nil, fmt.Errorf("specs[%d]: %w", i, err)
		}
		engine, err := engineArg(m["engine"])
		if err != nil {
			return nil, fmt.Errorf("specs[%d]: %w", i, err)
		}
		out = append(out, studio.SectionSpec{
			ChildID: argInt(m, "child_id"), From: argFloat(m, "from"), To: argFloat(m, "to"),
			Lead: argFloat(m, "lead"), BeatSec: argFloat(m, "beat_sec"), Stems: argStringSlice(m, "stems"),
			Db: argFloat(m, "db"), FadeIn: argFloat(m, "fade_in"), FadeOut: argFloat(m, "fade_out"),
			KeepHighHz: argFloat(m, "keep_high_hz"), Revoice: argBool(m, "revoice"),
			Chain: argString(m, "chain"), Params: argNumMap(m, "params"), Steps: steps, Envelope: argEnvelope(m, "envelope"),
			Engine: engine, Add: argBool(m, "add"),
		})
	}
	return out, nil
}

// engineArg — цепочка звукового движка записи пересборки как есть ([{type, …}]);
// нет поля — nil; не массив объектов — ошибка (иначе запись тихо стала бы «громкостью»).
// Сами блоки проверяет воркер.
func engineArg(raw any) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, errors.New("engine: нужен массив блоков [{type, …}]")
	}
	out := make([]map[string]any, 0, len(list))
	for j, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("engine[%d]: нужен объект {type, …}", j)
		}
		out = append(out, m)
	}
	return out, nil
}

// argEnvelope — точки линии громкости [{t, db}]; нечисловые поля — 0,
// не-объекты пропускаются (проверка точек — dsp.NormalizeEnvelope).
func argEnvelope(args map[string]any, key string) []dsp.EnvPoint {
	raw, _ := args[key].([]any)
	var out []dsp.EnvPoint
	for _, it := range raw {
		if m, ok := it.(map[string]any); ok {
			out = append(out, dsp.EnvPoint{T: argFloat(m, "t"), Db: argFloat(m, "db")})
		}
	}
	return out
}
