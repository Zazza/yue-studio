// Откалиброванные пресеты для групп библиотеки стилей.
// slots — декомпозиция для правки отдельных полей (скомпилируется заново);
// style — точная исходная строка калибровки (кнопка «строкой» — 1:1 воспроизведение).

export const presets = [
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
