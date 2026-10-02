// Тесты фильтров и пейджера списка треков — по спецификации отбора.
import { describe, it, expect } from 'vitest'
import { defaultJobFilter, filterJobs, pageJobs, pageCount, QUEUE_PAGE_SIZE, groupJobs, folderNames, DEFAULT_FOLDERS, FOLDER_NONE } from './jobFilter.js'

const NOW = Date.parse('2026-09-28T12:00:00Z')
const j = (over = {}) => ({
  id: 1, title: 'трек', style: 'rock', status: 'done',
  duration_sec: 200, created_at: '2026-09-28T11:00:00Z', draft: false, ...over,
})

describe('фильтр по статусу', () => {
  it('active — очередь и рендер, done — готовые, failed — ошибки и отмены', () => {
    const jobs = [j({ id: 1, status: 'queued' }), j({ id: 2, status: 'running' }),
      j({ id: 3, status: 'done' }), j({ id: 4, status: 'error' }), j({ id: 5, status: 'canceled' })]
    expect(filterJobs(jobs, { ...defaultJobFilter(), status: 'active' }).map(x => x.id)).toEqual([1, 2])
    expect(filterJobs(jobs, { ...defaultJobFilter(), status: 'done' }).map(x => x.id)).toEqual([3])
    expect(filterJobs(jobs, { ...defaultJobFilter(), status: 'failed' }).map(x => x.id)).toEqual([4, 5])
    expect(filterJobs(jobs, defaultJobFilter()).length).toBe(5)
  })
})

describe('фильтр по периоду', () => {
  it('сегодня / 7 дней / 30 дней от now, без даты — не проходит', () => {
    const jobs = [j({ id: 1, created_at: '2026-09-28T10:00:00Z' }),
      j({ id: 2, created_at: '2026-09-25T10:00:00Z' }),
      j({ id: 3, created_at: '2026-08-30T10:00:00Z' }),
      j({ id: 4, created_at: 'битая дата' })]
    expect(filterJobs(jobs, { ...defaultJobFilter(), period: 'today' }, NOW).map(x => x.id)).toEqual([1])
    expect(filterJobs(jobs, { ...defaultJobFilter(), period: 'week' }, NOW).map(x => x.id)).toEqual([1, 2])
    expect(filterJobs(jobs, { ...defaultJobFilter(), period: 'month' }, NOW).map(x => x.id)).toEqual([1, 2, 3])
  })
})

describe('фильтр по длительности', () => {
  it('границы коридоров: 18 с — проба, 2 мин — короткий, 5 мин — средний, 10 мин — длинный', () => {
    const jobs = [j({ id: 1, duration_sec: 18 }), j({ id: 2, duration_sec: 120 }),
      j({ id: 3, duration_sec: 300 }), j({ id: 4, duration_sec: 600 })]
    expect(filterJobs(jobs, { ...defaultJobFilter(), dur: 'draft' }).map(x => x.id)).toEqual([1])
    expect(filterJobs(jobs, { ...defaultJobFilter(), dur: 'short' }).map(x => x.id)).toEqual([2])
    expect(filterJobs(jobs, { ...defaultJobFilter(), dur: 'mid' }).map(x => x.id)).toEqual([3])
    expect(filterJobs(jobs, { ...defaultJobFilter(), dur: 'long' }).map(x => x.id)).toEqual([4])
  })

  it('стыки коридоров: ровно 60 с — уже короткий, 180 — короткий, 360 — средний', () => {
    expect(filterJobs([j({ duration_sec: 59 })], { ...defaultJobFilter(), dur: 'draft' }).length).toBe(1)
    expect(filterJobs([j({ duration_sec: 60 })], { ...defaultJobFilter(), dur: 'draft' }).length).toBe(0)
    expect(filterJobs([j({ duration_sec: 60 })], { ...defaultJobFilter(), dur: 'short' }).length).toBe(1)
    expect(filterJobs([j({ duration_sec: 180 })], { ...defaultJobFilter(), dur: 'short' }).length).toBe(1)
    expect(filterJobs([j({ duration_sec: 180 })], { ...defaultJobFilter(), dur: 'mid' }).length).toBe(0)
    expect(filterJobs([j({ duration_sec: 360 })], { ...defaultJobFilter(), dur: 'mid' }).length).toBe(1)
  })
})

