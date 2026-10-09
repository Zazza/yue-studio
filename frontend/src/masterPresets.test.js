// Тесты карточки internal-own-track, этап 6, условие 46 (тест-кейс ТК76): готовые мастера и
// место по умолчанию у готовых партий в fxPresets.js.
//   мастера — stems ['master']: «Стриминг −14 LUFS», «Громко −11 LUFS» (последний блок limiter,
//   target_lufs −14 / −11), «Мягкая склейка» — без громкости (без limiter);
//   перкуссии хэт/шейкер/бубен/ковбелл/райд — place.pan от центра (0,2 ≤ |pan| ≤ 0,4);
//   бочка/хлопки/римшот — центр; пэды — place.width 1,5.
// Название мастера ищется по вхождению (без учёта регистра): приставка вроде «Мастер: » допустима.
// Цепочки мастеров проверяются по описанию блоков (fxBlocks.json — копия worker/fx_blocks.json).
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import blocks from './fxBlocks.json'
import { fxPresets } from './fxPresets.js'

const masters = fxPresets.filter((p) => (p.stems || []).includes('master'))
const NAMES = ['Стриминг −14 LUFS', 'Громко −11 LUFS', 'Мягкая склейка']
const byName = (name) => masters.filter((p) => p.name.ru.toLowerCase().includes(name.toLowerCase()))

function inRange(v, spec) {
  if (typeof v !== 'number' || !Number.isFinite(v)) return false
  if (spec.zero_off && v === 0) return true
  return v >= spec.min && v <= spec.max
}

describe('готовые мастера (ТК76)', () => {
  it('три мастера со stems [master], по одному на название карточки', () => {
    expect(masters.length).toBe(3)
    for (const p of masters) expect(p.stems, p.id).toEqual(['master'])
    for (const n of NAMES) expect(byName(n).length, n).toBe(1)
  })

  it.each([['Стриминг −14 LUFS', -14], ['Громко −11 LUFS', -11]])('%s: последний блок limiter с target %d',
    (name, target) => {
      const [p] = byName(name)
      expect(p).toBeDefined()
      const last = p.chain[p.chain.length - 1]
      expect(last.type).toBe('limiter')
      expect(last.target_lufs).toBe(target)
    })

  it('«Мягкая склейка» — без limiter (громкость не трогает)', () => {
    const [p] = byName('Мягкая склейка')
    expect(p).toBeDefined()
    expect(p.chain.some((b) => b.type === 'limiter')).toBe(false)
    expect(p.chain.length).toBeGreaterThan(0)
  })

  it.each(masters.map((p) => [p.id, p]))('%s: цепочка проходит описание блоков', (_, p) => {
    expect(p.chain.length).toBeLessThanOrEqual(16)
    for (const b of p.chain) {
      const spec = blocks[b.type]
      expect(spec, `блок ${b.type}`).toBeDefined()
      for (const [k, v] of Object.entries(b)) {
        if (k === 'type') continue
        const ps = spec.params.find((x) => x.id === k)
        expect(ps, `${b.type}.${k}`).toBeDefined()
        if (typeof v === 'number') expect(inRange(v, ps), `${b.type}.${k}=${v}`).toBe(true)
      }
    }
  })
})

describe('место готовых партий по умолчанию (ТК76, усл. 46б)', () => {
  const percs = fxPresets.filter((p) => (p.stems || []).includes('perc'))
  const pads = fxPresets.filter((p) => (p.stems || []).includes('synth') && p.style === 'pad')
  const SIDE = /хэт|шейкер|бубен|ковбелл|райд/i
  const CENTER = /бочк|хлопк|римшот/i

  it('перкуссии со сдвигом есть (хэт, шейкер, бубен, ковбелл, райд)', () => {
    expect(percs.filter((p) => SIDE.test(p.name.ru)).length).toBeGreaterThanOrEqual(5)
  })

  it.each(percs.filter((p) => SIDE.test(p.name.ru)).map((p) => [p.name.ru, p]))(
    '%s: сдвиг от центра 0,2…0,4', (_, p) => {
      expect(p.place).toBeDefined()
      expect(p.place.pan).not.toBe(0)
      expect(Math.abs(p.place.pan)).toBeGreaterThanOrEqual(0.2)
      expect(Math.abs(p.place.pan)).toBeLessThanOrEqual(0.4)
    })

  it.each(percs.filter((p) => CENTER.test(p.name.ru)).map((p) => [p.name.ru, p]))(
    '%s: центр', (_, p) => {
      expect(p.place?.pan ?? 0).toBe(0)
    })

  it.each(pads.map((p) => [p.id, p]))('%s: пэд — ширина 1,5', (_, p) => {
    expect(p.place).toBeDefined()
    expect(p.place.width).toBe(1.5)
  })
})
