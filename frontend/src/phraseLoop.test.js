// Тесты карточки internal-own-track, этап 13, условия 97–98 (тест-кейс ТК136) и этап 13б, условие 101 (ТК138):
// phraseLoop.js — phaseAfter(phaseStart, posSec, cycleOld, cycleNew), familyOf(preset), phraseStems(preset),
// pagePresets(list), phraseChain(chain, preset, phrase, tempo); preset — {id, stems:[...], chain}.
// Страница «Инструменты» — по исходнику InstrumentsPage.vue. Этап 13б поменял спецификацию: синты и перкуссия
// показываются на странице (familyOf synth → synth, perc → perc). Написаны по карточке, без чтения реализации.
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, it, expect } from 'vitest'
import { phaseAfter, familyOf, phraseStems, pagePresets, phraseChain } from './phraseLoop.js'
import { partNotes } from './synthPart.js'

const preset = (id, stems) => ({ id, stems, chain: [{ type: 'eq' }] })
const DRUM_PARTS = ['kick', 'snare', 'toms', 'hh', 'ride', 'crash']

describe('phaseAfter (условие 97, ТК136)', () => {
  it('тот же круг: место = фаза + пройденное', () => {
    expect(phaseAfter(0, 1, 4, 4)).toBeCloseTo(1, 9)
    expect(phaseAfter(1.5, 1, 4, 4)).toBeCloseTo(2.5, 9)
  })

  it('переход через конец круга — по модулю', () => {
    expect(phaseAfter(3, 2, 4, 4)).toBeCloseTo(1, 9)
    expect(phaseAfter(0, 9, 4, 4)).toBeCloseTo(1, 9) // два с лишним круга
  })

  it('cycleNew ≠ cycleOld: сохраняется доля круга', () => {
    expect(phaseAfter(0, 1, 4, 8)).toBeCloseTo(2, 9) // четверть старого → четверть нового
    expect(phaseAfter(1, 1, 4, 2)).toBeCloseTo(1, 9) // половина
    expect(phaseAfter(3, 2, 4, 6)).toBeCloseTo(1.5, 9) // (5 mod 4)/4 · 6
  })

  it('ровно на конце круга → начало нового', () => {
    expect(phaseAfter(2, 2, 4, 5)).toBeCloseTo(0, 9)
  })

  it('результат всегда в [0, cycleNew)', () => {
    for (const [ps, pos, co, cn] of [[0, 0, 4, 3], [3.9, 100.3, 4, 2], [0.5, 7.25, 2.5, 10]]) {
      const v = phaseAfter(ps, pos, co, cn)
      expect(v).toBeGreaterThanOrEqual(0)
      expect(v).toBeLessThan(cn)
    }
  })
})

describe('familyOf (условия 97, 101; ТК136, ТК138)', () => {
  it.each([
    [['guitar'], 'guitar'],
    [['other'], 'guitar'],
    [['piano'], 'guitar'],
    [['guitar', 'other'], 'guitar'],
    [['other', 'piano'], 'guitar'],
    [['bass'], 'bass'],
    [['drums'], 'drums'],
    ...DRUM_PARTS.map((p) => [[p], 'drums']),
    [['synth'], 'synth'], // этап 13б, условие 101: синты — на странице
    [['perc'], 'perc'], // перкуссия — на странице (фразы — барабанные)
  ])('stems %j → %s', (stems, fam) => {
    expect(familyOf(preset('x', stems))).toBe(fam)
  })

  it.each([[['vocals']], [['master']], [['mix']], [[]]])(
    'stems %j → null (не инструмент страницы: голос, мастер — по-прежнему нет)',
    (stems) => {
      expect(familyOf(preset('x', stems))).toBeNull()
    },
  )
})

describe('phraseStems (условие 97, ТК136)', () => {
  it('гитара → [guitar] (в т.ч. пресеты на other/piano)', () => {
    for (const stems of [['guitar'], ['other'], ['piano'], ['guitar', 'other']]) {
      expect(phraseStems(preset('g', stems))).toEqual(['guitar'])
    }
  })

  it('бас → [bass]', () => {
    expect(phraseStems(preset('b', ['bass']))).toEqual(['bass'])
  })

  it('drums → все части барабанов', () => {
    expect([...phraseStems(preset('d', ['drums']))].sort()).toEqual([...DRUM_PARTS].sort())
  })

  it.each(DRUM_PARTS)('часть %s → только она', (part) => {
    expect(phraseStems(preset('p', [part]))).toEqual([part])
  })

  it('синт → [synth] (условие 101)', () => {
    expect(phraseStems(preset('s', ['synth']))).toEqual(['synth'])
  })

  it('перкуссия → [perc] (условие 101)', () => {
    expect(phraseStems(preset('pc', ['perc']))).toEqual(['perc'])
  })
})

