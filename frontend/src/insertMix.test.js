// Тесты громкости вклейки: ползунок в дБ относительно оригинала.
import { describe, it, expect } from 'vitest'
import { clampDb, INSERT_DEFAULT_DB, INSERT_MIN_DB, INSERT_MAX_DB } from './insertMix.js'

describe('громкость вклейки (clampDb)', () => {
  it('константы диапазона', () => {
    expect(INSERT_DEFAULT_DB).toBe(0)
    expect(INSERT_MIN_DB).toBe(-24)
    expect(INSERT_MAX_DB).toBe(12)
  })

  it('ниже минимума — зажимается к -24', () => {
    expect(clampDb(-30)).toBe(-24)
  })

  it('выше максимума — зажимается к 12', () => {
    expect(clampDb(20)).toBe(12)
  })

  it('внутри диапазона — без изменений, границы включены', () => {
    expect(clampDb(-3)).toBe(-3)
    expect(clampDb(-24)).toBe(-24)
    expect(clampDb(12)).toBe(12)
    expect(clampDb(0)).toBe(0)
  })

  it('не число — значение по умолчанию', () => {
    expect(clampDb(undefined)).toBe(0)
    expect(clampDb('x')).toBe(0)
    expect(clampDb(NaN)).toBe(0)
  })
})
