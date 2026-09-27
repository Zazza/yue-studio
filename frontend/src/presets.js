// Библиотека откалиброванных пресетов (марафон тестов YuE2, 2026-09-25).
// Источник: ~/talk/topics/yue-samples/presets/*.json.
// slots — декомпозиция для правки отдельных полей (скомпилируется заново);
// style — точная исходная строка калибровки (кнопка «строкой» — 1:1 воспроизведение).

const grobLyrics = `[Verse]
Вечная мерзлота в каждом окне
Колея да яма, яма да колея
Серое небо серому снегу
Шепчет: ничего, ничего, ничего

[Chorus]
Всё как всегда, всё как всегда
Никогда не будет нового дня
Замёрзший свет, замёрзший звук
Мы лежим на дне, и дно — наш друг`

const stihLyrics = `[Verse]
Прежде мы были птицами,
крылья — тончайший лён,
небо — лазурно-ситцево,
сшитый душицей склон

сердце подобно вишенке —
крохотный спелый плод
прежде мы были выше,
а мир суетился под

[Verse]
прежде мы были истинны,
как для младенца «Ма»,
как для деревьев листья
и как неизбежно тьма

зверем полночным близится —
за утомлённым днём
прежде мы были близ Отца,
не сомневаясь в Нём

[Verse]
прежде рассветы полнились
лентами наших тел
крылья касались волн
и ввысь каждый из нас летел,

к огненной колеснице
мы пили вишнёвый ром

[Outro]
прежде мы были птицами
прячет ладонь перо`

const femLyrics = `[Verse]
Город не спит, я тоже не сплю
Считаю трещины на потолке

[Chorus]
Не люби меня, я злая
Уезжай, пока светает`

