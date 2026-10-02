package mcp

import (
	"fmt"
	"os/exec"
	"strings"
)

// RegisterInstallTools — установка и диагностика. Два режима:
//   - всё на одном ПК: команды выполняются локально;
//   - GPU-машина отдельно: host="user@gpu-host" — те же шаги через ssh.
//
// Установка не выполняет деструктивных действий без явных флагов;
// каждый шаг печатает результат в ответ инструмента.
func RegisterInstallTools(s *Server) {
	s.Register(Tool{
		Name: "doctor",
		Description: "Диагностика окружения: доступность воркера, GPU/VRAM (nvidia-smi), ffmpeg, " +
			"HF-токен для весов YuE2, версия Go/Node. Для GPU-проверок на отдельной машине — host.",
		InputSchema: props(map[string]any{
			"host": prop("user@gpu-host для проверки удалённой машины (пусто = локально)", "string"),
		}),
		Handler: func(s *Server, _ map[string]any) (string, error) {
			out, _ := runSteps(s, "", []step{
				{"воркер Yue (health)", fmt.Sprintf("curl -sf --max-time 5 %s/health", s.client.GetURL())},
				{"GPU/VRAM", "nvidia-smi --query-gpu=name,memory.total,memory.free --format=csv,noheader"},
				{"ffmpeg (нужен для DSP на ПК)", "ffmpeg -version | head -1"},
				{"Go (сборка приложения)", "go version"},
				{"Node (сборка фронта)", "node --version"},
				{"HF-токен (веса YuE2, gated)", "test -f ~/.cache/huggingface/token && echo токен есть || echo нет (нужен huggingface-cli login)"},
				{"Seed-VC («голос альбома», необязательно)", fmt.Sprintf("curl -sf --max-time 5 %s/config | grep -o '\"seedvc_[a-z]*\": *[^,}]*'", s.client.GetURL())},
			})
			return out, nil
		},
	})

	s.Register(Tool{
		Name: "install_worker",
		Description: "Установить воркер на GPU-машине (Ubuntu + CUDA): venv, пакеты из requirements.txt, " +
			"whisper-venv, systemd-юнит. По умолчанию сухой прогон (dry_run=true) — покажи план пользователю; " +
			"выполнение только с confirm=true. Пустой host = всё на этом ПК.",
		InputSchema: props(map[string]any{
			"host":       prop("user@gpu-host (пусто = локально на этой машине)", "string"),
			"dry_run":    prop("показать план без выполнения (по умолчанию true)", "boolean"),
			"confirm":    prop("выполнить установку (спроси пользователя)", "boolean"),
			"hf_home":    prop("каталог кеша весов HF (по умолчанию ~/yue/hf-cache)", "string"),
			"seedvc_dir": prop("поставить и Seed-VC («голос альбома», эксперимент; ~14 ГБ на установку, ~10 ГБ после) в этот каталог (пусто — не ставить)", "string"),
		}),
		Handler: func(s *Server, args map[string]any) (string, error) {
			dryRun := true
			if v, ok := args["dry_run"].(bool); ok {
				dryRun = v
			}
			if !dryRun && !argBool(args, "confirm") {
				return "", errConfirm
			}
			hfHome := argString(args, "hf_home")
			if hfHome == "" {
				hfHome = "~/yue/hf-cache"
			}
			steps := []step{
				{"каталоги", "mkdir -p ~/yue-studio/data ~/yue-studio/units"},
				{"venv воркера (python 3.12)", "uv venv ~/yue/.venv --python 3.12 || python3 -m venv ~/yue/.venv"},
				{"пакеты (torch cu128 + API)", "uv pip install --python ~/yue/.venv/bin/python -r ~/yue-studio/requirements.txt --index-url https://download.pytorch.org/whl/cu128 --extra-index-url https://pypi.org/simple || ~/yue/.venv/bin/pip install -r ~/yue-studio/requirements.txt"},
				{"whisper-venv (тексты треков)", "uv venv ~/whisper-venv && uv pip install --python ~/whisper-venv/bin/python faster-whisper || true"},
				{"worker.env", fmt.Sprintf("test -f ~/yue-studio/worker.env || printf 'HF_HOME=%s\\n' > ~/yue-studio/worker.env", hfHome)},
				{"systemd-юнит", "cp ~/yue-studio/units/yue-worker.service ~/.config/systemd/user/ 2>/dev/null && systemctl --user daemon-reload && systemctl --user enable --now yue-worker || echo юнит пропущен (запуск вручную: ~/yue/.venv/bin/python ~/yue-studio/yue_worker.py)"},
				{"health", "sleep 3 && curl -sf --max-time 10 http://localhost:8091/health"},
			}
			if d := argString(args, "seedvc_dir"); d != "" {
				steps = append(steps, step{"Seed-VC («голос альбома»)", fmt.Sprintf(
					"~/yue-studio/seedvc_install.sh %q && (grep -q '^YUE_SEEDVC_DIR=' ~/yue-studio/worker.env || echo 'YUE_SEEDVC_DIR=%s' >> ~/yue-studio/worker.env)", d, d)})
			}
			if argString(args, "host") == "" {
				steps = append([]step{
					{"скопировать файлы воркера", "cp worker/*.py worker/requirements.txt worker/requirements-seedvc.txt worker/seedvc_install.sh ~/yue-studio/ 2>/dev/null || echo 'запусти из корня репозитория Yue Studio'"},
				}, steps...)
			}
			out, err := runSteps(s, argString(args, "host"), steps)
			if dryRun {
				out = "СУХОЙ ПРОГОН (план установки; выполнение: dry_run=false + confirm=true):\n" + out
			}
			return out, err
		},
	})

	s.Register(Tool{
		Name:        "install_app",
		Description: "Собрать desktop-приложение Yue Studio на этом ПК (wails build; нужны Go, Node, wails CLI).",
		InputSchema: props(map[string]any{
			"dry_run": prop("показать план без выполнения (по умолчанию true)", "boolean"),
			"confirm": prop("выполнить сборку (спроси пользователя)", "boolean"),
		}),
		Handler: func(_ *Server, args map[string]any) (string, error) {
			dryRun := true
			if v, ok := args["dry_run"].(bool); ok {
				dryRun = v
			}
			if !dryRun && !argBool(args, "confirm") {
				return "", errConfirm
			}
			out, err := runSteps(nil, "", []step{
				{"фронтенд", "cd frontend && npm ci && npm run build"},
				{"wails", "wails build"},
			})
			if dryRun {
				out = "СУХОЙ ПРОГОН (выполнение: dry_run=false + confirm=true):\n" + out
			}
			return out, err
		},
	})
}

type step struct {
	name string
	cmd  string
}

// runSteps выполняет команды локально (host="") или через ssh; шаги независимы,
// ошибка шага не прерывает остальные — итог в отчёте.
func runSteps(s *Server, host string, steps []step) (string, error) {
	var b strings.Builder
	for _, st := range steps {
		fmt.Fprintf(&b, "— %s\n  $ %s\n", st.name, st.cmd)
		cmd := exec.Command("bash", "-c", st.cmd)
		if host != "" {
			cmd = exec.Command("ssh", host, "bash", "-s")
			cmd.Stdin = strings.NewReader(st.cmd)
		}
		out, err := cmd.CombinedOutput()
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			for _, line := range strings.Split(trimmed, "\n") {
				fmt.Fprintf(&b, "  %s\n", line)
			}
		}
		if err != nil {
			fmt.Fprintf(&b, "  [ошибка: %v]\n", err)
		}
	}
	if s != nil {
		fmt.Fprintf(&b, "\nадрес воркера сейчас: %s (сменить: config_set)", s.client.GetURL())
	}
	return b.String(), nil
}
