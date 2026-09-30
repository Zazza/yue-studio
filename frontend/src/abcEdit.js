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
    // сначала снимаем знак октавы (c' ↓ → c, C, ↑ → C), только без него меняем регистр
    // буквы: раньше c' вниз давало C — сразу две октавы
    return seg.replace(NOTE_RE, (m0, acc, letter, oct) => {
      if (dir === 'down') {
        if (oct.includes("'")) return acc + letter + oct.replace("'", '')
        if (letter === letter.toLowerCase()) return acc + letter.toUpperCase() + oct
        return acc + letter + oct + ','
      }
      if (oct.includes(',')) return acc + letter + oct.replace(',', '')
      if (letter === letter.toUpperCase()) return acc + letter.toLowerCase() + oct
      return acc + letter + oct + "'"
    })
  }).join('')
}

// ---------- мелодия голоса: ступени лада ----------
// Ступень = буква по порядку (C D E F G A B c d e … c' …): знаки ключа (K:)
// держат лад, поэтому «на ступень выше» — просто следующая буква. Замер
// 2026-09-30: модель поёт мелодию Vocal плана нота в ноту; выше потолка
// голоса (верх плана + 2 ступени) — писк/фальцет.
const LETTERS = 'CDEFGAB'
const VOCAL_KINDS = new Set(['octave', 'vocalUp', 'vocalVary'])