describe('фильтр по черновикам и поиск', () => {
  it('only/hide по флагу draft', () => {
    const jobs = [j({ id: 1, draft: true }), j({ id: 2, draft: false })]
    expect(filterJobs(jobs, { ...defaultJobFilter(), draft: 'only' }).map(x => x.id)).toEqual([1])
    expect(filterJobs(jobs, { ...defaultJobFilter(), draft: 'hide' }).map(x => x.id)).toEqual([2])
  })

  it('поиск без учёта регистра по названию и стилю, с trim', () => {
    const jobs = [j({ id: 1, title: 'Ночной ДОЖДЬ', style: 'synthwave' }),
      j({ id: 2, title: 'утро', style: 'ambient rain' })]
    expect(filterJobs(jobs, { ...defaultJobFilter(), q: ' ДОЖДЬ ' }).map(x => x.id)).toEqual([1])
    expect(filterJobs(jobs, { ...defaultJobFilter(), q: 'RAIN' }).map(x => x.id)).toEqual([2])
    expect(filterJobs(jobs, { ...defaultJobFilter(), q: 'ночного' }).length).toBe(0)
  })
})

describe('пейджер', () => {
  const many = Array.from({ length: 45 }, (_, i) => j({ id: i + 1 }))

  it('страница по 20, последняя — остаток', () => {
    expect(pageJobs(many, 1).length).toBe(QUEUE_PAGE_SIZE)
    expect(pageJobs(many, 2).length).toBe(QUEUE_PAGE_SIZE)
    expect(pageJobs(many, 3).length).toBe(5)
    expect(pageJobs(many, 1)[0].id).toBe(1)
  })

  it('выход за границы зажимается, пустой список — пустая страница', () => {
    expect(pageJobs(many, 99).length).toBe(5)
    expect(pageJobs(many, 0).length).toBe(QUEUE_PAGE_SIZE)
    expect(pageJobs([], 1)).toEqual([])
  })

  it('число страниц: 45 → 3, 0 → 1', () => {
    expect(pageCount(45)).toBe(3)
    expect(pageCount(0)).toBe(1)
  })
})

