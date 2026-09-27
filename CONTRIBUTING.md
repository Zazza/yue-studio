# Контрибьюторам

Спасибо за интерес к Yue Studio!

## Окружение

- Go 1.22+, Node.js 20+, [Wails CLI v2](https://wails.io) — desktop-приложение
- Python 3.12 — воркер (требования: `worker/requirements.txt`)
- ffmpeg — DSP-цепочки эффектов и тесты `internal/dsp`

Полная инструкция по развёртыванию — [docs/deployment.md](docs/deployment.md).

## Перед отправкой PR

Всё должно быть зелёным — локально то же, что гоняет CI:

```bash
make lint     # go vet + golangci-lint, eslint (frontend), ruff (worker)
make test     # go test ./..., vitest, unittest воркера
```

Коммиты — в стиле [Conventional Commits](https://www.conventionalcommits.org/ru/)
(`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`) — на русском или английском,
главное действие + область.

## Правила проекта

- Поведение меняем осознанно: если меняется пользовательское поведение — это должно
  быть видно в commit message и, при заметности, в [CHANGELOG.md](CHANGELOG.md).
- Тесты пишем по спецификации «как должно работать», а не фиксацией текущего поведения.
- UI-тексты и комментарии — на русском (язык проекта), идентификаторы кода — на английском.
- Новые env-переменные воркера — добавить в `worker.env.example` и таблицу в
  [docs/deployment.md](docs/deployment.md).

## Релизы

- Тег `vX.Y.Z` + GitHub Release; бинарники Linux/Windows — в аттач релиза.
- **Обязательно:** перед тегом добавить запись в [CHANGELOG.md](CHANGELOG.md) —
  раздел «Unreleased» переименовывается в версию с датой.
