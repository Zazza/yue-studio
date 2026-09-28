// Тесты приёмов над ABC-планом: разбор/сборка и мутации тактов.
import { describe, it, expect } from 'vitest'
import { splitBars, assemble, barUnits, keyRoot, borrowedChord, applyTrick, pickTargets, sliceAbc, TRICK_INSTRUMENTS, trickStyleSuffix } from './abcEdit.js'

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
