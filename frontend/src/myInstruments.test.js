// Карточка internal-own-track, этап 13в, условия 105–106 / ТК140: свои инструменты.
// mergeInstruments(builtin, mine, locale) — готовые как есть + свои в конце; свои ложатся
// в свою группу через groupPresets (внизу группы; неизвестная группа — отдельной в конце).
// instrumentFromPreset(p, chain, name) — тело для воркера. По исходникам: студия и
// «Инструменты» берут список из useInstruments; сохранение своих — только на «Инструментах».
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { mergeInstruments, instrumentFromPreset } from './myInstruments.js'
import { groupPresets } from './presetGroups.js'
import { fxPresets } from './fxPresets.js'

const here = dirname(fileURLToPath(import.meta.url))
const src = (rel) => readFileSync(join(here, rel), 'utf8')

// Текст поля на языке: объект {ru, en} или уже строка
const txt = (v, lang) => (v && typeof v === 'object' ? v[lang] : v)

// Готовые — маленькие фикстуры в форме fxPresets.js + один реальный (guitar-fuzz)
const realFuzz = fxPresets.find((p) => p.id === 'guitar-fuzz')

const builtin = () => [
  {
    id: 'g-clean',
    name: { ru: 'Чистая', en: 'Clean' },
    note: { ru: 'чисто', en: 'clean' },
    group: 'guitar-clean',
    stems: ['guitar'],
    chain: [{ type: 'eq', highpass_hz: 80 }],
  },
  {
    id: 'g-drive',
    name: { ru: 'Перегруз', en: 'Drive' },
    note: { ru: 'перегруз', en: 'drive' },
    group: 'guitar-drive',
    stems: ['guitar', 'other'],
    chain: [{ type: 'drive', gain_db: 20, mix: 1 }],
  },
  {
    id: 's-pad',
    name: { ru: 'Пэд', en: 'Pad' },
    note: { ru: 'пэд', en: 'pad' },
    group: 'synth-pad',
    stems: ['synth'],
    style: 'pad',
    octave: 1,
    place: { pan: 0, width: 1.5 },
    chain: [{ type: 'synth', wave: 'saw' }],
  },
]

const mineDrive = () => ({
  id: 7,
  name: 'Мой перегруз',
  base: 'g-drive',
  group: 'guitar-drive',
  stems: ['guitar', 'other'],
  chain: [{ type: 'drive', gain_db: 30, mix: 1 }],
  extra: {},
})

const mineSynth = () => ({
  id: 8,
  name: 'Мой арп',
  base: 's-pad',
  group: 'synth-pad',
  stems: ['synth'],
  chain: [{ type: 'synth', wave: 'square' }],
  extra: { style: 'arp', octave: 2, pattern: 'x.x.', swing: 0.2, accent: 0.5 },
})

