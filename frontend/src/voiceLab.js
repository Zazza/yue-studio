// Примерочная голосов: чистая логика «ручки → дескриптор вокала».
// YuE2 кондиционируется только текстом, поэтому ручки — детерминированный
// маппинг в английские теги; дескриптор пересчитывается из params всегда
// на клиенте (на воркере хранятся только params и seed).
import { buildStyleLine } from './styleLogic.js'

export const PARAMS_VERSION = 1

// Регистр-выбор для VSelect (лейблы — voicelab.register.* в i18n).
export const VOICE_REGISTERS = ['female', 'male', 'male-high', 'male-low']

// Слайдеры: key + максимум уровня. Подписи и названия уровней — voicelab.<key>.*
export const VOICE_KNOBS = [
  { key: 'rough', max: 3 },
  { key: 'creak', max: 2 },
  { key: 'delivery', max: 3 },
  { key: 'breath', max: 2 },
]

const REGISTER_TOKENS = {
  'female': 'female vocals',
  'male': 'male vocals',
  'male-high': 'high tenor male vocals',
  'male-low': 'deep resonant low male vocals',
}
const ROUGH_TOKENS = ['', 'raspy', 'gritty raspy', 'gravelly growled']
const CREAK_TOKENS = ['', 'creaky', 'vocal fry']
const DELIVERY_TOKENS = ['soft intimate delivery', 'even delivery', 'powerful delivery',
  'raw breaking delivery, on the verge of tears']
const BREATH_TOKENS = ['', 'breathy', 'whispered']

// Стенд прослушивания: фиксированный контекст, чтобы голоса были сравнимы
// между собой — меняется только дескриптор вокала. Свой ABC-план (механизм
// req_abc воркера): без интро, припев с первой доли — иначе draft-проба (~18 с,
// это начало трека) уходила на инструментальное интро и слов не было слышно.
// Формат подсмотрен в планах самой модели (score.abc): секции комментариями,
// Vocal/Ins по голосам, L:1/16 — длительности в 16-х.
export const AUDITION_BED = {
  slots: {
    language: 'English',
    genre: 'energetic pop rock',
    rhythm: 'driving mid-tempo beat',
    guitars: '',
    keys: '',
    mood: '',
    production: 'clean production, vocals up front',
    bpm: 110,
  },
  lyrics: '[Chorus]\nTake it all, take it now\nSay the only word out loud\nBurn it down and make it true\nNothing left but me and you\n',
  cot: 'full',
  abc: [
    'X:1',
    'T:voice audition',
    'M:4/4',
    'L:1/16',
    'Q:1/4=110',
    'V: Vocal clef=treble name="Vocal Melody" snm="Vocal"',
    'V: Ins clef=treble name="Ins Melody" snm="Inst."',
    'K:Dm',
    '% chorus',
    'V: Vocal',
    '"Dm"z4d2f2a2a2f2d2|"Bb"z4d2f2g2g2f2d2|"F"z4c2f2a2a2g2f2|"C"z4e2g2g2e2d2e2|',
    'V: Ins',
    '"Dm"d4f2d2f2g2gfd2|"Bb"d4f2d2f2g2gfd2|"F"c4a2f2a2g2gfc2|"C"e4g2e2g2a2age2|',
    'V: Vocal',
    '"Dm"z4d2f2a2a2f2d2|"Bb"z4d2f2g2g2f2d2|"F"z4c2f2a2a2g2f2|"C"z4e2g2g2e2d2e2|',
    'V: Ins',
    '"Dm"d4f2d2f2g2gfd2|"Bb"d4f2d2f2g2gfd2|"F"c4a2f2a2g2gfc2|"C"e4g2e2g2a2age2|',
    '',
  ].join('\n'),
}

export function defaultVoiceParams() {
  return { register: 'female', rough: 0, creak: 0, delivery: 1, breath: 0, extra: '' }
}

// Быстрые пресеты: задают стартовые ручки + «своё», дальше пользователь
// докручивает. Это данные, не поведение: страница применяет их через
// normalizeVoiceParams. Имена — voicelab.preset.<id> в i18n.
export const VOICE_PRESETS = [
  {
    id: 'operatic',
    params: { register: 'female', rough: 0, creak: 0, delivery: 2, breath: 0,
      extra: 'operatic soprano, symphonic' },
  },
  {
    id: 'dark-drama',
    params: { register: 'female', rough: 1, creak: 0, delivery: 3, breath: 0,
      extra: 'dramatic dark, soaring powerful' },
  },
  {
    id: 'scream',
    params: { register: 'female', rough: 2, creak: 0, delivery: 3, breath: 0,
      extra: 'screamed chorus, desperate' },
  },
  {
    id: 'whisper',
    params: { register: 'female', rough: 0, creak: 0, delivery: 0, breath: 2, extra: '' },
  },
  {
    id: 'velvet-low',
    params: { register: 'male-low', rough: 1, creak: 1, delivery: 2, breath: 0, extra: '' },
  },
  {
    id: 'reset',
    params: defaultVoiceParams(),
  },
]