describe('pagePresets (условия 97, 101; ТК136, ТК138)', () => {
  it('оставляет гитару/бас/барабаны/синт/перкуссию, выкидывает голос, мастер — порядок сохранён', () => {
    const list = [
      preset('synth', ['synth']),
      preset('gtr', ['guitar', 'other']),
      preset('perc', ['perc']),
      preset('bass', ['bass']),
      preset('voice', ['vocals']),
      preset('kick', ['kick']),
      preset('master', ['master']),
      preset('mix', ['mix']),
      preset('empty', []),
      preset('drums', ['drums']),
    ]
    expect(pagePresets(list).map((p) => p.id)).toEqual(['synth', 'gtr', 'perc', 'bass', 'kick', 'drums'])
  })

  it('пустой список → пустой', () => {
    expect(pagePresets([])).toEqual([])
  })

  it('отдаёт сами объекты пресетов (с цепочкой)', () => {
    const g = preset('gtr', ['guitar'])
    expect(pagePresets([g])[0]).toBe(g)
  })
})

describe('phraseChain (условие 101, ТК138)', () => {
  const synthPhrase = { id: 'synth-pad', family: 'synth', beats: 16, chords: ['Em', 'C', 'G', 'D'], style: 'pad', cycle_sec: 9.6 }
  const synthPreset = { id: 'pad', stems: ['synth'], style: 'pad', octave: 0 }
  const chain = () => [
    { type: 'synth', osc1: 1, release_s: 0.5 },
    { type: 'reverb', decay_s: 2, wet: 0.4 },
  ]
  // такты круга по карточке: beats/4 тактов, длина такта = cycle_sec / tempo / тактов, аккорды из chords по кругу
  const bars = (phrase, tempo) => {
    const n = phrase.beats / 4
    const len = phrase.cycle_sec / tempo / n
    return Array.from({ length: n }, (_, i) => ({ start: i * len, end: (i + 1) * len, chord: phrase.chords[i % (phrase.chords.length || 1)], section: '' }))
  }

  it('pad, tempo 1: synth-блок получил 4 ноты-аккорда с t = 0, 2.4, 4.8, 7.2', () => {
    const out = phraseChain(chain(), synthPreset, synthPhrase, 1)
    const notes = out[0].notes
    expect(notes).toHaveLength(4)
    ;[0, 2.4, 4.8, 7.2].forEach((t, i) => {
      expect(notes[i].t).toBeCloseTo(t, 9)
      expect(notes[i].d).toBeCloseTo(2.4, 9)
      expect(Array.isArray(notes[i].midi)).toBe(true)
      expect(notes[i].midi.length).toBeGreaterThanOrEqual(3) // аккорд, а не одна нота
    })
    // высоты — те же, что даёт партия synthPart по этим тактам (Em C G D)
    expect(notes.map((n) => n.midi)).toEqual(partNotes(bars(synthPhrase, 1), { style: 'pad', octave: 0 }).map((n) => n.midi))
  })

  it('tempo 1.5 → те же ноты, t и d в 1,5 раза меньше', () => {
    const slow = phraseChain(chain(), synthPreset, synthPhrase, 1)[0].notes
    const fast = phraseChain(chain(), synthPreset, synthPhrase, 1.5)[0].notes
    expect(fast).toHaveLength(slow.length)
    fast.forEach((n, i) => {
      expect(n.t).toBeCloseTo(slow[i].t / 1.5, 9)
      expect(n.d).toBeCloseTo(slow[i].d / 1.5, 9)
      expect(n.midi).toEqual(slow[i].midi)
    })
    expect(fast.map((n) => n.t)).toEqual([0, 1.6, 3.2, 4.8].map((t) => expect.closeTo(t, 9)))
  })

  it('octave пресета сдвигает ноты на октаву', () => {
    const base = phraseChain(chain(), synthPreset, synthPhrase, 1)[0].notes
    const up = phraseChain(chain(), { ...synthPreset, octave: 1 }, synthPhrase, 1)[0].notes
    up.forEach((n, i) => expect(n.midi).toEqual(base[i].midi.map((m) => m + 12)))
  })

  it('прочие блоки без изменений, порядок блоков тот же, параметры synth-блока сохранены', () => {
    const src = chain()
    const out = phraseChain(src, synthPreset, synthPhrase, 1)
    expect(out.map((b) => b.type)).toEqual(['synth', 'reverb'])
    expect(out[1]).toEqual(src[1])
    expect(out[0]).toMatchObject({ type: 'synth', osc1: 1, release_s: 0.5 })
  })

  it('входная цепочка и её объекты не мутируются', () => {
    const src = chain()
    const before = JSON.parse(JSON.stringify(src))
    const presetBefore = JSON.parse(JSON.stringify(synthPreset))
    const phraseBefore = JSON.parse(JSON.stringify(synthPhrase))
    const out = phraseChain(src, synthPreset, synthPhrase, 1)
    expect(src).toEqual(before)
    expect(src[0].notes).toBeUndefined()
    expect(out[0]).not.toBe(src[0])
    expect(synthPreset).toEqual(presetBefore)
    expect(synthPhrase).toEqual(phraseBefore)
  })

  it('perc, eighths: 8 ударов на такт, 2 такта → 16, t кратны 0.25', () => {
    const percPreset = { id: 'shaker', stems: ['perc'], pattern: 'eighths' }
    const drums = { id: 'drums-x', family: 'drums', beats: 8, cycle_sec: 4.0, chords: [] }
    const src = [{ type: 'perc', voice: 1 }]
    const out = phraseChain(src, percPreset, drums, 1)
    expect(out).toHaveLength(1)
    expect(out[0]).toMatchObject({ type: 'perc', voice: 1 })
    const hits = out[0].notes
    expect(hits).toHaveLength(16)
    hits.forEach((h, i) => {
      expect(h.t).toBeCloseTo(i * 0.25, 9)
      expect(h.t / 0.25).toBeCloseTo(Math.round(h.t / 0.25), 9)
      expect(h.t).toBeLessThan(4.0)
    })
    expect(src).toEqual([{ type: 'perc', voice: 1 }])
  })

  it('цепочка без synth/perc — блоки те же', () => {
    const src = [{ type: 'eq', low_db: 3 }, { type: 'gain', gain_db: -2 }]
    const out = phraseChain(src, synthPreset, synthPhrase, 1)
    expect(out).toEqual(src)
  })

  it('пустая цепочка → пустая', () => {
    expect(phraseChain([], synthPreset, synthPhrase, 1)).toEqual([])
  })
})

