# Развёртывание

Три части: GPU-машина с воркером, опционально Ollama, desktop-приложение на ПК.
Адрес воркера задаётся переменной `YUE_URL` или меняется в шапке приложения.

## Воркер (Python) на GPU-машине

Воркер — обычные Python-файлы из `worker/` + окружение на GPU-машине (Ubuntu + CUDA).
Требования — `worker/requirements.txt`; машинные настройки — `worker.env.example`
(скопировать в `~/yue-studio/worker.env`, подхватывается systemd-юнитом).

Вручную:

1. Скопировать `worker/*.py` в `~/yue-studio/` на GPU-машину (scp / WinSCP / флешка).
2. Окружение (один раз):
   ```bash
   uv venv ~/yue/.venv --python 3.12
   uv pip install --python ~/yue/.venv/bin/python -r worker/requirements.txt \
       --index-url https://download.pytorch.org/whl/cu128 --extra-index-url https://pypi.org/simple
   ```
3. Отдельный venv для Whisper (тексты треков корпусов):
   `uv venv ~/whisper-venv && uv pip install --python ~/whisper-venv/bin/python faster-whisper`
   (воркер вызывает его сам, путь — `YUE_WHISPER_PY`).
4. Веса: `HF_HOME` — каталог кеша Hugging Face (YuE2, SheetSage2, demucs качаются при
   первом вызове; нужен интернет; YuE2 — gated-репо, нужен HF-токен).
5. Запуск: `~/yue/.venv/bin/python ~/yue-studio/yue_worker.py` (порт 8091);
   для автозапуска — systemd-юнит из `deploy/units/` или другой способ
   (Task Scheduler, launchd, окно терминала).
6. Опционально: `ffmpeg` в PATH — только для спектрограммы в студии
   (`GET /jobs/{id}/spectrum.png`, готовой картинкой showspectrumpic).
   Нет бинарника — режим «спектр» честно откажет (503), волна громкости
   работает и без ffmpeg: `sudo apt install ffmpeg`.

### Автоматический деплой

`make worker` (или `./deploy.sh worker`) — scp файлов воркера на GPU-машину и рестарт
systemd-юнита `yue-worker`. Хост задаётся `YUE_DEPLOY_HOST=user@gpu-host`.
`worker.env` на машине не перезаписывается.

### Переменные воркера

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `YUE_DATA_DIR` | `~/yue-studio/data` | SQLite-очередь, джобы, профили корпусов |
| `YUE_OLLAMA_URL` | `http://127.0.0.1:11434/api/chat` | Ollama для копайтера/перевода/стиля профиля |
| `YUE_OLLAMA_MODEL` | `qwen2.5-chat-ru:latest` | модель Ollama |
| `YUE_WHISPER_PY` | `~/whisper-venv/bin/python` | интерпретатор whisper-venv |
| `HF_HOME` | — | кеш весов Hugging Face |
| `YUE2_MAX_TOKENS` | `16000` | бюджет семантических токенов на песню ≈ макс. длина (дефолт протокола 9000 ≈ 4.5–6 мин; 16000 ≈ до ~10 мин; пик VRAM растёт с длиной) |
| `YUE2_CFG_SCALE` | `1.5` | следование стилю (classifier-free guidance): выше — точнее, но суше |
| `YUE_SEEDVC_DIR` | `~/yue-studio/seedvc` | каталог Seed-VC («голос альбома», необязательно) — см. ниже |

Настройки Ollama также меняются на лету из приложения (⚙ в шапке → `/config` воркера).

## «Голос альбома» — Seed-VC (необязательно, ЭКСПЕРИМЕНТ)

> Экспериментально: голос узнаётся (сходство с образцом по модели говорящего —
> почти как у самого образца), но **дрожит** — «как будто стесняется петь». Это
> свойство Seed-VC: не уходит ни от шагов/cfg, ни от дообучения на голос, ни от
> другого разделителя (BS-RoFormer на наших треках вышел хуже demucs). Внутри фраз
> музыка слегка страдает: дорожка голоса demucs несёт куски музыки.

