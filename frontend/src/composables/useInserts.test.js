/* global process */
// Тесты сервиса вклеек (useInserts) по контракту: очередь ожидающих спек,
// реестр применённых вклеек, пересборки строго по одной, зажим громкости,
// перенос вклеек на новую версию трека (carryTo).
// Модуль — синглтон: читает localStorage и заводит setInterval при импорте,
// поэтому каждый тест импортирует его заново после vi.resetModules().
import { describe, it, expect, vi, afterEach } from 'vitest'
import { unref } from 'vue'

// Мок внешней границы — API воркера. Объект общий, поля подменяются в каждом тесте.
const apiMock = vi.hoisted(() => ({}))
vi.mock('../api.js', () => ({ api: apiMock }))

// node-окружение vitest без DOM: минимальный localStorage
class MemStorage {
  constructor() { this.m = new Map() }
  getItem(k) { return this.m.has(k) ? this.m.get(k) : null }
  setItem(k, v) { this.m.set(k, String(v)) }
  removeItem(k) { this.m.delete(k) }
  clear() { this.m.clear() }
}

// Управляемый промис: пересборка «висит», пока тест не разрешит её вручную.
function deferred() {
  let resolve, reject
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

// Прогнать микрозадачи (цепочки await внутри сервиса) без сдвига времени.
async function flush() {
  for (let i = 0; i < 10; i++) await vi.advanceTimersByTimeAsync(0)
}

// Отчёт воркера о пересборке для набора спек.
function report(specs, extra = {}) {
  return {
    variant: { file: 'out.wav' },
    inserts: specs.map(s => ({ child_id: s.child_id, aligned: true, score: 0.9, start_sec: s.from, gain: 1, ...(extra[s.child_id] || {}) })),
  }
}

const A = { childId: 1, instId: 'i-a', from: 0, to: 10, lead: 0, beat: 0.5, db: -6 }
const specB = { parent: 'P', childId: 2, instId: 'i-b', from: 5, to: 15, lead: 0.1, beat: 0.5, db: -6, srcJob: 'P' }

// Подготовить окружение и импортировать свежий экземпляр модуля.
async function load({ queue = [], applied = {}, jobs = [], rebuildSections }) {
  vi.resetModules()
  globalThis.localStorage = new MemStorage()
  localStorage.setItem('yue_insert_queue', JSON.stringify(queue))
  localStorage.setItem('yue_insert_applied', JSON.stringify(applied))
  vi.useFakeTimers()
  apiMock.jobs = vi.fn(async () => jobs)
  apiMock.rebuildSections = rebuildSections || vi.fn(async (_p, specs) => report(specs))
  // старый контракт: если сервис его вызовет, тест это увидит
  apiMock.rebuildInserts = vi.fn(async (_p, specs) => report(specs))
  const mod = await import('./useInserts.js')
  return mod.useInserts()
}

const pendingOf = ins => unref(ins.pending) || []
const childIds = list => list.map(x => x.childId).sort()

afterEach(() => {
  vi.clearAllTimers()
  vi.useRealTimers()
})

describe('гонка пересборок', () => {
  it('вклейка, добавленная тиком во время чужой пересборки, не пропадает, вторая пересборка шлёт обе', async () => {
    const calls = []
    const rebuildSections = vi.fn((parent, specs) => {
      const d = deferred()
      calls.push({ parent, specs: JSON.parse(JSON.stringify(specs)), d })
      return d.promise
    })
    const ins = await load({
      applied: { P: [A] },
      queue: [specB],
      jobs: [{ id: 2, status: 'done' }],
      rebuildSections,
    })

    // первая пересборка (от setDb) повисла
    ins.setDb('P', 1, -12)
    await flush()
    expect(calls.length).toBe(1)

    // тик: ребёнок 2 готов → вклейка B в реестр, пересборка встаёт в очередь
    await vi.advanceTimersByTimeAsync(3000)
    // строго по одной: пока первая висит, вторая не стартовала
    expect(calls.length).toBe(1)

    calls[0].d.resolve(report(calls[0].specs))
    await flush()
    expect(calls.length).toBe(2)
    expect(calls[1].specs.map(s => s.child_id).sort()).toEqual([1, 2])

    calls[1].d.resolve(report(calls[1].specs))
    await flush()

    const reg = ins.appliedFor('P')
    expect(childIds(reg)).toEqual([1, 2])
    expect(reg.find(x => x.childId === 1).db).toBe(-12)
    expect(pendingOf(ins)).toEqual([])
  })
})

describe('setDb', () => {
  it('зажимает снизу до −24: в api и в реестре', async () => {
    const ins = await load({ applied: { P: [A] } })
    await ins.setDb('P', 1, -40)
    await flush()
    const call = apiMock.rebuildSections.mock.calls.at(-1)
    expect(call[0]).toBe('P')
    expect(call[1].find(s => s.child_id === 1).db).toBe(-24)
    expect(ins.appliedFor('P')[0].db).toBe(-24)
  })

  it('зажимает сверху до +12', async () => {
    const ins = await load({ applied: { P: [A] } })
    await ins.setDb('P', 1, 20)
    await flush()
    expect(apiMock.rebuildSections.mock.calls.at(-1)[1][0].db).toBe(12)
    expect(ins.appliedFor('P')[0].db).toBe(12)
  })

  it('значение внутри диапазона не меняется (граница 0)', async () => {
    const ins = await load({ applied: { P: [A] } })
    await ins.setDb('P', 1, 0)
    await flush()
    expect(ins.appliedFor('P')[0].db).toBe(0)
  })
})

describe('отчёт пересборки', () => {
  it('aligned/score из отчёта записываются в вклейку реестра', async () => {
    const rebuildSections = vi.fn(async () => ({
      variant: { file: 'out.wav' },
      inserts: [{ child_id: 1, aligned: false, score: 0, start_sec: 0, gain: 1 }],
    }))
    const ins = await load({ applied: { P: [A] }, rebuildSections })
    await ins.rebuild('P')
    await flush()
    const reg = ins.appliedFor('P')
    expect(reg[0].aligned).toBe(false)
    expect(reg[0].score).toBe(0)
  })

  it('пустой реестр неизвестного трека → appliedFor даёт пустой список', async () => {
    const ins = await load({})
    expect(ins.appliedFor('нет-такого') || []).toEqual([])
  })
})

describe('tick', () => {
  it('ребёнок ещё не готов → вклейки нет, пересборки нет, спека в очереди', async () => {
    const ins = await load({ queue: [specB], jobs: [{ id: 2, status: 'running' }] })
    await vi.advanceTimersByTimeAsync(3000)
    await flush()
    expect(ins.appliedFor('P') || []).toEqual([])
    expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    expect(childIds(pendingOf(ins))).toEqual([2])
  })

  it('ребёнок готов → вклейка в реестре, трек пересобран, спека ушла из очереди', async () => {
    const ins = await load({ queue: [specB], jobs: [{ id: 2, status: 'done' }] })
    await vi.advanceTimersByTimeAsync(3000)
    await flush()
    expect(childIds(ins.appliedFor('P'))).toEqual([2])
    expect(apiMock.rebuildSections).toHaveBeenCalledTimes(1)
    expect(apiMock.rebuildSections.mock.calls[0][0]).toBe('P')
    expect(pendingOf(ins)).toEqual([])
  })

  it('ошибка пересборки не теряет вклейку: она в реестре, спека в очереди, повтор на следующем тике', async () => {
    let fail = true
    const rebuildSections = vi.fn(async (_p, specs) => {
      if (fail) throw new Error('воркер недоступен')
      return report(specs)
    })
    const ins = await load({ queue: [specB], jobs: [{ id: 2, status: 'done' }], rebuildSections })
    await vi.advanceTimersByTimeAsync(3000)
    await flush()
    expect(rebuildSections).toHaveBeenCalledTimes(1)
    expect(childIds(ins.appliedFor('P'))).toEqual([2])
    expect(childIds(pendingOf(ins))).toEqual([2])

    // следующий тик — повтор, теперь успешный
    fail = false
    await vi.advanceTimersByTimeAsync(3000)
    await flush()
    expect(rebuildSections.mock.calls.length).toBeGreaterThanOrEqual(2)
    expect(childIds(ins.appliedFor('P'))).toEqual([2])
    expect(pendingOf(ins)).toEqual([])
  })
})

describe('carryTo', () => {
  it('переносит вклейки реестра (с текущим db) и ожидающие спеки', async () => {
    const specC = { parent: 'P', childId: 3, instId: 'i-c', from: 20, to: 30, lead: 0, beat: 0.5, db: -3, srcJob: 'P' }
    const ins = await load({
      applied: { P: [A] },
      queue: [specC],
      jobs: [{ id: 3, status: 'running' }],
    })
    await ins.setDb('P', 1, -18)
    await flush()

    ins.carryTo('P', 'Q', 'P')
    await flush()

    const forQ = pendingOf(ins).filter(s => s.parent === 'Q')
    const c1 = forQ.find(s => s.childId === 1)
    expect(c1).toBeTruthy()
    expect(c1).toMatchObject({ instId: 'i-a', from: 0, to: 10, lead: 0, beat: 0.5, db: -18, srcJob: 'P' })
    const c3 = forQ.find(s => s.childId === 3)
    expect(c3).toMatchObject({ from: 20, to: 30, lead: 0, beat: 0.5, db: -3, srcJob: 'P' })
  })

  it('у трека нет ни вклеек, ни спек → в очереди ничего для нового трека', async () => {
    const ins = await load({})
    ins.carryTo('P', 'Q', 'P')
    await flush()
    expect(pendingOf(ins).filter(s => s.parent === 'Q')).toEqual([])
  })
})

// Спека вклейки A в очереди (та же вклейка, что уже в реестре P)
const specA = { parent: 'P', childId: 1, instId: 'i-a', from: 0, to: 10, lead: 0, beat: 0.5, db: -6, srcJob: 'P' }
const B = { childId: 2, instId: 'i-b', from: 5, to: 15, lead: 0.1, beat: 0.5, db: -6 }

describe('повторное попадание вклейки в реестр', () => {
  it('спека уже применённой вклейки не сбрасывает db из setDb и не меняет порядок реестра', async () => {
    let n = 0
    const rebuildSections = vi.fn(async (_p, specs) => {
      n++
      if (n === 1) throw new Error('воркер недоступен')
      return report(specs)
    })
    const ins = await load({
      applied: { P: [A, B] },
      queue: [specA],
      jobs: [{ id: 1, status: 'done' }, { id: 2, status: 'done' }],
      rebuildSections,
    })

    // первый тик: пересборка падает, спека A остаётся в очереди
    await vi.advanceTimersByTimeAsync(3000)
    await flush()
    expect(rebuildSections).toHaveBeenCalledTimes(1)

    // пользователь меняет громкость — пересборка успешна
    await ins.setDb('P', 1, -12)
    await flush()

    // следующий тик снова обрабатывает спеку A (db −6 в спеке)
    await vi.advanceTimersByTimeAsync(3000)
    await flush()

    const reg = ins.appliedFor('P')
    expect(reg.map(x => x.childId)).toEqual([1, 2])
    expect(reg.find(x => x.childId === 1).db).toBe(-12)
    const last = rebuildSections.mock.calls.at(-1)
    expect(last[1].find(s => s.child_id === 1).db).toBe(-12)
  })
})

describe('carryTo: вклейка и в реестре, и в очереди', () => {
  it('в очередь нового трека попадает одна спека на childId, с db из реестра', async () => {
    const specC = { parent: 'P', childId: 3, instId: 'i-c', from: 20, to: 30, lead: 0, beat: 0.5, db: -3, srcJob: 'P' }
    const ins = await load({
      applied: { P: [A] },
      queue: [specA, specC],
      jobs: [{ id: 1, status: 'running' }, { id: 3, status: 'running' }],
    })
    await ins.setDb('P', 1, -18)
    await flush()

    ins.carryTo('P', 'Q', 'P')
    await flush()

    const forQ = pendingOf(ins).filter(s => s.parent === 'Q')
    const c1 = forQ.filter(s => s.childId === 1)
    expect(c1.length).toBe(1)
    expect(c1[0].db).toBe(-18)
    expect(forQ.filter(s => s.childId === 3).length).toBe(1)
  })
})

describe('flush', () => {
  it('без сдвига таймеров обрабатывает очередь: готовая вклейка в реестре и в пересборке', async () => {
    const ins = await load({ queue: [specB], jobs: [{ id: 2, status: 'done' }] })
    await ins.flush()
    expect(childIds(ins.appliedFor('P'))).toEqual([2])
    expect(apiMock.rebuildSections).toHaveBeenCalled()
    const call = apiMock.rebuildSections.mock.calls.at(-1)
    expect(call[0]).toBe('P')
    expect(call[1].map(s => s.child_id)).toContain(2)
    expect(pendingOf(ins)).toEqual([])
  })

  it('резолвится только после завершения пересборки', async () => {
    const d = deferred()
    const rebuildSections = vi.fn(() => d.promise)
    const ins = await load({ queue: [specB], jobs: [{ id: 2, status: 'done' }], rebuildSections })
    let done = false
    const p = ins.flush().then(() => { done = true })
    await flush()
    expect(rebuildSections).toHaveBeenCalledTimes(1)
    expect(done).toBe(false)
    d.resolve(report(rebuildSections.mock.calls[0][1]))
    await p
    expect(done).toBe(true)
  })

  it('пустая очередь → резолвится без пересборок', async () => {
    const ins = await load({})
    await ins.flush()
    expect(apiMock.rebuildSections).not.toHaveBeenCalled()
  })
})

describe('latestFile', () => {
  it('имя файла по childId последней вклейки реестра', async () => {
    const ins = await load({ applied: { P: [A, B] } })
    expect(ins.latestFile('P')).toBe('overdub-inst-2.flac')
  })

  it('пустой реестр → null', async () => {
    const ins = await load({ applied: { P: [] } })
    expect(ins.latestFile('P')).toBeNull()
    expect(ins.latestFile('нет-такого')).toBeNull()
  })
})

describe('flush: ошибка списка джоб и перекрытие с проходом по таймеру', () => {
  it('api.jobs отклонён в проходе от flush → flush отклоняется той же ошибкой, реестр и очередь не меняются', async () => {
    const ins = await load({ queue: [specB] })
    apiMock.jobs.mockImplementation(async () => { throw new Error('net') })
    await expect(ins.flush()).rejects.toThrow('net')
    expect(ins.appliedFor('P') || []).toEqual([])
    expect(childIds(pendingOf(ins))).toEqual([2])
    expect(apiMock.rebuildSections).not.toHaveBeenCalled()
  })

  it('api.jobs отклонён в проходе по таймеру → нет необработанного отклонения, очередь и реестр не меняются', async () => {
    const unhandled = []
    const onUnhandled = reason => { unhandled.push(reason) }
    process.on('unhandledRejection', onUnhandled)
    try {
      const ins = await load({ queue: [specB] })
      apiMock.jobs.mockImplementation(async () => { throw new Error('net') })
      await vi.advanceTimersByTimeAsync(3000)
      await flush()
      expect(apiMock.jobs).toHaveBeenCalled()
      // событие unhandledRejection node шлёт после макрозадачи — дать ей пройти на реальных таймерах
      vi.useRealTimers()
      await new Promise(r => setTimeout(r, 20))
      expect(unhandled).toEqual([])
      expect(ins.appliedFor('P') || []).toEqual([])
      expect(childIds(pendingOf(ins))).toEqual([2])
      expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    } finally {
      process.off('unhandledRejection', onUnhandled)
    }
  })

  it('flush во время прохода по таймеру дожидается его и делает ещё один проход', async () => {
    const first = deferred()
    const ins = await load({ queue: [specB] })
    let n = 0
    apiMock.jobs.mockImplementation(() => {
      n++
      if (n === 1) return first.promise
      return Promise.resolve([{ id: 2, status: 'done' }])
    })

    // проход по таймеру повис на первом api.jobs
    await vi.advanceTimersByTimeAsync(3000)
    expect(apiMock.jobs).toHaveBeenCalledTimes(1)

    let done = false
    const p = ins.flush().then(() => { done = true })
    await flush()
    expect(done).toBe(false)

    // первый проход видит ребёнка ещё в работе
    first.resolve([{ id: 2, status: 'running' }])
    await p

    expect(done).toBe(true)
    expect(apiMock.jobs).toHaveBeenCalledTimes(2)
    expect(childIds(ins.appliedFor('P'))).toEqual([2])
    expect(apiMock.rebuildSections).toHaveBeenCalled()
    expect(apiMock.rebuildSections.mock.calls.at(-1)[0]).toBe('P')
  })
})

// ---- замена дорожек по стемам: stems/fadeIn/fadeOut едут до api.rebuildSections ----

describe('пересборка по стемам (rebuildSections)', () => {
  const specS = {
    parent: 'P', childId: 5, instId: 'bass', from: 8, to: 16, lead: 2, beat: 0.5, db: -3, srcJob: 'P',
    stems: ['bass'], fadeIn: 0.25, fadeOut: 0.5,
  }

  it('stems/fadeIn/fadeOut спеки из очереди доходят до api вместе с окном', async () => {
    const ins = await load({ queue: [specS], jobs: [{ id: 5, status: 'done' }] })
    await ins.flush()
    expect(apiMock.rebuildSections).toHaveBeenCalled()
    const [parent, specs] = apiMock.rebuildSections.mock.calls.at(-1)
    expect(parent).toBe('P')
    const s = specs.find(x => x.child_id === 5)
    expect(s).toEqual({
      child_id: 5, from: 8, to: 16, lead: 2, beat_sec: 0.5, db: -3,
      stems: ['bass'], fade_in: 0.25, fade_out: 0.5, keep_high_hz: 0,
    })
    expect(apiMock.rebuildInserts).not.toHaveBeenCalled()
  })

  it('вклейка реестра хранит stems/fadeIn/fadeOut: они же уходят в api при setDb', async () => {
    const S = { childId: 5, instId: 'drumfill', from: 8, to: 16, lead: 2, beat: 0.5, db: -6, stems: ['drums'], fadeIn: 0.1, fadeOut: 0.2 }
    const ins = await load({ applied: { P: [S] } })
    await ins.setDb('P', 5, -3)
    await flush()
    const s = apiMock.rebuildSections.mock.calls.at(-1)[1].find(x => x.child_id === 5)
    expect(s).toMatchObject({ stems: ['drums'], fade_in: 0.1, fade_out: 0.2, db: -3 })
    expect(ins.appliedFor('P')[0]).toMatchObject({ stems: ['drums'], fadeIn: 0.1, fadeOut: 0.2 })
  })

  it('carryTo переносит stems/fadeIn/fadeOut на новую версию трека', async () => {
    const S = { childId: 5, instId: 'buildup', from: 8, to: 16, lead: 2, beat: 0.5, db: -6, stems: ['drums', 'bass', 'other'], fadeIn: 0.1, fadeOut: 0.2 }
    const ins = await load({ applied: { P: [S] } })
    ins.carryTo('P', 'Q', 'P')
    await flush()
    const c = pendingOf(ins).find(s => s.parent === 'Q' && s.childId === 5)
    expect(c).toMatchObject({ stems: ['drums', 'bass', 'other'], fadeIn: 0.1, fadeOut: 0.2 })
  })
})

// ---- сбивка: у старых барабанов вычитается только низ (keepHighHz → keep_high_hz) ----

describe('keepHighHz вклейки доходит до api (keep_high_hz)', () => {
  const specK = {
    parent: 'P', childId: 6, instId: 'drumfill', from: 8, to: 16, lead: 2, beat: 0.5, db: 0, srcJob: 'P',
    stems: ['drums'], fadeIn: 0.1, fadeOut: 0.2, keepHighHz: 6000,
  }

  it('keepHighHz спеки из очереди уходит в api как keep_high_hz вместе с остальными полями', async () => {
    const ins = await load({ queue: [specK], jobs: [{ id: 6, status: 'done' }] })
    await ins.flush()
    expect(apiMock.rebuildSections).toHaveBeenCalled()
    const [parent, specs] = apiMock.rebuildSections.mock.calls.at(-1)
    expect(parent).toBe('P')
    expect(specs.find(x => x.child_id === 6)).toEqual({
      child_id: 6, from: 8, to: 16, lead: 2, beat_sec: 0.5, db: 0,
      stems: ['drums'], fade_in: 0.1, fade_out: 0.2, keep_high_hz: 6000,
    })
  })

  it('спека без keepHighHz → keep_high_hz: 0', async () => {
    const noKeep = { ...specK }
    delete noKeep.keepHighHz
    const ins = await load({ queue: [noKeep], jobs: [{ id: 6, status: 'done' }] })
    await ins.flush()
    const s = apiMock.rebuildSections.mock.calls.at(-1)[1].find(x => x.child_id === 6)
    expect(s.keep_high_hz).toBe(0)
  })

  it('вклейка реестра хранит keepHighHz: он же уходит в api при setDb', async () => {
    const K = { childId: 6, instId: 'drumfill', from: 8, to: 16, lead: 2, beat: 0.5, db: -6, stems: ['drums'], fadeIn: 0.1, fadeOut: 0.2, keepHighHz: 6000 }
    const ins = await load({ applied: { P: [K] } })
    await ins.setDb('P', 6, -3)
    await flush()
    const s = apiMock.rebuildSections.mock.calls.at(-1)[1].find(x => x.child_id === 6)
    expect(s).toMatchObject({ keep_high_hz: 6000, db: -3 })
    expect(ins.appliedFor('P')[0]).toMatchObject({ keepHighHz: 6000 })
  })

  it('спека из очереди попадает в реестр вместе с keepHighHz', async () => {
    const ins = await load({ queue: [specK], jobs: [{ id: 6, status: 'done' }] })
    await ins.flush()
    expect(ins.appliedFor('P').find(x => x.childId === 6)).toMatchObject({ keepHighHz: 6000 })
  })

  it('carryTo переносит keepHighHz на новую версию трека', async () => {
    const K = { childId: 6, instId: 'drumfill', from: 8, to: 16, lead: 2, beat: 0.5, db: -6, stems: ['drums'], fadeIn: 0.1, fadeOut: 0.2, keepHighHz: 6000 }
    const ins = await load({ applied: { P: [K] } })
    ins.carryTo('P', 'Q', 'P')
    await flush()
    const c = pendingOf(ins).find(s => s.parent === 'Q' && s.childId === 6)
    expect(c).toMatchObject({ keepHighHz: 6000 })
  })
})

describe('addMutes — громкость дорожек без рендера', () => {
  const mute = (from, to, db, stems = ['other']) => ({ instId: 'drumsolo', from, to, stems, db })
  const lastCall = () => apiMock.rebuildSections.mock.calls.at(-1)[1]

  it('записи в реестре с уникальными отрицательными childId, сразу одна пересборка', async () => {
    const ins = await load({ applied: { P: [A] } })
    await ins.addMutes('P', [mute(0, 5, -100), mute(10, 15, -100)])
    await flush()
    const reg = ins.appliedFor('P')
    const mutes = reg.filter(x => x.childId <= 0)
    expect(mutes.length).toBe(2)
    for (const m of mutes) expect(m.childId).toBeLessThan(0)
    expect(new Set(mutes.map(x => x.childId)).size).toBe(2)
    expect(reg.some(x => x.childId === 1)).toBe(true)   // вклейка A не потерялась
    expect(apiMock.rebuildSections).toHaveBeenCalledTimes(1)
    expect(lastCall().length).toBe(3)
  })

  it('два вызова подряд (время не сдвигается) — childId всё равно не совпадают', async () => {
    const ins = await load({})
    await ins.addMutes('P', [mute(0, 5, -100)])
    await ins.addMutes('P', [mute(10, 15, -100)])
    await flush()
    const ids = ins.appliedFor('P').map(x => x.childId)
    expect(ids.length).toBe(2)
    expect(new Set(ids).size).toBe(2)
    for (const id of ids) expect(id).toBeLessThan(0)
  })

  it('в api у записи громкости child_id = 0, окно и дорожки доходят', async () => {
    const ins = await load({})
    await ins.addMutes('P', [mute(8, 16, -100, ['other', 'bass'])])
    await flush()
    const s = lastCall()[0]
    expect(s.child_id).toBe(0)
    expect(s).toMatchObject({ from: 8, to: 16, stems: ['other', 'bass'] })
  })

  it('db у записей громкости не зажимается: 0 / −60 / −100 уходят как есть', async () => {
    const ins = await load({})
    await ins.addMutes('P', [mute(0, 2, 0), mute(4, 6, -60), mute(8, 10, -100)])
    await flush()
    const byFrom = Object.fromEntries(lastCall().map(s => [s.from, s.db]))
    expect(byFrom).toEqual({ 0: 0, 4: -60, 8: -100 })
    const reg = Object.fromEntries(ins.appliedFor('P').map(x => [x.from, x.db]))
    expect(reg).toEqual({ 0: 0, 4: -60, 8: -100 })
  })

  it('в той же пересборке обычная вклейка (childId > 0) зажимается в −24…+12', async () => {
    const ins = await load({ applied: { P: [{ ...A, db: -60 }] } })
    await ins.addMutes('P', [mute(20, 25, -60)])
    await flush()
    const call = lastCall()
    expect(call.find(s => s.child_id === 1).db).toBe(-24)
    expect(call.find(s => s.child_id === 0).db).toBe(-60)
  })

  it('latestFile не именует файл по записи громкости', async () => {
    const ins = await load({ applied: { P: [{ ...A, childId: 2 }] } })
    await ins.addMutes('P', [mute(0, 5, -100)])
    await flush()
    expect(ins.latestFile('P')).toBe('overdub-inst-2.flac')
  })

  it('в реестре только записи громкости → latestFile = null', async () => {
    const ins = await load({})
    await ins.addMutes('P', [mute(0, 5, -100)])
    await flush()
    expect(ins.latestFile('P')).toBeNull()
  })
})

describe('selectAlt — другой вариант вклейки', () => {
  it('запись меняет childId на выбранный вариант, трек пересобирается с ним', async () => {
    const ins = await load({ applied: { P: [{ ...A, alts: [7] }] } })
    await ins.selectAlt('P', 1, 7)
    await flush()
    const reg = ins.appliedFor('P')
    expect(reg.length).toBe(1)
    expect(reg[0].childId).toBe(7)
    expect(reg[0]).toMatchObject({ instId: 'i-a', from: 0, to: 10 })
    expect(apiMock.rebuildSections).toHaveBeenCalled()
    expect(apiMock.rebuildSections.mock.calls.at(-1)[1].map(s => s.child_id)).toEqual([7])
    expect(ins.latestFile('P')).toBe('overdub-inst-7.flac')
  })

  it('вклейки нет в реестре → реестр не меняется, пересборки нет', async () => {
    const ins = await load({ applied: { P: [A] } })
    await ins.selectAlt('P', 99, 7)
    await flush()
    expect(childIds(ins.appliedFor('P'))).toEqual([1])
    expect(apiMock.rebuildSections).not.toHaveBeenCalled()
  })
})

describe('addStemFx — эффект на дорожку через пересборку', () => {
  const fx = (over = {}) => ({
    stem: 'vocals', chain: 'soften', params: { strength: 0.6, freq: 6000 }, from: 176, to: 0, ...over,
  })
  const lastCall = () => apiMock.rebuildSections.mock.calls.at(-1)[1]
  const fxSpec = specs => specs.find(s => s.chain)

  it('запись в реестре с отрицательным childId, сразу пересборка', async () => {
    const ins = await load({ applied: { P: [A] } })
    await ins.addStemFx('P', fx())
    await flush()
    const reg = ins.appliedFor('P')
    const recs = reg.filter(x => x.childId < 0)
    expect(recs.length).toBe(1)
    expect(reg.some(x => x.childId === 1)).toBe(true)   // вклейка A не потерялась
    expect(apiMock.rebuildSections).toHaveBeenCalledTimes(1)
    expect(apiMock.rebuildSections.mock.calls.at(-1)[0]).toBe('P')
    expect(lastCall().length).toBe(2)
  })

  it('в api уходит child_id 0, stems [stem], chain, params, окно; to 0 — как есть (до конца)', async () => {
    const ins = await load({})
    await ins.addStemFx('P', fx())
    await flush()
    const s = lastCall()[0]
    expect(s.child_id).toBe(0)
    expect(s.stems).toEqual(['vocals'])
    expect(s.chain).toBe('soften')
    expect(s.params).toEqual({ strength: 0.6, freq: 6000 })
    expect(s.from).toBe(176)
    expect(s.to).toBe(0)
  })

  it('окно с концом передаётся без изменений, стем — любой из дорожек', async () => {
    const ins = await load({})
    await ins.addStemFx('P', fx({ stem: 'drums', chain: 'dewhistle', params: { freqs: [2638, 3628] }, from: 2, to: 4 }))
    await flush()
    expect(lastCall()[0]).toMatchObject({
      child_id: 0, stems: ['drums'], chain: 'dewhistle', params: { freqs: [2638, 3628] }, from: 2, to: 4,
    })
  })

  it('db записи эффекта 0: в api и в реестре (громкость не меняется)', async () => {
    const ins = await load({})
    await ins.addStemFx('P', fx())
    await flush()
    expect(lastCall()[0].db).toBe(0)
    expect(ins.appliedFor('P').find(x => x.childId < 0).db).toBe(0)
  })

  it('два эффекта подряд и заглушка — childId уникальны и отрицательны', async () => {
    const ins = await load({})
    await ins.addStemFx('P', fx())
    await ins.addStemFx('P', fx({ chain: 'dewhistle', params: { freqs: [2638] } }))
    await ins.addMutes('P', [{ instId: 'm', from: 0, to: 5, stems: ['other'], db: -100 }])
    await flush()
    const ids = ins.appliedFor('P').map(x => x.childId)
    expect(ids.length).toBe(3)
    expect(new Set(ids).size).toBe(3)
    for (const id of ids) expect(id).toBeLessThan(0)
  })

  it('повторная пересборка (addMutes) снова передаёт chain/params эффекта, заглушка — без chain', async () => {
    const ins = await load({})
    await ins.addStemFx('P', fx())
    await flush()
    await ins.addMutes('P', [{ instId: 'm', from: 10, to: 20, stems: ['other'], db: -100 }])
    await flush()
    expect(apiMock.rebuildSections).toHaveBeenCalledTimes(2)
    const call = lastCall()
    expect(call.length).toBe(2)
    const s = fxSpec(call)
    expect(s).toMatchObject({ child_id: 0, stems: ['vocals'], chain: 'soften', params: { strength: 0.6, freq: 6000 }, from: 176, to: 0, db: 0 })
    const mute = call.find(x => x !== s)
    expect(mute.chain || '').toBe('')
    expect(mute.db).toBe(-100)
  })

  it('повторная пересборка через setDb обычной вклейки тоже сохраняет эффект; вклейка зажимается', async () => {
    const ins = await load({ applied: { P: [A] } })
    await ins.addStemFx('P', fx())
    await flush()
    await ins.setDb('P', 1, -40)
    await flush()
    const call = lastCall()
    expect(call.find(s => s.child_id === 1).db).toBe(-24)
    expect(fxSpec(call)).toMatchObject({ child_id: 0, chain: 'soften', params: { strength: 0.6, freq: 6000 }, db: 0 })
  })

  it('latestFile не именует файл по записи эффекта', async () => {
    const ins = await load({ applied: { P: [{ ...A, childId: 2 }] } })
    await ins.addStemFx('P', fx())
    await flush()
    expect(ins.latestFile('P')).toBe('overdub-inst-2.flac')
  })

  it('в реестре только эффект → latestFile = null', async () => {
    const ins = await load({})
    await ins.addStemFx('P', fx())
    await flush()
    expect(ins.latestFile('P')).toBeNull()
  })
})

describe('addStemEnvelope — громкость по волне на дорожку через пересборку', () => {
  const envV = [{ t: 1, db: 0 }, { t: 3, db: -12 }]
  const envV2 = [{ t: 2, db: -6 }]
  const envD = [{ t: 0, db: 3 }, { t: 4, db: -3 }]
  const lastCall = () => apiMock.rebuildSections.mock.calls.at(-1)[1]
  const envSpecs = specs => specs.filter(s => Array.isArray(s.envelope) && s.envelope.length)

  it('запись в реестре с отрицательным childId, вклейки не теряются, сразу одна пересборка', async () => {
    const ins = await load({ applied: { P: [A] } })
    await ins.addStemEnvelope('P', { stem: 'vocals', envelope: envV })
    await flush()
    const reg = ins.appliedFor('P')
    expect(reg.filter(x => x.childId < 0).length).toBe(1)
    expect(reg.some(x => x.childId === 1)).toBe(true)   // вклейка A не потерялась
    expect(apiMock.rebuildSections).toHaveBeenCalledTimes(1)
    expect(apiMock.rebuildSections.mock.calls.at(-1)[0]).toBe('P')
    expect(lastCall().length).toBe(2)
  })

  it('в api уходит child_id 0, stems [stem], envelope как передан, без chain', async () => {
    const ins = await load({})
    await ins.addStemEnvelope('P', { stem: 'vocals', envelope: envV })
    await flush()
    const s = lastCall()[0]
    expect(s.child_id).toBe(0)
    expect(s.stems).toEqual(['vocals'])
    expect(s.envelope).toEqual(envV)
    expect(s.chain || '').toBe('')
  })

  it('повторно на ту же дорожку — заменяет прежнюю огибающую (одна запись на дорожку)', async () => {
    const ins = await load({})
    await ins.addStemEnvelope('P', { stem: 'vocals', envelope: envV })
    await flush()
    await ins.addStemEnvelope('P', { stem: 'vocals', envelope: envV2 })
    await flush()
    expect(ins.appliedFor('P').filter(x => x.childId < 0).length).toBe(1)
    const specs = envSpecs(lastCall())
    expect(specs.length).toBe(1)
    expect(specs[0]).toMatchObject({ child_id: 0, stems: ['vocals'] })
    expect(specs[0].envelope).toEqual(envV2)
  })

  it('на другую дорожку — добавляет вторую огибающую', async () => {
    const ins = await load({})
    await ins.addStemEnvelope('P', { stem: 'vocals', envelope: envV })
    await flush()
    await ins.addStemEnvelope('P', { stem: 'drums', envelope: envD })
    await flush()
    const recs = ins.appliedFor('P').filter(x => x.childId < 0)
    expect(recs.length).toBe(2)
    expect(new Set(recs.map(x => x.childId)).size).toBe(2)
    const byStem = Object.fromEntries(envSpecs(lastCall()).map(s => [s.stems.join(','), s.envelope]))
    expect(byStem).toEqual({ vocals: envV, drums: envD })
  })

  it('огибающая не путается с эффектом той же дорожки: эффект остаётся', async () => {
    const ins = await load({})
    await ins.addStemFx('P', { stem: 'vocals', chain: 'soften', params: { strength: 0.6 }, from: 0, to: 0 })
    await flush()
    await ins.addStemEnvelope('P', { stem: 'vocals', envelope: envV })
    await flush()
    const call = lastCall()
    expect(call.length).toBe(2)
    expect(call.some(s => s.chain === 'soften')).toBe(true)
    expect(envSpecs(call).length).toBe(1)
  })
})

// Карточка internal-studio-engine, тест-кейс 9 (условие 4): цепочка звукового движка
// на дорожку — запись реестра (instId 'engine', engine: chain), в api уходит полем
// engine; подпись — label записи.
describe('addStemEngine — цепочка движка на дорожку через пересборку', () => {
  const chain = [
    { type: 'amp', model: 'JCM2000.nam', input_db: -6 },
    { type: 'cab', cutoff_hz: 7000 },
  ]
  const eng = (over = {}) => ({ stem: 'other', chain, from: 20, to: 35, label: 'Гитара через JCM2000', ...over })
  const lastCall = () => apiMock.rebuildSections.mock.calls.at(-1)[1]
  const engSpecs = specs => specs.filter(s => Array.isArray(s.engine) && s.engine.length)

  it('запись в реестре: childId < 0, instId engine, engine = цепочка, окно, дорожка, label; вклейки на месте', async () => {
    const ins = await load({ applied: { P: [A] } })
    await ins.addStemEngine('P', eng())
    await flush()
    const reg = ins.appliedFor('P')
    expect(reg.some(x => x.childId === 1)).toBe(true)   // вклейка A не потерялась
    const recs = reg.filter(x => x.childId < 0)
    expect(recs.length).toBe(1)
    expect(recs[0]).toMatchObject({ instId: 'engine', from: 20, to: 35, stems: ['other'], db: 0, label: 'Гитара через JCM2000' })
    expect(recs[0].engine).toEqual(chain)
  })

  it('сразу одна пересборка; в api — child_id 0, stems [stem], engine как есть, окно, db 0, без chain/steps', async () => {
    const ins = await load({})
    await ins.addStemEngine('P', eng())
    await flush()
    expect(apiMock.rebuildSections).toHaveBeenCalledTimes(1)
    expect(apiMock.rebuildSections.mock.calls.at(-1)[0]).toBe('P')
    const s = lastCall()[0]
    expect(s).toMatchObject({ child_id: 0, stems: ['other'], from: 20, to: 35, db: 0 })
    expect(s.engine).toEqual(chain)
    expect(s.chain || '').toBe('')
    expect(s.steps).toBeUndefined()
  })

  it('окно по умолчанию — весь трек (from 0, to 0), label по умолчанию пустой', async () => {
    const ins = await load({})
    await ins.addStemEngine('P', { stem: 'guitar', chain })
    await flush()
    expect(lastCall()[0]).toMatchObject({ child_id: 0, stems: ['guitar'], from: 0, to: 0 })
    const rec = ins.appliedFor('P').find(x => x.childId < 0)
    expect(rec.from).toBe(0)
    expect(rec.to).toBe(0)
    expect(rec.label || '').toBe('')
  })

  it('копится: повторная пересборка (addMutes) снова отдаёт engine, заглушка — без engine', async () => {
    const ins = await load({})
    await ins.addStemEngine('P', eng())
    await flush()
    await ins.addMutes('P', [{ instId: 'm', from: 0, to: 5, stems: ['drums'], db: -100 }])
    await flush()
    expect(apiMock.rebuildSections).toHaveBeenCalledTimes(2)
    const call = lastCall()
    expect(call.length).toBe(2)
    const specs = engSpecs(call)
    expect(specs.length).toBe(1)
    expect(specs[0]).toMatchObject({ child_id: 0, stems: ['other'], from: 20, to: 35 })
    expect(specs[0].engine).toEqual(chain)
    const mute = call.find(s => !s.engine)
    expect(mute.db).toBe(-100)
  })

  it('вместе с эффектом ffmpeg и вклейкой: в api все три, у каждой своё поле', async () => {
    const ins = await load({ applied: { P: [A] } })
    await ins.addStemFx('P', { stem: 'vocals', chain: 'soften', params: { strength: 0.6 }, from: 0, to: 0 })
    await ins.addStemEngine('P', eng())
    await flush()
    const call = lastCall()
    expect(call.length).toBe(3)
    expect(call.some(s => s.child_id === 1)).toBe(true)
    expect(call.find(s => s.chain === 'soften').engine).toBeUndefined()
    expect(engSpecs(call).length).toBe(1)
  })

  it('две цепочки движка — две записи с уникальными отрицательными childId', async () => {
    const ins = await load({})
    await ins.addStemEngine('P', eng())
    await ins.addStemEngine('P', eng({ stem: 'vocals', chain: [{ type: 'reverb', wet: 0.2 }], label: '' }))
    await flush()
    const ids = ins.appliedFor('P').map(x => x.childId)
    expect(ids.length).toBe(2)
    expect(new Set(ids).size).toBe(2)
    for (const id of ids) expect(id).toBeLessThan(0)
    expect(engSpecs(lastCall()).map(s => s.stems[0]).sort()).toEqual(['other', 'vocals'])
  })

  it('реестр сохраняется в localStorage: engine и label на месте', async () => {
    const ins = await load({})
    await ins.addStemEngine('P', eng())
    await flush()
    const saved = JSON.parse(localStorage.getItem('yue_insert_applied'))
    const rec = saved.P.find(x => x.childId < 0)
    expect(rec.engine).toEqual(chain)
    expect(rec.label).toBe('Гитара через JCM2000')
  })

  it('latestFile не именует файл по записи движка', async () => {
    const ins = await load({ applied: { P: [{ ...A, childId: 2 }] } })
    await ins.addStemEngine('P', eng())
    await flush()
    expect(ins.latestFile('P')).toBe('overdub-inst-2.flac')
  })
})

// Карточка internal-own-track, этап 2, условие 16 (тест-кейс ТК38): addStemEngines(parentId, items) —
// несколько записей движка (каждая как в addStemEngine) одной пересборкой. Написаны по
// карточке, без чтения реализации.
describe('addStemEngines — несколько цепочек движка одной пересборкой (ТК38)', () => {
  const smp = kit => [{ type: 'sampler', kit }]
  const items = [
    { stem: 'kick', chain: smp('osdk/kick'), from: 0, to: 0, label: 'Бочка: набор' },
    { stem: 'snare', chain: smp('osdk/snare'), from: 0, to: 0, label: 'Малый: набор' },
    { stem: 'bass', chain: [{ type: 'bass', kit: 'growlybass/bass' }], from: 0, to: 0, label: 'Бас: бас-гитара (набор)' },
  ]
  const engSpecs = specs => specs.filter(s => Array.isArray(s.engine) && s.engine.length)

  it('три записи → одна пересборка, в ней все три цепочки', async () => {
    const ins = await load({})
    await ins.addStemEngines('P', items)
    await flush()
    expect(apiMock.rebuildSections).toHaveBeenCalledTimes(1)
    const [parent, specs] = apiMock.rebuildSections.mock.calls[0]
    expect(parent).toBe('P')
    const eng = engSpecs(specs)
    expect(eng.length).toBe(3)
    for (const item of items) {
      const s = eng.find(x => x.stems[0] === item.stem)
      expect(s).toMatchObject({ child_id: 0, stems: [item.stem], from: 0, to: 0, db: 0 })
      expect(s.engine).toEqual(item.chain)
    }
  })

  it('все три — в реестре: instId engine, уникальные отрицательные childId, label; вклейки на месте', async () => {
    const ins = await load({ applied: { P: [A] } })
    await ins.addStemEngines('P', items)
    await flush()
    const reg = ins.appliedFor('P')
    expect(reg.some(x => x.childId === 1)).toBe(true)
    const recs = reg.filter(x => x.childId < 0)
    expect(recs.length).toBe(3)
    expect(new Set(recs.map(r => r.childId)).size).toBe(3)
    for (const item of items) {
      const r = recs.find(x => x.stems[0] === item.stem)
      expect(r).toMatchObject({ instId: 'engine', stems: [item.stem], from: 0, to: 0, db: 0, label: item.label })
      expect(r.engine).toEqual(item.chain)
    }
    // вклейка A вошла в ту же пересборку
    expect(apiMock.rebuildSections.mock.calls[0][1].some(s => s.child_id === 1)).toBe(true)
  })

  it('окно выделения доходит до каждой записи', async () => {
    const ins = await load({})
    await ins.addStemEngines('P', items.map(item => ({ ...item, from: 12, to: 27 })))
    await flush()
    for (const s of engSpecs(apiMock.rebuildSections.mock.calls[0][1])) expect(s).toMatchObject({ from: 12, to: 27 })
  })

  it('пустой список — без пересборки, реестр не меняется', async () => {
    const ins = await load({ applied: { P: [A] } })
    await ins.addStemEngines('P', [])
    await flush()
    expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    expect(ins.appliedFor('P').map(x => x.childId)).toEqual([1])
  })

  it('реестр сохраняется в localStorage: все три записи', async () => {
    const ins = await load({})
    await ins.addStemEngines('P', items)
    await flush()
    const saved = JSON.parse(localStorage.getItem('yue_insert_applied'))
    expect(saved.P.filter(x => x.childId < 0).map(x => x.stems[0]).sort()).toEqual(['bass', 'kick', 'snare'])
  })
})

// Карточка internal-own-track, этап 0, условие 1 (тест-кейсы ТК1–ТК7): реестр правок —
// выключить (setOff), удалить (remove), заменить цепочку движка (replaceEngine);
// пересборка без активных записей не зовёт воркер; latestFile — только по активным;
// off переживает перезапуск и переносится carryTo. Написаны по карточке, без реализации.
describe('реестр правок: выключить, удалить, заменить (internal-own-track)', () => {
  // записи-эффекты в форме реестра (как их кладёт addStemFx): childId < 0, в api — child_id 0
  const fxRec = (childId, over = {}) => ({
    childId, instId: 'fx-soften', from: 0, to: 0, lead: 0, beat: 0, db: 0,
    stems: ['vocals'], fadeIn: 0, fadeOut: 0, keepHighHz: 0,
    chain: 'soften', params: { strength: 0.6 }, ...over,
  })
  const E1 = fxRec(-11)
  const E2 = fxRec(-12, { instId: 'fx-dewhistle', from: 10, to: 20, stems: ['drums'], chain: 'dewhistle', params: { freqs: [2638] } })
  // запись движка в форме реестра (как её кладёт addStemEngine)
  const oldChain = [{ type: 'amp', model: 'JCM2000.nam', input_db: -6 }, { type: 'cab', cutoff_hz: 7000 }]
  const newChain = [{ type: 'reverb', wet: 0.3 }]
  const ENG = {
    childId: -21, instId: 'engine', from: 20, to: 35, lead: 0, beat: 0, db: 0,
    stems: ['other'], fadeIn: 0, fadeOut: 0, keepHighHz: 0, engine: oldChain, label: 'Гитара через JCM2000',
  }
  const calls = () => apiMock.rebuildSections.mock.calls
  const lastSpecs = () => calls().at(-1)[1]
  const recOf = (ins, id) => ins.appliedFor('P').find(x => x.childId === id)

  // перезапуск приложения: модуль импортируется заново, localStorage — прежний
  async function reload() {
    vi.resetModules()
    const mod = await import('./useInserts.js')
    return mod.useInserts()
  }

  // ---- ТК1 ----
  describe('setOff (ТК1)', () => {
    it('выключенная запись остаётся в реестре с off: true, в пересборку уходит только вторая', async () => {
      const ins = await load({ applied: { P: [E1, E2] } })
      await ins.setOff('P', -11, true)
      await flush()
      expect(apiMock.rebuildSections).toHaveBeenCalledTimes(1)
      expect(calls().at(-1)[0]).toBe('P')
      const specs = lastSpecs()
      expect(specs.length).toBe(1)
      expect(specs[0]).toMatchObject({ child_id: 0, chain: 'dewhistle', stems: ['drums'], from: 10, to: 20 })
      expect(childIds(ins.appliedFor('P'))).toEqual([-11, -12].sort())
      expect(recOf(ins, -11).off).toBe(true)
      expect(recOf(ins, -12).off || false).toBe(false)
    })

    it('setOff(…, false) возвращает запись: пересборка снова с обеими', async () => {
      const ins = await load({ applied: { P: [E1, E2] } })
      await ins.setOff('P', -11, true)
      await flush()
      await ins.setOff('P', -11, false)
      await flush()
      expect(apiMock.rebuildSections).toHaveBeenCalledTimes(2)
      const chains = lastSpecs().map(s => s.chain).sort()
      expect(chains).toEqual(['dewhistle', 'soften'])
      expect(recOf(ins, -11).off).toBe(false)
    })

    it('выключенная вклейка (childId > 0) тоже не уходит в пересборку', async () => {
      const ins = await load({ applied: { P: [A, B] } })
      await ins.setOff('P', 1, true)
      await flush()
      expect(lastSpecs().map(s => s.child_id)).toEqual([2])
      expect(recOf(ins, 1).off).toBe(true)
    })
  })

  // ---- ТК2 ----
  describe('все записи выключены (ТК2)', () => {
    it('setOff единственной → воркер не вызван, результат { empty: true }', async () => {
      const ins = await load({ applied: { P: [E1] } })
      const res = await ins.setOff('P', -11, true)
      await flush()
      expect(apiMock.rebuildSections).not.toHaveBeenCalled()
      expect(res).toEqual({ empty: true })
      expect(recOf(ins, -11).off).toBe(true)
    })

    it('rebuild при реестре только из выключенных → { empty: true }, воркер не вызван', async () => {
      const ins = await load({ applied: { P: [{ ...E1, off: true }, { ...A, off: true }] } })
      const res = await ins.rebuild('P')
      expect(res).toEqual({ empty: true })
      expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    })

    it('rebuild трека без записей вовсе → { empty: true }, воркер не вызван', async () => {
      const ins = await load({})
      const res = await ins.rebuild('P')
      expect(res).toEqual({ empty: true })
      expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    })
  })

  // ---- ТК3 ----
  describe('remove (ТК3)', () => {
    it('удалённой записи нет в реестре, пересборка без неё', async () => {
      const ins = await load({ applied: { P: [E1, E2] } })
      await ins.remove('P', -11)
      await flush()
      expect(childIds(ins.appliedFor('P'))).toEqual([-12])
      expect(apiMock.rebuildSections).toHaveBeenCalledTimes(1)
      const specs = lastSpecs()
      expect(specs.length).toBe(1)
      expect(specs[0].chain).toBe('dewhistle')
    })

    it('удаление сохраняется в localStorage', async () => {
      const ins = await load({ applied: { P: [E1, E2] } })
      await ins.remove('P', -11)
      await flush()
      const saved = JSON.parse(localStorage.getItem('yue_insert_applied'))
      expect(saved.P.map(x => x.childId)).toEqual([-12])
    })

    it('повторный remove несуществующей записи — без ошибки и без пересборки', async () => {
      const ins = await load({ applied: { P: [E1, E2] } })
      await ins.remove('P', -11)
      await flush()
      await ins.remove('P', -11)   // не бросает и не отклоняется
      await flush()
      expect(apiMock.rebuildSections).toHaveBeenCalledTimes(1)
      expect(childIds(ins.appliedFor('P'))).toEqual([-12])
    })

    it('remove у трека без реестра — без ошибки и без пересборки', async () => {
      const ins = await load({})
      await ins.remove('нет-такого', 5)
      await flush()
      expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    })

    it('удаление последней записи → реестр пуст, воркер не зовётся (звучит оригинал)', async () => {
      const ins = await load({ applied: { P: [E1] } })
      await ins.remove('P', -11)
      await flush()
      expect(ins.appliedFor('P') || []).toEqual([])
      expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    })
  })

  // ---- ТК4 ----
  describe('remove вклейки с ожидающей спекой в очереди (ТК4)', () => {
    it('после remove и тика очереди (рендер done) запись не возвращается в реестр', async () => {
      const ins = await load({
        applied: { P: [A, B] },
        queue: [specB],
        jobs: [{ id: 2, status: 'done' }],
      })
      await ins.remove('P', 2)
      await flush()
      expect(childIds(pendingOf(ins))).not.toContain(2)

      // тик очереди: рендер ребёнка 2 готов
      await vi.advanceTimersByTimeAsync(3000)
      await flush()
      expect(childIds(ins.appliedFor('P'))).toEqual([1])
      for (const [, specs] of calls()) {
        expect(specs.map(s => s.child_id)).not.toContain(2)
      }
    })

    it('спеки других вклеек в очереди не трогаются', async () => {
      const specC = { parent: 'P', childId: 3, instId: 'i-c', from: 20, to: 30, lead: 0, beat: 0.5, db: -3, srcJob: 'P' }
      const ins = await load({
        applied: { P: [A, B] },
        queue: [specB, specC],
        jobs: [{ id: 2, status: 'running' }, { id: 3, status: 'running' }],
      })
      await ins.remove('P', 2)
      await flush()
      expect(childIds(pendingOf(ins))).toEqual([3])
    })
  })

  // ---- ТК5 ----
  describe('replaceEngine (ТК5)', () => {
    it('у записи движка новая цепочка и подпись; окно и дорожка прежние; пересборка с новой engine', async () => {
      const ins = await load({ applied: { P: [A, ENG] } })
      await ins.replaceEngine('P', -21, { chain: newChain, label: 'Реверб' })
      await flush()
      const rec = recOf(ins, -21)
      expect(rec.engine).toEqual(newChain)
      expect(rec.label).toBe('Реверб')
      expect(rec).toMatchObject({ from: 20, to: 35, stems: ['other'] })
      expect(childIds(ins.appliedFor('P'))).toEqual([-21, 1].sort())
      expect(apiMock.rebuildSections).toHaveBeenCalledTimes(1)
      const eng = lastSpecs().filter(s => Array.isArray(s.engine))
      expect(eng.length).toBe(1)
      expect(eng[0].engine).toEqual(newChain)
      expect(eng[0]).toMatchObject({ child_id: 0, from: 20, to: 35, stems: ['other'] })
    })

    it('без label подпись остаётся прежней', async () => {
      const ins = await load({ applied: { P: [ENG] } })
      await ins.replaceEngine('P', -21, { chain: newChain })
      await flush()
      const rec = recOf(ins, -21)
      expect(rec.engine).toEqual(newChain)
      expect(rec.label).toBe('Гитара через JCM2000')
    })

    it('запись-эффект (не движок) → ошибка, реестр без изменений, пересборки нет', async () => {
      const ins = await load({ applied: { P: [E1, ENG] } })
      const before = JSON.parse(JSON.stringify(ins.appliedFor('P')))
      await expect(ins.replaceEngine('P', -11, { chain: newChain, label: 'X' })).rejects.toThrow()
      await flush()
      expect(JSON.parse(JSON.stringify(ins.appliedFor('P')))).toEqual(before)
      expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    })

    it('вклейка (childId > 0) → ошибка, реестр без изменений, пересборки нет', async () => {
      const ins = await load({ applied: { P: [A] } })
      const before = JSON.parse(JSON.stringify(ins.appliedFor('P')))
      await expect(ins.replaceEngine('P', 1, { chain: newChain })).rejects.toThrow()
      await flush()
      expect(JSON.parse(JSON.stringify(ins.appliedFor('P')))).toEqual(before)
      expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    })
  })

  // ---- ТК6 ----
  describe('latestFile и выключенные вклейки (ТК6)', () => {
    it('последняя выключена → файл именует последняя активная вклейка', async () => {
      const ins = await load({ applied: { P: [A, { ...B, off: true }] } })
      expect(ins.latestFile('P')).toBe('overdub-inst-1.flac')
    })

    it('после setOff последней вклейки latestFile переходит на предыдущую активную', async () => {
      const ins = await load({ applied: { P: [A, B] } })
      await ins.setOff('P', 2, true)
      await flush()
      expect(ins.latestFile('P')).toBe('overdub-inst-1.flac')
    })

    it('все вклейки выключены → null (даже если есть активный эффект)', async () => {
      const ins = await load({ applied: { P: [{ ...A, off: true }, { ...B, off: true }, E1] } })
      expect(ins.latestFile('P')).toBeNull()
    })
  })

  // ---- ТК7 ----
  describe('off переживает перезапуск и переносится carryTo (ТК7)', () => {
    it('выключенная запись сохраняется в localStorage и после перезапуска модуля остаётся выключенной', async () => {
      const ins = await load({ applied: { P: [E1, E2] } })
      await ins.setOff('P', -11, true)
      await flush()
      const saved = JSON.parse(localStorage.getItem('yue_insert_applied'))
      expect(saved.P.find(x => x.childId === -11).off).toBe(true)

      const again = await reload()
      expect(recOf(again, -11).off).toBe(true)
      expect(childIds(again.appliedFor('P'))).toEqual([-11, -12].sort())

      // после перезапуска выключенная по-прежнему не уходит в пересборку
      apiMock.rebuildSections.mockClear()
      await again.rebuild('P')
      await flush()
      const specs = lastSpecs()
      expect(specs.length).toBe(1)
      expect(specs[0].chain).toBe('dewhistle')
    })

    // этап 1, условие 10: записи без рендера (childId < 0) переносятся сразу в реестр
    // новой версии, а не в очередь — off проверяется там
    it('carryTo переносит off как есть: выключенная — выключенной, включённая — включённой', async () => {
      const ins = await load({ applied: { P: [{ ...E1, off: true }, E2] } })
      ins.carryTo('P', 'Q', 'P')
      await flush()
      const regQ = ins.appliedFor('Q')
      expect(regQ.find(s => s.childId === -11).off).toBe(true)
      expect(regQ.find(s => s.childId === -12).off || false).toBe(false)
    })

    // находка ревью s0: тик собирал перенесённую вклейку в реестр новой версии без off — включённой
    it('выключенная вклейка после carryTo и тика остаётся выключенной в реестре новой версии', async () => {
      const ins = await load({ applied: { P: [{ ...A, off: true }] }, jobs: [{ id: 1, status: 'done' }] })
      ins.carryTo('P', 'Q', 'P')
      await ins.flush()
      await flush()
      const rec = ins.appliedFor('Q').find(x => x.childId === 1)
      expect(rec).toBeTruthy()
      expect(rec.off).toBe(true)
    })
  })

  // ---- кросс-ревью s0, круг 1 ----
  describe('гонки с тиком очереди и идущей пересборкой (кросс-ревью s0)', () => {
    it('remove во время тика: удалённая вклейка из очереди не возвращается в реестр', async () => {
      // тик ждёт пересборку первой вклейки; в это время пользователь удаляет вторую
      const d = deferred()
      const specA = { parent: 'P', childId: 1, instId: 'i-a', from: 0, to: 10, lead: 0, beat: 0.5, db: -6, srcJob: 'P' }
      const ins = await load({
        queue: [specA, specB], jobs: [{ id: 1, status: 'done' }, { id: 2, status: 'done' }],
        rebuildSections: vi.fn(() => d.promise),
      })
      const tick = ins.flush()
      await flush()
      const rm = ins.remove('P', 2)
      d.resolve(report([]))
      await tick.catch(() => {})
      await rm
      await flush()
      expect(ins.appliedFor('P').some(x => x.childId === 2)).toBe(false)
    })

    it('isBuilding: true, пока идёт пересборка трека, затем false', async () => {
      const d = deferred()
      const ins = await load({ applied: { P: [E1] }, rebuildSections: vi.fn(() => d.promise) })
      expect(unref(ins.isBuilding('P'))).toBe(false)
      const run = ins.rebuild('P')
      await flush()
      expect(unref(ins.isBuilding('P'))).toBe(true)
      expect(unref(ins.isBuilding('Q'))).toBe(false)
      d.resolve(report([]))
      await run
      await flush()
      expect(unref(ins.isBuilding('P'))).toBe(false)
    })

    it('isBuilding сбрасывается и после ошибки пересборки', async () => {
      const ins = await load({ applied: { P: [E1] }, rebuildSections: vi.fn(async () => { throw new Error('воркер упал') }) })
      await ins.rebuild('P').catch(() => {})
      await flush()
      expect(unref(ins.isBuilding('P'))).toBe(false)
    })
  })
})

// Этап 1 «Пресеты звука», условие 10 (ТК26): carryTo кладёт записи без рендера
// (эффект, педали, движок, громкость, линия; childId < 0) сразу в реестр новой версии —
// в очереди ожидания тик не находит их джобу и теряет. Вклейки (childId > 0) — через очередь.
describe('carryTo: правки без рендера — сразу в реестр новой версии (ТК26)', () => {
  const FX = {
    childId: -31, instId: 'fx-soften', from: 0, to: 0, lead: 0, beat: 0, db: 0,
    stems: ['vocals'], fadeIn: 0, fadeOut: 0, keepHighHz: 0, chain: 'soften', params: { strength: 0.6 }, off: true,
  }
  const ENG = {
    childId: -32, instId: 'engine', from: 20, to: 35, lead: 0, beat: 0, db: -4,
    stems: ['bass'], fadeIn: 0, fadeOut: 0, keepHighHz: 0,
    engine: [{ type: 'eq', highpass_hz: 80 }], label: 'Бас', off: false,
  }
  const PED = {
    childId: -33, instId: 'pedals', from: 0, to: 0, lead: 0, beat: 0, db: 0,
    stems: ['other'], fadeIn: 0, fadeOut: 0, keepHighHz: 0, steps: [{ chain: 'soften', params: { strength: 0.3 } }], label: 'Педали',
  }
  const MUTE = { childId: -34, instId: 'mute-drums', from: 5, to: 9, lead: 0, beat: 0, db: -100, stems: ['drums'] }
  const ENV = {
    childId: -35, instId: 'env', from: 0, to: 0, lead: 0, beat: 0, db: 0,
    stems: ['vocals'], envelope: [{ t: 0, db: 0 }, { t: 4, db: -6 }],
  }
  const recQ = (ins, id) => ins.appliedFor('Q').find(x => x.childId === id)

  it('эффект и движок сразу в реестре новой версии с теми же полями и off', async () => {
    const ins = await load({ applied: { P: [FX, ENG] } })
    ins.carryTo('P', 'Q', 'P')
    await flush()
    expect(recQ(ins, -31)).toMatchObject(FX)
    expect(recQ(ins, -32)).toMatchObject(ENG)
    expect(recQ(ins, -31).off).toBe(true)
    expect(recQ(ins, -32).off || false).toBe(false)
  })

  it('в очередь ожидания записи без рендера не попадают', async () => {
    const ins = await load({ applied: { P: [FX, ENG, PED, MUTE, ENV] } })
    ins.carryTo('P', 'Q', 'P')
    await flush()
    const forQ = pendingOf(ins).filter(s => s.parent === 'Q')
    expect(forQ.filter(s => s.childId <= 0)).toEqual([])
  })

  it('педали, громкость и линия громкости тоже переносятся сразу', async () => {
    const ins = await load({ applied: { P: [PED, MUTE, ENV] } })
    ins.carryTo('P', 'Q', 'P')
    await flush()
    expect(recQ(ins, -33)).toMatchObject(PED)
    expect(recQ(ins, -34)).toMatchObject(MUTE)
    expect(recQ(ins, -35)).toMatchObject(ENV)
  })

  it('тик очереди не теряет перенесённые записи (их джобы нет в списке)', async () => {
    const ins = await load({ applied: { P: [FX, ENG] }, jobs: [{ id: 99, status: 'done' }] })
    ins.carryTo('P', 'Q', 'P')
    await ins.flush()
    await vi.advanceTimersByTimeAsync(3000)
    await flush()
    expect(childIds(ins.appliedFor('Q'))).toEqual([-31, -32].sort())
  })

  it('вклейка (childId > 0) — по-прежнему через очередь: до готовности её нет в реестре', async () => {
    const ins = await load({ applied: { P: [A, ENG] }, jobs: [{ id: 1, status: 'running' }] })
    ins.carryTo('P', 'Q', 'P')
    await flush()
    const forQ = pendingOf(ins).filter(s => s.parent === 'Q')
    expect(forQ.map(s => s.childId)).toEqual([1])
    expect(recQ(ins, 1)).toBeUndefined()
    expect(recQ(ins, -32)).toMatchObject(ENG)
  })

  it('вклейка после готовности рендера попадает в реестр рядом с перенесённым движком', async () => {
    const ins = await load({ applied: { P: [A, ENG] }, jobs: [{ id: 1, status: 'done' }] })
    ins.carryTo('P', 'Q', 'P')
    await ins.flush()
    await flush()
    expect(childIds(ins.appliedFor('Q'))).toEqual([-32, 1].sort())
  })

  it('реестр исходной версии не меняется', async () => {
    const ins = await load({ applied: { P: [FX, ENG] } })
    ins.carryTo('P', 'Q', 'P')
    await flush()
    expect(ins.appliedFor('P').map(x => x.childId)).toEqual([-31, -32])
    expect(ins.appliedFor('P').find(x => x.childId === -31)).toMatchObject(FX)
  })

  it('перенесённые записи сохранены в localStorage под новой версией', async () => {
    const ins = await load({ applied: { P: [FX, ENG] } })
    ins.carryTo('P', 'Q', 'P')
    await flush()
    const saved = JSON.parse(localStorage.getItem('yue_insert_applied'))
    expect(childIds(saved.Q || [])).toEqual([-31, -32].sort())
  })
})
