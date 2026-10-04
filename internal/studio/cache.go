package studio

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"yue-studio/internal/yue"
)

// Cache — скачанные с воркера звук трека и стемы на ПК: превью эффекта на
// кусок не качает трек и дорожки заново на каждое нажатие (пересборка качала
// всё целиком — это секунды на каждый перебор). Живёт с приложением; после
// разделения на стемы — Invalidate (файлы на воркере стали другими).
type Cache struct {
	dir   string
	mu    sync.Mutex
	files map[string]string // "<job>/<имя>" → путь
}

// NewCache — кэш в каталоге dir (создаётся при первом скачивании).
func NewCache(dir string) *Cache {
	return &Cache{dir: dir, files: map[string]string{}}
}

func cacheKey(jobID int64, name string) string { return fmt.Sprintf("%d/%s", jobID, name) }

// Fetch — путь к артефакту джобы на ПК: первый раз качает с воркера, дальше
// отдаёт тот же файл. Файл пропал с диска — качает снова.
func (c *Cache) Fetch(ctx context.Context, svc yue.Service, jobID int64, name string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := cacheKey(jobID, name)
	if p, ok := c.files[key]; ok {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		delete(c.files, key)
	}
	dir := filepath.Join(c.dir, fmt.Sprint(jobID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p, err := FetchTemp(ctx, svc, jobID, name, dir, "*-"+name)
	if err != nil {
		return "", err
	}
	c.files[key] = p
	return p, nil
}

// Base — звук трека через кэш: как FetchBase, первое имя из baseAudioFiles,
// которое есть у джобы.
func (c *Cache) Base(ctx context.Context, svc yue.Service, jobID int64) (string, error) {
	var last error
	for _, name := range baseAudioFiles {
		p, err := c.Fetch(ctx, svc, jobID, name)
		if err == nil {
			return p, nil
		}
		if !isNotFound(err) {
			return "", err
		}
		last = err
	}
	return "", last
}

// Invalidate — забыть файлы джобы: следующий Fetch качает заново.
func (c *Cache) Invalidate(jobID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := fmt.Sprintf("%d/", jobID)
	for k, p := range c.files {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			_ = os.Remove(p) // файл кэша; не удалился — перезапишется при следующем скачивании
			delete(c.files, k)
		}
	}
}

func isNotFound(err error) bool {
	var se *yue.StatusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}