describe('группировка по версиям (groupJobs)', () => {
  // все треки результата: верхний уровень + вложенные, каждый ровно один раз
  const allIds = g => [...g.top, ...Object.values(g.children).flat()].map(x => x.id).sort((a, b) => a - b)

  it('пустой список → пустые группы', () => {
    expect(groupJobs([])).toEqual({ top: [], children: {} })
  })

  it('производные (parent_id + role) — под корнем, корень с head_id — наверху', () => {
    const jobs = [j({ id: 1, head_id: 3 }),
      j({ id: 2, parent_id: 1, role: 'section' }),
      j({ id: 3, parent_id: 1, role: 'rebuild' }),
      j({ id: 4, parent_id: 1, role: 'variant' })]
    const g = groupJobs(jobs)
    expect(g.top.map(x => x.id)).toEqual([1])
    expect(g.children[1].map(x => x.id).sort()).toEqual([2, 3, 4])
  })

  it('внук по цепочке parent_id попадает в группу корня', () => {
    const jobs = [j({ id: 1 }), j({ id: 2, parent_id: 1, role: 'rebuild' }),
      j({ id: 3, parent_id: 2, role: 'continue' }), j({ id: 4, parent_id: 3, role: 'fragment' })]
    const g = groupJobs(jobs)
    expect(g.top.map(x => x.id)).toEqual([1])
    expect(g.children[1].map(x => x.id).sort()).toEqual([2, 3, 4])
    expect(g.children[2]).toBeUndefined()
    expect(g.children[3]).toBeUndefined()
  })

  it('overdub_of трактуется как родитель', () => {
    const g = groupJobs([j({ id: 1 }), j({ id: 2, overdub_of: 1 })])
    expect(g.top.map(x => x.id)).toEqual([1])
    expect(g.children[1].map(x => x.id)).toEqual([2])
  })

  it('родителя нет в списке (удалён) → трек своей группой наверху, не теряется', () => {
    const g = groupJobs([j({ id: 5, parent_id: 999, role: 'variant' }), j({ id: 6, parent_id: 5, role: 'section' })])
    expect(g.top.map(x => x.id)).toEqual([5])
    expect(g.children[5].map(x => x.id)).toEqual([6])
  })

  it('цикл parent_id (A→B→A) — не зависает, оба трека в результате ровно по разу', () => {
    const g = groupJobs([j({ id: 1, parent_id: 2 }), j({ id: 2, parent_id: 1 })])
    expect(allIds(g)).toEqual([1, 2])
  })

  it('группы по последней активности: свежая версия поднимает старую песню наверх', () => {
    const jobs = [
      j({ id: 1, created_at: '2026-09-20T10:00:00Z' }),                     // старая песня
      j({ id: 2, created_at: '2026-09-27T10:00:00Z' }),                     // новее как корень
      j({ id: 3, parent_id: 1, role: 'rebuild', created_at: '2026-09-28T10:00:00Z' }), // но у старой свежая версия
      j({ id: 4, created_at: '2026-09-25T10:00:00Z' }),
    ]
    expect(groupJobs(jobs).top.map(x => x.id)).toEqual([1, 2, 4])
  })

  it('без производных — просто от свежего к старому', () => {
    const jobs = [j({ id: 1, created_at: '2026-09-20T10:00:00Z' }), j({ id: 2, created_at: '2026-09-28T10:00:00Z' }),
      j({ id: 3, created_at: '2026-09-24T10:00:00Z' })]
    expect(groupJobs(jobs).top.map(x => x.id)).toEqual([2, 3, 1])
  })
})

describe('папки: константы и фильтр по умолчанию', () => {
  it('defaultJobFilter: folder all, остальные поля прежние', () => {
    expect(defaultJobFilter()).toEqual({ status: 'all', period: 'all', dur: 'all', draft: 'all', q: '', folder: 'all' })
  })

  it('DEFAULT_FOLDERS в фиксированном порядке, FOLDER_NONE = "-"', () => {
    expect(DEFAULT_FOLDERS).toEqual(['Альбом', 'Основы', 'Эксперименты'])
    expect(FOLDER_NONE).toBe('-')
  })
})

describe('фильтр по папке (filterJobs)', () => {
  const jobs = () => [
    j({ id: 1, folder: 'Альбом' }),
    j({ id: 2, folder: '  альбом ' }),
    j({ id: 3, folder: 'Основы' }),
    j({ id: 4 }),                       // поля нет
    j({ id: 5, folder: '' }),
    j({ id: 6, folder: '   ' }),
    j({ id: 7, folder: 'Свои' }),
  ]
  const ids = (f) => filterJobs(jobs(), { ...defaultJobFilter(), ...f }).map(x => x.id)

  it('all — не фильтрует по папке', () => {
    expect(ids({ folder: 'all' })).toEqual([1, 2, 3, 4, 5, 6, 7])
  })

  it('поле folder отсутствует в фильтре — не фильтрует (старые сохранённые фильтры)', () => {
    const f = defaultJobFilter()
    delete f.folder
    expect(filterJobs(jobs(), f).map(x => x.id)).toEqual([1, 2, 3, 4, 5, 6, 7])
  })

  it('FOLDER_NONE — только без папки: нет поля, пустая строка, одни пробелы', () => {
    expect(ids({ folder: FOLDER_NONE })).toEqual([4, 5, 6])
  })

  it('имя папки — совпадение без учёта регистра и пробелов по краям', () => {
    expect(ids({ folder: 'Альбом' })).toEqual([1, 2])
    expect(ids({ folder: ' АЛЬБОМ ' })).toEqual([1, 2])
    expect(ids({ folder: 'основы' })).toEqual([3])
  })

  it('несуществующая папка — пусто, частичное совпадение не проходит', () => {
    expect(ids({ folder: 'Нет такой' })).toEqual([])
    expect(ids({ folder: 'Альб' })).toEqual([])
  })

  it('папка комбинируется с остальными фильтрами через И', () => {
    const list = [
      j({ id: 1, folder: 'Альбом', status: 'done', title: 'дождь' }),
      j({ id: 2, folder: 'Альбом', status: 'error', title: 'дождь' }),
      j({ id: 3, folder: 'Основы', status: 'done', title: 'дождь' }),
      j({ id: 4, folder: 'Альбом', status: 'done', title: 'утро' }),
      j({ id: 5, status: 'done', title: 'дождь' }),
    ]
    expect(filterJobs(list, { ...defaultJobFilter(), folder: 'альбом', status: 'done', q: 'дождь' }).map(x => x.id)).toEqual([1])
    expect(filterJobs(list, { ...defaultJobFilter(), folder: FOLDER_NONE, status: 'done' }).map(x => x.id)).toEqual([5])
    expect(filterJobs(list, { ...defaultJobFilter(), folder: FOLDER_NONE, status: 'failed' }).map(x => x.id)).toEqual([])
  })
})

