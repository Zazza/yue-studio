# MCP-сервер (yue-mcp)

`yue-mcp` — [Model Context Protocol](https://modelcontextprotocol.io)-сервер (stdio) для
управления Yue Studio через AI-агента (Claude Desktop, Codex и др.): весь рабочий флоу
генерации, настройки, библиотека стилей, диагностика и установка.

## Сборка

```bash
make mcp          # → build/bin/yue-mcp
make mcp-data     # перегенерировать данные библиотеки из фронтендовых источников
```

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

### Codex (~/.codex/config.toml)

```toml
[mcp_servers.yue-studio]
command = "/путь/к/build/bin/yue-mcp"
env = { "YUE_URL" = "http://gpu-host:8091" }
```

## Инструменты

**Рабочий флоу**: `status` · `jobs` · `submit` (стиль/стих/сид/веер n) · `plan` (ABC до
рендера) · `render_abc` · `cancel`† · `delete_job`† · `artifacts` (скачать трек/партитуру,
возвращает путь) · `transcribe` (трек → ABC) · `job_score` · `job_preview` (фрагмент).

**Студия трека**: `make_stems` (demucs) · `make_minus` · `overdub` · `import_track` ·
`dsp_chains` / `dsp_apply` / `dsp_preview` / `dsp_variants` · `analyze_job` (метрики).

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
