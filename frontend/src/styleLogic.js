// Чистая логика сборки строки стиля и подготовки стиха к отправке.
// Вынесена из App.vue для тестируемости.
import { slotDict, durOptions } from './slotOptions.js'

// порядок слотов в строке стиля — язык/жанр первыми, продакшн последним
export const SLOT_ORDER = [
  'language', 'genre', 'rhythm', 'guitars', 'keys', 'vocals', 'mood', 'production',
]

// строка стиля из слотов (+ строка стойки, + BPM): пустые слоты пропускаются
export function buildStyleLine(slots, rackStr = '') {
  const parts = SLOT_ORDER
    .map(k => (slots[k] || '').trim())
    .filter(Boolean)
  if (rackStr) parts.push(rackStr)
  if (slots.bpm) parts.push(slots.bpm + ' BPM')
  return parts.join(', ')
}

// значение слота → английский тег: известное русское подставляется словарём,
// неизвестное (в т.ч. самописное) проходит как есть
export function dictStyle(s) {
  const t = (s || '').trim()
  return slotDict[t.toLowerCase()] || t
}

// стих к отправке: строки-пометки (начинаются с #) вырезаются — это заметки
// для себя (ударения, произношение), модель их не поёт
export function cleanLyrics(text) {
  return text.split('\n').filter(l => !l.trim().startsWith('#')).join('\n')
}

// «без слов»: вместо стиха — n секций [Instrumental] (n из режима длительности)
export function instrumentalLyrics(durMode) {
  const opt = durOptions.find(o => o.id === durMode) || durOptions[0]
  return Array(Math.max(1, opt.n)).fill('[Instrumental]').join('\n\n')
}

export function effectiveLyrics(lyrics, noLyrics, durMode) {
  return noLyrics ? instrumentalLyrics(durMode) : cleanLyrics(lyrics)
}
