package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"yue-studio/internal/config"
	"yue-studio/internal/dsp"
	"yue-studio/internal/yue"
)

// RegisterStudioTools — настройка адреса, Ollama, DSP, стемы, корпуса.
func RegisterStudioTools(s *Server) {
	s.Register(Tool{
		Name:        "config_get",
		Description: "Текущие настройки: адрес воркера, Ollama (url/модель), пути данных воркера.",
		InputSchema: props(nil),
		Handler: func(s *Server, _ map[string]any) (string, error) {
			out := map[string]any{"server_url": s.client.GetURL()}
			if cfg, err := s.client.WorkerConfig(context.Background()); err == nil {
				out["worker"] = cfg
			} else {
				out["worker"] = "недоступен: " + err.Error()
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name: "config_set",
		Description: "Настройки: server_url — адрес воркера (сохраняется и в settings приложения); " +
			"ollama_url / ollama_model — применяются на лету.",
		InputSchema: props(map[string]any{
			"server_url":   prop("адрес воркера, напр. http://gpu-host:8091", "string"),
			"ollama_url":   prop("URL Ollama (api/chat)", "string"),
			"ollama_model": prop("модель Ollama", "string"),
		}),
		Handler: func(s *Server, args map[string]any) (string, error) {
			if u := argString(args, "server_url"); u != "" {
				s.client.SetURL(strings.TrimRight(u, "/"))
				config.SaveSettings(config.Settings{ServerURL: s.client.GetURL()})
			}
			cfg := map[string]any{}
			if v := argString(args, "ollama_url"); v != "" {
				cfg["ollama_url"] = v
			}
			if v := argString(args, "ollama_model"); v != "" {
				cfg["ollama_model"] = v
			}
			if len(cfg) > 0 {
				if err := s.client.SetWorkerConfig(context.Background(), cfg); err != nil {
					return "", fmt.Errorf("воркер не принял настройки: %w (старая версия или недоступен)", err)
				}
			}
			return "ок. Проверь config_get", nil
		},
	})

	// ---------- DSP и метрики ----------

	s.Register(Tool{
		Name:        "dsp_chains",
		Description: "Пресеты DSP-цепочек эффектов (ffmpeg на ПК): список, параметры и диапазоны крутилок.",
		InputSchema: props(nil),
		Handler: func(s *Server, _ map[string]any) (string, error) {
			return toJSON(dsp.All()), nil
		},
	})

	s.Register(Tool{
		Name: "dsp_apply",
		Description: "Применить DSP-цепочку к треку джобы (ffmpeg локально): «стена громкости», «кассета» и т.д. " +
			"params — {id параметра: число} (диапазоны — dsp_chains).",
		InputSchema: props(map[string]any{
			"job_id": prop("ID джобы", "integer"),
			"chain":  prop("id цепочки (см. dsp_chains)", "string"),
			"params": prop("значения крутилок {param_id: число}", "object"),
		}, "job_id", "chain"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			params := map[string]float64{}
			if raw, ok := args["params"].(map[string]any); ok {
				for k, v := range raw {
					if f, ok := v.(float64); ok {
						params[k] = f
					}
				}
			}
			v, err := s.applyDsp(argInt(args, "job_id"), argString(args, "chain"), params, false)
			if err != nil {
				return "", err
			}
			return toJSON(v), nil
		},
	})

	s.Register(Tool{
		Name:        "dsp_preview",
		Description: "Превью DSP-цепочки: 15-секундный кусок трека через эффекты (быстро послушать результат).",
		InputSchema: props(map[string]any{
			"job_id": prop("ID джобы", "integer"),
			"chain":  prop("id цепочки", "string"),
			"params": prop("значения крутилок {param_id: число}", "object"),
		}, "job_id", "chain"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			params := map[string]float64{}
			if raw, ok := args["params"].(map[string]any); ok {
				for k, v := range raw {
					if f, ok := v.(float64); ok {
						params[k] = f
					}
				}
			}
			v, err := s.applyDsp(argInt(args, "job_id"), argString(args, "chain"), params, true)
			if err != nil {
				return "", err
			}
			return toJSON(v), nil
		},
	})

	s.Register(Tool{
		Name:        "dsp_variants",
		Description: "Список применённых DSP-вариантов джобы (с метриками).",
		InputSchema: props(map[string]any{
			"job_id": prop("ID джобы", "integer"),
		}, "job_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			vs, err := s.client.JobDspVariants(context.Background(), argInt(args, "job_id"))
			if err != nil {
				return "", err
			}
			return toJSON(vs), nil
		},
	})

	s.Register(Tool{
		Name:        "analyze_job",
		Description: "Метрики трека джобы (librosa): темп, крест-фактор, динамика, спектр. Для сравнения вариантов.",
		InputSchema: props(map[string]any{
			"job_id": prop("ID джобы", "integer"),
		}, "job_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			out, err := s.client.AnalyzeJob(context.Background(), argInt(args, "job_id"))
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	// ---------- стемы / минус / овердаб / импорт ----------

	s.Register(Tool{
		Name:        "make_stems",
		Description: "Разделить трек джобы на стемы demucs (drums/bass/other/vocals). Медленно при первом вызове.",
		InputSchema: props(map[string]any{
			"job_id": prop("ID джобы", "integer"),
		}, "job_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			out, err := s.client.MakeStems(context.Background(), argInt(args, "job_id"))
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name:        "dsp_variant_delete",
		Description: "Удалить вариант эффекта/вклейки (файл + метрики). Деструктивное: требует confirm=true.",
		InputSchema: props(map[string]any{
			"job_id":  prop("ID джобы", "integer"),
			"file":    prop("имя файла варианта (dsp-*.flac / overdub-*.flac)", "string"),
			"confirm": prop("явное подтверждение пользователя", "boolean"),
		}, "job_id", "file", "confirm"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			if !argBool(args, "confirm") {
				return "", errConfirm
			}
			ok, err := s.client.DspVariantDelete(context.Background(),
				argInt(args, "job_id"), argString(args, "file"))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("deleted=%v", ok), nil
		},
	})

	s.Register(Tool{
		Name:        "variant_track",
		Description: "Вариант DSP-эффекта (dsp-*.flac) отдельным треком с подписью эффекта — дальше работают стемы/минус/эффекты.",
		InputSchema: props(map[string]any{
			"job_id":    prop("ID джобы с вариантом", "integer"),
			"file":      prop("имя файла варианта, напр. dsp-tape.flac", "string"),
			"title":     prop("название нового трека (обычно «исходное · эффект»)", "string"),
			"voice_src": prop("ID рендера, чей голос подставлен в эту версию (после «перепеть»); 0 — голос не менялся", "integer"),
		}, "job_id", "file"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			id, err := s.client.VariantToTrack(context.Background(),
				argInt(args, "job_id"), argString(args, "file"), argString(args, "title"), argInt(args, "voice_src"))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("вариант стал треком #%d", id), nil
		},
	})

	s.Register(Tool{
		Name: "continue_job",
		Description: "«Продолжение с места»: новый трек = трек до from_sec + продолжение моделью " +
			"(то же исполнение до отметки, без склейки; план и звучание можно изменить). Трек-вложение под родителем.",
		InputSchema: props(map[string]any{
			"job_id":    prop("ID готовой джобы", "integer"),
			"from_sec":  prop("с какой секунды играть заново", "number"),
			"seed":      prop("сид продолжения; 0 — случайный", "integer"),
			"abc":       prop("изменённый план (необязательно)", "string"),
			"style_add": prop("что изменить в звучании с этого места, напр. electric guitar enters and builds", "string"),
		}, "job_id", "from_sec"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			from, _ := args["from_sec"].(float64)
			id, err := s.client.ContinueJob(context.Background(), argInt(args, "job_id"), from,
				argInt(args, "seed"), argString(args, "abc"), argString(args, "style_add"))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("продолжение поставлено: #%d", id), nil
		},
	})

	s.Register(Tool{
		Name:        "set_head",
		Description: "Основная версия песни: job_id — корень, head_id — он сам (0) или его версия-потомок.",
		InputSchema: props(map[string]any{
			"job_id":  prop("ID корня песни", "integer"),
			"head_id": prop("ID версии; 0 — сам трек", "integer"),
		}, "job_id", "head_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			if err := s.client.SetHead(context.Background(), argInt(args, "job_id"), argInt(args, "head_id")); err != nil {
				return "", err
			}
			return "основная версия обновлена", nil
		},
	})

	s.Register(Tool{
		Name:        "make_minus",
		Description: "Минус-трек: микс стемов без выбранных групп (напр. vocals для караоке).",
		InputSchema: props(map[string]any{
			"job_id": prop("ID джобы", "integer"),
			"exclude": prop("что убрать: drums/bass/other/vocals", "array",
				map[string]any{"items": map[string]any{"type": "string"}}),
		}, "job_id", "exclude"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			out, err := s.client.MakeMinus(context.Background(), argInt(args, "job_id"), argStringSlice(args, "exclude"))
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name: "overdub",
		Description: "Овердаб: партия поверх трека джобы по его партитуре с новым стилем + микс (gain 0.1–1). " +
			"lyrics — текст голосовой партии: без него модель импровизирует вокализ (часто несуразный).",
		InputSchema: props(map[string]any{
			"job_id": prop("ID джобы", "integer"),
			"style":  prop("строка стиля партии (англ. теги)", "string"),
			"lyrics": prop("текст голосовой партии (пусто = вокализ; [Instrumental] = без голоса)", "string"),
			"gain":   prop("гейн микса (0.1–1, по умолчанию 0.5)", "number"),
			"abc":    prop("свой план партии (пусто = партитура джобы); инструмент-только-в-куске строит soloInstrumentPlan", "string"),
			"seed":   prop("сид родителя = «та же интерпретация», ближе к оригиналу (0 = новая)", "integer"),
		}, "job_id", "style"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			gain := argFloat(args, "gain")
			if gain == 0 {
				gain = 0.5
			}
			id, err := s.client.SubmitOverdub(context.Background(), argInt(args, "job_id"),
				argString(args, "style"), argString(args, "lyrics"), gain, argString(args, "abc"),
				argInt(args, "seed"))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("джоба-овердаб #%d в очереди", id), nil
		},
	})

	s.Register(Tool{
		Name:        "recognize_lyrics",
		Description: "Распознать текст трека (faster-whisper): оригинал для кавера, дальше lyrics_adapt или правка руками.",
		InputSchema: props(map[string]any{
			"path": prop("путь к аудиофайлу (flac/mp3/wav/ogg/m4a)", "string"),
		}, "path"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			data, err := os.ReadFile(argString(args, "path"))
			if err != nil {
				return "", err
			}
			out, err := s.client.RecognizeLyrics(context.Background(), argString(args, "path"), data)
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name:        "job_lyrics",
		Description: "Распознать текст из готового аудио джобы (faster-whisper): без повторной загрузки файла — для овердаба/кавера этой же джобы.",
		InputSchema: props(map[string]any{
			"job_id": prop("ID джобы (статус done)", "integer"),
		}, "job_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			out, err := s.client.JobLyrics(context.Background(), argInt(args, "job_id"))
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name: "lyrics_adapt",
		Description: "Адаптация-перевод лирики под пение: сохранение числа строк и слогов (±1) на строку, " +
			"секционные теги [Verse]/[Chorus] остаются. Для каверов на другом языке.",
		InputSchema: props(map[string]any{
			"text": prop("исходный текст (можно с [Verse]/[Chorus])", "string"),
			"to":   prop("язык результата (Russian, English, ...; по умолчанию Russian)", "string"),
		}, "text"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			to := argString(args, "to")
			if to == "" {
				to = "Russian"
			}
			out, err := s.client.AdaptLyrics(context.Background(), argString(args, "text"), to)
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name: "import_track",
		Description: "Импорт своего трека как джобы (дальше работают стемы/минус/эффекты/овердаб). " +
			"transcribe=true — сразу транскрипция для ролла (медленно).",
		InputSchema: props(map[string]any{
			"path":       prop("путь к аудиофайлу (flac/mp3/wav/ogg/m4a)", "string"),
			"transcribe": prop("сделать транскрипцию (для пиано-ролла/овердаба)", "boolean"),
		}, "path"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			data, err := os.ReadFile(argString(args, "path"))
			if err != nil {
				return "", err
			}
			if len(data) > 200<<20 {
				return "", fmt.Errorf("файл слишком большой (>200 МБ)")
			}
			out, err := s.client.ImportTrack(context.Background(), filepath.Base(argString(args, "path")), data, argBool(args, "transcribe"))
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	// ---------- корпуса (профили исполнителей) ----------

	s.Register(Tool{
		Name:        "corpus_list",
		Description: "Список корпусов (профили исполнителей из 3–10 треков).",
		InputSchema: props(nil),
		Handler: func(s *Server, _ map[string]any) (string, error) {
			out, err := s.client.CorpusList(context.Background())
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name:        "corpus_create",
		Description: "Создать корпус (профиль исполнителя): дальше добавить треки и собрать профиль.",
		InputSchema: props(map[string]any{
			"name": prop("имя профиля, напр. «блюз 60-х»", "string"),
		}, "name"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			id, err := s.client.CorpusCreate(context.Background(), argString(args, "name"))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("корпус #%d создан. Добавь треки: corpus_add_tracks", id), nil
		},
	})

	s.Register(Tool{
		Name:        "corpus_add_tracks",
		Description: "Добавить треки в корпус (каждый: DSP-паспорт + транскрипция + текст; минуты на трек).",
		InputSchema: props(map[string]any{
			"corpus_id": prop("ID корпуса", "integer"),
			"paths": prop("пути к аудиофайлам (3–10 одного исполнителя/периода)", "array",
				map[string]any{"items": map[string]any{"type": "string"}}),
		}, "corpus_id", "paths"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			id := argInt(args, "corpus_id")
			loaded := 0
			var lastErr error
			for _, p := range argStringSlice(args, "paths") {
				data, err := os.ReadFile(p)
				if err != nil {
					lastErr = err
					continue
				}
				if _, err := s.client.CorpusAddTrack(context.Background(), id, filepath.Base(p), data); err != nil {
					lastErr = err
					continue
				}
				loaded++
			}
			if loaded == 0 && lastErr != nil {
				return "", lastErr
			}
			return fmt.Sprintf("загружено треков: %d. Дальше: corpus_build", loaded), nil
		},
	})

	s.Register(Tool{
		Name:        "corpus_build",
		Description: "Собрать профиль корпуса: тональности/прогрессии/темп/структура + строка стиля (Ollama).",
		InputSchema: props(map[string]any{
			"corpus_id": prop("ID корпуса", "integer"),
		}, "corpus_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			out, err := s.client.CorpusBuild(context.Background(), argInt(args, "corpus_id"))
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name:        "corpus_get",
		Description: "Профиль корпуса: статистика, строка стиля, нотный шаблон ABC.",
		InputSchema: props(map[string]any{
			"corpus_id": prop("ID корпуса", "integer"),
		}, "corpus_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			out, err := s.client.CorpusGet(context.Background(), argInt(args, "corpus_id"))
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	// ---------- голоса (примерочная) ----------

	s.Register(Tool{
		Name: "voices_list",
		Description: "Карточки голосов примерочной: ручки характера (params), seed прослушивания, " +
			"жива ли исходная джоба (job_alive).",
		InputSchema: props(nil),
		Handler: func(s *Server, _ map[string]any) (string, error) {
			out, err := s.client.Voices(context.Background())
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name: "voice_create",
		Description: "Сохранить карточку голоса из джобы-прослушивания (draft-джоба примерочной). " +
			"params — JSON ручек: {register, rough, creak, delivery, breath, extra}.",
		InputSchema: props(map[string]any{
			"name":   prop("имя голоса", "string"),
			"job_id": prop("ID готовой джобы-прослушивания", "integer"),
			"params": prop("JSON ручек примерочной", "string"),
			"seed":   prop("seed прослушивания (для воспроизводимости голоса)", "integer"),
		}, "name", "job_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			id, err := s.client.VoiceCreate(context.Background(),
				argString(args, "name"), argInt(args, "job_id"),
				argString(args, "params"), argInt(args, "seed"))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("голос #%d сохранён (джоба #%d)", id, argInt(args, "job_id")), nil
		},
	})

	s.Register(Tool{
		Name: "voice_delete",
		Description: "Удалить карточку голоса и копию аудио на сервере. Необратимо. " +
			"Деструктивное: требует confirm=true (спроси пользователя).",
		InputSchema: props(map[string]any{
			"voice_id": prop("ID карточки голоса", "integer"),
			"confirm":  prop("явное подтверждение пользователя", "boolean"),
		}, "voice_id", "confirm"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			if !argBool(args, "confirm") {
				return "", errConfirm
			}
			ok, err := s.client.VoiceDelete(context.Background(), argInt(args, "voice_id"))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("deleted=%v", ok), nil
		},
	})
}

// applyDsp — конвейер как в приложении (app.go): скачать flac → ffmpeg → залить вариант.
func (s *Server) applyDsp(jobID int64, chainID string, params map[string]float64, preview bool) (*yue.DspVariant, error) {
	chain := dsp.ByID(chainID)
	if chain == nil {
		return nil, fmt.Errorf("unknown chain %q (см. dsp_chains)", chainID)
	}
	jobs, err := s.client.Jobs(context.Background())
	if err != nil {
		return nil, err
	}
	audio := ""
	for _, j := range jobs {
		if j.ID == jobID {
			audio = j.AudioFile
			break
		}
	}
	if audio == "" {
		return nil, fmt.Errorf("job %d has no audio", jobID)
	}
	body, _, err := s.client.FetchAudio(context.Background(), jobID, audio)
	if err != nil {
		return nil, err
	}
	tmpIn, err := os.CreateTemp("", fmt.Sprintf("yue-mcp-dsp-%d-in-*.flac", jobID))
	if err != nil {
		_ = body.Close()
		return nil, err
	}
	_, cpErr := tmpIn.ReadFrom(body)
	_ = body.Close()
	tmpIn.Close()
	if cpErr != nil {
		os.Remove(tmpIn.Name())
		return nil, cpErr
	}
	defer os.Remove(tmpIn.Name())

	tmpOut, err := os.CreateTemp("", fmt.Sprintf("yue-mcp-dsp-%d-out-*.flac", jobID))
	if err != nil {
		return nil, err
	}
	tmpOut.Close()
	defer os.Remove(tmpOut.Name())

	var span *dsp.Span
	if preview {
		span = &dsp.Span{StartSec: 20, DurSec: 15}
	}
	if err := dsp.Run(tmpIn.Name(), tmpOut.Name(), chain.FilterGraph(params), span); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(tmpOut.Name())
	if err != nil {
		return nil, err
	}
	fname := fmt.Sprintf("dsp-%s.flac", chainID)
	if preview {
		fname = fmt.Sprintf("dsp-preview-%s.flac", chainID)
	}
	return s.client.UploadDsp(context.Background(), jobID, fname, data)
}
