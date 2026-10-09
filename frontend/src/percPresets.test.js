// Тесты карточки internal-own-track, этап 5 «Перкуссия по сетке», условие 33 (тест-кейс ТК60):
// девять готовых перкуссий в fxPresets со stems ['perc'], у каждой pattern (fours/eighths/
// sixteenths/backbeat/offbeat) и swing 0…0,5, цепочка начинается с блока perc; kit-перкуссии —
// kit osdk/…; наборы докачиваются как у sampler — missingKits видит perc.
// Голос и рисунок по названию — из списка условия 30 (1 хэт, 2 шейкер, 3 хлопок, 4 ковбелл,
// 5 римшот, 6 бубен) и названий готовых перкуссий.
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { fxPresets } from './fxPresets.js'
import { missingKits } from './fxChain.js'

const PATTERNS = ['fours', 'eighths', 'sixteenths', 'backbeat', 'offbeat']
// название → [voice или kit, рисунок]
const WANT = {
  'Хэт восьмыми (808)': [{ voice: 1 }, 'eighths'],
  'Хэт шестнадцатыми (808)': [{ voice: 1 }, 'sixteenths'],
  'Шейкер шестнадцатыми': [{ voice: 2 }, 'sixteenths'],
  'Бубен на 2 и 4': [{ voice: 6 }, 'backbeat'],
  'Хлопки на 2 и 4 (909)': [{ voice: 3 }, 'backbeat'],
  'Ковбелл четвертями': [{ voice: 4 }, 'fours'],
  'Римшот на 2 и 4': [{ voice: 5 }, 'backbeat'],
  'Удвоение бочки (osdk)': [{ kit: 'osdk/kick' }, 'fours'],
  'Райд восьмыми (osdk)': [{ kit: 'osdk/ride' }, 'eighths'],
}

describe('fxPresets: готовые перкуссии (ТК60)', () => {
  const percs = fxPresets.filter((p) => (p.stems || []).includes('perc'))

  it('девять перкуссий со stems [perc]', () => {
    expect(percs.length).toBe(9)
    for (const p of percs) expect(p.stems, p.id).toEqual(['perc'])
  })

  it('названия — из карточки', () => {
    expect(percs.map((p) => p.name.ru).sort()).toEqual(Object.keys(WANT).sort())
  })

  it.each(percs.map((p) => [p.id, p]))('%s: pattern из пяти и swing 0…0,5', (_, p) => {
    expect(PATTERNS).toContain(p.pattern)
    expect(typeof p.swing).toBe('number')
    expect(p.swing).toBeGreaterThanOrEqual(0)
    expect(p.swing).toBeLessThanOrEqual(0.5)
  })

  it.each(percs.map((p) => [p.id, p]))('%s: цепочка начинается с perc', (_, p) => {
    expect(p.chain[0].type).toBe('perc')
  })

  it.each(Object.entries(WANT))('%s: звук и рисунок по названию', (name, [sound, pattern]) => {
    const p = percs.find((x) => x.name.ru === name)
    expect(p, `нет перкуссии «${name}»`).toBeTruthy()
    expect(p.pattern).toBe(pattern)
    if (sound.kit) {
      expect(p.chain[0].kit).toBe(sound.kit)
      // voice 0 — сэмплы набора (условие 30); не задан — тоже допустимо, если 0 по умолчанию
      if (p.chain[0].voice !== undefined) expect(p.chain[0].voice).toBe(0)
    } else {
      expect(p.chain[0].voice).toBe(sound.voice)
    }
  })

  it('kit-перкуссии — наборы osdk/…', () => {
    const kits = percs.filter((p) => p.chain[0].kit)
    expect(kits.length).toBe(2)
    for (const p of kits) expect(p.chain[0].kit, p.id).toMatch(/^osdk\//)
  })

  it('перкуссии не подсовываются обычным дорожкам: у прочих пресетов нет perc в цепочке', () => {
    for (const p of fxPresets.filter((x) => !(x.stems || []).includes('perc'))) {
      expect(p.chain.some((b) => b.type === 'perc'), p.id).toBe(false)
    }
  })
})

describe('missingKits видит perc (ТК60)', () => {
  it("perc с kit osdk/ride, наборов нет → ['osdk']", () => {
    expect(missingKits([{ type: 'perc', kit: 'osdk/ride' }], [])).toEqual(['osdk'])
  })

  it('набор уже установлен → []', () => {
    expect(missingKits([{ type: 'perc', kit: 'osdk/ride' }], [{ name: 'osdk/ride', samples: 2 }])).toEqual([])
  })

  it('perc голосом драм-машины (без kit) → []', () => {
    expect(missingKits([{ type: 'perc', voice: 1 }], [])).toEqual([])
  })
})
