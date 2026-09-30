// Тесты приёмов над ABC-планом: разбор/сборка и мутации тактов.
import { describe, it, expect } from 'vitest'
import { splitBars, assemble, barUnits, keyRoot, borrowedChord, applyTrick, pickTargets, sliceAbc, sliceLeadSec, TRICK_INSTRUMENTS, trickStyleSuffix, expandMultiRests, sectionStyle, SOFT_MOOD_WORDS, sectionWindows, TRICK_MUTES, vocalCeiling, continuationPlan } from './abcEdit.js'

// диалект YuE: та же фикстура, что в worker/test_pure.py
const ABC = [
  'X:1', 'T:', 'M:4/4', 'L:1/16', 'Q:1/4=100',
  'V: Vocal clef=treble name="Vocal Melody" snm="Vocal"',
  'V: Ins clef=treble name="Ins Melody" snm="Inst."',
  'K:Dm',
  '% intro',
  'V: Vocal',
  'z16|',
  'V: Ins',
  'd8z8|',
  '% verse',
  'V: Vocal',
  '"Dm"d2a2g2e2|',
  'V: Ins',
  'd4d4d4d4|',
  '% chorus',
  'V: Vocal',
  '"Dm"d2a2g2e2|',
  'V: Ins',
  'e4e4e4e4|',
].join('\n')

// много тактов в строке (стенд примерочной)
const MULTI = [
  'X:1', 'M:4/4', 'L:1/16', 'Q:1/4=110',
  'V: Vocal clef=treble name="Vocal Melody" snm="Vocal"',
  'V: Ins clef=treble name="Ins Melody" snm="Inst."',
  'K:Dm',
  '% chorus',
  'V: Vocal',
  '"Dm"z4d2f2a2a2f2d2|"Bb"z4d2f2g2g2f2d2|"F"z4c2f2a2a2g2f2|"C"z4e2g2g2e2d2e2|',
  'V: Ins',
  '"Dm"d4f2d2f2g2gfd2|"Bb"d4f2d2f2g2gfd2|"F"c4a2f2a2g2gfc2|"C"e4g2e2g2a2age2|',
].join('\n')

describe('разбор и сборка', () => {
  it('round-trip без потерь на обеих фикстурах', () => {
    expect(assemble(splitBars(ABC))).toBe(ABC)
    expect(assemble(splitBars(MULTI))).toBe(MULTI)
  })

  it('строки тела получают голос и такты', () => {
    const parts = splitBars(MULTI)
    const body = parts.filter((p) => p.kind === 'body')
    expect(body.map((p) => p.voice)).toEqual(['Vocal', 'Ins'])
    expect(body[0].chunks.length).toBe(5)   // 4 такта + хвост после финальной |
  })
})

describe('метрики плана', () => {
  it('единиц на такт: 4/4 + 1/16 → 16, 7/8 + 1/8 → 7', () => {
    expect(barUnits(ABC)).toBe(16)
    expect(barUnits('M:7/8\nL:1/8\n')).toBe(7)
    expect(barUnits('без заголовков')).toBe(16)   // дефолты диалекта
  })

  it('тональность: K:Dm → минор от D, K:C → мажор от C, отсутствие — дефолт', () => {
    expect(keyRoot(ABC)).toEqual({ root: 2, mode: 'minor' })
    expect(keyRoot('K:C')).toEqual({ root: 0, mode: 'major' })
    expect(keyRoot('K:Bb')).toEqual({ root: 10, mode: 'major' })
    expect(keyRoot('')).toEqual({ root: 0, mode: 'major' })
  })

  it('заимствованные аккорды: минор Dm → Bb/F/Eb, мажор C → Am/F/Bb', () => {
    const dm = keyRoot(ABC)
    expect(borrowedChord(dm, 'dark')).toBe('Bb')
    expect(borrowedChord(dm, 'lift')).toBe('F')
    expect(borrowedChord(dm, 'tense')).toBe('Eb')
    const c = { root: 0, mode: 'major' }
    expect(borrowedChord(c, 'dark')).toBe('Am')
    expect(borrowedChord(c, 'lift')).toBe('F')
    expect(borrowedChord(c, 'tense')).toBe('Bb')
  })
})

describe('приём «не тот аккорд»', () => {
  it('заменяет аккорды только в целевых тактах целевого голоса', () => {
    const out = applyTrick(MULTI, { kind: 'chord', flavor: 'dark', targets: [{ voice: 'Vocal', bar: 1 }] })
    expect(out).toContain('"Bb"z4d2f2g2g2f2d2')      // такт 1 Vocal: Bb вместо родного
    expect(out).toContain('"Dm"z4d2f2a2a2f2d2')      // такт 0 Vocal не тронут
    expect(out).toContain('"Bb"d4f2d2f2g2gfd2')      // Ins не тронут (его такт 1 остался Bb как был)
  })

  it('в такте с двумя аккордами заменяются все аннотации', () => {
    const two = 'X:1\nM:4/4\nL:1/16\nK:Dm\nV: Vocal\n"Dm"z4"F"z12|\n'
    const out = applyTrick(two, { kind: 'chord', flavor: 'tense', targets: [{ voice: 'Vocal', bar: 0 }] })
    expect(out).toContain('"Eb"z4"Eb"z12')
  })

  it('такт без аккордовой аннотации не меняется', () => {
    const out = applyTrick(ABC, { kind: 'chord', flavor: 'dark', targets: [{ voice: 'Ins', bar: 0 }] })
    expect(out).toContain('d8z8|')
  })
})

