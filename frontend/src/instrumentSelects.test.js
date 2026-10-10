// Тесты карточки internal-own-track, этап 12, условия 90–91 (тест-кейс ТК130): страница «Инструменты» —
// два списка (группа → инструмент) вместо стены кнопок пресетов.
// presetGroups.js: groupOptions(groups, firstLabel) → [{value, label}], itemOptions(group, locale) →
// [{value, label, title}], groupOf(groups, id) → group. groups — выход groupPresets.
// Страница проверяется по исходнику InstrumentsPage.vue. Написаны по карточке, без чтения реализации.
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, it, expect } from 'vitest'
import { PRESET_GROUPS, groupPresets, groupOptions, itemOptions, groupOf } from './presetGroups.js'
import { fxPresets } from './fxPresets.js'

const [G1, G2] = Object.keys(PRESET_GROUPS)
const p = (id, group, extra = {}) => ({
  id,
  ...(group === undefined ? {} : { group }),
  name: { ru: `имя ${id}`, en: `name ${id}` },
  note: { ru: `заметка ${id}`, en: `note ${id}` },
  ...extra,
})
// [a{G1}, b{G2}, c{G1}, d{без группы}] → G1:[a,c], G2:[b], '':[d]
const list = () => [p('a', G1), p('b', G2), p('c', G1), p('d')]

describe('groupOptions (условие 90, ТК130)', () => {
  it('value — group по порядку groupPresets, label — подпись группы', () => {
    const groups = groupPresets(list(), 'en')
    expect(groupOptions(groups, 'First')).toEqual([
      { value: G1, label: PRESET_GROUPS[G1].en },
      { value: G2, label: PRESET_GROUPS[G2].en },
      { value: '', label: 'First' },
    ])
  })

  it('подписи — на языке groups (ru)', () => {
    const groups = groupPresets(list(), 'ru')
    expect(groupOptions(groups, 'Первая').map((o) => o.label))
      .toEqual([PRESET_GROUPS[G1].ru, PRESET_GROUPS[G2].ru, 'Первая'])
  })

  it('группа без подписи первой по порядку → firstLabel', () => {
    const groups = groupPresets([p('x'), p('y', G2)], 'en')
    expect(groupOptions(groups, 'Presets')).toEqual([
      { value: '', label: 'Presets' },
      { value: G2, label: PRESET_GROUPS[G2].en },
    ])
  })

  it('пусто → []', () => {
    expect(groupOptions([], 'Presets')).toEqual([])
  })

  it('на реальных fxPresets: по одному пункту на группу, порядок groupPresets, подписи непустые', () => {
    const groups = groupPresets(fxPresets, 'ru')
    const out = groupOptions(groups, 'Пресеты')
    expect(out.map((o) => o.value)).toEqual(groups.map((g) => g.group))
    for (const o of out) expect(String(o.label).trim(), `подпись группы ${o.value}`).not.toBe('')
  })
})

describe('itemOptions (условие 90, ТК130)', () => {
  it('value = id, label = name[locale], title = note[locale] — по порядку пунктов группы (en)', () => {
    const g1 = groupPresets(list(), 'en')[0]
    expect(itemOptions(g1, 'en')).toEqual([
      { value: 'a', label: 'name a', title: 'note a' },
      { value: 'c', label: 'name c', title: 'note c' },
    ])
  })

  it('locale ru → русские имя и заметка', () => {
    const g1 = groupPresets(list(), 'ru')[0]
    expect(itemOptions(g1, 'ru')).toEqual([
      { value: 'a', label: 'имя a', title: 'заметка a' },
      { value: 'c', label: 'имя c', title: 'заметка c' },
    ])
  })

  it('чужие группы не попадают', () => {
    const groups = groupPresets(list(), 'en')
    const g2 = groups.find((g) => g.group === G2)
    expect(itemOptions(g2, 'en').map((o) => o.value)).toEqual(['b'])
    const none = groups.find((g) => g.group === '')
    expect(itemOptions(none, 'en').map((o) => o.value)).toEqual(['d'])
  })

  it('нет группы (undefined/null) → []', () => {
    expect(itemOptions(undefined, 'en')).toEqual([])
    expect(itemOptions(null, 'ru')).toEqual([])
  })

  it('на реальных fxPresets: пункты группы = её пресеты, подписи из name', () => {
    const groups = groupPresets(fxPresets, 'en')
    for (const g of groups) {
      const out = itemOptions(g, 'en')
      expect(out.map((o) => o.value)).toEqual(g.items.map((x) => x.id))
      expect(out.map((o) => o.label)).toEqual(g.items.map((x) => x.name.en))
    }
  })
})

describe('groupOf (условие 90, ТК130)', () => {
  const groups = () => groupPresets(list(), 'en')

  it('нашёл → группа, где лежит пресет', () => {
    expect(groupOf(groups(), 'a')).toBe(G1)
    expect(groupOf(groups(), 'c')).toBe(G1)
    expect(groupOf(groups(), 'b')).toBe(G2)
    expect(groupOf(groups(), 'd')).toBe('')
  })

  it('не нашёл → группа первой', () => {
    expect(groupOf(groups(), 'nope')).toBe(G1)
    expect(groupOf(groupPresets([p('x', G2), p('y', G1)], 'en'), 'nope')).toBe(G2)
  })

  it('пустой id → группа первой', () => {
    expect(groupOf(groups(), '')).toBe(G1)
  })

  it('пусто (нет групп) → ""', () => {
    expect(groupOf([], 'a')).toBe('')
  })
})

