package mcp

import (
	"encoding/json"
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

// Пути установщика: «~» и «~/…» раскрываются в $HOME (systemd тильду не
// раскрывает — в worker.env нужен абсолютный путь), остальное — как задано.
// Проверяется выполнением шагов в sh, а не внутренней функцией.
func TestInstallPathsTildeExpandsInWorkerEnv(t *testing.T) {
	for in, rel := range map[string]string{
		"~/a/b": "a/b",
		"~":     "",
		"/abs":  "",
		"rel":   "",
	} {
		t.Run(in, func(t *testing.T) {
			home := t.TempDir()
			_, steps := workerInstallPlanSep(false, false, false, false, in, "~/s", "")
			env, _ := runInstallSteps(t, home, steps)
			want := in
			switch {
			case in == "~":
				want = home
			case rel != "":
				want = filepath.Join(home, rel)
			}
			if got := envValues(env, "HF_HOME"); len(got) != 1 || got[0] != want {
				t.Errorf("HF_HOME for %q = %q, want [%s]\n%s", in, got, want, env)
			}
		})
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

// --- internal-roformer-stems, тест-кейс 15: RoFormer ставится только по выбору ---

func TestInstallPlanRoformerOnlyWhenChosen(t *testing.T) {
	_, off := workerInstallPlanRF(true, true, true, false, "~/h", "~/s")
	if st, ok := findNamed(off, "roformer"); ok {
		t.Errorf("RoFormer step present with roformer=false: %+v", st)
	}
	_, on := workerInstallPlanRF(true, false, false, true, "~/h", "~/s")
	if _, ok := findNamed(on, "roformer"); !ok {
		names := make([]string, 0, len(on))
		for _, st := range on {
			names = append(names, st.name)
		}
		t.Errorf("RoFormer step missing with roformer=true; steps: %q", names)
	}
}

func TestInstallPlanRoformerDisk(t *testing.T) {
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
	for _, c := range []struct{ whisper, seedvc bool }{{false, false}, {true, true}} {
		_, old := workerInstallPlan(true, c.whisper, c.seedvc, "~/h", "~/s")
		_, off := workerInstallPlanRF(true, c.whisper, c.seedvc, false, "~/h", "~/s")
		_, on := workerInstallPlanRF(true, c.whisper, c.seedvc, true, "~/h", "~/s")
		no, nf, nn := diskNumbers(t, old), diskNumbers(t, off), diskNumbers(t, on)
		if d := growth(no, nf); d != 0 {
			t.Errorf("whisper=%v seedvc=%v: roformer=false must not change disk requirement, got %+.1f (%v -> %v)",
				c.whisper, c.seedvc, d, no, nf)
		}
		if d := growth(nf, nn); d < 6 || d > 8 {
			t.Errorf("whisper=%v seedvc=%v: roformer should add ~7 GB to disk requirement, got %+.1f (%v -> %v)",
				c.whisper, c.seedvc, d, nf, nn)
		}
	}
}

func TestInstallWorkerRequiresExplicitRoformer(t *testing.T) {
	s, _ := newTestServer(t)
	out, ok := call(t, s, "install_worker", map[string]any{
		"dry_run": false, "confirm": true, "whisper": false, "seedvc": false,
	})
	if ok {
		t.Fatalf("install without explicit roformer must be an error, got:\n%s", out)
	}
	if !strings.Contains(out, "roformer") {
		t.Errorf("error must mention roformer: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "спрос") {
		t.Errorf("error must say the user has to be asked: %s", out)
	}
}

func TestInstallWorkerDryRunWithoutRoformerOK(t *testing.T) {
	s, _ := newTestServer(t)
	out, ok := call(t, s, "install_worker", map[string]any{"whisper": false, "seedvc": false})
	if !ok {
		t.Fatalf("dry run without roformer must succeed, got error: %s", out)
	}
	if !strings.Contains(out, "СУХОЙ ПРОГОН") {
		t.Errorf("expected dry run output:\n%s", out)
	}
}

// --- internal-roformer-stems, тест-кейс 16: каталог RoFormer sep_dir ---

// sepEnvSteps — шаги, которые пишут пути RoFormer в worker.env.
func sepEnvSteps(steps []step) []step {
	var out []step
	for _, st := range steps {
		if strings.Contains(st.cmd, "YUE_SEP_PY") || strings.Contains(st.cmd, "YUE_SEP_MODELS") {
			out = append(out, st)
		}
	}
	return out
}

// runEnvStep выполняет шаг с HOME во временном каталоге и возвращает worker.env.
func runEnvStep(t *testing.T, home string, st step) string {
	t.Helper()
	cmd := exec.Command("sh", "-c", st.cmd)
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worker.env step failed: %v\n%s\ncmd: %s", err, out, st.cmd)
	}
	data, err := os.ReadFile(filepath.Join(home, "yue-studio", "worker.env"))
	if err != nil {
		t.Fatalf("worker.env not written under $HOME/yue-studio: %v (cmd %s)", err, st.cmd)
	}
	return string(data)
}

func TestInstallSepDirVenvInDir(t *testing.T) {
	_, steps := workerInstallPlanSep(true, false, false, true, "~/h", "~/s", "/data/sep")
	st, ok := findNamed(steps, "roformer")
	if !ok {
		t.Fatalf("RoFormer step missing with roformer=true")
	}
	if !strings.Contains(st.cmd, "/data/sep/venv") {
		t.Errorf("RoFormer step must install env into /data/sep/venv: %s", st.cmd)
	}
	if strings.Contains(st.cmd, "sep-venv") {
		t.Errorf("RoFormer step with sep_dir must not use ~/sep-venv: %s", st.cmd)
	}
}

func TestInstallSepDirWritesWorkerEnv(t *testing.T) {
	_, steps := workerInstallPlanSep(true, false, false, true, "~/h", "~/s", "/data/sep")
	env := sepEnvSteps(steps)
	if len(env) == 0 {
		t.Fatalf("no step writes YUE_SEP_PY/YUE_SEP_MODELS to worker.env with sep_dir")
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "yue-studio"), 0o755); err != nil {
		t.Fatal(err)
	}
	var data string
	for _, st := range env {
		data = runEnvStep(t, home, st)
	}
	if got := envValues(data, "YUE_SEP_PY"); len(got) != 1 || got[0] != "/data/sep/venv/bin/python" {
		t.Errorf("YUE_SEP_PY in worker.env = %q, want [/data/sep/venv/bin/python]\n%s", got, data)
	}
	if got := envValues(data, "YUE_SEP_MODELS"); len(got) != 1 || got[0] != "/data/sep/models" {
		t.Errorf("YUE_SEP_MODELS in worker.env = %q, want [/data/sep/models]\n%s", got, data)
	}
	// повторная установка не дублирует строки
	for _, st := range env {
		data = runEnvStep(t, home, st)
	}
	if n := len(envValues(data, "YUE_SEP_PY")); n != 1 {
		t.Errorf("after second run YUE_SEP_PY appears %d times:\n%s", n, data)
	}
}

func TestInstallSepDirKeepsExistingWorkerEnv(t *testing.T) {
	_, steps := workerInstallPlanSep(true, false, false, true, "~/h", "~/s", "/data/sep")
	env := sepEnvSteps(steps)
	if len(env) == 0 {
		t.Fatalf("no step writes YUE_SEP_PY/YUE_SEP_MODELS to worker.env with sep_dir")
	}
	home := t.TempDir()
	dir := filepath.Join(home, "yue-studio")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pre := "HF_HOME=/x/hf\nYUE_SEP_PY=/opt/my/python\nYUE_SEP_MODELS=/opt/my/models\n"
	if err := os.WriteFile(filepath.Join(dir, "worker.env"), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	var data string
	for _, st := range env {
		data = runEnvStep(t, home, st)
	}
	if got := envValues(data, "YUE_SEP_PY"); len(got) != 1 || got[0] != "/opt/my/python" {
		t.Errorf("already set YUE_SEP_PY overwritten/duplicated: %q\n%s", got, data)
	}
	if got := envValues(data, "YUE_SEP_MODELS"); len(got) != 1 || got[0] != "/opt/my/models" {
		t.Errorf("already set YUE_SEP_MODELS overwritten/duplicated: %q\n%s", got, data)
	}
	if got := envValues(data, "HF_HOME"); len(got) != 1 || got[0] != "/x/hf" {
		t.Errorf("other worker.env lines lost: %q\n%s", got, data)
	}
}

func TestInstallSepDirHomeDiskAsWithoutRoformer(t *testing.T) {
	for _, c := range []struct{ whisper, seedvc bool }{{false, false}, {true, true}} {
		_, off := workerInstallPlanSep(true, c.whisper, c.seedvc, false, "~/h", "~/s", "")
		_, on := workerInstallPlanSep(true, c.whisper, c.seedvc, true, "~/h", "~/s", "/data/sep")
		nf, nn := diskNumbers(t, off), diskNumbers(t, on)
		if len(nf) != len(nn) {
			t.Fatalf("disk step shape differs: %v vs %v", nf, nn)
		}
		for i := range nf {
			if nf[i] != nn[i] {
				t.Errorf("whisper=%v seedvc=%v: sep_dir set, home disk requirement must equal no-RoFormer: %v vs %v",
					c.whisper, c.seedvc, nf, nn)
				break
			}
		}
	}
}

func TestInstallNoSepDirDefaults(t *testing.T) {
	_, steps := workerInstallPlanSep(true, false, false, true, "~/h", "~/s", "")
	st, ok := findNamed(steps, "roformer")
	if !ok {
		t.Fatalf("RoFormer step missing with roformer=true")
	}
	if !strings.Contains(st.cmd, "sep-venv") {
		t.Errorf("without sep_dir RoFormer env must be ~/sep-venv: %s", st.cmd)
	}
	if env := sepEnvSteps(steps); len(env) != 0 {
		t.Errorf("without sep_dir there must be no worker.env step for RoFormer, got %d: %+v", len(env), env)
	}
}

// --- internal-roformer-stems, тест-кейсы 17 и 18: пути установщика в shell ---
//
// Пути (sep_dir, hf_home, seedvc_dir) уходят в shell-команды одним словом:
// спецсимволы передаются буквально и не выполняются, «~/» раскрывается в
// $HOME; отклоняются только перевод строки и нулевой байт.

// trapShells подменяет PATH каталогом, где bash/sh/ssh только записывают факт
// вызова в журнал и выходят с 0: так видно, запускалась ли хоть одна команда,
// и настоящая установка на машине теста не происходит ни при каком исходе.
func trapShells(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\necho \"$0 $*\" >> " + shellQuoteTest(log) + "\nexit 0\n"
	for _, name := range []string{"bash", "sh", "ssh"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return log
}

// runInstallSteps выполняет шаги плана в bash с HOME во временном каталоге.
// Внешние программы (uv, systemctl, curl, nvidia-smi) и
// ~/yue-studio/seedvc_install.sh — заглушки: пишут свои аргументы по одному
// на строку в журнал и выходят с 0. Возвращает worker.env и журнал заглушек.
func runInstallSteps(t *testing.T, home string, steps []step) (env, calls string) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	stubs := t.TempDir()
	log := filepath.Join(stubs, "stubs.log")
	stub := "#!/bin/sh\n{ echo \"== $(basename \"$0\")\"; for a in \"$@\"; do printf '%s\\n' \"$a\"; done; } >> " +
		shellQuoteTest(log) + "\nexit 0\n"
	for _, name := range []string{"uv", "systemctl", "curl", "nvidia-smi", "sleep"} {
		if err := os.WriteFile(filepath.Join(stubs, name), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ys := filepath.Join(home, "yue-studio")
	if err := os.MkdirAll(ys, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ys, "seedvc_install.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, st := range steps {
		if i < 2 { // проверки видеокарты и места — не про пути
			continue
		}
		cmd := exec.Command(bash, "-c", st.cmd)
		cmd.Dir = home
		cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+stubs+":"+os.Getenv("PATH"))
		// сбой шага не роняет тест: проверяются результаты (worker.env, аргументы
		// заглушек, маркер). Шаги путей при сбое дадут неверный worker.env.
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("step %q failed: %v\n%s", st.name, err, out)
		}
	}
	data, err := os.ReadFile(filepath.Join(ys, "worker.env"))
	if err != nil {
		t.Fatalf("worker.env not written: %v", err)
	}
	c, _ := os.ReadFile(log)
	return string(data), string(c)
}

func envValues(data, key string) []string {
	var vals []string
	for _, line := range strings.Split(data, "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			vals = append(vals, v)
		}
	}
	return vals
}

// hasArg — заглушка получила аргумент ровно таким (одной строкой журнала).
func hasArg(calls, arg string) bool {
	for _, line := range strings.Split(calls, "\n") {
		if line == arg {
			return true
		}
	}
	return false
}

// trickyPaths — пути со спецсимволами; mark — файл, который появится, если
// подстановка выполнится. Каталог mark не должен содержать спецсимволов.
func trickyPaths(mark string) []string {
	return []string{
		"/data/sep$(touch " + mark + ")",
		"/data/sep`touch " + mark + "`",
		"/data/a;touch " + mark + ";b",
		"/data/it's",
		`/data/say "hi"`,
		"/data/my sep",
		"/data/Дорожки",
		"~/my sep$(touch " + mark + ")",
	}
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if r, ok := strings.CutPrefix(p, "~/"); ok {
		return home + "/" + r
	}
	return p
}

func TestInstallSepDirSpecialCharsLiteral(t *testing.T) {
	for i, dir := range trickyPaths("MARK") {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			home := t.TempDir()
			mark := filepath.Join(t.TempDir(), "executed")
			dir := strings.ReplaceAll(dir, "MARK", mark)
			_, steps := workerInstallPlanSep(false, false, false, true, "~/h", "~/s", dir)
			env, calls := runInstallSteps(t, home, steps)
			if fileExists(mark) {
				t.Fatalf("substitution in sep_dir %q was executed", dir)
			}
			base := expandHome(dir, home)
			if got := envValues(env, "YUE_SEP_PY"); len(got) != 1 || got[0] != base+"/venv/bin/python" {
				t.Errorf("YUE_SEP_PY = %q, want [%s]\n%s", got, base+"/venv/bin/python", env)
			}
			if got := envValues(env, "YUE_SEP_MODELS"); len(got) != 1 || got[0] != base+"/models" {
				t.Errorf("YUE_SEP_MODELS = %q, want [%s]\n%s", got, base+"/models", env)
			}
			if !hasArg(calls, base+"/venv") {
				t.Errorf("uv did not get venv path %q literally; stub calls:\n%s", base+"/venv", calls)
			}
		})
	}
}