describe('приём «провал»', () => {
  it('целевые такты становятся полной паузой, аккорд сохраняется, прочее живо', () => {
    const out = applyTrick(MULTI, {
      kind: 'rest',
      targets: [{ voice: 'Vocal', bar: 2 }, { voice: 'Ins', bar: 2 }],
    })
    expect(out).toContain('"F"z16|')                  // оба голоса в такте 2 молчат
    expect(out).not.toContain('z4c2f2a2a2g2f2')
    expect(out).toContain('"Dm"z4d2f2a2a2f2d2')       // такт 0 Vocal поёт
  })
})

describe('приём «вырезать такты»', () => {
  it('вырезанный такт исчезает у всех голосов, строка остаётся валидной', () => {
    const out = applyTrick(MULTI, {
      kind: 'cut',
      targets: [{ voice: 'Vocal', bar: 1 }, { voice: 'Ins', bar: 1 }],
    })
    expect(out).not.toContain('"Bb"z4d2f2g2g2f2d2')
    expect(out).not.toContain('"Bb"d4f2d2f2g2gfd2')
    // в каждой строке тела теперь 3 такта, соседние склеены без пустых
    const body = out.split('\n').filter((l) => l.includes('|'))
    for (const l of body) expect(l.match(/\|/g).length).toBe(3)
  })

  it('если вырезаны все такты строки — строка убирается целиком', () => {
    const one = 'X:1\nM:4/4\nL:1/16\nK:Dm\nV: Vocal\nz16|\nV: Ins\nd4d4d4d4|\n'
    const out = applyTrick(one, { kind: 'cut', targets: [{ voice: 'Vocal', bar: 0 }] })
    expect(out).not.toContain('V: Vocal\nd')   // хвост
    expect(out).toContain('d4d4d4d4|')          // Ins жив
    expect(out.split('\n').filter((l) => l.trim() === 'z16|')).toHaveLength(0)
  })
})

describe('приём «октава»', () => {
  it('вверх: заглавные становятся строчными, строчные получают апостроф; аккорды целы', () => {
    const src = 'X:1\nM:4/4\nL:1/16\nK:Dm\nV: Vocal\n"Dm"d2A2g2e2|\n'
    const out = applyTrick(src, { kind: 'octave', dir: 'up', targets: [{ voice: 'Vocal', bar: 0 }] })
    expect(out).toContain('"Dm"d\'2a2g\'2e\'2|')
  })

  it('вниз — обратное преобразование', () => {
    const src = 'X:1\nM:4/4\nL:1/16\nK:Dm\nV: Vocal\n"Dm"d2a2g2e2|\n'
    const out = applyTrick(src, { kind: 'octave', dir: 'down', targets: [{ voice: 'Vocal', bar: 0 }] })
    expect(out).toContain('"Dm"D2A2G2E2|')
  })

  it('октава не трогает заголовки и длительности', () => {
    const src = 'X:1\nM:4/4\nL:1/16\nQ:1/4=110\nK:Dm\nV: Vocal\n"Dm"d2a2g2e2|\n'
    const out = applyTrick(src, { kind: 'octave', dir: 'down', targets: [{ voice: 'Vocal', bar: 0 }] })
    expect(out).toContain('Q:1/4=110')
    expect(out).toContain('L:1/16')
    expect(out).toContain('"Dm"D2A2G2E2')
  })
})

describe('адресация тактов сквозная по голосу', () => {
  it('вторая строка того же голоса продолжает нумерацию', () => {
    const two = 'X:1\nM:4/4\nL:1/16\nK:Dm\nV: Vocal\nz16|z16|\nV: Ins\nd4d4d4d4|\nV: Vocal\n"Dm"d2a2g2e2|\n'
    const out = applyTrick(two, { kind: 'chord', flavor: 'dark', targets: [{ voice: 'Vocal', bar: 2 }] })
    expect(out).toContain('"Bb"d2a2g2e2')   // такт 2 Vocal — это третья строка
  })
})

// позиционная карта ролла: колонка = музыкальный такт, голоса параллельны
const VOICE_BARS = {
  Vocal: [
    { section: 'verse', voices: { Vocal: 4 }, chords: ['Fm'], start_sec: 0, end_sec: 2.2 },
    { section: 'verse', voices: { Vocal: 6 }, chords: ['Db'], start_sec: 2.2, end_sec: 4.4 },
    { section: 'verse', voices: { Vocal: 2 }, chords: ['Eb'], start_sec: 4.4, end_sec: 6.6 },
  ],
  Ins: [
    { section: 'verse', voices: {}, rests: { Ins: 32 }, chords: [], start_sec: 0, end_sec: 2.2 },
    { section: 'verse', voices: { Ins: 6 }, chords: ['Fm'], start_sec: 2.2, end_sec: 4.4 },
    { section: 'verse', voices: { Ins: 4 }, chords: [], start_sec: 4.4, end_sec: 6.6 },
  ],
}

describe('адресация приёма по позициям ролла (pickTargets)', () => {
  it('позиции 0–1: все голоса в этих колонках, время — границы момента', () => {
    const r = pickTargets(VOICE_BARS, 0, 1, 'rest')
    expect(r.targets).toEqual([
      { voice: 'Vocal', bar: 0 }, { voice: 'Vocal', bar: 1 },
      { voice: 'Ins', bar: 0 }, { voice: 'Ins', bar: 1 },
    ])
    expect(r.from).toBe(0)
    expect(r.to).toBeCloseTo(4.4)
  })

  it('октава — только вокальный голос выбранных позиций; без вокала — null', () => {
    expect(pickTargets({ Ins: VOICE_BARS.Ins }, 0, 1, 'octave')).toBeNull()
    expect(pickTargets(VOICE_BARS, 1, 2, 'octave').targets)
      .toEqual([{ voice: 'Vocal', bar: 1 }, { voice: 'Vocal', bar: 2 }])
  })

  it('у голоса меньше тактов — недостающие позиции пропускаются', () => {
    const vb = { Vocal: VOICE_BARS.Vocal, Ins: VOICE_BARS.Ins.slice(0, 1) }
    expect(pickTargets(vb, 1, 2, 'rest').targets)
      .toEqual([{ voice: 'Vocal', bar: 1 }, { voice: 'Vocal', bar: 2 }])
  })

  it('пустой диапазон → null', () => {
    expect(pickTargets(VOICE_BARS, 10, 20, 'rest')).toBeNull()
    expect(pickTargets({}, 0, 1, 'rest')).toBeNull()
  })

  it('провал по позициям гасит оба голоса в плане', () => {
    const r = pickTargets(VOICE_BARS, 0, 1, 'rest')
    const out = applyTrick(MULTI, { kind: 'rest', targets: r.targets })
    expect(out.split('"Dm"z16|"Bb"z16|').length - 1).toBe(2)
  })
})

