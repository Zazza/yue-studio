// Тесты окна превью DSP (карточка internal-guitar-pedals, условие 1.5):
// выделение → оно (обрезка по длине трека); нет выделения, есть курсор →
// [курсор, курсор+15] (у конца — последние 15 с); ничего — [20, 35]
// (короткий трек — весь; окно не длиннее трека); всегда from < to.
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { previewWindow } from './fxPreview.js'

const win = (sel, cursor, dur) => {
  const w = previewWindow(sel, cursor, dur)
  return { from: w.from, to: w.to }
}

describe('previewWindow: выделение', () => {
  it('выделение внутри трека → ровно оно', () => {
    expect(win({ from: 10, to: 22.5 }, 40, 180)).toEqual({ from: 10, to: 22.5 })
  })

  it('выделение важнее курсора', () => {
    expect(win({ from: 60, to: 70 }, 5, 180)).toEqual({ from: 60, to: 70 })
  })

  it('выделение за концом трека обрезается по длине', () => {
    expect(win({ from: 170, to: 200 }, null, 180)).toEqual({ from: 170, to: 180 })
  })

  it('выделение до нуля обрезается к 0', () => {
    expect(win({ from: -3, to: 4 }, null, 180)).toEqual({ from: 0, to: 4 })
  })

  it('выделение нулевой длины — всё равно from < to', () => {
    const w = win({ from: 10, to: 10 }, null, 180)
    expect(w.from).toBeLessThan(w.to)
  })
})

describe('previewWindow: курсор без выделения', () => {
  it('курсор → [курсор, курсор+15]', () => {
    expect(win(null, 30, 180)).toEqual({ from: 30, to: 45 })
  })

  it('курсор в 0 — это курсор (не «нет курсора») → [0, 15]', () => {
    expect(win(null, 0, 180)).toEqual({ from: 0, to: 15 })
  })

  it('курсор у конца → последние 15 с', () => {
    expect(win(null, 175, 180)).toEqual({ from: 165, to: 180 })
  })

  it('курсор за концом трека → последние 15 с', () => {
    expect(win(null, 400, 180)).toEqual({ from: 165, to: 180 })
  })

  it('трек короче 15 с → весь трек', () => {
    expect(win(null, 5, 10)).toEqual({ from: 0, to: 10 })
  })
})

describe('previewWindow: ни выделения, ни курсора', () => {
  it('→ [20, 35]', () => {
    expect(win(null, null, 180)).toEqual({ from: 20, to: 35 })
  })

  it('короткий трек → весь', () => {
    expect(win(null, null, 12)).toEqual({ from: 0, to: 12 })
  })

  it('трек короче 1 с → весь трек (окно не длиннее трека)', () => {
    expect(win(null, null, 0.4)).toEqual({ from: 0, to: 0.4 })
  })
})

describe('previewWindow: всегда from < to', () => {
  const sels = [null, { from: 0, to: 0 }, { from: 5, to: 5 }, { from: 300, to: 400 }, { from: 2, to: 9 }]
  const cursors = [null, 0, 7, 179.9, 180, 500]
  const durs = [0, 0.5, 3, 14, 30, 180]
  for (const sel of sels) {
    for (const cur of cursors) {
      for (const dur of durs) {
        it(`sel=${JSON.stringify(sel)} cursor=${cur} dur=${dur}`, () => {
          const w = win(sel, cur, dur)
          expect(Number.isFinite(w.from) && Number.isFinite(w.to)).toBe(true)
          expect(w.from).toBeLessThan(w.to)
          expect(w.from).toBeGreaterThanOrEqual(0)
        })
      }
    }
  }
})