func TestInstallHFHomeSpecialCharsLiteral(t *testing.T) {
	for i, dir := range trickyPaths("MARK") {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			home := t.TempDir()
			mark := filepath.Join(t.TempDir(), "executed")
			dir := strings.ReplaceAll(dir, "MARK", mark)
			_, steps := workerInstallPlanSep(false, false, false, false, dir, "~/s", "")
			env, _ := runInstallSteps(t, home, steps)
			if fileExists(mark) {
				t.Fatalf("substitution in hf_home %q was executed", dir)
			}
			if got := envValues(env, "HF_HOME"); len(got) != 1 || got[0] != expandHome(dir, home) {
				t.Errorf("HF_HOME = %q, want [%s]\n%s", got, expandHome(dir, home), env)
			}
		})
	}
}

func TestInstallSeedVCDirSpecialCharsLiteral(t *testing.T) {
	for i, dir := range trickyPaths("MARK") {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			home := t.TempDir()
			mark := filepath.Join(t.TempDir(), "executed")
			dir := strings.ReplaceAll(dir, "MARK", mark)
			_, steps := workerInstallPlanSep(false, false, true, false, "~/h", dir, "")
			env, calls := runInstallSteps(t, home, steps)
			if fileExists(mark) {
				t.Fatalf("substitution in seedvc_dir %q was executed", dir)
			}
			want := expandHome(dir, home)
			if !strings.Contains(calls, "== seedvc_install.sh") || !hasArg(calls, want) {
				t.Errorf("seedvc_install.sh did not get %q literally; stub calls:\n%s", want, calls)
			}
			if got := envValues(env, "YUE_SEEDVC_DIR"); len(got) != 1 || got[0] != want {
				t.Errorf("YUE_SEEDVC_DIR = %q, want [%s]\n%s", got, want, env)
			}
		})
	}
}