describe('мини-партитура для проверки куска (sliceAbc)', () => {
  // MULTI: 110 BPM, 4/4, L:1/16 → такт = 2.18 с; у каждого голоса по 4 такта
  it('окно на такты 0–1: остаются они и такт контекста справа, у обоих голосов', () => {
    const out = sliceAbc(MULTI, 0.1, 4.0, 1)
    const body = out.split('\n').filter((l) => l.includes('|'))
    expect(body).toHaveLength(2)                                  // Vocal и Ins
    for (const l of body) expect(l.match(/\|/g).length).toBe(3)  // окно 0–1 + контекст 2
    expect(out).toContain('"Bb"z4d2f2g2g2f2d2')                  // такт окна на месте
  })

  it('заголовок сохраняется, результат — валидный ABC (round-trip)', () => {
    const out = sliceAbc(MULTI, 0, 2.2, 0)
    expect(out.startsWith('X:1')).toBe(true)
    expect(assemble(splitBars(out))).toBe(out)
  })

  it('окно мимо всех тактов — телесные строки пропадают, заголовок остаётся', () => {
    const out = sliceAbc(MULTI, 500, 502, 0)
    expect(out).toContain('K:Dm')
    expect(out.split('\n').filter((l) => l.includes('|'))).toHaveLength(0)
  })
})

describe('приём «темп»', () => {
  it('вверх: Q:1/4=121 (110+10%) вставляется перед строкой с целевым тактом', () => {
    const out = applyTrick(MULTI, { kind: 'tempo', dir: 'up', targets: [{ voice: 'Ins', bar: 1 }] })
    const lines = out.split('\n')
    const qi = lines.indexOf('Q:1/4=121')
    expect(qi).toBeGreaterThan(0)
    // такты Ins в фикстуре — одна строка на 4 такта: Q встаёт перед строкой
    expect(lines[qi + 1].startsWith('"Dm"d4f2d2f2g2gfd2')).toBe(true)
    expect(lines.filter((l) => l.startsWith('Q:')).length).toBe(2)     // исходный + вставленный
  })

  it('применения накапливаются: второй раз от вставленного темпа', () => {
    const once = applyTrick(MULTI, { kind: 'tempo', dir: 'up', targets: [{ voice: 'Vocal', bar: 0 }] })
    const twice = applyTrick(once, { kind: 'tempo', dir: 'up', targets: [{ voice: 'Vocal', bar: 0 }] })
    expect(once).toContain('Q:1/4=121')
    expect(twice).toContain('Q:1/4=133')
  })

  it('вниз — 0.9×, с floor от 40 bpm', () => {
    const out = applyTrick(MULTI, { kind: 'tempo', dir: 'down', targets: [{ voice: 'Vocal', bar: 0 }] })
    expect(out).toContain('Q:1/4=99')
  })
})

describe('приём «+ инструмент» и стилевые приписки', () => {
  it('инструмент план не меняет — только стиль при пересборке', () => {
    const out = applyTrick(MULTI, { kind: 'instrument', inst: 'flute', targets: [] })
    expect(out).toBe(MULTI)
  })

  it('trickStyleSuffix: только октава — инструменты локализуются овердабом, не стилем', () => {
    expect(trickStyleSuffix([{ kind: 'octave', dir: 'down' }])).toBe('sudden octave-down vocal drop')
    expect(trickStyleSuffix([{ kind: 'instrument', inst: 'flute', section: 'intro' }])).toBe('')
    expect(trickStyleSuffix([])).toBe('')
    expect(trickStyleSuffix([{ kind: 'rest' }])).toBe('')
  })

  it('все инструменты имеют en-тег', () => {
    for (const i of TRICK_INSTRUMENTS) expect(i.en.trim()).not.toBe('')
  })
})

describe('сколько плана звучит до начала окна (sliceLeadSec)', () => {
  // 4/4, L:1/16, 120 BPM → такт = 16 × 1/16 × 4 × 60/120 = 2 с; 12 тактов
  const plan = (q) => [
    'X:1', 'M:4/4', 'L:1/16', ...(q ? [q] : []), 'K:C', 'V:Inst',
    'c4c4c4c4|d4d4d4d4|e4e4e4e4|f4f4f4f4|',
    'g4g4g4g4|a4a4a4a4|b4b4b4b4|c4c4c4c4|',
    'd4d4d4d4|e4e4e4e4|f4f4f4f4|g4g4g4g4|',
  ].join('\n')
  const Q120 = plan('Q:1/4=120')

  it('from внутри такта (4–6), контекст 1 такт → от начала такта 2 с', () => {
    expect(sliceLeadSec(Q120, 5, 1)).toBeCloseTo(3, 6)
  })

  it('from ровно на границе такта → только такт контекста', () => {
    expect(sliceLeadSec(Q120, 4, 1)).toBeCloseTo(2, 6)
  })

  it('контекст раньше начала плана не уходит', () => {
    expect(sliceLeadSec(Q120, 0.5, 1)).toBeCloseTo(0.5, 6)
  })

  it('без контекста — от начала своего такта', () => {
    expect(sliceLeadSec(Q120, 5, 0)).toBeCloseTo(1, 6)
  })

  it('padBars по умолчанию = 1', () => {
    expect(sliceLeadSec(Q120, 5)).toBeCloseTo(3, 6)
  })

  it('темп 60 → такт 4 с: from 5 в такте 4–8, контекст с 0', () => {
    expect(sliceLeadSec(plan('Q:1/4=60'), 5, 1)).toBeCloseTo(5, 6)
  })

  it('без Q: — темп по умолчанию 120', () => {
    expect(sliceLeadSec(plan(null), 5, 1)).toBeCloseTo(3, 6)
  })
})


