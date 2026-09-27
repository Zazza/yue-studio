# HTTP API воркера

Все ответы — JSON, если не указано иное. Базовый URL — адрес воркера (по умолчанию `http://<gpu-host>:8091`).
Аутентификации нет (см. раздел «Безопасность» в README).

```
GET/POST /config       настройки: ollama_url, ollama_model (меняются на лету)
GET  /health           статус, модель в памяти?
POST /jobs             {title, style, lyrics, seed, cot, abc?, draft?}  — abc: свой план; draft: черновик ~15-20 с
POST /plan             {style, lyrics, seed, cot} → {abc, truncated, seconds}  — только план
GET  /jobs[/{id}]      список/статус (req_abc = рендер по своему ABC)
POST /jobs/{id}/cancel отмена: queued — из очереди; running — остановка генерации
                       (пайплайн завершится на ближайшем шаге). В running-джобах
                       /jobs отдаёт живой прогресс: stage, tokens, tok_per_s, elapsed_s
GET  /audio/{id}/{f}   audio.flac/.mp3/.wav, score.abc, request.abc, dsp/overdub/preview/stem-*.flac
GET  /listen/{id}      страница прослушивания (?f= — вариант)

POST /transcribe       трек (байты, X-Filename) → ABC (SheetSage2) — каверы
POST /lyrics           трек (байты, X-Filename) → текст (faster-whisper) — оригинал для кавера
POST /jobs/{id}/lyrics текст из готового аудио джобы (без повторной загрузки файла)
POST /lyrics/adapt     {text, to} → адаптация-перевод под пение с сохранением слогов (Ollama)
GET  /jobs/{id}/score  таймлайн: такты × голоса, аккорды, секции, RMS по секциям
POST /jobs/{id}/preview {from_sec, to_sec} → preview-*.flac (VAE-decode куска латентов)
POST /jobs/{id}/overdub {style, lyrics, gain} → джоба-партия поверх трека (микс автоматом;
                       lyrics — текст голоса, без него модель поёт вокализ)
POST /jobs/{id}/stems  demucs → stem-{drums,bass,other,vocals}.flac
GET  /jobs/{id}/stems  список стемов

POST /corpus           {name} — профиль из корпуса
POST /corpus/{id}/track  трек (байты): DSP + транскрипция + Whisper
POST /corpus/{id}/build  агрегация → profile.json (стиль через Ollama)
GET  /corpus[/{id}]    список / профиль
```
