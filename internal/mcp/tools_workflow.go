package mcp

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"time"

	"yue-studio/internal/yue"
)

// RegisterWorkflowTools — генерация: статусы, отправка, план, артефакты.
func RegisterWorkflowTools(s *Server) {
	s.Register(Tool{
		Name:        "status",
		Description: "Статус воркера Yue Studio: доступность, загружена ли модель YuE2 в память.",
		InputSchema: props(nil),
		Handler: func(s *Server, _ map[string]any) (string, error) {
			h, err := s.client.Health(context.Background())
			if err != nil {
				return "", fmt.Errorf("воркер недоступен (%s): %w — проверь адрес через config_get, установка: install_worker", s.client.GetURL(), err)
			}
			return toJSON(h), nil
		},
	})

	s.Register(Tool{
		Name:        "jobs",
		Description: "Список джоб генерации: статусы (queued/running/done/error/canceled), стили, длительности.",
		InputSchema: props(nil),
		Handler: func(s *Server, _ map[string]any) (string, error) {
			jobs, err := s.client.Jobs(context.Background())
			if err != nil {
				return "", err
			}
			return toJSON(jobs), nil
		},
	})

	s.Register(Tool{
		Name: "submit",
		Description: "Поставить трек в очередь генерации. Стиль — строка английских тегов YuE " +
			"(подсказки значений — styles); стих — с секциями [Verse]/[Chorus]; n — веер best-of-N (сиды base+0..n-1). " +
			"Инструментал — lyrics='[Instrumental]' (для длины повторить секции).",
		InputSchema: props(map[string]any{
			"style":  prop("строка стиля (англ. теги: жанр, инструменты, настроение, BPM)", "string"),
			"lyrics": prop("стих с [Verse]/[Chorus] или [Instrumental]", "string"),
			"title":  prop("название", "string"),
			"seed":   prop("сид (0 = случайный)", "integer"),
			"cot":    prop("режим размышлений: full | melody | off", "string"),
			"n":      prop("веер: число джоб с сидами base+0..n-1 (1..10)", "integer"),
		}, "style", "lyrics"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			p := yue.SubmitParams{
				Title:  argString(args, "title"),
				Style:  argString(args, "style"),
				Lyrics: argString(args, "lyrics"),
				Seed:   argInt(args, "seed"),
				Cot:    argString(args, "cot"),
			}
			if p.Cot == "" {
				p.Cot = "full"
			}
			ctx := context.Background()
			n := int(argInt(args, "n"))
			if n > 1 {
				// веер best-of-N: сиды base+0..n-1 (как YueSubmitFan в приложении)
				if n > 10 {
					n = 10
				}
				base := p.Seed
				if base <= 0 {
					base = time.Now().UnixNano() % 1_000_000_000
				}
				ids := make([]int64, 0, n)
				for i := 0; i < n; i++ {
					q := p
					q.Seed = base + int64(i)
					if p.Title != "" {
						q.Title = fmt.Sprintf("%s [%d/%d]", p.Title, i+1, n)
					}
					id, err := s.client.Submit(ctx, q)
					if err != nil {
						return fmt.Sprintf("поставлено %d из %d джоб, ошибка: %v", len(ids), n, err), nil
					}
					ids = append(ids, id)
				}
				return "поставлено джоб: " + fmt.Sprint(ids), nil
			}
			id, err := s.client.Submit(ctx, p)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("джоба #%d в очереди. Статус: jobs", id), nil
		},
	})

	s.Register(Tool{
		Name:        "plan",
		Description: "План трека (ABC-партитура) без рендера: посмотреть/поправить ноты до генерации. Медленно (GPU).",
		InputSchema: props(map[string]any{
			"style":  prop("строка стиля (англ. теги)", "string"),
			"lyrics": prop("стих с секциями", "string"),
			"seed":   prop("сид (0 = случайный)", "integer"),
		}, "style", "lyrics"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			r, err := s.client.Plan(context.Background(), yue.PlanParams{
				Style:  argString(args, "style"),
				Lyrics: argString(args, "lyrics"),
				Seed:   argInt(args, "seed"),
				Cot:    "full",
			})
			if err != nil {
				return "", err
			}
			return toJSON(r), nil
		},
	})

	s.Register(Tool{
		Name:        "render_abc",
		Description: "Рендер трека по своей ABC-партитуре (из plan или score.abc джобы).",
		InputSchema: props(map[string]any{
			"abc":    prop("ABC-партитура", "string"),
			"style":  prop("строка стиля (англ. теги)", "string"),
			"lyrics": prop("стих", "string"),
			"title":  prop("название", "string"),
			"seed":   prop("сид", "integer"),
		}, "abc", "style"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			p := yue.SubmitParams{
				Title:  argString(args, "title"),
				Style:  argString(args, "style"),
				Lyrics: argString(args, "lyrics"),
				Seed:   argInt(args, "seed"),
				Cot:    "melody", // abc требует full|melody
				Abc:    argString(args, "abc"),
			}
			id, err := s.client.Submit(context.Background(), p)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("джоба #%d в очереди (рендер по своему ABC)", id), nil
		},
	})

	s.Register(Tool{
		Name:        "cancel",
		Description: "Отменить джобу: в очереди — снять, идущую — остановить генерацию. Деструктивное: требует confirm=true (спроси пользователя).",
		InputSchema: props(map[string]any{
			"job_id":  prop("ID джобы", "integer"),
			"confirm": prop("явное подтверждение пользователя", "boolean"),
		}, "job_id", "confirm"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			if !argBool(args, "confirm") {
				return "", errConfirm
			}
			ok, err := s.client.Cancel(context.Background(), argInt(args, "job_id"))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("canceled=%v", ok), nil
		},
	})

	s.Register(Tool{
		Name: "delete_job",
		Description: "Удалить джобу со всеми файлами (аудио, партитура, стемы). Необратимо. " +
			"Деструктивное: требует confirm=true (спроси пользователя).",
		InputSchema: props(map[string]any{
			"job_id":  prop("ID джобы", "integer"),
			"confirm": prop("явное подтверждение пользователя", "boolean"),
		}, "job_id", "confirm"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			if !argBool(args, "confirm") {
				return "", errConfirm
			}
			ok, err := s.client.DeleteJob(context.Background(), argInt(args, "job_id"))
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("deleted=%v", ok), nil
		},
	})

	s.Register(Tool{
		Name: "artifacts",
		Description: "Скачать артефакт джобы в папку и вернуть путь: трек (flac/mp3/wav) или партитуру score.abc. " +
			"Скачивается в каталог загрузок MCP; аудио агент не проигрывает — отдай путь пользователю.",
		InputSchema: props(map[string]any{
			"job_id": prop("ID джобы", "integer"),
			"file":   prop("имя файла: audio.flac / audio.mp3 / audio.wav / score.abc / request.abc (пусто = лучший трек)", "string"),
		}, "job_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			id := argInt(args, "job_id")
			file := argString(args, "file")
			if file == "" {
				jobs, err := s.client.Jobs(context.Background())
				if err != nil {
					return "", err
				}
				for _, j := range jobs {
					if j.ID == id {
						file = j.Mp3File
						if file == "" {
							file = j.AudioFile
						}
						break
					}
				}
				if file == "" {
					return "", fmt.Errorf("job %d has no audio", id)
				}
			}
			path, err := s.download(id, file)
			if err != nil {
				return "", err
			}
			abs, _ := filepath.Abs(path)
			return "сохранено: " + abs, nil
		},
	})

	s.Register(Tool{
		Name:        "job_score",
		Description: "Таймлайн партитуры джобы для пиано-ролла: такты × голоса, аккорды, секции, секунды.",
		InputSchema: props(map[string]any{
			"job_id": prop("ID джобы", "integer"),
		}, "job_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			out, err := s.client.JobScore(context.Background(), argInt(args, "job_id"))
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name:        "job_preview",
		Description: "Превью фрагмента джобы (VAE-decode куска латентов — секунды, без AR-генерации); возвращает файл для скачивания.",
		InputSchema: props(map[string]any{
			"job_id":   prop("ID джобы", "integer"),
			"from_sec": prop("начало, сек", "number"),
			"to_sec":   prop("конец, сек", "number"),
		}, "job_id", "from_sec", "to_sec"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			out, err := s.client.JobPreview(context.Background(), argInt(args, "job_id"),
				argFloat(args, "from_sec"), argFloat(args, "to_sec"))
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name:        "transcribe",
		Description: "Транскрипция трека в ABC (SheetSage2) — для каверов по чужой/своей мелодии.",
		InputSchema: props(map[string]any{
			"path": prop("путь к аудиофайлу (flac/mp3/wav/ogg/m4a)", "string"),
		}, "path"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			data, err := os.ReadFile(argString(args, "path"))
			if err != nil {
				return "", err
			}
			name := filepath.Base(argString(args, "path"))
			out, err := s.client.Transcribe(context.Background(), name, data)
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})
}

var errConfirm = fmt.Errorf("требуется подтверждение: спроси пользователя и передай confirm=true")

// download скачивает артефакт джобы в каталог загрузок, возвращает путь.
func (s *Server) download(id int64, file string) (string, error) {
	body, _, err := s.client.FetchAudio(context.Background(), id, file)
	if err != nil {
		return "", err
	}
	defer body.Close()
	if err := os.MkdirAll(s.downloadDir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("yue-%d-%s", id, file)
	path := filepath.Join(s.downloadDir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, body); err != nil {
		return "", err
	}
	return path, nil
}
