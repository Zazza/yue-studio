// Тесты карточки internal-own-track, этап 10: условие 84 (тест-кейс ТК125 — часть фронта).
//   fxPresets.js: готовая цепочка «Хэт: набор» (drums-hh-kit) — sampler (output_db 0) → eq: срез низа ≥ 800 Гц,
//   полоса около 10 кГц ≥ +6 дБ, около 1 кГц ≤ −3 дБ. Хэты драм-машин (drums-hh-<машина>) не меняются —
//   только sampler.
//   Сторож (ревью этапа 10): хэт драм-машин — прежний удар, как на коммите fdc7a59: sampler floor_db −24,
//   output_db 4 — и в готовых цепочках drums-hh-<машина>, и в «ритм-секции набором» (trackDesk.rhythmSection).
// Толкование: «около» — 10 кГц = 8–12,5 кГц, 1 кГц = 0,8–1,25 кГц.
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { fxPresets } from './fxPresets.js'
import { rhythmSection } from './trackDesk.js'

const MACHINES = ['tr808', 'tr909', 'linn', 'cr78', 'simmons']
const byId = (id) => fxPresets.find((p) => p.id === id)

function expectBrightEq(eq) {
  expect(eq.type).toBe('eq')
  expect(Number(eq.highpass_hz || 0)).toBeGreaterThanOrEqual(800)
  const bands = eq.bands || []
  const near = (lo, hi) => bands.filter((b) => b.freq_hz >= lo && b.freq_hz <= hi)
  expect(near(8000, 12500).some((b) => b.gain_db >= 6), `нет полосы ~10 кГц ≥ +6: ${JSON.stringify(bands)}`).toBe(true)
  expect(near(800, 1250).some((b) => b.gain_db <= -3), `нет полосы ~1 кГц ≤ −3: ${JSON.stringify(bands)}`).toBe(true)
}

describe('ТК125: хэт набора ярче (drums-hh-kit)', () => {
  it('цепочка sampler (output_db 0) → eq', () => {
    const p = byId('drums-hh-kit')
    expect(p).toBeTruthy()
    expect(p.chain.map((b) => b.type).slice(0, 2)).toEqual(['sampler', 'eq'])
    expect(Number(p.chain[0].output_db || 0)).toBe(0)
  })

  it('eq: срез низа ≥ 800, ~10 кГц ≥ +6, ~1 кГц ≤ −3', () => {
    const eq = byId('drums-hh-kit').chain.find((b) => b.type === 'eq')
    expect(eq, 'в drums-hh-kit нет eq').toBeTruthy()
    expectBrightEq(eq)
  })

  it('сэмплы хэта — прежние (закрытый/полузакрытый osdk, глушение)', () => {
    const s = byId('drums-hh-kit').chain[0]
    expect(s).toMatchObject({ type: 'sampler', kit: 'osdk/hh-closed', kit_open: 'osdk/hh-half', choke: 1 })
  })

  it.each(MACHINES)('драм-машина %s: хэт — только sampler, без eq', (m) => {
    const p = byId(`drums-hh-${m}`)
    expect(p, `нет drums-hh-${m}`).toBeTruthy()
    expect(p.chain.map((b) => b.type)).toEqual(['sampler'])
  })
})

// значения хэта машин до этапа 10 (fdc7a59): floor_db −24, output_db 4, закрытый/открытый набора, глушение
const OLD_MACHINE_HH = (m) => ({ type: 'sampler', floor_db: -24, output_db: 4,
  kit: `${m}/hh-closed`, kit_open: `${m}/hh-open`, choke: 1 })

describe('ТК125 (сторож): хэт драм-машин не меняется', () => {
  it.each(MACHINES)('drums-hh-%s: sampler как до этапа 10 (output_db 4)', (m) => {
    const p = byId(`drums-hh-${m}`)
    expect(p, `нет drums-hh-${m}`).toBeTruthy()
    expect(p.chain).toEqual([OLD_MACHINE_HH(m)])
  })

  it.each(MACHINES)('ритм-секция набором %s: хэт — sampler машины с output_db 4, без eq', (m) => {
    const stems = ['kick', 'snare', 'toms', 'hh', 'ride', 'crash', 'bass']
    const hh = rhythmSection(stems, fxPresets, false, 'ru', m).find((r) => r.stem === 'hh')
    expect(hh, `нет хэта в ритм-секции ${m}`).toBeTruthy()
    expect(hh.chain).toEqual([OLD_MACHINE_HH(m)])
  })
})
