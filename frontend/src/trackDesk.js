// Пульт дорожек студии — чистая логика без DOM: строки дорожек с их правками, окно «было/стало»,
// окно записи в трек, готовые цепочки для дорожки. Компонент — components/TrackDesk.vue.

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
// «комната» — короткое помещение вокруг сухих сэмплов барабанов (то же, что у пресета «Живая ритм-секция»)
export const ROOM = { type: 'reverb', decay_s: 0.5, predelay_ms: 5, lowpass_hz: 7000, wet: 0.12 }

/** Ритм-секция одной записью: для частей барабанов и баса, которые есть у трека, — {stem, chain, label};
 *  room — к частям барабанов (не к басу) в конец комната; locale — язык подписи. Нет ни одной — []. */
export function rhythmSection(stemNames, presets, room, locale = 'ru') {
  const have = new Set(stemNames || [])
  const out = []
  for (const [stem, id] of RHYTHM) {
    const p = (presets || []).find((x) => x.id === id)
    if (!have.has(stem) || !p) continue
    const chain = JSON.parse(JSON.stringify(p.chain))
    if (room && stem !== 'bass') chain.push({ ...ROOM })
    const name = p.name[locale] || p.name.ru
    out.push({ stem, chain, label: name + (room && stem !== 'bass' ? (locale === 'en' ? ' + room' : ' + комната') : '') })
  }
  return out
}
