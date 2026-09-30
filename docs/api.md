# HTTP API воркера

Все ответы — JSON, если не указано иное. Базовый URL — адрес воркера (по умолчанию `http://<gpu-host>:8091`).
Аутентификации нет (см. раздел «Безопасность» в README).

```
GET/POST /config       настройки: ollama_url, ollama_model (меняются на лету)
GET  /health           статус, модель в памяти?
POST /jobs             {title, style, lyrics, seed, cot, abc?, draft?, arc?, max_tokens?} — abc: свой план; draft: черновик ~15-20 с;
                       max_tokens — жёсткий потолок длины (~25 т/с: 3000 ≈ 1–2 мин), 0 = бюджет воркера;
                       arc: драматургия поверх плана (build|wave|burst: дуга темпа по секциям,
                       burst — голос на октаву выше в финале; с abc несовместим);
                       parent_id?, role? — производный трек (section | rebuild | fragment | variant;
                       continue — только через /continue): в списке приложения прячется под родителем («📎 N»)
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
GET  /jobs/{id}/peaks?file=&bins=  огибающая громкости артефакта (волна студии):
                       {file, bins, duration_sec, peaks: [[min,max],...]} — амплитуды [-1,1] по окнам,
                       file пуст = основной трек; bins пуст = каноническое ~10 окон/с (кламп 500..8000,
                       только оно кэшируется), явное — кламп 100..12000 без кэша. Кэш-сайдикар
                       <файл>.peaks.json рядом с аудио (mtime-гейт, _v — как score.json)
GET  /jobs/{id}/spectrum.png?file=  спектрограмма артефакта готовой картинкой ffmpeg
                       (showspectrumpic 1600×320, ось X линейна 0..длительность — та же шкала
                       времени, что волна). Кэш <файл>.spectrum.png + метаданные .spectrum.json.
                       ffmpeg на GPU-машине опционален: нет бинарника → 503 (волна работает без него)
POST /jobs/{id}/preview {from_sec, to_sec} → preview-*.flac (VAE-decode куска латентов)
POST /jobs/{id}/overdub {style, lyrics, seed?, gain, abc?} → джоба-партия поверх трека (микс автоматом; abc — свой план партии, так «+ инструмент» локализуется выделением; seed родителя = та же интерпретация;
                       lyrics — текст голоса, без него модель поёт вокализ)
POST /jobs/{id}/stems  demucs → stem-{drums,bass,other,vocals}.flac
GET  /jobs/{id}/stems  список стемов

POST /corpus           {name} — профиль из корпуса
POST /corpus/{id}/track  трек (байты): DSP + транскрипция + Whisper
POST /corpus/{id}/build  агрегация → profile.json (стиль через Ollama)
GET  /corpus[/{id}]    список / профиль

POST /voices           {name, job_id, params, seed} — карточка голоса из джобы-прослушивания
                       примерочной (копия аудио → voices/<id>/, переживает удаление джобы)
GET  /voices           список карточек (job_alive — жива ли исходная джоба, has_audio)
DELETE /voices/{id}   удалить карточку (строка БД + voices/<id>/)

DELETE /jobs/{id}/dsp/{file}  удалить вариант эффекта/вклейки (файл + метрики)
POST /jobs/{id}/variant_track  {file, title, voice_src?} — вариант DSP-эффекта (dsp-*.flac) отдельным
                       треком-готов: копия аудио + партитура исходника, стемы/минус работают
                       (parent_id = исходник, role = variant)
                       voice_src — рендер, чей голос подставлен в версию («перепеть с места»;
                       несуществующий → 422). В /jobs — поле voice_src (источник голоса версии;
                       пусто — голос свой у сгенерированного трека или от родителя у варианта)
POST /jobs/{id}/continue {from_sec, seed?, abc?, style_add?} — «продолжение с места»: новая
                       джоба = шаги модели исходника (semantic.npy) до from_sec + продолжение
                       (другой сид; abc — изменённый план; style_add — приписка к стилю исходника
                       «что изменить в звучании»). parent_id = исходник, role = continue, cont_from.
                       422: нет semantic.npy или from_sec за концом трека
GET  /jobs/{id}/vocal_contour?from=&to=  высота голоса по тактам плана (стем vocals, pyin):
                       {bars:[{index,start,end,notes[4]}], median_hz, low_hz, high_hz}; ноты по
                       четвертям такта («D4», «·» — нет голоса); to=0 — до конца. 409 — нет стема
                       vocals (сначала stems). Сверка «спето ли по плану» и потолка голоса
POST /jobs/{id}/head   {head_id?} — основная версия песни: id — корень, head_id — он сам или его
                       потомок (иначе 422); null/0 — основной снова сам трек. В /jobs — поле head_id
```
