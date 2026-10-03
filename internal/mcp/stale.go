package mcp

import (
	"sync"
	"time"

	"yue-studio/internal/buildinfo"
)

// staleInterval — как часто MCP сверяет свой отпечаток с исходниками.
const staleInterval = 30 * time.Minute

// staleWarning — приписка к ответу инструмента, когда MCP собран раньше, чем
// менялись его исходники: новые поля и инструменты он молча потеряет.
const staleWarning = "⚠ MCP-сервер устарел: его исходники изменились после сборки — новые поля и инструменты " +
	"он не знает. Пересоберите (make mcp) и переподключите MCP (в Claude Code — /mcp).\n\n"

// staleChecker — сверка вшитого при сборке отпечатка исходников (buildHash) с
// текущими исходниками в srcDir: при первом вызове и не чаще staleInterval.
// Нет отпечатка или каталога (MCP установлен без репозитория) — не проверяем.
type staleChecker struct {
	buildHash, srcDir string
	hash              func(root string) (string, error)
	now               func() time.Time

	mu      sync.Mutex
	checked time.Time
	stale   bool
}

// SetBuildInfo — отпечаток исходников, вшитый при сборке, и каталог репозитория.
func (s *Server) SetBuildInfo(buildHash, srcDir string) {
	s.stale = &staleChecker{buildHash: buildHash, srcDir: srcDir, hash: buildinfo.SourceHash, now: time.Now}
}

// warning — приписка «MCP устарел» или пусто.
func (c *staleChecker) warning() string {
	if c == nil || c.buildHash == "" || c.srcDir == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if now := c.now(); c.checked.IsZero() || now.Sub(c.checked) >= staleInterval {
		c.checked = now
		h, err := c.hash(c.srcDir)
		c.stale = err == nil && h != c.buildHash // нет исходников — не шумим
	}
	if c.stale {
		return staleWarning
	}
	return ""
}
