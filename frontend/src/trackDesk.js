// Пульт дорожек студии — чистая логика без DOM: строки дорожек с их правками, окно «было/стало»,
// окно записи в трек, готовые цепочки для дорожки. Компонент — components/TrackDesk.vue.
import { DRUM_KITS, DRUM_TREATMENTS, PART_NAMES, ROOM, kitSampler } from './drumKits.js'

// порядок строк: голос, барабаны и их части, бас, гитара, клавиши, «прочее»
export const DESK_ORDER = ['vocals', 'drums', 'kick', 'snare', 'toms', 'hh', 'ride', 'crash', 'bass', 'guitar', 'piano', 'other']

const PREVIEW_MIN = 3   // остаток до конца меньше — окно сдвигается влево

/** Строки пульта: только дорожки трека, известные — в DESK_ORDER, прочие — в конце по алфавиту;
 *  edits — записи реестра с этой дорожкой в stems (выключенные тоже). */
export function deskRows(stemNames, applied) {
  const names = [...new Set(stemNames || [])]
  const known = DESK_ORDER.filter((s) => names.includes(s))
  const other = names.filter((s) => !DESK_ORDER.includes(s)).sort()
  return [...known, ...other].map((stem) => ({
    stem,
    edits: (applied || []).filter((it) => (it.stems || []).includes(stem)),
  }))
}

const hasSel = (sel) => !!sel && Number(sel.to) > Number(sel.from)

/** Окно «было/стало»: выделение как есть; без него — len секунд от курсора в пределах трека
 *  (у конца — сдвиг влево, чтобы кусок был не короче min(len, dur)); длина неизвестна — без обрезки. */
export function previewWindow(sel, cursor, dur, len = 15) {
  if (hasSel(sel)) return { from: sel.from, to: sel.to }
  const from = Math.max(0, Number(cursor) || 0)
  if (!(dur > 0)) return { from, to: from + len }
  const to = Math.min(from + len, dur)
  if (dur - from < PREVIEW_MIN) return { from: Math.max(0, dur - Math.min(len, dur)), to: dur }
  return { from: Math.min(from, dur), to }
}

/** Окно записи в трек: выделение или весь трек ({0, 0}). */
export function applyWindow(sel) {
  return hasSel(sel) ? { from: sel.from, to: sel.to } : { from: 0, to: 0 }
}

/** Готовые цепочки для дорожки: пресет без stems (или с пустым) подходит всем; порядок исходный. */
export function presetsFor(stem, presets) {
  return (presets || []).filter((p) => !(p.stems && p.stems.length) || p.stems.includes(stem))
}

// ритм-секция набором: часть → готовая цепочка «набором» (fxPresets), порядок — как в кнопке пульта
const RHYTHM = [['kick', 'drums-kick-kit'], ['snare', 'drums-snare-kit'], ['toms', 'drums-toms-kit'],
  ['hh', 'drums-hh-kit'], ['ride', 'drums-ride-kit'], ['crash', 'drums-crash-kit'], ['bass', 'bass-kit']]
export { ROOM }

/** Ритм-секция одной записью: для частей барабанов и баса, которые есть у трека, — {stem, chain, label}.
 *  room — true/false (комната/сухо, как раньше) или id обработки DRUM_TREATMENTS (неизвестный — сухо): цепочка
 *  обработки — в конец каждой части барабанов, к басу — нет. kit — набор DRUM_KITS: 'osdk' — прежние готовые
 *  цепочки; машина — sampler с частями набора и прочими параметрами osdk той же части. Нет ни одной — []. */
export function rhythmSection(stemNames, presets, room, locale = 'ru', kit = 'osdk') {
  const have = new Set(stemNames || [])
  const tid = room === true ? 'room' : (room === false || room == null ? 'dry' : String(room))
  const treat = DRUM_TREATMENTS.find((x) => x.id === tid) || DRUM_TREATMENTS[0]
  const set = DRUM_KITS.find((k) => k.id === kit) || DRUM_KITS[0]
  const tr = (l) => l[locale] || l.ru
  const out = []
  for (const [stem, id] of RHYTHM) {
    const p = (presets || []).find((x) => x.id === id)
    if (!have.has(stem) || !p) continue
    let chain = JSON.parse(JSON.stringify(p.chain))
    if (stem === 'bass') {
      out.push({ stem, chain, label: tr(p.name) })
      continue
    }
    let label = tr(p.name)
    if (set.id !== 'osdk') {
      chain = [kitSampler(chain[0], set.parts[stem], stem)]
      label = `${tr(set.name)} · ${tr(PART_NAMES[stem])}`
    }
    chain.push(...JSON.parse(JSON.stringify(treat.chain)))
    if (treat.id !== 'dry') label += ` + ${tr(treat.name)}`
    out.push({ stem, chain, label })
  }
  return out
}

// основные дорожки: их сумма — весь трек (части барабанов, гитара и клавиши — внутри них)
const MAIN_STEMS = ['vocals', 'drums', 'bass', 'other']
const p95 = (s) => {
  const v = s && s.metrics && s.metrics.metrics && s.metrics.metrics.rms_p95_db
  return typeof v === 'number' && Number.isFinite(v) ? v : null
}

/** Громкость громких мест каждой дорожки (rms_p95_db замера воркера) относительно всего трека — суммы мощностей
 *  основных дорожек с замером: {имя: дБ}. Дорожка без замера в ответ не идёт; нет основных — {}. */
export function stemLevels(stems) {
  const list = stems || []
  const main = list.filter((s) => MAIN_STEMS.includes(s.name) && p95(s) != null)
  if (!main.length) return {}
  const total = 10 * Math.log10(main.reduce((a, s) => a + 10 ** (p95(s) / 10), 0))
  const out = {}
  for (const s of list) if (p95(s) != null) out[s.name] = p95(s) - total
  return out
}

/** Слышна ли дорожка: тише −30 дБ к треку — 'silent' (место и обработку не услышать), до −20 — 'quiet', иначе ''. */
export function stemAudibility(db) {
  if (db < -30) return 'silent'
  if (db < -20) return 'quiet'
  return ''
}