func TestInstallDefaultPathsExpandHome(t *testing.T) {
	home := t.TempDir()
	_, steps := workerInstallPlanSep(false, false, true, false, "~/yue/hf-cache", "~/yue-studio/seedvc", "")
	env, calls := runInstallSteps(t, home, steps)
	if got := envValues(env, "HF_HOME"); len(got) != 1 || got[0] != home+"/yue/hf-cache" {
		t.Errorf("HF_HOME = %q, want [%s]", got, home+"/yue/hf-cache")
	}
	if got := envValues(env, "YUE_SEEDVC_DIR"); len(got) != 1 || got[0] != home+"/yue-studio/seedvc" {
		t.Errorf("YUE_SEEDVC_DIR = %q, want [%s]", got, home+"/yue-studio/seedvc")
	}
	if !hasArg(calls, home+"/yue-studio/seedvc") {
		t.Errorf("seedvc_install.sh did not get expanded default dir; calls:\n%s", calls)
	}
}

// Обработчик: спецсимволы не отклоняются (ни сухой прогон, ни установка),
// перевод строки и нулевой байт — ошибка с именем параметра до любых команд.
func TestInstallWorkerAcceptsSpecialCharPaths(t *testing.T) {
	for _, param := range []string{"sep_dir", "hf_home", "seedvc_dir"} {
		for i, dir := range trickyPaths("/nonexistent-dir/mark") {
			for _, dry := range []bool{true, false} {
				t.Run(param+"/"+strconv.Itoa(i)+map[bool]string{true: "/dry", false: "/run"}[dry], func(t *testing.T) {
					trapShells(t) // установка «выполняется» заглушками, на машине ничего не ставится
					s, _ := newTestServer(t)
					args := map[string]any{"whisper": false, "seedvc": true, "roformer": true, param: dir}
					if !dry {
						args["dry_run"] = false
						args["confirm"] = true
						args["stems_model"] = "htdemucs" // при roformer=true установка требует выбора (условие 11)
					}
					out, ok := call(t, s, "install_worker", args)
					if !ok {
						t.Errorf("%s %q (dry=%v) must be accepted, got error: %s", param, dir, dry, out)
					}
				})
			}
		}
	}
}

