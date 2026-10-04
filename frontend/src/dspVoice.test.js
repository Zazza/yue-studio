import { describe, it, expect } from 'vitest'
import { chainDefaults, voiceTarget, hasGrid, needsStem } from './dspVoice.js'

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

// Карточка internal-dsp-space 10.4: «найти сетку» — у всех цепочек с параметром bpm;
// цепочка с key (ducking, ключ — барабаны) сама выбирает «прочее», весь трек ей недоступен.
const gate = { id: 'gate', name: 'Гейт', params: [{ id: 'bpm', default: 120 }, { id: 'div', default: 2 }] }
const delay = { id: 'delay', name: 'Дилей в темп', params: [{ id: 'bpm', default: 120 }, { id: 'wet', default: 0.5 }] }
const ducking = {
  id: 'ducking', name: 'Ducking от барабанов', key: 'drums',
  params: [{ id: 'threshold', default: 0.08 }, { id: 'from', default: 0 }],
}

describe('hasGrid — кнопка «найти сетку»', () => {
  it('у любой цепочки с параметром bpm, не только у gate', () => {
    expect(hasGrid(gate)).toBe(true)
    expect(hasGrid(delay)).toBe(true)
  })
  it('без bpm — нет; без цепочки и без params — нет', () => {
    expect(hasGrid(tape)).toBe(false)
    expect(hasGrid(ducking)).toBe(false)
    expect(hasGrid(null)).toBe(false)
    expect(hasGrid({ id: 'x' })).toBe(false)
  })
})

describe('needsStem — цепочке нужна дорожка (весь трек недоступен)', () => {
  it('цепочка с key — да', () => {
    expect(needsStem(ducking)).toBe(true)
  })
  it('обычная и голосовая — нет (весь трек остаётся возможным); без цепочки — нет', () => {
    expect(needsStem(tape)).toBe(false)
    expect(needsStem(megaphone)).toBe(false)
    expect(needsStem(null)).toBe(false)
    expect(needsStem({ id: 'x', key: '' })).toBe(false)
  })
})

describe('voiceTarget — цепочка с key', () => {
  it('без выбранной дорожки — сама на «прочее»', () => {
    expect(voiceTarget(ducking, '')).toBe('other')
  })
  it('осознанный выбор дорожки не перекрывает', () => {
    expect(voiceTarget(ducking, 'bass')).toBe('bass')
  })
  it('голосовая — по-прежнему «голос»', () => {
    expect(voiceTarget(megaphone, '')).toBe('vocals')
  })
})
