package buildinfo

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// writeFile создаёт файл (с каталогами) под root.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newTree — минимальное дерево репозитория: internal/ и cmd/yue-mcp/.
func newTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "internal/mcp/server.go", "package mcp\n")
	writeFile(t, root, "internal/mcp/server_test.go", "package mcp\n// test\n")
	writeFile(t, root, "cmd/yue-mcp/main.go", "package main\n")
	return root
}

func mustHash(t *testing.T, root string) string {
	t.Helper()
	h, err := SourceHash(root)
	if err != nil {
		t.Fatalf("SourceHash: %v", err)
	}
	return h
}

func TestSourceHashFormat(t *testing.T) {
	h := mustHash(t, newTree(t))
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(h) {
		t.Fatalf("hash %q: want 12 hex chars", h)
	}
}

func TestSourceHashDeterministic(t *testing.T) {
	root := newTree(t)
	if a, b := mustHash(t, root), mustHash(t, root); a != b {
		t.Fatalf("same tree, different hashes: %q vs %q", a, b)
	}
	// Одинаковое содержимое в другом каталоге — тот же отпечаток.
	if a, b := mustHash(t, root), mustHash(t, newTree(t)); a != b {
		t.Fatalf("identical trees, different hashes: %q vs %q", a, b)
	}
}

func TestSourceHashChangesOnGoContent(t *testing.T) {
	for _, rel := range []string{"internal/mcp/server.go", "cmd/yue-mcp/main.go"} {
		t.Run(rel, func(t *testing.T) {
			root := newTree(t)
			before := mustHash(t, root)
			writeFile(t, root, rel, "package x\n// changed\n")
			if after := mustHash(t, root); after == before {
				t.Fatalf("hash did not change after editing %s", rel)
			}
		})
	}
}

func TestSourceHashChangesOnGoFileAdded(t *testing.T) {
	for _, rel := range []string{"internal/yue/new.go", "cmd/yue-mcp/extra.go"} {
		t.Run(rel, func(t *testing.T) {
			root := newTree(t)
			before := mustHash(t, root)
			writeFile(t, root, rel, "package x\n")
			if after := mustHash(t, root); after == before {
				t.Fatalf("hash did not change after adding %s", rel)
			}
		})
	}
}

func TestSourceHashIgnoresTestFiles(t *testing.T) {
	root := newTree(t)
	before := mustHash(t, root)
	writeFile(t, root, "internal/mcp/server_test.go", "package mcp\n// edited test\n")
	writeFile(t, root, "internal/mcp/new_test.go", "package mcp\n")
	if after := mustHash(t, root); after != before {
		t.Fatalf("hash changed on _test.go edit: %q -> %q", before, after)
	}
}

func TestSourceHashIgnoresNonGoFiles(t *testing.T) {
	root := newTree(t)
	before := mustHash(t, root)
	writeFile(t, root, "internal/mcp/notes.md", "# notes\n")
	writeFile(t, root, "internal/mcp/data.json", "{}\n")
	if after := mustHash(t, root); after != before {
		t.Fatalf("hash changed on non-.go file: %q -> %q", before, after)
	}
}

func TestSourceHashMissingInternal(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "cmd/yue-mcp/main.go", "package main\n")
	if h, err := SourceHash(root); err == nil {
		t.Fatalf("want error for missing internal/, got hash %q", h)
	}
}