describe('список папок (folderNames)', () => {
  it('пустой/undefined вход — только дефолтные папки', () => {
    expect(folderNames([])).toEqual(['Альбом', 'Основы', 'Эксперименты'])
    expect(folderNames(undefined)).toEqual(['Альбом', 'Основы', 'Эксперименты'])
  })

  it('дефолтные всегда первыми в своём порядке, даже без треков в них', () => {
    expect(folderNames([j({ folder: 'Эксперименты' })])).toEqual(['Альбом', 'Основы', 'Эксперименты'])
  })

  it('свои папки — после дефолтных, по алфавиту (ru), пустые пропускаются', () => {
    const jobs = [j({ id: 1, folder: 'Яблоко' }), j({ id: 2, folder: 'Ёлка' }), j({ id: 3, folder: 'Береза' }),
      j({ id: 4 }), j({ id: 5, folder: '' }), j({ id: 6, folder: '  ' })]
    const expected = ['Яблоко', 'Ёлка', 'Береза'].sort((a, b) => a.localeCompare(b, 'ru'))
    expect(folderNames(jobs)).toEqual(['Альбом', 'Основы', 'Эксперименты', ...expected])
  })

  it('повторы без учёта регистра/пробелов схлопываются в первое написание, обрезанное', () => {
    const jobs = [j({ id: 1, folder: '  Демо ' }), j({ id: 2, folder: 'демо' }), j({ id: 3, folder: 'ДЕМО  ' })]
    expect(folderNames(jobs)).toEqual(['Альбом', 'Основы', 'Эксперименты', 'Демо'])
  })

  it('своя папка, совпадающая с дефолтной без учёта регистра, не дублирует её', () => {
    const jobs = [j({ id: 1, folder: 'альбом' }), j({ id: 2, folder: ' ОСНОВЫ ' })]
    expect(folderNames(jobs)).toEqual(['Альбом', 'Основы', 'Эксперименты'])
  })

  it('вход не мутируется', () => {
    const jobs = [j({ id: 1, folder: 'Зима' }), j({ id: 2, folder: ' альбом ' })]
    const snapshot = JSON.parse(JSON.stringify(jobs))
    folderNames(jobs)
    expect(jobs).toEqual(snapshot)
  })

  it('возвращает новый массив: правка результата не портит DEFAULT_FOLDERS', () => {
    const r = folderNames([])
    r.push('мусор')
    expect(DEFAULT_FOLDERS).toEqual(['Альбом', 'Основы', 'Эксперименты'])
    expect(folderNames([])).toEqual(['Альбом', 'Основы', 'Эксперименты'])
  })
})

