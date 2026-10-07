package mcp

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"yue-studio/internal/yue"
)

// Описание блоков движка (копия worker/fx_blocks.json) и готовые цепочки (из
// frontend/src/fxPresets.js) — генерирует make mcp-data, руками не править.
//
//go:embed fx_blocks.json
var fxBlocksJSON string

//go:embed fx_presets.json
var fxPresetsJSON string

// fxChainDoc — формат цепочки звукового движка для описаний инструментов.
const fxChainDoc = "Цепочка — массив блоков по порядку: " +
	"gate {threshold_db −50, range_db −40, attack_ms 1, release_ms 100}; " +
	"eq {highpass_hz 0=выкл, lowpass_hz 0=выкл, bands [{freq_hz, gain_db, q}]}; " +
	"comp {threshold_db −20, ratio 4, attack_ms 10, release_ms 100, makeup_db 0}; " +
	"drive {gain_db 12, mix 1, output_db 0}; " +
	"amp {model — имя захвата NAM из fx_assets, input_db 0 (от −20 дБFS RMS), output_db 0}; " +
	"cab {ir — IR из fx_assets, пусто — «лёгкий кабинет» срезом на cutoff_hz 7000; mix 1}; " +
	"reverb {ir пусто — встроенный зал, decay_s 1.5, predelay_ms 10, lowpass_hz 8000, wet 0.3}; " +
	"delay {time_ms 375, feedback 0.35, lowpass_hz 6000, wet 0.3}; " +
	"gain {gain_db 0} — громкость (перегруз и усилитель выравнивают выход по входу); " +
	"sampler {kit «osdk/kick», floor_db −18, output_db 0} — замена ударов части барабанов (source kick/snare) " +
	"сэмплами набора (fx_kit_install), пик в пик. " +
	"Пропущенные параметры — по умолчанию. Обработка не сдвигает звук (выход нота в ноту с исходником)."

