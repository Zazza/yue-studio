// Страница «Инструменты»: фраза играет по кругу, цепочка крутится на ходу. Чистая логика без DOM:
// семья фразы по пресету, части фразы под цепочку, ноты синта/перкуссии по тактам круга и место в круге после
// пересчёта.
import { partNotes } from './synthPart.js'
import { percHits } from './percPart.js'

export const DRUM_PARTS = ['kick', 'snare', 'hh', 'toms', 'ride', 'crash']
const GUITAR = ['guitar', 'other', 'piano']

// семья фраз для пресета (по его дорожкам): guitar | bass | drums | synth | perc; голос, мастер — null
export function familyOf(preset) {
  const stems = (preset && preset.stems) || []
  if (!stems.length) return null
  if (stems.every((s) => s === 'synth')) return 'synth'
  if (stems.every((s) => s === 'perc')) return 'perc'
  // мелодия-замена (гитара/прочее/голос) слышна на гитарной фразе; голос сам по себе — не инструмент страницы
  if (stems.every((s) => GUITAR.includes(s) || s === 'vocals') && stems.some((s) => s !== 'vocals')) return 'guitar'
  if (stems.every((s) => s === 'bass')) return 'bass'
  if (stems.every((s) => s === 'drums' || DRUM_PARTS.includes(s))) return 'drums'
  return null
}

// части фразы, на которые ложится цепочка: барабаны целиком — все части, часть барабанов — только она
export function phraseStems(preset) {
  const fam = familyOf(preset)
  if (fam === 'guitar') return ['guitar']
  if (fam === 'bass') return ['bass']
  if (fam === 'synth') return ['synth']
  if (fam === 'perc') return ['perc']
  if (fam === 'drums') {
    const stems = preset.stems
    return stems.includes('drums') ? [...DRUM_PARTS] : stems.filter((s) => DRUM_PARTS.includes(s))
  }
  return []
}

// пресеты, которые эта страница умеет сыграть фразой
export function pagePresets(list) {
  return (list || []).filter((p) => familyOf(p) !== null)
}

// место в новом круге: доля старого круга сохраняется (темп сменился — круг другой длины, доля та же)
// phaseStart — с какого места круга запущен файл, posSec — сколько он играет
export function phaseAfter(phaseStart, posSec, cycleOld, cycleNew) {
  if (!(cycleOld > 0) || !(cycleNew > 0)) return 0
  const at = (((phaseStart + posSec) % cycleOld) + cycleOld) % cycleOld
  const v = (at / cycleOld) * cycleNew
  return v >= cycleNew ? 0 : v
}

// семья фраз, которые играет пресет: перкуссия ложится поверх барабанного бита
export function phraseFamily(fam) {
  return fam === 'perc' ? 'drums' : fam
}

// такты круга {start, end, chord} при темпе tempo: такт — 4 доли, аккорды фразы по кругу
export function phraseBars(phrase, tempo = 1) {
  const n = Math.max(1, Math.round((phrase.beats || 16) / 4))
  const len = phrase.cycle_sec / (tempo || 1) / n
  const chords = phrase.chords || []
  return Array.from({ length: n }, (_, i) => ({ start: i * len, end: (i + 1) * len,
    chord: chords.length ? chords[i % chords.length] : '' }))
}

// цепочка для круга: блокам synth — ноты стиля фразы (или пресета) по аккордам тактов, блокам perc — удары рисунка
// пресета; прочие блоки как есть. Новые объекты: цепочка редактора не меняется
export function phraseChain(chain, preset, phrase, tempo = 1) {
  if (!phrase || !(phrase.cycle_sec > 0)) return chain
  const bars = phraseBars(phrase, tempo)
  const p = preset || {}
  return chain.map((b) => {
    if (b.type === 'synth') {
      return { ...b, notes: partNotes(bars, { style: phrase.style || p.style || 'pad', octave: p.octave || 0 }) }
    }
    if (b.type === 'perc') {
      return { ...b, notes: percHits(bars, { pattern: p.pattern || 'eighths', swing: p.swing || 0, accent: p.accent ?? 1 }) }
    }
    return b
  })
}
