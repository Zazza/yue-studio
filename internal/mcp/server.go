// Package mcp — MCP-сервер (Model Context Protocol) для управления Yue Studio:
// рабочий флоу генерации, настройки, диагностика и установка воркера.
// Транспорт — stdio, протокол — JSON-RPC 2.0 (line-delimited).
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"

	"yue-studio/internal/yue"
)

// Tool — регистрируемый MCP-инструмент.
type Tool struct {
	Name        string
	Description string
	// InputSchema — JSON Schema аргументов (map[string]any, сериализуется как есть).
	InputSchema map[string]any
	// Handler выполняет инструмент; args — распарсенные аргументы вызова.
	Handler func(s *Server, args map[string]any) (string, error)
}

// Server — реестр инструментов поверх клиента воркера.
type Server struct {
	client      yue.Service
	downloadDir string // куда складывать скачиваемые артефакты
	tools       map[string]Tool
	toolOrder   []string
	mu          sync.RWMutex
}

func NewServer(client yue.Service, downloadDir string) *Server {
	return &Server{client: client, downloadDir: downloadDir, tools: map[string]Tool{}}
}

func (s *Server) Register(t Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.tools[t.Name]; dup {
		log.Fatalf("mcp: duplicate tool %q", t.Name)
	}
	s.tools[t.Name] = t
	s.toolOrder = append(s.toolOrder, t.Name)
}

// jsonSchema — компактная сборка object-схемы аргументов.
func props(props map[string]any, required ...string) map[string]any {
	// properties — всегда объект: null клиенты (Claude Code) отвергают, и весь
	// список инструментов не загружается
	if props == nil {
		props = map[string]any{}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func prop(desc, typ string, extra ...map[string]any) map[string]any {
	p := map[string]any{"type": typ, "description": desc}
	for _, e := range extra {
		for k, v := range e {
			p[k] = v
		}
	}
	return p
}

// ---------- JSON-RPC / MCP протокол ----------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callToolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

const (
	protocolVersion   = "2024-11-05"
	errInvalidArgs    = -32602
	errMethodNotFound = -32601
	errInternal       = -32603
)

// Serve читает line-delimited JSON-RPC из r и пишет ответы в w, пока не EOF.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20) // артефакты/ABC могут быть большими
	out := bufio.NewWriter(w)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.write(out, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		// notification (без id) — просто игнорируем (notifications/initialized и пр.)
		if len(req.ID) == 0 {
			continue
		}
		resp := s.dispatch(req)
		s.write(out, resp)
	}
	return sc.Err()
}

func (s *Server) write(out *bufio.Writer, resp rpcResponse) {
	b, err := json.Marshal(resp)
	if err != nil {
		return
	}
	if _, err := out.Write(b); err != nil {
		return
	}
	_ = out.WriteByte('\n')
	out.Flush()
}

func (s *Server) dispatch(req rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "yue-studio", "version": "0.1.0"},
		}}
	case "ping":
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		s.mu.RLock()
		defer s.mu.RUnlock()
		tools := make([]map[string]any, 0, len(s.toolOrder))
		for _, name := range s.toolOrder {
			t := s.tools[name]
			tools = append(tools, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": tools}}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID,
				Error: &rpcError{Code: errInvalidArgs, Message: "bad tools/call params"}}
		}
		s.mu.RLock()
		t, ok := s.tools[p.Name]
		s.mu.RUnlock()
		if !ok {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID,
				Error: &rpcError{Code: errMethodNotFound, Message: fmt.Sprintf("unknown tool %q", p.Name)}}
		}
		args := map[string]any{}
		if len(p.Arguments) > 0 {
			if err := json.Unmarshal(p.Arguments, &args); err != nil {
				return rpcResponse{JSONRPC: "2.0", ID: req.ID,
					Error: &rpcError{Code: errInvalidArgs, Message: "arguments must be an object"}}
			}
		}
		text, err := t.Handler(s, args)
		if err != nil {
			return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: callToolResult{
				Content: []toolContent{{Type: "text", Text: err.Error()}}, IsError: true}}
		}
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: callToolResult{
			Content: []toolContent{{Type: "text", Text: text}}}}
	}
	return rpcResponse{JSONRPC: "2.0", ID: req.ID,
		Error: &rpcError{Code: errMethodNotFound, Message: "method not found: " + req.Method}}
}

// ---------- утилиты разбора аргументов ----------

func argString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func argFloat(args map[string]any, key string) float64 {
	switch v := args[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

func argInt(args map[string]any, key string) int64 {
	return int64(argFloat(args, key))
}

func argBool(args map[string]any, key string) bool {
	v, _ := args[key].(bool)
	return v
}

func argStringSlice(args map[string]any, key string) []string {
	raw, _ := args[key].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
