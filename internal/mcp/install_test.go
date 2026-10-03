package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Тесты установщика написаны по спецификации задачи (не по реализации):
// сухой прогон ничего не выполняет, must-шаг останавливает установку,
// реальная установка требует confirm и явного выбора whisper/seedvc.

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestInstallRunStepsDryRunExecutesNothing(t *testing.T) {
	f := filepath.Join(t.TempDir(), "dry")
	steps := []step{{name: "создать метку", cmd: "touch " + shellQuoteTest(f)}}
	out, err := runSteps("", steps, false)
	if err != nil {
		t.Fatalf("dry run: unexpected error %v", err)
	}
	if fileExists(f) {
		t.Fatalf("dry run executed the step: %s exists", f)
	}
	if !strings.Contains(out, "создать метку") || !strings.Contains(out, "touch "+shellQuoteTest(f)) {
		t.Fatalf("dry run output must list step name and command, got:\n%s", out)
	}
}

func TestInstallRunStepsRunsLocally(t *testing.T) {
	f := filepath.Join(t.TempDir(), "ran")
	_, _ = runSteps("", []step{{name: "метка", cmd: "touch " + shellQuoteTest(f)}}, true)
	if !fileExists(f) {
		t.Fatalf("run=true did not execute the step locally")
	}
}

func TestInstallRunStepsNonMustFailureContinues(t *testing.T) {
	f := filepath.Join(t.TempDir(), "after")
	out, _ := runSteps("", []step{
		{name: "необязательный провал", cmd: "exit 1"},
		{name: "следующий", cmd: "touch " + shellQuoteTest(f)},
	}, true)
	if !fileExists(f) {
		t.Fatalf("failing non-must step stopped later steps; output:\n%s", out)
	}
	if strings.Contains(out, "ОСТАНОВЛЕНО") {
		t.Fatalf("non-must failure must not stop the run; output:\n%s", out)
	}
}

func TestInstallRunStepsMustFailureStops(t *testing.T) {
	f := filepath.Join(t.TempDir(), "never")
	out, err := runSteps("", []step{
		{name: "проверка", cmd: "exit 1", must: true},
		{name: "следующий", cmd: "touch " + shellQuoteTest(f)},
	}, true)
	if err == nil {
		t.Fatalf("failed must-step must return an error")
	}
	if fileExists(f) {
		t.Fatalf("step after failed must-step was executed")
	}
	if !strings.Contains(out, "ОСТАНОВЛЕНО") {
		t.Fatalf("output must contain ОСТАНОВЛЕНО, got:\n%s", out)
	}
}

func TestInstallWorkerDefaultIsDryRun(t *testing.T) {
	s, _ := newTestServer(t)
	start := time.Now()
	out, ok := call(t, s, "install_worker", map[string]any{})
	if !ok {
		t.Fatalf("install_worker without args must succeed as dry run, got error: %s", out)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("dry run took %v — looks like something was executed", d)
	}
	for _, want := range []string{"СУХОЙ ПРОГОН", "воркер", "whisper", "Seed-VC", "ГБ"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "[ошибка") {
		t.Errorf("dry run must not execute steps, but output has error lines:\n%s", out)
	}
}

func TestInstallWorkerRequiresConfirm(t *testing.T) {
	s, _ := newTestServer(t)
	out, ok := call(t, s, "install_worker", map[string]any{"dry_run": false, "whisper": false, "seedvc": false})
	if ok {
		t.Fatalf("dry_run=false without confirm must be an error, got:\n%s", out)
	}
	if !strings.Contains(out, "confirm") {
		t.Errorf("error should mention confirm: %s", out)
	}
}

func TestInstallWorkerRequiresExplicitComponents(t *testing.T) {
	cases := []map[string]any{
		{"dry_run": false, "confirm": true},                  // оба не заданы
		{"dry_run": false, "confirm": true, "whisper": true}, // нет seedvc
		{"dry_run": false, "confirm": true, "seedvc": false}, // нет whisper
	}
	for _, args := range cases {
		s, _ := newTestServer(t)
		out, ok := call(t, s, "install_worker", args)
		if ok {
			t.Fatalf("args %v: must be an error (components not chosen), got:\n%s", args, out)
		}
		if !strings.Contains(out, "whisper") || !strings.Contains(out, "seedvc") {
			t.Errorf("args %v: error must mention whisper and seedvc: %s", args, out)
		}
		if !strings.Contains(strings.ToLower(out), "спрос") {
			t.Errorf("args %v: error must say the user has to be asked: %s", args, out)
		}
	}
}

// findNamed ищет шаг по имени (шаг компонента узнаётся по названию; упоминание
// в чужой команде шагом компонента не является).
func findNamed(steps []step, sub string) (step, bool) {
	for _, st := range steps {
		if strings.Contains(strings.ToLower(st.name), strings.ToLower(sub)) {
			return st, true
		}
	}
	return step{}, false
}

func findStep(steps []step, sub string) (step, bool) {
	for _, st := range steps {
		if strings.Contains(st.name, sub) || strings.Contains(st.cmd, sub) {
			return st, true
		}
	}
	return step{}, false
}

var numRe = regexp.MustCompile(`\d+(?:\.\d+)?`)

func diskNumbers(t *testing.T, steps []step) []float64 {
	t.Helper()
	if len(steps) < 2 {
		t.Fatalf("plan has %d steps, want at least 2", len(steps))
	}
	var nums []float64
	for _, m := range numRe.FindAllString(steps[1].name+" "+steps[1].cmd, -1) {
		v, _ := strconv.ParseFloat(m, 64)
		nums = append(nums, v)
	}
	return nums
}

