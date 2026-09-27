package main

import (
	"embed"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"

	"yue-studio/internal/config"
	"yue-studio/internal/yue"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	cfg := config.Load()
	client := yue.New(cfg.YueURL)
	app := NewApp(client, NewPlayer())

	if err := wails.Run(&options.App{
		Title:     "Yue Studio",
		Width:     1200,
		Height:    800,
		Frameless: false,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: &audioProxy{client: client},
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
		Linux: &linux.Options{
			WebviewGpuPolicy: linux.WebviewGpuPolicyAlways,
		},
	}); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

// audioProxy отдаёт артефакты джоб через встроенный ассет-сервер,
// чтобы webview грузил их с того же origin (без прямых http-ссылок на воркер).
type audioProxy struct {
	client *yue.Client
}

func (h *audioProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var id int64
	if _, err := fmt.Sscanf(r.URL.Path, "/audio/%d/", &id); err != nil {
		http.NotFound(w, r)
		return
	}
	file := strings.Trim(strings.TrimPrefix(r.URL.Path, fmt.Sprintf("/audio/%d/", id)), "/")
	if file == "" {
		http.NotFound(w, r)
		return
	}
	resp, err := h.client.FetchAudioReq(r.Context(), id, file, r.Header.Get("Range"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	w.Header().Set("Accept-Ranges", "bytes")
	for _, hk := range []string{"Content-Type", "Content-Range", "Content-Length"} {
		if v := resp.Header.Get(hk); v != "" {
			w.Header().Set(hk, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body) // клиент ушёл — ничего не сделать
}
