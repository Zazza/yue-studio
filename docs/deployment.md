# Развёртывание

Три части: GPU-машина с воркером, опционально Ollama, desktop-приложение на ПК.
Адрес воркера задаётся переменной `YUE_URL` или меняется в шапке приложения.

## Установка: компоненты и где что стоит

Два сценария, установка одна и та же:

- **Всё на одном ПК** (самый частый): приложение и воркер на машине с видеокартой NVIDIA.
- **GPU отдельно:** приложение на рабочем ПК, воркер — на машине с видеокартой по сети
  (в приложении — адрес воркера в настройках; установка — по ssh).

Проще всего ставить через агента: MCP-инструмент `install_worker` (пустой `host` — этот ПК,
`host=user@gpu-host` — отдельная машина). Сухой прогон по умолчанию **ничего не выполняет** —
показывает компоненты, место и шаги; установка — `dry_run=false`, `confirm=true` и явный выбор
необязательных компонентов (`whisper`, `seedvc`). Перед установкой проверяются видеокарта и место
на диске: при нехватке установка не начинается. `doctor` — что стоит и работает сейчас.

| Компонент | Нужен | Место | Без него |
|---|---|---|---|
| Воркер: генерация YuE2, разбор нот, дорожки demucs | обязательно | ~17,5 ГБ (окружение ~7,4 + веса ~10, качаются при первом запуске) | — |
| RoFormer: более чистое разделение дорожек (+ барабаны по частям) | по желанию | ~7,3 ГБ (окружение 6,1 + веса 1,1) | дорожки делает demucs, частей барабанов нет |
| whisper: распознавание текстов треков | по желанию | ~3,2 ГБ | кнопки распознавания неактивны с подписью «не установлен» |
| Seed-VC: «голос альбома» (эксперимент) | по желанию | ~11 ГБ после установки, ~14 ГБ во время | блок «Голос альбома» в студии с подписью «не установлен» |

Веса Hugging Face (`HF_HOME`) и Seed-VC (`YUE_SEEDVC_DIR`) можно держать на другом диске —
место на домашнем тогда нужно меньше. Пути с `~` установщик раскрывает в `$HOME`: systemd читает
`worker.env` без раскрытия тильды.

## Воркер (Python) на GPU-машине

Воркер — обычные Python-файлы из `worker/` + окружение на GPU-машине (Ubuntu + CUDA).
Требования — `worker/requirements.txt`; машинные настройки — `worker.env.example`
(скопировать в `~/yue-studio/worker.env`, подхватывается systemd-юнитом).

Вручную:

1. Скопировать `worker/*.py` и `worker/fx_blocks.json` (описание блоков звукового движка — без него
   движок не загрузится) в `~/yue-studio/` на GPU-машину (scp / WinSCP / флешка).
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
| `YUE_SEP_PY` | `~/sep-venv/bin/python` | интерпретатор окружения разделения (RoFormer) — см. ниже |
| `YUE_SEP_MODELS` | `~/sep-models` | каталог весов RoFormer/DrumSep (~1,1 ГБ, качаются при первом разделении) |
| `YUE_STEMS_MODEL` | — | `demucs` — откат: разделять demucs, даже если RoFormer выбран в настройках и установлен |
| `YUE_NAM_DEPS` | — | папка с пакетами NAM для блока `amp` звукового движка — см. ниже |
| `YUE_FX_ENGINE` | — | `0` — откат: звуковой движок выключен (`/jobs/{id}/fx` → 503), что бы ни было в настройках |

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

## Звуковой движок и усилители NAM

Звуковой движок (`POST /jobs/{id}/fx`, MCP `fx_apply`) работает на numpy/scipy — они уже есть у
воркера, ставить ничего не нужно. Только блоку `amp` (усилитель NAM) нужен пакет
neural-amp-modeler **0.12.2** (MIT; читает захваты `.nam` формата v0.5, 0.13 — уже нет). Импортом он
тянет matplotlib и pytorch_lightning, поэтому ставится отдельной папкой без своего torch (torch берётся
у воркера), на любой диск:

