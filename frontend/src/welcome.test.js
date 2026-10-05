// Тесты окна «О приложении / Что нового» при запуске: какую вкладку показать и как
// меняется состояние при закрытии; плюс проверка данных RELEASE_NOTES.
import { describe, it, expect } from 'vitest'
import { welcomeTab, welcomeClosed } from './welcome.js'
import { RELEASE_NOTES } from './releaseNotes.js'

const V = '0.8.0'

describe('welcomeTab', () => {
  it('первый запуск без состояния → «О приложении»', () => {
    expect(welcomeTab(null, V)).toBe('about')
    expect(welcomeTab(undefined, V)).toBe('about')
  })

  it('первый запуск с пустой seenVersion → «О приложении», не новости', () => {
    expect(welcomeTab({ dismissed: false, seenVersion: '' }, V)).toBe('about')
  })

  it('закрывали без «не показывать», версия та же → снова «О приложении»', () => {
    expect(welcomeTab({ dismissed: false, seenVersion: V }, V)).toBe('about')
  })

  it('поставили «не показывать», версия та же → окна нет', () => {
    expect(welcomeTab({ dismissed: true, seenVersion: V }, V)).toBeNull()
  })

  it('новая версия → «Что нового» без отказа', () => {
    expect(welcomeTab({ dismissed: false, seenVersion: '0.7.0' }, V)).toBe('news')
  })

  it('новая версия → «Что нового» даже после «не показывать»', () => {
    expect(welcomeTab({ dismissed: true, seenVersion: '0.7.0' }, V)).toBe('news')
  })

  it('текущая версия не задана → новостей нет, работают правила пп.1–3', () => {
    for (const cur of ['', undefined, null]) {
      expect(welcomeTab(null, cur)).toBe('about')
      expect(welcomeTab({ dismissed: false, seenVersion: '0.7.0' }, cur)).toBe('about')
      expect(welcomeTab({ dismissed: true, seenVersion: '0.7.0' }, cur)).toBeNull()
    }
  })
})

describe('welcomeClosed', () => {
  it('seenVersion становится текущей версией', () => {
    expect(welcomeClosed({ dismissed: false, seenVersion: '0.7.0' }, V, false).seenVersion).toBe(V)
  })

  it('галка «не показывать» ставит dismissed', () => {
    expect(welcomeClosed({ dismissed: false, seenVersion: V }, V, true).dismissed).toBe(true)
  })

  it('без галки dismissed остаётся false', () => {
    expect(welcomeClosed({ dismissed: false, seenVersion: V }, V, false).dismissed).toBe(false)
  })

  it('закрытие без галки не снимает прежний отказ', () => {
    expect(welcomeClosed({ dismissed: true, seenVersion: '0.7.0' }, V, false).dismissed).toBe(true)
  })

  it('работает при отсутствующем состоянии', () => {
    for (const s of [null, undefined]) {
      expect(welcomeClosed(s, V, false)).toMatchObject({ dismissed: false, seenVersion: V })
      expect(welcomeClosed(s, V, true)).toMatchObject({ dismissed: true, seenVersion: V })
    }
  })

  it('сценарий целиком: знакомство → отказ → новая версия → снова тихо', () => {
    let s = null
    expect(welcomeTab(s, '0.8.0')).toBe('about')
    s = welcomeClosed(s, '0.8.0', false)
    expect(welcomeTab(s, '0.8.0')).toBe('about')
    s = welcomeClosed(s, '0.8.0', true)
    expect(welcomeTab(s, '0.8.0')).toBeNull()
    expect(welcomeTab(s, '0.9.0')).toBe('news')
    s = welcomeClosed(s, '0.9.0', false)
    expect(welcomeTab(s, '0.9.0')).toBeNull()
  })
})

describe('RELEASE_NOTES', () => {
  const parse = (v) => v.split('.').map(Number)
  const cmp = (a, b) => {
    const [x, y] = [parse(a), parse(b)]
    for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] - y[i]
    return 0
  }

  it('непустой массив', () => {
    expect(Array.isArray(RELEASE_NOTES)).toBe(true)
    expect(RELEASE_NOTES.length).toBeGreaterThan(0)
  })

  it('у каждой записи версия X.Y.Z и непустые ru/en одинаковой длины', () => {
    for (const n of RELEASE_NOTES) {
      expect(n.version).toMatch(/^\d+\.\d+\.\d+$/)
      expect(Array.isArray(n.ru)).toBe(true)
      expect(Array.isArray(n.en)).toBe(true)
      expect(n.ru.length).toBeGreaterThanOrEqual(1)
      expect(n.en.length).toBe(n.ru.length)
      for (const line of [...n.ru, ...n.en]) {
        expect(typeof line).toBe('string')
        expect(line.trim().length).toBeGreaterThan(0)
      }
    }
  })

  it('версии уникальны и идут от новой к старой', () => {
    const vs = RELEASE_NOTES.map((n) => n.version)
    expect(new Set(vs).size).toBe(vs.length)
    for (let i = 1; i < vs.length; i++) expect(cmp(vs[i - 1], vs[i])).toBeGreaterThan(0)
  })
})