func TestInstallWorkerRejectsNewlineAndNUL(t *testing.T) {
	for _, param := range []string{"sep_dir", "hf_home", "seedvc_dir"} {
		for _, dir := range []string{"/data/a\nb", "/data/a\x00b", "/data/a\n"} {
			for _, dry := range []bool{true, false} {
				t.Run(param+map[bool]string{true: "/dry", false: "/run"}[dry], func(t *testing.T) {
					log := trapShells(t)
					s, _ := newTestServer(t)
					args := map[string]any{"whisper": false, "seedvc": true, "roformer": true, param: dir}
					if !dry {
						args["dry_run"] = false
						args["confirm"] = true
						// stems_model задан: ошибка должна быть именно из-за пути, а не из-за выбора модели
						args["stems_model"] = "roformer"
					}
					out, ok := call(t, s, "install_worker", args)
					if ok {
						t.Errorf("%s %q (dry=%v) must be an error, got:\n%s", param, dir, dry, out)
					}
					if !strings.Contains(out, param) {
						t.Errorf("error must mention %s: %s", param, out)
					}
					if data, err := os.ReadFile(log); err == nil && len(data) > 0 {
						t.Errorf("commands were executed with %s %q:\n%s", param, dir, data)
					}
				})
			}
		}
	}
}