export const presets = [
  {
    id: 'interpol',
    name: 'Interpol — dark post-punk',
    seed: 831001,
    style: 'English, dark post-punk, driving repetitive bassline, staccato arpeggiated guitars, deadpan baritone male vocals, atmospheric, moody, steady energetic groove, 135 BPM',
    slots: {
      language: 'English', genre: 'dark post-punk',
      rhythm: 'driving repetitive bassline, steady energetic groove',
      guitars: 'staccato arpeggiated guitars', keys: '',
      vocals: 'deadpan baritone male vocals',
      mood: 'atmospheric, moody', production: '',
    },
    bpm: 135,
    lyrics: `[Verse]
Sodium light on empty streets
I count the cracks beneath my feet
Every window is a lie
Someone else's alibi

[Chorus]
Slow down, the night is caving in
We were never meant to win
I wear your silence like a crown
And watch this city drown`,
  },
  {
    id: 'idles',
    name: 'IDLES — aggressive post-punk',
    seed: 412007,
    style: 'English, aggressive post-punk, raw overdriven guitars, pounding drums, shouted desperate male vocals, abrasive, urgent, energetic, 160 BPM',
    slots: {
      language: 'English', genre: 'aggressive post-punk',
      rhythm: 'pounding drums',
      guitars: 'raw overdriven guitars', keys: '',
      vocals: 'shouted desperate male vocals',
      mood: 'abrasive, urgent, energetic', production: '',
    },
    bpm: 160,
    lyrics: `[Verse]
Television man selling fear again
Selling fear again, selling fear again
Keep your head down, bite your tongue
But I can't, no I can't

[Chorus]
Kick it down, kick it down
We don't want it anymore
Love is worth the war`,
  },
  {
    id: 'grob-v1',
    name: 'ГрОб v1 — raw Siberian punk',
    seed: 660013,
    style: 'Russian, raw lo-fi Siberian punk, cheap overdriven guitar, desperate raspy male vocals, lo-fi cassette recording, primitive chords, 1980s Soviet underground punk, 170 BPM',
    slots: {
      language: 'Russian', genre: 'raw lo-fi Siberian punk, 1980s Soviet underground punk',
      rhythm: '',
      guitars: 'cheap overdriven guitar, primitive chords', keys: '',
      vocals: 'desperate raspy male vocals',
      mood: '', production: 'lo-fi cassette recording',
    },
    bpm: 170,
    lyrics: grobLyrics,
  },
  {
    id: 'grob-v2',
    name: 'ГрОб v2 — bleak despair',
    seed: 660013,
    style: 'Russian, bleak lo-fi Siberian punk, monotonous hopeless despair, dour male vocals with overdriven microphone clipping, tape saturation, cheap droning guitar, cold damp basement recording, no bright melody, grey atmosphere, mid-tempo, 115 BPM',
    slots: {
      language: 'Russian', genre: 'bleak lo-fi Siberian punk',
      rhythm: 'mid-tempo',
      guitars: 'cheap droning guitar', keys: '',
      vocals: 'dour male vocals with overdriven microphone clipping',
      mood: 'monotonous hopeless despair, no bright melody, grey atmosphere',
      production: 'tape saturation, cold damp basement recording',
    },
    bpm: 115,
    lyrics: grobLyrics,
  },
  {
    id: 'grob-v3',
    name: 'ГрОб v3 — half-spoken deadpan',
    seed: 660013,
    style: 'Russian, 1987 home tape recording, lo-fi murk, flat monotonous low male voice, half-spoken deadpan singing, detached emotionless delivery, vocals buried in tape noise, droning cheap guitar, bleak slow grey song, no shouting, 115 BPM',
    slots: {
      language: 'Russian', genre: 'bleak slow grey song',
      rhythm: 'slow',
      guitars: 'droning cheap guitar', keys: '',
      vocals: 'flat monotonous low male voice, half-spoken deadpan singing, detached emotionless delivery, vocals buried in tape noise',
      mood: 'no shouting',
      production: '1987 home tape recording, lo-fi murk',
    },
    bpm: 115,
    lyrics: grobLyrics,
  },
  {
    id: 'grob-v4',
    name: 'ГрОб v4 — панк + регги-пульс',
    seed: 660013,
    style: 'Russian, 1980s Siberian underground rock, lo-fi home-tape murk, punk energy over post-punk gloom with a heavy loping reggae-leaning bass pulse, cheap droning overdriven guitar, cardboard drums, flat low half-spoken male vocals, deadpan delivery buried in tape noise, bleak post-soviet hopelessness, mid-tempo lilt, 110 BPM',
    slots: {
      language: 'Russian', genre: '1980s Siberian underground rock, punk energy over post-punk gloom',
      rhythm: 'heavy loping reggae-leaning bass pulse, cardboard drums, mid-tempo lilt',
      guitars: 'cheap droning overdriven guitar', keys: '',
      vocals: 'flat low half-spoken male vocals, deadpan delivery buried in tape noise',
      mood: 'bleak post-soviet hopelessness',
      production: 'lo-fi home-tape murk',
    },
    bpm: 110,
    lyrics: grobLyrics,
  },
  {
    id: 'grob-v5',
    name: 'ГрОб v5 — slow dirge',
    seed: 660013,
    style: 'Russian, 1980s Siberian underground rock, blown-out cassette murk, tape wobble and hiss, slow minor key dirge, heavy loping bass pulse, cheap distorted drone guitar, muffled cardboard drums, weary strained low male voice cracking with anguish, half-spoken, buried in mud, no catchy melody, bleak hopelessness, 85 BPM',
    slots: {
      language: 'Russian', genre: '1980s Siberian underground rock, slow minor key dirge',
      rhythm: 'heavy loping bass pulse, muffled cardboard drums',
      guitars: 'cheap distorted drone guitar', keys: '',
      vocals: 'weary strained low male voice cracking with anguish, half-spoken, buried in mud',
      mood: 'no catchy melody, bleak hopelessness',
      production: 'blown-out cassette murk, tape wobble and hiss',
    },
    bpm: 85,
    lyrics: grobLyrics,
  },
  {
    id: 'grob-v6',
    name: 'ГрОб v6 — fierce wall',
    seed: 660013,
    style: 'Russian, 1988 Siberian underground home-tape recording, booming sputtering bass guitar at the forefront, drowned buried male vocals on the verge of breaking, gothic post-punk gloom with raw punk energy, monotone incantation delivery, gang shout backing vocals, cheap sludgy overdriven guitar, drum machine and box percussion, lo-fi tape murk with noisy industrial chaos, bleak fierce and urgent, fast short song, 150 BPM',
    slots: {
      language: 'Russian', genre: 'gothic post-punk gloom with raw punk energy',
      rhythm: 'booming sputtering bass guitar at the forefront, drum machine and box percussion, fast short song',
      guitars: 'cheap sludgy overdriven guitar', keys: '',
      vocals: 'drowned buried male vocals on the verge of breaking, monotone incantation delivery, gang shout backing vocals',
      mood: 'bleak fierce and urgent',
      production: '1988 Siberian underground home-tape recording, lo-fi tape murk with noisy industrial chaos',
    },
    bpm: 150,
    lyrics: grobLyrics,
  },
  {
    id: 'doors',
    name: 'Doors — psychedelic blues',
    seed: 196702,
    style: 'English, 1960s psychedelic blues rock, Hammond organ, bluesy guitar licks, swaggering baritone male vocals, vintage analog recording, hypnotic groove, 100 BPM',
    slots: {
      language: 'English', genre: '1960s psychedelic blues rock',
      rhythm: 'hypnotic groove',
      guitars: 'bluesy guitar licks', keys: 'Hammond organ',
      vocals: 'swaggering baritone male vocals',
      mood: '', production: 'vintage analog recording',
    },
    bpm: 100,
    lyrics: `[Verse]
Moonlight crawls across my floor
I heard the knock, can't say no more
The snake is sleeping in my shoe
Waiting for the midnight blue

[Chorus]
Light my candle, watch it burn
No return, no return`,
  },
  {
    id: 'kish',
    name: 'КИШ — horror punk',
    seed: 173501,
    style: 'Russian theatrical horror punk, storytelling male vocals with dramatic delivery, fast punk energy with folk accordion accents, dark fairy tale atmosphere, singalong gang chorus, 160 BPM',
    slots: {
      language: 'Russian', genre: 'theatrical horror punk',
      rhythm: 'fast punk energy',
      guitars: '', keys: 'folk accordion accents',
      vocals: 'storytelling male vocals with dramatic delivery, singalong gang chorus',
      mood: 'dark fairy tale atmosphere', production: '',
    },
    bpm: 160,
    lyrics: `[Verse]
Ночью в лес забрёл мальчишка, заблудился в трёх соснах
Видит — избушка на курьих ножках, жёлтый свет в окне
Старушка отворила: «Заходи, не бойся, друг»
Напоила, накормила — и заперла на крюк

[Chorus]
Не ходи в тот лес, не ищи тот дом
Кто вошёл в него — не вернулся в дом`,
  },
  {
    id: 'marley',
    name: 'Marley — reggae',
    seed: 194507,
    style: 'English reggae, one drop groove, warm round bass, offbeat skank guitar, laid-back soulful male vocals, sunny morning, Hammond organ bubbles, 78 BPM',
    slots: {
      language: 'English', genre: 'reggae',
      rhythm: 'one drop groove, warm round bass',
      guitars: 'offbeat skank guitar', keys: 'Hammond organ bubbles',
      vocals: 'laid-back soulful male vocals',
      mood: 'sunny morning', production: '',
    },
    bpm: 78,
    lyrics: `[Verse]
Morning sun is rising, blessing on my door
I don't need no silver, don't need nothing more
River keeps on flowing, teaches me the way
Every little worry — wash them all away

[Chorus]
One love, one bright morning
Sing it loud, the road is calling
One love, keep on moving
Hearts are free and skies are grooving`,
  },
  {
    id: 'slipknot',
    name: 'Slipknot — nu metal',
    seed: 950011,
    style: 'English nu metal, aggressive down-tuned distorted guitar riffs, double kick blast drums, screamed and growled male vocals, chaotic turntable scratches, intense aggression, 145 BPM',
    slots: {
      language: 'English', genre: 'nu metal',
      rhythm: 'double kick blast drums',
      guitars: 'aggressive down-tuned distorted guitar riffs',
      keys: 'chaotic turntable scratches',
      vocals: 'screamed and growled male vocals',
      mood: 'intense aggression', production: '',
    },
    bpm: 145,
    lyrics: `[Verse]
Break the glass inside my head
Everything you built is dead
Feed the machine, swallow the lie
I am the virus that won't die

[Chorus]
Scream — let it all out
Burn this world down
Scream — no surrender
I'm the storm, I'm the ending`,
  },
  {
    id: 'ruki-vverh',
    name: 'Руки Вверх — eurodance',
    seed: 199708,
    style: 'Russian eurodance 1997, four-on-the-floor drum machine, bright synth lead hook, catchy cheerful male vocals, simple happy pop melody, party, 140 BPM',
    slots: {
      language: 'Russian', genre: 'eurodance 1997',
      rhythm: 'four-on-the-floor drum machine',
      guitars: '', keys: 'bright synth lead hook',
      vocals: 'catchy cheerful male vocals',
      mood: 'simple happy pop melody, party', production: '',
    },
    bpm: 140,
    lyrics: `[Verse]
Ты мне вчера сказала: встретимся в семь
Я ждал тебя у школы — а ты не пришла совсем
Позвонил тебе домой — ты с другим гуляешь там
Зачем тогда мне обещала, всё сказала нам

[Chorus]
Ты моя малышка, солнце моё светит
Ты моя малышка, лучше всех на свете`,
  },
  {
    id: 'stih-neofolk',
    name: 'Стих: неофолк',
    seed: 731109,
    style: 'Russian neofolk ballad, harp and acoustic guitar, wooden flute, modal minor, airy ethereal female vocals, gentle lament, slow elegiac, 80 BPM',
    slots: {
      language: 'Russian', genre: 'neofolk ballad, modal minor',
      rhythm: 'slow',
      guitars: 'harp and acoustic guitar', keys: 'wooden flute',
      vocals: 'airy ethereal female vocals',
      mood: 'gentle lament, elegiac', production: '',
    },
    bpm: 80,
    lyrics: stihLyrics,
  },
  {
    id: 'stih-piknik',
    name: 'Стих: Пикニック (art rock)',
    seed: 411207,
    style: 'Russian mystic art rock, vintage analog synth pads, clean electric guitar arpeggios, half-spoken charismatic male baritone, cabaret theatre mood, hypnotic brooding, 95 BPM',
    slots: {
      language: 'Russian', genre: 'mystic art rock',
      rhythm: '',
      guitars: 'clean electric guitar arpeggios', keys: 'vintage analog synth pads',
      vocals: 'half-spoken charismatic male baritone',
      mood: 'cabaret theatre mood, hypnotic brooding', production: '',
    },
    bpm: 95,
    lyrics: stihLyrics,
  },
  {
    id: 'stih-bg',
    name: 'Стих: БГ (folk rock)',
    seed: 900314,
    style: 'Russian folk rock, acoustic guitar strum, ethnic percussion, warm intimate male vocals, meditative spiritual ballad, world music shades, 90 BPM',
    slots: {
      language: 'Russian', genre: 'folk rock, world music shades',
      rhythm: 'ethnic percussion',
      guitars: 'acoustic guitar strum', keys: '',
      vocals: 'warm intimate male vocals',
      mood: 'meditative spiritual ballad', production: '',
    },
    bpm: 90,
    lyrics: stihLyrics,
  },
  {
    id: 'stih-postrock',
    name: 'Стих: пост-рок',
    seed: 515003,
    style: 'Russian post-rock, atmospheric instrumental waves, delay-drenched guitars, soft drums building and receding, whispered spoken-word male recitation over soundscape, cinematic, 85 BPM',
    slots: {
      language: 'Russian', genre: 'post-rock, atmospheric instrumental waves',
      rhythm: 'soft drums building and receding',
      guitars: 'delay-drenched guitars', keys: '',
      vocals: 'whispered spoken-word male recitation over soundscape',
      mood: 'cinematic', production: '',
    },
    bpm: 85,
    lyrics: stihLyrics,
  },
  // ---- артистические пресеты (не калиброваны марафоном: эпоха в продакшне/жанре
  // + механика подачи голоса; подбирайте сид веером ×5) ----
  {
    id: 'nirvana',
    name: 'Кобейн / Nirvana',
    seed: 910001,
    style: 'English, grunge, raw overdriven guitars, heavy loping bass pulse, pounding drums, raspy desperate male vocals, cracked voice, screamed chorus, off-key sloppy punk delivery, 1991 raw grunge production, weary resignation, 145 BPM',
    slots: {
      language: 'English', genre: 'grunge',
      rhythm: 'heavy loping bass pulse, pounding drums',
      guitars: 'raw overdriven guitars', keys: '',
      vocals: 'raspy desperate male vocals, cracked voice, screamed chorus, off-key sloppy punk delivery',
      mood: 'weary resignation', production: '1991 raw grunge production',
    },
    bpm: 145,
    lyrics: `[Verse]
Sell me a laugh, I'll pay in rust
Everything's fine in the dust
Come as you are, leave as you were

[Chorus]
Half a heart, half a lie
Raincoat soaked from inside`,
  },
  {
    id: 'bob-marley',
    name: 'Боб Марли',
    seed: 770002,
    style: 'English, reggae, one drop groove, warm round bass, offbeat skank guitar, laid-back soulful raspy male vocals, reggae phrasing, offbeat accents, 1977 analog reggae production, sunny morning, 78 BPM',
    slots: {
      language: 'English', genre: 'reggae',
      rhythm: 'one drop groove, warm round bass',
      guitars: 'offbeat skank guitar', keys: 'vintage electric piano chords',
      vocals: 'laid-back soulful raspy male vocals, reggae phrasing, offbeat accents',
      mood: 'sunny morning', production: '1977 analog reggae production',
    },
    bpm: 78,
    lyrics: `[Verse]
Morning come, the corner busy
Every man a carry him load
Sister selling bread and honey
Blessing on the dusty road

[Chorus]
Rise and shine, rise and shine
One love at a time`,
  },
  {
    id: 'doors',
    name: 'Дорс / Моррисон',
    seed: 670003,
    style: 'English, 1960s psychedelic blues rock, organ-led groove, ethnic percussion, bluesy guitar licks, Hammond organ bubbles, deep resonant baritone male vocals swaggering theatrical delivery spoken-word verses building to shouted incantation, 1960s garage recording vintage analog, hypnotic brooding, 100 BPM',
    slots: {
      language: 'English', genre: '1960s psychedelic blues rock',
      rhythm: 'hypnotic groove, ethnic percussion',
      guitars: 'bluesy guitar licks', keys: 'Hammond organ bubbles',
      vocals: 'deep resonant baritone male vocals, swaggering theatrical delivery, spoken-word verses building to shouted incantation',
      mood: 'hypnotic brooding, dark fairy tale atmosphere', production: '1960s garage recording, vintage analog',
    },
    bpm: 100,
    lyrics: `[Verse]
Riders on the storm-lit plain
The carnival closed years ago

[Chorus]
Break on through, the night is wide
The snake he laughs and opens the door`,
  },
  {
    id: 'interpol-preset',
    name: 'Интерпол',
    seed: 830004,
    style: 'English, dark post-punk, driving repetitive bassline, staccato arpeggiated guitars, deadpan baritone male vocals, atmospheric, moody, compressed radio sound, 135 BPM',
    slots: {
      language: 'English', genre: 'dark post-punk',
      rhythm: 'driving repetitive bassline, steady energetic groove',
      guitars: 'staccato arpeggiated guitars', keys: '',
      vocals: 'deadpan baritone male vocals',
      mood: 'atmospheric, moody', production: 'compressed radio sound',
    },
    bpm: 135,
    lyrics: `[Verse]
Sodium light on empty streets
I count the cracks beneath my feet

[Chorus]
Every window is a lie
I keep walking anyway`,
  },
  {
    id: 'dayte-tank',
    name: 'Дайте Танк!',
    seed: 120005,
    style: 'Russian, klezmer punk, punk energy over folk dance, ethnic percussion, folk accordion accents, raw overdriven guitars, half-spoken charismatic male baritone, theatrical storytelling delivery, cabaret theatre mood, blown-out cassette murk, playful absurdity, 160 BPM',
    slots: {
      language: 'Russian', genre: 'klezmer punk',
      rhythm: 'fast punk energy, ethnic percussion',
      guitars: 'raw overdriven guitars', keys: 'folk accordion accents',
      vocals: 'half-spoken charismatic male baritone, theatrical storytelling delivery',
      mood: 'playful absurdity, cabaret theatre mood', production: 'blown-out cassette murk',
    },
    bpm: 160,
    lyrics: stihLyrics,
  },
  {
    id: 'mumiy-troll',
    name: 'Мумий Тролль',
    seed: 970006,
    style: 'Russian, glam rock britpop blend, funky syncopated groove, chiming 12-string guitar, jangle pop guitars, swaggering baritone male vocals with playful sneer, oceanic reverb guitars, swaggering confidence, vintage analog recording, 118 BPM',
    slots: {
      language: 'Russian', genre: 'glam rock, britpop',
      rhythm: 'funky syncopated groove',
      guitars: 'chiming 12-string guitar, jangle pop guitars', keys: '',
      vocals: 'swaggering baritone male vocals with playful sneer',
      mood: 'swaggering confidence', production: 'vintage analog recording',
    },
    bpm: 118,
    lyrics: `[Verse]
Владивосток-2000, чайки и порт
Мои волны меня не ждут

[Chorus]
Утекай, куда-нибудь
Небо спрячет наш маршрут`,
  },
  {
    id: 'agata-kristi',
    name: 'Агата Кристи',
    seed: 930007,
    style: 'Russian, gothic post-punk gloom with raw punk energy, motorik krautrock beat, dark cabaret, Hammond organ bubbles, tremolo-picked guitar, dour male vocals with overdriven microphone clipping, dark fairy tale atmosphere, hypnotic brooding, tape saturation, cold damp basement recording, 105 BPM',
    slots: {
      language: 'Russian', genre: 'gothic post-punk, dark cabaret',
      rhythm: 'motorik krautrock beat',
      guitars: 'tremolo-picked guitar', keys: 'Hammond organ bubbles',
      vocals: 'dour male vocals with overdriven microphone clipping',
      mood: 'dark fairy tale atmosphere, hypnotic brooding', production: 'tape saturation, cold damp basement recording',
    },
    bpm: 105,
    lyrics: `[Verse]
Как на войне, как на войне
Все мои друзья со мной

[Chorus]
Опиум для никого
Небо в лужах отражает дым`,
  },
  {
    id: 'grob',
    name: 'Гражданская Оборона',
    seed: 880008,
    style: 'Russian, raw lo-fi Siberian punk, 1980s Soviet underground punk, cheap overdriven guitar primitive chords, fast punk energy, drowned buried male vocals on the verge of breaking, monotone incantation delivery, 1988 Siberian underground home-tape recording, lo-fi tape murk with noisy industrial chaos, bleak post-soviet hopelessness, 168 BPM',
    slots: {
      language: 'Russian', genre: 'raw lo-fi Siberian punk, 1980s Soviet underground punk',
      rhythm: 'fast punk energy',
      guitars: 'cheap overdriven guitar, primitive chords', keys: '',
      vocals: 'drowned buried male vocals on the verge of breaking, monotone incantation delivery',
      mood: 'bleak post-soviet hopelessness', production: '1988 Siberian underground home-tape recording, lo-fi tape murk',
    },
    bpm: 168,
    lyrics: grobLyrics,
  },
  {
    id: 'madonna',
    name: 'Мадонна',
    seed: 860009,
    style: 'English, 80s dance-pop synthpop, four-on-the-floor drum machine, programmed trap-free 80s drum machine, moogy synth bass, bright synth lead hook, catchy cheerful female vocals confident delivery, bright modern pop sheen style 1985 analog, celebratory party, 120 BPM',
    slots: {
      language: 'English', genre: 'synthpop, dance-pop',
      rhythm: 'four-on-the-floor drum machine',
      guitars: '', keys: 'bright synth lead hook, moogy synth bass',
      vocals: 'catchy cheerful confident female vocals',
      mood: 'celebratory party', production: 'vintage analog recording, 1985',
    },
    bpm: 120,
    lyrics: `[Verse]
Neon on the boulevard
Dress made of borrowed light

[Chorus]
Strike a pose, hold the night
We only get one life`,
  },
  {
    id: 'celine-dion',
    name: 'Селин Дион',
    seed: 970010,
    style: 'English, power ballad, slow soft building drums, piano ballad with string quartet pad, soaring powerful female vocals belting delivery big chorus melisma, polished radio-ready mix, epic grandeur, romantic longing, 66 BPM',
    slots: {
      language: 'English', genre: 'power ballad',
      rhythm: 'soft drums building and receding',
      guitars: '', keys: 'detuned upright piano, string quartet pad',
      vocals: 'soaring powerful female vocals, belting delivery, big chorus',
      mood: 'epic grandeur, romantic longing', production: 'polished radio-ready mix',
    },
    bpm: 66,
    lyrics: `[Verse]
Every night I read your letters
Sleep will not come to me

[Chorus]
Wherever you are, I will find
My way back to your heart`,
  },
  {
    id: 'multfilmy',
    name: 'Мультфильмы',
    seed: 910011,
    style: 'Russian, new wave synthpop post-punk, four-on-the-floor drum machine, bright synth lead hook, vintage electric piano chords, flat monotonous deadpan male vocals detached delivery, spacious wide stereo, cynical irony, bleak post-soviet hopelessness, 130 BPM',
    slots: {
      language: 'Russian', genre: 'new wave, synthpop',
      rhythm: 'four-on-the-floor drum machine',
      guitars: '', keys: 'bright synth lead hook, vintage electric piano chords',
      vocals: 'flat monotonous deadpan male vocals, detached delivery',
      mood: 'cynical irony, bleak post-soviet hopelessness', production: 'spacious wide stereo',
    },
    bpm: 130,
    lyrics: `[Verse]
Город спит, и я не сплю
Триста кассет — и все не о том

[Chorus]
Она не придёт, она сказала: нет
Мультфильм кончился, погас экран`,
  },
  {
    id: 'sex-pistols',
    name: 'Sex Pistols',
    seed: 770012,
    style: 'English, 1977 punk, fast punk energy, raw overdriven guitars cheap sludgy chords, pounding drums, sneered shouted desperate male vocals snarling delivery off-key, abrasive urgent energetic, blown-out cassette murk 1977 live room, rebellious defiance, 185 BPM',
    slots: {
      language: 'English', genre: 'punk, 1977',
      rhythm: 'fast punk energy, pounding drums',
      guitars: 'raw overdriven guitars, cheap sludgy chords', keys: '',
      vocals: 'sneered shouted desperate male vocals, snarling delivery, off-key',
      mood: 'abrasive, urgent, energetic, rebellious defiance', production: '1977 live room, blown-out murk',
    },
    bpm: 185,
    lyrics: `[Verse]
Cheap holidays in other people's misery
I hate the queue and the dole

[Chorus]
No future, no future for you
God save us all from boredom`,
  },
  {
    id: 'bg',
    name: 'Гребенщиков / Аквариум',
    seed: 810013,
    style: 'Russian, mystic art rock folk, hypnotic groove, clean plucked guitar intimate, detuned upright piano, half-spoken charismatic male baritone loose phrasing, gentle lament elegiac, vintage analog recording, dark fairy tale atmosphere, 92 BPM',
    slots: {
      language: 'Russian', genre: 'mystic art rock, folk',
      rhythm: 'hypnotic groove, mid-tempo',
      guitars: 'clean plucked guitar, intimate', keys: 'detuned upright piano',
      vocals: 'half-spoken charismatic male baritone, loose phrasing',
      mood: 'gentle lament, elegiac, dark fairy tale atmosphere', production: 'vintage analog recording',
    },
    bpm: 92,
    lyrics: stihLyrics,
  },
  {
    id: 'kino',
    name: 'Кино / Цой',
    seed: 860014,
    style: 'Russian, new wave post-punk, driving repetitive bassline steady energetic groove, clean electric guitar arpeggios simple, minimal drum machine, flat low half-spoken male vocals deadpan delivery, cold wave atmosphere, narrow mono, bleak post-soviet hopelessness, 140 BPM',
    slots: {
      language: 'Russian', genre: 'new wave, post-punk',
      rhythm: 'driving repetitive bassline, minimal drum machine',
      guitars: 'clean electric guitar arpeggios, simple', keys: '',
      vocals: 'flat low half-spoken male vocals, deadpan delivery',
      mood: 'bleak post-soviet hopelessness', production: 'narrow mono, cold wave',
    },
    bpm: 140,
    lyrics: `[Verse]
Ночь коротка, цель далека
Ночью так часто хочется пить

[Chorus]
Мы ждём перемен
Но каждый сам за себя`,
  },
  // ---- от себя ----
  {
    id: 'nautilus',
    name: 'Наутилус Помпилиус',
    seed: 880015,
    style: 'Russian, 80s soviet new wave rock, driving repetitive bassline, clean electric guitar arpeggios, vintage analog synth pads, high strained male vocals russian delivery dramatic, cinematic, cold wave, narrow mono, 110 BPM',
    slots: {
      language: 'Russian', genre: 'new wave rock',
      rhythm: 'driving repetitive bassline',
      guitars: 'clean electric guitar arpeggios', keys: 'vintage analog synth pads',
      vocals: 'high strained male vocals, dramatic delivery',
      mood: 'cinematic, bleak', production: 'narrow mono, vintage analog',
    },
    bpm: 110,
    lyrics: `[Verse]
Скованные одной цепью
Связанные одной целью

[Chorus]
Здесь меридианы скованы
Стянуты сеткой одной`,
  },
  {
    id: 'alisa',
    name: 'Алиса',
    seed: 870016,
    style: 'Russian, hard rock with punk energy, pounding drums, raw overdriven guitars, shouted desperate male vocals russian, dynamic energetic, dense wall of sound, rebellious defiance, 150 BPM',
    slots: {
      language: 'Russian', genre: 'hard rock, punk energy',
      rhythm: 'pounding drums, fast',
      guitars: 'raw overdriven guitars', keys: '',
      vocals: 'shouted desperate male vocals',
      mood: 'rebellious defiance, intense aggression', production: 'dense wall of sound',
    },
    bpm: 150,
    lyrics: grobLyrics,
  },
  {
    id: 'ddt',
    name: 'ДДТ',
    seed: 900017,
    style: 'Russian, russian rock ballad with blues roots, bluesy guitar licks, soft drums building and receding, weary strained low male voice cracking with anguish half-spoken, cinematic, tape saturation cold damp basement, weary resignation, 95 BPM',
    slots: {
      language: 'Russian', genre: 'russian rock ballad, blues rock',
      rhythm: 'soft drums building and receding',
      guitars: 'bluesy guitar licks, slide accents', keys: 'Hammond organ',
      vocals: 'weary strained low male voice cracking with anguish, half-spoken',
      mood: 'weary resignation, cinematic', production: 'tape saturation, cold damp basement recording',
    },
    bpm: 95,
    lyrics: grobLyrics,
  },
  {
    id: 'radiohead',
    name: 'Radiohead',
    seed: 970018,
    style: 'English, art rock alternative, glitchy IDM beats mixed with live drums, chiming guitars into noise walls, high strained male vocals fragile falsetto, eerie unsettling, spacious wide stereo, bittersweet nostalgia, 120 BPM',
    slots: {
      language: 'English', genre: 'art rock, alternative',
      rhythm: 'half-time groove, glitchy beats',
      guitars: 'chiming guitars into noise walls', keys: 'vintage analog synth pads',
      vocals: 'high strained male vocals, fragile falsetto',
      mood: 'eerie unsettling, bittersweet nostalgia', production: 'spacious wide stereo',
    },
    bpm: 120,
    lyrics: `[Verse]
I wake alone in the machine
Everyone is connected here

[Chorus]
Everything in its right place
Yesterday I woke up sucking a lemon`,
  },
  {
    id: 'depeche',
    name: 'Depeche Mode',
    seed: 900019,
    style: 'English, synthpop darkwave, four-on-the-floor drum machine, moogy synth bass, bright synth lead hook dark, swaggering baritone male vocals sensual brooding, hypnotic trance-like repetition, compressed radio sound, 125 BPM',
    slots: {
      language: 'English', genre: 'synthpop, darkwave',
      rhythm: 'four-on-the-floor drum machine',
      guitars: '', keys: 'moogy synth bass, dark synth lead',
      vocals: 'swaggering baritone male vocals, sensual brooding delivery',
      mood: 'hypnotic, claustrophobic', production: 'compressed radio sound',
    },
    bpm: 125,
    lyrics: `[Verse]
Words like violence break the silence

[Chorus]
I can't understand your meaning
Enjoy the silence`,
  },
  {
    id: 'rammstein',
    name: 'Rammstein',
    seed: 970020,
    style: 'German, industrial metal, double kick drums, aggressive down-tuned distorted guitar riffs, pounding mechanical rhythm, guttural commanding male vocals german rolled r delivery, intense aggression, dense wall of sound, 140 BPM',
    slots: {
      language: 'German', genre: 'industrial metal',
      rhythm: 'double kick blast drums, mechanical pounding',
      guitars: 'aggressive down-tuned distorted guitar riffs', keys: '',
      vocals: 'guttural commanding male vocals, rolled r, dramatic delivery',
      mood: 'intense aggression', production: 'dense wall of sound',
    },
    bpm: 140,
    lyrics: `[Verse]
Ich sehe dich am anderen Ufer
Das Feuer wartet schon

[Chorus]
Willkommen in der Nacht
Hier ist dein Platz`,
  },
  {
    id: 'queen',
    name: 'Queen',
    seed: 750021,
    style: 'English, 1970s arena rock glam, pounding drums, double-tracked hard-panned guitars, twin guitar harmonies, operatic layered choir backing vocals, swaggering powerful male vocals wide range dramatic delivery, vintage analog recording dense wall of sound, anthemic triumphant epic grandeur, 110 BPM',
    slots: {
      language: 'English', genre: 'arena rock, glam rock, 1970s',
      rhythm: 'pounding drums, mid-tempo anthemic',
      guitars: 'double-tracked hard-panned guitars, twin guitar harmonies', keys: 'grand piano',
      vocals: 'swaggering powerful male vocals, wide range, dramatic delivery, operatic layered choir backing',
      mood: 'anthemic triumphant, epic grandeur', production: 'vintage analog recording, dense wall of sound',
    },
    bpm: 110,
    lyrics: `[Verse]
Caught in a one-way track
No time for goodbye

[Chorus]
The show must go on
Inside my heart is breaking`,
  },
  // ---- женские манеры ----
  {
    id: 'zemfira',
    name: 'Земфира',
    seed: 990101,
    style: 'Russian, 90s russian alt-rock, driving repetitive bassline, clean electric guitar arpeggios into overdrive, raw drums, androgynous airy female vocals deadpan verses exploding to raw choruses, 1999 russian alt production dry, cynical irony, romantic longing, 130 BPM',
    slots: {
      language: 'Russian', genre: 'russian alt-rock, 1990s',
      rhythm: 'driving repetitive bassline, raw drums',
      guitars: 'clean electric guitar arpeggios into overdrive', keys: '',
      vocals: 'androgynous airy female vocals, deadpan verses exploding to raw choruses',
      mood: 'cynical irony, romantic longing', production: '1999 russian alt production, dry',
    },
    bpm: 130,
    lyrics: femLyrics,
  },
  {
    id: 'lanadelrey',
    name: 'Лана Дель Рей',
    seed: 110102,
    style: 'English, cinematic torch song sadcore, slow hypnotic groove, twangy reverbed guitar, vintage electric piano, string quartet pad, airy low female vocals breathy detached delivery lazy phrasing, 1960s hollywood demo tape hiss, bittersweet nostalgia, romantic longing, 75 BPM',
    slots: {
      language: 'English', genre: 'cinematic torch song, sadcore',
      rhythm: 'slow, hypnotic groove',
      guitars: 'twangy reverbed guitar', keys: 'vintage electric piano, string quartet pad',
      vocals: 'airy low female vocals, breathy detached delivery, lazy phrasing',
      mood: 'bittersweet nostalgia, romantic longing', production: '1960s hollywood demo tape, hiss',
    },
    bpm: 75,
    lyrics: `[Verse]
Palm trees in black and white
Old money, older lies

[Chorus]
Ride till we die
Video games on a friday night`,
  },
  {
    id: 'bjork',
    name: 'Бьорк',
    seed: 950103,
    style: 'English, art pop electronic, glitchy IDM beats, string quartet stabs, music box plucks, childlike female vocals extreme dynamics whoops and gasps, raw emotional delivery, spacious wide stereo electronics, cosmic wonder, 100 BPM',
    slots: {
      language: 'English', genre: 'art pop, electronic',
      rhythm: 'glitchy IDM beats',
      guitars: '', keys: 'string quartet stabs, music box plucks',
      vocals: 'childlike female vocals, extreme dynamics, whoops and gasps, raw emotional delivery',
      mood: 'cosmic wonder, tender lullaby', production: 'spacious wide stereo, electronics',
    },
    bpm: 100,
    lyrics: `[Verse]
I miss you, but I have not met you yet

[Chorus]
All these states I am in
You'll be given love`,
  },
  {
    id: 'pjharvey',
    name: 'PJ Harvey',
    seed: 930104,
    style: 'English, 90s alternative blues rock, heavy loping bass, raw slide guitar, pounding drums, fierce raspy female vocals howling and whispering blues phrasing, 1993 raw analog production, intense aggression, dark fairy tale atmosphere, 120 BPM',
    slots: {
      language: 'English', genre: 'alternative blues rock, 1990s',
      rhythm: 'heavy loping bass pulse, pounding drums',
      guitars: 'raw slide guitar, aggressive riffs', keys: '',
      vocals: 'fierce raspy female vocals, howling and whispering, blues phrasing',
      mood: 'intense aggression, dark fairy tale atmosphere', production: '1993 raw analog production',
    },
    bpm: 120,
    lyrics: `[Verse]
I was born in a desert place
Raised on snake and stone

[Chorus]
Bring me my lover tonight
Rid of me, rid of me`,
  },
  {
    id: 'nico',
    name: 'Нико',
    seed: 670105,
    style: 'English, 1960s avant-folk drone, droning cheap guitar, detuned upright piano, dark cello drones, low androgynous female vocals flat heavy delivery no vibrato, vintage 1960s recording, hypnotic repetition, eerie unsettling, 80 BPM',
    slots: {
      language: 'English', genre: 'avant-folk, drone, 1960s',
      rhythm: 'slow, hypnotic',
      guitars: 'droning cheap guitar', keys: 'detuned upright piano, dark cello drones',
      vocals: 'low androgynous female vocals, flat heavy delivery, no vibrato',
      mood: 'eerie unsettling, hypnotic repetition', production: 'vintage 1960s recording',
    },
    bpm: 80,
    lyrics: `[Verse]
These days it is not the same
Folding time on a grey morning

[Chorus]
I will keep it in my coat
Winter lady, cold and slow`,
  },
  {
    id: 'janis',
    name: 'Дженис Джоплин',
    seed: 680106,
    style: 'English, 1960s blues rock soul, pounding drums, raw bluesy guitar licks, Hammond organ, fierce raspy female vocals screaming soul delivery whiskey voice off-key passion, 1968 live band room recording, intense aggression, weary resignation, 110 BPM',
    slots: {
      language: 'English', genre: 'blues rock, soul, 1960s',
      rhythm: 'pounding drums, shuffle groove',
      guitars: 'raw bluesy guitar licks', keys: 'Hammond organ',
      vocals: 'fierce raspy female vocals, screaming soul delivery, whiskey voice, off-key passion',
      mood: 'intense aggression, weary resignation', production: '1968 live band room recording',
    },
    bpm: 110,
    lyrics: `[Verse]
Broken heart on a dirt road
Take another little piece of my heart

[Chorus]
Cry baby, cry baby
The night is long and mean`,
  },
  {
    id: 'katebush',
    name: 'Кейт Буш',
    seed: 780107,
    style: 'English, 1970s art rock, grand piano with prog turns, fretless bass, string quartet pad, high theatrical female vocals wide range soaring airy dramatic melisma, vintage analog recording, dark fairy tale atmosphere, epic grandeur, 105 BPM',
    slots: {
      language: 'English', genre: 'art rock, 1970s',
      rhythm: 'mid-tempo, shifting meters',
      guitars: '', keys: 'grand piano, string quartet pad',
      vocals: 'high theatrical female vocals, wide range, soaring airy, dramatic delivery',
      mood: 'dark fairy tale atmosphere, epic grandeur', production: 'vintage analog recording',
    },
    bpm: 105,
    lyrics: `[Verse]
Heathcliff, it is me, Cathy
Out on the winding, wet moor

[Chorus]
Running up that road
With my hands in the sky`,
  },
  {
    id: 'portishead',
    name: 'Портишед / Бет Гиббонс',
    seed: 940108,
    style: 'English, trip-hop, dusty breaks, dub bassline with spacey delays, tremolo whisper-reverb guitar, airy wounded female vocals breathy aching delivery intimate, 1994 bristol production vinyl crackle, claustrophobic dread, lonely midnight, 90 BPM',
    slots: {
      language: 'English', genre: 'trip-hop',
      rhythm: 'trip-hop beat, dusty breaks',
      guitars: 'tremolo guitar, whisper reverb', keys: 'Rhodes electric piano',
      vocals: 'airy wounded female vocals, breathy aching delivery, intimate',
      mood: 'claustrophobic dread, lonely midnight', production: '1994 bristol production, vinyl crackle',
    },
    bpm: 90,
    lyrics: `[Verse]
Nobody loves me, it is true
Not like you do

[Chorus]
Give me a reason to love you
All of the time`,
  },
  {
    id: 'cranberries',
    name: 'Кранберрис / Долорес',
    seed: 940109,
    style: 'English, 1990s celtic alt-rock, driving chiming jangle guitars, melodic bass, airy lilting female vocals irish keening lilt yodel flips clear bright tone, 1993 spacious studio production, wistful wanderlust, romantic longing, 118 BPM',
    slots: {
      language: 'English', genre: 'celtic alt-rock, 1990s',
      rhythm: 'driving mid-tempo groove',
      guitars: 'jangle pop guitars, chiming', keys: '',
      vocals: 'airy lilting female vocals, irish keening lilt, yodel flips, clear bright tone',
      mood: 'wistful wanderlust, romantic longing', production: '1993 spacious studio',
    },
    bpm: 118,
    lyrics: `[Verse]
Another head hangs lowly
Time is slowly taken

[Chorus]
In your head, the old song
The wars go on and on`,
  },
  {
    id: 'amywinehouse',
    name: 'Эми Уайнхаус',
    seed: 600110,
    style: 'English, retro soul jazz neo-soul, boom bap head-nodding swing, walking upright bass, muted trumpet, warm smoky female vocals deep chesty delivery 60s girl-group harmonies, 2006 analog tape soul production, weary resignation, cynical irony, 88 BPM',
    slots: {
      language: 'English', genre: 'retro soul, jazz neo-soul',
      rhythm: 'boom bap beat, head-nodding swing',
      guitars: '', keys: 'Rhodes, muted trumpet',
      vocals: 'warm smoky female vocals, deep chesty delivery, 60s girl-group harmonies',
      mood: 'weary resignation, cynical irony', production: '2006 analog tape soul production',
    },
    bpm: 88,
    lyrics: `[Verse]
They tried to make me go to rehab
I said no, no, no

[Chorus]
Back to black, my love
Three tears for the days we had`,
  },
  {
    id: 'florence',
    name: 'Флоренс + The Machine',
    seed: 900111,
    style: 'English, baroque pop indie anthemic, pounding tribal drums, harp arpeggios, string quartet swells, soaring powerful female vocals operatic chest belt communal chorus, cathedral reverb huge space, dark fairy tale atmosphere, triumphant epic, 125 BPM',
    slots: {
      language: 'English', genre: 'baroque pop, indie anthemic',
      rhythm: 'pounding drums, tribal gallop',
      guitars: '', keys: 'harp arpeggios, string quartet swells',
      vocals: 'soaring powerful female vocals, operatic chest belt, communal chorus',
      mood: 'dark fairy tale atmosphere, triumphant epic', production: 'cathedral reverb, huge space',
    },
    bpm: 125,
    lyrics: `[Verse]
I was drowning in the river
Looking for a stone to skip

[Chorus]
Raise it up, raise it up
The dog days are over`,
  },
  {
    id: 'siouxie',
    name: 'Сьюкси Сиукс',
    seed: 800112,
    style: 'English, 1980s gothic post-punk, motorik beat, chorus-drenched sharp guitar, hypnotic bass, commanding dramatic female vocals cold sneering delivery deep register, 1980 cold wave production narrow, dark fairy tale atmosphere, hypnotic brooding, 140 BPM',
    slots: {
      language: 'English', genre: 'gothic post-punk, 1980s',
      rhythm: 'motorik krautrock beat',
      guitars: 'chorus-drenched sharp guitar', keys: '',
      vocals: 'commanding dramatic female vocals, cold sneering delivery, deep register',
      mood: 'dark fairy tale atmosphere, hypnotic brooding', production: '1980 cold wave production, narrow',
    },
    bpm: 140,
    lyrics: `[Verse]
Cities in dust, glitter and guilt
Spellbound on a wire

[Chorus]
Dear prudence, come out tonight
The shadows are ours`,
  },
  {
    id: 'nochnye',
    name: 'Ночные снайперы / Арбенина',
    seed: 980113,
    style: 'Russian, 90s russian indie rock new wave, driving bassline, clean arpeggiated guitar, dry drums, androgynous raspy female vocals spoken verses lifting to clipped choruses detached cool delivery, 1998 russian indie production dry, nervous paranoia, lonely midnight, 135 BPM',
    slots: {
      language: 'Russian', genre: 'russian indie rock, new wave',
      rhythm: 'driving bassline, dry drums',
      guitars: 'clean arpeggiated guitar', keys: '',
      vocals: 'androgynous raspy female vocals, spoken verses lifting to clipped choruses',
      mood: 'nervous paranoia, lonely midnight', production: '1998 russian indie production, dry',
    },
    bpm: 135,
    lyrics: femLyrics,
  },
  {
    id: 'pelageya',
    name: 'Пелагея',
    seed: 300114,
    style: 'Russian, russian folk world fusion, ethnic percussion, acoustic guitar strum, folk accordion, powerful female vocals folk belting delivery ornamentation octave shifts village style, warm live room recording, meditative spiritual ballad, dark fairy tale atmosphere, 105 BPM',
    slots: {
      language: 'Russian', genre: 'russian folk, world fusion',
      rhythm: 'ethnic percussion, mid-tempo',
      guitars: 'acoustic guitar strum', keys: 'folk accordion',
      vocals: 'powerful female vocals, folk belting delivery, ornamentation, octave shifts',
      mood: 'meditative spiritual ballad, dark fairy tale atmosphere', production: 'warm live room recording',
    },
    bpm: 105,
    lyrics: femLyrics,
  },
  // ---- электроника / инструментал / этника ----
  // лирика [Instrumental] — трек без вокала (в слоте вокала тоже подкреплено)
  {
    id: 'chillout-bg',
    name: 'Чилаут фоновый',
    seed: 200201,
    style: 'Instrumental, no vocals, downtempo chillout, laid-back dub bass, soft drums building and receding, warm analog synth pad, Rhodes chords, vinyl crackle, spacious wide stereo, dreamy melancholy, 82 BPM',
    slots: {
      language: 'English', genre: 'downtempo, chillout',
      rhythm: 'dub bassline, slow groove',
      guitars: '', keys: 'warm analog synth pad, Rhodes electric piano',
      vocals: 'instrumental, no vocals',
      mood: 'dreamy melancholy, sunny morning', production: 'spacious wide stereo, vinyl crackle',
    },
    bpm: 82,
    lyrics: '[Instrumental]',
  },
  {
    id: 'ambient-relax',
    name: 'Релакс / медитация',
    seed: 200202,
    style: 'Instrumental, no vocals, dark ambient into warm ambient, no percussion, slow evolving drones, mellotron flutes, music box plucks deep in space, cathedral reverb huge space, meditative spiritual, 55 BPM',
    slots: {
      language: 'English', genre: 'ambient, drone',
      rhythm: 'no percussion, extremely slow',
      guitars: '', keys: 'mellotron flutes, warm analog synth pad',
      vocals: 'instrumental, no vocals',
      mood: 'meditative spiritual ballad, cosmic wonder', production: 'cathedral reverb, huge space',
    },
    bpm: 55,
    lyrics: '[Instrumental]',
  },
  {
    id: 'lofi-beats',
    name: 'Lo-fi биты',
    seed: 200203,
    style: 'Instrumental, no vocals, lo-fi hip-hop beats, boom bap head-nodding swing, dusty piano loops, warm vinyl hiss and wobble, tape saturation, rainy window mood, bittersweet nostalgia, 78 BPM',
    slots: {
      language: 'English', genre: 'lo-fi hip-hop',
      rhythm: 'boom bap beat, head-nodding swing',
      guitars: '', keys: 'detuned upright piano, warm vinyl samples',
      vocals: 'instrumental, no vocals',
      mood: 'bittersweet nostalgia, lonely midnight', production: 'lo-fi cassette recording, vinyl hiss and wobble',
    },
    bpm: 78,
    lyrics: '[Instrumental]',
  },
  {
    id: 'bigbeat-guitars',
    name: 'Электроника + жёсткие гитары',
    seed: 200204,
    style: 'Instrumental, no vocals, big beat electronic rock, breakbeat chopped funk drums, aggressive down-tuned distorted guitar riffs, moogy synth bass, pounding relentless, loudness war brickwall, intense aggression, rebellious defiance, 145 BPM',
    slots: {
      language: 'English', genre: 'big beat, industrial rock',
      rhythm: 'breakbeat, chopped funk drums, relentless',
      guitars: 'aggressive down-tuned distorted guitar riffs', keys: 'moogy synth bass',
      vocals: 'instrumental, no vocals',
      mood: 'intense aggression, swaggering confidence', production: 'loudness war brickwall, dense wall of sound',
    },
    bpm: 145,
    lyrics: '[Instrumental]',
  },
  {
    id: 'dark-techno',
    name: 'Тёмный техно',
    seed: 200205,
    style: 'Instrumental, no vocals, dub techno dark warehouse, four-on-the-floor pounding kick, hypotonic dub chords with spacey delays, metallic clangorous hits, cavernous reverb, paranoid claustrophobia, 130 BPM',
    slots: {
      language: 'English', genre: 'dub techno, dark techno',
      rhythm: 'four-on-the-floor drum machine, relentless',
      guitars: '', keys: 'dub chords, spacey delays',
      vocals: 'instrumental, no vocals',
      mood: 'paranoid claustrophobia, hypnotic trance-like repetition', production: 'cavernous reverb, compressed radio sound',
    },
    bpm: 130,
    lyrics: '[Instrumental]',
  },
  {
    id: 'india-mantra',
    name: 'Индия / мантра',
    seed: 200206,
    style: 'Sanskrit chant, indian raga drone, tanpura drone, sitar lines, tabla groove, bansuri flute, low resonant male chant vocals group unison slow, hypnotic trance-like repetition, meditative spiritual, warm live room, 80 BPM',
    slots: {
      language: 'Sanskrit', genre: 'indian raga, devotional chant',
      rhythm: 'tabla groove, slow hypnotic',
      guitars: '', keys: 'tanpura drone, bansuri flute, sitar',
      vocals: 'low resonant male chant, group unison, slow meditative',
      mood: 'meditative spiritual, hypnotic trance-like repetition', production: 'warm live room recording',
    },
    bpm: 80,
    lyrics: `[Verse]
Om namah shivaya
Om namah shivaya

[Chorus]
Om shanti shanti shanti
Om namah shivaya`,
  },
  {
    id: 'ethnic-oriental',
    name: 'Этника восточная',
    seed: 200207,
    style: 'Instrumental, no vocals, oriental world fusion, driving darbuka and frame drum groove, oud and bouzouki lines, duduk wails, warm live room, dark fairy tale atmosphere with sunny morning turn, 105 BPM',
    slots: {
      language: 'English', genre: 'world fusion, oriental',
      rhythm: 'ethnic percussion, driving darbuka groove',
      guitars: 'bouzouki lines', keys: 'oud, duduk',
      vocals: 'instrumental, no vocals',
      mood: 'dark fairy tale atmosphere, wistful wanderlust', production: 'warm live room recording',
    },
    bpm: 105,
    lyrics: '[Instrumental]',
  },
  {
    id: 'romantic-theme',
    name: 'Романтическая тема',
    seed: 200208,
    style: 'Instrumental, no vocals, cinematic love theme, grand piano melody, string quartet swells, warm analog synth pad under, soft brushes, spacious wide stereo, romantic longing, tender lullaby, 72 BPM',
    slots: {
      language: 'English', genre: 'cinematic ballad, neoclassical',
      rhythm: 'slow, soft drums building and receding',
      guitars: 'clean plucked guitar, intimate', keys: 'grand piano, string quartet pad',
      vocals: 'instrumental, no vocals',
      mood: 'romantic longing, tender lullaby', production: 'spacious wide stereo, vintage analog recording',
    },
    bpm: 72,
    lyrics: '[Instrumental]',
  },
]
