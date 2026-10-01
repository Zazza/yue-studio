import { describe, it, expect } from 'vitest'
import { ONE_CLICK_LEVELS, ONE_CLICK_CHAINS, oneClickParams } from './oneClick.js'

// Эффекты «одним кликом» с уровнем: легко / средне / сильно.
// Диапазоны и дефолты крутилок — из internal/dsp/dsp.go (masterParams, breatheParams).

const RANGES = {
  master: { drive: [1, 3], grit: [0, 3], breath: [0, 1.5] },
  breathe: { amount: [0, 1.5] },
}
const chainId = c => (typeof c === 'string' ? c : c && c.id)

describe('ONE_CLICK_LEVELS / ONE_CLICK_CHAINS', () => {
  it('уровни: легко, средне, сильно — в этом порядке', () => {
    expect(ONE_CLICK_LEVELS).toEqual(['light', 'medium', 'strong'])
  })

  it('цепочки с уровнями содержат master и breathe', () => {
    const ids = ONE_CLICK_CHAINS.map(chainId)
    expect(ids).toContain('master')
    expect(ids).toContain('breathe')
  })
})

describe('oneClickParams: средний уровень — дефолты цепочки', () => {
  it('master medium — ровно дефолты dsp.go (drive 1.5, grit 1.3, breath 1.5)', () => {
    expect(oneClickParams('master', 'medium')).toEqual({ drive: 1.5, grit: 1.3, breath: 1.5 })
  })

  it('breathe medium — amount 1', () => {
    expect(oneClickParams('breathe', 'medium')).toEqual({ amount: 1 })
  })
})

describe('oneClickParams: диапазоны и монотонность', () => {
  for (const chain of Object.keys(RANGES)) {
    it(`${chain}: все уровни в диапазонах крутилок и с полным набором крутилок`, () => {
      for (const level of ONE_CLICK_LEVELS) {
        const p = oneClickParams(chain, level)
        expect(Object.keys(p).sort()).toEqual(Object.keys(RANGES[chain]).sort())
        for (const [knob, [min, max]] of Object.entries(RANGES[chain])) {
          expect(typeof p[knob]).toBe('number')
          expect(p[knob]).toBeGreaterThanOrEqual(min)
          expect(p[knob]).toBeLessThanOrEqual(max)
        }
      }
    })

    it(`${chain}: light ≤ medium ≤ strong по каждой крутилке, хотя бы одна строго растёт`, () => {
      const l = oneClickParams(chain, 'light')
      const m = oneClickParams(chain, 'medium')
      const s = oneClickParams(chain, 'strong')
      let grows = false
      for (const knob of Object.keys(RANGES[chain])) {
        expect(l[knob]).toBeLessThanOrEqual(m[knob])
        expect(m[knob]).toBeLessThanOrEqual(s[knob])
        if (s[knob] > l[knob]) grows = true
      }
      expect(grows).toBe(true)
    })
  }

  it('master light: песок верхов (grit) строго меньше, чем у medium — против писка', () => {
    expect(oneClickParams('master', 'light').grit).toBeLessThan(oneClickParams('master', 'medium').grit)
  })
})

describe('oneClickParams: краевые случаи', () => {
  it('неизвестный уровень и undefined — как medium', () => {
    for (const chain of ['master', 'breathe']) {
      const medium = oneClickParams(chain, 'medium')
      expect(oneClickParams(chain, 'extreme')).toEqual(medium)
      expect(oneClickParams(chain, undefined)).toEqual(medium)
      expect(oneClickParams(chain)).toEqual(medium)
      expect(oneClickParams(chain, '')).toEqual(medium)
    }
  })

  it('неизвестная цепочка — null', () => {
    expect(oneClickParams('no-such-chain', 'medium')).toBeNull()
    expect(oneClickParams(undefined, 'medium')).toBeNull()
  })

  it('каждый вызов — новый объект: мутация результата не влияет на следующий вызов', () => {
    for (const chain of ['master', 'breathe']) {
      for (const level of ONE_CLICK_LEVELS) {
        const first = oneClickParams(chain, level)
        const before = JSON.parse(JSON.stringify(first))
        for (const k of Object.keys(first)) first[k] = 999
        first.extra = 1
        const second = oneClickParams(chain, level)
        expect(second).not.toBe(first)
        expect(second).toEqual(before)
      }
    }
  })
})
