// Разбор строки стиля джобы (запятая-разделённые английские теги YuE)
// в группы по смыслу: язык, жанр, ритм, гитары, клавиши, голос, настроение,
// звучание, форма песни, темп. Чистая функция для карточки трека.
import { slotOptions } from './slotOptions.js'

const BPM_RE = /\b\d{2,3}\s*bpm\b/i

// en-теги словарей по полям, длинные вперед: «post-punk revival» точнее «post-punk»
const dictBySlot = Object.entries(slotOptions).map(([slot, opts]) => ({
  slot,
  tags: opts.map(([, en]) => en.trim().toLowerCase()).sort((a, b) => b.length - a.length),
}))

// маркерные слова — когда фраза не совпала со словарём целиком
const markers = [
  ['structure', /\b(chorus|verses?|bridge|outro|intro|ending|coda|breakdown)\b/i],
  ['guitars', /\b(guitar|guitars|riff|solo|arpeggios?|chords?|picked|strummed|palm.?mute|slide|bends?|bass|bassline)\b/i],
  ['keys', /\b(synth|synths|piano|organ|keyboard|pad|harpsichord|melodica)\b/i],
  ['vocals', /\b(male|female|vocal|vocals|tenor|baritone|soprano|alto|voice|choir|a\s?cappella|falsetto|shout|whisper|delivery|sung|range|instrumental)\b/i],
  ['rhythm', /\b(beat|pulse|groove|breakbeat|drums?|percussion|motorik|swing|shuffle|waltz|polka)\b/i],
  ['production', /\b(mix|reverb|delay|distortion|fuzz|lo-?fi|punchy|dry|wet|production|recorded|tape|vinyl|analog|mastering|compression)\b/i],
  // основы слов без границы в конце: melanchol → melancholic, nostalg → nostalgic
  ['mood', /\b(melanchol|dark|bright|nostalg|epic|desperat|doom|romance|night|city|dream|sad|happy|angry|cold|warm|gloom|energetic|urgent|dramatic|soaring|intense|tense|calm|mournful)/i],
]

const INSTRUMENT_RE = {
  guitars: /\b(guitars?|bass|bassline)\b/i,
  keys: /\b(synths?|piano|organ|keyboards?)\b/i,
}

const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

function classify(phrase) {
  if (BPM_RE.test(phrase)) return 'bpm'
  const low = phrase.toLowerCase()
  // самое длинное совпадение со словарём среди всех полей: «slow … acoustic guitar
  // arpeggios» — гитары («acoustic guitar»), а не ритм по короткому «slow»
  let best = null
  for (const { slot, tags } of dictBySlot) {
    if (tags.includes(low)) return slot
    for (const tag of tags) {
      // вхождение по границе слова: «post-punk revival» содержит «post-punk»
      if (tag.length >= 4 && (!best || tag.length > best.len) &&
          new RegExp(`\\b${escapeRe(tag)}\\b`).test(low)) best = { slot, len: tag.length }
    }
  }
  // фраза про инструмент («detuned blues slide guitar»): словарь нашёл лишь
  // прилагательное из жанра/ритма/настроения — решает названный инструмент
  if (best && !['guitars', 'keys'].includes(best.slot)) {
    if (INSTRUMENT_RE.guitars.test(low)) return 'guitars'
    if (INSTRUMENT_RE.keys.test(low)) return 'keys'
  }
  if (best) return best.slot
  for (const [slot, re] of markers) {
    if (re.test(phrase)) return slot
  }
  return 'other'
}

// 'English, post-punk, 138 BPM' → { language: ['English'], genre: ['post-punk'], bpm: ['138 BPM'] }
export function parseStyleTags(style) {
  const out = {}
  for (const raw of String(style || '').split(',')) {
    const phrase = raw.trim()
    if (!phrase) continue
    const slot = classify(phrase)
    ;(out[slot] ||= []).push(phrase)
  }
  return out
}

// порядок вывода групп на карточке: поля формы, потом форма/темп/прочее
export const styleTagOrder = [...Object.keys(slotOptions), 'structure', 'bpm', 'other']

// [слот, фразы] в порядке вывода — пустые группы выброшены
export function styleRows(style) {
  const tags = parseStyleTags(style)
  return styleTagOrder.filter((k) => tags[k]).map((k) => [k, tags[k]])
}