// --- internal-roformer-stems, тест-кейс 19: шаги установки корректны для bash ---

func TestInstallAllStepsBashSyntax(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	for _, local := range []bool{false, true} {
		for mask := 0; mask < 8; mask++ {
			for _, sepDir := range []string{"", "/data/sep", "~/my sep$(id)"} {
				w, sv, rf := mask&1 != 0, mask&2 != 0, mask&4 != 0
				_, steps := workerInstallPlanSep(local, w, sv, rf, "~/yue/hf-cache", "~/yue-studio/seedvc", sepDir)
				for _, st := range steps {
					cmd := exec.Command(bash, "-n", "-c", st.cmd)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Errorf("step %q (local=%v whisper=%v seedvc=%v roformer=%v sep_dir=%q) is not valid bash: %v\n%s\ncmd: %s",
							st.name, local, w, sv, rf, sepDir, err, out, st.cmd)
					}
				}
			}
		}
	}
}

// Без systemd (systemctl падает) и без юнита шаг «systemd-юнит» не падает,
// а печатает подсказку о ручном запуске.
func TestInstallSystemdStepFallbackHint(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	_, steps := workerInstallPlanSep(false, false, false, false, "~/h", "~/s", "")
	st, ok := findNamed(steps, "systemd")
	if !ok {
		t.Fatalf("systemd step missing")
	}
	for _, withUnit := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-unit", true: "unit-no-systemd"}[withUnit], func(t *testing.T) {
			home := t.TempDir()
			if withUnit {
				units := filepath.Join(home, "yue-studio", "units")
				if err := os.MkdirAll(units, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(units, "yue-worker.service"), []byte("[Unit]\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(home, ".config", "systemd", "user"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			// systemctl-заглушка: «systemd недоступен»; настоящий systemctl не вызывается
			stubs := t.TempDir()
			script := "#!/bin/sh\necho 'Failed to connect to bus' >&2\nexit 1\n"
			if err := os.WriteFile(filepath.Join(stubs, "systemctl"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bash, "-c", st.cmd)
			cmd.Dir = home
			cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+stubs+":"+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("systemd step must not fail without systemd: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "вручную") || !strings.Contains(string(out), "yue_worker.py") {
				t.Errorf("fallback must print manual start hint, got:\n%s", out)
			}
		})
	}
}

// --- internal-instruments-page, решение кросс-ревью: локальная установка
// копирует worker/fx_blocks.json — без него воркер не загрузит звуковой движок.

func TestInstallPlanLocalCopiesFxBlocks(t *testing.T) {
	_, steps := workerInstallPlan(true, false, false, "~/h", "~/s")
	st, ok := findNamed(steps, "скопировать файлы воркера")
	if !ok {
		t.Fatalf("copy-files step missing with local=true")
	}
	if !strings.Contains(st.cmd, "fx_blocks.json") && !strings.Contains(st.cmd, "*.json") {
		t.Errorf("copy-files step does not copy fx_blocks.json: %s", st.cmd)
	}
}

// --- internal-studio-engine, условие 11: модель дорожек по умолчанию при установке ---
//
// stems_model (htdemucs | roformer): при roformer=true и установке обязателен —
// агент спрашивает пользователя; roformer без roformer=true — ошибка; без
// RoFormer и без выбора — настройка не меняется (переустановка не сбрасывает
// выбранный ранее RoFormer). Выбор пишется в ~/yue-studio/data/settings.json
// слиянием до запуска юнита; план показывает выбор; при roformer после
// установки — проверка действующей модели (/config stems_model = bs-roformer-sw)
// с понятным предупреждением.

// installRunArgs — аргументы настоящей установки (не сухой прогон).
func installRunArgs(extra map[string]any) map[string]any {
	args := map[string]any{"dry_run": false, "confirm": true, "whisper": false, "seedvc": false}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

// assertNothingRan — ни одна команда установки не запускалась (журнал trapShells пуст).
func assertNothingRan(t *testing.T, log string) {
	t.Helper()
	if data, err := os.ReadFile(log); err == nil && len(data) > 0 {
		t.Errorf("commands were executed:\n%s", data)
	}
}

func TestInstallStemsModelRequiredWithRoformer(t *testing.T) {
	log := trapShells(t)
	s, _ := newTestServer(t)
	out, ok := call(t, s, "install_worker", installRunArgs(map[string]any{"roformer": true}))
	if ok {
		t.Fatalf("install with roformer=true and no stems_model must be an error, got:\n%s", out)
	}
	if !strings.Contains(out, "stems_model") {
		t.Errorf("error must mention stems_model: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "спрос") {
		t.Errorf("error must say the user has to be asked: %s", out)
	}
	assertNothingRan(t, log)
}

func TestInstallStemsModelDryRunWithoutChoiceOK(t *testing.T) {
	log := trapShells(t)
	s, _ := newTestServer(t)
	out, ok := call(t, s, "install_worker", map[string]any{"whisper": false, "seedvc": false, "roformer": true})
	if !ok {
		t.Fatalf("dry run with roformer=true and no stems_model must succeed, got error: %s", out)
	}
	if !strings.Contains(out, "СУХОЙ ПРОГОН") {
		t.Errorf("expected dry run output:\n%s", out)
	}
	assertNothingRan(t, log)
}

func TestInstallStemsModelRoformerRequiresRoformerInstall(t *testing.T) {
	for _, dry := range []bool{true, false} {
		t.Run(map[bool]string{true: "dry", false: "run"}[dry], func(t *testing.T) {
			log := trapShells(t)
			s, _ := newTestServer(t)
			args := map[string]any{"whisper": false, "seedvc": false, "roformer": false, "stems_model": "roformer"}
			if !dry {
				args = installRunArgs(args)
			}
			out, ok := call(t, s, "install_worker", args)
			if ok {
				t.Fatalf("stems_model=roformer with roformer=false must be an error, got:\n%s", out)
			}
			if !strings.Contains(out, "roformer") {
				t.Errorf("error must mention roformer: %s", out)
			}
			assertNothingRan(t, log)
		})
	}
}

func TestInstallStemsModelUnknownValue(t *testing.T) {
	for _, val := range []string{"mdx", "Demucs", " htdemucs"} {
		for _, dry := range []bool{true, false} {
			t.Run(val+map[bool]string{true: "/dry", false: "/run"}[dry], func(t *testing.T) {
				log := trapShells(t)
				s, _ := newTestServer(t)
				args := map[string]any{"whisper": false, "seedvc": false, "roformer": true, "stems_model": val}
				if !dry {
					args = installRunArgs(args)
				}
				out, ok := call(t, s, "install_worker", args)
				if ok {
					t.Fatalf("unknown stems_model %q must be an error, got:\n%s", val, out)
				}
				if !strings.Contains(out, "stems_model") {
					t.Errorf("error must mention stems_model: %s", out)
				}
				assertNothingRan(t, log)
			})
		}
	}
}

// Без RoFormer и без stems_model шага записи stems_model в settings.json нет:
// переустановка (например, ради whisper) не сбрасывает выбранный ранее RoFormer.
func TestInstallStemsModelUnchangedWithoutChoice(t *testing.T) {
	for _, c := range []struct{ whisper, seedvc bool }{{false, false}, {true, false}, {false, true}} {
		_, steps := workerInstallPlanFull(false, c.whisper, c.seedvc, false, "~/h", "~/s", "", "")
		for _, st := range steps {
			if strings.Contains(st.cmd, "settings.json") && strings.Contains(st.cmd, "stems_model") {
				t.Errorf("whisper=%v seedvc=%v, no RoFormer, no stems_model: unexpected settings step %q: %s",
					c.whisper, c.seedvc, st.name, st.cmd)
			}
		}
	}
}

// Сухой прогон через MCP без RoFormer и без выбора — успешен, проверки
// RoFormer после установки в плане нет.
func TestInstallStemsModelDryRunWithoutRoformerNoChoice(t *testing.T) {
	s, _ := newTestServer(t)
	out, ok := call(t, s, "install_worker", map[string]any{"whisper": false, "seedvc": false, "roformer": false})
	if !ok {
		t.Fatalf("dry run without roformer must succeed, got error: %s", out)
	}
	if strings.Contains(out, "bs-roformer-sw") {
		t.Errorf("plan without RoFormer must not check bs-roformer-sw:\n%s", out)
	}
}

// Полный прогон шагов без RoFormer и без выбора: ранее выбранный RoFormer
// в settings.json сохраняется, прочие ключи тоже.
func TestInstallStemsModelReinstallKeepsRoformerSetting(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}
	home := t.TempDir()
	fakeWorkerVenv(t, home)
	dir := filepath.Join(home, "yue-studio", "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{"stems_model": "roformer", "llm_model": "qwen"}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	_, steps := workerInstallPlanFull(false, true, false, false, "~/h", "~/s", "", "")
	runInstallSteps(t, home, steps)
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json disappeared after reinstall: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v\n%s", err, data)
	}
	if m["stems_model"] != "roformer" || m["llm_model"] != "qwen" {
		t.Errorf("reinstall without stems_model changed settings: %v", m)
	}
}

// Явный stems_model=htdemucs без RoFormer — шаг записи есть, план называет модель.
func TestInstallStemsModelExplicitHtdemucsWithoutRoformer(t *testing.T) {
	plan, steps := workerInstallPlanFull(false, false, false, false, "~/h", "~/s", "", "htdemucs")
	_, st := settingsStep(t, steps)
	if !strings.Contains(st.cmd, "htdemucs") {
		t.Errorf("settings step does not contain htdemucs: %s", st.cmd)
	}
	if !strings.Contains(strings.ToLower(plan), "demucs") {
		t.Errorf("plan for explicit htdemucs does not name it:\n%s", plan)
	}
	s, _ := newTestServer(t)
	out, ok := call(t, s, "install_worker", map[string]any{"whisper": false, "seedvc": false, "roformer": false, "stems_model": "htdemucs"})
	if !ok {
		t.Fatalf("dry run with explicit htdemucs without roformer must succeed, got error: %s", out)
	}
}

// Установка без RoFormer (без stems_model) не требует выбора модели.
func TestInstallStemsModelNotRequiredWithoutRoformer(t *testing.T) {
	trapShells(t)
	s, _ := newTestServer(t)
	out, ok := call(t, s, "install_worker", installRunArgs(map[string]any{"roformer": false}))
	if !ok && strings.Contains(out, "stems_model") {
		t.Fatalf("install without RoFormer must not require stems_model, got error: %s", out)
	}
}

func TestInstallStemsModelPlanNamesChoice(t *testing.T) {
	planH, _ := workerInstallPlanFull(false, false, false, true, "~/h", "~/s", "", "htdemucs")
	planR, _ := workerInstallPlanFull(false, false, false, true, "~/h", "~/s", "", "roformer")
	// в плане модель может называться по-человечески («Demucs»)
	if !strings.Contains(strings.ToLower(planH), "demucs") {
		t.Errorf("plan for stems_model=htdemucs does not name it:\n%s", planH)
	}
	if !strings.Contains(strings.ToLower(planR), "roformer") {
		t.Errorf("plan for stems_model=roformer does not name it:\n%s", planR)
	}
	if planH == planR {
		t.Errorf("plan text does not depend on stems_model choice:\n%s", planH)
	}
}

// settingsStep — шаг, который пишет stems_model в settings.json воркера.
func settingsStep(t *testing.T, steps []step) (int, step) {
	t.Helper()
	for i, st := range steps {
		if strings.Contains(st.cmd, "settings.json") && strings.Contains(st.cmd, "stems_model") {
			return i, st
		}
	}
	t.Fatalf("no step writes stems_model to settings.json")
	return -1, step{}
}

// fakeWorkerVenv — окружение воркера (~/yue/.venv, его создаёт uv раньше в
// установке) подменяется системным python3: в тесте uv не запускается.
func fakeWorkerVenv(t *testing.T, home string) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found")
	}
	bin := filepath.Join(home, "yue", ".venv", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"python", "python3"} {
		if err := os.Symlink(py, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
}

// runSettingsStep выполняет шаг с HOME во временном каталоге и возвращает
// разобранный ~/yue-studio/data/settings.json.
func runSettingsStep(t *testing.T, home string, st step) map[string]any {
	t.Helper()
	fakeWorkerVenv(t, home)
	cmd := exec.Command("bash", "-c", st.cmd)
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("settings step failed: %v\n%s\ncmd: %s", err, out, st.cmd)
	}
	data, err := os.ReadFile(filepath.Join(home, "yue-studio", "data", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not written under $HOME/yue-studio/data: %v (cmd %s)", err, st.cmd)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v\n%s", err, data)
	}
	return m
}

func TestInstallStemsModelWritesSettingsMerged(t *testing.T) {
	for _, model := range []string{"htdemucs", "roformer"} {
		t.Run(model, func(t *testing.T) {
			_, steps := workerInstallPlanFull(false, false, false, true, "~/h", "~/s", "", model)
			_, st := settingsStep(t, steps)
			if !strings.Contains(st.cmd, model) {
				t.Errorf("settings step does not contain value %q: %s", model, st.cmd)
			}
			home := t.TempDir()
			dir := filepath.Join(home, "yue-studio", "data")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			other := "htdemucs"
			if model == "htdemucs" {
				other = "roformer"
			}
			pre := `{"stems_model": "` + other + `", "llm_model": "qwen", "name": "Дорожки", "nested": {"a": 1}}`
			if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(pre), 0o644); err != nil {
				t.Fatal(err)
			}
			m := runSettingsStep(t, home, st)
			if m["stems_model"] != model {
				t.Errorf("stems_model = %v, want %q", m["stems_model"], model)
			}
			if m["llm_model"] != "qwen" || m["name"] != "Дорожки" {
				t.Errorf("other settings keys lost or changed: %v", m)
			}
			if n, ok := m["nested"].(map[string]any); !ok || n["a"] != float64(1) {
				t.Errorf("nested settings key lost: %v", m)
			}
		})
	}
}

