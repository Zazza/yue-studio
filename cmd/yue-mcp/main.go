// yue-mcp — MCP-сервер (stdio) для управления Yue Studio через AI-агентов:
// генерация музыки, настройки, библиотека стилей, установка воркера.
//
// Адрес воркера: переменная YUE_URL или сохранённые настройки приложения.
// Артефакты скачиваются в YUE_MCP_DIR (по умолчанию ~/yue-mcp-downloads).
package main

import (
	"log"
	"os"
	"path/filepath"

	"yue-studio/internal/config"
	"yue-studio/internal/mcp"
	"yue-studio/internal/yue"
)

// Вшиваются при сборке (make mcp): отпечаток исходников и каталог репозитория —
// по ним MCP замечает, что собран раньше, чем менялся код.
var (
	buildHash string
	srcDir    string
)

func main() {
	log.SetFlags(0)

	cfg := config.Load()
	if u := os.Getenv("YUE_URL"); u != "" {
		cfg.YueURL = u
	}
	dir := os.Getenv("YUE_MCP_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("home dir: %v", err)
		}
		dir = filepath.Join(home, "yue-mcp-downloads")
	}

	srv := mcp.NewServer(yue.New(cfg.YueURL), dir)
	srv.SetBuildInfo(buildHash, srcDir)
	mcp.RegisterWorkflowTools(srv)
	mcp.RegisterStudioTools(srv)
	mcp.RegisterLibraryTools(srv)
	mcp.RegisterInstallTools(srv)

	if err := srv.Serve(os.Stdin, os.Stdout); err != nil {
		log.Fatalf("mcp serve: %v", err)
	}
}
