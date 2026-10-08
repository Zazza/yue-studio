package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"yue-studio/internal/studio"
	"yue-studio/internal/yue"
)

// ---------- пресеты звука ----------

// argJSON — аргумент инструмента в Go-тип через JSON (списки объектов пресета); нет — без ошибки
func argJSON(args map[string]any, key string, out any) error {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}

func argPreset(args map[string]any) (yue.SoundPreset, error) {
	p := yue.SoundPreset{Name: argString(args, "name"), Note: argString(args, "note"),
		ReferenceJobID: argInt(args, "reference_job_id")}
	if err := argJSON(args, "specs", &p.Specs); err != nil {
		return p, err
	}
	err := argJSON(args, "final", &p.Final)
	return p, err
}

const presetSpecHelp = "specs — правки дорожек на весь трек: [{stems: [дорожки], ровно одно из engine (цепочка " +
	"движка, как в fx_apply) | chain+params (эффект dsp_chains) | steps (педали [{chain, params, off}]), db}]; " +
	"final — цепочка ffmpeg на весь микс после правок: [{chain, params, off}] (dsp_chains)."

func registerPresetTools(s *Server) {
	s.Register(Tool{
		Name: "sound_presets",
		Description: "Пресеты звука: сохранённые рецепты обработки трека (правки дорожек + финал на микс), " +
			"встроенные первыми (builtin — не меняются). Применить — sound_preset_apply; при создании трека — " +
			"submit с sound_preset_ids.",
		InputSchema: props(nil),
		Handler: func(s *Server, _ map[string]any) (string, error) {
			list, err := s.client.SoundPresets(context.Background())
			if err != nil {
				return "", err
			}
			return toJSON(list), nil
		},
	})
	s.Register(Tool{
		Name:        "sound_preset_create",
		Description: "Сохранить свой пресет звука. " + presetSpecHelp + " Нужно хоть что-то из specs/final.",
		InputSchema: props(map[string]any{
			"name":             prop("название (1–80 символов)", "string"),
			"note":             prop("описание: какой звук получается", "string"),
			"specs":            map[string]any{"type": "array", "description": "правки дорожек", "items": map[string]any{"type": "object"}},
			"final":            map[string]any{"type": "array", "description": "финал на весь микс", "items": map[string]any{"type": "object"}},
			"reference_job_id": prop("трек-эталон, по которому настраивался (необязательно)", "integer"),
		}, "name"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			p, err := argPreset(args)
			if err != nil {
				return "", err
			}
			out, err := s.client.SoundPresetCreate(context.Background(), p)
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})
	s.Register(Tool{
		Name:        "sound_preset_update",
		Description: "Заменить свой пресет целиком (встроенный — нельзя, 409). " + presetSpecHelp,
		InputSchema: props(map[string]any{
			"preset_id":        prop("id пресета", "integer"),
			"name":             prop("название", "string"),
			"note":             prop("описание", "string"),
			"specs":            map[string]any{"type": "array", "description": "правки дорожек", "items": map[string]any{"type": "object"}},
			"final":            map[string]any{"type": "array", "description": "финал на весь микс", "items": map[string]any{"type": "object"}},
			"reference_job_id": prop("трек-эталон (необязательно)", "integer"),
		}, "preset_id", "name"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			p, err := argPreset(args)
			if err != nil {
				return "", err
			}
			out, err := s.client.SoundPresetUpdate(context.Background(), argInt(args, "preset_id"), p)
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})
	s.Register(Tool{
		Name:        "sound_preset_delete",
		Description: "Удалить свой пресет звука (встроенный — нельзя). Деструктивно: спроси пользователя, confirm=true.",
		InputSchema: props(map[string]any{
			"preset_id": prop("id пресета", "integer"),
			"confirm":   prop("подтверждение пользователя", "boolean"),
		}, "preset_id", "confirm"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			if !argBool(args, "confirm") {
				return "", errConfirm
			}
			if err := s.client.SoundPresetDelete(context.Background(), argInt(args, "preset_id")); err != nil {
				return "", err
			}
			return "deleted=true", nil
		},
	})
	s.Register(Tool{
		Name: "sound_preset_apply",
		Description: "Применить пресет звука к готовому треку сейчас (синхронно, минуты): нет дорожек — разделит " +
			"(части барабанов — RoFormer), недостающие наборы сэмплов поставит, пересоберёт дорожки, финал — на " +
			"результате, новая версия-трек «<трек> · <пресет>». Ответ — id версии.",
		InputSchema: props(map[string]any{
			"job_id":    prop("ID трека", "integer"),
			"preset_id": prop("id пресета (sound_presets)", "integer"),
		}, "job_id", "preset_id"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			ctx := context.Background()
			list, err := s.client.SoundPresets(ctx)
			if err != nil {
				return "", err
			}
			pid := argInt(args, "preset_id")
			for _, p := range list {
				if p.ID == pid {
					res, err := studio.ApplySoundPreset(ctx, s.client, argInt(args, "job_id"), p)
					if err != nil {
						return "", err
					}
					return toJSON(map[string]any{"version_id": res.ChildID, "file": res.File}), nil
				}
			}
			return "", fmt.Errorf("нет пресета %d (см. sound_presets)", pid)
		},
	})
}