// settings.json ещё нет — шаг создаёт его. Каталог data/ на чистой машине
// создают предыдущие шаги — это проверяет TestInstallStemsModelFullRunWritesSettings.
func TestInstallStemsModelWritesSettingsFresh(t *testing.T) {
	_, steps := workerInstallPlanFull(false, false, false, true, "~/h", "~/s", "", "roformer")
	_, st := settingsStep(t, steps)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "yue-studio", "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := runSettingsStep(t, home, st)
	if m["stems_model"] != "roformer" {
		t.Errorf("stems_model = %v, want roformer", m["stems_model"])
	}
}

// Полный прогон шагов на чистом HOME (внешние программы — заглушки):
// settings.json появляется с выбранной моделью.
func TestInstallStemsModelFullRunWritesSettings(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}
	home := t.TempDir()
	fakeWorkerVenv(t, home)
	_, steps := workerInstallPlanFull(false, false, false, false, "~/h", "~/s", "", "htdemucs")
	runInstallSteps(t, home, steps)
	data, err := os.ReadFile(filepath.Join(home, "yue-studio", "data", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not written by install steps: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v\n%s", err, data)
	}
	if m["stems_model"] != "htdemucs" {
		t.Errorf("stems_model = %v, want htdemucs", m["stems_model"])
	}
}