// ---- мультипауза Z<n> = n тактов тишины (карточка internal-studio-insert-sync, п.8–9) ----

// план как у реальных рендеров: 4/4, L:1/16, Q=120 → такт = 2 с.
// Секция A (0–8 с): Vocal поёт 4 такта, Ins молчит одной мультипаузой Z4.
// Секция B (8–16 с): Vocal молчит 4 такта (аккорды есть), Ins играет E/F/G/A.
const ZPLAN = [
  'X:1', 'T:', 'M:4/4', 'L:1/16', 'Q:1/4=120',
  'V: Vocal clef=treble name="Vocal Melody" snm="Vocal"',
  'V: Ins clef=treble name="Ins Melody" snm="Inst."',
  'K:Em',
  '% sectionA',
  'V: Vocal',
  '"Em"B4B2B2B4B2B2|"C"c2c2c2c2"D"B2A2A4|"Em"e4e4e4e4|"G"d4d4d4d4|',
  'V: Ins',
  'Z4|',
  '% sectionB',
  'V: Vocal',
  '"Am"z16|"C"z16|"D"z16|"Em"z16|',
  'V: Ins',
  'E4E4E4E4|F4F4F4F4|G4G4G4G4|A4A4A4A4|',
].join('\n')

// строки тела (с тактами) заданного голоса в порядке появления
function voiceLines(abc, name) {
  const out = []
  let cur = null
  for (const line of abc.split('\n')) {
    const t = line.trim()
    if (t.startsWith('V:')) { cur = t.slice(2).trim().split(/\s+/)[0]; continue }
    if (t.startsWith('%')) continue
    if (cur === name && t.includes('|')) out.push(t)
  }
  return out
}

// такты голоса (непустые чанки между |), без пробелов по краям
function voiceBarsText(abc, name) {
  return voiceLines(abc, name).flatMap((l) => l.split('|')).map((c) => c.trim()).filter(Boolean)
}


describe('разворот мультипауз (expandMultiRests)', () => {
  it('Z4| → 4 такта полной паузы z16', () => {
    const out = expandMultiRests('X:1\nM:4/4\nL:1/16\nK:C\nV: Ins\nZ4|\n')
    expect(voiceBarsText(out, 'Ins')).toEqual(['z16', 'z16', 'z16', 'z16'])
  })

  it('Z| без числа → ровно 1 такт', () => {
    const out = expandMultiRests('X:1\nM:4/4\nL:1/16\nK:C\nV: Ins\nZ|\n')
    expect(voiceBarsText(out, 'Ins')).toEqual(['z16'])
  })

  it('пробелы вокруг Z допустимы', () => {
    const out = expandMultiRests('X:1\nM:4/4\nL:1/16\nK:C\nV: Ins\n Z2 |\n')
    expect(voiceBarsText(out, 'Ins')).toEqual(['z16', 'z16'])
  })

  it('длина паузы — единиц L в такте (4/4 + 1/8 → z8)', () => {
    const out = expandMultiRests('X:1\nM:4/4\nL:1/8\nK:C\nV: Ins\nZ3|\n')
    expect(voiceBarsText(out, 'Ins')).toEqual(['z8', 'z8', 'z8'])
  })

  it('такт с нотами и аккордом рядом с мультипаузой не меняется', () => {
    const out = expandMultiRests('X:1\nM:4/4\nL:1/16\nK:Em\nV: Vocal\n"Em"B4B2B2B4B2B2|Z2|\n')
    expect(voiceBarsText(out, 'Vocal')).toEqual(['"Em"B4B2B2B4B2B2', 'z16', 'z16'])
  })

  it('строки без Z, заголовок, % и V: остаются идентичными', () => {
    const out = expandMultiRests(ZPLAN)
    const src = ZPLAN.split('\n')
    const got = out.split('\n')
    for (const l of src) {
      if (l.trim() === 'Z4|') continue
      expect(got).toContain(l)
    }
    expect(got).not.toContain('Z4|')
    expect(voiceBarsText(out, 'Ins').slice(0, 4)).toEqual(['z16', 'z16', 'z16', 'z16'])
  })

  it('план без мультипауз возвращается без изменений (обычная пауза z не трогается)', () => {
    expect(expandMultiRests(MULTI)).toBe(MULTI)
    expect(expandMultiRests(ABC)).toBe(ABC)
  })
})

