# MCP-сервер (yue-mcp)

`yue-mcp` — [Model Context Protocol](https://modelcontextprotocol.io)-сервер (stdio) для
управления Yue Studio через AI-агента (Claude Desktop, Codex и др.): весь рабочий флоу
генерации, настройки, библиотека стилей, диагностика и установка.

## Сборка

```bash
make mcp          # → build/bin/yue-mcp
make mcp-data     # перегенерировать данные библиотеки из фронтендовых источников
```

MCP-сервер — отдельный бинарник: правки в `internal/` или `cmd/yue-mcp` подхватываются только
после `make mcp` и переподключения клиента (в Claude Code — `/mcp`). Чтобы старый бинарник не терял
молча новые поля и инструменты, `make mcp` вшивает в него отпечаток исходников; при первом вызове и
дальше раз в 30 минут MCP сверяет его с репозиторием и, если код уже другой, добавляет к ответу
инструмента «⚠ MCP-сервер устарел… make mcp и переподключите». Без репозитория рядом (бинарник
установлен отдельно) проверка не делается.

## Конфигурация

- `YUE_URL` — адрес воркера (по умолчанию — сохранённые настройки приложения).
- `YUE_MCP_DIR` — куда скачивать артефакты (по умолчанию `~/yue-mcp-downloads`).

### Claude Desktop

`claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "yue-studio": {
      "command": "/путь/к/build/bin/yue-mcp",
      "env": { "YUE_URL": "http://gpu-host:8091" }
    }
  }
}
```

### Claude Code

Локально для клона (адрес воркера в репозиторий не попадает):

```bash
make mcp
claude mcp add yue-studio --scope local -e YUE_URL=http://gpu-host:8091 -- "$PWD/build/bin/yue-mcp"
```

### Codex (~/.codex/config.toml)

```toml
[mcp_servers.yue-studio]
command = "/путь/к/build/bin/yue-mcp"
env = { "YUE_URL" = "http://gpu-host:8091" }
```

## Инструменты

**Рабочий флоу**: `status` · `jobs` (фильтр `folder` — папка песни, `"-"` — без папки; `limit`; `brief` — краткие строки) · `job_update` (своё название и папка трека) · `submit` (стиль/стих/сид/веер n; характер — `temperature` 0.5–1.5 и `cfg` 1–4) · `plan` (ABC до
рендера) · `render_abc` (`parent_id`/`role` — кусок для вклейки под родителем) · `cancel`† · `retry_job` (упавшую или отменённую джобу — снова в очередь с теми же параметрами) · `delete_job`† · `artifacts` (скачать трек/партитуру,
возвращает путь) · `transcribe` (трек → ABC) · `job_score` · `job_preview` (фрагмент) ·
`recognize_lyrics` (трек → текст) · `job_lyrics` (текст из аудио джобы) · `lyrics_adapt` (перевод под пение, слоги сохраняются).

**Слой над моделью** (то, чего нет у YuE; те же механизмы, что в студии):
`continue_job` (заново с места, без склейки) · `set_head` (основная версия) · `variant_track`
(вариант → версия-трек, `voice_src`) · `rebuild_sections` (вклейки куском, громкость дорожек /
заглушить, «перепеть» — замена голоса) · `revoice_start` → `revoice_apply` («перепеть с места»
по частям: дубли от источника голоса, голос только в окне части) · `vocal_contour` (высота
голоса по тактам — спето ли по плану, не выше ли потолка) · `find_tones` (узкий «свист» в
окне, в миксе или дорожке `stem`: частоты для эффекта `dewhistle` — «Убрать свист») ·
`splice` (склейка кусков версий в новую версию: вернуть вырезанный проигрыш, собрать лучшие куски;
резать по границам тактов одного исполнения) · `plan_check` (изменённый план: что изменилось, потолок голоса, правки до отметки; то же
само делают `continue_job`/`revoice_start` с `abc`) · эффект на отдельную дорожку: `dsp_apply` со `stem` (+ `from`/`to`, `db`)
или спека `rebuild_sections` с `chain`/`params` — остальные дорожки не меняются (`soften` — «Смягчить звон» голоса);
голосовые цепочки (`voice=true` в `dsp_chains`: мегафон/телефон/перегруз/слэпбэк) — громкость обработанной дорожки
выравнивается по исходной (RMS), `db` сверху, крутилка `mix` — доля эффекта. Цепочки с `key`
(`ducking`, `key=drums`) — только со `stem`: ключ — стем трека; на весь трек — ошибка. Реверб и
дилей со `stem` в окне `from`–`to` продолжают звучать хвостом после `to`.

**Студия трека**: `make_stems` (demucs) · `make_minus` · `overdub` (стиль + лирика) · `import_track` (сразу с `title`/`folder`) · `transcribe_job` (партитура для трека без неё — импорт без транскрипции или с упавшей) · `voice_convert` (ЭКСПЕРИМЕНТ «голос альбома»: голос трека тембром образца, Seed-VC на воркере; голос дрожит — docs/deployment.md) ·
`dsp_chains` / `dsp_presets` (наборы педалей: `steps` для превью/применения) / `dsp_apply` (`steps` —
цепочка по порядку; без `stem` — вариант `dsp-pedals.flac`) / `dsp_preview` (кусок `from`–`to`, на дорожку `stem`, цепочка по порядку
`steps: [{chain, params, off}]`, `solo` — только обработанная дорожка без микса; результат — вариант `dsp-preview-*.flac` с метриками) / `dsp_variants`
(что делает каждая цепочка — [effects.md](effects.md)) · `volume_envelope` (линия громкости
`points [{t, db}]`, весь трек или дорожка `stem`; в `rebuild_sections` — спека с `envelope`) ·
`analyze_job` (метрики) ·
`job_peaks` (огибающая громкости числами — найти провал/вступление, выбрать место правки) ·
`job_spectrum` (спектрограмма PNG в загрузки; нужен ffmpeg на воркере).

**Профили исполнителей**: `corpus_list` / `corpus_create` / `corpus_add_tracks` /
`corpus_build` / `corpus_get`.

**Библиотека**: `styles` (группы/поиск, готовые строки стиля) · `slot_options`
(рус → англ словарь слотов).

**Звуковой движок** (обработка на воркере, не ffmpeg): `fx_apply` — цепочка блоков
gate/eq/comp/drive/amp (NAM)/cab/reverb/delay на трек или дорожку → вариант `dsp-fx-*.flac`
(звук не сдвигается — годится для вклейки нота в ноту) · `fx_assets` — загруженные захваты NAM и IR ·
`fx_asset_upload` — загрузить свой `.nam`/`.wav` с ПК · `fx_kit_install` — набор сэмплов на воркер (барабаны `osdk` — блок `sampler`, бас `growlybass` — блок `bass`) · `fx_blocks` — описание блоков (умолчания,
границы, подписи) · `fx_presets` — готовые цепочки страницы «Инструменты» · `fx_apply preview=true` —
только прослушать кусок `from`–`to` (не вариант). В трек — `rebuild_sections` с записью
`{child_id: 0, stems, from, to, engine: [блоки]}` (копится со вклейками и эффектами). Подробно — docs/effects.md, «Звуковой движок».

**Синт по аккордам:** `chord_grid` (аккорды и секции плана по тактам звука) → блок движка `synth` с нотами
(секунды трека) → `rebuild_sections` с записью `{child_id: 0, stems: ["mix"], add: true, engine: [synth…, эффекты…]}`
(добавление поверх трека, без разделения); послушать — `fx_apply` `source mix, preview, add`. Готовые синты — `fx_presets`
со `stems: ["synth"]`.

**Перкуссия по сетке:** такты — `chord_grid` (с планом) или `beat_grid` (темп и сильная доля) → блок `perc` с ударами
`notes` [{t, d, vel}] (секунды трека) → `rebuild_sections` записью `add` (как у синта). Готовые — `fx_presets` со
`stems: ["perc"]` (у каждого `pattern` и `swing` — рисунок, который строит студия).

**Сведение и мастер:** место в стерео — `rebuild_sections` с `place {pan, width}`: у записи `add` — место партии,
своей записью `{child_id 0, stems: [дорожка], place}` — место дорожки (на все её правки). Мастер — запись
`{child_id 0, master: true, engine: [glue…, limiter {target_lufs, ceiling_db}]}`: на весь собранный микс на воркере,
ответ — LUFS и истинный пик. Послушать мастер на готовом миксе — `fx_apply` `file` (вариант-микс) `preview`;
записать в тот же файл — `in_place`. Готовые мастера — `fx_presets` со `stems: ["master"]`.

**Пресеты звука** (рецепт обработки трека целиком: правки дорожек + финал на микс → версия «трек · пресет»):
`sound_presets` · `sound_preset_create` / `sound_preset_update` (target_lufs — громкость результата по истинному пику на воркере, master — цепочка движка на микс; у update не переданы — прежние; specs — правки на весь трек: stems + engine |
chain+params | steps, db; final — цепочка на микс) · `sound_preset_delete`† · `sound_preset_apply` (`db` — громкость записей на этот раз {"индекс": дБ}; сейчас, синхронно:
разделит, поставит наборы, пересоберёт, финал на результате; ответ — id версии) · `submit` с `sound_preset_ids`
(до 3; применит открытое приложение, когда трек готов и очередь пуста; у черновика — нельзя).

**Настройки**: `config_get` / `config_set` (адрес воркера, Ollama, `stems_model`, `fx_engine` — движок вкл/выкл).

**Установка и диагностика**: `doctor` (воркер/GPU/ffmpeg/HF-токен) ·
`install_worker` (локально или по ssh на GPU-машину; сухой прогон по умолчанию) ·
`install_app` (сборка wails).

† — деструктивные: требуют `confirm=true`, агент обязан спросить пользователя.

## Пример флоу через агента

1. «Поставь трек в стиле "70s analog reggae, 75 BPM" инструменталом» → `submit`
2. Агент ждёт `jobs` → done
3. «Скачай mp3» → `artifacts` → путь к файлу у пользователя
4. «Сделай минус без вокала» → `make_stems` + `make_minus` → `artifacts`

## Установка: два режима

- **Всё на одном ПК**: `install_worker` без `host` — venv/пакеты/systemd локально.
- **GPU-машина отдельно**: `install_worker` с `host=user@gpu-host` — те же шаги по ssh
  (нужен настроенный ssh-доступ); приложение/MCP на ПК, воркер — на GPU-машине.

## Разработка

- Сервер — `internal/mcp/` (протокол JSON-RPC stdio, без внешних зависимостей),
  точка входа `cmd/yue-mcp`.
- Клиентская логика переиспользует `internal/yue.Service` (моки — в server_test.go).
- Данные библиотеки (`internal/mcp/*.json`) генерируются из фронтенда
  (`tools/mcp-gendata.sh`) и вшиваются через `go:embed`; **руками их не править** —
  правь источники во `frontend/src/` и перегенерируй (`make mcp-data`).
- Новый инструмент воркера = новый метод `yue.Service` + инструмент MCP (паритет,
  см. AGENTS.md).
