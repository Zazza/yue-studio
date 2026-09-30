// Тесты сервиса «перепеть с места» (useRevoice) по спецификации: очередь подстановок
// голоса переживает перезапуск; tick() по статусам задач решает — ждать, подставить
// голос дубля в родителя и сохранить версию, или выбросить запись.
// Модуль — синглтон, поэтому каждый тест импортирует его заново после vi.resetModules().
import { describe, it, expect, vi, afterEach } from 'vitest'
import { unref } from 'vue'

// Мок внешней границы — API воркера.
const apiMock = vi.hoisted(() => ({}))
vi.mock('../api.js', () => ({ api: apiMock }))

class MemStorage {
  constructor() { this.m = new Map() }
  getItem(k) { return this.m.has(k) ? this.m.get(k) : null }
  setItem(k, v) { this.m.set(k, String(v)) }
  removeItem(k) { this.m.delete(k) }
  clear() { this.m.clear() }
}

async function flush() {
  for (let i = 0; i < 10; i++) await vi.advanceTimersByTimeAsync(0)
}

const entry = (child, extra = {}) => ({
  parent: 'P', child, from: 12.5, to: 30, beat: 0.48, voiceSrc: 'voice-A', title: 'Перепето ' + child, ...extra,
})

// Импорт свежего экземпляра; storage — сохраняется между «перезапусками», если передан.
async function load({ jobs = [], storage } = {}) {
  vi.resetModules()
  globalThis.localStorage = storage || new MemStorage()
  vi.useFakeTimers()
  apiMock.jobs = vi.fn(async () => jobs)
  apiMock.rebuildSections = vi.fn(async () => ({ variant: { file: 'rebuilt.wav' } }))
  apiMock.variantToTrack = vi.fn(async () => ({}))
  const mod = await import('./useRevoice.js')
  return mod.useRevoice()
}

const pendingOf = rv => unref(rv.pending) || []
const children = rv => pendingOf(rv).map(x => x.child).sort()

async function tickNow(rv) {
  const p = rv.tick()
  await flush()
  await p
}

afterEach(() => {
  vi.clearAllTimers()
  vi.useRealTimers()
})

describe('register и хранение очереди', () => {
  it('register ставит подстановки в очередь', async () => {
    const rv = await load()
    rv.register([entry('C1'), entry('C2')])
    await flush()
    expect(children(rv)).toEqual(['C1', 'C2'])
  })

  it('очередь переживает перезапуск (localStorage)', async () => {
    const storage = new MemStorage()
    const rv1 = await load({ storage })
    rv1.register([entry('C1')])
    await flush()
    const rv2 = await load({ storage })
    expect(pendingOf(rv2)).toEqual([expect.objectContaining(entry('C1'))])
  })

  it('пустой register не ломает очередь', async () => {
    const rv = await load()
    rv.register([])
    await flush()
    expect(pendingOf(rv)).toEqual([])
  })
})

