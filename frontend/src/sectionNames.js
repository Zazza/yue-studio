// Подписи секций песни для интерфейса. Модель пишет секции английскими словами
// в свободной форме («blues guitar solo», «drum fill», «verse 2») — в плане и на
// волне показываем по-русски, неизвестное — как есть.

const RU = [
  [/^pre[\s-]?chorus$/, 'предприпев'],
  [/^(chorus|hook)$/, 'припев'],
  [/^verse$/, 'куплет'],
  [/^intro$/, 'вступление'],
  [/^outro$/, 'концовка'],
  [/^bridge$/, 'бридж'],
  [/^(interlude|instrumental)$/, 'проигрыш'],
  [/^breakdown$/, 'брейк'],
  [/^(drum\s+)?fill$/, 'сбивка'],
]

function soloLabel(name) {
  if (/\bguitar\b/.test(name)) return 'соло гитары'
  if (/\b(piano|keys|synth)\b/.test(name)) return 'соло клавиш'
  return 'соло'
}

export function sectionLabel(raw, locale = 'ru') {
  const src = String(raw ?? '').trim()
  if (!src || locale !== 'ru') return src
  // номер в конце («verse 2», «Chorus2») сохраняется
  const m = src.toLowerCase().replace(/\s+/g, ' ').match(/^(.*?)\s*(\d+)?$/)
  const name = m[1]
  const num = m[2] ? ' ' + m[2] : ''
  if (/\bsolo\b/.test(name)) return soloLabel(name) + num
  for (const [re, ru] of RU) if (re.test(name)) return ru + num
  return src
}
