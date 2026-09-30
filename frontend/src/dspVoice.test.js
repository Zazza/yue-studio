import { describe, it, expect } from 'vitest'
import { chainDefaults, voiceTarget } from './dspVoice.js'

// Голосовые цепочки приходят из Go (dsp.All) с флагом voice и крутилкой mix.
// Фикстура — как отдаёт биндинг YueDspChains.
const megaphone = {
  id: 'megaphone', name: 'Мегафон', voice: true,
  params: [
    { id: 'lo', min: 250, max: 800, step: 10, default: 400 },
    { id: 'drive', min: 1, max: 5, step: 0.1, default: 2.5 },
    { id: 'from', min: 0, max: 600, step: 0.5, default: 0 },
    { id: 'mix', min: 0, max: 1, step: 0.05, default: 1 },
  ],
}
const tape = { id: 'tape', name: 'Кассета', voice: false, params: [{ id: 'wow', default: 0.1 }] }

describe('voiceTarget — дорожка эффекта при выборе цепочки', () => {
  it('голосовая цепочка без выбранной дорожки — сама на «голос»', () => {
    expect(voiceTarget(megaphone, '')).toBe('vocals')
  })
  it('осознанный выбор дорожки не перекрывает', () => {
    expect(voiceTarget(megaphone, 'drums')).toBe('drums')
    expect(voiceTarget(megaphone, 'vocals')).toBe('vocals')
  })
  it('обычная цепочка дорожку не меняет', () => {
    expect(voiceTarget(tape, '')).toBe('')
    expect(voiceTarget(tape, 'other')).toBe('other')
  })
  it('без цепочки — как было', () => {
    expect(voiceTarget(null, '')).toBe('')
  })
})

describe('chainDefaults — крутилки цепочки по умолчанию', () => {
  it('у голосовой цепочки обязательна доля эффекта mix = 1 (только эффект)', () => {
    const p = chainDefaults(megaphone)
    expect(p.mix).toBe(1)
    expect(p.lo).toBe(400)
    expect(p.drive).toBe(2.5)
  })
  it('полный набор крутилок; без цепочки — пусто', () => {
    expect(Object.keys(chainDefaults(megaphone))).toHaveLength(megaphone.params.length)
    expect(chainDefaults(null)).toEqual({})
  })
})
