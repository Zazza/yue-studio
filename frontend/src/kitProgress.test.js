// Тесты карточки internal-own-track, этап 14б, условие 117 (тест-кейс ТК145, фронт):
// kitProgressText(p, t) — строка «качаю набор «<name>»: часть <part>, <done> из <total> файлов, <МБ> МБ»;
// p — {name, part, done, total, bytes} из GET /fx/kits/progress; нет установки ({} или null) — ''.
// t — функция перевода (здесь заглушка: ключ + переменные JSON — числа видны в любом случае).
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { kitProgressText } from './kitProgress.js'

const t = (k, vars) => k + JSON.stringify(vars ?? {})
const P = { name: 'vsco-violin', part: 'pizz', done: 7, total: 31, bytes: 12_345_678 }

describe('kitProgressText (условие 117, ТК145)', () => {
  it('нет установки — пустая строка', () => {
    expect(kitProgressText({}, t)).toBe('')
    expect(kitProgressText(null, t)).toBe('')
    expect(kitProgressText(undefined, t)).toBe('')
  })

  it('идёт установка — набор, часть, done, total', () => {
    const s = kitProgressText(P, t)
    expect(s).toContain('vsco-violin')
    expect(s).toContain('pizz')
    expect(s).toMatch(/(?<![\d.])7(?![\d.])/)
    expect(s).toMatch(/(?<![\d.])31(?![\d.])/)
  })

  it('объём — в мегабайтах (12 345 678 байт → 12,3 или 12 МБ), не в байтах', () => {
    const s = kitProgressText(P, t)
    expect(s).toMatch(/(?<![\d.,])12(?:[.,]3)?(?![\d])/)
    expect(s).not.toContain('12345678')
  })

  it('ноль скачанного — строка с нулями, не пустая', () => {
    const s = kitProgressText({ name: 'swagbass', part: 'bass', done: 0, total: 12, bytes: 0 }, t)
    expect(s).toContain('swagbass')
    expect(s).toMatch(/(?<![\d.])12(?![\d.])/)
    expect(s).toMatch(/(?<![\d.,])0(?![\d])/)
  })
})
