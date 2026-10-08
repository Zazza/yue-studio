// Тесты карточки internal-own-track, этап 4 «Синты», условие 22 (тест-кейс ТК46):
// synthPart.js — ноты синт-партии по сетке аккордов трека.
// Контракт: chordPcs(name) — классы высот аккорда (как chord_pcs воркера: основной тон,
// терция, квинта, (септима); нераспознанный → null); partNotes(bars, {style, octave = 0,
// sections = null}) → [{t, d, midi[], vel}] в секундах трека, midi по возрастанию;
// bars — [{start, end, chord, section}] из /chord_grid. Регистр pad/arp/drone — C4…B4
// (60…71), pulse — C2…B2 (36…47); octave — сдвиг на 12·octave. pad — трезвучие на весь
// такт; arp — 8 нот на такт по 1/8 такта по кругу трезвучия снизу вверх; pulse — основной
// тон восьмыми; drone — основной тон + квинта, тянется, пока аккорд тот же. Такты вне
// выбранных секций и с нераспознанным аккордом — без нот.
// Написаны по карточке и контракту, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { chordPcs, partNotes } from './synthPart.js'

const bar = (start, end, chord, section = 'verse') => ({ start, end, chord, section })
const close = (a, b, eps = 1e-6) => Math.abs(a - b) <= eps

describe('chordPcs — классы высот аккорда', () => {
  it.each([
    ['Dm', [2, 5, 9]],
    ['Bb', [10, 2, 5]],
    ['C', [0, 4, 7]],
    ['A', [9, 1, 4]],
    ['F#m', [6, 9, 1]],
    ['Eb', [3, 7, 10]],
    ['Bdim', [11, 2, 5]],
    ['G7', [7, 11, 2, 5]],
    ['Cmaj7', [0, 4, 7, 11]],
    ['Am7', [9, 0, 4, 7]],
    ['Dsus2', [2, 4, 9]],
    ['Dsus4', [2, 7, 9]],
  ])('%s → %j', (name, want) => {
    expect(chordPcs(name)).toEqual(want)
  })

  it.each(['Xyz', '', 'm'])('нераспознанный «%s» → null', (name) => {
    expect(chordPcs(name)).toBeNull()
  })
})

describe('partNotes: pad (ТК46)', () => {
  const pad = (chord) => partNotes([bar(0, 2, chord)], { style: 'pad' })

  it('Dm, такт 0–2 с → одна нота {t 0, d 2, midi [62, 65, 69]}', () => {
    const notes = pad('Dm')
    expect(notes.length).toBe(1)
    expect(notes[0].t).toBeCloseTo(0, 6)
    expect(notes[0].d).toBeCloseTo(2, 6)
    expect(notes[0].midi).toEqual([62, 65, 69])
    expect(typeof notes[0].vel).toBe('number')
    expect(notes[0].vel).toBeGreaterThan(0)
    expect(notes[0].vel).toBeLessThanOrEqual(1)
  })

  it.each([
    ['C', [60, 64, 67]],
    ['Bb', [62, 65, 70]],
    ['F#m', [61, 66, 69]],
  ])('%s → %j', (chord, want) => {
    expect(pad(chord)[0].midi).toEqual(want)
  })

  it.each(['C', 'Dm', 'Bb', 'F#m', 'Bdim', 'G7', 'Cmaj7', 'Am7', 'Dsus2', 'Dsus4', 'B', 'Ab'])(
    '%s: каждая нота в C4…B4, по возрастанию, классы высот — аккорда', (chord) => {
      const [n] = pad(chord)
      for (const m of n.midi) {
        expect(m).toBeGreaterThanOrEqual(60)
        expect(m).toBeLessThanOrEqual(71)
      }
      expect(n.midi).toEqual([...n.midi].sort((a, b) => a - b))
      expect(new Set(n.midi.map((m) => m % 12))).toEqual(new Set(chordPcs(chord)))
    })

  it('время — секунды трека: такт 10–12,4 с → t 10, d 2,4', () => {
    const [n] = partNotes([bar(10, 12.4, 'Dm')], { style: 'pad' })
    expect(n.t).toBeCloseTo(10, 6)
    expect(n.d).toBeCloseTo(2.4, 6)
  })

  it('по ноте на такт: Dm C → две ноты подряд', () => {
    const notes = partNotes([bar(0, 2, 'Dm'), bar(2, 4, 'C')], { style: 'pad' })
    expect(notes.map((n) => n.midi)).toEqual([[62, 65, 69], [60, 64, 67]])
    expect(notes.map((n) => n.t)).toEqual([0, 2])
  })
})

describe('partNotes: arp (ТК46)', () => {
  it('такт 2 с → 8 нот по 0,25 с, по кругу трезвучия снизу', () => {
    const notes = partNotes([bar(0, 2, 'Dm')], { style: 'arp' })
    expect(notes.length).toBe(8)
    notes.forEach((n, i) => {
      expect(close(n.t, i * 0.25), `t[${i}] = ${n.t}`).toBe(true)
      expect(n.d).toBeGreaterThan(0)
      expect(n.d).toBeLessThanOrEqual(0.25 + 1e-9)
      expect(n.midi.length).toBe(1)
    })
    expect(notes.map((n) => n.midi[0])).toEqual([62, 65, 69, 62, 65, 69, 62, 65])
  })

  it('такт другой длины (2,4 с) → 8 нот по 0,3 с от начала такта', () => {
    const notes = partNotes([bar(4, 6.4, 'C')], { style: 'arp' })
    expect(notes.length).toBe(8)
    notes.forEach((n, i) => expect(close(n.t, 4 + i * 0.3), `t[${i}] = ${n.t}`).toBe(true))
    expect(notes.map((n) => n.midi[0])).toEqual([60, 64, 67, 60, 64, 67, 60, 64])
  })
})

