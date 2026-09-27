# Yue Studio — основные задачи. make help покажет список.
WAILS ?= $(HOME)/go/bin/wails
DEPLOY_HOST ?=

.PHONY: help test test-go test-front test-worker lint lint-go lint-front lint-worker fmt build worker mcp mcp-data clean

help: ## показать список задач
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[1m%-14s\033[0m %s\n", $$1, $$2}'

## test: все тесты (Go + фронтенд + воркер)
test: test-go test-front test-worker

test-go: ## тесты Go
	go test ./...

test-front: ## тесты фронтенда (vitest)
	cd frontend && npm test

test-worker: ## тесты воркера (unittest)
	cd worker && python3 -m unittest test_pure

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

worker: ## деплой воркера на GPU-машину (YUE_DEPLOY_HOST=user@gpu-host)
	./deploy.sh worker

mcp: ## собрать MCP-сервер (build/bin/yue-mcp)
	go build -o build/bin/yue-mcp ./cmd/yue-mcp
	@echo "mcp-сервер: $(CURDIR)/build/bin/yue-mcp (конфигурация клиентов: docs/mcp.md)"

mcp-data: ## перегенерировать данные библиотеки MCP из фронтенда (go:embed)
	./tools/mcp-gendata.sh

clean: ## почистить артефакты сборки фронта
	cd frontend && rm -rf dist