// Вход из карточки/MCP может быть любым: клампим ручки, белый список регистров,
// недостающее добираем из дефолта.
export function normalizeVoiceParams(o) {
  const d = defaultVoiceParams()
  const src = (o && typeof o === 'object') ? o : {}
  const clamp = (v, max) => {
    const n = Number.parseInt(v, 10)
    return Number.isFinite(n) ? Math.min(Math.max(n, 0), max) : 0
  }
  const out = { ...d, register: VOICE_REGISTERS.includes(src.register) ? src.register : d.register }
  for (const k of VOICE_KNOBS) out[k.key] = clamp(src[k.key], k.max)
  out.extra = typeof src.extra === 'string' ? src.extra.trim() : ''
  return out
}

// Архотипы «в духе»: подпись в UI может называть артиста, но сами теги —
// обезличенные клише (в промпт имена не идут, модель их всё равно не знает).
export const VOICE_ARCHETYPES = [
  {
    id: 'letov', params: { register: 'male', rough: 2, creak: 1, delivery: 3, breath: 0,
      extra: 'nasal, cracked, shouted, off-key' },
  },
  {
    id: 'thom-yorke', params: { register: 'male-high', rough: 0, creak: 0, delivery: 0, breath: 1,
      extra: 'nasal, frail falsetto, anxious, mournful' },
  },
  {
    id: 'jack-white', params: { register: 'male-high', rough: 1, creak: 0, delivery: 3, breath: 0,
      extra: 'piercing whiny wail, nasal, manic' },
  },
  {
    id: 'placebo', params: { register: 'male-high', rough: 0, creak: 0, delivery: 2, breath: 0,
      extra: 'androgynous, sneering, seductive' },
  },
  {
    id: 'interpol', params: { register: 'male', rough: 0, creak: 0, delivery: 1, breath: 0,
      extra: 'deadpan baritone, cold, cryptic, unsettling' },
  },
  {
    id: 'zemfira', params: { register: 'female', rough: 1, creak: 0, delivery: 2, breath: 0,
      extra: 'husky, laid-back phrasing, confident' },
  },
  {
    id: 'mumiy-troll', params: { register: 'male-high', rough: 0, creak: 0, delivery: 1, breath: 1,
      extra: 'purring, mannered, drawling, playful' },
  },
  {
    id: 'cobain', params: { register: 'male', rough: 2, creak: 1, delivery: 3, breath: 0,
      extra: 'grunge whine, cracked, desperate' },
  },
  {
    id: 'nick-cave', params: { register: 'male-low', rough: 1, creak: 0, delivery: 2, breath: 0,
      extra: 'deep baritone, ominous, sermonizing' },
  },
  {
    id: 'amy-winehouse', params: { register: 'female', rough: 1, creak: 0, delivery: 2, breath: 0,
      extra: 'smoky soul, jazzy phrasing, brazen' },
  },
  {
    id: 'bjork', params: { register: 'female', rough: 0, creak: 0, delivery: 3, breath: 0,
      extra: 'eccentric, wild leaps, child-like' },
  },
  {
    id: 'lou-reed', params: { register: 'male', rough: 1, creak: 1, delivery: 1, breath: 0,
      extra: 'talk-singing, deadpan, dry' },
  },
  {
    id: 'vysocky', params: { register: 'male', rough: 3, creak: 2, delivery: 3, breath: 0,
      extra: 'rasping, straining, theatrical intensity' },
  },
  {
    id: 'bg', params: { register: 'male', rough: 0, creak: 0, delivery: 1, breath: 0,
      extra: 'cool, detached, enigmatic' },
  },
]

// Кириллица в extra — нужен перевод перед отправкой (как autoTranslate в форме).
export function needsTranslate(s) {
  return /[а-яё]/i.test(s || '')
}

// Ручки → английский дескриптор вокального слота. Детерминирован: одинаковые
// params → одинаковая строка. Порядок фиксирован: регистр, скрип, грубость,
// дыхание, подача; extra — последним, своим текстом.
export function voiceDescriptor(p) {
  const n = normalizeVoiceParams(p)
  const parts = [REGISTER_TOKENS[n.register]]
  if (n.creak > 0) parts.push(CREAK_TOKENS[n.creak])
  if (n.rough > 0) parts.push(ROUGH_TOKENS[n.rough])
  // шёпот побеждает экстремальную подачу: «на разрыв» шёпотом не бывает
  if (n.breath === 2 && n.delivery >= 3) {
    parts.push('hushed whispered delivery')
  } else {
    if (n.breath > 0) parts.push(BREATH_TOKENS[n.breath])
    parts.push(DELIVERY_TOKENS[n.delivery])
  }
  if (n.extra) parts.push(n.extra)
  return parts.join(', ')
}

// Строка стиля для джобы-прослушивания: стенд + дескриптор вокала.
export function auditionStyle(vocalsDescriptor) {
  return buildStyleLine({ ...AUDITION_BED.slots, vocals: vocalsDescriptor || '' })
}

// Полный payload для api.submit: draft-превью на стенде с конкретным seed
// и фиксированным планом без интро. Seed обязан быть конкретным числом (не 0):
// воркер не пишет обратно использованный случайный seed, воспроизводимость
// голоса — только через seed.
export function auditionJob(p, seed) {
  return {
    title: 'voice audition',
    style: auditionStyle(voiceDescriptor(p)),
    lyrics: AUDITION_BED.lyrics,
    seed,
    cot: AUDITION_BED.cot,
    abc: AUDITION_BED.abc,
    draft: true,
  }
}
