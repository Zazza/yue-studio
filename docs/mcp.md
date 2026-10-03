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

**Рабочий флоу**: `status` · `jobs` (фильтр `folder` — папка песни, `"-"` — без папки; `limit`; `brief` — краткие строки) · `job_update` (своё название и папка трека) · `submit` (стиль/стих/сид/веер n) · `plan` (ABC до
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
выравнивается по исходной (RMS), `db` сверху, крутилка `mix` — доля эффекта.

**Студия трека**: `make_stems` (demucs) · `make_minus` · `overdub` (стиль + лирика) · `import_track` (сразу с `title`/`folder`) · `voice_convert` (ЭКСПЕРИМЕНТ «голос альбома»: голос трека тембром образца, Seed-VC на воркере; голос дрожит — docs/deployment.md) ·
`dsp_chains` / `dsp_apply` / `dsp_preview` / `dsp_variants` · `volume_envelope` (линия громкости
`points [{t, db}]`, весь трек или дорожка `stem`; в `rebuild_sections` — спека с `envelope`) ·
`analyze_job` (метрики) ·
`job_peaks` (огибающая громкости числами — найти провал/вступление, выбрать место правки) ·
`job_spectrum` (спектрограмма PNG в загрузки; нужен ffmpeg на воркере).

**Профили исполнителей**: `corpus_list` / `corpus_create` / `corpus_add_tracks` /
`corpus_build` / `corpus_get`.

**Библиотека**: `styles` (группы/поиск, готовые строки стиля) · `slot_options`
(рус → англ словарь слотов).

**Настройки**: `config_get` / `config_set` (адрес воркера, Ollama).

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
