// Тесты карточки internal-own-track, этап 13б, условие 100 («bypass у цепочки с synth/perc — только эти блоки»)
// и этап 14б, условие 118 («нет набора в приложении — докачка»): на странице «Инструменты» при выключенной
// обработке (bypass) наборы докачиваются только для блоков synth/perc — их и играет воркер; наборы прочих
// блоков (sampler и т.п.) в этом режиме не нужны и не качаются. Проверка по исходнику InstrumentsPage.vue:
// в аргументе вызова ensureKits(...) (или в присваивании переменной-аргумента выше) есть отбор блоков по
// type synth/perc, зависящий от bypass; отбор без bypass не годится — с обработкой нужны все наборы.
// Написаны по карточке, без чтения реализации.
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, it, expect } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const src = readFileSync(join(here, 'components', 'InstrumentsPage.vue'), 'utf8')

const BEFORE_LINES = 12 // сколько строк перед вызовом считать «подготовкой аргумента»

// Вызовы ensureKits(...) (не объявление): окно = до BEFORE_LINES строк перед вызовом + аргументы до парной скобки.
function ensureKitsCalls(text) {
  const out = []
  const re = /\bensureKits\s*\(/g
  let m
  while ((m = re.exec(text))) {
    const head = text.slice(Math.max(0, m.index - 40), m.index)
    if (/function\s*$/.test(head)) continue // объявление функции, а не вызов
    let depth = 0
    let end = m.index + m[0].length - 1
    for (; end < text.length; end++) {
      if (text[end] === '(') depth++
      else if (text[end] === ')' && --depth === 0) break
    }
    const lineStart = text.lastIndexOf('\n', m.index)
    let from = lineStart
    for (let i = 0; i < BEFORE_LINES && from > 0; i++) from = text.lastIndexOf('\n', from - 1)
    out.push({ args: text.slice(m.index + m[0].length, end), window: text.slice(Math.max(0, from), end + 1) })
  }
  return out
}

// В куске кода есть отбор блоков по type, где упомянуты и synth, и perc.
function filtersSynthPerc(code) {
  const hasTypes = /['"]synth['"]/.test(code) && /['"]perc['"]/.test(code)
  const byType = /\.type\b/.test(code) || /\{\s*type\s*\}/.test(code)
  const selects = /\.filter\s*\(/.test(code) || /\.includes\s*\(/.test(code) || /\.has\s*\(/.test(code)
  return hasTypes && byType && selects
}

describe('InstrumentsPage: bypass — наборы только для synth/perc (условия 100, 118)', () => {
  const calls = ensureKitsCalls(src)

  it('страница докачивает наборы через ensureKits', () => {
    expect(calls.length, 'нет вызова ensureKits(...) в InstrumentsPage.vue').toBeGreaterThan(0)
  })

  it('у вызова ensureKits есть фильтр блоков synth/perc, зависящий от bypass', () => {
    const ok = calls.some((c) => {
      if (/bypass/.test(c.args) && filtersSynthPerc(c.args)) return true // отбор прямо в аргументе
      // аргумент — переменная, подготовленная выше: от её присваивания до вызова — bypass и отбор
      const id = c.args.trim().match(/^[A-Za-z_$][\w$]*$/)
      if (!id) return false
      const at = c.window.search(new RegExp(`\\b${id[0]}\\s*=[^=]`))
      const prep = at >= 0 ? c.window.slice(at) : ''
      return /bypass/.test(prep) && filtersSynthPerc(prep)
    })
    expect(ok, 'ни у одного вызова ensureKits нет отбора synth/perc по bypass:\n' +
      calls.map((c) => c.window).join('\n-----\n')).toBe(true)
  })

})
