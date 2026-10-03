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
			"HF-токен для весов YuE2, версия Go/Node, необязательные компоненты (whisper, Seed-VC). " +
			"Для GPU-проверок на отдельной машине — host.",
		InputSchema: props(map[string]any{
			"host": prop("user@gpu-host для проверки удалённой машины (пусто = локально)", "string"),
		}),
		Handler: func(s *Server, args map[string]any) (string, error) {
			// воркер и компоненты — по его адресу (отсюда); железо и токен — на машине с GPU
			out, _ := runSteps(s, "", []step{
				{name: "воркер Yue (health)", cmd: fmt.Sprintf("curl -sf --max-time 5 %s/health", s.client.GetURL())},
				{name: "компоненты воркера (whisper, Seed-VC)", cmd: fmt.Sprintf(
					"curl -sf --max-time 5 %s/config | grep -o '\"\\(whisper\\|seedvc\\)_available\": *[a-z]*'", s.client.GetURL())},
				{name: "ffmpeg (нужен для DSP на ПК)", cmd: "ffmpeg -version | head -1"},
				{name: "Go (сборка приложения)", cmd: "go version"},
				{name: "Node (сборка фронта)", cmd: "node --version"},
			}, true)
			gpu, _ := runSteps(nil, argString(args, "host"), []step{
				{name: "GPU/VRAM", cmd: "nvidia-smi --query-gpu=name,memory.total,memory.free --format=csv,noheader"},
				{name: "место на диске (домашний каталог)", cmd: "df -h ~ | tail -1"},
				{name: "HF-токен (веса YuE2, gated)", cmd: "test -f ~/.cache/huggingface/token && echo токен есть || echo нет (нужен huggingface-cli login)"},
			}, true)
			return out + "\n" + gpu, nil
		},
	})

	s.Register(Tool{
		Name: "install_worker",
		Description: "Установить воркер на машине с GPU (Ubuntu + NVIDIA/CUDA). Пустой host — всё на этом ПК " +
			"(частая схема: приложение и GPU на одной машине), host — отдельная машина по ssh. " +
			"Компоненты: воркер (обязательно, ~" + gbStr(sizeWorkerGB) + " ГБ: окружение + веса YuE2 и разбора нот), " +
			"whisper — распознавание текстов треков (~" + gbStr(sizeWhisperGB) + " ГБ), " +
			"Seed-VC — «голос альбома», ЭКСПЕРИМЕНТ (~" + gbStr(sizeSeedvcGB) + " ГБ после установки, больше во время). " +
			"Сначала сухой прогон (по умолчанию) — покажи пользователю план и СПРОСИ, нужны ли whisper и Seed-VC; " +
			"установка — dry_run=false, confirm=true и явные whisper/seedvc (true или false). " +
			"Перед установкой проверяются GPU и место на диске — при нехватке установка не начинается.",
		InputSchema: props(map[string]any{
			"host":       prop("user@gpu-host (пусто = локально на этой машине)", "string"),
			"dry_run":    prop("только показать план, ничего не выполняя (по умолчанию true)", "boolean"),
			"confirm":    prop("выполнить установку (спроси пользователя)", "boolean"),
			"whisper":    prop("ставить whisper — тексты треков, ~"+gbStr(sizeWhisperGB)+" ГБ (спроси пользователя)", "boolean"),
			"seedvc":     prop("ставить Seed-VC — «голос альбома», эксперимент, ~"+gbStr(sizeSeedvcGB)+" ГБ (спроси пользователя)", "boolean"),
			"seedvc_dir": prop("каталог Seed-VC (по умолчанию ~/yue-studio/seedvc; можно на другом диске)", "string"),
			"hf_home":    prop("каталог кеша весов HF (по умолчанию ~/yue/hf-cache)", "string"),
		}),
		Handler: func(s *Server, args map[string]any) (string, error) {
			dryRun := true
			if v, ok := args["dry_run"].(bool); ok {
				dryRun = v
			}
			if !dryRun && !argBool(args, "confirm") {
				return "", errConfirm
			}
			whisper, wOK := args["whisper"].(bool)
			seedvc, sOK := args["seedvc"].(bool)
			if !dryRun && (!wOK || !sOK) {
				return "", fmt.Errorf("перед установкой спроси пользователя, нужны ли необязательные компоненты, " +
					"и передай явно whisper=true|false и seedvc=true|false")
			}
			if dryRun && !wOK {
				whisper = true // в плане показываем и его — пусть пользователь решит
			}
			hfHome := argString(args, "hf_home")
			if hfHome == "" {
				hfHome = "~/yue/hf-cache"
			}
			seedvcDir := argString(args, "seedvc_dir")
			if seedvcDir == "" {
				seedvcDir = "~/yue-studio/seedvc"
			}
			plan, steps := workerInstallPlan(argString(args, "host") == "", whisper, seedvc, hfHome, seedvcDir)
			out, err := runSteps(s, argString(args, "host"), steps, !dryRun)
			if dryRun {
				return "СУХОЙ ПРОГОН — ничего не выполнено, только план (установка: dry_run=false, confirm=true, whisper, seedvc).\n\n" +
					plan + "\n" + out, err
			}
			return plan + "\n" + out, err
		},
	})

	s.Register(Tool{
		Name:        "install_app",
		Description: "Собрать desktop-приложение Yue Studio на этом ПК (wails build; нужны Go, Node, wails CLI). Сухой прогон по умолчанию — только план.",
		InputSchema: props(map[string]any{
			"dry_run": prop("только показать план, ничего не выполняя (по умолчанию true)", "boolean"),
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
				{name: "фронтенд", cmd: "cd frontend && npm ci && npm run build", must: true},
				{name: "wails", cmd: "wails build"},
			}, !dryRun)
			if dryRun {
				out = "СУХОЙ ПРОГОН — ничего не выполнено (сборка: dry_run=false + confirm=true):\n" + out
			}
			return out, err
		},
	})
}

