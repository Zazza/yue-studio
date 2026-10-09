// ТК110 (карточка internal-own-track, ревью s8b, условие 73а): поиск в выборе пресетов — и по семье
// с течением. У опции может быть поле search (доп. текст для поиска); matchOptions ищет по label + search;
// presetOptions кладёт в search «<семья> <течение>». Написаны по карточке, без чтения реализации.
// Предположение: заголовки presetOptions отличаются disabled (или props.disabled), как в ТК107.
import { describe, it, expect } from 'vitest'
import { matchOptions } from './optionFilter.js'
import { presetOptions } from './soundPresets.js'

const values = (list) => list.map((x) => x.value)
const isHeader = (o) => o.disabled === true || (o.props && o.props.disabled === true)
const p = (id, name, family) => ({ id, name, family, note: '', specs: [] })

describe('matchOptions: поле search (ТК110)', () => {
  it('опция {label X, search «электроника синти-поп»} находится по «электроника»', () => {
    const opts = [{ value: 1, label: 'X', search: 'электроника синти-поп' }, { value: 2, label: 'Y' }]
    expect(values(matchOptions(opts, 'электроника'))).toEqual([1])
    expect(values(matchOptions(opts, 'синти-поп'))).toEqual([1])
  })

  it('слова запроса могут делиться между label и search', () => {
    const opts = [{ value: 1, label: 'Светлый', search: 'Электроника Синти-поп' }]
    expect(values(matchOptions(opts, 'светлый электроника'))).toEqual([1])
    expect(values(matchOptions(opts, 'тёмный электроника'))).toEqual([])
  })

  it('регистр и ё в search — как в label', () => {
    const opts = [{ value: 1, label: 'X', search: 'Тяжёлое Металл' }]
    expect(values(matchOptions(opts, 'ТЯЖЕЛОЕ'))).toEqual([1])
  })

  it('по label находится как раньше, и при наличии search', () => {
    const opts = [{ value: 1, label: 'Пост-панк · холодный', search: 'Рок Пост-панк' }]
    expect(values(matchOptions(opts, 'холодный'))).toEqual([1])
  })

  it('без search — как раньше, только по label', () => {
    const opts = [{ value: 1, label: 'heavy rock' }, { value: 2, label: 'jazz' }, { value: 3, label: null }]
    expect(values(matchOptions(opts, 'rock'))).toEqual([1])
    expect(values(matchOptions(opts, 'undefined'))).toEqual([])
    expect(values(matchOptions(opts, ''))).toEqual([1, 2, 3])
  })

  it('пустой или null search не находит «undefined»/«null»', () => {
    const opts = [{ value: 1, label: 'a', search: null }, { value: 2, label: 'b', search: '' }]
    expect(values(matchOptions(opts, 'null'))).toEqual([])
    expect(values(matchOptions(opts, 'undefined'))).toEqual([])
  })
})

describe('presetOptions: search содержит семью и течение (ТК110)', () => {
  const input = [
    p(1, 'Синти-поп · светлый', 'Электроника'),
    p(2, 'Пост-панк · холодный', 'Рок'),
    p(3, 'Пост-панк · тёплый', 'Рок'),
    p(4, 'Индастриал · жёсткий', 'Электроника'),
    p(5, 'Металл · плотный', 'Тяжёлое'),
    p(100, 'Мой бас', ''),
  ]
  const byValue = (opts) => Object.fromEntries(opts.filter((o) => !isHeader(o)).map((o) => [o.value, o]))

  it('у каждого пресета search содержит его семью и течение', () => {
    const opts = byValue(presetOptions(input))
    const want = { 1: ['Электроника', 'Синти-поп'], 2: ['Рок', 'Пост-панк'], 3: ['Рок', 'Пост-панк'],
      4: ['Электроника', 'Индастриал'], 5: ['Тяжёлое', 'Металл'], 100: ['Мой бас'] }
    for (const [id, parts] of Object.entries(want)) {
      expect(typeof opts[id]?.search, `пресет ${id}`).toBe('string')
      for (const s of parts) expect(opts[id].search, `пресет ${id}`).toContain(s)
    }
  })

  it('поиск «электроника» по presetOptions находит пресеты семьи Электроника', () => {
    const found = matchOptions(presetOptions(input), 'электроника').filter((o) => !isHeader(o))
    expect(values(found).sort((a, b) => a - b)).toEqual([1, 4])
  })

  it('поиск по течению находит все его варианты, по семье+течению — только их', () => {
    const opts = presetOptions(input)
    const items = (q) => values(matchOptions(opts, q).filter((o) => !isHeader(o))).sort((a, b) => a - b)
    expect(items('пост-панк')).toEqual([2, 3])
    expect(items('рок пост-панк')).toEqual([2, 3])
    expect(items('электроника металл')).toEqual([])
  })

  it('пусто → [] и поиск по нему пуст', () => {
    expect(matchOptions(presetOptions([]), 'электроника')).toEqual([])
  })
})
