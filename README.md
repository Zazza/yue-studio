# Yue Studio

Desktop-приложение (Wails v2: Go + Vue 3) генерации музыки над YuE2-3B.
Генерация — на отдельном воркере (FastAPI :8091) с GPU; адрес воркера и Ollama настраиваются в приложении (⚙ в шапке).

Документация: [docs/](docs/) — архитектура, гайд по UI, работа со стихом и мелодией, честные ограничения.

## Структура

- `main.go`, `app.go`, `player.go` — Wails-приложение: биндинги, прокси артефактов
- `internal/yue/` — Go-клиент воркера (jobs, plan, fan, артефакты; Proxy: nil — мимо локального прокси)
- `worker/yue_worker.py` — FastAPI :8091: резидентная YuE2Pipeline + очередь SQLite
- `frontend/` — Vue 3 + Vite; `src/presets.js` — откалиброванные пресеты; `src/groups.js` — группы библиотеки стилей
- Машинные настройки воркера — `~/yue-studio/worker.env` на GPU-машине (`YUE_OLLAMA_URL`, `YUE_OLLAMA_MODEL`, см. юнит).

`deploy.sh worker|build` — деплой воркера (хост: `YUE_DEPLOY_HOST=user@gpu-host`) / сборка desktop-приложения

## Что умеет

- Форма-слоты стиля → компиляция в строку, или стиль одной строкой
- Библиотека стилей: 27 курируемых групп (100+ стилей) — двухуровневый выбор «группа → стиль», плюс свои группы (страница «Библиотека»)
- Веер best-of-N: «×5 сидов» = 5 джоб с сидами base+0..4
- Стадия плана отдельно: «План (ABC)» → просмотр/правка ABC до рендера → рендер по своему ABC
- **Из трека (Remix)**: транскрипция трека SheetSage2 → ABC → правка → кавер с новым стилем/текстом
- ABC готовой джобы — обратно в редактор («ABC →»), «↺» — вернуть параметры джобы в форму
- Очередь/статусы/отмена, история, `/listen/{id}` в браузере (у webview нет звука), сохранение flac
- **Пиано-ролл**: расклад партитуры по тактам × голоса (▓/▒/░ по плотности), аккорды, секции;
  выделение тактов мышью → **превью фрагмента** (VAE-decode куска латентов, секунды, без AR-генерации)
- **Инструментальная стойка**: карточки инструментов по группам с примочками — разворачиваются
  в текст стиля автоматически, словесная кухня спрятана
- **Овердаб**: рендер партии по партитуре джобы с новым стилем + микс с оригиналом
  (не настоящий овердаб: YuE2 не даёт стемов)
- **Стемы (demucs)**: drums/bass/other/vocals из готового трека
- **Профиль из корпуса**: 3–10 треков исполнителя → тональности/прогрессии/темп/структура
  (SheetSage2), DSP-паспорт, тексты (faster-whisper), строка стиля (Ollama) → пресет
- DSP-цепочки эффектов (ffmpeg на ПК), метрики с дельтами до референсов/джоб
- Копайтер стихов (Ollama qwen2.5)

## API воркера

```
GET/POST /config       настройки: ollama_url, ollama_model (меняются на лету)
GET  /health           статус, модель в памяти?
POST /jobs             {title, style, lyrics, seed, cot, abc?}  — abc: рендер по своему плану
POST /plan             {style, lyrics, seed, cot} → {abc, truncated, seconds}  — только план
GET  /jobs[/{id}]      список/статус (req_abc = рендер по своему ABC)
POST /jobs/{id}/cancel отмена (queued)
GET  /audio/{id}/{f}   audio.flac/.mp3/.wav, score.abc, request.abc, dsp/overdub/preview/stem-*.flac
GET  /listen/{id}      страница прослушивания (?f= — вариант)

POST /transcribe       трек (байты, X-Filename) → ABC (SheetSage2) — каверы
GET  /jobs/{id}/score  таймлайн: такты × голоса, аккорды, секции, RMS по секциям
POST /jobs/{id}/preview {from_sec, to_sec} → preview-*.flac (VAE-decode куска латентов)
POST /jobs/{id}/overdub {style, gain} → джоба-партия поверх трека (микс автоматом)
POST /jobs/{id}/stems  demucs → stem-{drums,bass,other,vocals}.flac
GET  /jobs/{id}/stems  список стемов

POST /corpus           {name} — профиль из корпуса
POST /corpus/{id}/track  трек (байты): DSP + транскрипция + Whisper
POST /corpus/{id}/build  агрегация → profile.json (стиль через Ollama)
GET  /corpus[/{id}]    список / профиль
```

## Разработка

```bash
./deploy.sh worker   # задеплоить воркер на GPU-машину (scp + systemctl --user restart)
./deploy.sh build    # ~/go/bin/wails build → build/bin/yue-studio
```

### Как воркер (Python) попадает на машину с YuE

Воркер — это обычные Python-файлы из `worker/` + окружение на GPU-машине.
Скрипт `./deploy.sh worker` делает это автоматически: `scp` файлов
(`yue_worker.py`, `dsp.py`, `sheetsage.py`, `stems.py`, `abcparse.py`,
`whisper_run.py`) в `~/yue-studio` и рестарт systemd-юнита `yue-worker.service`.
Для другой GPU-машины — то же самое руками:

