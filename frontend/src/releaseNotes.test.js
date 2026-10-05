import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { RELEASE_NOTES } from './releaseNotes.js'

// Страж шага релиза (AGENTS.md п.2): версия вышла в CHANGELOG — в окне
// «Что нового» должна быть её запись. Иначе приложение не узнает о новой версии.
const num = (v) => v.split('.').map(Number)
const newer = (a, b) => {
  const [x, y] = [num(a), num(b)]
  for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] > y[i]
  return false
}

describe('releaseNotes: запись на каждую вышедшую версию', () => {
  it('первая запись не старше последней версии в CHANGELOG', () => {
    const changelog = readFileSync(new URL('../../CHANGELOG.md', import.meta.url), 'utf8')
    const released = [...changelog.matchAll(/^## \[(\d+\.\d+\.\d+)\]/gm)].map((m) => m[1])
    expect(released.length).toBeGreaterThan(0)
    const latest = released.reduce((a, b) => (newer(b, a) ? b : a))
    expect(newer(latest, RELEASE_NOTES[0].version),
      `в CHANGELOG вышла ${latest}, а первая запись releaseNotes.js — ${RELEASE_NOTES[0].version}`).toBe(false)
  })
})