Замена тембра голоса трека на тембр образца: голос из другого трека (например, «голос
альбома») накладывается на новую песню, музыка и мелодия не меняются
(`POST /jobs/{id}/voice`, MCP `voice_convert`). Делает это
[Seed-VC](https://github.com/Plachtaa/seed-vc) — отдельная установка в своём каталоге
и venv; воркер вызывает его отдельной программой (`seedvc_run.py`), как whisper.
Без установки воркер работает как раньше, запрос отвечает 503 с подсказкой.

Установка на GPU-машине (после `make worker` файлы уже в `~/yue-studio/`):

```bash
~/yue-studio/seedvc_install.sh                 # в ~/yue-studio/seedvc
~/yue-studio/seedvc_install.sh /data/yue-seedvc  # или в свой каталог
```

- Нужно: `git`, [`uv`](https://docs.astral.sh/uv/), CUDA-GPU, **~14 ГБ** на время установки,
  ~10 ГБ после: venv 7,5 (из них CUDA-библиотеки 4,3) + веса 2,4 (скрипт проверит место до установки и уберёт кэш пакетов в конце). Всё — код закреплённой версии, venv, веса,
  кэши uv и Hugging Face — внутри одного каталога: мимо него на системный диск
  ничего не пишется, каталог можно перенести на другой диск целиком.
- Каталог не по умолчанию — `YUE_SEEDVC_DIR=<каталог>` в `~/yue-studio/worker.env`
  и перезапуск воркера. Проверка: MCP `doctor` или `GET /config` (`seedvc_available`).
- PyTorch ставится с индекса CUDA 12.8; другая CUDA — `YUE_TORCH_INDEX=<индекс>` перед
  запуском скрипта.
- Ресурсы (замер на RTX 4070 Ti SUPER 16 ГБ): +3,8 ГБ видеопамяти на время замены
  (рядом с загруженной YuE2 ~7.7 ГБ помещается); 50 шагов диффузии — ~0.6 от
  длительности голоса.
- Лицензия Seed-VC — GPL-3.0: ставится пользователем явно и работает отдельной
  программой, код Yue Studio под неё не попадает.

## Ollama

Отдельный сервис на `127.0.0.1:11434`, модель `qwen2.5`. Не обязателен: без него
генерация работает, недоступны копайтер/автоперевод/профиль-стиль.

## Воркер в WSL2 (dual-boot Windows/Linux)

На Windows-буте поднимается клон в WSL2, GPU пробрасывается (CUDA в WSL2 поддерживается):

```powershell
wsl --install -d Ubuntu            # один раз; драйвер NVIDIA уже стоит в Windows
```

Внутри WSL (Ubuntu):
```bash
# 1. окружение — как в разделе выше (venv, пакеты, whisper-venv)
# 2. веса не перекачивать: HF_HOME указывает на общий с Linux-бутом диск
#    (пример для диска D:):
export HF_HOME=/mnt/d/AI/hf-cache
# 3. общие данные с Linux-бутом — та же папка на общем диске (или своя):
export YUE_DATA_DIR=/mnt/d/yue-studio-data
# 4. Ollama под Windows: localhost Windows из WSL2
export YUE_OLLAMA_URL=http://$(ip route show default | awk '{print $3}'):11434/api/chat
#    (в WSL с mirrored-сетью подойдёт и http://127.0.0.1:11434/api/chat)
# 5. запуск:
~/yue/.venv/bin/python ~/yue-studio/yue_worker.py
```

Приложение на Windows подключается к `http://localhost:8091` (или к IP WSL —
`wsl hostname -I`). Нюанс: если данные делятся через общий диск, не запускайте
воркеры обоих бутов одновременно — SQLite-база одна.

## Сборка desktop-приложения по ОС

Требования: Go 1.22+, Node.js 20+, [Wails CLI v2](https://wails.io)
(`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

**Linux (основная, всё протестировано):**
```bash
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
make build          # или: wails build
# бинарник: build/bin/yue-studio
```
Звук — через встроенный pw-play плеер (PipeWire): в webkit2gtk у webview нет
аудиовыхода, это ограничение именно Linux-сборки.

**Windows:**
```powershell
wails build          # webview2 (Edge) — ставится автоматически
# бинарник: build\bin\yue-studio.exe
```
Звук — через WPF MediaPlayer (PowerShell-обёртка). `deploy.sh` заменить на ручной
scp/WinSCP или запуск из WSL/Git Bash. Воркер может оставаться на GPU-машине —
приложению нужен только HTTP-доступ к нему.

**macOS:** `wails build` (WKWebView). Те же нюансы, что и Windows: pw-play нет,
нужен аудио-бэкенд (afplay или `<audio>` — в WKWebView звук работает). Не тестировалось.