1. Скопировать `worker/*.py` в `~/yue-studio/` на GPU-машине (scp / WinSCP /
   просто флешка).
2. Окружение (Ubuntu + CUDA, один раз):
   ```bash
   uv venv ~/yue/.venv --python 3.12
   uv pip install --python ~/yue/.venv/bin/python torch torchaudio --index-url https://download.pytorch.org/whl/cu128
   uv pip install --python ~/yue/.venv/bin/python fastapi uvicorn soundfile lameenc librosa \
       transformers>=4.57 einops "torchaudio==2.10.0" pretty_midi mido mir_eval demucs
   ```
3. Отдельный venv для Whisper: `uv venv ~/whisper-venv && uv pip install --python ~/whisper-venv/bin/python faster-whisper`
   (воркер вызывает его сам, путь зашит в `WHISPER_PY`).
4. Веса: `HF_HOME` — каталог кеша Hugging Face, например `/opt/yue/hf-cache`
   (YuE2, SheetSage2, demucs качаются при
   первом вызове; нужен интернет).
5. Запуск: `~/yue/.venv/bin/python ~/yue-studio/yue_worker.py` (порт 8091);
   для автозапуска — systemd-юнит из `deploy/units/` (Linux) или любой другой
   способ (окно терминала, Task Scheduler, launchd).

Ollama (копайтер, перевод слотов, строка стиля профиля) — отдельный сервис на
`127.0.0.1:11434`, модель `qwen2.5-chat-ru:latest`. Не обязателен: без него
генерация работает, недоступны копайтер/автоперевод/профиль-стиль.
Настраивается переменными: `YUE_OLLAMA_URL`, `YUE_OLLAMA_MODEL`.

Переменные воркера: `YUE_DATA_DIR` (данные, по умолчанию `~/yue-studio/data`),
`YUE_OLLAMA_URL`/`YUE_OLLAMA_MODEL`, `YUE_WHISPER_PY` (интерпретатор
whisper-venv), `HF_HOME` (кеш весов).

### Воркер в WSL2 (та же машина, другой бут — Windows)

Если GPU-машина — dual-boot: на Linux-буте воркер как обычно; на Windows-буте
поднимается клон в WSL2, GPU пробрасывается (CUDA в WSL2 поддерживается):

```powershell
wsl --install -d Ubuntu            # один раз; драйвер NVIDIA уже стоит в Windows
```

Внутри WSL (Ubuntu):
```bash
# 1. окружение — как в разделе выше (uv venv, пакеты, whisper-venv)
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

### Сборка desktop-приложения по ОС

Требования общие: Go 1.22+, Node.js 20+, [Wails CLI v2](https://wails.io) (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

**Linux (основная, всё протестировано):**
```bash
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
./deploy.sh build          # или просто: wails build
# бинарник: build/bin/yue-studio
```
Звук — через встроенный pw-play плеер (PipeWire): в webkit2gtk у webview нет
аудиовыхода, это ограничение именно Linux-сборки.

**Windows:**
```powershell
wails build                # webview2 (Edge) — ставится автоматически
# бинарник: build\bin\yue-studio.exe
```
Нюансы: бэкенд плеера `player.go` использует pw-play и POSIX-сигналы — на
Windows звук пока не работает «из коробки» (нужен порты: `<audio>` в webview2
работает, в отличие от Linux). `deploy.sh` заменить на ручной scp/WinSCP или
запуск из WSL/Git Bash. Воркер может оставаться на GPU-машине — приложению нужен
только HTTP-доступ к нему.

**macOS:**
```bash
wails build                # WKWebView
# бинарник: build/bin/yue-studio.app
```
Те же нюансы, что и Windows: pw-play нет, нужен аудио-бэкенд (afplay или
`<audio>` — в WKWebView звук работает). Не тестировалось.

Адрес воркера задаётся переменной `YUE_URL` или меняется в шапке приложения
(клик по адресу сервера).

YuE-окружение: `~/yue/.venv`, веса в каталоге `HF_HOME`, юнит `yue-worker.service`.
Дополнительно в `~/yue/.venv`: torchaudio, pretty_midi, mir_eval, demucs (SheetSage2 качается из HF
при первом вызове); faster-whisper — в отдельном `~/whisper-venv`.

## Известные ограничения

- **Встроенный звук в webview недоступен** (webkit2gtk/Wails): `<audio>` и WebAudio молчат.
  Прослушивание — страница `/listen/{id}` в браузере.
- VRAM 16 ГБ делится с Ollama на той же машине: если Ollama держит модель — генерация падает OOM.
- ffmpeg на GPU-машине не требуется — конвертации в воркере через soundfile + lameenc; SheetSage2 получает
  waveform напрямую (минуя его FFmpeg-вход).
- Точечная правка середины трека невозможна (AR-генерация слева-направо): превью фрагмента —
  только прослушать кусок; правка = полный пересбор (~минута GPU).
- Лицензия весов YuE2/SheetSage2 CC BY-NC — некоммерческое использование.