// ---- страница по исходнику (условие 91) ----

const srcDir = dirname(fileURLToPath(import.meta.url))
const page = readFileSync(join(srcDir, 'components', 'InstrumentsPage.vue'), 'utf8')
const template = page.slice(page.indexOf('<template'), page.lastIndexOf('</template>'))
const script = (page.match(/<script\b[^>]*>([\s\S]*?)<\/script>/g) || []).join('\n')

// открывающие теги <Tag ...> целиком (атрибуты могут идти в несколько строк)
// кавычки учитываются: внутри обработчика может быть стрелка =>
const tags = (name) => template.match(new RegExp(`<${name}\\b(?:[^>"']|"[^"]*"|'[^']*')*>`, 'g')) || []
const attr = (tag, name) => {
  const m = tag.match(new RegExp(`(?:^|\\s)${name}="([^"]*)"`))
  return m ? m[1] : null
}
// выражение опирается на fn: прямо или через computed/функцию скрипта, построенную на fn
function builtOn(expr, fn) {
  if (!expr) return false
  if (expr.includes(`${fn}(`)) return true
  const name = (expr.match(/^\s*([A-Za-z_$][\w$]*)/) || [])[1]
  if (!name) return false
  const def = script.match(new RegExp(`(?:const|let|function)\\s+${name}\\b[\\s\\S]*?(?=\\n(?:const|let|function|async function|watch|onMounted)\\b|$)`))
  return !!def && def[0].includes(`${fn}(`)
}
// тело функции/watch из скрипта до следующего определения верхнего уровня
const bodyAfter = (re) => {
  const m = script.match(new RegExp(`${re.source}[\\s\\S]*?(?=\\n(?:const|let|function|async function|watch|onMounted)\\b|$)`))
  return m ? m[0] : ''
}

describe('InstrumentsPage: два списка вместо кнопок (условие 91, ТК130)', () => {
  it('нет кнопок пресетов: ни <button> с applyPreset, ни v-for кнопок small-btn', () => {
    for (const b of tags('button')) {
      expect(b, 'кнопка выбирает пресет').not.toMatch(/applyPreset/)
      expect(b.includes('v-for') && b.includes('small-btn'), `v-for кнопок: ${b}`).toBe(false)
    }
  })

  it('импортирует groupOptions и itemOptions из presetGroups.js', () => {
    expect(script).toMatch(/import\s*\{[^}]*\bgroupOptions\b[^}]*\}\s*from\s*['"]\.\.\/presetGroups\.js['"]/)
    expect(script).toMatch(/import\s*\{[^}]*\bitemOptions\b[^}]*\}\s*from\s*['"]\.\.\/presetGroups\.js['"]/)
  })

  const selects = () => tags('VSelect')
  const groupSel = () => selects().filter((s) => builtOn(attr(s, ':options'), 'groupOptions'))
  const itemSel = () => selects().filter((s) => builtOn(attr(s, ':options'), 'itemOptions'))

  it('есть VSelect групп (options из groupOptions), без поиска', () => {
    expect(groupSel()).toHaveLength(1)
    expect(groupSel()[0]).not.toMatch(/\ssearchable\b/)
  })

  it('есть VSelect инструментов (options из itemOptions), searchable', () => {
    expect(itemSel()).toHaveLength(1)
    expect(itemSel()[0]).toMatch(/\ssearchable\b/)
  })

  it('оба списка в одной строке: между ними нет других элементов', () => {
    const [g] = groupSel()
    const [i] = itemSel()
    expect(g && i).toBeTruthy()
    const gi = template.indexOf(g)
    const ii = template.indexOf(i)
    expect(gi).toBeLessThan(ii)
    const between = template.slice(gi + g.length, ii)
    expect(between, 'между списками другие VSelect/кнопки/ChainEditor').not.toMatch(/<(VSelect|button|ChainEditor)\b/)
  })

  it('выбор инструмента вызывает applyPreset(id)', () => {
    const [i] = itemSel()
    expect(i).toBeTruthy()
    const on = attr(i, '@update:model-value') ?? attr(i, '@update:modelValue') ?? attr(i, 'v-on:update:model-value')
    const model = attr(i, 'v-model') ?? attr(i, ':model-value') ?? attr(i, ':modelValue')
    let ok = false
    if (on) {
      ok = on.includes('applyPreset') ||
        bodyAfter(new RegExp(`function\\s+${on.trim().split(/[\s(]/)[0]}\\b`)).includes('applyPreset')
    }
    if (!ok && model) {
      const name = model.trim().split(/[\s.]/)[0]
      ok = bodyAfter(new RegExp(`watch\\(\\s*(?:\\(\\)\\s*=>\\s*)?${name}\\b`)).includes('applyPreset')
    }
    expect(ok, `обработчик выбора: ${i}`).toBe(true)
  })

  it('подсказка пресета (note) под списками осталась', () => {
    const [i] = itemSel()
    expect(i).toBeTruthy()
    const after = template.slice(template.indexOf(i))
    expect(after).toMatch(/tr\(\s*preset\.note\s*\)/)
  })
})
