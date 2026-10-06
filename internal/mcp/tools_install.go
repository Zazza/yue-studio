package mcp

import (
	"fmt"
	"math"
	"os/exec"
	"strconv"
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
			out, _ := runSteps("", []step{
				{name: "воркер Yue (health)", cmd: fmt.Sprintf("curl -sf --max-time 5 %s/health", s.client.GetURL())},
				{name: "компоненты воркера (whisper, Seed-VC, модель разделения)", cmd: fmt.Sprintf(
					"curl -sf --max-time 5 %s/config | grep -o '\"\\(whisper_available\\|seedvc_available\\|stems_model\\)\": *\"*[a-z-]*'", s.client.GetURL())},
				{name: "ffmpeg (нужен для DSP на ПК)", cmd: "ffmpeg -version | head -1"},
				{name: "Go (сборка приложения)", cmd: "go version"},
				{name: "Node (сборка фронта)", cmd: "node --version"},
			}, true)
			gpu, _ := runSteps(argString(args, "host"), []step{
				{name: "GPU/VRAM", cmd: "nvidia-smi --query-gpu=name,memory.total,memory.free --format=csv,noheader"},
				{name: "место на диске (домашний каталог)", cmd: "df -h ~ | tail -1"},
				{name: "HF-токен (веса YuE2, gated)", cmd: "test -f ~/.cache/huggingface/token && echo токен есть || echo 'нет (нужен huggingface-cli login)'"},
			}, true)
			return out + "\n" + gpu + workerAddress(s), nil
		},
	})

	s.Register(Tool{
		Name: "install_worker",
		Description: "Установить воркер на машине с GPU (Ubuntu + NVIDIA/CUDA). Пустой host — всё на этом ПК " +
			"(частая схема: приложение и GPU на одной машине), host — отдельная машина по ssh. " +
			"Компоненты: воркер (обязательно, ~" + gbStr(sizeWorkerGB) + " ГБ: окружение + веса YuE2 и разбора нот), " +
			"whisper — распознавание текстов треков (~" + gbStr(sizeWhisperGB) + " ГБ), " +
			"Seed-VC — «голос альбома», ЭКСПЕРИМЕНТ (~" + gbStr(sizeSeedvcGB) + " ГБ после установки, больше во время). " +
			"RoFormer — более чистое разделение дорожек (~" + gbStr(sizeSepGB) + " ГБ, веса некоммерческие). " +
			"Сначала сухой прогон (по умолчанию) — покажи пользователю план и СПРОСИ, нужны ли whisper, Seed-VC и RoFormer; " +
			"установка — dry_run=false, confirm=true и явные whisper/seedvc/roformer (true или false). " +
			"Перед установкой проверяются GPU и место на диске — при нехватке установка не начинается.",
		InputSchema: props(map[string]any{
			"host":       prop("user@gpu-host (пусто = локально на этой машине)", "string"),
			"dry_run":    prop("только показать план, ничего не выполняя (по умолчанию true)", "boolean"),
			"confirm":    prop("выполнить установку (спроси пользователя)", "boolean"),
			"whisper":    prop("ставить whisper — тексты треков, ~"+gbStr(sizeWhisperGB)+" ГБ (спроси пользователя)", "boolean"),
			"seedvc":     prop("ставить Seed-VC — «голос альбома», эксперимент, ~"+gbStr(sizeSeedvcGB)+" ГБ (спроси пользователя)", "boolean"),
			"roformer":   prop("ставить RoFormer — заметно более чистые дорожки (~4,5× дольше demucs), ~"+gbStr(sizeSepGB)+" ГБ, веса некоммерческие (спроси пользователя)", "boolean"),
			"sep_dir":    prop("каталог RoFormer: окружение <dir>/venv и веса <dir>/models, можно на другом диске (пусто — ~/sep-venv и ~/sep-models)", "string"),
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
			roformer, rOK := args["roformer"].(bool)
			if !dryRun && (!wOK || !sOK || !rOK) {
				return "", fmt.Errorf("перед установкой спроси пользователя, нужны ли необязательные компоненты, " +
					"и передай явно whisper=true|false, seedvc=true|false и roformer=true|false")
			}
			hfHome := argString(args, "hf_home")
			if hfHome == "" {
				hfHome = "~/yue/hf-cache"
			}
			seedvcDir := argString(args, "seedvc_dir")
			if seedvcDir == "" {
				seedvcDir = "~/yue-studio/seedvc"
			}
			sepDir := argString(args, "sep_dir")
			// пути уходят в shell-команды экранированными (shellQuote) и в worker.env строкой:
			// перевод строки и нулевой байт туда не записать — такие значения отклоняются
			for name, v := range map[string]string{"hf_home": hfHome, "seedvc_dir": seedvcDir, "sep_dir": sepDir} {
				if strings.ContainsAny(v, "\n\r\x00") {
					return "", fmt.Errorf("%s: путь не может содержать перевод строки или нулевой байт (получено %q)", name, v)
				}
			}
			plan, steps := workerInstallPlanSep(argString(args, "host") == "", whisper, seedvc, roformer, hfHome, seedvcDir, sepDir)
			out, err := runSteps(argString(args, "host"), steps, !dryRun)
			out = plan + "\n" + out + workerAddress(s)
			if dryRun {
				out = "СУХОЙ ПРОГОН — ничего не выполнено, только план (установка: dry_run=false, confirm=true, whisper, seedvc, roformer).\n\n" + out
			}
			if err != nil {
				return "", fmt.Errorf("%w\n\n%s", err, out)
			}
			return out, nil
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
			out, err := runSteps("", []step{
				{name: "фронтенд", cmd: "cd frontend && npm ci && npm run build", must: true},
				{name: "wails", cmd: "wails build"},
			}, !dryRun)
			if dryRun {
				out = "СУХОЙ ПРОГОН — ничего не выполнено (сборка: dry_run=false + confirm=true):\n" + out
			}
			if err != nil {
				return "", fmt.Errorf("%w\n\n%s", err, out)
			}
			return out, nil
		},
	})
}

