import { describe, it, expect } from 'vitest'
import { applyFoundTones } from './dspTones.js'

describe('applyFoundTones — найденные тона в крутилки «Убрать свист»', () => {
  const tones = [{ hz: 5263.4, prominence_db: 21 }, { hz: 3526.1, prominence_db: 15 }]
  it('самый заметный — freq, следующий — freq2, третьего нет — 0; шаг 5 Гц', () => {
    expect(applyFoundTones({ depth: 30 }, tones, null)).toEqual({ depth: 30, freq: 5265, freq2: 3525, freq3: 0 })
  })
  it('выделение задаёт окно с шагом 0.5 с наружу', () => {
    const p = applyFoundTones({}, tones.slice(0, 1), { from: 195.3, to: 212.2 })
    expect([p.start, p.end]).toEqual([195, 212.5])
  })
  it('без выделения окно не трогается', () => {
    expect(applyFoundTones({ start: 10, end: 20 }, tones, null)).toMatchObject({ start: 10, end: 20 })
  })
})
