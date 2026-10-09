// Тесты карточки internal-own-track, этап 6б, условие 48 (тест-кейс ТК80): громкость дорожки на
// пульте и подсказка «слышно ли».
//   stemLevels(stems) → {имя: дБ}: rms_p95_db дорожки (metrics.metrics.rms_p95_db из списка дорожек
//     воркера) относительно суммы основных (vocals, drums, bass, other — сумма мощностей); дорожка
//     без замера — без записи; основных нет — {};
//   stemAudibility(db): < −30 — 'silent', −30…−20 — 'quiet', иначе ''.
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { stemLevels, stemAudibility } from './trackDesk.js'

// запись списка дорожек воркера: {name, file, metrics: {file, metrics: {rms_p95_db, …}}}
const stem = (name, db) => ({
  name,
  file: `stem-${name}.flac`,
  metrics: { file: `stem-${name}.flac`, metrics: { rms_p95_db: db, rms_median_db: db - 6, peak: 0.5 } },
})
const MAIN = ['vocals', 'drums', 'bass', 'other'].map((n) => stem(n, -20))

describe('stemLevels (ТК80)', () => {
  it('основные по −20 дБ (сумма −14) и hh −40 → hh −26, каждая основная −6', () => {
    const lv = stemLevels([...MAIN, stem('hh', -40)])
    const sum = 10 * Math.log10(4 * 10 ** (-20 / 10)) // −13,98
    expect(lv.hh).toBeCloseTo(-40 - sum, 1)
    expect(Math.abs(lv.hh - -26)).toBeLessThanOrEqual(0.1)
    for (const n of ['vocals', 'drums', 'bass', 'other']) {
      expect(Math.abs(lv[n] - -6)).toBeLessThanOrEqual(0.1)
    }
  })

  it('дорожка без замера — нет в ответе', () => {
    const lv = stemLevels([...MAIN, { name: 'ride', file: 'stem-ride.flac' },
      { name: 'crash', file: 'stem-crash.flac', metrics: null },
      { name: 'toms', file: 'stem-toms.flac', metrics: { file: 'stem-toms.flac', metrics: {} } }])
    expect(lv).not.toHaveProperty('ride')
    expect(lv).not.toHaveProperty('crash')
    expect(lv).not.toHaveProperty('toms')
    expect(Object.keys(lv).sort()).toEqual(['bass', 'drums', 'other', 'vocals'])
  })

  it('нет основных дорожек — {}', () => {
    expect(stemLevels([stem('hh', -40), stem('kick', -25)])).toEqual({})
    expect(stemLevels([])).toEqual({})
  })
})

describe('stemAudibility (ТК80)', () => {
  it.each([[-35, 'silent'], [-25, 'quiet'], [-10, ''], [-30, 'quiet'], [-20, '']])('%d дБ → %j', (db, want) => {
    expect(stemAudibility(db)).toBe(want)
  })
})
