// Тесты партии голоса: источник голоса у рендера, спека «перепеть», тихий конец окна.
import { describe, it, expect } from 'vitest'
import { voiceSource, revoiceSpec, vocalEndsQuiet } from './vocalParts.js'

const byId = (...jobs) => Object.fromEntries(jobs.map((j) => [j.id, j]))

describe('voiceSource — рендер-источник голоса', () => {
  it('voice_src задан → он, даже у варианта', () => {
    const j = { id: 'v1', role: 'variant', parent_id: 'g1', voice_src: 'src9' }
    expect(voiceSource(j, byId(j, { id: 'g1', role: '' }))).toBe('src9')
    const g = { id: 'g2', role: 'continue', voice_src: 'src7' }
    expect(voiceSource(g, byId(g))).toBe('src7')
  })

  it('сгенерированный трек (role не variant) → свой id', () => {
    for (const role of ['', 'continue', 'section', 'rebuild', 'fragment']) {
      const j = { id: 'g-' + role, role }
      expect(voiceSource(j, byId(j))).toBe('g-' + role)
    }
  })

  it('variant без voice_src → источник родителя', () => {
    const g = { id: 'g1', role: '' }
    const v = { id: 'v1', role: 'variant', parent_id: 'g1' }
    expect(voiceSource(v, byId(g, v))).toBe('g1')
  })

  it('цепочка вариантов идёт до сгенерированного предка', () => {
    const g = { id: 'g1', role: 'section' }
    const v1 = { id: 'v1', role: 'variant', parent_id: 'g1' }
    const v2 = { id: 'v2', role: 'variant', parent_id: 'v1' }
    const v3 = { id: 'v3', role: 'variant', parent_id: 'v2' }
    expect(voiceSource(v3, byId(g, v1, v2, v3))).toBe('g1')
  })

  it('в цепочке вариант с voice_src — берётся его voice_src', () => {
    const g = { id: 'g1', role: '' }
    const v1 = { id: 'v1', role: 'variant', parent_id: 'g1', voice_src: 'src5' }
    const v2 = { id: 'v2', role: 'variant', parent_id: 'v1' }
    expect(voiceSource(v2, byId(g, v1, v2))).toBe('src5')
  })

  it('родитель не найден → null', () => {
    const v = { id: 'v1', role: 'variant', parent_id: 'нет' }
    expect(voiceSource(v, byId(v))).toBeNull()
    const orphan = { id: 'v2', role: 'variant' }
    expect(voiceSource(orphan, byId(orphan))).toBeNull()
  })

  it('цикл parent_id → null без зависания', () => {
    const a = { id: 'a', role: 'variant', parent_id: 'b' }
    const b = { id: 'b', role: 'variant', parent_id: 'a' }
    expect(voiceSource(a, byId(a, b))).toBeNull()
    const self = { id: 's', role: 'variant', parent_id: 's' }
    expect(voiceSource(self, byId(self))).toBeNull()
  })
})

describe('revoiceSpec — SectionSpec для «перепеть»', () => {
  it('lead = from, стем только vocals, флаг revoice', () => {
    expect(revoiceSpec('c1', 12.5, 20, 0.5)).toEqual({
      child_id: 'c1', from: 12.5, to: 20, lead: 12.5, beat_sec: 0.5, stems: ['vocals'], revoice: true,
    })
  })

  it('from = 0 → lead 0', () => {
    expect(revoiceSpec('c2', 0, 8, 0.6)).toEqual({
      child_id: 'c2', from: 0, to: 8, lead: 0, beat_sec: 0.6, stems: ['vocals'], revoice: true,
    })
  })
})

describe('vocalEndsQuiet — голос молчит на конце окна', () => {
  const bar = (start, end, vocal) => (vocal > 0
    ? { section: 'verse', voices: { Vocal: vocal, Ins: 4 }, rests: {}, start_sec: start, end_sec: end }
    : { section: 'verse', voices: vocal === 0 ? { Vocal: 0, Ins: 4 } : { Ins: 4 }, rests: { Vocal: 16 }, start_sec: start, end_sec: end })
  // 0–2 поёт, 2–4 поёт, 4–6 молчит (Vocal: 0), 6–8 поёт, 8–10 поёт, 10–12 молчит (нет ключа Vocal)
  const bars = [bar(0, 2, 4), bar(2, 4, 3), bar(4, 6, 0), bar(6, 8, 2), bar(8, 10, 2), bar(10, 12, undefined)]

  it('граница такта, после которой голос молчит → true', () => {
    expect(vocalEndsQuiet(bars, 4)).toBe(true)
    expect(vocalEndsQuiet(bars, 10)).toBe(true)
  })

  it('to внутри такта, где голос молчит (Vocal: 0 или нет нот) → true', () => {
    expect(vocalEndsQuiet(bars, 5)).toBe(true)
    expect(vocalEndsQuiet(bars, 11)).toBe(true)
  })

  it('to ≥ конца плана → true', () => {
    expect(vocalEndsQuiet(bars, 12)).toBe(true)
    expect(vocalEndsQuiet(bars, 30)).toBe(true)
    const allSing = [bar(0, 2, 4), bar(2, 4, 4)]
    expect(vocalEndsQuiet(allSing, 4)).toBe(true)
  })

  it('to внутри поющего такта, и следующий поёт → false', () => {
    expect(vocalEndsQuiet(bars, 1)).toBe(false)
    expect(vocalEndsQuiet(bars, 7)).toBe(false)
  })

  it('граница такта, после которой голос поёт → false', () => {
    expect(vocalEndsQuiet(bars, 2)).toBe(false)
    expect(vocalEndsQuiet(bars, 8)).toBe(false)
  })
})
