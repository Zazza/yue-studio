// Тесты карточки internal-own-track, этап 5 «Перкуссия по сетке», условие 32 (тест-кейсы ТК58, ТК59):
// percPart.js — удары перкуссионной партии по сетке тактов трека.
// Контракт: percHits(bars, {pattern, sections, swing, accent}) → [{t, d, vel}] в секундах трека;
// такт {start, end, section} (как bars у chord_grid) = 4 равные доли. Рисунки: fours — каждая доля;
// eighths — восьмые; sixteenths — шестнадцатые; backbeat — доли 2 и 4; offbeat — восьмые между долями.
// Сила при accent 1: доля 0,9; восьмая между долями 0,6; шестнадцатая между восьмыми 0,4; accent 0 —
// все 0,9; между — линейно. swing 0…0,5: каждая вторая клетка рисунка (восьмые — между долями,
// шестнадцатые — 2-я и 4-я в доле; offbeat — все) сдвигается позже на swing × длина клетки.
// d — длина клетки. Такты вне sections — без ударов; sections пусто/не задано — все такты.
// barsFromBeat({bpm, offset}, dur) → такты по 4 доли от offset до конца трека (последний может быть
// неполным), section ''.
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { percHits, barsFromBeat } from './percPart.js'

const bar = (start, end, section = 'verse') => ({ start, end, section })
const BAR = [bar(0, 2)]
// accent 1 и swing 0 заданы явно: ТК58 описывает силу «при accent 1» (умолчание карточка не называет)
const hits = (pattern, opts = {}) => percHits(BAR, { pattern, accent: 1, swing: 0, ...opts })
const ts = (h) => h.map((x) => x.t)
const vels = (h) => h.map((x) => x.vel)
const expectClose = (got, want) => {
  expect(got.length).toBe(want.length)
  got.forEach((g, i) => expect(g, `#${i}`).toBeCloseTo(want[i], 6))
}

describe('percHits: рисунки (ТК58)', () => {
  it('fours → t [0, .5, 1, 1.5], d .5, сила доли .9', () => {
    const h = hits('fours')
    expectClose(ts(h), [0, 0.5, 1, 1.5])
    for (const x of h) expect(x.d).toBeCloseTo(0.5, 6)
    expectClose(vels(h), [0.9, 0.9, 0.9, 0.9])
  })

  it('eighths → 8 ударов по .25, vel [.9, .6, …]', () => {
    const h = hits('eighths')
    expectClose(ts(h), [0, 0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75])
    for (const x of h) expect(x.d).toBeCloseTo(0.25, 6)
    expectClose(vels(h), [0.9, 0.6, 0.9, 0.6, 0.9, 0.6, 0.9, 0.6])
  })

  it('sixteenths → 16 ударов по .125, vel [.9, .4, .6, .4, …]', () => {
    const h = hits('sixteenths')
    expectClose(ts(h), Array.from({ length: 16 }, (_, i) => i * 0.125))
    for (const x of h) expect(x.d).toBeCloseTo(0.125, 6)
    expectClose(vels(h), Array(4).fill([0.9, 0.4, 0.6, 0.4]).flat())
  })

  it('backbeat → доли 2 и 4: t [.5, 1.5], сила доли', () => {
    const h = hits('backbeat')
    expectClose(ts(h), [0.5, 1.5])
    expectClose(vels(h), [0.9, 0.9])
  })

  it('offbeat → восьмые между долями: t [.25, .75, 1.25, 1.75], сила .6', () => {
    const h = hits('offbeat')
    expectClose(ts(h), [0.25, 0.75, 1.25, 1.75])
    expectClose(vels(h), [0.6, 0.6, 0.6, 0.6])
  })

  it('t — секунды трека: такт 4–6 с сдвигает удары на 4', () => {
    expectClose(ts(percHits([bar(4, 6)], { pattern: 'fours', accent: 1, swing: 0 })), [4, 4.5, 5, 5.5])
  })

  it('два такта подряд → удары обоих тактов по возрастанию t', () => {
    const h = percHits([bar(0, 2), bar(2, 4)], { pattern: 'fours', accent: 1, swing: 0 })
    expectClose(ts(h), [0, 0.5, 1, 1.5, 2, 2.5, 3, 3.5])
  })

  it('пустая сетка → без ударов', () => {
    expect(percHits([], { pattern: 'fours', accent: 1, swing: 0 })).toEqual([])
  })
})

describe('percHits: акцент (ТК58)', () => {
  it('accent 0 → все удары .9', () => {
    for (const p of ['eighths', 'sixteenths', 'offbeat']) {
      for (const v of vels(hits(p, { accent: 0 }))) expect(v, p).toBeCloseTo(0.9, 6)
    }
  })

  it('accent .5 → линейно между: восьмая .75, шестнадцатая .65', () => {
    expectClose(vels(hits('sixteenths', { accent: 0.5 })).slice(0, 4), [0.9, 0.65, 0.75, 0.65])
  })
})

describe('percHits: свинг (ТК58)', () => {
  it('eighths swing .5 → вторые восьмые на .375 (0,25 + 0,5·0,25), первые на месте', () => {
    expectClose(ts(hits('eighths', { swing: 0.5 })), [0, 0.375, 0.5, 0.875, 1, 1.375, 1.5, 1.875])
  })

  it('sixteenths swing .5 → 2-я и 4-я шестнадцатые позже на .0625', () => {
    const t = ts(hits('sixteenths', { swing: 0.5 }))
    expectClose(t.slice(0, 4), [0, 0.1875, 0.25, 0.4375])
  })

  it('offbeat swing .5 → сдвинуты все: .375, .875, …', () => {
    expectClose(ts(hits('offbeat', { swing: 0.5 })), [0.375, 0.875, 1.375, 1.875])
  })
})

