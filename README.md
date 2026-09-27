# Yue Studio

Desktop-приложение для генерации музыки на [YuE2-3B](https://huggingface.co/m-a-p/YuE2-3B):
форма-слоты стиля → песня со стихом или инструментал. Wails v2 (Go + Vue 3) на ПК,
FastAPI-воркер с GPU — отдельно, по сети.

- **Форма-слоты стиля** на русском (жанр, ритм, гитары, голос, настроение…) — компилируются
  в строку тегов YuE, словарь подсказок + автоперевод через Ollama
- **Библиотека стилей**: 27 курируемых групп (100+ откалиброванных стилей) + свои группы
- **Веер best-of-N** (×5 сидов), стадия плана (ABC-партитура) с правкой до рендера,
  каверы из чужих треков (транскрипция SheetSage2)
- **Пиано-ролл** партитуры с превью фрагментов (VAE-decode, секунды), **стемы** (demucs),
  **минус-треки**, **овердаб**, DSP-цепочки эффектов (ffmpeg на ПК) с метриками и дельтами
- **Профили исполнителей** из корпусов треков (тональности/прогрессии/темп/структура + строка стиля)
- Копайтер стихов (Ollama), история, отмена, прослушивание в браузере

> Скриншот: TODO до первого релиза.

## Быстрый старт

Нужно три части: GPU-машина с воркером, опционально Ollama, и desktop-приложение.

1. **Воркер** (Ubuntu + CUDA, NVIDIA ≥16 ГБ VRAM): окружение и запуск —
   [docs/deployment.md](docs/deployment.md). Коротко:
   ```bash
   uv venv ~/yue/.venv --python 3.12
   uv pip install --python ~/yue/.venv/bin/python -r worker/requirements.txt \
       --index-url https://download.pytorch.org/whl/cu128 --extra-index-url https://pypi.org/simple
   ~/yue/.venv/bin/python worker/yue_worker.py   # :8091
   ```
2. **Приложение** (Go 1.22+, Node.js 20+, [Wails CLI v2](https://wails.io)):
   ```bash
   wails build && ./build/bin/yue-studio
   ```
3. Адрес воркера — переменная `YUE_URL` или клик по адресу сервера в шапке приложения.

Ollama (`qwen2.5`) не обязателен: без него работают генерация/стемы/эффекты,
недоступны копайтер/автоперевод/профиль-стиль.

## Документация

- [docs/deployment.md](docs/deployment.md) — развёртывание воркера (включая WSL2/dual-boot), деплой, сборка по ОС
- [docs/architecture.md](docs/architecture.md) — архитектура: стороны ПК/GPU, конвейер YuE2, VRAM
- [docs/api.md](docs/api.md) — HTTP API воркера
- [docs/ui-guide.md](docs/ui-guide.md) — гайд по интерфейсу
- [docs/lyrics-melody.md](docs/lyrics-melody.md) — работа со стихом и мелодией
- [docs/limitations.md](docs/limitations.md) — честные ограничения (правка середины, звук в webview и пр.)

## Структура

| Каталог | Роль |
|---|---|
| `main.go`, `app.go`, `player*.go` | Wails-приложение: биндинги, прокси аудио, встроенный плеер |
| `internal/yue/` | Go-клиент воркера (jobs, план, стемы, корпус) |
| `internal/dsp/` | ffmpeg-цепочки эффектов на ПК |
| `frontend/` | Vue 3 + Vite |
| `worker/` | FastAPI-воркер: резидентная YuE2Pipeline + очередь SQLite |

## Тесты и линтеры

```bash
make test      # всё: Go + фронтенд + воркер
make lint      # go vet + golangci-lint, eslint, ruff
```

Или напрямую: `go test ./...` · `cd frontend && npm test` · `cd worker && python3 -m unittest test_pure`.
CI прогоняет то же самое на каждый push/PR.

## Безопасность

Воркер слушает `0.0.0.0:8091` **без аутентификации**: API позволяет загружать/удалять файлы
и управлять очередью генераций. Это осознанное решение для LAN-инструмента — **не выставляйте
воркер в интернет** напрямую (при необходимости — reverse-proxy с авторизацией или VPN/SSH-туннель).
Аутентификация в планах, пока нет.

## Лицензия

Код — [MIT](LICENSE). Важно: **веса моделей** YuE2 и SheetSage2 распространяются под
CC BY-NC (некоммерческое использование) — генерируемый контент наследует это ограничение,
условия применения проверяйте на страницах моделей.