describe('tick', () => {
  for (const status of ['queued', 'running']) {
    it(`дубль ${status} → запись остаётся, API пересборки не вызывается`, async () => {
      const rv = await load({ jobs: [{ id: 'P', status: 'done' }, { id: 'C1', status }] })
      rv.register([entry('C1')])
      await tickNow(rv)
      expect(children(rv)).toEqual(['C1'])
      expect(apiMock.rebuildSections).not.toHaveBeenCalled()
      expect(apiMock.variantToTrack).not.toHaveBeenCalled()
    })
  }

  it('дубль done → одна спека только вокала, затем версия из файла ответа; запись уходит', async () => {
    const rv = await load({ jobs: [{ id: 'P', status: 'done' }, { id: 'C1', status: 'done' }] })
    apiMock.rebuildSections = vi.fn(async () => ({ variant: { file: 'variant-777.wav' } }))
    rv.register([entry('C1')])
    await tickNow(rv)
    expect(apiMock.rebuildSections).toHaveBeenCalledTimes(1)
    expect(apiMock.rebuildSections).toHaveBeenCalledWith('P', [
      { child_id: 'C1', from: 12.5, to: 30, lead: 12.5, beat_sec: 0.48, stems: ['vocals'], revoice: true },
    ])
    expect(apiMock.variantToTrack).toHaveBeenCalledTimes(1)
    expect(apiMock.variantToTrack).toHaveBeenCalledWith('P', 'variant-777.wav', 'Перепето C1', 'voice-A')
    expect(apiMock.rebuildSections.mock.invocationCallOrder[0])
      .toBeLessThan(apiMock.variantToTrack.mock.invocationCallOrder[0])
    expect(pendingOf(rv)).toEqual([])
  })

  it('после успешной подстановки запись не возвращается после перезапуска', async () => {
    const storage = new MemStorage()
    const jobs = [{ id: 'P', status: 'done' }, { id: 'C1', status: 'done' }]
    const rv = await load({ jobs, storage })
    rv.register([entry('C1')])
    await tickNow(rv)
    const rv2 = await load({ jobs, storage })
    expect(pendingOf(rv2)).toEqual([])
  })

  for (const status of ['error', 'canceled']) {
    it(`дубль ${status} → запись убирается без вызовов`, async () => {
      const rv = await load({ jobs: [{ id: 'P', status: 'done' }, { id: 'C1', status }] })
      rv.register([entry('C1')])
      await tickNow(rv)
      expect(pendingOf(rv)).toEqual([])
      expect(apiMock.rebuildSections).not.toHaveBeenCalled()
      expect(apiMock.variantToTrack).not.toHaveBeenCalled()
    })
  }

  it('дубль исчез из списка задач → запись убирается без вызовов', async () => {
    const rv = await load({ jobs: [{ id: 'P', status: 'done' }] })
    rv.register([entry('C1')])
    await tickNow(rv)
    expect(pendingOf(rv)).toEqual([])
    expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    expect(apiMock.variantToTrack).not.toHaveBeenCalled()
  })

  it('исчез parent (дубль done) → запись убирается без вызовов', async () => {
    const rv = await load({ jobs: [{ id: 'C1', status: 'done' }] })
    rv.register([entry('C1')])
    await tickNow(rv)
    expect(pendingOf(rv)).toEqual([])
    expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    expect(apiMock.variantToTrack).not.toHaveBeenCalled()
  })

  it('сбой rebuildSections → запись остаётся, другие обрабатываются, повтор на следующем tick', async () => {
    const rv = await load({ jobs: [
      { id: 'P', status: 'done' }, { id: 'C1', status: 'done' }, { id: 'C2', status: 'done' },
    ] })
    apiMock.rebuildSections = vi.fn(async (_p, specs) => {
      if (specs[0].child_id === 'C1') throw new Error('worker down')
      return { variant: { file: 'ok.wav' } }
    })
    rv.register([entry('C1'), entry('C2')])
    await tickNow(rv)
    expect(children(rv)).toEqual(['C1'])
    expect(apiMock.variantToTrack).toHaveBeenCalledTimes(1)
    expect(apiMock.variantToTrack).toHaveBeenCalledWith('P', 'ok.wav', 'Перепето C2', 'voice-A')

    apiMock.rebuildSections = vi.fn(async () => ({ variant: { file: 'retry.wav' } }))
    await tickNow(rv)
    expect(apiMock.rebuildSections).toHaveBeenCalledTimes(1)
    expect(apiMock.variantToTrack).toHaveBeenLastCalledWith('P', 'retry.wav', 'Перепето C1', 'voice-A')
    expect(pendingOf(rv)).toEqual([])
  })

  it('сбой variantToTrack → запись остаётся, другие обрабатываются', async () => {
    const rv = await load({ jobs: [
      { id: 'P', status: 'done' }, { id: 'C1', status: 'done' }, { id: 'C2', status: 'done' },
    ] })
    apiMock.variantToTrack = vi.fn(async (_p, _f, title) => {
      if (title === 'Перепето C1') throw new Error('disk full')
      return {}
    })
    rv.register([entry('C1'), entry('C2')])
    await tickNow(rv)
    expect(children(rv)).toEqual(['C1'])
    expect(apiMock.variantToTrack).toHaveBeenCalledTimes(2)
  })

  it('api.jobs() упал → очередь не меняется, пересборок нет', async () => {
    const rv = await load()
    apiMock.jobs = vi.fn(async () => { throw new Error('offline') })
    rv.register([entry('C1'), entry('C2')])
    await flush()
    const before = JSON.parse(JSON.stringify(pendingOf(rv)))
    await tickNow(rv)
    expect(pendingOf(rv)).toEqual(before)
    expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    expect(apiMock.variantToTrack).not.toHaveBeenCalled()
  })

  it('пустая очередь → tick ничего не пересобирает', async () => {
    const rv = await load({ jobs: [{ id: 'P', status: 'done' }] })
    await tickNow(rv)
    expect(apiMock.rebuildSections).not.toHaveBeenCalled()
    expect(pendingOf(rv)).toEqual([])
  })
})

describe('два дубля одной части', () => {
  it('две подстановки и две версии: свой child, один voiceSrc', async () => {
    const rv = await load({ jobs: [
      { id: 'P', status: 'done' }, { id: 'C1', status: 'done' }, { id: 'C2', status: 'done' },
    ] })
    apiMock.rebuildSections = vi.fn(async (_p, specs) => ({ variant: { file: `v-${specs[0].child_id}.wav` } }))
    rv.register([entry('C1'), entry('C2')])
    await tickNow(rv)
    expect(apiMock.rebuildSections).toHaveBeenCalledTimes(2)
    const sent = apiMock.rebuildSections.mock.calls.map(c => { expect(c[1]).toHaveLength(1); return c[1][0].child_id }).sort()
    expect(sent).toEqual(['C1', 'C2'])
    const versions = apiMock.variantToTrack.mock.calls.map(c => [c[1], c[3]]).sort()
    expect(versions).toEqual([['v-C1.wav', 'voice-A'], ['v-C2.wav', 'voice-A']])
    expect(pendingOf(rv)).toEqual([])
  })
})