describe('мультипауза в нарезке и приёмах (sliceAbc / applyTrick / sliceLeadSec)', () => {
  it('sliceAbc [10,12] без контекста → у Ins ровно такт F4F4F4F4 (6-й такт, Z4 = 4 такта)', () => {
    const out = sliceAbc(ZPLAN, 10, 12, 0)
    expect(voiceBarsText(out, 'Ins')).toEqual(['F4F4F4F4'])
    expect(voiceBarsText(out, 'Vocal')).toEqual(['"C"z16'])
  })

  it('sliceAbc: «проверить кусок» не глушит вокал (п.9 — меняется только мини-план вклейки)', () => {
    const out = sliceAbc(ZPLAN, 2, 4, 0)
    expect(voiceBarsText(out, 'Vocal')).toEqual(['"C"c2c2c2c2"D"B2A2A4'])
  })

  it('applyTrick rest на Ins, такт 5 → паузой становится F4F4F4F4', () => {
    const out = applyTrick(ZPLAN, { kind: 'rest', targets: [{ voice: 'Ins', bar: 5 }] })
    expect(out).not.toContain('F4F4F4F4')
    expect(out).toContain('E4E4E4E4')
    expect(out).toContain('G4G4G4G4')
    expect(out).toContain('A4A4A4A4')
    // вокал не тронут
    expect(out).toContain('"Em"B4B2B2B4B2B2|"C"c2c2c2c2"D"B2A2A4|"Em"e4e4e4e4|"G"d4d4d4d4|')
  })

  it('sliceLeadSec на плане с Z4 в начале у всех голосов — число ≥ 0', () => {
    const plan = [
      'X:1', 'M:4/4', 'L:1/16', 'Q:1/4=120', 'K:C',
      'V: Vocal', 'Z4|', 'c4c4c4c4|d4d4d4d4|',
      'V: Ins', 'Z4|', 'e4e4e4e4|f4f4f4f4|',
    ].join('\n')
    const lead = sliceLeadSec(plan, 9, 1)
    expect(Number.isFinite(lead)).toBe(true)
    expect(lead).toBeGreaterThanOrEqual(0)
    // Z4 = 4 такта по 2 с: from 9 в такте 8–10, контекст 1 такт → с 6 с
    expect(lead).toBeCloseTo(3, 6)
  })
})