```bash
uv pip install --python ~/yue/.venv/bin/python --target /opt/yue/nam-deps --no-deps \
  neural-amp-modeler==0.12.2 wavio pytorch_lightning lightning_utilities torchmetrics auraloss \
  matplotlib contourpy cycler fonttools kiwisolver pillow pyparsing python-dateutil
echo 'YUE_NAM_DEPS=/opt/yue/nam-deps' >> ~/yue-studio/worker.env   # и перезапуск воркера
```

- Захваты усилителей (`.nam`) и импульсные отклики кабинетов/залов (`.wav`) в поставку не входят:
  пользователь загружает свои (`fx_asset_upload`, у каждого захвата своя лицензия). Лежат в
  `<YUE_DATA_DIR>/fx/amps` и `fx/irs`. Задержка захвата меряется щелчком при первом применении и
  кэшируется рядом (`<имя>.nam.latency.json`).
- Ресурсы (замер на RTX 4070 Ti SUPER): первая загрузка ~13 с (инициализация CUDA), дальше — доли
  секунды; 4 минуты стерео через усилитель — ~2 с; остальные блоки — 0,4–2 с на CPU.
- Без `YUE_NAM_DEPS` движок работает, недоступен только блок `amp` (ошибка с причиной).
- Откат: `YUE_FX_ENGINE=0` в `worker.env` или `config_set fx_engine=false` — эндпоинт отвечает 503,
  эффекты ffmpeg приложения работают как прежде (движок их не заменяет).
- pedalboard (Spotify) не используется: его лицензия GPL-3, проект — MIT.

## Разделение на дорожки — BS-Roformer-SW

Дорожки по умолчанию делает demucs (быстро, ~20 с на 4-минутный трек). По желанию — BS-Roformer-SW
(python-audio-separator): заметно чище (замер на MUSDB18, медиана SDR: голос 11,7 против 9,1 дБ,
«прочее» 7,6 против 4,8), но ~4,5 раза дольше (~1,5 мин на трек); плюс DrumSep раскладывает барабаны
на бочку, малый, томы, хай-хэт, райд, крэш. Включается в настройках приложения («Разделение на
дорожки»), через MCP — `config_set stems_model=roformer`, разово для трека — `make_stems model=roformer`.
Работает в
**отдельном окружении** (у audio-separator свой torch/onnxruntime — в окружении YuE2 они бы
конфликтовали), воркер вызывает его подпроцессом `sep_run.py`:

```bash
uv venv ~/sep-venv --python 3.12
uv pip install --python ~/sep-venv/bin/python "audio-separator[gpu]" audioread
```

Окружение ~6,1 ГБ; при нехватке места на домашнем — поставьте на другой диск и укажите
`YUE_SEP_PY` (и `YUE_SEP_MODELS` для весов) в `worker.env`. Через агента — `install_worker roformer=true
sep_dir=/mnt/d/sep`: окружение встанет в `<sep_dir>/venv`, веса — в `<sep_dir>/models`, пути
допишутся в `worker.env` сами. Нет окружения или оно упало —
разделяет demucs, ошибки нет; `YUE_STEMS_MODEL=demucs` выключает RoFormer принудительно, что бы ни
было выбрано в настройках.
Какая модель активна — в настройках приложения и в `/config` (`stems_model`).

**Лицензия весов:** RoFormer-SW и DrumSep — CC BY-NC-SA (некоммерческие); в репозиторий не
входят, качаются при первом разделении. Для коммерчески чистого режима — `YUE_STEMS_MODEL=demucs`
(веса demucs — MIT).

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
make install-desktop   # ярлык в меню приложений (~/.local/share/applications)
```
Ярлык нужен и ради иконки: GNOME на Wayland берёт её только из `.desktop`-файла
(сопоставляет по имени `yue-studio`), иконку самого окна там не показывает.
Запуск с ярлыка не видит переменных окружения шелла — адрес воркера задаётся
в настройках приложения, а не через `YUE_URL`. Убрать — `make uninstall-desktop`.
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
