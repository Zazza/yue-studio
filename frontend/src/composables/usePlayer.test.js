// Тесты «играть выделенное» (usePlayer.togglePlay) по контракту: верхний ▶
// при активном выделении гоняет кусок [from, to] артефакта волны — один прогон:
// playFile + перемотка на from (ползунок и тайминг шапки сразу на месте),
// пауза внутри куска — продолжение с места паузы, конец/стоп — новый прогон.
// Без выделения ▶ — обычная пауза/продолжение загруженного.
// Модуль — синглтон с setInterval: каждый тест импортирует его заново.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { unref } from 'vue'

const apiMock = vi.hoisted(() => ({}))
vi.mock('../api.js', () => ({ api: apiMock }))

// node-окружение vitest без DOM: минимальный localStorage (том плеера)
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

const RANGE = { key: 'w15:main', jobId: 15, file: 'audio.flac', dur: 180, from: 160, to: 168 }

async function freshPlayer(state) {
  vi.resetModules()
  const mod = await import('./usePlayer.js')
  const p = mod.usePlayer()
  p.playerState.value = { playing: false, position_sec: 0, duration_sec: 0, job_id: 0, ...state }
  return p
}

describe('▶ шапки: играть выделенное (togglePlay)', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('localStorage', new MemStorage())
    apiMock.playFile = vi.fn().mockResolvedValue({})
    apiMock.seekAudio = vi.fn().mockResolvedValue({})
    apiMock.toggleAudio = vi.fn().mockResolvedValue({})
    apiMock.stopAudio = vi.fn().mockResolvedValue({})
    apiMock.audioState = vi.fn().mockResolvedValue({ playing: false, position_sec: 0, duration_sec: 0, job_id: 0 })
  })
  afterEach(() => vi.useRealTimers())

  it('без выделения — обычная пауза загруженного', async () => {
    const p = await freshPlayer({ playing: true, job_id: 15, position_sec: 10, duration_sec: 180 })
    await p.togglePlay()
    expect(apiMock.toggleAudio).toHaveBeenCalledTimes(1)
    expect(apiMock.playFile).not.toHaveBeenCalled()
  })

  it('с выделением и без loaded — прогон куска: playFile, ползунок на from, стоп в конце', async () => {
    const p = await freshPlayer()
    p.setPlayRange(RANGE)
    await p.togglePlay()
    await flush()
    expect(apiMock.playFile).toHaveBeenCalledWith(15, 'audio.flac', 180)
    expect(apiMock.seekAudio).toHaveBeenCalledWith(160)
    expect(unref(p.seekPos)).toBe(160)              // ползунок сразу на начале куска
    expect(unref(p.rangeUntil)).toEqual({ key: 'w15:main', to: 168 })
    expect(unref(p.nowPlayingKey)).toBe('w15:main')
  })

  it('пауза внутри куска — продолжение с места паузы, без нового playFile', async () => {
    const p = await freshPlayer({ playing: false, job_id: 15, position_sec: 163, duration_sec: 180 })
    p.setPlayRange(RANGE)
    p.nowPlayingKey.value = 'w15:main'
    await p.togglePlay()
    expect(apiMock.toggleAudio).toHaveBeenCalledTimes(1)
    expect(apiMock.playFile).not.toHaveBeenCalled()
  })

  it('играет кусок — повторный ▶ это пауза', async () => {
    const p = await freshPlayer({ playing: true, job_id: 15, position_sec: 162, duration_sec: 180 })
    p.setPlayRange(RANGE)
    p.nowPlayingKey.value = 'w15:main'
    await p.togglePlay()
    expect(apiMock.toggleAudio).toHaveBeenCalledTimes(1)
    expect(apiMock.playFile).not.toHaveBeenCalled()
  })

  it('позиция за концом куска — новый прогон с начала', async () => {
    const p = await freshPlayer({ playing: false, job_id: 15, position_sec: 170, duration_sec: 180 })
    p.setPlayRange(RANGE)
    await p.togglePlay()
    await flush()
    expect(apiMock.playFile).toHaveBeenCalledTimes(1)
    expect(apiMock.seekAudio).toHaveBeenCalledWith(160)
  })

  it('снятие выделения очищает и действующий прогон', () => {
    // отдельно от freshPlayer: модуль уже импортирован предыдущими тестами
    return freshPlayer().then((p) => {
      p.setPlayRange(RANGE)
      p.playerState.value = { ...p.playerState.value, playing: false }
      p.rangeUntil.value = { key: 'w15:main', to: 168 }
      p.setPlayRange(null)
      expect(unref(p.playRange)).toBeNull()
      expect(unref(p.rangeUntil)).toBeNull()
    })
  })
})
