// Группы стилей для двухуровневого комбобокса «группа → стиль».
// Фокус: нейромузыка хорошо делает клише и нейтральные жанры —
// электроника, эмбиент, мантры, регги, блюз, этника, кино.
// Откалиброванные пресеты переиспользуются из presets.js по id.
import { presets } from './presets.js'

const byId = (id) => presets.find((p) => p.id === id)

// Общие слоты для инструментальных жанров: вокал не нужен.
const instr = (over = {}) => ({
  language: 'English',
  vocals: 'instrumental, no vocals',
  ...over,
})

export const groups = [
  {
    id: 'chillout', name: 'Электроника · чилаут',
    items: [
      byId('chillout-bg'),
      {
        id: 'trip-hop', name: 'Трип-хоп (Portishead-вайб, без цитаты)',
        style: 'Instrumental, no vocals, trip-hop, slow heavy hip-hop breakbeat, moody bassline, dusty vinyl samples, theremin-like synth wails, noir atmosphere, melancholy paranoia, 88 BPM',
        slots: instr({ genre: 'trip-hop', rhythm: 'slow heavy breakbeat, head-nodding', keys: 'moody bassline, dusty samples, theremin-like synth', mood: 'noir melancholy, paranoid', production: 'vinyl hiss, dark wide mix' }), bpm: 88, lyrics: '[Instrumental]',
      },
      {
        id: 'balearic', name: 'Балеарик / солнечное утро',
        style: 'Instrumental, no vocals, balearic downtempo, gentle tropical percussion, nylon guitar licks, warm analog synth pad, marimba, ocean-wide reverb, sunny serenity, 96 BPM',
        slots: instr({ genre: 'balearic, downtempo', rhythm: 'gentle shuffling groove', guitars: 'nylon guitar licks', keys: 'warm analog synth pad, marimba', mood: 'sunny serenity, seaside morning', production: 'ocean-wide reverb, smooth' }), bpm: 96, lyrics: '[Instrumental]',
      },
      {
        id: 'lounge-chill', name: 'Лаунж-чилл (кафе/фон)',
        style: 'Instrumental, no vocals, smooth lounge chillout, brushed drums, upright walking bass, soft electric piano chords, subtle vibraphone, relaxed coffee-house vibe, polite and warm, 90 BPM',
        slots: instr({ genre: 'lounge, smooth jazz', rhythm: 'brushed drums, relaxed swing', keys: 'soft electric piano, vibraphone', mood: 'relaxed, cozy, unobtrusive', production: 'warm close-mic, smooth' }), bpm: 90, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'ambient', name: 'Эмбиент / медитация',
    items: [
      byId('ambient-relax'),
      {
        id: 'space-ambient', name: 'Космический эмбиент',
        style: 'Instrumental, no vocals, space ambient, no percussion, deep sub drones, shimmering high harmonics, slow chord clouds, NASA radio chatter feel far away, immense scale, weightless floating, 50 BPM',
        slots: instr({ genre: 'space ambient, drone', rhythm: 'no percussion, drifting', keys: 'sub drones, shimmering pads', mood: 'weightless, immense cosmic calm', production: 'huge stereo field, endless reverb' }), bpm: 50, lyrics: '[Instrumental]',
      },
      {
        id: 'dark-ambient', name: 'Тёмный эмбиент',
        style: 'Instrumental, no vocals, dark ambient, no beat, low cavernous drones, metallic resonances, distant industrial echoes, unsettling tension, abyssal darkness, 45 BPM',
        slots: instr({ genre: 'dark ambient', rhythm: 'no beat, pulseless drift', keys: 'low drones, metallic resonances', mood: 'unease, abyssal darkness', production: 'cavernous, isolated' }), bpm: 45, lyrics: '[Instrumental]',
      },
      {
        id: 'nature-newage', name: 'Нью-эйдж / природа',
        style: 'Instrumental, no vocals, new age, harp arpeggios, soft flute melody, gentle string pad, brook-and-birds atmosphere, healing spa serenity, 60 BPM',
        slots: instr({ genre: 'new age', rhythm: 'free-flowing arpeggios', keys: 'harp, soft flute, string pad', mood: 'healing, serene, fresh morning', production: 'airy, gentle reverb' }), bpm: 60, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'lofi', name: 'Lo-fi / биты',
    items: [
      byId('lofi-beats'),
      {
        id: 'jazzhop', name: 'Джаз-хоп',
        style: 'Instrumental, no vocals, jazz-hop, swung boom bap drums, walking upright bass, muted trumpet phrases, Rhodes chords, smoky club atmosphere, laid-back cool, 84 BPM',
        slots: instr({ genre: 'jazz-hop, jazzy beats', rhythm: 'swung boom bap', keys: 'Rhodes, muted trumpet, upright bass', mood: 'smoky laid-back cool', production: 'warm dusty mix' }), bpm: 84, lyrics: '[Instrumental]',
      },
      {
        id: 'chillhop-guitar', name: 'Чилл-хоп с гитарой',
        style: 'Instrumental, no vocals, chillhop, soft head-nod beat, mellow electric guitar licks with reverb, warm sub bass, rain-sample texture, cozy evening calm, 80 BPM',
        slots: instr({ genre: 'chillhop', rhythm: 'soft head-nod beat', guitars: 'mellow electric guitar licks', keys: 'warm sub bass', mood: 'cozy evening calm', production: 'rainy texture, warm' }), bpm: 80, lyrics: '[Instrumental]',
      },
      {
        id: 'sleepy-tape', name: 'Сонная плёнка / sleepy',
        style: 'Instrumental, no vocals, sleepy lo-fi tape music, barely-there beat, detuned music box and toy piano, heavy tape wobble, under-water feeling, drowsy half-dream, 66 BPM',
        slots: instr({ genre: 'lo-fi, tape music', rhythm: 'barely-there slow beat', keys: 'detuned music box, toy piano', mood: 'drowsy half-dream', production: 'heavy tape wobble, underwater' }), bpm: 66, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'techno', name: 'Техно',
    items: [
      byId('dark-techno'),
      {
        id: 'melodic-techno', name: 'Мелодичное техно',
        style: 'Instrumental, no vocals, melodic techno, driving four-on-the-floor, hypnotic arpeggio bassline, cinematic pad swells, build-ups and drops, euphoric dark energy, 124 BPM',
        slots: instr({ genre: 'melodic techno', rhythm: 'four-on-the-floor, driving', keys: 'arpeggio bass, cinematic pads', mood: 'euphoric dark energy, hypnotic', production: 'tight modern club mix' }), bpm: 124, lyrics: '[Instrumental]',
      },
      {
        id: 'minimal-techno', name: 'Минимал',
        style: 'Instrumental, no vocals, minimal techno, sparse clicking percussion, one dry bassline loop, tiny blips and glitches, tons of space, clinical precision, 128 BPM',
        slots: instr({ genre: 'minimal techno', rhythm: 'sparse exact groove', keys: 'dry bass loop, glitch blips', mood: 'clinical, hypnotic restraint', production: 'dry, wide space' }), bpm: 128, lyrics: '[Instrumental]',
      },
      {
        id: 'acid', name: 'Эсид',
        style: 'Instrumental, no vocals, acid techno, squelchy 303 acid bassline, relentless straight kick, sharp hi-hats, tweaky filter sweeps, sweaty warehouse trance, 132 BPM',
        slots: instr({ genre: 'acid techno', rhythm: 'relentless straight kick', keys: 'squelchy 303 acid line', mood: 'sweaty hypnotic intensity', production: 'raw analog' }), bpm: 132, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'house', name: 'Хаус',
    items: [
      {
        id: 'deep-house', name: 'Дип-хаус',
        style: 'Instrumental, no vocals, deep house, warm bubbling bassline, four-on-the-floor with soft clap, jazzy chords through filter, late-night groove, sophisticated ease, 120 BPM',
        slots: instr({ genre: 'deep house', rhythm: 'four-on-the-floor, groovy', keys: 'bubbling bass, jazzy filtered chords', mood: 'late-night sophisticated ease', production: 'deep warm club mix' }), bpm: 120, lyrics: '[Instrumental]',
      },
      {
        id: 'funky-house', name: 'Фанки-хаус / диско-хаус',
        style: 'Instrumental, no vocals, funky disco house, chopped disco guitar loops, bouncing bass, filter sweeps, hands-in-the-air piano stabs, party euphoria, 124 BPM',
        slots: instr({ genre: 'funky house, disco house', rhythm: 'bouncing four-on-the-floor', guitars: 'chopped disco guitar loops', keys: 'piano stabs', mood: 'party euphoria, glossy fun', production: 'punchy bright club' }), bpm: 124, lyrics: '[Instrumental]',
      },
      {
        id: 'prog-house', name: 'Прогрессив-хаус',
        style: 'Instrumental, no vocals, progressive house, long smooth build-ups, layered synth arpeggios, wide atmospherics, emotional breakdown into drop, uplifting journey, 126 BPM',
        slots: instr({ genre: 'progressive house', rhythm: 'driving four-on-the-floor', keys: 'layered arpeggios, wide pads', mood: 'uplifting journey, emotional', production: 'wide cinematic club' }), bpm: 126, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'trance', name: 'Транс',
    items: [
      {
        id: 'uplifting', name: 'Аплифтинг-транс',
        style: 'Instrumental, no vocals, uplifting trance, fast driving beat, supersaw leads, epic breakdown with plucked melody, big euphoric drop, hands-up euphoria, 138 BPM',
        slots: instr({ genre: 'uplifting trance', rhythm: 'fast driving four-on-the-floor', keys: 'supersaw leads, plucked melody', mood: 'euphoric, epic, radiant', production: 'huge bright festival' }), bpm: 138, lyrics: '[Instrumental]',
      },
      {
        id: 'psytrance', name: 'Пси-транс',
        style: 'Instrumental, no vocals, psychedelic trance, rolling bassline, zapping alien synths, dense layered percussion, hypnotic acid squelches, trippy fractal energy, 145 BPM',
        slots: instr({ genre: 'psytrance', rhythm: 'rolling fast beat', keys: 'zapping synths, acid squelches', mood: 'trippy hypnotic intensity', production: 'dense psychedelic' }), bpm: 145, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'dnb', name: 'Drum\'n\'bass / джангл',
    items: [
      {
        id: 'liquid-dnb', name: 'Ликвид DnB',
        style: 'Instrumental, no vocals, liquid drum and bass, rolling breakbeats, warm sub bass, lush pad chords, soulful vocal-chop echoes (no words), sunrise euphoria, 174 BPM',
        slots: instr({ genre: 'liquid drum and bass', rhythm: 'rolling breakbeats', keys: 'warm sub bass, lush pads', mood: 'sunrise euphoria, soulful', production: 'polished warm' }), bpm: 174, lyrics: '[Instrumental]',
      },
      {
        id: 'jungle', name: 'Джангл (рэгги-сэмплы)',
        style: 'Instrumental, no vocals, jungle, chopped amen breaks, deep dub bassline, reggae skank chords, sirens and dub delays, raw rude-bwoy energy, 165 BPM',
        slots: instr({ genre: 'jungle', rhythm: 'chopped amen breaks', keys: 'dub bass, reggae skank chords', mood: 'raw energetic rave', production: 'dub delays, raw' }), bpm: 165, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'breaks', name: 'Брейки / big beat',
    items: [
      byId('bigbeat-guitars'),
      {
        id: 'electro-breaks', name: 'Электро-брейки',
        style: 'Instrumental, no vocals, electro breaks, funky syncopated beat, robotic talkbox bass, 808 claps, scratching, retro-futuristic b-boy energy, 128 BPM',
        slots: instr({ genre: 'electro, breaks', rhythm: 'syncopated funky breaks', keys: 'talkbox bass, 808 claps', mood: 'retro-futuristic swagger', production: 'punchy analog' }), bpm: 128, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'synthwave', name: 'Синтвейв / ретро',
    items: [
      {
        id: 'outrun', name: 'Аутран',
        style: 'Instrumental, no vocals, synthwave outrun, driving 80s beat, arpeggiated analog bass, gated reverb drums, neon sax or lead synth, sunset highway nostalgia, 110 BPM',
        slots: instr({ genre: 'synthwave, outrun', rhythm: 'driving 80s beat, gated reverb snare', keys: 'arpeggio analog bass, neon lead synth', mood: 'neon nostalgia, sunset highway', production: '1985 analog sheen' }), bpm: 110, lyrics: '[Instrumental]',
      },
      {
        id: 'darksynth', name: 'Дарк-синт (хоррор 80-х)',
        style: 'Instrumental, no vocals, darksynth, menacing slow pounding beat, distorted bass pulse, horror-movie staccato strings synth, cold fear, midnight slasher chase, 100 BPM',
        slots: instr({ genre: 'darksynth, horror synth', rhythm: 'pounding slow beat', keys: 'distorted bass, staccato string synth', mood: 'cold fear, menacing', production: 'dark analog grit' }), bpm: 100, lyrics: '[Instrumental]',
      },
      {
        id: 'vaporwave', name: 'Вейпорвейв',
        style: 'Instrumental, no vocals, vaporwave, slowed chopped easy-listening samples, pitch-dropped sax and muzak piano, tape slow-downs, ironic nostalgic haze, 70 BPM',
        slots: instr({ genre: 'vaporwave', rhythm: 'slowed loose beat', keys: 'pitch-dropped sax, muzak piano', mood: 'ironic nostalgic haze', production: 'tape slow-down, lo-fi sheen' }), bpm: 70, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'chiptune', name: 'Чиптюн / 8-бит',
    items: [
      {
        id: 'chiptune', name: 'Чиптюн',
        style: 'Instrumental, no vocals, chiptune, 8-bit NES square-wave lead melodies, Game Boy chiptune arpeggios, 8-bit noise-channel drums, playful retro video game energy, 150 BPM',
        slots: instr({ genre: 'chiptune', rhythm: '8-bit noise-channel drums', keys: '8-bit NES square-wave lead melodies, Game Boy chiptune arpeggios', mood: 'playful retro video game energy', production: 'lo-fi 8-bit bitcrushed chip sound' }), bpm: 150, lyrics: '[Instrumental]',
      },
      {
        id: 'nintendo-punk', name: 'Нинтендо-панк (в духе Bondage Fairies)',
        style: 'English, chiptune punk, electropunk, 8-bit NES square-wave lead melodies, aggressive down-tuned distorted guitar riffs, power chords, fast punk energy, shouted male vocals, raw snotty fun, 170 BPM',
        slots: { language: 'English', genre: 'chiptune punk, electropunk', rhythm: 'fast punk energy', guitars: 'aggressive down-tuned distorted guitar riffs, power chords', keys: '8-bit NES square-wave lead melodies', vocals: 'shouted male', mood: 'raw snotty fun', production: 'raw loud' }, bpm: 170,
      },
      {
        id: 'nintendocore', name: 'Нинтендокор (чиптюн + метал)',
        style: 'English, nintendocore, metalcore riffs with 8-bit Game Boy arpeggios, double kick blast drums, palm-muted chug, screamed and clean male vocals, frantic video game boss fight energy, 180 BPM',
        slots: { language: 'English', genre: 'nintendocore', rhythm: 'double kick blast drums', guitars: 'power chords, palm-muted chug', keys: 'Game Boy chiptune arpeggios', vocals: 'screamed and clean male', mood: 'frantic boss fight energy', production: 'modern loud' }, bpm: 180,
      },
    ],
  },
  {
    id: 'industrial', name: 'Индастриал / EBM',
    items: [
      {
        id: 'ebm', name: 'EBM',
        style: 'Instrumental, no vocals, EBM electronic body music, pumping industrial bass sequencer, metallic percussion, hard steady beat, cold machine discipline, 130 BPM',
        slots: instr({ genre: 'EBM, industrial', rhythm: 'hard steady machine beat', keys: 'pumping sequencer bass', mood: 'cold machine discipline, marching', production: 'metallic, punchy' }), bpm: 130, lyrics: '[Instrumental]',
      },
      {
        id: 'dark-electro', name: 'Тёмное электро',
        style: 'Instrumental, no vocals, dark electro, distorted beat, gritty bass growls, ominous bell tones, gothic night factory mood, 125 BPM',
        slots: instr({ genre: 'dark electro', rhythm: 'distorted steady beat', keys: 'gritty bass growls, bells', mood: 'gothic ominous', production: 'gritty dark' }), bpm: 125, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'reggae', name: 'Регги / даб',
    items: [
      byId('marley'),
      {
        id: 'dub', name: 'Даб',
        style: 'Instrumental, no vocals, roots dub, heavy one-drop bass and drums, spring reverb explosions, tape delay echoes, skanking guitar offbeats, sparse melodica, deep bass meditation, 75 BPM',
        slots: instr({ genre: 'dub', rhythm: 'one-drop, heavy and slow', guitars: 'skanking offbeat chords', keys: 'melodica, tape delay echoes', mood: 'deep meditative groove', production: 'spring reverb, tape delay throws' }), bpm: 75, lyrics: '[Instrumental]',
      },
      {
        id: 'dancehall', name: 'Дэнсхолл',
        style: 'Jamaican patois, dancehall, riddim bounce, punchy synth stabs, energetic toasting male vocals, party crowd energy, 102 BPM',
        slots: { language: 'Jamaican patois', genre: 'dancehall', rhythm: 'bouncing riddim', guitars: '', keys: 'punchy synth stabs', vocals: 'energetic male toasting, rhythmic', mood: 'party crowd energy', production: 'punchy modern' }, bpm: 102,
      },
      {
        id: 'lovers-rock', name: 'Лаверс-рок (нежное регги)',
        style: 'English, lovers rock reggae, smooth one-drop groove, silky soul female vocals, romantic, warm horns, sweet and mellow, 78 BPM',
        slots: { language: 'English', genre: 'lovers rock, reggae soul', rhythm: 'one-drop, mellow', guitars: 'soft skank', keys: 'warm Rhodes, sweet horns', vocals: 'silky soul female, tender', mood: 'romantic, sweet', production: 'warm analog' }, bpm: 78,
      },
    ],
  },
  {
    id: 'blues', name: 'Блюз',
    items: [
      {
        id: 'chicago-blues', name: 'Чикаго-блюз (электро)',
        style: 'English, Chicago electric blues, shuffle groove, gritty tube guitar solos, harmonica wails, walking bass, barroom piano, raspy male vocals, whiskey and smoke, 96 BPM',
        slots: { language: 'English', genre: 'Chicago blues', rhythm: 'blues shuffle', guitars: 'gritty tube electric solos', keys: 'barroom piano, harmonica', vocals: 'raspy male, worn', mood: 'whiskey melancholy, smoky', production: 'live club, tube warmth' }, bpm: 96,
      },
      {
        id: 'delta-blues', name: 'Дельта-блюз (акустика)',
        style: 'English, delta blues, fingerpicked acoustic slide guitar, stomping foot and harmonica, gravel male voice, 1930s porch recording, lonely road story, 78 BPM',
        slots: { language: 'English', genre: 'delta blues, country blues', rhythm: 'stomping foot, free', guitars: 'acoustic slide, fingerpicked', keys: 'harmonica', vocals: 'gravel male, raw', mood: 'lonely road, haunted', production: '1930s mono field recording' }, bpm: 78,
      },
      {
        id: 'slow-blues', name: 'Слоу-блюз (полночный)',
        style: 'English, slow blues 12/8, mournful bends guitar, sparse piano chords, brushed drums, world-weary male vocals, last-call bar sadness, 58 BPM',
        slots: { language: 'English', genre: 'slow blues', rhythm: '12/8 slow drag', guitars: 'mournful slow bends', keys: 'sparse piano', vocals: 'world-weary male', mood: 'last-call sadness', production: 'smoky close' }, bpm: 58,
      },
      {
        id: 'blues-rock', name: 'Блюз-рок',
        style: 'English, blues rock, driving shuffle-rock beat, searing blues guitar leads, Hammond organ, powerful gritty male vocals, swagger, 118 BPM',
        slots: { language: 'English', genre: 'blues rock', rhythm: 'driving shuffle rock', guitars: 'searing blues leads', keys: 'Hammond organ', vocals: 'powerful gritty male', mood: 'swagger, confident', production: 'loud live band' }, bpm: 118,
      },
    ],
  },
  {
    id: 'mantra', name: 'Мантры / Индия',
    items: [
      byId('india-mantra'),
      {
        id: 'kirtan', name: 'Киртан (групповое пение)',
        style: 'Sanskrit, kirtan devotional chant, call-and-response group vocals, harmonium drone, tabla and kartal hand claps, joyful communal worship, rising ecstatic repetition, 100 BPM',
        slots: { language: 'Sanskrit', genre: 'kirtan, devotional', rhythm: 'tabla and kartal, building', guitars: '', keys: 'harmonium drone', vocals: 'call-and-response group, joyful', mood: 'ecstatic communal joy', production: 'live temple room' }, bpm: 100,
      },
      {
        id: 'sitar-raga', name: 'Ситар-рага (инструментал)',
        style: 'Instrumental, no vocals, indian classical raga, sitar alaps and fast meend runs, tanpura drone, tabla accelerating tala, exotic microtonal beauty, hypnotic, 90 BPM',
        slots: instr({ genre: 'indian classical, raga', rhythm: 'tabla tala, accelerating', keys: 'sitar, tanpura drone', mood: 'hypnotic exotic serenity', production: 'intimate live room' }), bpm: 90, lyrics: '[Instrumental]',
      },
      {
        id: 'bollywood-retro', name: 'Болливуд-ретро',
        style: 'Hindi, retro Bollywood song, playful filmi orchestra, dholak groove, sweet soaring female vocals with male answer, strings and flute flourishes, theatrical romance, 105 BPM',
        slots: { language: 'Hindi', genre: 'filmi, Bollywood retro', rhythm: 'dholak dance groove', guitars: '', keys: 'film orchestra strings, flute', vocals: 'sweet soaring female and male duet', mood: 'theatrical romance, playful', production: 'vintage film orchestra' }, bpm: 105,
      },
    ],
  },
  {
    id: 'oriental', name: 'Восток / этника',
    items: [
      byId('ethnic-oriental'),
      {
        id: 'turkish-psych', name: 'Турецкий психоделик-фолк',
        style: 'Turkish, anatolian psychedelic folk, saz electric baglama riffs, funky groove, hypnotic east modes, passionate male vocals, vintage 70s fuzz, 110 BPM',
        slots: { language: 'Turkish', genre: 'anatolian psych folk', rhythm: 'funky hypnotic groove', guitars: 'electric saz fuzz riffs', keys: '', vocals: 'passionate male, melismatic', mood: 'hypnotic fiery', production: '70s vintage fuzz' }, bpm: 110,
      },
      {
        id: 'arabesque', name: 'Арабеска (драматичная)',
        style: 'Instrumental, no vocals, arabesque orchestral, violin and qanun unison bends, darbuka groove, dramatic minor modes, oud solos, bazaar-to-moonlight romance, 100 BPM',
        slots: instr({ genre: 'arabesque, middle-eastern', rhythm: 'darbuka groove', guitars: 'oud solos', keys: 'violin, qanun', mood: 'dramatic romantic longing', production: 'orchestral warm' }), bpm: 100, lyrics: '[Instrumental]',
      },
      {
        id: 'shamanic', name: 'Шаманские бубны',
        style: 'Instrumental, no vocals, shamanic drumming, frame drum heart-beat pulse, rattle and throat-hum overtones, ceremonial fire circle, trance possession rhythm, 130 BPM',
        slots: instr({ genre: 'shamanic, ritual', rhythm: 'frame drum trance pulse', guitars: '', keys: 'rattles, throat overtones', mood: 'ceremonial trance, primal', production: 'raw live circle' }), bpm: 130, lyrics: '[Instrumental]',
      },
      {
        id: 'celtic', name: 'Кельтика',
        style: 'Instrumental, no vocals, celtic folk, tin whistle and fiddle dance melody, acoustic guitar strum, bodhran pulse, tavern cheer to misty hills, 115 BPM',
        slots: instr({ genre: 'celtic folk', rhythm: 'bodhran dance pulse', guitars: 'acoustic strum', keys: 'tin whistle, fiddle', mood: 'tavern cheer, misty nostalgia', production: 'warm live' }), bpm: 115, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'jazz', name: 'Джаз / босса',
    items: [
      {
        id: 'smooth-jazz', name: 'Смус-джаз',
        style: 'Instrumental, no vocals, smooth jazz, silky saxophone lead, funky-lite drum groove, Rhodes comping, fretless bass, city-night elegance, 95 BPM',
        slots: instr({ genre: 'smooth jazz', rhythm: 'lite funk groove', guitars: 'clean jazz comping', keys: 'Rhodes, sax lead, fretless bass', mood: 'city-night elegance', production: 'silky polished' }), bpm: 95, lyrics: '[Instrumental]',
      },
      {
        id: 'noir-jazz', name: 'Нуар-джаз',
        style: 'Instrumental, no vocals, jazz noir, sparse smoky trio, walking bass, brushed snare, lonely muted trumpet, dark alley atmosphere, rain and neon reflections, 76 BPM',
        slots: instr({ genre: 'jazz noir', rhythm: 'slow walking swing', guitars: '', keys: 'muted trumpet, piano', mood: 'dark rain-soaked loneliness', production: 'smoky close-mic' }), bpm: 76, lyrics: '[Instrumental]',
      },
      {
        id: 'bossa', name: 'Босса-нова',
        style: 'Portuguese, bossa nova, nylon guitar bossa pattern, soft shaker, breathy female vocals, gentle trumpet, seaside evening romance, 80 BPM',
        slots: { language: 'Portuguese', genre: 'bossa nova', rhythm: 'bossa guitar pattern, shaker', guitars: 'nylon bossa comping', keys: 'gentle trumpet', vocals: 'breathy female, intimate', mood: 'seaside evening romance', production: 'intimate warm' }, bpm: 80,
      },
      {
        id: 'swing-bigband', name: 'Свинг / биг-бэнд',
        style: 'English, big band swing, punchy brass sections, walking bass, ride cymbal swing, joyful shout vocals with band answers, dancehall 1940s celebration, 140 BPM',
        slots: { language: 'English', genre: 'big band swing', rhythm: 'fast swing, ride cymbal', guitars: 'rhythm guitar chunk', keys: 'brass sections, piano', vocals: 'joyful male shout with band answers', mood: '1940s celebration', production: 'vintage ballroom' }, bpm: 140,
      },
    ],
  },
  {
    id: 'cinematic', name: 'Кино-саундтрек / неоклассика',
    items: [
      byId('romantic-theme'),
      {
        id: 'epic', name: 'Эпика (трейлерная)',
        style: 'Instrumental, no vocals, epic cinematic trailer, huge taiko drums and ostinato strings, brass fanfare, choir swells, rising tension into heroic climax, 90 BPM',
        slots: instr({ genre: 'epic orchestral, trailer music', rhythm: 'taiko ostinato, driving', guitars: '', keys: 'string ostinato, brass, choir', mood: 'heroic rising grandeur', production: 'huge cinematic mix' }), bpm: 90, lyrics: '[Instrumental]',
      },
      {
        id: 'noir-score', name: 'Нуар-саундтрек',
        style: 'Instrumental, no vocals, film noir score, lonely sax over dark strings, slow jazz-noir drum shadows, muted trumpet lament, crime-city 2am, 72 BPM',
        slots: instr({ genre: 'noir score, jazz', rhythm: 'slow shadowed beat', guitars: '', keys: 'dark strings, lonely sax, muted trumpet', mood: 'crime-city 2am fatalism', production: 'dark analog film mix' }), bpm: 72, lyrics: '[Instrumental]',
      },
      {
        id: 'neoclassical', name: 'Неоклассика (пиано)',
        style: 'Instrumental, no vocals, neoclassical piano, intimate felt piano melody, subtle string swells, pedaled resonances, bittersweet introspection, 70 BPM',
        slots: instr({ genre: 'neoclassical, modern classical', rhythm: 'rubato, breathing', guitars: '', keys: 'felt piano, subtle strings', mood: 'bittersweet introspection', production: 'intimate close piano' }), bpm: 70, lyrics: '[Instrumental]',
      },
      {
        id: 'western', name: 'Вестерн (пустыня)',
        style: 'Instrumental, no vocals, spaghetti western, twanging reverb guitar, whistling melody, mariachi trumpets, galloping rhythm, dusty showdown tension, 95 BPM',
        slots: instr({ genre: 'spaghetti western', rhythm: 'galloping', guitars: 'twang reverb lead', keys: 'mariachi trumpet, whistle', mood: 'dusty showdown tension', production: '60s cinema analog' }), bpm: 95, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'pop', name: 'Поп / диско',
    items: [
      byId('ruki-vverh'),
      {
        id: 'synthpop', name: 'Синт-поп 80-х',
        style: 'English, 80s synth-pop, programmed Linn drums, warm analog synth bass, chorus-drenched guitar sparkles, catchy bittersweet chorus, neon romance, 118 BPM',
        slots: { language: 'English', genre: 'synth-pop, new wave', rhythm: 'programmed 80s beat', guitars: 'chorus sparkle arpeggios', keys: 'analog synth bass, brass stabs', vocals: 'smooth male or female, melodic', mood: 'neon bittersweet romance', production: 'gated reverb 1985' }, bpm: 118,
      },
      {
        id: 'disco-70', name: 'Диско 70-х',
        style: 'English, 70s disco, four-on-the-floor kick, funky bass, string section riffs, wah guitar, diva female vocals, mirror-ball euphoria, 120 BPM',
        slots: { language: 'English', genre: 'disco, funk', rhythm: 'four-on-the-floor disco', guitars: 'wah funk rhythm', keys: 'string section, electric piano', vocals: 'powerful diva female', mood: 'mirror-ball euphoria', production: 'lush 1977 orchestra' }, bpm: 120,
      },
      {
        id: 'citypop', name: 'Сити-поп',
        style: 'Japanese, 80s city pop, smooth funky groove, shimmering electric piano, glassy guitar solo, sweet female vocals, night-drive Tokyo glamour, 104 BPM',
        slots: { language: 'Japanese', genre: 'city pop, J-pop funk', rhythm: 'smooth funky groove', guitars: 'glassy clean solos', keys: 'electric piano, synth brass', vocals: 'sweet clear female', mood: 'night-drive glamour', production: 'polished 1983 sheen' }, bpm: 104,
      },
    ],
  },
  {
    id: 'rock', name: 'Рок',
    items: [
      {
        id: 'classic-rock', name: 'Классический рок 70-х',
        style: 'English, classic 1970s rock, bluesy guitar riffs, powerful live drums, Hammond organ, raspy male vocals, anthemic sing-along chorus, vintage analog production, 120 BPM',
        slots: { language: 'English', genre: 'classic 1970s rock', rhythm: 'powerful live drums, steady', guitars: 'bluesy riffs', keys: 'Hammond organ', vocals: 'raspy male, anthemic', mood: 'confident, celebratory', production: 'vintage analog' }, bpm: 120,
      },
      {
        id: 'grunge', name: 'Гранж 90-х',
        style: 'English, 1990s grunge, detuned distorted guitars, loud-quiet-loud dynamics, heavy fuzz bass, apathetic angsty male vocals, raw unpolished production, 110 BPM',
        slots: { language: 'English', genre: 'grunge', rhythm: 'loud-quiet dynamics, plodding to explosive', guitars: 'detuned distorted, thick fuzz', keys: '', vocals: 'apathetic angsty male, cracked', mood: 'angst, resignation', production: 'raw unpolished 1991' }, bpm: 110,
      },
      {
        id: 'grunge-instr', name: 'Гранж без слов',
        style: 'Instrumental, no vocals, instrumental grunge, detuned distorted guitar riffs carrying the melody, loud-quiet-loud dynamics, heavy fuzz bass, raw unpolished production, 108 BPM',
        slots: instr({ genre: 'grunge, instrumental rock', rhythm: 'loud-quiet dynamics', guitars: 'detuned distorted riffs, melodic leads', keys: '', mood: 'angst, brooding', production: 'raw unpolished' }), bpm: 108, lyrics: '[Instrumental]',
      },
      {
        id: 'indie-rock', name: 'Инди-рок',
        style: 'English, indie rock, jangly clean guitars, driving upbeat drums, earnest youthful male vocals, bittersweet bright mood, 135 BPM',
        slots: { language: 'English', genre: 'indie rock', rhythm: 'driving upbeat', guitars: 'jangly clean arpeggios', keys: '', vocals: 'earnest youthful male', mood: 'bittersweet bright', production: 'clean modern indie' }, bpm: 135,
      },
      {
        id: 'garage-rock', name: 'Гараж-рок',
        style: 'English, garage rock, raw crunchy guitars, simple fast energetic beat, shouted sloppy vocals, two-minute song energy, 145 BPM',
        slots: { language: 'English', genre: 'garage rock', rhythm: 'simple fast energetic', guitars: 'raw crunchy chords', keys: 'cheap organ stabs', vocals: 'shouted, sloppy, urgent', mood: 'reckless energy', production: 'raw garage' }, bpm: 145,
      },
      {
        id: 'surf', name: 'Сёрф (без слов)',
        style: 'Instrumental, no vocals, 1960s surf rock, twangy wet-reverb guitar lead, driving beat, hand claps, sunny beach chase energy, 150 BPM',
        slots: instr({ genre: 'surf rock', rhythm: 'driving dance beat', guitars: 'twangy wet-reverb lead', keys: 'sax bursts', mood: 'sunny beach fun', production: '1960s vintage' }), bpm: 150, lyrics: '[Instrumental]',
      },
      {
        id: 'prog-rock', name: 'Прог-рок',
        style: 'English, progressive rock, shifting time signatures, Mellotron flutes, long instrumental passages, dynamic quiet-loud shifts, melodic high male vocals, 110 BPM',
        slots: { language: 'English', genre: 'progressive rock', rhythm: 'shifting signatures, dynamic', guitars: 'alternating clean and heavy', keys: 'Mellotron, Moog', vocals: 'melodic high male', mood: 'epic, adventurous', production: '1970s elaborate' }, bpm: 110,
      },
      {
        id: 'rock-instr', name: 'Инструментальный рок (без слов)',
        style: 'Instrumental, no vocals, instrumental rock, melodic guitar leads over driving rhythm, powerful drums, dynamic build-ups, 125 BPM',
        slots: instr({ genre: 'instrumental rock', rhythm: 'driving, dynamic build-ups', guitars: 'melodic lead guitar', keys: '', mood: 'determined, cinematic', production: 'polished wide' }), bpm: 125, lyrics: '[Instrumental]',
      },
    ],
  },
  // Альт-рок с «умной» гитарной гармонией: интерлок-арпеджио, sus/add9-аккорды,
  // chorus-стены, слайд в открытом строе. Вайбы Interpol / Joy Division / Radiohead /
  // Placebo / White Stripes без прямых цитат; каждый стиль — с вокалом и «без слов».
  {
    id: 'alt-rock', name: 'Альт-рок / гитарная гармония',
    items: [
      {
        id: 'dark-arpeggio-postpunk', name: 'Тёмный пост-панк с арпеджио (Interpol-вайб)',
        style: 'English, dark post-punk, two interlocking minor-key arpeggiated guitars, deep propulsive bass, metronomic drums, deadpan baritone male vocals, doomed urban romance, 130 BPM',
        slots: { language: 'English', genre: 'post-punk, post-punk revival', rhythm: 'metronomic driving', guitars: 'interlocking minor arpeggios, staccato chords', keys: '', vocals: 'deadpan baritone male', mood: 'doomed romance, night city', production: 'dry tight, punchy modern' }, bpm: 130,
      },
      {
        id: 'dark-arpeggio-postpunk-instr', name: 'Тёмный пост-панк с арпеджио (без слов)',
        style: 'Instrumental, no vocals, dark post-punk, interlocking minor-key arpeggiated guitars carrying the melody, deep propulsive bass, metronomic drums, doomed urban night, 130 BPM',
        slots: instr({ genre: 'instrumental post-punk', rhythm: 'metronomic driving', guitars: 'interlocking minor arpeggios, melodic leads', keys: '', mood: 'doomed urban night', production: 'dry tight modern' }), bpm: 130, lyrics: '[Instrumental]',
      },
      // Joy Division: первоисточник (1979) — мелодию ведёт высокий бас, гитара
      // скупая и тонкая, механические барабаны в гулкой реверберации, ледяные
      // струнные синты (поздние вещи), глубокий баритон; холод и пустота, не грязь
      {
        id: 'cold-bass-postpunk', name: 'Холодный пост-панк с мелодичным басом (Joy Division-вайб)',
        style: 'English, dark post-punk, cold 1979 minimalism, melodic high-register bass guitar playing the lead melody, mechanical tight drums with cavernous reverb, sparse thin trebly guitar, icy string synth pads, deep deadpan baritone male vocals, bleak isolation, 140 BPM',
        slots: { language: 'English', genre: 'post-punk, cold wave', rhythm: 'mechanical tight, cavernous', guitars: 'sparse thin trebly, bass carries the melody', keys: 'icy string synth pads', vocals: 'deep deadpan baritone male', mood: 'bleak isolation, despair', production: 'cavernous reverb, spacious raw 1979' }, bpm: 140,
      },
      {
        id: 'cold-bass-postpunk-instr', name: 'Холодный пост-панк с мелодичным басом (без слов)',
        style: 'Instrumental, no vocals, dark post-punk, cold 1979 minimalism, melodic high-register bass guitar carrying the melody, mechanical tight drums with cavernous reverb, sparse thin trebly guitar, icy string synth pads, bleak isolation, 140 BPM',
        slots: instr({ genre: 'instrumental post-punk, cold wave', rhythm: 'mechanical tight, cavernous', guitars: 'sparse thin trebly, bass melody leads', keys: 'icy string synth pads', mood: 'bleak isolation', production: 'cavernous reverb, spacious raw 1979' }), bpm: 140, lyrics: '[Instrumental]',
      },
      {
        id: 'art-rock-chords', name: 'Арт-рок с необычными аккордами (Radiohead-вайб)',
        style: 'English, art rock, unusual guitar harmony with sus2 and add9 colors, clean delayed arpeggios, paranoid melancholy, quiet-loud dynamics, occasional 5/4 passage, anxious high male vocals, 110 BPM',
        slots: { language: 'English', genre: 'art rock, alternative', rhythm: 'shifting, breathing, dynamic', guitars: 'clean sus/add9 chords, delay arpeggios', keys: '', vocals: 'anxious high male, falsetto leaps', mood: 'paranoid melancholy, alienation', production: 'wide modern alt-rock' }, bpm: 110,
      },
      {
        id: 'art-rock-chords-instr', name: 'Арт-рок с необычными аккордами (без слов)',
        style: 'Instrumental, no vocals, art rock, unusual sus2/add9 guitar harmony, clean delayed arpeggios, paranoid melancholy, quiet-loud dynamics, occasional 5/4 passage, 110 BPM',
        slots: instr({ genre: 'instrumental art rock', rhythm: 'shifting, dynamic', guitars: 'sus/add9 chords, delay arpeggios', keys: '', mood: 'paranoid melancholy', production: 'wide modern alt-rock' }), bpm: 110, lyrics: '[Instrumental]',
      },
      {
        id: 'chorus-wall-alt', name: 'Альт-рок с chorus-гитарами (Placebo-вайб)',
        style: 'English, alternative rock, chorus-drenched buzzing guitar walls, glam edge, driving straight beat, ringing melodic riffs, tense high androgynous male vocals, bittersweet intoxicating romance, 125 BPM',
        slots: { language: 'English', genre: 'alternative rock, glam-tinged', rhythm: 'driving straight', guitars: 'chorus-drenched walls, ringing riffs', keys: '', vocals: 'tense high androgynous male', mood: 'bittersweet intoxicating romance', production: 'polished 1998 alt-rock' }, bpm: 125,
      },
      {
        id: 'chorus-wall-alt-instr', name: 'Альт-рок с chorus-гитарами (без слов)',
        style: 'Instrumental, no vocals, alternative rock, chorus-drenched buzzing guitar walls, glam edge, driving straight beat, ringing melodic riffs, bittersweet intensity, 125 BPM',
        slots: instr({ genre: 'instrumental alternative rock', rhythm: 'driving straight', guitars: 'chorus-drenched walls, ringing melodies', keys: '', mood: 'bittersweet intensity', production: 'polished alt-rock' }), bpm: 125, lyrics: '[Instrumental]',
      },
      {
        id: 'garage-blues-duo', name: 'Гараж-блюз-дуэт (White Stripes-вайб)',
        style: 'English, garage blues rock duo, slide guitar in open tuning, no bass guitar, pounding heavy drums, stomping fuzz riffs, raw passionate male vocals, frantic swagger, 120 BPM',
        slots: { language: 'English', genre: 'garage blues', rhythm: 'pounding stomping', guitars: 'open-tuning slide, fuzz riffs', keys: '', vocals: 'raw passionate male, howling', mood: 'frantic swagger', production: 'raw analog two-piece' }, bpm: 120,
      },
      {
        id: 'garage-blues-duo-instr', name: 'Гараж-блюз-дуэт (без слов)',
        style: 'Instrumental, no vocals, garage blues rock duo, slide guitar in open tuning carrying the tune, no bass, pounding heavy drums, stomping fuzz riffs, frantic swagger, 120 BPM',
        slots: instr({ genre: 'instrumental garage blues', rhythm: 'pounding stomping', guitars: 'open-tuning slide leads, fuzz riffs', keys: '', mood: 'frantic swagger', production: 'raw analog two-piece' }), bpm: 120, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'punk', name: 'Панк / пост-панк',
    items: [
      {
        id: 'punk-rock', name: 'Панк-рок',
        style: 'English, punk rock, fast three-chord songs, raw shouted vocals, simple loud drums and bass, two minutes of fury, 170 BPM',
        slots: { language: 'English', genre: 'punk rock', rhythm: 'fast raw, straight', guitars: 'barre chords, no solos', keys: '', vocals: 'shouted raw', mood: 'fury, defiance', production: 'raw live' }, bpm: 170,
      },
      {
        id: 'post-punk', name: 'Пост-панк',
        style: 'English, post-punk, cold driving bassline, metallic-edged guitars, deadpan baritone vocals, tense grey atmosphere, danceable tension, 145 BPM',
        slots: { language: 'English', genre: 'post-punk', rhythm: 'cold motorik drive', guitars: 'metallic edge, sparse', keys: '', vocals: 'deadpan baritone', mood: 'tense grey detachment', production: 'dry cold 1980' }, bpm: 145,
      },
      {
        id: 'post-punk-instr', name: 'Пост-панк без слов',
        style: 'Instrumental, no vocals, instrumental post-punk, motorik driving bass and drums, icy chorus guitars, hypnotic grey groove, 140 BPM',
        slots: instr({ genre: 'instrumental post-punk', rhythm: 'motorik driving', guitars: 'icy chorus arpeggios', keys: '', mood: 'hypnotic grey tension', production: 'dry cold' }), bpm: 140, lyrics: '[Instrumental]',
      },
      {
        id: 'gothic-rock', name: 'Готик-рок',
        style: 'English, gothic rock, chorus-drenched rolling bass, dark brooding atmosphere, dramatic deep male vocals, cavernous reverb, 120 BPM',
        slots: { language: 'English', genre: 'gothic rock, darkwave', rhythm: 'rolling processional', guitars: 'chorus-drenched, shimmering dark', keys: '', vocals: 'dramatic deep male', mood: 'brooding romantic darkness', production: 'cavernous 1985' }, bpm: 120,
      },
      {
        id: 'horror-punk', name: 'Хоррор-панк',
        style: 'English, horror punk, spooky catchy punk, galloping horror riff, ghoul-chorus backing vocals, campy monster-movie imagery, 160 BPM',
        slots: { language: 'English', genre: 'horror punk', rhythm: 'galloping fast', guitars: 'spooky horror riffs', keys: '', vocals: 'snarling male with ghoul choir', mood: 'campy spooky fun', production: 'raw 1980' }, bpm: 160,
      },
    ],
  },
  {
    id: 'metal', name: 'Метал',
    items: [
      {
        id: 'heavy-metal', name: 'Хеви-метал (классика 80-х)',
        style: 'English, classic 1980s heavy metal, galloping bass, twin guitar harmonies, soaring high male vocals, punchy drums, 135 BPM',
        slots: { language: 'English', genre: 'heavy metal', rhythm: 'galloping, punchy', guitars: 'twin harmonic leads', keys: '', vocals: 'soaring high male', mood: 'heroic defiance', production: '1983 punchy' }, bpm: 135,
      },
      {
        id: 'thrash', name: 'Трэш-метал',
        style: 'English, thrash metal, aggressive downpicked riffs, fast double-kick drums, barked rhythmic vocals, breakneck tempo, 185 BPM',
        slots: { language: 'English', genre: 'thrash metal', rhythm: 'breakneck double-kick', guitars: 'aggressive downpicked riffs', keys: '', vocals: 'barked rhythmic', mood: 'aggression, urgency', production: 'sharp loud' }, bpm: 185,
      },
      {
        id: 'doom', name: 'Дум-метал',
        style: 'English, doom metal, crushingly slow riffs, thick dark distortion, mournful wailing vocals, funeral pace, cavernous heaviness, 70 BPM',
        slots: { language: 'English', genre: 'doom metal', rhythm: 'crushingly slow', guitars: 'thick dark riffs', keys: '', vocals: 'mournful wailing', mood: 'grief, dread', production: 'thick cavernous' }, bpm: 70,
      },
      {
        id: 'stoner-instr', name: 'Стоунер-рок без слов',
        style: 'Instrumental, no vocals, instrumental stoner rock, fuzzy thick retro riffs, groovy heavy jam, vintage tube fuzz, hazy desert vibe, 100 BPM',
        slots: instr({ genre: 'stoner rock, instrumental', rhythm: 'groovy heavy', guitars: 'fuzzy thick retro riffs', keys: '', mood: 'hazy desert groove', production: 'vintage tube fuzz' }), bpm: 100, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'folk', name: 'Фолк / акустика',
    items: [
      {
        id: 'singersongwriter', name: 'Бардовская / авторская (гитара+голос)',
        style: 'Russian, singer-songwriter, one acoustic guitar and voice, intimate storytelling verses, sincere conversational vocals, live room, campfire honesty, 95 BPM',
        slots: { language: 'Russian', genre: 'singer-songwriter, bard song', rhythm: 'free-flowing, rubato', guitars: 'fingerpicked acoustic guitar', keys: '', vocals: 'sincere male, conversational storytelling', mood: 'intimate, sincere', production: 'live room, close and dry' }, bpm: 95,
      },
      {
        id: 'folk-acoustic', name: 'Фолк-акустика (без слов)',
        style: 'Instrumental, no vocals, acoustic folk, fingerpicked guitar interludes, wooden flute, gentle frame drum, meadow-and-forest serenity, 90 BPM',
        slots: instr({ genre: 'acoustic folk', rhythm: 'gentle pulsing', guitars: 'fingerpicked melodies', keys: 'wooden flute', mood: 'pastoral serenity', production: 'warm natural' }), bpm: 90, lyrics: '[Instrumental]',
      },
      {
        id: 'slavic-folk', name: 'Славянский фолк',
        style: 'Russian, slavic folk, gusli and zhaleyka motifs, female folk vocals with ornaments, circle-dance rhythm, ancient field and river imagery, 105 BPM',
        slots: { language: 'Russian', genre: 'slavic folk', rhythm: 'circle-dance pulse', guitars: '', keys: 'gusli, zhaleyka, whistle', vocals: 'female folk, ornamented', mood: 'ancient, earthy', production: 'natural live' }, bpm: 105,
      },
      {
        id: 'folk-rock', name: 'Фолк-рок',
        style: 'English, folk rock, strummed acoustic guitars with electric leads, steady drums, storytelling male vocals, anthemic choruses, 125 BPM',
        slots: { language: 'English', genre: 'folk rock', rhythm: 'steady driving', guitars: 'strummed acoustic + electric leads', keys: 'mandolin', vocals: 'storytelling male', mood: 'anthemic, road-trip', production: 'warm full band' }, bpm: 125,
      },
    ],
  },
  {
    id: 'country', name: 'Кантри',
    items: [
      {
        id: 'country-classic', name: 'Классическое кантри',
        style: 'English, classic country, twangy telecaster, steel guitar bends, train-beat bass, lonesome male vocals, whiskey-and-heartache storytelling, 100 BPM',
        slots: { language: 'English', genre: 'classic country', rhythm: 'train beat', guitars: 'twangy telecaster', keys: 'steel guitar', vocals: 'lonesome male drawl', mood: 'heartache, highway', production: '1960s nashville' }, bpm: 100,
      },
      {
        id: 'bluegrass', name: 'Блюграсс',
        style: 'English, bluegrass, breakneck banjo rolls, fiddle and mandolin breaks, upright slap bass, high lonesome harmonies, porch energy, 150 BPM',
        slots: { language: 'English', genre: 'bluegrass', rhythm: 'breakneck roll', guitars: 'flatpicking runs', keys: 'banjo, fiddle, mandolin', vocals: 'high lonesome harmonies', mood: 'joyous porch energy', production: 'live around one mic' }, bpm: 150,
      },
      {
        id: 'country-ballad', name: 'Кантри-баллада',
        style: 'English, country ballad, slow swaying 6/8, weeping steel guitar, brushed drums, tender male vocals, dusty sunset nostalgia, 70 BPM',
        slots: { language: 'English', genre: 'country ballad', rhythm: 'slow 6/8 sway', guitars: 'soft strums', keys: 'weeping steel guitar, piano', vocals: 'tender male', mood: 'dusty nostalgia', production: 'warm classic' }, bpm: 70,
      },
    ],
  },
  {
    id: 'funk-soul', name: 'Фанк / соул / госпел',
    items: [
      {
        id: 'funk', name: 'Фанк',
        style: 'English, funk, syncopated slap bass, wah rhythm guitar, punchy horns, tight drums with ghost notes, swaggering groove, 105 BPM',
        slots: { language: 'English', genre: 'funk', rhythm: 'syncopated tight groove', guitars: 'wah rhythm chops', keys: 'clavinet, punchy horns', vocals: 'confident, call-and-response', mood: 'swagger, party', production: 'punchy 1975' }, bpm: 105,
      },
      {
        id: 'soul', name: 'Соул (мотаун-вайб)',
        style: 'English, classic soul, warm tremolo guitar, walking bass, baritone sax, passionate female vocals with gospel runs, bittersweet love, 85 BPM',
        slots: { language: 'English', genre: 'classic soul, motown', rhythm: 'smooth mid-tempo', guitars: 'warm tremolo chords', keys: 'piano, baritone sax, strings', vocals: 'passionate female, gospel runs', mood: 'bittersweet love', production: 'warm 1966 analog' }, bpm: 85,
      },
      {
        id: 'gospel', name: 'Госпел',
        style: 'English, gospel, church organ and piano, hand-claps, mass choir with lead vocalist, rising testifying fervor, uplifted, 90 BPM',
        slots: { language: 'English', genre: 'gospel', rhythm: 'hand-claps, stomping', guitars: '', keys: 'hammond organ, piano', vocals: 'mass choir + powerful lead', mood: 'testifying fervor, uplift', production: 'big church live' }, bpm: 90,
      },
      {
        id: 'funk-instr', name: 'Фанк без слов',
        style: 'Instrumental, no vocals, instrumental funk, syncopated slap bass leads, wah guitar licks, tight horn stabs, breakbeat drums, 108 BPM',
        slots: instr({ genre: 'instrumental funk', rhythm: 'syncopated tight', guitars: 'wah licks', keys: 'clavinet, horn stabs', mood: 'swagger groove', production: 'punchy analog' }), bpm: 108, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'hiphop', name: 'Хип-хоп',
    items: [
      {
        id: 'boombap', name: 'Бум-бэп (золотая эра)',
        style: 'English, boom bap hip-hop, dusty sampled beat, fat kick and snappy snare, jazzy sample loop, confident rap flow, 92 BPM',
        slots: { language: 'English', genre: 'boom bap hip-hop', rhythm: 'dusty head-nod beat', guitars: '', keys: 'jazzy sample loop', vocals: 'confident rap flow', mood: 'golden-era cool', production: 'sampled, warm dusty' }, bpm: 92,
      },
      {
        id: 'trap', name: 'Трэп',
        style: 'English, trap, booming 808 bass, rolling hi-hats, dark minor synth melody, laid-back menacing flow, 140 BPM',
        slots: { language: 'English', genre: 'trap', rhythm: 'rolling hi-hats, half-time', guitars: '', keys: 'dark minor synth', vocals: 'laid-back rap, autotune hooks', mood: 'menacing night', production: 'modern heavy 808s' }, bpm: 140,
      },
      {
        id: 'rus-hiphop', name: 'Русский хип-хоп (лирика)',
        style: 'Russian, melodic russian hip-hop, melancholic piano loop, crisp boom-bap-leaning beat, sincere rap verses and sung hook, rainy city mood, 90 BPM',
        slots: { language: 'Russian', genre: 'melodic hip-hop', rhythm: 'crisp mid-tempo beat', guitars: '', keys: 'melancholic piano loop', vocals: 'sincere rap + sung hook', mood: 'rainy city melancholy', production: 'clean modern' }, bpm: 90,
      },
      {
        id: 'ghetto-funk', name: 'Фанк-брейк с рэпом',
        style: 'English, funk-rap, live funk breakbeat, slap bass, horn stabs, old-school party rap with crowd responses, 105 BPM',
        slots: { language: 'English', genre: 'funk rap, old school', rhythm: 'live funk breakbeat', guitars: 'wah chops', keys: 'horn stabs, clavinet', vocals: 'old-school rap, crowd responses', mood: 'block party', production: 'live band' }, bpm: 105,
      },
    ],
  },
  {
    id: 'latino', name: 'Латино / фламенко',
    items: [
      {
        id: 'flamenco', name: 'Фламенко',
        style: 'Spanish, flamenco, fiery rasgueado guitar, palmas hand-claps, cajón, passionate wailing male vocals with melisma, duende intensity, 120 BPM',
        slots: { language: 'Spanish', genre: 'flamenco', rhythm: 'palmas, compás pulse', guitars: 'fiery rasgueado falsetas', keys: 'cajón', vocals: 'passionate wailing, melismatic', mood: 'duende, fierce sorrow', production: 'intimate live' }, bpm: 120,
      },
      {
        id: 'salsa', name: 'Сальса',
        style: 'Spanish, salsa, montuno piano, brass section, congas and timbales, swinging dance groove, joyful male vocals with coro, 95 BPM',
        slots: { language: 'Spanish', genre: 'salsa', rhythm: 'clave, congas, timbales', guitars: '', keys: 'montuno piano, brass', vocals: 'joyful male with coro responses', mood: 'dancefloor joy', production: 'big band latin' }, bpm: 95,
      },
      {
        id: 'tango', name: 'Танго',
        style: 'Instrumental, no vocals, argentine tango, bandoneón laments, dramatic staccato strings, piano stabs, sharp stops and lunges, fatal romance, 110 BPM',
        slots: instr({ genre: 'argentine tango', rhythm: 'sharp staccato, sudden stops', guitars: '', keys: 'bandoneón, dramatic strings', mood: 'fatal romance, dusk drama', production: 'vintage ballroom' }), bpm: 110, lyrics: '[Instrumental]',
      },
      {
        id: 'reggaeton', name: 'Реггетон',
        style: 'Spanish, reggaeton, dembow bounce, deep synth bass, catchy chanted hooks, club summer heat, 96 BPM',
        slots: { language: 'Spanish', genre: 'reggaeton', rhythm: 'dembow bounce', guitars: '', keys: 'deep synth bass, plucks', vocals: 'chanted catchy hooks', mood: 'club summer heat', production: 'modern club' }, bpm: 96,
      },
    ],
  },
  {
    id: 'classical', name: 'Классика / танцы',
    items: [
      {
        id: 'baroque', name: 'Барокко',
        style: 'Instrumental, no vocals, baroque, harpsichord and recorder sonata, walking cello bass, ornate trills, courtly elegance, 100 BPM',
        slots: instr({ genre: 'baroque chamber', rhythm: 'steady walking bass', guitars: 'lute', keys: 'harpsichord, recorder', mood: 'courtly elegance', production: 'intimate chamber' }), bpm: 100, lyrics: '[Instrumental]',
      },
      {
        id: 'string-quartet', name: 'Струнный квартет',
        style: 'Instrumental, no vocals, string quartet, four voices in conversation, warm vibrato, gentle andante with dramatic middle section, 80 BPM',
        slots: instr({ genre: 'chamber, string quartet', rhythm: 'andante, breathing', guitars: '', keys: 'two violins, viola, cello', mood: 'conversational warmth with drama', production: 'close hall' }), bpm: 80, lyrics: '[Instrumental]',
      },
      {
        id: 'waltz', name: 'Вальс',
        style: 'Instrumental, no vocals, waltz, oom-pah-pah bass, swirling melody line, old ballroom nostalgia, 3/4, 140 BPM',
        slots: instr({ genre: 'waltz', rhythm: '3/4 oom-pah-pah', guitars: '', keys: 'piano, strings, accordion', mood: 'ballroom nostalgia', production: 'vintage hall' }), bpm: 140, lyrics: '[Instrumental]',
      },
      {
        id: 'organ-dark', name: 'Орган, готика (без слов)',
        style: 'Instrumental, no vocals, dark organ toccata, cathedral echoes, ominous minor cascades, gothic dread and grandeur, 75 BPM',
        slots: instr({ genre: 'organ, gothic classical', rhythm: 'pulsing toccata', guitars: '', keys: 'pipe organ', mood: 'gothic dread, grandeur', production: 'huge cathedral' }), bpm: 75, lyrics: '[Instrumental]',
      },
    ],
  },
  {
    id: 'russian', name: 'Русское',
    items: [
      {
        id: 'ru-rock', name: 'Русский рок (классика жанра)',
        style: 'Russian, classic russian rock, anthemic minor-key guitars, earnest hoarse male vocals, live drums, sing-along chorus, kitchen-poetry sincerity, 120 BPM',
        slots: { language: 'Russian', genre: 'russian rock', rhythm: 'steady anthemic', guitars: 'clean-to-distort riffs', keys: '', vocals: 'earnest hoarse male', mood: 'sincere, defiant hope', production: 'live band, 1990s' }, bpm: 120,
      },
      {
        id: 'ru-estrada', name: 'Эстрада (советская вайб)',
        style: 'Russian, soviet-era estrada, orchestral pop, warm strings and brass, theatrical clear female or male vocals, lyrical melody, 90 BPM',
        slots: { language: 'Russian', genre: 'estrada, orchestral pop', rhythm: 'smooth fox-trot', guitars: '', keys: 'strings, brass, piano', vocals: 'theatrical clear vocals', mood: 'lyrical, solemn warmth', production: 'big orchestra, vintage' }, bpm: 90,
      },
      {
        id: 'ru-romance', name: 'Городской романс',
        style: 'Russian, urban romance, solo guitar, sorrowful gypsy-tinged vocals with tremble, minor key, candle-lit nostalgia, 70 BPM',
        slots: { language: 'Russian', genre: 'russian romance', rhythm: 'free, rubato', guitars: 'fingerpicked classical guitar', keys: 'accordion touches', vocals: 'sorrowful, trembling', mood: 'candle-lit sorrow', production: 'intimate vintage' }, bpm: 70,
      },
    ],
  },
]

// Пользовательские группы (страница «Библиотека») хранятся в localStorage.
export function loadCustomGroups() {
  try {
    const raw = JSON.parse(localStorage.getItem('yue_custom_groups') || '[]')
    return Array.isArray(raw) ? raw : []
  } catch {
    return []
  }
}

export function saveCustomGroups(gs) {
  localStorage.setItem('yue_custom_groups', JSON.stringify(gs))
}
