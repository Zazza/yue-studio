// Package yue — HTTP-клиент воркера Yue Studio: джобы, план, стемы,
// корпус, DSP-варианты, стриминг артефактов.
package yue

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	requestTimeout = 30 * time.Second
	// Заголовки тяжёлых запросов (corpus/track: DSP+SheetSage2+Whisper,
	// стриминг аудио) могут идти минутами — транспорт ждёт дольше,
	// короткие GET всё равно ограничены контекстом requestTimeout.
	headerTimeout = 15 * time.Minute
	// План грузит модель при холодном старте и генерирует ABC на GPU — минуты.
	planTimeout = 10 * time.Minute
	// Копайтер: Ollama грузит модель с диска + генерация текста.
	copilotTimeout = 6 * time.Minute
	// errBodyLimit — сколько байт тела ошибки включать в сообщение.
	errBodyLimit = 4 << 10
)

// Client — HTTP-клиент воркера. baseURL можно менять на лету (SetURL).
type Client struct {
	mu      sync.RWMutex
	baseURL string
	hc      *http.Client
}

var _ Service = (*Client)(nil)

func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		hc: &http.Client{
			// Без глобального таймаута: долгие вызовы (plan, стриминг аудио)
			// получают дедлайн per-request через контекст.
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				MaxIdleConns:          10,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       120 * time.Second,
				ResponseHeaderTimeout: headerTimeout,
				Proxy:                 nil,
			},
		},
	}
}

func (c *Client) SetURL(baseURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = baseURL
}

func (c *Client) GetURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// call — общий путь HTTP-запроса к API воркера: таймаут, опциональное тело
// (JSON или сырые байты с X-Filename), проверка статуса, декод JSON в out.
func (c *Client) call(ctx context.Context, method, path string, timeout time.Duration, req *request, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var body io.Reader
	if req != nil && req.raw != nil {
		body = strings.NewReader(string(req.raw))
	} else if req != nil && req.json != nil {
		b, err := json.Marshal(req.json)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(b))
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, c.GetURL()+path, body)
	if err != nil {
		return err
	}
	if req != nil {
		if req.json != nil {
			httpReq.Header.Set("Content-Type", "application/json")
		}
		if req.filename != "" {
			httpReq.Header.Set("X-Filename", req.filename)
		}
	}
	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
		return fmt.Errorf("yue %s: %s: %s", path, resp.Status, string(b))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// request — опциональное тело для call: либо JSON-значение, либо сырые байты.
type request struct {
	json     any
	raw      []byte
	filename string // X-Filename для сырых байтов (заливка аудио)
}

func jsonReq(v any) *request { return &request{json: v} }
func rawReq(name string, b []byte) *request {
	return &request{raw: b, filename: name}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.call(ctx, http.MethodGet, path, requestTimeout, nil, out)
}

func (c *Client) postJSON(ctx context.Context, path string, body any, timeout time.Duration, out any) error {
	return c.call(ctx, http.MethodPost, path, timeout, jsonReq(body), out)
}

func (c *Client) post(ctx context.Context, path string, timeout time.Duration, out any) error {
	return c.call(ctx, http.MethodPost, path, timeout, nil, out)
}

func (c *Client) postRaw(ctx context.Context, path, filename string, data []byte, timeout time.Duration, out any) error {
	return c.call(ctx, http.MethodPost, path, timeout, rawReq(filename, data), out)
}

func (c *Client) del(ctx context.Context, path string, out any) error {
	return c.call(ctx, http.MethodDelete, path, requestTimeout, nil, out)
}

// validFile — защита от path traversal в именах артефактов.
func validFile(file string) bool {
	if file == "" || strings.Contains(file, "/") || strings.Contains(file, "..") {
		return false
	}
	switch filepath.Ext(file) {
	case ".flac", ".mp3", ".wav", ".abc", ".json", ".npy":
		return true
	}
	return false
}

func contentTypeByExt(file string) string {
	switch filepath.Ext(file) {
	case ".flac":
		return "audio/flac"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".abc":
		return "text/plain; charset=utf-8"
	case ".json":
		return "application/json"
	}
	return "application/octet-stream"
}

// AudioURL — прямой URL артефакта джобы (для /listen и скачивания).
func (c *Client) AudioURL(id int64, file string) string {
	return fmt.Sprintf("%s/audio/%d/%s", c.GetURL(), id, url.PathEscape(file))
}