function noteStep(letter, oct) {
  const low = letter === letter.toLowerCase()
  let st = LETTERS.indexOf(letter.toUpperCase()) + (low ? 7 : 0)
  for (const ch of oct || '') st += ch === "'" ? 7 : -7
  return st
}
function stepNote(st) {
  if (st >= 7) {
    const up = Math.floor((st - 7) / 7)
    return LETTERS[(st - 7) % 7].toLowerCase() + "'".repeat(up)
  }
  const down = Math.ceil(-st / 7)
  return LETTERS[((st % 7) + 7) % 7] + ','.repeat(Math.max(0, down))
}
function parseNote(n) {
  const m = String(n).match(/^([A-Ga-g])([,']*)$/)
  return m ? noteStep(m[1], m[2]) : null
}

// ноты такта вне аккордов-аннотаций: fn(step, i) → новая ступень (i — номер ноты)
function mapNotes(chunk, fn) {
  let i = 0
  return chunk.split(/("[^"]*")/).map((seg) => {
    if (seg.startsWith('"')) return seg
    return seg.replace(NOTE_RE, (m0, acc, letter, oct) => {
      const st = noteStep(letter, oct)
      const ns = fn(st, i++)
      return ns === st ? m0 : stepNote(ns)   // сдвинутая нота — без явного знака
    })
  }).join('')
}
function noteCount(chunk) {
  let n = 0
  mapNotes(chunk, (st) => { n++; return st })
  return n
}

// потолок голоса: верхняя нота Vocal во всём плане + 2 ступени; нет нот — null
export function vocalCeiling(abc) {
  let top = -Infinity
  for (const p of splitBars(abc)) {
    if (p.kind !== 'body' || !/vocal/i.test(p.voice || '')) continue
    for (const c of p.chunks) mapNotes(c, (st) => { top = Math.max(top, st); return st })
  }
  return top === -Infinity ? null : stepNote(top + 2)
}

// «голос выше»: терция вверх (2 ступени), выше потолка — на потолок
function vocalUp(chunk, ceil) {
  return mapNotes(chunk, (st) => Math.min(st + 2, ceil))
}
// «вариации мотива»: ритм и первая нота такта — опора; конец фразы уходит на
// соседнюю ступень (чётный такт выделения вверх, нечётный вниз; упёрся в
// потолок — вниз), во второй половине выделения предпоследняя нота — на
// ступень вверх (фраза шире к концу). Детерминировано.
function vocalVary(chunk, ceil, k, n) {
  const cnt = noteCount(chunk)
  if (cnt < 2) return chunk
  const late = k >= n / 2
  return mapNotes(chunk, (st, i) => {
    if (i === 0) return st
    let ns = st
    if (i === cnt - 1) ns = k % 2 === 0 && st + 1 <= ceil ? st + 1 : st - 1
    else if (late && i === cnt - 2) ns = st + 1
    return Math.min(ns, ceil)
  })
}

// план для «заново с места»: приёмы ролла ровно по разу поверх исходника
// (раньше применялись к уже изменённому черновику — октава выходила двойной)
// Потолок голоса — по ИСХОДНОМУ плану, один на все приёмы: иначе второе
// «голос выше» считало бы потолок от уже поднятого плана и уползало вверх
export function continuationPlan(baseAbc, specs) {
  if (!specs || !specs.length) return ''
  const ceiling = vocalCeiling(baseAbc)
  return specs.reduce((abc, spec) => applyTrick(abc,
    spec.ceiling || !ceiling ? spec : { ...spec, ceiling }), baseAbc)
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
    if (VOCAL_KINDS.has(kind) && !/vocal/i.test(voice)) continue
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
// мультипауза Z<n> = n целых тактов тишины (так YuE пишет долгие паузы голоса).
// Счёт тактов/времени везде ведётся по развёрнутому плану: без этого голос с
// Z4 отставал на 3 такта на каждую мультипаузу, и нарезка брала чужой материал.
const MULTI_REST_RE = /^\s*Z(\d*)\s*$/
export function expandMultiRests(abc) {
  const text = String(abc)
  const units = barUnits(text)
  return assemble(splitBars(text).map((p) => {
    if (p.kind !== 'body') return p
    const chunks = []
    for (const c of p.chunks) {
      const m = c.match(MULTI_REST_RE)
      if (!m) { chunks.push(c); continue }
      for (let i = 0; i < Number(m[1] || 1); i++) chunks.push('z' + units)
    }
    return { ...p, chunks }
  }))
}

// длительность такта (с) при темпе tempo: единиц в такте × единица в
// четвертях × сек/четверть
function barSec(text, tempo) {
  const m = text.match(/^L:\s*1\/(\d+)/m)
  const lden = m ? Number(m[1]) : 16
  return barUnits(text) * (4 / lden) * (60 / tempo)
}

// таймлайн плана: {голос: [{start, end, tempo}]} по тактам голоса (после
// разворота мультипауз). Темп — ТОЛЬКО из заголовка (первая строка Q:): YuE
// смену темпа посреди плана не исполняет (замер: приём «темп +10%» — бочка
// осталась 120 BPM), и время звука идёт по темпу заголовка. Ускорение — эффект
// «Ускорить с отметки» (dsp tempo-from).
export function planTimeline(abc) {
  const text = expandMultiRests(abc)
  const q = text.match(/^Q:.*?=\s*(\d+)/m)
  const tempo = q ? Number(q[1]) : 120
  const out = {}
  for (const p of splitBars(text)) {
    if (p.kind !== 'body' || !p.voice) continue
    const closed = p.raw.trim().endsWith('|')
    const n = closed ? p.chunks.length - 1 : p.chunks.length
    const list = out[p.voice] || (out[p.voice] = [])
    const dur = barSec(text, tempo)
    for (let j = 0; j < n; j++) {
      const t0 = list.length ? list[list.length - 1].end : 0
      list.push({ start: t0, end: t0 + dur, tempo })
    }
  }
  return out
}

// длина доли (с) в момент sec: темп такта, который звучит в sec (первый голос,
// у которого такой такт есть); вне плана — темп последнего такта
export function beatSecAt(abc, sec) {
  let tempo = 0
  for (const list of Object.values(planTimeline(abc))) {
    const b = list.find((x) => x.start <= sec && sec < x.end) || list[list.length - 1]
    if (b) { tempo = b.tempo; break }
  }
  return 60 / (tempo || 120)
}

export function sliceAbc(abc, from, to, padBars = 1) {
  const text = expandMultiRests(abc)
  const keep = {}   // voice → Set(индексы тактов, попадающих в окно)
  for (const [voice, list] of Object.entries(planTimeline(text))) {
    list.forEach((b, i) => {
      if (b.end > from && b.start < to) (keep[voice] = keep[voice] || new Set()).add(i)
    })
  }
  const parts = splitBars(text)
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
// сколько секунд мини-партитуры sliceAbc звучит ДО момента from: такт
// контекста (padBars) + доля своего такта. Мини-рендер начинается с этого
// контекста — при вклейке его начало ставится на from − lead, а не на from
// (иначе партия опаздывает на такт).
export function sliceLeadSec(abc, from, padBars = 1) {
  // первый такт в окне (по любому голосу) минус padBars тактов контекста
  let lead = 0
  for (const list of Object.values(planTimeline(abc))) {
    const i = list.findIndex((b) => b.end > from + 1e-9)
    if (i < 0) continue
    const first = list[Math.max(0, i - padBars)]
    lead = Math.max(lead, from - first.start)
  }
  return lead
}

// инструменты и приёмы «+ инструмент»: кусок трека перерендеривается целой
// группой (стиль трека + en), а в треке заменяются только дорожки stems
// (стемы demucs; голос не трогается никогда). db — громкость новой дорожки
// по умолчанию, fadeIn — вход замены, с (сбивка — точно по доле), energetic —
// из стиля трека убираются мягкие слова настроения (SOFT_MOOD_WORDS),
// keepHighHz — у старой дорожки убирается только низ (хэт/тарелки остаются).
export const TRICK_INSTRUMENTS = [
  { id: 'eguitar', en: 'electric guitar enters and builds, crunchy overdriven electric guitar, rising intensity', stems: ['other'], energetic: true },
  { id: 'strings', en: 'lush string ensemble', stems: ['other'] },
  { id: 'piano', en: 'prominent grand piano', stems: ['other'] },
  { id: 'organ', en: 'hammond organ', stems: ['other'] },
  { id: 'synth', en: 'analog synth lead', stems: ['other'] },
  { id: 'flute', en: 'flute melody', stems: ['other'] },
  { id: 'sax', en: 'saxophone', stems: ['other'] },
  { id: 'bells', en: 'glockenspiel', stems: ['other'] },
  { id: 'bass', en: 'prominent driving bass guitar', stems: ['bass'], db: 4 },
  { id: 'drumfill', en: 'drum fill building up into the chorus, snare roll crescendo, tom fill, crash cymbal', stems: ['drums'], db: 4, fadeIn: 0.1, keepHighHz: 6000, energetic: true },
  { id: 'buildup', en: 'building up, rising intensity', stems: ['drums', 'bass', 'other'], energetic: true },
  // «жёстче» генерацией: перегруз обработкой срезал вершины волны (замер:
  // 2 → 358 срезанных сэмплов), а «стена» гитар от модели — это аранжировка
  { id: 'heavy', en: 'heavier, fuzz wall of distorted guitars, driving, soaring intensity', stems: ['other', 'bass'], energetic: true },
]

// запрос рендера куска группой: план окна со всеми голосами (+такт контекста),
// стиль трека с припиской, сид и режим плана родителя — тот же «дубль» по
// таймингу (замер: бочка перерендера совпадает с оригиналом)
// seed — для «ещё вариант» (модель играет кусок по-разному от сида к сиду)
export function sectionRequest(parent, abc, inst, from, to, seed) {
  return {
    title: (parent.title || 'трек') + ' · ' + inst.id,
    style: sectionStyle(parent.style, inst),
    lyrics: '[Instrumental]',
    seed: seed || parent.seed || Math.floor(Math.random() * 1e9),
    cot: parent.cot === 'off' || !parent.cot ? 'melody' : parent.cot,
    abc: sliceAbc(abc, from, to, 1),
    draft: false,
    // производный трек: в списке прячется под родителем
    parent_id: parent.id, role: 'section',
  }
}

// мягкие слова настроения: у спокойного трека они перебивали приписку
// «энергичного» инструмента — модель играла ту же акустику (замер: центр
// спектра гитар не сдвинулся; без этих слов электрогитара появилась)
export const SOFT_MOOD_WORDS = ['calm', 'sparse', 'intimate', 'soft', 'quiet', 'gentle', 'mellow', 'delicate']
const SOFT_RE = new RegExp('\\b(' + SOFT_MOOD_WORDS.join('|') + ')\\b', 'gi')

// приёмы громкости дорожек без рендера: в окне дорожки mute (можно и голос)
// меняются на db. «только барабаны» — остальное заглушено (db −100); «барабаны
// громче» — заход после провала модель играет тихо (замер −42…−31 при бите −18)
export const TRICK_MUTES = [
  { id: 'drumsolo', mute: ['other', 'bass', 'vocals'], db: -100 },
  { id: 'drumsup', mute: ['drums'], db: 6 },
  { id: 'otherdown', mute: ['other'], db: -6 },
  { id: 'bassdown', mute: ['bass'], db: -6 },
  // «стоп»: голос молчит, музыка идёт. Паузу голоса в плане модель заполняет
  // пением (замер #235/#236) — поэтому глушением дорожки, без генерации
  { id: 'vocalstop', mute: ['vocals'], db: -100 },
]

// окна секций по таймлайну ролла: подряд идущие такты голоса voice с секцией
// section → [{from, to}] (для «куплеты реже»: все куплеты одной кнопкой)
export function sectionWindows(bars, voice, section) {
  const out = []
  for (const b of bars || []) {
    if (!(b.voices && voice in b.voices)) continue
    const last = out[out.length - 1]
    if (b.section === section) {
      if (last && last.open && Math.abs(last.to - b.start_sec) < 0.05) last.to = b.end_sec
      else out.push({ from: b.start_sec, to: b.end_sec, open: true })
    } else if (last) last.open = false
  }
  return out.map(({ from, to }) => ({ from, to }))
}

// стиль рендера куска: стиль трека + приписка инструмента/приёма; для
// energetic-инструментов мягкие слова из стиля трека убираются
export function sectionStyle(parentStyle, inst) {
  let base = String(parentStyle || '')
  if (inst.energetic) {
    base = base.split(',').map((tag) => tag.replace(SOFT_RE, '').replace(/\s+/g, ' ').trim())
      .filter(Boolean).join(', ')
  }
  base = base.trim().replace(/[\s,]+$/, '')
  return base ? base + ', ' + inst.en : inst.en
}

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
  abc = expandMultiRests(abc)
  const want = new Set((spec.targets || []).map((t) => t.voice + '#' + t.bar))
  const units = barUnits(abc)
  const chord = spec.kind === 'chord' ? borrowedChord(keyRoot(abc), spec.flavor) : null
  const counters = {}
  const out = []
  let curTempo = baseTempo(abc)
  let tempoDone = spec.kind !== 'tempo'
  const vocal = spec.kind === 'vocalUp' || spec.kind === 'vocalVary'
  const ceilNote = vocal ? (spec.ceiling || vocalCeiling(abc)) : null
  const ceil = ceilNote ? parseNote(ceilNote) : Infinity
  let varied = 0
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
      else if (spec.kind === 'vocalUp') kept.push(vocalUp(c, ceil))
      else if (spec.kind === 'vocalVary') kept.push(vocalVary(c, ceil, varied++, want.size))
      // 'cut' — такт просто не попадает в kept
    }
    counters[p.voice] = base + bars.length
    if (spec.kind === 'cut' && !kept.length) continue   // вся строка вырезана — убрать
    out.push({ ...p, chunks: [...kept, ...(closed ? [''] : [])] })
  }
  return assemble(out)
}