func TestInstallPlanFirstStepsCheckGPUAndDisk(t *testing.T) {
	_, steps := workerInstallPlan(true, false, false, "~/yue/hf-cache", "~/yue-studio/seedvc")
	if len(steps) < 2 {
		t.Fatalf("plan too short: %d", len(steps))
	}
	if !steps[0].must || !strings.Contains(steps[0].cmd, "nvidia-smi") {
		t.Errorf("first step must be a must-step checking GPU via nvidia-smi: %+v", steps[0])
	}
	if !steps[1].must || !strings.Contains(steps[1].cmd, "df") {
		t.Errorf("second step must be a must-step checking disk space: %+v", steps[1])
	}
}

func TestInstallPlanDiskGrowsWithComponents(t *testing.T) {
	_, base := workerInstallPlan(true, false, false, "~/h", "~/s")
	_, withW := workerInstallPlan(true, true, false, "~/h", "~/s")
	_, withS := workerInstallPlan(true, false, true, "~/h", "~/s")
	nb, nw, ns := diskNumbers(t, base), diskNumbers(t, withW), diskNumbers(t, withS)
	// требование к месту — первое число шага, которое меняется от выбора компонентов
	growth := func(a, b []float64) float64 {
		if len(a) != len(b) {
			t.Fatalf("disk step shape differs: %v vs %v", a, b)
		}
		for i := range a {
			if b[i] != a[i] {
				return b[i] - a[i]
			}
		}
		return 0
	}
	if d := growth(nb, nw); d < 2 || d > 4 {
		t.Errorf("whisper should add ~3 GB to disk requirement, got %+.1f (%v -> %v)", d, nb, nw)
	}
	if d := growth(nb, ns); d < 12 || d > 16 {
		t.Errorf("seedvc should add ~14 GB to disk requirement, got %+.1f (%v -> %v)", d, nb, ns)
	}
}

func TestInstallPlanOptionalSteps(t *testing.T) {
	_, none := workerInstallPlan(false, false, false, "~/h", "/data/seedvc-x")
	if _, ok := findNamed(none, "whisper"); ok {
		t.Errorf("whisper step present with whisper=false")
	}
	if _, ok := findNamed(none, "Seed-VC"); ok {
		t.Errorf("Seed-VC step present with seedvc=false")
	}
	if _, ok := findStep(none, "/data/seedvc-x"); ok {
		t.Errorf("seedvc dir used with seedvc=false")
	}
	if _, ok := findNamed(none, "скопировать файлы воркера"); ok {
		t.Errorf("copy-files step present with local=false")
	}

	_, all := workerInstallPlan(true, true, true, "~/h", "/data/seedvc-x")
	if _, ok := findNamed(all, "whisper"); !ok {
		t.Errorf("whisper step missing with whisper=true")
	}
	st, ok := findNamed(all, "Seed-VC")
	if !ok {
		t.Errorf("Seed-VC step missing with seedvc=true")
	} else if _, ok := findStep(all, "/data/seedvc-x"); !ok {
		t.Errorf("Seed-VC step does not use given dir: %+v", st)
	}
	if _, ok := findNamed(all, "скопировать файлы воркера"); !ok {
		t.Errorf("copy-files step missing with local=true")
	}
}

func TestInstallPlanWorkerEnvUsesHOME(t *testing.T) {
	_, steps := workerInstallPlan(true, false, false, "~/yue/hf-cache", "~/s")
	st, ok := findNamed(steps, "worker.env")
	if !ok {
		t.Fatalf("worker.env step missing")
	}
	// выполняем шаг с подменённым HOME и читаем то, что реально записано
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "yue-studio"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", st.cmd)
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worker.env step failed: %v\n%s\ncmd: %s", err, out, st.cmd)
	}
	data, err := os.ReadFile(filepath.Join(home, "yue-studio", "worker.env"))
	if err != nil {
		t.Fatalf("worker.env not written under $HOME/yue-studio: %v (cmd %s)", err, st.cmd)
	}
	var val string
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "HF_HOME="); ok {
			val = v
		}
	}
	if strings.Contains(val, "~") {
		t.Errorf("written HF_HOME contains literal ~: %q", val)
	}
	if want := filepath.Join(home, "yue", "hf-cache"); val != want {
		t.Errorf("written HF_HOME = %q, want %q (expanded from $HOME)", val, want)
	}
}

func TestInstallShellPath(t *testing.T) {
	for in, want := range map[string]string{
		"~/a/b": "$HOME/a/b",
		"~":     "$HOME",
		"/abs":  "/abs",
		"rel":   "rel",
	} {
		if got := shellPath(in); got != want {
			t.Errorf("shellPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInstallAppDefaultIsDryRun(t *testing.T) {
	s, _ := newTestServer(t)
	start := time.Now()
	out, ok := call(t, s, "install_app", map[string]any{})
	if !ok {
		t.Fatalf("install_app without args must succeed as dry run, got error: %s", out)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("dry run took %v — looks like something was executed", d)
	}
	if !strings.Contains(out, "СУХОЙ ПРОГОН") {
		t.Errorf("output lacks СУХОЙ ПРОГОН:\n%s", out)
	}
	if strings.Contains(out, "[ошибка") {
		t.Errorf("dry run must not execute steps:\n%s", out)
	}
}

// shellQuoteTest — путь в одинарных кавычках для sh (t.TempDir может содержать
// спецсимволы из имени теста).
func shellQuoteTest(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}
