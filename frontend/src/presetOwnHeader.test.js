// Тесты карточки internal-own-track, «Мелкие долги этапов 7–8»: условие 77 (тест-кейс ТК113).
// presetOptions: свои пресеты (family '') — ровно один заголовок «— Мои —», за ним все свои по порядку
// дерева (presetTree); у остальных семей заголовки «— семья · течение —», как раньше.
// VSelect: у пункта списка привязан title — подсказка при наведении (проверка по исходнику шаблона).
// Написаны по карточке, без чтения реализации.
// Предположения: заголовок отличается disabled (или props.disabled), как в ТК107; текст заголовка — в
// title, text или label; пункты VSelect — <li> с v-for.
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, it, expect } from 'vitest'
import { presetTree, presetOptions } from './soundPresets.js'

const here = dirname(fileURLToPath(import.meta.url))
const isHeader = (o) => o.disabled === true || (o.props && o.props.disabled === true)
const texts = (o) => [o.title, o.text, o.label].filter((x) => typeof x === 'string')
const hasText = (o, t) => texts(o).includes(t)
const p = (id, name, family) => ({ id, name, family, note: '', specs: [] })
const OWN_HEAD = '— Мои —'

describe('presetOptions: свои пресеты под одним заголовком «— Мои —» (ТК113)', () => {
  const input = [
    p(101, 'Мастер для демо · громко', ''),
    p(2, 'Пост-панк · холодный', 'Рок'),
    p(100, 'Мой бас', ''),
    p(1, 'Синти-поп · светлый', 'Электроника'),
    p(102, 'Мастер для демо · тихо', ''),
    p(3, 'Пост-панк · тёплый', 'Рок'),
    p(103, 'Сухой', ''),
  ]
  const ownIds = presetTree(input).filter((f) => f.family === '')
    .flatMap((f) => f.genres.flatMap((g) => g.items.map((x) => x.id)))

  it('ровно один заголовок «— Мои —»', () => {
    const heads = presetOptions(input).filter((o) => isHeader(o) && hasText(o, OWN_HEAD))
    expect(heads).toHaveLength(1)
  })

  it('нет заголовков «— Мои · <имя> —»', () => {
    const bad = presetOptions(input).filter((o) => isHeader(o) && texts(o).some((t) => t.startsWith('— Мои ·')))
    expect(bad.map(texts)).toEqual([])
  })

  it('за «— Мои —» — все свои подряд, в порядке дерева, без других заголовков', () => {
    const opts = presetOptions(input)
    const i = opts.findIndex((o) => isHeader(o) && hasText(o, OWN_HEAD))
    expect(i).toBeGreaterThanOrEqual(0)
    const after = opts.slice(i + 1)
    expect(after.some(isHeader)).toBe(false)
    expect(after.map((o) => o.value)).toEqual(ownIds)
    expect([...ownIds].sort((a, b) => a - b)).toEqual([100, 101, 102, 103])
  })

  it('у остальных семей — заголовки «— семья · течение —», как раньше', () => {
    const opts = presetOptions(input)
    const heads = opts.filter((o) => isHeader(o) && !hasText(o, OWN_HEAD))
    expect(heads.map((o) => texts(o)[0])).toEqual(['— Рок · Пост-панк —', '— Электроника · Синти-поп —'])
    const items = opts.filter((o) => !isHeader(o)).map((o) => o.value)
    expect(items).toEqual([2, 3, 1, ...ownIds])
  })

  it('один свой пресет — тоже под «— Мои —»', () => {
    const opts = presetOptions([p(100, 'Мой бас', '')])
    expect(opts.filter(isHeader).map((o) => hasText(o, OWN_HEAD))).toEqual([true])
    expect(opts.filter((o) => !isHeader(o)).map((o) => o.value)).toEqual([100])
  })

  it('своих нет — нет и заголовка «— Мои —»', () => {
    const opts = presetOptions([p(2, 'Пост-панк · холодный', 'Рок')])
    expect(opts.some((o) => hasText(o, OWN_HEAD))).toBe(false)
  })

  it('пусто → []', () => {
    expect(presetOptions([])).toEqual([])
  })
})

describe('VSelect: title опции — подсказка при наведении (ТК113)', () => {
  const src = readFileSync(join(here, 'VSelect.vue'), 'utf8')
  const tpl = (src.match(/<template>([\s\S]*)<\/template>/) || [])[1] || ''
  // открывающие теги <li ...>, кавычки внутри атрибутов учитываются (в выражениях бывает «=>»)
  const lis = [...tpl.matchAll(/<li\b((?:[^>"']|"[^"]*"|'[^']*')*)>/g)].map((m) => m[1])
  const items = lis.filter((a) => /\bv-for=/.test(a))

  it('в шаблоне есть пункты списка <li v-for>', () => {
    expect(items.length).toBeGreaterThan(0)
  })

  it('у каждого пункта <li v-for> привязан title', () => {
    for (const a of items) expect(a, `<li${a}>`).toMatch(/(?:^|\s)(?::|v-bind:)title=/)
  })
})
