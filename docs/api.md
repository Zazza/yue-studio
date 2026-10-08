# HTTP API воркера

Все ответы — JSON, если не указано иное. Базовый URL — адрес воркера (по умолчанию `http://<gpu-host>:8091`).
Аутентификации нет (см. раздел «Безопасность» в README).

```
GET/POST /config       настройки: ollama_url, ollama_model (меняются на лету)
GET  /health           статус, модель в памяти?
POST /jobs             {title, style, lyrics, seed, cot, abc?, draft?, arc?, max_tokens?, temperature?, cfg?} — abc: свой план; draft: черновик ~15-20 с;
                       temperature (0 или 0.5–1.5, смелость игры) и cfg (0 или 1–4, точность по стилю/нотам) —
                       характер исполнения, 0 = по умолчанию (1.0 / YUE2_CFG_SCALE); у parent_id и /continue — как у родителя;
                       seed пустой/0 — случайный (записывается в трек), с parent_id — сид родителя (старый родитель без сида → 831001);
                       max_tokens — жёсткий потолок длины (~25 т/с: 3000 ≈ 1–2 мин), 0 = бюджет воркера;
                       arc: драматургия поверх плана (build|wave|burst: дуга темпа по секциям,
                       burst — голос на октаву выше в финале; с abc несовместим);
                       parent_id?, role? — производный трек (section | rebuild | fragment | variant;
                       continue — только через /continue): в списке приложения прячется под родителем («📎 N»)
POST /plan             {style, lyrics, seed, cot} → {abc, truncated, seconds, seed}  — только план (seed пустой — случайный, в ответе)
GET  /jobs[/{id}]      список/статус (req_abc = рендер по своему ABC)
                       mixes — число готовых миксов (overdub-inst-*.flac: вклейки, эффекты на дорожки)
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
                       file пуст = основной трек; bins пуст = каноническое ~60 окон/с, окно ~17 мс
                       (кламп 500..20000, только оно кэшируется), явное — кламп 100..20000 без кэша.
                       Кэш-сайдикар <файл>.peaks.json рядом с аудио (mtime-гейт, _v — как score.json;
                       _v=2 — переход с 10 на 60 окон/с пересчитал старые кэши)
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

POST /jobs/{id}/dsp?label=  вариант эффекта/микс от приложения (байты, X-Filename dsp-*/overdub-*.flac);
                       label (до 300 символов) — что сделано («Перегруз голоса · голос»)
GET  /jobs/{id}/dsp    варианты с метриками и label (пусто — понятно по имени файла)
DELETE /jobs/{id}/dsp/{file}  удалить вариант эффекта/вклейки (файл + метрики)
POST /jobs/{id}/fx     звуковой движок: {source: mix|vocals|drums|bass|other|guitar|piano|kick…,
                       chain: [{type: gate|eq|comp|drive|amp|cab|reverb|delay, …}], from?, to?,
                       output?: mix|solo, label?} → вариант dsp-fx-<source>-<хэш>.flac (как у /dsp,
                       + clipped). Без сдвига во времени; amp (NAM) — через очередь GPU. 422 — неверная
                       цепочка (причина в detail), 503 — движок выключен (fx_engine / YUE_FX_ENGINE=0)
                       preview: true (+ обязательные from/to) — только прослушать кусок: файл
                       preview-fx-<хэш>.flac (окно + хвост реверба/дилея до 3 с), без метрик, не в
                       вариантах; тот же запрос — тот же файл без пересчёта; на джобу ≤ 8 таких файлов
                       → {file, duration_sec, clipped}; fade (0…0,5 с, только превью) — вход окна с
                       линейными краями: рост с from, спад после to (пересборка студии вклеивает кусок
                       вместо исходной дорожки с теми же фейдами); pad: true — файл от начала трека (до from —
                       тишина), чтобы вставить его без задержки (adelay ffmpeg ошибается на сэмпл)
GET  /fx/assets        {amps: [{name, latency}], irs: [{name, sr, seconds}], kits: [{name: «osdk/kick», samples}]} —
                       захваты NAM, IR и наборы сэмплов барабанов
POST /fx/kits/install?name=osdk|growlybass  воркер качает набор из своего каталога (GitHub) → {name, parts, downloaded};
                       повтор — без сети; неизвестное имя → 422; сбой сети → 502 (без полукаталога)
POST /fx/assets?kind=amp|ir&name=  загрузить .nam / .wav (байты тела, ≤ 50 МБ) → {name, kind}
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
POST /jobs/{id}/plan_check {abc, from_sec?}  изменённый план против плана джобы: такты по
                       голосам было/стало, длина, изменённые такты (время, текст до/после),
                       потолок голоса (верх + 2 ступени), предупреждения; пустой abc → 422
GET  /jobs/{id}/tones?from=&to=&stem=  узкие устойчивые тона («свист», писк) в окне — в миксе
                       или в дорожке stem (vocals|drums|bass|other|guitar|piano; нет стема → 409, иное → 422):
                       [{hz, prominence_db}] — самый заметный первым, ≥ 12 дБ над окрестностью
                       ±300 Гц, не больше 5; to=0 — до конца; 422 — окно вне трека
GET  /jobs/{id}/vocal_contour?from=&to=  высота голоса по тактам плана (стем vocals, pyin):
                       {bars:[{index,start,end,notes[4]}], median_hz, low_hz, high_hz}; ноты по
                       четвертям такта («D4», «·» — нет голоса); to=0 — до конца. 409 — нет стема
                       vocals (сначала stems). Сверка «спето ли по плану» и потолка голоса
GET  /sound-presets    пресеты звука [{id, slug, name, note, specs, final, reference_job_id, builtin}]:
                       встроенные (slug transmission, sex-on-fire; при старте обновляются по slug из кода, id прежний) первыми
POST /sound-presets    {name, note?, specs?, final?, reference_job_id?, target_lufs?} → пресет с id. target_lufs
                       (−24…−6 или null) — громкость результата: после финала приложение добавляет level с
                       усилением «цель − громкость микса» (±12 дБ, потолок −1). specs — до 16 правок
                       на весь трек: {stems: [vocals|drums|bass|other|guitar|piano|kick|snare|toms|hh|ride|crash],
                       ровно одно из engine (цепочка движка, проверка как у /fx) | chain+params (dsp-цепочка) |
                       steps [{chain, params, off}] (до 12), db −24…24}; final — до 12 шагов {chain, params, off}
                       на весь микс после правок; пустой пресет и прочие ошибки → 422 с причиной
PUT  /sound-presets/{id}  заменить свой целиком (как POST); DELETE /sound-presets/{id} — удалить;
                       встроенный → 409, нет такого → 404
POST /jobs             + sound_preset_ids: [id] (до 3 разных существующих; с draft — 422) — у джобы поле
                       sound_presets [{id, status: pending|running|done|error, child_id, error}] (у старых — [])
POST /jobs/{id}/sound-presets/{pid}/state {status, child_id?, error?}  статус пресета у трека (применяет
                       приложение): pending→running (захват; уже не pending → 409), running→done (child_id —
                       версия), running→error (error ≤ 500), error|running→pending (повтор); прочее → 409,
                       нет поля → 422, пресета нет у трека → 404. Ответ — список sound_presets трека
POST /jobs/{id}/head   {head_id?} — основная версия песни: id — корень, head_id — он сам или его
                       потомок (иначе 422); null/0 — основной снова сам трек. В /jobs — поле head_id
```