// registerFxTools — звуковой движок воркера (POST /jobs/{id}/fx, /fx/assets).
func registerFxTools(s *Server) {
	s.Register(Tool{
		Name: "fx_apply",
		Description: "Звуковой движок на воркере (не ffmpeg): цепочка «педали → усилитель NAM → кабинет → пространство» " +
			"на весь трек (source=mix) или дорожку (vocals/drums/bass/other, guitar/piano, kick/snare/toms/hh/ride/crash). " +
			"Результат — вариант dsp-fx-*.flac в списке вариантов (dsp_variants, variant_track → в треки). " +
			"output=mix — трек, где дорожка заменена обработанной; solo — только обработанная дорожка. " +
			"from/to — окно в секундах (вне окна трек не меняется). Блок amp идёт через очередь видеокарты. " + fxChainDoc,
		InputSchema: props(map[string]any{
			"job_id": prop("ID джобы", "integer"),
			"source": prop("mix — весь трек | имя дорожки (дорожек нет — делаются)", "string"),
			"chain":  prop("массив блоков [{type, …параметры}] по порядку", "array", map[string]any{"items": map[string]any{"type": "object"}}),
			"from":   prop("начало окна, с (необязательно)", "number"),
			"to":     prop("конец окна, с (необязательно)", "number"),
			"output": prop("mix (по умолчанию) | solo", "string"),
			"label":  prop("подпись варианта (необязательно)", "string"),
			"preview": prop("true — только прослушать кусок окна from–to (+ хвост реверба/дилея до 3 с): "+
				"файл preview-fx-*.flac, в варианты не попадает, повтор тех же настроек — без пересчёта", "boolean"),
			"fade": prop("у превью: плавные края входа окна, с (0…0,5; рост с from, спад после to) — как у "+
				"вычитаемой дорожки в пересборке студии; по умолчанию 0", "number"),
		}, "job_id", "chain"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			chain, err := argObjects(args, "chain")
			if err != nil {
				return "", err
			}
			req := yue.FxRequest{Source: argString(args, "source"), Chain: chain,
				Output: argString(args, "output"), Label: argString(args, "label")}
			if req.Source == "" {
				req.Source = "mix"
			}
			req.From, req.To = optFloat(args, "from"), optFloat(args, "to")
			req.Preview = argBool(args, "preview")
			req.Fade = argFloat(args, "fade")
			out, err := s.client.ApplyFx(context.Background(), argInt(args, "job_id"), req)
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name: "fx_blocks",
		Description: "Описание блоков звукового движка для fx_apply: параметры каждого блока с умолчанием, " +
			"границами, шагом и подписями ru/en (zero_off — 0 значит «выкл»), строковые (захват/IR), полосы eq.",
		InputSchema: props(nil),
		Handler: func(s *Server, args map[string]any) (string, error) {
			return fxBlocksJSON, nil
		},
	})

	s.Register(Tool{
		Name: "fx_presets",
		Description: "Готовые цепочки звукового движка (те же, что на странице «Инструменты»): " +
			"{id, name, note, chain} — chain сразу годится в fx_apply. Пустой amp.model — подставь захват из fx_assets.",
		InputSchema: props(nil),
		Handler: func(s *Server, args map[string]any) (string, error) {
			return fxPresetsJSON, nil
		},
	})

	s.Register(Tool{
		Name: "fx_assets",
		Description: "Захваты усилителей NAM (.nam, с замеренной задержкой) и импульсные отклики IR (.wav), " +
			"загруженные на воркер для fx_apply (amp.model, cab.ir, reverb.ir). В поставку не входят — fx_asset_upload.",
		InputSchema: props(nil),
		Handler: func(s *Server, args map[string]any) (string, error) {
			out, err := s.client.FxAssets(context.Background())
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name: "fx_kit_install",
		Description: "Скачать на воркер набор сэмплов барабанов для блока sampler (замена ударов): osdk — " +
			"The Open Source Drum Kit (бочка, малый; общественное достояние). Повтор — без перекачки. " +
			"Наборы и их части — в fx_assets (kits: «osdk/kick», «osdk/snare»).",
		InputSchema: props(map[string]any{"name": prop("набор: osdk", "string")}, "name"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			out, err := s.client.InstallFxKit(context.Background(), argString(args, "name"))
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})

	s.Register(Tool{
		Name: "fx_asset_upload",
		Description: "Загрузить на воркер захват NAM (kind=amp, файл .nam) или IR кабинета/зала (kind=ir, .wav) " +
			"с ПК. Имя на воркере — имя файла. Лицензия захвата — на совести пользователя: не скачивать чужое без его просьбы.",
		InputSchema: props(map[string]any{
			"kind": prop("amp | ir", "string"),
			"path": prop("путь к файлу на ПК", "string"),
		}, "kind", "path"),
		Handler: func(s *Server, args map[string]any) (string, error) {
			path := argString(args, "path")
			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("не прочитать %s: %w", path, err)
			}
			out, err := s.client.UploadFxAsset(context.Background(), argString(args, "kind"), filepath.Base(path), data)
			if err != nil {
				return "", err
			}
			return toJSON(out), nil
		},
	})
}

// argObjects — массив объектов из JSON-аргумента как есть (цепочка движка).
func argObjects(args map[string]any, key string) ([]map[string]any, error) {
	raw, ok := args[key].([]any)
	if !ok || len(raw) == 0 {
		return nil, errors.New(key + ": нужен непустой массив объектов")
	}
	out := make([]map[string]any, 0, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s[%d]: нужен объект", key, i)
		}
		out = append(out, m)
	}
	return out, nil
}

// optFloat — числовой аргумент, если передан (0 — тоже значение); nil — не передан.
func optFloat(args map[string]any, key string) *float64 {
	if _, ok := args[key]; !ok {
		return nil
	}
	v := argFloat(args, key)
	return &v
}