// Размеры компонентов воркера, ГБ (замер на установленной машине, 2026-10-03):
// воркер — окружение 7,4 + веса YuE2-3B 6,8, MERT 2,4, VAE 0,5, SheetSage 0,2,
// demucs 0,1; whisper — окружение 2,7 + модель small 0,5; Seed-VC — 11 после
// установки (на время установки нужно ~14, см. seedvc_install.sh).
const (
	sizeWorkerGB  = 17.5
	sizeWhisperGB = 3.2
	sizeSeedvcGB  = 11
	seedvcPeakGB  = 14
	diskReserveGB = 2 // запас под данные треков на первое время
)

// shellPath — путь для двойных кавычек в shell: «~/…» → «$HOME/…». В worker.env
// (его читает systemd) тильда не раскрывается: HF_HOME=~/… уводил веса в
// каталог с именем «~».
func shellPath(p string) string {
	if p == "~" {
		return "$HOME"
	}
	if strings.HasPrefix(p, "~/") {
		return "$HOME/" + p[2:]
	}
	return p
}

func gbStr(v float64) string {
	return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", v), "0"), ".")
}

// workerInstallPlan — список компонентов с размерами (текст для пользователя)
// и шаги установки: сначала проверки GPU и места (провал — установка не
// начинается), затем воркер и выбранные компоненты.
func workerInstallPlan(local, whisper, seedvc bool, hfHome, seedvcDir string) (string, []step) {
	need := sizeWorkerGB + diskReserveGB
	var b strings.Builder
	b.WriteString("Компоненты:\n")
	fmt.Fprintf(&b, "  [x] воркер — генерация (YuE2), разбор нот, дорожки (demucs): ~%s ГБ (веса качаются при первом запуске)\n", gbStr(sizeWorkerGB))
	mark := func(on bool) string {
		if on {
			return "[x]"
		}
		return "[ ]"
	}
	fmt.Fprintf(&b, "  %s whisper — распознавание текстов треков: ~%s ГБ\n", mark(whisper), gbStr(sizeWhisperGB))
	fmt.Fprintf(&b, "  %s Seed-VC — «голос альбома», ЭКСПЕРИМЕНТ (голос узнаётся, но дрожит): ~%s ГБ после установки, ~%s ГБ во время, каталог %s\n",
		mark(seedvc), gbStr(sizeSeedvcGB), gbStr(seedvcPeakGB), seedvcDir)
	if whisper {
		need += sizeWhisperGB
	}
	if seedvc {
		need += seedvcPeakGB
	}
	fmt.Fprintf(&b, "Нужно места: ~%s ГБ (веса HF — в %s; если он на другом диске, места на домашнем нужно меньше).\n", gbStr(need), hfHome)
	b.WriteString("Без whisper и Seed-VC приложение работает; соответствующие кнопки будут неактивны с подписью «не установлен».\n")

	steps := []step{
		{name: "проверка: видеокарта NVIDIA и драйвер", must: true, cmd: "nvidia-smi --query-gpu=name,memory.total --format=csv,noheader || " +
			"{ echo 'нет NVIDIA GPU или драйвера: воркеру нужна видеокарта с CUDA'; exit 1; }"},
		{name: "проверка: место на диске", must: true, cmd: fmt.Sprintf(
			"free=$(df -Pk ~ | awk 'NR==2 {print int($4/1048576)}'); echo \"свободно $free ГБ, нужно ~%s ГБ\"; "+
				"[ \"$free\" -ge %d ] || { echo 'мало места: освободите диск или поставьте веса/Seed-VC на другой (hf_home, seedvc_dir)'; exit 1; }",
			gbStr(need), int(need))},
	}
	if local {
		steps = append(steps, step{name: "скопировать файлы воркера", must: true, cmd: "test -f worker/yue_worker.py || " +
			"{ echo 'запусти из корня репозитория Yue Studio'; exit 1; }; mkdir -p ~/yue-studio && " +
			"cp worker/*.py worker/requirements.txt worker/requirements-seedvc.txt worker/seedvc_install.sh ~/yue-studio/"})
	}
	steps = append(steps,
		step{name: "каталоги", cmd: "mkdir -p ~/yue-studio/data ~/yue-studio/units"},
		step{name: "venv воркера (python 3.12)", must: true, cmd: "uv venv ~/yue/.venv --python 3.12 || python3 -m venv ~/yue/.venv"},
		step{name: "пакеты (torch cu128 + API)", must: true, cmd: "uv pip install --python ~/yue/.venv/bin/python -r ~/yue-studio/requirements.txt " +
			"--index-url https://download.pytorch.org/whl/cu128 --extra-index-url https://pypi.org/simple || " +
			"~/yue/.venv/bin/pip install -r ~/yue-studio/requirements.txt"},
	)
	if whisper {
		steps = append(steps, step{name: "whisper (тексты треков)", cmd: "uv venv ~/whisper-venv && " +
			"uv pip install --python ~/whisper-venv/bin/python faster-whisper"})
	}
	steps = append(steps, step{name: "worker.env", cmd: fmt.Sprintf(
		"test -f ~/yue-studio/worker.env || printf 'HF_HOME=%%s\\n' \"%s\" > ~/yue-studio/worker.env", shellPath(hfHome))})
	if seedvc {
		steps = append(steps, step{name: "Seed-VC («голос альбома», эксперимент)", cmd: fmt.Sprintf(
			"~/yue-studio/seedvc_install.sh \"%[1]s\" && (grep -q '^YUE_SEEDVC_DIR=' ~/yue-studio/worker.env || "+
				"echo \"YUE_SEEDVC_DIR=%[1]s\" >> ~/yue-studio/worker.env)", shellPath(seedvcDir))})
	}
	steps = append(steps,
		step{name: "systemd-юнит", cmd: "cp ~/yue-studio/units/yue-worker.service ~/.config/systemd/user/ 2>/dev/null && " +
			"systemctl --user daemon-reload && systemctl --user enable --now yue-worker || " +
			"echo юнит пропущен (запуск вручную: ~/yue/.venv/bin/python ~/yue-studio/yue_worker.py)"},
		step{name: "health", cmd: "sleep 3 && curl -sf --max-time 10 http://localhost:8091/health"},
	)
	return b.String(), steps
}

type step struct {
	name string
	cmd  string
	// must — без этого шага дальше нельзя (проверка GPU/места, venv): провал
	// останавливает установку, остальные шаги не выполняются
	must bool
}

// runSteps выполняет команды локально (host="") или через ssh; run=false —
// только печатает шаги (сухой прогон, ничего не выполняется). Шаги независимы,
// кроме must: его провал останавливает выполнение.
func runSteps(s *Server, host string, steps []step, run bool) (string, error) {
	var b strings.Builder
	for i, st := range steps {
		fmt.Fprintf(&b, "— %s\n  $ %s\n", st.name, st.cmd)
		if !run {
			continue
		}
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
			if st.must {
				fmt.Fprintf(&b, "\nОСТАНОВЛЕНО: шаг «%s» обязателен; не выполнено шагов: %d.\n", st.name, len(steps)-i-1)
				break
			}
		}
	}
	if s != nil {
		fmt.Fprintf(&b, "\nадрес воркера сейчас: %s (сменить: config_set)", s.client.GetURL())
	}
	return b.String(), nil
}
