// Синт-партия по аккордам трека — чистая логика без DOM: такты {start, end, chord, section} (GET chord_grid) →
// ноты [{t, d, midi, vel}] (секунды трека) для блока движка synth. Стили: pad — аккорд на такт, arp — восьмые по
// кругу трезвучия, pulse — основной тон восьмыми (синт-бас), drone — основной тон + квинта, тянется, пока аккорд тот же.

const ROOT = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 }
const QUAL = { '': [4, 7], maj: [4, 7], m: [3, 7], min: [3, 7], dim: [3, 6], aug: [4, 8], 7: [4, 7, 10], maj7: [4, 7, 11],
  m7: [3, 7, 10], sus2: [2, 7], sus4: [5, 7], 5: [7] }
const CHORD_RE = /^([A-G])([#b]?)(maj7|maj|min|m7|m|dim|aug|sus2|sus4|7|5)?(?:\/[A-G][#b]?)?$/

/** «Dm» → [2, 5, 9] (основной тон, терция, квинта, …); нераспознанный — null. */
export function chordPcs(name) {
  const m = CHORD_RE.exec(String(name || '').trim())
  if (!m) return null
  const root = (ROOT[m[1]] + ({ '#': 1, b: -1 }[m[2]] || 0) + 12) % 12
  return [root, ...QUAL[m[3] || ''].map((i) => (root + i) % 12)]
}

const BASE = { pad: 60, arp: 60, drone: 60, pulse: 36 }   // нижняя нота регистра: C4 или C2

/** Ноты партии: style pad|arp|pulse|drone, octave −2…2, sections — список секций (null — все). */
export function partNotes(bars, { style = 'pad', octave = 0, sections = null } = {}) {
  const base = (BASE[style] ?? 60) + 12 * Math.max(-2, Math.min(2, Math.round(octave || 0)))
  const inReg = (pc) => base + pc            // класс высоты → нота в регистре [base, base+11]
  const out = []
  let drone = null
  const flush = () => { if (drone) { out.push(drone); drone = null } }
  for (const b of bars || []) {
    const pcs = chordPcs(b.chord)
    const on = !sections || sections.includes(b.section)
    const len = b.end - b.start
    if (!pcs || !on || !(len > 0)) { flush(); continue }
    const triad = pcs.slice(0, 3).map(inReg).sort((x, y) => x - y)
    // пэд — аккорд целиком (с септимой, если есть); арпеджио — по трезвучию
    if (style === 'pad') out.push({ t: b.start, d: len, midi: pcs.map(inReg).sort((x, y) => x - y), vel: 0.8 })
    else if (style === 'arp') {
      for (let i = 0; i < 8; i++) out.push({ t: b.start + (i * len) / 8, d: len / 8, midi: [triad[i % triad.length]], vel: i % 2 ? 0.65 : 0.8 })
    } else if (style === 'pulse') {
      for (let i = 0; i < 8; i++) out.push({ t: b.start + (i * len) / 8, d: len / 8, midi: [inReg(pcs[0])], vel: i % 2 ? 0.7 : 0.85 })
    } else if (style === 'drone') {
      const midi = [inReg(pcs[0]), inReg((pcs[0] + 7) % 12)].sort((x, y) => x - y)   // квинта — в том же регистре
      if (drone && drone.chord === b.chord && Math.abs(drone.t + drone.d - b.start) < 1e-6) drone.d += len
      else { flush(); drone = { t: b.start, d: len, midi, vel: 0.75, chord: b.chord } }
      continue
    }
    flush()
  }
  flush()
  return out.map(({ chord: _c, ...n }) => n)
}
