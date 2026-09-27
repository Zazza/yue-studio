// Тесты инструментальной стойки: компиляция выбора в текст стиля.
import { describe, it, expect } from 'vitest'
import { rackCompile, rackGroups, rackEffects, allRackItems } from './rack.js'

describe('rackCompile', () => {
  it('пустой выбор → пустая строка (стойка не влияет на стиль)', () => {
    expect(rackCompile([])).toBe('')
  })

  it('выбранный инструмент разворачивается в свою phrasing-формулировку', () => {
    const item = allRackItems()[0]
    const out = rackCompile([{ id: item.id, effect: '' }])
    expect(out).toBe(item.phrasing)
  })

  it('примочка добавляет свою английскую формулировку', () => {
    const item = allRackItems()[0]
    const out = rackCompile([{ id: item.id, effect: 'reverb' }])
    expect(out).toBe(`${item.phrasing} drenched in reverb`)
  })

  it('несколько инструментов склеиваются запятыми', () => {
    const [a, b] = allRackItems()
    const out = rackCompile([{ id: a.id, effect: '' }, { id: b.id, effect: '' }])
    expect(out).toBe(`${a.phrasing}, ${b.phrasing}`)
  })

  it('неизвестный id (устаревший выбор) молча пропускается', () => {
    expect(rackCompile([{ id: 'no-such', effect: '' }])).toBe('')
  })

  it('у каждого пункта стойки id уникален', () => {
    const ids = allRackItems().map(i => i.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('эффекты уникальны по id, «чисто» — пустой id', () => {
    const ids = rackEffects.map(e => e.id)
    expect(new Set(ids).size).toBe(ids.length)
    expect(ids).toContain('')
  })
})