describe('partNotes: pulse (ТК46)', () => {
  it('A → 8 нот midi [45] (A2) восьмыми', () => {
    const notes = partNotes([bar(0, 2, 'A')], { style: 'pulse' })
    expect(notes.length).toBe(8)
    notes.forEach((n, i) => {
      expect(n.midi).toEqual([45])
      expect(close(n.t, i * 0.25), `t[${i}] = ${n.t}`).toBe(true)
      expect(n.d).toBeGreaterThan(0)
      expect(n.d).toBeLessThanOrEqual(0.25 + 1e-9)
    })
  })

  it.each([['C', 36], ['B', 47], ['Dm', 38], ['F#m', 42]])('%s → основной тон в C2…B2: %i', (chord, m) => {
    const notes = partNotes([bar(0, 2, chord)], { style: 'pulse' })
    for (const n of notes) expect(n.midi).toEqual([m])
  })
})

describe('partNotes: drone (ТК46)', () => {
  it('два такта Dm подряд → одна нота d 4, [62, 69]', () => {
    const notes = partNotes([bar(0, 2, 'Dm'), bar(2, 4, 'Dm')], { style: 'drone' })
    expect(notes.length).toBe(1)
    expect(notes[0].t).toBeCloseTo(0, 6)
    expect(notes[0].d).toBeCloseTo(4, 6)
    expect(notes[0].midi).toEqual([62, 69])
  })

  it('смена аккорда — новая нота: Dm Dm C → две', () => {
    const notes = partNotes([bar(0, 2, 'Dm'), bar(2, 4, 'Dm'), bar(4, 6, 'C')], { style: 'drone' })
    expect(notes.length).toBe(2)
    expect(notes[1].t).toBeCloseTo(4, 6)
    expect(notes[1].d).toBeCloseTo(2, 6)
    expect(notes[1].midi).toEqual([60, 67])
  })
})

describe('partNotes: октава, секции, нераспознанное (ТК46)', () => {
  it('octave +1 → все ноты на 12 выше', () => {
    for (const style of ['pad', 'arp', 'pulse', 'drone']) {
      const bars = [bar(0, 2, 'Dm'), bar(2, 4, 'Bb')]
      const base = partNotes(bars, { style })
      const up = partNotes(bars, { style, octave: 1 })
      expect(up.length, style).toBe(base.length)
      up.forEach((n, i) => expect(n.midi, style).toEqual(base[i].midi.map((m) => m + 12)))
    }
  })

  it('octave −2: pulse A → [21]', () => {
    const notes = partNotes([bar(0, 2, 'A')], { style: 'pulse', octave: -2 })
    for (const n of notes) expect(n.midi).toEqual([21])
  })

  it('sections: такты вне выбранных секций — без нот', () => {
    const bars = [bar(0, 2, 'Dm', 'verse'), bar(2, 4, 'C', 'chorus'), bar(4, 6, 'Bb', 'verse')]
    const notes = partNotes(bars, { style: 'pad', sections: ['chorus'] })
    expect(notes.length).toBe(1)
    expect(notes[0].t).toBeCloseTo(2, 6)
    expect(notes[0].midi).toEqual([60, 64, 67])
  })

  it('sections null (по умолчанию) — все такты', () => {
    const bars = [bar(0, 2, 'Dm', 'verse'), bar(2, 4, 'C', 'chorus')]
    expect(partNotes(bars, { style: 'pad' }).length).toBe(2)
    expect(partNotes(bars, { style: 'pad', sections: null }).length).toBe(2)
  })

  it('drone не тянется через невыбранную секцию', () => {
    const bars = [bar(0, 2, 'Dm', 'verse'), bar(2, 4, 'Dm', 'chorus')]
    const notes = partNotes(bars, { style: 'drone', sections: ['verse'] })
    expect(notes.length).toBe(1)
    expect(notes[0].d).toBeCloseTo(2, 6)
  })

  it('«Xyz» и такт без аккорда → без нот', () => {
    for (const style of ['pad', 'arp', 'pulse', 'drone']) {
      expect(partNotes([bar(0, 2, 'Xyz')], { style }), style).toEqual([])
      expect(partNotes([bar(0, 2, null)], { style }), style).toEqual([])
    }
    const notes = partNotes([bar(0, 2, 'Xyz'), bar(2, 4, 'C')], { style: 'pad' })
    expect(notes.length).toBe(1)
    expect(notes[0].t).toBeCloseTo(2, 6)
  })

  it('пустая сетка → []', () => {
    expect(partNotes([], { style: 'pad' })).toEqual([])
  })
})

// кросс-ревью s4: квинта бурдона — в том же регистре C4…B4 (Bb: F4 и Bb4, F#m: C#4 и F#4)
describe('partNotes: drone — квинта в регистре', () => {
  const bar = (s, e, chord) => ({ start: s, end: e, chord, section: 'intro' })
  it('Bb → [65, 70], F#m → [61, 66]', () => {
    expect(partNotes([bar(0, 2, 'Bb')], { style: 'drone' })[0].midi).toEqual([65, 70])
    expect(partNotes([bar(0, 2, 'F#m')], { style: 'drone' })[0].midi).toEqual([61, 66])
  })
})
