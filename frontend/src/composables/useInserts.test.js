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