describe('поиск: номер песни и версии (filterJobs, f.q)', () => {
  const F = (q) => ({ ...defaultJobFilter(), q })
  const ids = (jobs, q, children) => (children === undefined
    ? filterJobs(jobs, F(q), NOW)
    : filterJobs(jobs, F(q), NOW, children)).map(x => x.id)

  it('подстрока названия или стиля без учёта регистра', () => {
    const jobs = [j({ id: 1, title: 'Ночной Дождь', style: 'synth' }), j({ id: 2, title: 'утро', style: 'Ambient' })]
    expect(ids(jobs, 'дождь')).toEqual([1])
    expect(ids(jobs, 'AMBIENT')).toEqual([2])
    expect(ids(jobs, 'нет такого')).toEqual([])
  })

  it('«425» и «#425» находят песню с id 425 точно: не 4250 и не 42', () => {
    const jobs = [j({ id: 42 }), j({ id: 425 }), j({ id: 4250 })]
    expect(ids(jobs, '425')).toEqual([425])
    expect(ids(jobs, '#425')).toEqual([425])
    expect(ids(jobs, '42')).toEqual([42])
  })

  it('число в названии/стиле находит песню подстрокой, даже если id другой', () => {
    const jobs = [j({ id: 1, title: 'демо 425 bpm' }), j({ id: 2, style: 'lofi425' }), j({ id: 3 })]
    expect(ids(jobs, '425')).toEqual([1, 2])
  })

  it('несуществующий номер — пусто', () => {
    expect(ids([j({ id: 1 }), j({ id: 2 })], '#999')).toEqual([])
  })

  it('песня проходит, если совпала её версия по номеру; в результате только песни верхнего уровня', () => {
    const jobs = [j({ id: 1 }), j({ id: 2 })]
    const children = { 1: [{ id: 10, title: 'версия', style: 'rock' }, { id: 11, title: 'ещё', style: 'rock' }] }
    expect(ids(jobs, '11', children)).toEqual([1])
    expect(ids(jobs, '#10', children)).toEqual([1])
    expect(ids(jobs, '1', children)).toEqual([1])   // сама песня 1; версии не добавляются
  })

  it('песня проходит, если совпала подстрока названия или стиля версии', () => {
    const jobs = [j({ id: 1, title: 'основа' }), j({ id: 2, title: 'другая' })]
    const children = { 2: [{ id: 20, title: 'Финал с ГИТАРОЙ', style: 'rock' }, { id: 21, title: 'x', style: 'Blues' }] }
    expect(ids(jobs, 'гитарой', children)).toEqual([2])
    expect(ids(jobs, 'blues', children)).toEqual([2])
    expect(ids(jobs, 'нет такого', children)).toEqual([])
  })

  it('номер версии ищется точно: 20 не находит версию 200', () => {
    const jobs = [j({ id: 1 }), j({ id: 2 })]
    const children = { 1: [{ id: 200, title: 'в', style: 'rock' }] }
    expect(ids(jobs, '20', children)).toEqual([])
  })

  it('children не передан — поиск только по самой песне', () => {
    const jobs = [j({ id: 1 }), j({ id: 2, title: 'гитара' })]
    expect(ids(jobs, '10')).toEqual([])
    expect(ids(jobs, '#1')).toEqual([1])
    expect(ids(jobs, 'гитара')).toEqual([2])
  })

  it('поиск по версии комбинируется со статусом через И', () => {
    const jobs = [j({ id: 1, status: 'done' }), j({ id: 2, status: 'error' })]
    const children = { 1: [{ id: 30, title: 'в', style: 'rock' }], 2: [{ id: 31, title: 'в', style: 'rock' }] }
    const f = (q) => ({ ...defaultJobFilter(), status: 'done', q })
    expect(filterJobs(jobs, f('31'), NOW, children).map(x => x.id)).toEqual([])
    expect(filterJobs(jobs, f('#30'), NOW, children).map(x => x.id)).toEqual([1])
  })

  it('пустой q или одни пробелы — не фильтрует', () => {
    const jobs = [j({ id: 1 }), j({ id: 2 }), j({ id: 3 })]
    const children = { 1: [{ id: 10, title: 'в', style: 'rock' }] }
    expect(ids(jobs, '')).toEqual([1, 2, 3])
    expect(ids(jobs, '   ', children)).toEqual([1, 2, 3])
  })
})
