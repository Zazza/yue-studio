# Yue Studio — основные задачи. make help покажет список.
WAILS ?= $(HOME)/go/bin/wails
DEPLOY_HOST ?=

.PHONY: help test test-go test-front test-worker lint lint-go lint-front lint-worker fmt build install-desktop uninstall-desktop worker mcp mcp-data clean

help: ## показать список задач
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[1m%-14s\033[0m %s\n", $$1, $$2}'

## test: все тесты (Go + фронтенд + воркер)
test: test-go test-front test-worker

test-go: ## тесты Go
	go test ./...

test-front: ## тесты фронтенда (vitest)
	cd frontend && npm test

test-worker: ## тесты воркера (unittest)
	cd worker && python3 -m unittest test_pure test_plancheck test_voice test_loudness test_grid test_vocal_leak test_roformer_stems \
		test_fx_engine test_fx_api

## lint: все линтеры (go vet + golangci-lint, eslint, ruff)
lint: lint-go lint-front lint-worker

lint-go: ## линт Go
	gofmt -l . | grep -q . && (gofmt -l . && echo 'запусти: make fmt' && exit 1) || true
	go vet ./...
	command -v golangci-lint >/dev/null && golangci-lint run || echo 'golangci-lint не установлен — пропущено (CI проверит)'

lint-front: ## линт фронтенда (eslint)
	cd frontend && npx eslint src

lint-worker: ## линт воркера (ruff)
	cd worker && (command -v ruff >/dev/null && ruff check . || echo 'ruff не установлен — пропущено (CI проверит)')

fmt: ## форматирование Go
	gofmt -w .

build: ## сборка desktop-приложения (wails build)
	$(WAILS) build
	@echo "бинарник: $(CURDIR)/build/bin/yue-studio"

# ярлык Linux: имя файла = app_id окна (yue-studio), по нему GNOME/Wayland
# сопоставляет окно с ярлыком и берёт иконку отсюда, а не из окна
DESKTOP_FILE ?= $(HOME)/.local/share/applications/yue-studio.desktop

install-desktop: ## ярлык в меню приложений Linux (на build/bin/yue-studio, иконка build/appicon.png)
	@test -x build/bin/yue-studio || { echo 'нет build/bin/yue-studio — сначала make build'; exit 1; }
	@mkdir -p $(dir $(DESKTOP_FILE))
	@printf '%s\n' '[Desktop Entry]' 'Type=Application' 'Name=Yue Studio' \
		'Comment=Генерация и доводка треков YuE' \
		'Exec="$(CURDIR)/build/bin/yue-studio"' 'Icon=$(CURDIR)/build/appicon.png' \
		'Terminal=false' 'Categories=AudioVideo;Audio;' 'StartupWMClass=yue-studio' > $(DESKTOP_FILE)
	@command -v update-desktop-database >/dev/null && update-desktop-database -q $(dir $(DESKTOP_FILE)) || true
	@echo "ярлык: $(DESKTOP_FILE)"

uninstall-desktop: ## убрать ярлык из меню приложений
	rm -f $(DESKTOP_FILE)

worker: ## деплой воркера на GPU-машину (YUE_DEPLOY_HOST=user@gpu-host)
	./deploy.sh worker

mcp: ## собрать MCP-сервер (build/bin/yue-mcp)
	go build -ldflags "-X main.buildHash=$$(go run ./cmd/srchash) -X main.srcDir=$(CURDIR)" -o build/bin/yue-mcp ./cmd/yue-mcp
	@echo "mcp-сервер: $(CURDIR)/build/bin/yue-mcp (конфигурация клиентов: docs/mcp.md)"

mcp-data: ## перегенерировать данные библиотеки MCP из фронтенда (go:embed)
	./tools/mcp-gendata.sh

clean: ## почистить артефакты сборки фронта
	cd frontend && rm -rf dist
