// Тесты фильтров и пейджера списка треков — по спецификации отбора.
import { describe, it, expect } from 'vitest'
import { defaultJobFilter, filterJobs, pageJobs, pageCount, QUEUE_PAGE_SIZE } from './jobFilter.js'

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
