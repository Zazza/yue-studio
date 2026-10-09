// Тесты карточки internal-own-track, этап 7а, условия 53 и 53а (тест-кейс ТК88): группы в списках
// готовых. presetGroups.js экспортирует PRESET_GROUPS — {id: {ru, en}} и чистую
// groupPresets(list, locale) → [{group, label, items}]: порядок первого появления группы, внутри —
// исходный порядок; без group — группа '' с label ''.
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { PRESET_GROUPS, groupPresets } from './presetGroups.js'

describe('PRESET_GROUPS (условие 53а)', () => {
  it('объект id → {ru, en}, не меньше двух групп (гитары и басы)', () => {
    expect(PRESET_GROUPS && typeof PRESET_GROUPS).toBe('object')
    const ids = Object.keys(PRESET_GROUPS)
    expect(ids.length).toBeGreaterThanOrEqual(2)
    for (const id of ids) {
      expect(id.trim(), 'пустой id группы').not.toBe('')
      for (const lang of ['ru', 'en']) {
        expect(typeof PRESET_GROUPS[id][lang], `${id}.${lang}`).toBe('string')
        expect(PRESET_GROUPS[id][lang].trim(), `${id}.${lang}`).not.toBe('')
      }
    }
  })
})

describe('groupPresets (ТК88)', () => {
  const [g1, g2] = Object.keys(PRESET_GROUPS)
  const list = () => [
    { id: 'a', group: g1 }, { id: 'b', group: g2 }, { id: 'c', group: g1 }, { id: 'd' },
  ]
  const shape = (out) => out.map((x) => [x.group, x.items.map((p) => p.id)])

  it('[a{g1}, b{g2}, c{g1}, d{}] → g1:[a,c], g2:[b], "":[d]', () => {
    expect(shape(groupPresets(list(), 'en'))).toEqual([[g1, ['a', 'c']], [g2, ['b']], ['', ['d']]])
  })

  it('label — подпись группы на языке locale (en и ru); у группы "" — пустая', () => {
    const en = groupPresets(list(), 'en')
    expect(en.map((x) => x.label)).toEqual([PRESET_GROUPS[g1].en, PRESET_GROUPS[g2].en, ''])
    const ru = groupPresets(list(), 'ru')
    expect(ru.map((x) => x.label)).toEqual([PRESET_GROUPS[g1].ru, PRESET_GROUPS[g2].ru, ''])
  })

  it('порядок групп — по первому появлению, не по PRESET_GROUPS', () => {
    const out = groupPresets([{ id: 'x' }, { id: 'y', group: g2 }, { id: 'z', group: g1 }], 'en')
    expect(shape(out)).toEqual([['', ['x']], [g2, ['y']], [g1, ['z']]])
  })

  it('элементы — сами пресеты (не копии без полей)', () => {
    const l = list()
    const out = groupPresets(l, 'en')
    expect(out[0].items[0]).toEqual(l[0])
  })

  it('пустой список → []', () => {
    expect(groupPresets([], 'en')).toEqual([])
  })

  it('все без group → одна группа "" с label ""', () => {
    const out = groupPresets([{ id: 'p' }, { id: 'q' }], 'ru')
    expect(out).toEqual([{ group: '', label: '', items: [{ id: 'p' }, { id: 'q' }] }])
  })

  it('чистая: вход не мутируется', () => {
    const l = list()
    const before = JSON.parse(JSON.stringify(l))
    groupPresets(l, 'en')
    expect(l).toEqual(before)
  })
})