describe('percHits: секции (ТК58)', () => {
  const two = [bar(0, 2, 'verse'), bar(2, 4, 'chorus')]

  it('секция не выбрана → её такты без ударов', () => {
    const h = percHits(two, { pattern: 'fours', sections: ['chorus'], accent: 1, swing: 0 })
    expectClose(ts(h), [2, 2.5, 3, 3.5])
  })

  it('sections [] → все такты', () => {
    expect(percHits(two, { pattern: 'fours', sections: [], accent: 1, swing: 0 }).length).toBe(8)
  })

  it('sections не задано → все такты', () => {
    expect(percHits(two, { pattern: 'fours', accent: 1, swing: 0 }).length).toBe(8)
  })
})

describe('barsFromBeat (ТК59)', () => {
  it('bpm 120, offset .3, dur 10 → такты по 2 с от .3, последний неполный до 10, section ""', () => {
    const bars = barsFromBeat({ bpm: 120, offset: 0.3 }, 10)
    const want = [[0.3, 2.3], [2.3, 4.3], [4.3, 6.3], [6.3, 8.3], [8.3, 10]]
    expect(bars.length).toBe(want.length)
    bars.forEach((b, i) => {
      expect(b.start, `start #${i}`).toBeCloseTo(want[i][0], 6)
      expect(b.end, `end #${i}`).toBeCloseTo(want[i][1], 6)
      expect(b.section).toBe('')
    })
  })

  it('до offset неполный такт не берётся: первый такт начинается в offset', () => {
    const bars = barsFromBeat({ bpm: 120, offset: 1.7 }, 6)
    expect(bars[0].start).toBeCloseTo(1.7, 6)
    for (const b of bars) expect(b.start).toBeGreaterThanOrEqual(1.7 - 1e-9)
  })

  it('край: трек кратен такту → без пустого такта нулевой длины', () => {
    const bars = barsFromBeat({ bpm: 120, offset: 0 }, 4)
    expect(bars.length).toBe(2)
    for (const b of bars) expect(b.end - b.start).toBeGreaterThan(0)
  })

  it('сетка из barsFromBeat годится для percHits', () => {
    const h = percHits(barsFromBeat({ bpm: 120, offset: 0.3 }, 4.3), { pattern: 'fours', accent: 1, swing: 0 })
    expectClose(ts(h), [0.3, 0.8, 1.3, 1.8, 2.3, 2.8, 3.3, 3.8])
  })
})

// Условие 38 (ТК62): barsFromBeat({bpm, offset}, dur, shift) — shift 0…3 — с какой доли (от offset)
// начинается такт: первый такт с offset + shift·(60/bpm); вне 0…3 — зажим, не целое — округление.
describe('barsFromBeat: такт с доли shift (ТК62)', () => {
  const GRID = { bpm: 120, offset: 0.3 }
  const expectBars = (bars, want) => {
    expect(bars.length).toBe(want.length)
    bars.forEach((b, i) => {
      expect(b.start, `start #${i}`).toBeCloseTo(want[i][0], 6)
      expect(b.end, `end #${i}`).toBeCloseTo(want[i][1], 6)
      expect(b.section).toBe('')
    })
  }

  it('shift 0 → как ТК59 (такты от offset .3)', () => {
    expectBars(barsFromBeat(GRID, 10, 0), [[0.3, 2.3], [2.3, 4.3], [4.3, 6.3], [6.3, 8.3], [8.3, 10]])
  })

  it('shift 0 совпадает с вызовом без shift', () => {
    expect(barsFromBeat(GRID, 10, 0)).toEqual(barsFromBeat(GRID, 10))
  })

  it('shift 1 → первый такт [0,8; 2,8), дальше по 2 с до конца трека', () => {
    expectBars(barsFromBeat(GRID, 10, 1), [[0.8, 2.8], [2.8, 4.8], [4.8, 6.8], [6.8, 8.8], [8.8, 10]])
  })

  it('shift 3 → первый такт [1,8; 3,8)', () => {
    const bars = barsFromBeat(GRID, 10, 3)
    expect(bars[0].start).toBeCloseTo(1.8, 6)
    expect(bars[0].end).toBeCloseTo(3.8, 6)
    expect(bars[bars.length - 1].end).toBeCloseTo(10, 6)
  })

  it('shift 5 → зажим до 3 (как shift 3)', () => {
    expect(barsFromBeat(GRID, 10, 5)).toEqual(barsFromBeat(GRID, 10, 3))
  })

  it('shift −1 → зажим до 0 (как shift 0)', () => {
    expect(barsFromBeat(GRID, 10, -1)).toEqual(barsFromBeat(GRID, 10, 0))
  })

  it('не целое → округление: 1,4 как 1; 2,6 как 3', () => {
    expect(barsFromBeat(GRID, 10, 1.4)).toEqual(barsFromBeat(GRID, 10, 1))
    expect(barsFromBeat(GRID, 10, 2.6)).toEqual(barsFromBeat(GRID, 10, 3))
  })

  it('backbeat по тактам shift 1 → первые удары 1,3 и 2,3', () => {
    const h = percHits(barsFromBeat(GRID, 10, 1), { pattern: 'backbeat', accent: 1, swing: 0 })
    expect(h.length).toBeGreaterThanOrEqual(2)
    expect(h[0].t).toBeCloseTo(1.3, 6)
    expect(h[1].t).toBeCloseTo(2.3, 6)
  })
})