describe('mergeInstruments — общий список', () => {
  test('порядок: готовые как есть, затем свои', () => {
    const b = builtin()
    const out = mergeInstruments(b, [mineDrive(), mineSynth()], 'ru')
    expect(out).toHaveLength(b.length + 2)
    expect(out.slice(0, b.length)).toEqual(builtin())
    expect(out.slice(b.length).map((p) => p.id)).toEqual(['my-7', 'my-8'])
  })

  test('своих нет — список равен готовым', () => {
    expect(mergeInstruments(builtin(), [], 'ru')).toEqual(builtin())
  })

  test('свой: id my-<id>, mine: true, wid = id воркера', () => {
    const out = mergeInstruments(builtin(), [mineDrive()], 'ru')
    const m = out.find((p) => p.id === 'my-7')
    expect(m).toBeTruthy()
    expect(m.mine).toBe(true)
    expect(m.wid).toBe(7)
  })

  test('готовые не помечаются своими', () => {
    const out = mergeInstruments(builtin(), [mineDrive()], 'ru')
    for (const p of out.slice(0, 3)) expect(p.mine).toBeFalsy()
  })

  test('имя с пометкой «(мой)» / «(mine)» на обоих языках', () => {
    const m = mergeInstruments(builtin(), [mineDrive()], 'ru').find((p) => p.mine)
    expect(m.name.ru).toBe('Мой перегруз (мой)')
    expect(m.name.en).toBe('Мой перегруз (mine)')
  })

  test('note по base: «свой, на основе «<имя готового>»» на языке locale', () => {
    const ru = mergeInstruments(builtin(), [mineDrive()], 'ru').find((p) => p.mine)
    const noteRu = txt(ru.note, 'ru')
    expect(noteRu).toContain('свой')
    expect(noteRu).toContain('на основе')
    expect(noteRu).toContain('Перегруз')

    const en = mergeInstruments(builtin(), [mineDrive()], 'en').find((p) => p.mine)
    expect(txt(en.note, 'en')).toContain('Drive')
  })

  test('note на основе реального готового из fxPresets содержит его имя', () => {
    const mine = { ...mineDrive(), base: 'guitar-fuzz' }
    const m = mergeInstruments([realFuzz], [mine], 'ru').find((p) => p.mine)
    expect(txt(m.note, 'ru')).toContain(realFuzz.name.ru)
  })

  test('base не найден среди готовых — note «свой» без «на основе»', () => {
    const mine = { ...mineDrive(), base: 'no-such-preset' }
    const m = mergeInstruments(builtin(), [mine], 'ru').find((p) => p.mine)
    const note = txt(m.note, 'ru')
    expect(note).toContain('свой')
    expect(note).not.toContain('на основе')
  })

  test('group/stems/chain своего — как с воркера', () => {
    const w = mineDrive()
    const m = mergeInstruments(builtin(), [w], 'ru').find((p) => p.mine)
    expect(m.group).toBe(w.group)
    expect(m.stems).toEqual(w.stems)
    expect(m.chain).toEqual(w.chain)
  })

  test('поля extra (style, octave, pattern, swing, accent) перенесены на верхний уровень', () => {
    const m = mergeInstruments(builtin(), [mineSynth()], 'ru').find((p) => p.mine)
    expect(m.style).toBe('arp')
    expect(m.octave).toBe(2)
    expect(m.pattern).toBe('x.x.')
    expect(m.swing).toBe(0.2)
    expect(m.accent).toBe(0.5)
  })

  test('groupPresets: свой «guitar-drive» — последним в группе «Гитара: перегруз»', () => {
    const b = [...builtin(), { ...builtin()[1], id: 'g-drive-2' }]
    const groups = groupPresets(mergeInstruments(b, [mineDrive()], 'ru'), 'ru')
    const drive = groups.find((g) => g.group === 'guitar-drive')
    expect(drive.label).toBe('Гитара: перегруз')
    expect(drive.items.map((p) => p.id)).toEqual(['g-drive', 'g-drive-2', 'my-7'])
    // своя группа не появилась отдельно — порядок групп как у готовых
    expect(groups.map((g) => g.group)).toEqual(['guitar-clean', 'guitar-drive', 'synth-pad'])
  })

  test('свой с группой, которой нет у готовых, — отдельной группой в конце', () => {
    const odd = { ...mineDrive(), id: 9, group: 'my-own-group' }
    const groups = groupPresets(mergeInstruments(builtin(), [odd, mineSynth()], 'ru'), 'ru')
    expect(groups.map((g) => g.group)).toEqual(['guitar-clean', 'guitar-drive', 'synth-pad', 'my-own-group'])
    expect(groups.at(-1).items.map((p) => p.id)).toEqual(['my-9'])
    expect(groups.find((g) => g.group === 'synth-pad').items.map((p) => p.id)).toEqual(['s-pad', 'my-8'])
  })
})

