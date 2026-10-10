package mcp

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ТК111 (карточка internal-own-track, «Мелкие долги этапов 7–8», условие 75): цель lint-worker
// в Makefile при отсутствии ruff — ошибка, а не «пропущено»: сообщение «ruff не установлен —
// pip install ruff» и код ≠ 0. Написаны по карточке, без чтения реализации.
// Предположение: рецепт lint-worker — строки с табом сразу после «lint-worker:».

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Makefile")); err != nil {
		t.Fatalf("нет Makefile в корне репо %s: %v", root, err)
	}
	return root
}

func lintWorkerRecipe(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	var rec []string
	in := false
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(line, "lint-worker:") {
			in = true
			continue
		}
		if in {
			if !strings.HasPrefix(line, "\t") {
				break
			}
			rec = append(rec, line)
		}
	}
	if len(rec) == 0 {
		t.Fatal("в Makefile нет рецепта цели lint-worker")
	}
	return strings.Join(rec, "\n")
}

// По исходнику: «|| echo …» без exit 1 глушит отсутствие ruff кодом 0.
func TestLintWorkerRecipeNoSilentEcho(t *testing.T) {
	rec := lintWorkerRecipe(t)
	silent := regexp.MustCompile(`\|\|\s*echo\s+'[^']*'\s*\)?\s*$`)
	for _, line := range strings.Split(rec, "\n") {
		if strings.Contains(line, "|| echo") && !strings.Contains(line, "exit 1") {
			t.Errorf("lint-worker: «|| echo» без exit 1 — ложно-зелёный гейт: %q", line)
		} else if silent.MatchString(line) {
			t.Errorf("lint-worker: строка заканчивается echo без ошибки: %q", line)
		}
	}
}

// pathWithoutRuff — PATH из каталогов, где нет ruff, плюс временный каталог со ссылкой на make.
func pathWithoutRuff(t *testing.T, makeBin string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink(makeBin, filepath.Join(dir, "make")); err != nil {
		t.Fatal(err)
	}
	keep := []string{dir}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if d == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(d, "ruff")); err == nil {
			continue
		}
		keep = append(keep, d)
	}
	return strings.Join(keep, string(os.PathListSeparator))
}

func TestLintWorkerWithoutRuffFails(t *testing.T) {
	makeBin, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make недоступен")
	}
	path := pathWithoutRuff(t, makeBin)
	cmd := exec.Command(makeBin, "lint-worker")
	cmd.Dir = repoRoot(t)
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "PATH="+path)
	// сама проверка: в этом PATH ruff не находится
	probe := exec.Command("sh", "-c", "command -v ruff")
	probe.Env = cmd.Env
	if out, err := probe.Output(); err == nil {
		t.Fatalf("ruff всё ещё в PATH: %s", out)
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	runErr := cmd.Run()
	out := buf.String()
	if runErr == nil {
		t.Errorf("make lint-worker без ruff завершился с кодом 0, want ≠ 0:\n%s", out)
	} else if _, ok := runErr.(*exec.ExitError); !ok {
		t.Fatalf("make не запустился: %v\n%s", runErr, out)
	}
	for _, want := range []string{"ruff не установлен", "pip install ruff"} {
		if !strings.Contains(out, want) {
			t.Errorf("в выводе нет %q:\n%s", want, out)
		}
	}
}
