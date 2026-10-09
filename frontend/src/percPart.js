// Перкуссия по сетке трека — чистая логика без DOM: такты {start, end, section} (chord_grid или barsFromBeat) →
// удары [{t, d, vel}] (секунды трека) для блока движка perc. Такт — 4 равные доли; d — длина клетки рисунка.

// рисунок: клеток в такте и какие из них играют (номер клетки → играет?)
const PATTERNS = {
  fours: { cells: 4, on: () => true },
  eighths: { cells: 8, on: () => true },
  sixteenths: { cells: 16, on: () => true },
  backbeat: { cells: 4, on: (i) => i % 2 === 1 },
  offbeat: { cells: 8, on: (i) => i % 2 === 1 },
}
export const PERC_PATTERNS = Object.keys(PATTERNS)

const STRONG = 0.9   // доля
// сила клетки при accent 1: доля .9, восьмая между долями .6, шестнадцатая между восьмыми .4
function baseVel(i, cells) {
  const per = cells / 4                 // клеток в доле
  const k = i % per
  if (k === 0) return STRONG
  return per === 4 && k % 2 === 1 ? 0.4 : 0.6
}

/** Удары партии: pattern — один из PERC_PATTERNS; sections — список секций (пусто/нет — все такты);
 *  swing 0…0,5 — вторая клетка пары (восьмые/шестнадцатые между) позже на swing × клетка; accent 0…1 — от
 *  ровного (все .9) до полного рисунка силы. */
export function percHits(bars, { pattern = 'eighths', sections = null, swing = 0, accent = 1 } = {}) {
  const pat = PATTERNS[pattern] || PATTERNS.eighths
  const sw = Math.max(0, Math.min(0.5, Number(swing) || 0))
  const ac = Math.max(0, Math.min(1, accent ?? 1))
  const all = !sections || !sections.length
  const out = []
  for (const b of bars || []) {
    const len = b.end - b.start
    if (!(len > 0) || (!all && !sections.includes(b.section))) continue
    const cell = len / pat.cells
    for (let i = 0; i < pat.cells; i++) {
      if (!pat.on(i)) continue
      // свинг — у вторых клеток пар (восьмые и шестнадцатые между); четверти не свингуют
      const late = pat.cells > 4 && i % 2 === 1 ? sw * cell : 0
      const vel = STRONG - ac * (STRONG - baseVel(i, pat.cells))
      out.push({ t: b.start + i * cell + late, d: cell, vel: Math.round(vel * 1000) / 1000 })
    }
  }
  return out
}

/** Такты без плана — по темпу и доле (/grid): по 4 доли от offset + shift долей до конца трека (последний —
 *  неполный), section ''. /grid даёт фазу доли, а не начало такта: shift 0…3 — с какой доли такт (иначе «на 2 и 4»
 *  ложится на 1 и 3). */
export function barsFromBeat({ bpm, offset = 0 } = {}, dur, shift = 0) {
  const len = 240 / bpm
  const out = []
  if (!(len > 0) || !(dur > 0)) return out
  const k0 = Math.max(0, Math.min(3, Math.round(Number(shift) || 0)))
  const o = Math.max(0, offset) + (k0 * len) / 4
  for (let k = 0; dur - (o + k * len) > 1e-6; k++) out.push({ start: o + k * len, end: Math.min(o + (k + 1) * len, dur), section: '' })
  return out
}
