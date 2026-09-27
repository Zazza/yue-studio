package mcp

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Данные библиотеки стилей хранятся как JSON рядом с кодом и генерируются
// из фронтендовых источников (frontend/src/slotOptions.js, groups.js, presets.js)
// скриптом tools/mcp-gendata.sh (make mcp-data). Ручная правка JSON — запрещена.

type styleItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Style string `json:"style"`
}

type styleGroup struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Items []styleItem `json:"items"`
}

// RegisterLibraryTools — библиотека стилей (та же, что в UI): группы, поиск,
// русско-английский словарь слотов для сборки строки стиля.
func RegisterLibraryTools(s *Server) {
	s.Register(Tool{
		Name:        "styles",
		Description: "Библиотека стилей: группы (встроенные + свои) и поиск. Возвращает стили с готовыми строками стиля для submit.",
		InputSchema: props(map[string]any{
			"search": prop("подстрока для поиска по названию стиля/группе (пусто = список групп)", "string"),
			"group":  prop("id группы — вернуть её стили", "string"),
		}),
		Handler: func(_ *Server, args map[string]any) (string, error) {
			groups := loadStyleGroups()
			if groups == nil {
				return "", fmt.Errorf("данные библиотеки не найдены (internal/mcp/style_groups.json); собери: make mcp-data")
			}
			if q := strings.ToLower(argString(args, "search")); q != "" {
				var found []map[string]any
				for _, g := range groups {
					for _, it := range g.Items {
						if strings.Contains(strings.ToLower(it.Name+" "+it.Style), q) {
							found = append(found, map[string]any{
								"group": g.Name, "id": it.ID, "name": it.Name, "style": it.Style,
							})
						}
					}
				}
				if found == nil {
					return "не найдено; посмотри список групп: styles без аргументов", nil
				}
				return toJSON(found), nil
			}
			if gid := argString(args, "group"); gid != "" {
				for _, g := range groups {
					if g.ID == gid {
						return toJSON(g.Items), nil
					}
				}
				return "", fmt.Errorf("группа %q не найдена (см. styles без аргументов)", gid)
			}
			out := make([]map[string]any, 0, len(groups))
			for _, g := range groups {
				out = append(out, map[string]any{"id": g.ID, "name": g.Name, "styles": len(g.Items)})
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name: "slot_options",
		Description: "Подсказки значений слотов стиля: русская фраза → английский тег YuE " +
			"(жанр, ритм, гитары, клавиши, голос, настроение, продакшн). Собирай строку стиля из них.",
		InputSchema: props(map[string]any{
			"slot":   prop("поле: genre/rhythm/guitars/keys/vocals/mood/production/language (пусто = все)", "string"),
			"search": prop("подстрока поиска по русским названиям", "string"),
		}),
		Handler: func(_ *Server, args map[string]any) (string, error) {
			data := loadSlotOptions()
			if data == nil {
				return "", fmt.Errorf("словарь слотов не найден (internal/mcp/slot_options.json); собери: make mcp-data")
			}
			slot := argString(args, "slot")
			q := strings.ToLower(argString(args, "search"))
			out := map[string]any{}
			for k, opts := range data {
				if slot != "" && k != slot {
					continue
				}
				var pairs [][2]string
				for _, p := range opts {
					if q == "" || strings.Contains(strings.ToLower(p[0]), q) {
						pairs = append(pairs, p)
					}
				}
				out[k] = pairs
			}
			if len(out) == 0 {
				return "", fmt.Errorf("нет такого поля: %q (genre/rhythm/guitars/keys/vocals/mood/production/language)", slot)
			}
			return toJSON(out), nil
		},
	})
}

// dataFS — данные библиотеки, вшитые в бинарник (генерация: make mcp-data).
//
//go:embed slot_options.json style_groups.json
var dataFS embed.FS

func loadStyleGroups() []styleGroup {
	b, err := dataFS.ReadFile("style_groups.json")
	if err != nil {
		return nil
	}
	var gs []styleGroup
	if json.Unmarshal(b, &gs) != nil {
		return nil
	}
	return gs
}

func loadSlotOptions() map[string][][2]string {
	b, err := dataFS.ReadFile("slot_options.json")
	if err != nil {
		return nil
	}
	var m map[string][][2]string
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}
