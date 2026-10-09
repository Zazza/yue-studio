// Готовые цепочки звукового движка (страница «Инструменты», MCP fx_presets через make mcp-data).
// chain — тот же JSON, что уходит воркеру (POST /jobs/{id}/fx); пропущенное — умолчания блоков.
// amp.model пустой — захват выбирает пользователь из загруженных (в поставке их нет).
// stems — дорожки, для которых цепочка (пульт дорожек студии); нет поля — подходит всем.

export const fxPresets = [
  {
    id: 'guitar-amp-clean',
    stems: ['guitar', 'other'],
    name: { ru: 'Гитара: чистый усилитель', en: 'Guitar: clean amp' },
    note: {
      ru: 'Перегруз YuE через чистый захват (Fender Twin и похожие) с убавленным входом и комнатой — звук заметно другой, старая гитара не узнаётся. Захват — только усилителя, без кабинета.',
      en: 'YuE overdrive through a clean capture (Fender Twin and the like) with lowered input and a room — clearly different, the old guitar is not recognisable. Amp-only capture, no cab.',
    },
    chain: [
      { type: 'gate', threshold_db: -55 },
      { type: 'eq', highpass_hz: 100, lowpass_hz: 5500 },
      { type: 'amp', model: '', input_db: -6 },
      { type: 'cab', cutoff_hz: 8000 },
      { type: 'reverb', decay_s: 1.2, predelay_ms: 15, wet: 0.2 },
    ],
  },
  {
    id: 'guitar-amp-hot',
    stems: ['guitar', 'other'],
    name: { ru: 'Гитара: перегруз погорячее', en: 'Guitar: hotter drive' },
    note: {
      ru: 'Вход усилителя +12 дБ — захват перегруза (JCM2000/JCM900 и похожие) работает в полную силу. На похожем захвате с тем же уровнем замена малозаметна — гоните вход. Захват без кабинета.',
      en: 'Amp input +12 dB — a drive capture (JCM2000/JCM900 and the like) at full tilt. A similar capture at the same level barely changes the sound — push the input. Amp-only capture.',
    },
    chain: [
      { type: 'gate', threshold_db: -55 },
      { type: 'eq', highpass_hz: 100, lowpass_hz: 5500 },
      { type: 'amp', model: '', input_db: 12 },
      { type: 'cab', cutoff_hz: 7000 },
    ],
  },
  {
    id: 'guitar-amp-spring',
    stems: ['guitar', 'other'],
    name: { ru: 'Гитара: Vox и короткий реверб', en: 'Guitar: Vox and short reverb' },
    note: {
      ru: 'Захват в духе Vox AC15 TopBoost, вход +6 дБ, короткий тёмный реверб (встроенный, в духе пружины; настоящую пружину — своим IR) — винтажный звон.',
      en: 'A Vox AC15 TopBoost style capture, input +6 dB, a short dark built-in reverb (spring-like; a real spring — your own IR) — vintage jangle.',
    },
    chain: [
      { type: 'gate', threshold_db: -55 },
      { type: 'eq', highpass_hz: 100, lowpass_hz: 5500 },
      { type: 'amp', model: '', input_db: 6 },
      { type: 'cab', cutoff_hz: 7500 },
      { type: 'reverb', decay_s: 1.0, predelay_ms: 0, lowpass_hz: 4500, wet: 0.3 },
    ],
  },
  {
    id: 'guitar-clean',
    stems: ['guitar', 'other'],
    name: { ru: 'Гитара: чистая с коротким ревербом', en: 'Guitar: clean with short reverb' },
    note: {
      ru: 'Без усилителя: ровнее компрессором, чуть яснее середина, короткий тёмный хвост как у пружины.',
      en: 'No amp: evened out by the compressor, a little clearer mids, a short dark spring-like tail.',
    },
    chain: [
      { type: 'gate', threshold_db: -60 },
      { type: 'eq', highpass_hz: 80, bands: [{ freq_hz: 3000, gain_db: 2, q: 1 }] },
      { type: 'comp', threshold_db: -24, ratio: 3, makeup_db: 3 },
      { type: 'reverb', decay_s: 1.2, predelay_ms: 0, lowpass_hz: 4500, wet: 0.25 },
    ],
  },
  {
    id: 'vocal-plate',
    stems: ['vocals'],
    name: { ru: 'Голос: плейт', en: 'Vocal: plate' },
    note: {
      ru: 'Гейт, срез низа, разборчивость на 3 кГц, компрессор, хвост с предзадержкой 30 мс — слова не тонут.',
      en: 'Gate, low cut, presence at 3 kHz, compressor, tail with 30 ms pre-delay — words stay clear.',
    },
    chain: [
      { type: 'gate', threshold_db: -50 },
      { type: 'eq', highpass_hz: 100, bands: [{ freq_hz: 3000, gain_db: 2, q: 1 }] },
      { type: 'comp', threshold_db: -24, ratio: 3, makeup_db: 6 },
      { type: 'reverb', decay_s: 1.6, predelay_ms: 30, lowpass_hz: 8000, wet: 0.25 },
    ],
  },
  {
    id: 'drums-room',
    stems: ['drums'],
    name: { ru: 'Барабаны: комната', en: 'Drums: room' },
    note: {
      ru: 'Компрессор с медленной атакой (удар проходит) и короткая комната — барабаны «в помещении», а не в вакууме.',
      en: 'Slow-attack compressor (the hit gets through) and a short room — drums in a space, not in a vacuum.',
    },
    chain: [
      { type: 'comp', threshold_db: -18, ratio: 3, attack_ms: 20, release_ms: 150, makeup_db: 2 },
      { type: 'reverb', decay_s: 0.6, predelay_ms: 5, lowpass_hz: 9000, wet: 0.2 },
    ],
  },
  {
    id: 'synth-delay',
    stems: ['other', 'piano'],
    name: { ru: 'Синт: дилей и зал', en: 'Synth: delay and hall' },
    note: {
      ru: 'Срез низа (место басу), повторы 375 мс и длинный зал — пэд шире и глубже.',
      en: 'Low cut (room for the bass), 375 ms repeats and a long hall — the pad gets wider and deeper.',
    },
    chain: [
      { type: 'eq', highpass_hz: 120 },
      { type: 'delay', time_ms: 375, feedback: 0.35, lowpass_hz: 5000, wet: 0.25 },
      { type: 'reverb', decay_s: 2.5, predelay_ms: 20, lowpass_hz: 7000, wet: 0.2 },
    ],
  },
  {
    id: 'drums-kick-kit',
    stems: ['kick'],
    name: { ru: 'Бочка: набор', en: 'Kick: kit' },
    note: {
      ru: 'На дорожку «бочка» (RoFormer): удары бочки заменяются сэмплами набора The Open Source Drum Kit, сила удара выбирает сэмпл. Набор — fx_kit_install osdk.',
      en: 'On the "kick" stem (RoFormer): kick hits are replaced with The Open Source Drum Kit samples, hit strength picks the sample. Kit — fx_kit_install osdk.',
    },
    chain: [{ type: 'sampler', kit: 'osdk/kick', floor_db: -18, output_db: -2 }],
  },
  {
    id: 'drums-snare-kit',
    stems: ['snare'],
    name: { ru: 'Малый: набор', en: 'Snare: kit' },
    note: {
      ru: 'На дорожку «малый барабан» (RoFormer): удары малого — сэмплами набора; порог −20 дБ пропускает и тихие удары (гоулст-ноты); слышна протечка бочки/хэта — поднимите порог.',
      en: 'On the "snare" stem (RoFormer): snare hits with kit samples; the −20 dB floor keeps quiet ghost notes too; if kick/hat bleed is heard — raise the floor.',
    },
    chain: [{ type: 'sampler', kit: 'osdk/snare', floor_db: -20, output_db: -2 }],
  },
  {
    id: 'drums-toms-kit',
    stems: ['toms'],
    name: { ru: 'Тамы: набор по высоте', en: 'Toms: kit by pitch' },
    note: {
      ru: 'На дорожку «тамы» (RoFormer): удары делятся по высоте — высокие играет малый там, средние — средний, низкие — большой (The Open Source Drum Kit). Близкие по высоте удары (меньше ~2 полутонов) — один там. Набор — fx_kit_install osdk.',
      en: 'On the "toms" stem (RoFormer): hits are split by pitch — high ones go to the small tom, middle to the medium, low to the large (The Open Source Drum Kit). Hits within ~2 semitones are one tom. Kit — fx_kit_install osdk.',
    },
    chain: [{ type: 'sampler', kit: 'osdk/tom-small', kit_mid: 'osdk/tom-medium', kit_low: 'osdk/tom-large', floor_db: -18, output_db: -2 }],
  },
  {
    id: 'drums-hh-kit',
    stems: ['hh'],
    name: { ru: 'Хэт: набор', en: 'Hi-hat: kit' },
    note: {
      ru: 'На дорожку «хэт» (RoFormer): удары хэта — сэмплами The Open Source Drum Kit. Коротко звучащий удар — закрытый хэт, долго звучащий — полузакрытый; новый удар глушит предыдущий, как педаль. Громкость +4 дБ: хэт YuE тихий, на слух лучше заметнее (опыт #663). Набор — fx_kit_install osdk.',
      en: 'On the "hi-hat" stem (RoFormer): hi-hat hits with The Open Source Drum Kit samples. A short hit gets a closed hat, a long-ringing one a half-closed hat; each hit chokes the previous one like the pedal. Output +4 dB: the YuE hi-hat is quiet, a bit more presence sounds better. Kit — fx_kit_install osdk.',
    },
    chain: [{ type: 'sampler', kit: 'osdk/hh-closed', kit_open: 'osdk/hh-half', choke: 1, floor_db: -24, output_db: 4 }],
  },
  {
    id: 'drums-ride-kit',
    stems: ['ride'],
    name: { ru: 'Райд: набор', en: 'Ride: kit' },
    note: {
      ru: 'На дорожку «райд» (RoFormer): удары райда — сэмплами набора. Если в дорожке в основном протечка других барабанов, поднимите порог.',
      en: 'On the "ride" stem (RoFormer): ride hits with kit samples. If the stem is mostly bleed from other drums, raise the floor.',
    },
    chain: [{ type: 'sampler', kit: 'osdk/ride', floor_db: -18, output_db: -2 }],
  },
  {
    id: 'drums-crash-kit',
    stems: ['crash'],
    name: { ru: 'Крэш: набор', en: 'Crash: kit' },
    note: {
      ru: 'На дорожку «крэш» (RoFormer): удары тарелки — сэмплами набора, тарелка звенит до конца.',
      en: 'On the "crash" stem (RoFormer): crash hits with kit samples, the cymbal rings out.',
    },
    chain: [{ type: 'sampler', kit: 'osdk/crash', floor_db: -18, output_db: -2 }],
  },
  {
    id: 'bass-kit',
    stems: ['bass'],
    name: { ru: 'Бас: бас-гитара (набор)', en: 'Bass: bass guitar (kit)' },
    note: {
      ru: 'На дорожку «бас»: ноты баса играются сэмплами настоящей бас-гитары (Growlybass, Squier Jazz). Ритм — доли дорожки (2 ноты на долю — восьмые), высота — по басу, громкость и баланс низа/середины следуют за исходным басом. Сложный рисунок (слэп, быстрые пассажи) не повторит. Набор — fx_kit_install growlybass.',
      en: 'On the "bass" stem: the bass notes are played with real bass guitar samples (Growlybass, Squier Jazz). Rhythm follows the stem beats (2 notes per beat — eighths), pitch follows the bass, loudness and low/mid balance follow the original. Will not copy complex parts (slap, fast runs). Kit — fx_kit_install growlybass.',
    },
    chain: [{ type: 'bass', kit: 'growlybass/bass', division: 2, floor_db: -20 }],
  },
  {
    id: 'master-glue',
    name: { ru: 'Мастер: склейка', en: 'Master: glue' },
    note: {
      ru: 'На весь трек: чуть низа и воздуха, мягкий компрессор 2:1 с медленной атакой — микс плотнее, удары целы.',
      en: 'Whole track: a touch of lows and air, gentle 2:1 slow-attack compressor — a tighter mix with intact hits.',
    },
    chain: [
      { type: 'eq', highpass_hz: 30, bands: [{ freq_hz: 100, gain_db: 1.5, q: 0.7 }, { freq_hz: 10000, gain_db: 1.5, q: 0.7 }] },
      { type: 'comp', threshold_db: -12, ratio: 2, attack_ms: 30, release_ms: 200 },
    ],
  },

  // ---------- мастер (этап 6): на весь собранный микс, на воркере; limiter — громкость к цели по истинному пику ----------
  {
    id: 'master-stream', stems: ['master'],
    name: { ru: 'Мастер: стриминг −14 LUFS', en: 'Master: streaming −14 LUFS' },
    note: {
      ru: 'Лёгкая склейка шины и громкость −14 LUFS (как у стримингов), пики не выше −1 dBTP — без перегруза в mp3.',
      en: 'Light bus glue and −14 LUFS (streaming level), peaks at most −1 dBTP — no clipping after mp3.',
    },
    chain: [
      { type: 'glue', threshold_db: -18, ratio: 1.5, attack_ms: 30, release_ms: 300 },
      { type: 'limiter', ceiling_db: -1, target_lufs: -14 },
    ],
  },
  {
    id: 'master-loud', stems: ['master'],
    name: { ru: 'Мастер: громко −11 LUFS', en: 'Master: loud −11 LUFS' },
    note: {
      ru: 'Плотнее и громче: срез инфранизов, склейка 2:1, −11 LUFS, пики −1 dBTP. Ударам меньше места — для громкого рока.',
      en: 'Denser and louder: sub cut, 2:1 glue, −11 LUFS, peaks −1 dBTP. Less room for hits — for loud rock.',
    },
    chain: [
      { type: 'eq', highpass_hz: 30 },
      { type: 'glue', threshold_db: -20, ratio: 2, attack_ms: 10, release_ms: 200 },
      { type: 'limiter', ceiling_db: -1, target_lufs: -11 },
    ],
  },
  {
    id: 'master-soft-glue', stems: ['master'],
    name: { ru: 'Мастер: мягкая склейка', en: 'Master: soft glue' },
    note: {
      ru: 'Параллельная склейка (60 % сжатого): партии держатся вместе, громкость и пики не трогаются.',
      en: 'Parallel glue (60% compressed): parts sit together, loudness and peaks untouched.',
    },
    chain: [{ type: 'glue', threshold_db: -22, ratio: 1.5, attack_ms: 30, release_ms: 400, mix: 0.6 }],
  },

  // ---------- синты (этап 4): блок synth (ноты партии подставляет студия по аккордам трека) + эффекты ----------
  // пэды — на октаву выше гитар (C5…B5): в регистре гитар пэд сливается с ними и не слышен даже громким
  // (прослушивание «Gone» #683/#685 — «не слышу», #687 октавой выше — «отчётливо»)
  {
    id: 'synth-solina', stems: ['synth'], style: 'pad', octave: 1, place: { pan: 0, width: 1.5 },
    name: { ru: 'Струнный ансамбль (Solina)', en: 'String ensemble (Solina)' },
    note: { ru: 'Мягкие «струны» 70-х: три расстроенные пилы, медленная атака и густой ансамбль-хорус — холодная подушка под гитары (Joy Division, Молчат Дома).', en: '70s soft "strings": three detuned saws, slow attack and a thick ensemble chorus — a cold bed under guitars.' },
    chain: [
      { type: 'synth', osc1: 0, unison: 3, detune_cents: 14, cutoff_hz: 6000, attack_s: 0.4, decay_s: 0.3, sustain: 0.9, release_s: 1.0, output_db: 0 },
      { type: 'chorus', voices: 3, depth_ms: 4, rate_hz: 0.6, mix: 0.7 },
      { type: 'reverb', decay_s: 2.2, predelay_ms: 20, lowpass_hz: 6000, wet: 0.25 },
    ],
  },
  {
    id: 'synth-juno', stems: ['synth'], style: 'pad', octave: 1, place: { pan: 0, width: 1.5 },
    name: { ru: 'Пэд с хорусом (Juno)', en: 'Chorus pad (Juno)' },
    note: { ru: 'Пульс с суб-октавой через тёплый фильтр, медленно «дышащий» от LFO, и фирменный хорус — тёплый пэд 80-х.', en: 'Pulse with a sub-octave through a warm filter slowly breathing with the LFO, and the signature chorus — a warm 80s pad.' },
    chain: [
      { type: 'synth', osc1: 2, pwm: 0.4, osc2: 0, osc_mix: 0.3, sub: 0.3, cutoff_hz: 1500, resonance: 0.2, lfo_cutoff: 0.15, vib_rate: 0.3, attack_s: 0.6, decay_s: 0.5, sustain: 0.8, release_s: 1.2, output_db: 0 },
      { type: 'chorus', voices: 2, depth_ms: 3, rate_hz: 0.5, mix: 0.8 },
    ],
  },
  {
    id: 'synth-moog-bass', stems: ['synth'], style: 'pulse', octave: 0,
    name: { ru: 'Синт-бас (Moog)', en: 'Synth bass (Moog)' },
    note: { ru: 'Пила и квадрат на октаву ниже через резонансный фильтр с коротким «щелчком» огибающей — плотный пульсирующий бас восьмыми.', en: 'Saw plus a square an octave down through a resonant filter with a short envelope "pluck" — a tight eighth-note bass.' },
    chain: [
      { type: 'synth', osc1: 0, osc2: 1, osc2_semi: -12, osc_mix: 0.4, cutoff_hz: 500, resonance: 0.4, env_amount: 0.6, f_attack_s: 0.005, f_decay_s: 0.25, attack_s: 0.005, decay_s: 0.25, sustain: 0.6, release_s: 0.1, output_db: 0 },
    ],
  },
  {
    id: 'synth-moog-lead', stems: ['synth'], style: 'arp', octave: 1,
    name: { ru: 'Лид (Moog)', en: 'Lead (Moog)' },
    note: { ru: 'Две пилы с лёгкой расстройкой, резонанс и вибрато, ленточное эхо — арпеджио по аккордам поверх трека.', en: 'Two slightly detuned saws, resonance and vibrato, tape echo — an arpeggio over the chords.' },
    chain: [
      { type: 'synth', osc1: 0, unison: 2, detune_cents: 6, cutoff_hz: 2500, resonance: 0.35, env_amount: 0.4, f_decay_s: 0.3, vib_rate: 5, vib_cents: 8, attack_s: 0.01, decay_s: 0.2, sustain: 0.7, release_s: 0.2, output_db: 0 },
      { type: 'delay', time_ms: 340, feedback: 0.35, lowpass_hz: 4000, wet: 0.3 },
      { type: 'tape', wow: 0.15, flutter: 0.1, saturation: 0.2, lowpass_hz: 9000, hiss: 0 },
    ],
  },
  {
    id: 'synth-cs80-brass', stems: ['synth'], style: 'pad', octave: 1, place: { pan: 0, width: 1.5 },
    name: { ru: 'Медь (CS-80)', en: 'Brass (CS-80)' },
    note: { ru: 'Расстроенные пилы и фильтр, раскрывающийся на атаке, с медленным вибрато — «Blade Runner»-медь, торжественно и тревожно.', en: 'Detuned saws and a filter opening on the attack with slow vibrato — Blade Runner style brass, solemn and uneasy.' },
    chain: [
      { type: 'synth', osc1: 0, unison: 2, detune_cents: 10, cutoff_hz: 900, env_amount: 0.7, f_attack_s: 0.15, f_decay_s: 0.6, vib_rate: 5.5, vib_cents: 10, attack_s: 0.08, decay_s: 0.4, sustain: 0.8, release_s: 0.4, output_db: 0 },
      { type: 'chorus', voices: 2, depth_ms: 2, rate_hz: 0.3, mix: 0.4 },
      { type: 'reverb', decay_s: 2.8, predelay_ms: 30, lowpass_hz: 7000, wet: 0.3 },
    ],
  },
  {
    id: 'synth-farfisa', stems: ['synth'], style: 'pad', octave: 1, place: { pan: 0, width: 1.5 },
    name: { ru: 'Орган (Farfisa)', en: 'Organ (Farfisa)' },
    note: { ru: 'Квадрат с октавой сверху и быстрым вибрато, без атаки, через пружину — гаражный орган 60-х.', en: 'Square with an octave on top and fast vibrato, no attack, through a spring — a 60s garage organ.' },
    chain: [
      { type: 'synth', osc1: 1, osc2: 1, osc2_semi: 12, osc_mix: 0.35, cutoff_hz: 6000, vib_rate: 6, vib_cents: 12, attack_s: 0.005, decay_s: 0.05, sustain: 1, release_s: 0.06, output_db: 0 },
      { type: 'spring', decay_s: 1.6, tone: 0.6, wet: 0.25 },
    ],
  },
  {
    id: 'synth-vox-continental', stems: ['synth'], style: 'pad', octave: 1, place: { pan: 0, width: 1.5 },
    name: { ru: 'Орган (Vox Continental)', en: 'Organ (Vox Continental)' },
    note: { ru: 'Треугольник с квадратом на октаву выше, лёгкое вибрато и старая лента — тонкий «стеклянный» орган (The Doors, The Animals).', en: 'Triangle with a square an octave up, light vibrato and old tape — a thin "glassy" organ.' },
    chain: [
      { type: 'synth', osc1: 3, osc2: 1, osc2_semi: 12, osc_mix: 0.25, cutoff_hz: 5000, vib_rate: 6.5, vib_cents: 6, attack_s: 0.005, decay_s: 0.05, sustain: 1, release_s: 0.08, output_db: 0 },
      { type: 'tape', wow: 0.1, flutter: 0.15, saturation: 0.15, lowpass_hz: 10000, hiss: 0.1 },
      { type: 'spring', decay_s: 1.2, tone: 0.5, wet: 0.2 },
    ],
  },
  {
    id: 'synth-vltone', stems: ['synth'], style: 'arp', octave: 1,
    name: { ru: 'Игрушка (VL-Tone)', en: 'Toy (VL-Tone)' },
    note: { ru: 'Узкий пульс с коротким звуком через зажатую ленту с шипением — карманный калькулятор-синт («Da Da Da»), странно и по-детски.', en: 'A narrow pulse with short notes through squashed hissing tape — the pocket calculator synth, odd and childlike.' },
    chain: [
      { type: 'synth', osc1: 2, pwm: 0.25, cutoff_hz: 7000, attack_s: 0.002, decay_s: 0.15, sustain: 0.3, release_s: 0.1, output_db: 0 },
      { type: 'tape', wow: 0.2, flutter: 0.3, saturation: 0.5, lowpass_hz: 7000, hiss: 0.2 },
    ],
  },

  // ---------- перкуссия по сетке (этап 5): блок perc (удары подставляет студия по тактам трека) + эффекты ----------
  {
    id: 'perc-hat8', stems: ['perc'], place: { pan: 0.3, width: 1 }, pattern: 'eighths', swing: 0,
    name: { ru: 'Хэт восьмыми (808)', en: 'Hi-hat eighths (808)' },
    note: { ru: 'Сухой закрытый хэт драм-машины восьмыми, слабые доли тише — пульс поверх живых барабанов.', en: 'A dry closed drum-machine hat in eighths, weak beats softer — a pulse on top of the drums.' },
    chain: [{ type: 'perc', voice: 1, tone: 1, decay: 0.8, rel_db: -16 }],
  },
  {
    id: 'perc-hat16', stems: ['perc'], place: { pan: 0.3, width: 1 }, pattern: 'sixteenths', swing: 0.1,
    name: { ru: 'Хэт шестнадцатыми (808)', en: 'Hi-hat sixteenths (808)' },
    note: { ru: 'Частый хэт шестнадцатыми с лёгким свингом — движение в куплете, диско и пост-панк.', en: 'Busy sixteenth hat with a light swing — motion for verses, disco and post-punk.' },
    chain: [{ type: 'perc', voice: 1, tone: 1.1, decay: 0.6, rel_db: -18 }],
  },
  {
    id: 'perc-shaker16', stems: ['perc'], place: { pan: -0.3, width: 1 }, pattern: 'sixteenths', swing: 0.15,
    name: { ru: 'Шейкер шестнадцатыми', en: 'Shaker sixteenths' },
    note: { ru: 'Мягкий шейкер с подтянутым свингом и «живым» разбросом — воздух и шорох между ударами.', en: 'A soft shaker with swing and a human spread — air and rustle between the hits.' },
    chain: [{ type: 'perc', voice: 2, tone: 1, decay: 1, humanize_ms: 6, rel_db: -20 }],
  },
  {
    id: 'perc-tamb24', stems: ['perc'], place: { pan: -0.4, width: 1 }, pattern: 'backbeat', swing: 0,
    name: { ru: 'Бубен на 2 и 4', en: 'Tambourine on 2 and 4' },
    note: { ru: 'Бубен на слабые доли вместе с малым — классика соула и брит-попа, припев становится шире.', en: 'Tambourine on the backbeat with the snare — soul and britpop classic, widens a chorus.' },
    chain: [{ type: 'perc', voice: 6, tone: 1, decay: 1, humanize_ms: 4, rel_db: -16 }, { type: 'reverb', decay_s: 1.2, predelay_ms: 10, lowpass_hz: 8000, wet: 0.15 }],
  },
  {
    id: 'perc-clap24', stems: ['perc'], pattern: 'backbeat', swing: 0,
    name: { ru: 'Хлопки на 2 и 4 (909)', en: 'Claps on 2 and 4 (909)' },
    note: { ru: 'Хлопки драм-машины поверх малого с комнатой — танцевальный удар на слабую долю.', en: 'Drum-machine claps over the snare with a room — a dance backbeat.' },
    chain: [{ type: 'perc', voice: 3, tone: 1, decay: 1, rel_db: -14 }, { type: 'reverb', decay_s: 0.9, predelay_ms: 5, lowpass_hz: 7000, wet: 0.2 }],
  },
  {
    id: 'perc-cowbell4', stems: ['perc'], place: { pan: 0.2, width: 1 }, pattern: 'fours', swing: 0,
    name: { ru: 'Ковбелл четвертями', en: 'Cowbell quarters' },
    note: { ru: 'Ковбелл 808 на каждую долю — фанк и нью-вейв, звучит нарочито и смешно; тише — держит темп.', en: 'An 808 cowbell on every beat — funk and new wave, deliberately cheeky; quieter it keeps time.' },
    chain: [{ type: 'perc', voice: 4, tone: 1, decay: 0.8, rel_db: -20 }],
  },
  {
    id: 'perc-rim24', stems: ['perc'], pattern: 'backbeat', swing: 0,
    name: { ru: 'Римшот на 2 и 4', en: 'Rimshot on 2 and 4' },
    note: { ru: 'Сухой щелчок по ободу на слабые доли — для тихих куплетов и баллад вместо малого.', en: 'A dry rim click on the backbeat — for quiet verses and ballads instead of the snare.' },
    chain: [{ type: 'perc', voice: 5, tone: 1, decay: 1, rel_db: -16 }],
  },
  {
    id: 'perc-kick-double', stems: ['perc'], pattern: 'fours', swing: 0,
    name: { ru: 'Удвоение бочки (osdk)', en: 'Kick doubling (osdk)' },
    note: { ru: 'Живая бочка на каждую долю поверх трека — «четыре в пол»: плотнее низ, танцевальнее припев.', en: 'A real kick on every beat on top of the track — four on the floor: denser low end, a dancier chorus.' },
    chain: [{ type: 'perc', voice: 0, kit: 'osdk/kick', rel_db: -12 }],
  },
  {
    id: 'perc-ride8', stems: ['perc'], place: { pan: 0.35, width: 1 }, pattern: 'eighths', swing: 0,
    name: { ru: 'Райд восьмыми (osdk)', en: 'Ride eighths (osdk)' },
    note: { ru: 'Живая тарелка райд восьмыми — открывает припев и бридж, звенит дольше хэта.', en: 'A real ride cymbal in eighths — opens up a chorus or bridge, rings longer than a hat.' },
    chain: [{ type: 'perc', voice: 0, kit: 'osdk/ride', rel_db: -18 }],
  },
]