describe('страница «Инструменты» по исходнику (условие 98, ТК136)', () => {
  const here = dirname(fileURLToPath(import.meta.url))
  const src = readFileSync(join(here, 'components', 'InstrumentsPage.vue'), 'utf8')

  it('нет выбора трека/дорожки и секунд', () => {
    expect(src).not.toMatch(/\bjobId\b/)
    expect(src).not.toMatch(/['"]instr\.start['"]/)
    expect(src).not.toMatch(/['"]instr\.len['"]/)
  })

  it('нет «→ в треки», «→ в студию», «было/стало»', () => {
    expect(src).not.toMatch(/\btoTrack\b/)
    expect(src).not.toMatch(/\btoStudio\b/)
    expect(src).not.toMatch(/\bplayBefore\b/)
  })

  it('расчёт фразы и игра по кругу — через api.fxPhrase и api.playLoop', () => {
    expect(src).toMatch(/api\.fxPhrase\s*\(/)
    expect(src).toMatch(/api\.playLoop\s*\(/)
  })

  it('продолжение с того же места круга — через phaseAfter', () => {
    expect(src).toMatch(/\bphaseAfter\b/)
  })

  it('есть галочка «обработка» (checkbox, связанный с bypass/fx)', () => {
    // тег <input …> целиком (атрибуты могут быть на разных строках) либо одна строка
    const tags = src.match(/<input\b[^>]*>/g) || []
    const chunks = [...tags, ...src.split('\n')]
    const ok = chunks.some((l) => /type=["']checkbox["']/.test(l) && /bypass|instr\.fx\b/i.test(l))
    expect(ok, 'нет checkbox, связанного с bypass/instr.fx').toBe(true)
  })
})
