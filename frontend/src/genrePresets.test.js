// Тесты карточки internal-own-track, этап 8б: условия 71 и 73 (тест-кейсы ТК106, ТК107).
// ТК106 — в названиях и подсказках готовых цепочек (fxPresets) нет имён групп и исполнителей из списка условия 71.
// ТК107 — presetTree / presetOptions из soundPresets.js: список пресетов звука по семьям и течениям.
// Написаны по карточке, без чтения реализации.
// Предположения (карточка не уточняет): имя ищется целым словом без учёта регистра; опция-заголовок
// неактивна через disabled или props.disabled, подпись — title (или text); порядок семей проверяется
// относительный (пустые семьи в дереве карточкой не оговорены).
import { describe, it, expect } from 'vitest'
import { fxPresets } from './fxPresets.js'
import { presetTree, presetOptions } from './soundPresets.js'

const BANNED = [
  'Interpol', 'Joy Division', 'The Cure', 'New Order', 'Rammstein', 'Nightwish', 'Placebo', 'Radiohead',
  'Smiths', 'R.E.M.', 'Jack White', 'White Stripes', 'My Bloody Valentine', 'Slowdive', 'Muse',
  'Royal Blood', 'Hook', 'Хук', 'Beatles', 'King Crimson', 'Depeche Mode', 'Deep Purple', 'Doors',
  'Nick Cave', 'Portishead', 'Massive Attack', 'Supertramp', 'Ray Charles', 'Whitney Houston', 'a-ha',
  'Stevie Wonder', 'Stranglers', 'Pixies', 'Bowie', 'Björk', 'Чайковский', 'Eno', 'Gorillaz', 'Trio',
  'Daniel Johnston', 'Animals', 'Тарантино', 'Tarantino', 'Молчат Дома', 'Transmission', 'Sex on Fire',
]
const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
const RX = BANNED.map((n) => [n, new RegExp(`(?<![\\p{L}\\p{N}_])${esc(n)}(?![\\p{L}\\p{N}_])`, 'iu')])
const bannedIn = (text) => RX.filter(([, rx]) => rx.test(text || '')).map(([n]) => n)
const texts = (v) => (typeof v === 'string' ? [v] : v && typeof v === 'object' ? Object.values(v).filter((x) => typeof x === 'string') : [])

describe('fxPresets: без имён групп и исполнителей (ТК106)', () => {
  it('сверка работает: регистр не важен, слово целиком', () => {
    expect(bannedIn('как у the cure')).toEqual(['The Cure'])
    expect(bannedIn('r.e.m. и BOWIE')).toEqual(['R.E.M.', 'Bowie'])
    expect(bannedIn('enough room, museum hall')).toEqual([])
  })

  const cases = fxPresets.flatMap((p) => ['name', 'note'].flatMap((f) => texts(p[f]).map((t) => [p.id, f, t])))
  it.each(cases)('%s.%s без имён', (id, field, text) => {
    expect(bannedIn(text)).toEqual([])
  })
})

const FAM = ['Рок', 'Тяжёлое', 'Электроника', 'Поп и другое']
const p = (id, name, family) => ({ id, name, family, note: '', specs: [] })

describe('presetTree — семьи → течения → пресеты (ТК107)', () => {
  // нарочно вперемешку: свои, электроника, рок, поп, тяжёлое
  const input = [
    p(100, 'Мой бас', ''),
    p(1, 'Синти-поп · светлый', 'Электроника'),
    p(2, 'Пост-панк · холодный', 'Рок'),
    p(3, 'Гранж · грязный', 'Рок'),
    p(4, 'Пост-панк · тёплый', 'Рок'),
    p(5, 'Диско · клуб', 'Поп и другое'),
    p(6, 'Металл · плотный', 'Тяжёлое'),
    p(7, 'Синти-поп · тёмный', 'Электроника'),
    p(101, 'Мастер для демо · громко', ''),
    p(8, 'Живая ритм-секция', 'Рок'),
  ]

  it('семьи в порядке Рок, Тяжёлое, Электроника, Поп и другое, «Мои» (family \'\') в конце', () => {
    const fams = presetTree(input).filter((f) => f.genres.length).map((f) => f.family)
    expect(fams).toEqual([...FAM, ''])
  })

  it('порядок семей не зависит от порядка входа; «Мои» последние', () => {
    const fams = presetTree([p(1, 'Мой', ''), p(2, 'Диско · а', 'Поп и другое'), p(3, 'Гранж · б', 'Рок')])
      .filter((f) => f.genres.length).map((f) => f.family)
    expect(fams).toEqual(['Рок', 'Поп и другое', ''])
  })

  it('течения — по первому появлению, genre — до « · », без разделителя — всё название', () => {
    const rock = presetTree(input).find((f) => f.family === 'Рок')
    expect(rock.genres.map((g) => g.genre)).toEqual(['Пост-панк', 'Гранж', 'Живая ритм-секция'])
    expect(rock.genres[0].items.map((x) => x.id)).toEqual([2, 4])
    const own = presetTree(input).find((f) => f.family === '')
    expect(own.genres.map((g) => g.genre)).toEqual(['Мой бас', 'Мастер для демо'])
  })

  it('каждый пресет ровно в одном месте дерева', () => {
    const ids = presetTree(input).flatMap((f) => f.genres.flatMap((g) => g.items.map((x) => x.id)))
    expect([...ids].sort((a, b) => a - b)).toEqual(input.map((x) => x.id).sort((a, b) => a - b))
  })

  it('пусто → нет ни одного пресета', () => {
    const ids = presetTree([]).flatMap((f) => f.genres.flatMap((g) => g.items))
    expect(ids).toEqual([])
  })
})

const isHeader = (o) => o.disabled === true || (o.props && o.props.disabled === true)
const label = (o) => o.title ?? o.text

describe('presetOptions — опции VSelect с заголовками (ТК107)', () => {
  const input = [
    p(1, 'Синти-поп · светлый', 'Электроника'),
    p(2, 'Пост-панк · холодный', 'Рок'),
    p(3, 'Пост-панк · тёплый', 'Рок'),
    p(100, 'Мой бас', ''),
  ]

  it('пусто → []', () => {
    expect(presetOptions([])).toEqual([])
  })

  it('заголовки неактивны «— семья · течение —», value пресетов — id, порядок как в дереве', () => {
    const opts = presetOptions(input)
    const heads = opts.filter(isHeader)
    expect(heads.map(label).slice(0, 2)).toEqual(['— Рок · Пост-панк —', '— Электроника · Синти-поп —'])
    const items = opts.filter((o) => !isHeader(o))
    const treeIds = presetTree(input).flatMap((f) => f.genres.flatMap((g) => g.items.map((x) => x.id)))
    expect(items.map((o) => o.value)).toEqual(treeIds)
    expect(items.map((o) => o.value)).toEqual([2, 3, 1, 100])
  })

  it('заголовок стоит перед пресетами своего течения', () => {
    const opts = presetOptions(input)
    const iHead = opts.findIndex((o) => isHeader(o) && label(o) === '— Рок · Пост-панк —')
    expect(iHead).toBeGreaterThanOrEqual(0)
    expect(opts.slice(iHead + 1, iHead + 3).map((o) => o.value)).toEqual([2, 3])
    // перед каждым пресетом где-то выше есть заголовок
    expect(isHeader(opts[0])).toBe(true)
  })
})
