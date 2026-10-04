// «Характер исполнения» трека: смелость (temperature) и точность (cfg) —
// по спецификации задачи: умолчание не отправляется, значения зажимаются и
// округляются до шага, подпись карточки показывает только сохранённое.
import { describe, it, expect } from 'vitest'
import { CHARACTER, characterPayload, characterLabel } from './character.js'

describe('CHARACTER — диапазоны ползунков', () => {
  it('temperature и cfg: min/max/step/def', () => {
    expect(CHARACTER).toEqual({
      temperature: { min: 0.7, max: 1.4, step: 0.05, def: 1.0 },
      cfg: { min: 1.0, max: 3.5, step: 0.1, def: 1.5 },
    })
  })
})

describe('characterPayload — что отправить воркеру', () => {
  it('оба по умолчанию → пустой объект', () => {
    expect(characterPayload(1.0, 1.5)).toEqual({})
  })
  it('только смелость', () => {
    expect(characterPayload(1.15, 1.5)).toEqual({ temperature: 1.15 })
  })
  it('только точность', () => {
    expect(characterPayload(1.0, 2.5)).toEqual({ cfg: 2.5 })
  })
  it('оба изменены → оба ключа', () => {
    expect(characterPayload(1.15, 2.5)).toEqual({ temperature: 1.15, cfg: 2.5 })
  })
  it('выше максимума зажимается к max', () => {
    expect(characterPayload(3, 9)).toEqual({ temperature: 1.4, cfg: 3.5 })
  })
  it('ниже минимума зажимается к min', () => {
    expect(characterPayload(0.1, 0.2)).toEqual({ temperature: 0.7, cfg: 1.0 })
  })
  it('нечисловые и пустые → как по умолчанию, не отправляются', () => {
    for (const v of [undefined, null, '', 'abc', NaN]) {
      expect(characterPayload(v, v)).toEqual({})
    }
    expect(characterPayload('', 2.5)).toEqual({ cfg: 2.5 })
    expect(characterPayload(1.15, undefined)).toEqual({ temperature: 1.15 })
  })
  it('округление до шага без хвостов float', () => {
    // 0.7 + 9 × 0.05 в float даёт 1.1500000000000001
    const t = 0.7 + 9 * 0.05
    expect(characterPayload(t, 1.5)).toEqual({ temperature: 1.15 })
    expect(characterPayload(1.0, 1.0 + 0.1 * 12)).toEqual({ cfg: 2.2 })
    expect(characterPayload(1.13, 2.46)).toEqual({ temperature: 1.15, cfg: 2.5 })
  })
  it('значение, после округления равное умолчанию, не отправляется', () => {
    expect(characterPayload(1.01, 1.52)).toEqual({})
  })
})

describe('characterLabel — подпись карточки трека', () => {
  it('нет значений или 0 → пустая строка', () => {
    expect(characterLabel({})).toBe('')
    expect(characterLabel({ temperature: 0, cfg: 0 })).toBe('')
  })
  it('только смелость', () => {
    expect(characterLabel({ temperature: 1.15, cfg: 0 })).toBe('смелость 1.15')
  })
  it('только точность', () => {
    expect(characterLabel({ temperature: 0, cfg: 2.5 })).toBe('точность 2.5')
  })
  it('обе', () => {
    expect(characterLabel({ temperature: 1.15, cfg: 2.5 })).toBe('смелость 1.15 · точность 2.5')
  })
  it('сохранённое умолчание показывается, число без лишних нулей', () => {
    expect(characterLabel({ temperature: 1.0, cfg: 1.5 })).toBe('смелость 1 · точность 1.5')
    expect(characterLabel({ temperature: 1, cfg: 3 })).toBe('смелость 1 · точность 3')
  })
})