describe('каталог TRICK_INSTRUMENTS: какие дорожки меняет каждый', () => {
  const ALLOWED = ['drums', 'bass', 'other']
  const byId = (id) => TRICK_INSTRUMENTS.find((i) => i.id === id)
  const sorted = (a) => [...a].sort()

  it('у каждого непустой stems из drums/bass/other, vocals — никогда', () => {
    expect(TRICK_INSTRUMENTS.length).toBeGreaterThan(0)
    for (const i of TRICK_INSTRUMENTS) {
      expect(Array.isArray(i.stems), `${i.id}: stems не массив`).toBe(true)
      expect(i.stems.length, `${i.id}: пустой stems`).toBeGreaterThan(0)
      for (const s of i.stems) expect(ALLOWED, `${i.id}: недопустимый стем ${s}`).toContain(s)
      expect(i.stems).not.toContain('vocals')
    }
  })

  it('id уникальны', () => {
    const ids = TRICK_INSTRUMENTS.map((i) => i.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('поля mode больше нет', () => {
    for (const i of TRICK_INSTRUMENTS) expect(i, i.id).not.toHaveProperty('mode')
  })

  it('bass → [bass], drumfill → [drums], buildup → все три', () => {
    expect(byId('bass')?.stems).toEqual(['bass'])
    expect(byId('drumfill')?.stems).toEqual(['drums'])
    expect(sorted(byId('buildup')?.stems || [])).toEqual(['bass', 'drums', 'other'])
  })

  // замена барабанов на сбивку: у старых барабанов вычитается только низ (бочка/малый/томы),
  // хэт и тарелки остаются — иначе трек «глохнет» в окне
  it('drumfill несёт keepHighHz 6000', () => {
    expect(byId('drumfill')?.keepHighHz).toBe(6000)
  })

  it('у остальных инструментов keepHighHz нет', () => {
    for (const i of TRICK_INSTRUMENTS) {
      if (i.id === 'drumfill') continue
      expect(i, i.id).not.toHaveProperty('keepHighHz')
    }
  })

  it('мелодические (eguitar, strings, flute, piano) → [other]', () => {
    for (const id of ['eguitar', 'strings', 'flute', 'piano']) {
      expect(byId(id)?.stems, id).toEqual(['other'])
    }
  })
})

describe('стиль рендера куска (sectionStyle)', () => {
  const inst = { id: 'flute', en: 'flute solo', stems: ['other'] }

  it('стиль трека + ", " + en инструмента', () => {
    expect(sectionStyle('pop, female vocal', inst)).toBe('pop, female vocal, flute solo')
  })

  it('хвостовая запятая и пробелы стиля трека срезаются', () => {
    expect(sectionStyle('pop, female vocal, ', inst)).toBe('pop, female vocal, flute solo')
    expect(sectionStyle('pop,', inst)).toBe('pop, flute solo')
    expect(sectionStyle('pop   ', inst)).toBe('pop, flute solo')
  })

  it('пустой стиль → только en', () => {
    expect(sectionStyle('', inst)).toBe('flute solo')
    expect(sectionStyle('   ', inst)).toBe('flute solo')
  })
})

describe('мягкие слова настроения и energetic-инструменты', () => {
  const byId = (id) => TRICK_INSTRUMENTS.find((i) => i.id === id)
  // синтетический energetic-инструмент: проверяем правило, не текст каталога
  const loud = { id: 'x-loud', en: 'loud riff', stems: ['other'], energetic: true }
  const FOLK = 'instrumental, acoustic folk, calm fingerpicked acoustic guitar riff, warm, intimate, sparse, 90 BPM'

  it('SOFT_MOOD_WORDS содержит базовый набор мягких слов', () => {
    expect(Array.isArray(SOFT_MOOD_WORDS)).toBe(true)
    for (const w of ['calm', 'sparse', 'intimate', 'soft', 'quiet', 'gentle', 'mellow', 'delicate']) {
      expect(SOFT_MOOD_WORDS, w).toContain(w)
    }
  })

  it('eguitar, buildup, drumfill помечены energetic: true', () => {
    for (const id of ['eguitar', 'buildup', 'drumfill']) {
      expect(byId(id)?.energetic, id).toBe(true)
    }
  })

  it('у strings, piano, flute, bass флага energetic нет', () => {
    for (const id of ['strings', 'piano', 'flute', 'bass']) {
      expect(byId(id), id).toBeDefined()
      expect(byId(id)?.energetic, id).toBeFalsy()
    }
  })

  it('eguitar.en — нарастающая перегруженная электрогитара', () => {
    expect(byId('eguitar')?.en).toBe('electric guitar enters and builds, crunchy overdriven electric guitar, rising intensity')
  })

  it('energetic: мягкие слова вырезаются из стиля трека, в т.ч. внутри тега; пустые теги выкидываются', () => {
    const eg = byId('eguitar')
    expect(sectionStyle(FOLK, eg)).toBe('instrumental, acoustic folk, fingerpicked acoustic guitar riff, warm, 90 BPM, ' + eg.en)
    expect(sectionStyle(FOLK, loud)).toBe('instrumental, acoustic folk, fingerpicked acoustic guitar riff, warm, 90 BPM, loud riff')
  })

  it('не-energetic (strings): стиль трека не меняется', () => {
    const st = byId('strings')
    expect(sectionStyle(FOLK, st)).toBe(FOLK + ', ' + st.en)
  })

  it('регистр не важен: "Calm, SOFT piano" → "piano"', () => {
    expect(sectionStyle('Calm, SOFT piano', loud)).toBe('piano, loud riff')
  })

  it('лишние пробелы после вырезания схлопываются', () => {
    expect(sectionStyle('warm  gentle   pads, mellow', loud)).toBe('warm pads, loud riff')
  })

  it('слово внутри другого слова не трогается (softness, calmer)', () => {
    expect(sectionStyle('softness, calmer mood', loud)).toBe('softness, calmer mood, loud riff')
  })

  it('energetic, пустой стиль → только en', () => {
    expect(sectionStyle('', loud)).toBe('loud riff')
    expect(sectionStyle('', byId('eguitar'))).toBe(byId('eguitar').en)
  })

  it('energetic, стиль только из мягких слов → только en', () => {
    expect(sectionStyle('calm, quiet, ', loud)).toBe('loud riff')
  })
})

describe('sectionWindows — окна секции в голосе', () => {
  // такт таймлайна ролла: секция, окно в секундах, голоса такта
  const bar = (section, start, end, voices = { Vocal: {}, Ins: {} }) => ({ section, start_sec: start, end_sec: end, voices })
  const bars = [
    bar('intro', 0, 2),
    bar('verse', 2, 4), bar('verse', 4, 6),
    bar('chorus', 6, 8),
    bar('verse', 8, 10), bar('verse', 10, 12),
    bar('outro', 12, 14),
  ]

  it('все куплеты: подряд идущие такты сливаются в одно окно, между куплетами — разрыв', () => {
    expect(sectionWindows(bars, 'Vocal', 'verse')).toEqual([{ from: 2, to: 6 }, { from: 8, to: 12 }])
  })

  it('одиночный такт секции — окно ровно в такт', () => {
    expect(sectionWindows(bars, 'Ins', 'chorus')).toEqual([{ from: 6, to: 8 }])
  })

  it('такты, где голоса нет, в окна не попадают', () => {
    const b = [bar('verse', 0, 2, { Ins: {} }), bar('verse', 2, 4, { Vocal: {}, Ins: {} })]
    expect(sectionWindows(b, 'Vocal', 'verse')).toEqual([{ from: 2, to: 4 }])
  })

  it('пусто / нет такой секции / нет такого голоса → []', () => {
    expect(sectionWindows([], 'Vocal', 'verse')).toEqual([])
    expect(sectionWindows(null, 'Vocal', 'verse')).toEqual([])
    expect(sectionWindows(bars, 'Vocal', 'bridge')).toEqual([])
    expect(sectionWindows(bars, 'Нет', 'verse')).toEqual([])
  })

  it('TRICK_MUTES — приёмы громкости без рендера: у каждого id, дорожки и db', () => {
    expect(TRICK_MUTES.length).toBeGreaterThan(0)
    for (const t of TRICK_MUTES) {
      expect(typeof t.id).toBe('string')
      expect(Array.isArray(t.mute) && t.mute.length > 0).toBe(true)
      expect(typeof t.db).toBe('number')
    }
  })
})

// ── Голос-мелодия: потолок, «голос выше», «вариации мотива», «заново с места» ──

// план K:Em: Vocal — два такта, верхняя нота c; Ins выше вокала (не должен влиять на потолок)
const VPLAN = [
  'X:1', 'T:', 'M:4/4', 'L:1/16', 'Q:1/4=120',
  'V: Vocal clef=treble name="Vocal Melody" snm="Vocal"',
  'V: Ins clef=treble name="Ins Melody" snm="Inst."',
  'K:Em',
  '% verse',
  'V: Vocal',
  '"Em"B4B2B2B2B2B2B2|"C"c2c2c2c2"D"B2A2A4|',
  'V: Ins',
  'a4a4a4a4|g4g4g4g4|',
].join('\n')

const vplan = (vocalLine, insLine = 'E4E4E4E4|') => [
  'X:1', 'M:4/4', 'L:1/16', 'K:Em', 'V: Vocal', vocalLine, 'V: Ins', insLine,
].join('\n')

// ступень ноты: C=0 … B=6, строчные +7, ' — +7, , — −7
const STEP = { C: 0, D: 1, E: 2, F: 3, G: 4, A: 5, B: 6 }
function noteStep(n) {
  const m = /^([A-Ga-g])([',]*)$/.exec(n)
  if (!m) throw new Error('не нота: ' + n)
  let s = STEP[m[1].toUpperCase()] + (m[1] === m[1].toLowerCase() ? 7 : 0)
  for (const c of m[2]) s += c === "'" ? 7 : -7
  return s
}

// токены такта без аккордов: [{rest, note, dur}]
function barTokens(bar) {
  const body = bar.replace(/"[^"]*"/g, '')
  const out = []
  const re = /(z|[A-Ga-g][',]*)(\d*)/g
  let m
  while ((m = re.exec(body))) {
    out.push({ rest: m[1] === 'z', note: m[1] === 'z' ? null : m[1], dur: m[2] ? Number(m[2]) : 1 })
  }
  return out
}
const chordsOf = (bar) => bar.match(/"[^"]*"/g) || []
const notesOf = (bar) => barTokens(bar).filter((t) => !t.rest).map((t) => t.note)

describe('потолок голоса (vocalCeiling)', () => {
  it('верхняя нота «c» → потолок «e» (+2 ступени); Ins выше вокала не учитывается', () => {
    expect(vocalCeiling(VPLAN)).toBe('e')
  })

  it('верхняя нота «B» → «d»', () => {
    expect(vocalCeiling(vplan('"Em"B4B4A4G4|'))).toBe('d')
  })

  it('берётся весь план: верхняя «e» во второй строке голоса после пауз → «g»', () => {
    // ZPLAN: Vocal …|"Em"e4e4e4e4|… и далее строка пауз
    expect(vocalCeiling(ZPLAN)).toBe('g')
    const two = [
      'X:1', 'M:4/4', 'L:1/16', 'K:Em',
      'V: Vocal', 'B4B4B4B4|', 'V: Ins', 'E4E4E4E4|',
      'V: Vocal', 'z8e8|', 'V: Ins', 'E4E4E4E4|',
    ].join('\n')
    expect(vocalCeiling(two)).toBe('g')
  })

  it('нет голоса Vocal → null', () => {
    expect(vocalCeiling('X:1\nM:4/4\nL:1/16\nK:Em\nV: Ins\nE4E4E4E4|\n')).toBeNull()
  })

  it('в голосе Vocal только паузы (z и Z) → null', () => {
    expect(vocalCeiling(vplan('"Em"z16|Z2|'))).toBeNull()
  })
})

describe('приём «голос выше» (vocalUp)', () => {
  const both = [{ voice: 'Vocal', bar: 0 }, { voice: 'Vocal', bar: 1 }]

  it('каждая нота целевых тактов — на 2 ступени вверх, аккорды целы', () => {
    const out = applyTrick(VPLAN, { kind: 'vocalUp', targets: both, ceiling: 'e' })
    expect(voiceBarsText(out, 'Vocal')).toEqual(['"Em"d4d2d2d2d2d2d2', '"C"e2e2e2e2"D"d2c2c4'])
  })

  it('нота выше потолка после сдвига становится потолком: d при потолке e → e, не f', () => {
    const out = applyTrick(vplan('"G"d4d4d4d4|'), { kind: 'vocalUp', targets: [{ voice: 'Vocal', bar: 0 }], ceiling: 'e' })
    expect(voiceBarsText(out, 'Vocal')).toEqual(['"G"e4e4e4e4'])
  })

  it('сдвиг через октаву записи: b → d\' (при потолке e\')', () => {
    const out = applyTrick(vplan('b4b4b4b4|'), { kind: 'vocalUp', targets: [{ voice: 'Vocal', bar: 0 }], ceiling: "e'" })
    expect(voiceBarsText(out, 'Vocal')).toEqual(["d'4d'4d'4d'4"])
  })

  it('паузы остаются, длительности не меняются', () => {
    const out = applyTrick(vplan('"Am"z4B4z2B2B4|'), { kind: 'vocalUp', targets: [{ voice: 'Vocal', bar: 0 }], ceiling: 'e' })
    expect(voiceBarsText(out, 'Vocal')).toEqual(['"Am"z4d4z2d2d4'])
  })

  it('нецелевые такты и другие голоса не меняются', () => {
    const out = applyTrick(VPLAN, { kind: 'vocalUp', targets: [{ voice: 'Vocal', bar: 1 }], ceiling: 'e' })
    expect(voiceBarsText(out, 'Vocal')).toEqual(['"Em"B4B2B2B2B2B2B2', '"C"e2e2e2e2"D"d2c2c4'])
    expect(voiceBarsText(out, 'Ins')).toEqual(voiceBarsText(VPLAN, 'Ins'))
  })

  it('потолок не передан → берётся vocalCeiling(abc)', () => {
    const plan = vplan('"Em"B4B4B4B4|"G"g4g4g4g4|"C"e4f4g4e4|')
    const t = [{ voice: 'Vocal', bar: 0 }, { voice: 'Vocal', bar: 1 }, { voice: 'Vocal', bar: 2 }]
    const auto = applyTrick(plan, { kind: 'vocalUp', targets: t })
    expect(auto).toBe(applyTrick(plan, { kind: 'vocalUp', targets: t, ceiling: vocalCeiling(plan) }))
    // верх g → потолок b: g→b, e→g, f→a
    expect(voiceBarsText(auto, 'Vocal')).toEqual(['"Em"d4d4d4d4', '"G"b4b4b4b4', '"C"g4a4b4g4'])
  })
})

describe('приём «вариации мотива» (vocalVary)', () => {
  const plan = vplan('"Em"B4B2B2B2B2B2B2|"C"c2c2c2c2"D"B2A2A4|"Em"B4B4B4B4|', 'E4E4E4E4|F4F4F4F4|G4G4G4G4|')
  const targets = [{ voice: 'Vocal', bar: 0 }, { voice: 'Vocal', bar: 1 }]
  const run = (abc = plan, t = targets, ceiling = 'e') => applyTrick(abc, { kind: 'vocalVary', targets: t, ceiling })

  it('ритм сохраняется: та же последовательность длительностей и пауз, то же число нот', () => {
    const src = voiceBarsText(plan, 'Vocal')
    const out = voiceBarsText(run(), 'Vocal')
    expect(out).toHaveLength(src.length)
    for (const i of [0, 1]) {
      expect(barTokens(out[i]).map((t) => [t.rest, t.dur])).toEqual(barTokens(src[i]).map((t) => [t.rest, t.dur]))
    }
  })

  it('паузы на своих местах в такте с паузами', () => {
    const p = vplan('"Am"z4B4z2B2c4|')
    const out = voiceBarsText(run(p, [{ voice: 'Vocal', bar: 0 }]), 'Vocal')
    expect(barTokens(out[0]).map((t) => [t.rest, t.dur])).toEqual(barTokens('z4B4z2B2c4').map((t) => [t.rest, t.dur]))
  })

  it('первая нота каждого целевого такта не меняется, аккорды целы', () => {
    const src = voiceBarsText(plan, 'Vocal')
    const out = voiceBarsText(run(), 'Vocal')
    for (const i of [0, 1]) {
      expect(notesOf(out[i])[0]).toBe(notesOf(src[i])[0])
      expect(chordsOf(out[i])).toEqual(chordsOf(src[i]))
    }
  })

  it('в целевом участке изменена хотя бы одна нота', () => {
    const src = voiceBarsText(plan, 'Vocal').slice(0, 2).flatMap(notesOf)
    const out = voiceBarsText(run(), 'Vocal').slice(0, 2).flatMap(notesOf)
    expect(out).not.toEqual(src)
  })

  it('все ноты не выше потолка', () => {
    for (const c of ['e', 'c']) {
      const out = voiceBarsText(run(plan, targets, c), 'Vocal').slice(0, 2).flatMap(notesOf)
      for (const n of out) expect(noteStep(n)).toBeLessThanOrEqual(noteStep(c))
    }
  })

  it('детерминирован: два вызова дают одно и то же', () => {
    expect(run()).toBe(run())
  })

  it('такт из одинаковых нот: последняя нота отличается от исходной', () => {
    const out = voiceBarsText(run(plan, [{ voice: 'Vocal', bar: 2 }]), 'Vocal')
    const notes = notesOf(out[2])
    expect(notes).toHaveLength(4)
    expect(notes[0]).toBe('B')
    expect(notes[3]).not.toBe('B')
    expect(barTokens(out[2]).map((t) => t.dur)).toEqual([4, 4, 4, 4])
  })

  it('нецелевые такты и другие голоса не меняются', () => {
    const out = run()
    expect(voiceBarsText(out, 'Vocal')[2]).toBe('"Em"B4B4B4B4')
    expect(voiceBarsText(out, 'Ins')).toEqual(voiceBarsText(plan, 'Ins'))
  })
})

describe('pickTargets для «голос выше» / «вариации»', () => {
  for (const kind of ['vocalUp', 'vocalVary']) {
    it(`${kind}: только вокальный голос выбранных позиций; без вокала — null`, () => {
      expect(pickTargets(VOICE_BARS, 1, 2, kind).targets)
        .toEqual([{ voice: 'Vocal', bar: 1 }, { voice: 'Vocal', bar: 2 }])
      expect(pickTargets({ Ins: VOICE_BARS.Ins }, 0, 1, kind)).toBeNull()
    })
  }
})

describe('TRICK_MUTES: заглушить голос без генерации', () => {
  it('есть vocalstop: mute vocals, −100 дБ', () => {
    const t = TRICK_MUTES.find((x) => x.id === 'vocalstop')
    expect(t).toBeTruthy()
    expect(t.mute).toEqual(['vocals'])
    expect(t.db).toBe(-100)
  })
})

describe('план для «заново с места» (continuationPlan)', () => {
  const up = { kind: 'octave', dir: 'up', targets: [{ voice: 'Vocal', bar: 0 }] }

  it('октава вверх применяется ровно один раз (регрессия: было две октавы)', () => {
    const src = 'X:1\nM:4/4\nL:1/16\nK:Dm\nV: Vocal\n"Dm"d2A2g2e2|\n'
    const out = continuationPlan(src, [up])
    expect(out).toContain('"Dm"d\'2a2g\'2e\'2|')
    expect(out).not.toContain("d''")
    expect(out).toBe(applyTrick(src, up))
  })

  it('«голос выше» — ровно на 2 ступени, не на 4', () => {
    const spec = { kind: 'vocalUp', targets: [{ voice: 'Vocal', bar: 0 }], ceiling: "c'" }
    const out = continuationPlan(vplan('"Em"B4B4B4B4|'), [spec])
    expect(voiceBarsText(out, 'Vocal')).toEqual(['"Em"d4d4d4d4'])
  })

  it('несколько specs — каждый по разу, по порядку', () => {
    const rest = { kind: 'rest', targets: [{ voice: 'Ins', bar: 1 }] }
    const vu = { kind: 'vocalUp', targets: [{ voice: 'Vocal', bar: 0 }], ceiling: 'e' }
    expect(continuationPlan(VPLAN, [vu, rest])).toBe(applyTrick(applyTrick(VPLAN, vu), rest))
  })

  it('specs пустой → пустая строка (продолжение без изменённого плана)', () => {
    expect(continuationPlan(VPLAN, [])).toBe('')
  })
})

describe('потолок голоса не уползает от повторов', () => {
  it('два «голос выше» без явного потолка: ноты не выше потолка исходного плана', () => {
    const src = vplan('"Em"B4B4B4B4|"C"c4c4B4A4|')   // верх c → потолок e
    const t = [{ voice: 'Vocal', bar: 0 }, { voice: 'Vocal', bar: 1 }]
    const out = continuationPlan(src, [{ kind: 'vocalUp', targets: t }, { kind: 'vocalUp', targets: t }])
    expect(voiceBarsText(out, 'Vocal')).toEqual(['"Em"e4e4e4e4', '"C"e4e4e4e4'])
  })
})