describe('instrumentFromPreset — тело для воркера', () => {
  const chain = [{ type: 'drive', gain_db: 12, mix: 0.5 }]

  test('из готового: base = его id, group/stems из пресета, chain и name — переданные', () => {
    const body = instrumentFromPreset(builtin()[1], chain, 'Новый')
    expect(body.base).toBe('g-drive')
    expect(body.group).toBe('guitar-drive')
    expect(body.stems).toEqual(['guitar', 'other'])
    expect(body.chain).toEqual(chain)
    expect(body.name).toBe('Новый')
  })

  test('chain — переданная, а не цепочка пресета', () => {
    const body = instrumentFromPreset(realFuzz, chain, 'X')
    expect(body.chain).toEqual(chain)
    expect(body.chain).not.toEqual(realFuzz.chain)
    expect(body.base).toBe('guitar-fuzz')
  })

  test('extra — известные поля пресета (style, octave, place)', () => {
    const body = instrumentFromPreset(builtin()[2], chain, 'X')
    expect(body.extra).toEqual({ style: 'pad', octave: 1, place: { pan: 0, width: 1.5 } })
  })

  test('extra — посторонние поля не попадают', () => {
    const p = {
      ...builtin()[2],
      pattern: 'x...',
      swing: 0.1,
      accent: 0.3,
      amp_hint: 'nam',
      foo: 1,
      level_db: -8,
    }
    const body = instrumentFromPreset(p, chain, 'X')
    expect(body.extra).toEqual({
      style: 'pad',
      octave: 1,
      pattern: 'x...',
      swing: 0.1,
      accent: 0.3,
      amp_hint: 'nam',
      place: { pan: 0, width: 1.5 },
    })
    for (const k of ['foo', 'level_db', 'id', 'name', 'note', 'chain', 'stems', 'group', 'mine', 'wid', 'base']) {
      expect(body.extra).not.toHaveProperty(k)
    }
  })

  test('из своего: base — base своего, не его id', () => {
    const m = mergeInstruments(builtin(), [mineSynth()], 'ru').find((p) => p.mine)
    const body = instrumentFromPreset(m, chain, 'Копия')
    expect(body.base).toBe('s-pad')
    expect(body.group).toBe('synth-pad')
    expect(body.stems).toEqual(['synth'])
    expect(body.chain).toEqual(chain)
    expect(body.name).toBe('Копия')
    expect(body.extra).toEqual({ style: 'arp', octave: 2, pattern: 'x.x.', swing: 0.2, accent: 0.5 })
  })

  test('пресет без extra-полей — extra пустой', () => {
    const body = instrumentFromPreset(builtin()[0], chain, 'X')
    expect(body.extra).toEqual({})
  })
})

describe('по исходникам: список из useInstruments', () => {
  const importsUse = /import\s[^;]*from\s+['"][^'"]*useInstruments(\.js)?['"]/
  const files = [
    'components/InstrumentsPage.vue',
    'components/TrackDesk.vue',
    'components/StudioSynth.vue',
    'components/StudioPerc.vue',
  ]
  for (const f of files) {
    test(`${f} импортирует useInstruments`, () => {
      expect(src(f)).toMatch(importsUse)
    })
  }

  test('InstrumentsPage: «сохранить как мой», сохранить, удалить — через api воркера', () => {
    const s = src('components/InstrumentsPage.vue')
    expect(s).toMatch(/api\.fxInstrumentCreate\(/)
    expect(s).toMatch(/api\.fxInstrumentUpdate\(/)
    expect(s).toMatch(/api\.fxInstrumentDelete\(/)
  })

  test('InstrumentsPage: удаление с подтверждением', () => {
    expect(src('components/InstrumentsPage.vue')).toMatch(/useConfirm|ConfirmModal/)
  })

  test('в студии кнопок сохранения своих нет', () => {
    for (const f of ['components/TrackDesk.vue', 'components/StudioSynth.vue', 'components/StudioPerc.vue']) {
      expect(src(f), f).not.toMatch(/fxInstrument(Create|Update|Delete)/)
    }
  })
})