func TestInstallStemsModelSettingsBeforeSystemd(t *testing.T) {
	for _, c := range []struct {
		roformer bool
		model    string
	}{{false, "htdemucs"}, {true, "htdemucs"}, {true, "roformer"}} {
		_, steps := workerInstallPlanFull(false, false, false, c.roformer, "~/h", "~/s", "", c.model)
		si, _ := settingsStep(t, steps)
		ui := -1
		for i, st := range steps {
			if strings.Contains(strings.ToLower(st.name), "systemd") {
				ui = i
				break
			}
		}
		if ui < 0 {
			t.Fatalf("systemd step missing (roformer=%v model=%s)", c.roformer, c.model)
		}
		if si > ui {
			t.Errorf("roformer=%v model=%s: settings step (#%d) must come before systemd unit (#%d)",
				c.roformer, c.model, si, ui)
		}
	}
}

// runCheckStep выполняет шаг с curl-заглушкой, отдающей config, и
// sleep-заглушкой; возвращает вывод и ошибку.
func runCheckStep(t *testing.T, st step, config string) (string, error) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	stubs := t.TempDir()
	cfg := filepath.Join(stubs, "config.json")
	if err := os.WriteFile(cfg, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	curl := "#!/bin/sh\ncat " + shellQuoteTest(cfg) + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(stubs, "curl"), []byte(curl), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stubs, "sleep"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	cmd := exec.Command(bash, "-c", st.cmd)
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+stubs+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Проверка после установки — по действующей модели воркера (/config
// stems_model = bs-roformer-sw), а не по roformer_available: RoFormer может
// быть установлен, но воркер делать Demucs.
func TestInstallStemsModelRoformerFinalCheck(t *testing.T) {
	_, steps := workerInstallPlanFull(false, false, false, true, "~/h", "~/s", "", "roformer")
	last := steps[len(steps)-1]
	if last.must {
		t.Errorf("roformer check step must not be a must-step (warning, not stop)")
	}
	t.Run("not-active", func(t *testing.T) {
		for _, cfg := range []string{
			`{"roformer_available": true, "stems_model": "htdemucs"}`,
			`{"roformer_available":true,"stems_model":"htdemucs"}`,
			`{"roformer_available": false, "stems_model": "htdemucs"}`,
			`{"roformer_available": true}`,
			``,
		} {
			out, err := runCheckStep(t, last, cfg)
			if err == nil {
				t.Errorf("config %q: check must fail when active model is not bs-roformer-sw; output:\n%s", cfg, out)
			}
			if strings.TrimSpace(out) == "" || !strings.Contains(strings.ToLower(out), "roformer") {
				t.Errorf("config %q: check must print a warning mentioning RoFormer, got:\n%s", cfg, out)
			}
		}
	})
	t.Run("active", func(t *testing.T) {
		for _, cfg := range []string{
			`{"roformer_available": true, "stems_model": "bs-roformer-sw"}`,
			`{"roformer_available":true,"stems_model":"bs-roformer-sw"}`,
		} {
			out, err := runCheckStep(t, last, cfg)
			if err != nil {
				t.Errorf("config %q: check must pass when active model is bs-roformer-sw: %v\n%s", cfg, err, out)
			}
		}
	})
}

func TestInstallStemsModelNoRoformerCheckForHtdemucs(t *testing.T) {
	for _, c := range []struct {
		roformer bool
		model    string
	}{{false, "htdemucs"}, {true, "htdemucs"}, {false, ""}} {
		_, steps := workerInstallPlanFull(false, false, false, c.roformer, "~/h", "~/s", "", c.model)
		for _, st := range steps {
			if strings.Contains(st.cmd, "bs-roformer-sw") || strings.Contains(st.cmd, "roformer_available") {
				t.Errorf("roformer=%v stems_model=%q: unexpected RoFormer check %q", c.roformer, c.model, st.name)
			}
		}
	}
}