// Размеры компонентов воркера, ГБ (замер на установленной машине, 2026-10-03):
// воркер — окружение 7,4 + веса YuE2-3B 6,8, MERT 2,4, VAE 0,5, SheetSage 0,2,
// demucs 0,1; whisper — окружение 2,7 + модель small 0,5; Seed-VC — 11 после
// установки и ~14 на время установки (см. seedvc_install.sh).
const (
	sizeWorkerGB     = 17.5
	sizeWhisperGB    = 3.2
	sizeSeedvcGB     = 11
	sizeSeedvcPeakGB = 14
	sizeSepGB        = 7.3 // разделение дорожек: окружение audio-separator 6,1 + веса RoFormer/DrumSep 1,1
	sizeReserveGB    = 2   // запас под данные треков на первое время
)

// gbStr — «17.5», «11»: без лишних нулей.
func gbStr(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// shellQuote — путь как одно слово shell без подстановок: «'…'» (одинарная кавычка внутри
// — «'\”»), «~/…» → «"$HOME"/'…'». Пробелы, кириллица, $, `, кавычки проходят буквально.
// $HOME раскрывается сразу — в worker.env пишется абсолютный путь (systemd тильду не раскрывает).
func shellQuote(p string) string {
	q := func(x string) string { return "'" + strings.ReplaceAll(x, "'", `'\''`) + "'" }
	if p == "~" {
		return `"$HOME"`
	}
	if strings.HasPrefix(p, "~/") {
		return `"$HOME"/` + q(p[2:])
	}
	return q(p)
}

// workerAddress — строка в конец отчёта: по какому адресу MCP ходит к воркеру.
func workerAddress(s *Server) string {
	return fmt.Sprintf("\nадрес воркера сейчас: %s (сменить: config_set)", s.client.GetURL())
}

// diskNeedGB — сколько места нужно под выбранные компоненты (Seed-VC — по пику установки).
func diskNeedGB(whisper, seedvc, roformer bool) float64 {
	need := sizeWorkerGB + sizeReserveGB
	if roformer {
		need += sizeSepGB
	}
	if whisper {
		need += sizeWhisperGB
	}
	if seedvc {
		need += sizeSeedvcPeakGB
	}
	return need
}

// workerInstallPlan — текст для пользователя (компоненты, место) и шаги установки.
func workerInstallPlan(local, whisper, seedvc bool, hfHome, seedvcDir string) (string, []step) {
	return workerInstallPlanRF(local, whisper, seedvc, false, hfHome, seedvcDir)
}

// workerInstallPlanRF — то же с выбором RoFormer (чистое разделение дорожек, необязательно).
func workerInstallPlanRF(local, whisper, seedvc, roformer bool, hfHome, seedvcDir string) (string, []step) {
	return workerInstallPlanSep(local, whisper, seedvc, roformer, hfHome, seedvcDir, "")
}

// workerInstallPlanSep — с каталогом RoFormer sepDir: "" — ~/sep-venv и веса ~/sep-models
// (пути по умолчанию воркера); иначе <sepDir>/venv и <sepDir>/models, пути — в worker.env,
// а место RoFormer на домашнем диске не считается (он на другом).
func workerInstallPlanSep(local, whisper, seedvc, roformer bool, hfHome, seedvcDir, sepDir string) (string, []step) {
	need := diskNeedGB(whisper, seedvc, roformer && sepDir == "")
	return planText(whisper, seedvc, roformer, need, hfHome, seedvcDir),
		installSteps(local, whisper, seedvc, roformer, need, hfHome, seedvcDir, sepDir)
}

func planText(whisper, seedvc, roformer bool, need float64, hfHome, seedvcDir string) string {
	mark := map[bool]string{true: "[x]", false: "[ ]"}
	var b strings.Builder
	b.WriteString("Компоненты:\n")
	fmt.Fprintf(&b, "  [x] воркер — генерация (YuE2), разбор нот, дорожки (demucs): ~%s ГБ (веса качаются при первом запуске)\n",
		gbStr(sizeWorkerGB))
	fmt.Fprintf(&b, "  %s RoFormer — чистое разделение дорожек (+ барабаны по частям), включается в настройках: ~%s ГБ, "+
		"веса некоммерческие (CC BY-NC-SA); без него дорожки делает demucs\n", mark[roformer], gbStr(sizeSepGB))
	fmt.Fprintf(&b, "  %s whisper — распознавание текстов треков: ~%s ГБ\n", mark[whisper], gbStr(sizeWhisperGB))
	fmt.Fprintf(&b, "  %s Seed-VC — «голос альбома», ЭКСПЕРИМЕНТ (голос узнаётся, но дрожит): ~%s ГБ после установки, ~%s ГБ во время, каталог %s\n",
		mark[seedvc], gbStr(sizeSeedvcGB), gbStr(sizeSeedvcPeakGB), seedvcDir)
	fmt.Fprintf(&b, "Нужно места: ~%s ГБ (веса HF — в %s; если он на другом диске, места на домашнем нужно меньше).\n",
		gbStr(need), hfHome)
	b.WriteString("Без whisper, Seed-VC и RoFormer приложение работает; соответствующие функции будут неактивны с подписью «не установлен».\n")
	return b.String()
}

// Проверки до установки: их провал останавливает установку (must).
const (
	checkGPUCmd = "nvidia-smi --query-gpu=name,memory.total --format=csv,noheader || " +
		"{ echo 'нет NVIDIA GPU или драйвера: воркеру нужна видеокарта с CUDA'; exit 1; }"
	// %[1]d — сколько ГБ нужно; df -BG печатает свободное место в гигабайтах
	checkDiskCmd = `free=$(df -BG --output=avail ~ | tail -1 | tr -dc 0-9); echo "свободно $free ГБ, нужно ~%[1]d ГБ"; ` +
		`[ "$free" -ge %[1]d ] || { echo 'мало места: освободите диск или поставьте веса/Seed-VC на другой (hf_home, seedvc_dir)'; exit 1; }`
)

func installSteps(local, whisper, seedvc, roformer bool, need float64, hfHome, seedvcDir, sepDir string) []step {
	steps := []step{
		{name: "проверка: видеокарта NVIDIA и драйвер", cmd: checkGPUCmd, must: true},
		{name: "проверка: место на диске", cmd: fmt.Sprintf(checkDiskCmd, int(math.Ceil(need))), must: true},
	}
	if local {
		steps = append(steps, step{name: "скопировать файлы воркера", must: true, cmd: "test -f worker/yue_worker.py || " +
			"{ echo 'запусти из корня репозитория Yue Studio'; exit 1; }; mkdir -p ~/yue-studio && " +
			"cp worker/*.py worker/fx_blocks.json worker/requirements.txt worker/requirements-seedvc.txt worker/seedvc_install.sh ~/yue-studio/"})
	}
	steps = append(steps,
		step{name: "каталоги", cmd: "mkdir -p ~/yue-studio/data ~/yue-studio/units"},
		step{name: "venv воркера (python 3.12)", must: true, cmd: "uv venv ~/yue/.venv --python 3.12 || python3 -m venv ~/yue/.venv"},
		step{name: "пакеты (torch cu128 + API)", must: true, cmd: "uv pip install --python ~/yue/.venv/bin/python -r ~/yue-studio/requirements.txt " +
			"--index-url https://download.pytorch.org/whl/cu128 --extra-index-url https://pypi.org/simple || " +
			"~/yue/.venv/bin/pip install -r ~/yue-studio/requirements.txt"},
	)
	if roformer { // не must: без него разделение идёт прежним demucs
		venv, py := "~/sep-venv", "~/sep-venv/bin/python"
		if sepDir != "" {
			venv, py = shellQuote(sepDir+"/venv"), shellQuote(sepDir+"/venv/bin/python")
		}
		steps = append(steps, step{name: "RoFormer (чистое разделение дорожек, отдельное окружение)", cmd: fmt.Sprintf(
			`uv venv %s --python 3.12 && uv pip install --python %s "audio-separator[gpu]" audioread`, venv, py)})
	}
	if whisper {
		steps = append(steps, step{name: "whisper (тексты треков)", cmd: "uv venv ~/whisper-venv && " +
			"uv pip install --python ~/whisper-venv/bin/python faster-whisper"})
	}
	steps = append(steps, step{name: "worker.env", cmd: fmt.Sprintf(
		`test -f ~/yue-studio/worker.env || printf 'HF_HOME=%%s\n' %s > ~/yue-studio/worker.env`, shellQuote(hfHome))})
	if roformer && sepDir != "" { // нестандартный каталог — воркер узнаёт его из worker.env
		steps = append(steps, step{name: "worker.env: пути RoFormer", cmd: fmt.Sprintf(
			`(grep -q '^YUE_SEP_PY=' ~/yue-studio/worker.env || printf 'YUE_SEP_PY=%%s\n' %s >> ~/yue-studio/worker.env) && `+
				`(grep -q '^YUE_SEP_MODELS=' ~/yue-studio/worker.env || printf 'YUE_SEP_MODELS=%%s\n' %s >> ~/yue-studio/worker.env)`,
			shellQuote(sepDir+"/venv/bin/python"), shellQuote(sepDir+"/models"))})
	}
	if seedvc {
		dir := shellQuote(seedvcDir)
		steps = append(steps, step{name: "Seed-VC («голос альбома», эксперимент)", cmd: fmt.Sprintf(
			`~/yue-studio/seedvc_install.sh %[1]s && (grep -q '^YUE_SEEDVC_DIR=' ~/yue-studio/worker.env || `+
				`printf 'YUE_SEEDVC_DIR=%%s\n' %[1]s >> ~/yue-studio/worker.env)`, dir)})
	}
	return append(steps,
		step{name: "systemd-юнит", cmd: "cp ~/yue-studio/units/yue-worker.service ~/.config/systemd/user/ 2>/dev/null && " +
			"systemctl --user daemon-reload && systemctl --user enable --now yue-worker || " +
			"echo 'юнит пропущен (запуск вручную: ~/yue/.venv/bin/python ~/yue-studio/yue_worker.py)'"},
		step{name: "health", cmd: "sleep 3 && curl -sf --max-time 10 http://localhost:8091/health"},
	)
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
// кроме must: его провал останавливает выполнение и возвращается ошибкой.
func runSteps(host string, steps []step, run bool) (string, error) {
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
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			b.WriteString("  " + strings.ReplaceAll(trimmed, "\n", "\n  ") + "\n")
		}
		if err == nil {
			continue
		}
		fmt.Fprintf(&b, "  [ошибка: %v]\n", err)
		if st.must {
			fmt.Fprintf(&b, "\nОСТАНОВЛЕНО: шаг «%s» обязателен; не выполнено шагов: %d.\n", st.name, len(steps)-i-1)
			return b.String(), fmt.Errorf("установка остановлена: шаг «%s» не прошёл", st.name)
		}
	}
	return b.String(), nil
}
