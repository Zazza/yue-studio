// Package buildinfo — отпечаток исходников MCP-сервера: при сборке вшивается в
// бинарник (make mcp), при работе пересчитывается по репозиторию — так MCP
// узнаёт, что его исходники изменились, а сам он собран раньше и молча
// потеряет новые поля и инструменты.
package buildinfo

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sourceDirs — что входит в отпечаток: код MCP и всё, что он вызывает.
var sourceDirs = []string{"internal", filepath.Join("cmd", "yue-mcp")}

// SourceHash — отпечаток Go-исходников (без тестов) под root: пути и
// содержимое по порядку, sha256, первые 12 hex-знаков.
func SourceHash(root string) (string, error) {
	var files []string
	for _, d := range sourceDirs {
		base := filepath.Join(root, d)
		err := filepath.WalkDir(base, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !e.IsDir() && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Strings(files)
	h := sha256.New()
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return "", err
		}
		h.Write([]byte(filepath.ToSlash(rel)))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}
