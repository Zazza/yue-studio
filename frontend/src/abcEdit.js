// Приёмы над ABC-планом: разбор на такты с сохранением текста и точечные
// мутации — «не тот аккорд» (заимствованный от тональности), провал-пауза,
// вырезание тактов, октава голосу. Диалект как у воркера (abcparse.py/arc.py):
// секции — % комментарии, голоса — V: <Имя> ..., такты через |, аккорды —
// "Dm" внутри такта. Чистый текстовый уровень: ритм не пересобирается
// (кроме провала — полный такт паузы), аккорды — аннотации, замена безопасна.

const VOICE_RE = /^V:\s*(\S+)/
// поля заголовка (X:, T:, M:, L:, Q:, K:, w:) — не тело; V: разобран отдельно
const HEAD_RE = /^[A-uw-z]:/
const NOTE_RE = /([=_^]?)([A-Ga-g])([,']*)/g

// строки плана: head/blank/sec/voice проходят как есть,
// body хранится чанками тактов (chunks.join('|') === raw, round-trip точный)
export function splitBars(abc) {
  const parts = []
  let voice = null
  for (const raw of String(abc).split('\n')) {
    const t = raw.trim()
    if (!t) { parts.push({ kind: 'blank', raw }); continue }
    if (t.startsWith('%')) { parts.push({ kind: 'sec', raw }); continue }
    const vm = t.match(VOICE_RE)
    if (vm) { voice = vm[1]; parts.push({ kind: 'voice', raw, voice }); continue }
    if (HEAD_RE.test(t)) { parts.push({ kind: 'head', raw }); continue }
    parts.push({ kind: 'body', raw, voice, chunks: t.split('|') })
  }
  return parts
}

export function assemble(parts) {
  return parts.map((p) => (p.kind === 'body' ? p.chunks.join('|') : p.raw)).join('\n')
}

// единиц L: в такте из M:/L: (4/4 + 1/16 → 16); дефолты диалекта
export function barUnits(abc) {
  const m = String(abc).match(/^M:\s*(\d+)\/(\d+)/m)
  const l = String(abc).match(/^L:\s*(\d+)\/(\d+)/m)
  const beats = m ? Number(m[1]) : 4
  const beatDen = m ? Number(m[2]) : 4
  const lDen = l ? Number(l[2]) : 16
  return Math.max(1, Math.round(beats * (lDen / beatDen)))
}

const SEMI = { C: 0, D: 2, E: 4, F: 5, G: 7, A: 9, B: 11 }
const NAMES = ['C', 'Db', 'D', 'Eb', 'E', 'F', 'Gb', 'G', 'Ab', 'A', 'Bb', 'B']

export function keyRoot(abc) {
  const m = String(abc).match(/^K:\s*([A-G])([#b]?)(m?)/m)
  if (!m) return { root: 0, mode: 'major' }
  let s = SEMI[m[1]]
  if (m[2] === '#') s += 1
  if (m[2] === 'b') s -= 1
  return { root: ((s % 12) + 12) % 12, mode: m[3] === 'm' ? 'minor' : 'major' }
}

// заимствованный аккорд от тональности, три вкуса:
// minor: мрачнее bVI (Dm→Bb), светлее III (Dm→F), напряжённее bII (Dm→Eb)
// major: мрачнее vi (C→Am), светлее IV (C→F), напряжённее bVII (C→Bb)
export function borrowedChord(key, flavor) {
  const TABLE = {
    minor: { dark: [8, ''], lift: [3, ''], tense: [1, ''] },
    major: { dark: [9, 'm'], lift: [5, ''], tense: [10, ''] },
  }
  const row = TABLE[key.mode] || TABLE.major
  const [iv, suf] = row[flavor] || row.dark
  return NAMES[((key.root + iv) % 12 + 12) % 12] + suf
}

// октава вверх: A→a, a→a'; вниз: a→A, A→A,. Аккорды в кавычках не трогаем.
function octTranspose(s, dir) {
  return s.split(/("[^"]*")/).map((seg) => {
    if (seg.startsWith('"')) return seg
    return seg.replace(NOTE_RE, (m0, acc, letter, oct) => {
      if (dir === 'down') {
        if (letter === letter.toLowerCase()) return acc + letter.toUpperCase() + oct.replace("'", '')
        return acc + letter + oct + ','
      }
      if (letter === letter.toUpperCase()) return acc + letter.toLowerCase() + oct.replace(/,/g, '')
      return acc + letter + oct + "'"
    })
  }).join('')
}

// адресация приёма по позиционному выделению ролла: колонка = музыкальный
// такт, голоса в одной позиции звучат одновременно. voiceBars — {голос: [такты]}
// по позициям (как строит ролл). Семантика:
//   rest/cut/chord — все голоса в выбранных позициях
//   octave — только вокальный голос в выбранных позициях
// bar в targets = позиция внутри голоса (совпадает со счётом applyTrick).
// Возвращает { targets, from, to } либо null, если позиций нет/пусто.
export function pickTargets(voiceBars, lo, hi, kind) {
  const targets = []
  let from = Infinity, to = -Infinity
  for (const [voice, list] of Object.entries(voiceBars || {})) {
    if (kind === 'octave' && !/vocal/i.test(voice)) continue
    for (let p = Math.max(0, lo); p <= hi; p++) {
      const b = (list || [])[p]
      if (!b) continue
      targets.push({ voice, bar: p })
      from = Math.min(from, b.start_sec)
      to = Math.max(to, b.end_sec)
    }
  }
  if (!targets.length) return null
  return { targets, from, to }
}

// вырезать из плана мини-партитуру вокруг музыкального момента: заголовок и
// структура (%, V:) сохраняются, из строк тела остаются такты всех голосов,
// попадающие в окно [from, to] секунд (+ pad тактов контекста с каждой
// стороны). Времена считаем как abcparse: per-voice накопитель, длительность
// чанка — сумма единиц, пустой чанк — такт целиком. Нужен для «проверить
// кусок»: драфт-рендер мини-плана даёт послушать приём по месту без полной
// пересборки трека.
export function sliceAbc(abc, from, to, padBars = 1) {
  const text = String(abc)
  const q = text.match(/^Q:.*?=\s*(\d+)/m)
  const tempo = q ? Number(q[1]) : 120
  const m = text.match(/^L:\s*1\/(\d+)/m)
  const lden = m ? Number(m[1]) : 16
  // такт в секундах = единиц в такте × единица в четвертях × сек/четверть
  const secPerBar = barUnits(text) * (4 / lden) * (60 / tempo)

  const keep = {}   // voice → Set(индексы тактов, попадающих в окно)
  const counters = {}
  const vt = {}
  const parts = splitBars(text)
  for (const p of parts) {
    if (p.kind !== 'body' || !p.voice) continue
    const closed = p.raw.trim().endsWith('|')
    const n = closed ? p.chunks.length - 1 : p.chunks.length
    const base = counters[p.voice] || 0
    for (let j = 0; j < n; j++) {
      const t0 = vt[p.voice] || 0
      const t1 = t0 + secPerBar
      if (t1 > from && t0 < to) {
        keep[p.voice] = keep[p.voice] || new Set()
        keep[p.voice].add(base + j)
      }
      vt[p.voice] = t1
    }
    counters[p.voice] = base + n
  }
  // расширить окно на pad тактов контекста по каждому голосу
  for (const v of Object.keys(keep)) {
    const ids = [...keep[v]]
    const lo = Math.max(0, Math.min(...ids) - padBars)
    const hi = Math.max(...ids) + padBars
    for (let i = lo; i <= hi; i++) keep[v].add(i)
  }
  const out = []
  const c2 = {}
  for (const p of parts) {
    if (p.kind !== 'body' || !p.voice) { out.push(p); continue }
    const closed = p.raw.trim().endsWith('|')
    const n = closed ? p.chunks.length - 1 : p.chunks.length
    const base = c2[p.voice] || 0
    const kept = []
    for (let j = 0; j < n; j++) {
      if (keep[p.voice] && keep[p.voice].has(base + j)) kept.push(p.chunks[j])
    }
    c2[p.voice] = base + n
    if (!kept.length) continue
    out.push({ ...p, chunks: [...kept, ...(closed ? [''] : [])] })
  }
  return assemble(out)
}
// инструменты «+ инструмент»: mode overdub = цельный ре-рендер того же плана
// (ритм совпадает всегда, локализация — фразой в стиле); insert = подклад
// куском поверх (струнные/колокольчики: сетка им не нужна)
export const TRICK_INSTRUMENTS = [
  { id: 'flute', en: 'flute melody', mode: 'overdub' },
  { id: 'strings', en: 'string ensemble', mode: 'insert' },
  { id: 'organ', en: 'hammond organ', mode: 'overdub' },
  { id: 'sax', en: 'saxophone', mode: 'overdub' },
  { id: 'eguitar', en: 'distorted electric guitar', mode: 'overdub' },
  { id: 'synth', en: 'analog synth lead', mode: 'overdub' },
  { id: 'bells', en: 'glockenspiel', mode: 'insert' },
  { id: 'piano', en: 'grand piano', mode: 'overdub' },
]

// стилевые приписки от приёмов: октава и «+ инструмент» в секцию.
// Модель следует стилю приблизительно, но такие структурные подсказки
// работают (проверено драматургией «взрыв» — тот же ход).
// приписка только для октавы: инструменты локализуются овердаб-партией
// (soloInstrumentPlan), стилевая подсказка расползалась на весь трек
export function trickStyleSuffix(specs) {
  const parts = []
  for (const s of specs || []) {
    if (s.kind === 'octave' && s.dir) {
      parts.push(s.dir === 'up' ? 'sudden octave-up vocal lift' : 'sudden octave-down vocal drop')
    }
  }
  return [...new Set(parts)].join(', ')
}

function baseTempo(abc) {
  const m = String(abc).match(/^Q:.*?=\s*(\d+)/m)
  return m ? Number(m[1]) : 120
}

// targets — [{voice, bar}], bar — сквозной номер такта внутри голоса (0-баз).
// Виды: chord (flavor), rest, cut, octave (dir), tempo (dir: ±10%, вставка Q:
// перед строкой с первым целевым тактом — как драматургия; применения
// накапливаются), instrument (план не трогает — ходит стилевой припиской).
export function applyTrick(abc, spec) {
  const want = new Set((spec.targets || []).map((t) => t.voice + '#' + t.bar))
  const units = barUnits(abc)
  const chord = spec.kind === 'chord' ? borrowedChord(keyRoot(abc), spec.flavor) : null
  const counters = {}
  const out = []
  let curTempo = baseTempo(abc)
  let tempoDone = spec.kind !== 'tempo'
  for (const p of splitBars(abc)) {
    if (p.kind !== 'body' || !p.voice) {
      const qm = p.kind === 'head' && p.raw.match(/^Q:.*?=\s*(\d+)/)
      if (qm) curTempo = Number(qm[1])
      out.push(p)
      continue
    }
    const closed = p.raw.trim().endsWith('|')           // строка кончается тактовой чертой
    const bars = closed ? p.chunks.slice(0, -1) : p.chunks
    const base = counters[p.voice] || 0
    if (!tempoDone && bars.some((_, j) => want.has(p.voice + '#' + (base + j)))) {
      curTempo = Math.max(40, Math.round(curTempo * (spec.dir === 'down' ? 0.9 : 1.1)))
      out.push({ kind: 'head', raw: `Q:1/4=${curTempo}` })
      tempoDone = true
    }
    let kept = []
    for (let j = 0; j < bars.length; j++) {
      const c = bars[j]
      if (!want.has(p.voice + '#' + (base + j))) { kept.push(c); continue }
      if (spec.kind === 'chord') kept.push(c.replace(/"[^"]*"/g, '"' + chord + '"'))
      else if (spec.kind === 'rest') {
        const head = c.match(/^(?:"[^"]*"\s*)+/)   // аккордовые аннотации такта сохраняем
        kept.push((head ? head[0] : '') + 'z' + units)
      } else if (spec.kind === 'octave') kept.push(octTranspose(c, spec.dir || 'up'))
      // 'cut' — такт просто не попадает в kept
    }
    counters[p.voice] = base + bars.length
    if (spec.kind === 'cut' && !kept.length) continue   // вся строка вырезана — убрать
    out.push({ ...p, chunks: [...kept, ...(closed ? [''] : [])] })
  }
  return assemble(out)
}
