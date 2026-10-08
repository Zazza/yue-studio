// Тесты карточки internal-own-track, этап 0, условие 2 (тест-кейсы ТК8–ТК11): чистая
// логика пульта дорожек в студии — строки пульта по дорожкам трека, окно «было/стало»,
// окно записи в трек, готовые цепочки для дорожки. Окна — {from, to} в секундах.
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { deskRows, previewWindow, applyWindow, presetsFor } from './trackDesk.js'

// запись реестра правок (как в useInserts): нужны childId, stems, off
const rec = (childId, stems, over = {}) => ({ childId, instId: 'engine', from: 0, to: 0, db: 0, stems, ...over })
const ids = list => list.map(x => x.childId).sort((a, b) => a - b)

describe('deskRows — строки пульта (ТК8)', () => {
  it('порядок: голос, барабаны, части барабанов, бас, …, «прочее», затем неизвестные', () => {
    const rows = deskRows(['other', 'vocals', 'kick', 'bass', 'zzz', 'drums'], [])
    expect(rows.map(r => r.stem)).toEqual(['vocals', 'drums', 'kick', 'bass', 'other', 'zzz'])
  })

  it('полный набор известных дорожек в обратном порядке → порядок карточки', () => {
    const known = ['vocals', 'drums', 'kick', 'snare', 'toms', 'hh', 'ride', 'crash', 'bass', 'guitar', 'piano', 'other']
    const rows = deskRows([...known].reverse(), [])
    expect(rows.map(r => r.stem)).toEqual(known)
  })

  it('неизвестные дорожки — в конце, по алфавиту', () => {
    const rows = deskRows(['zzz', 'other', 'aaa', 'vocals', 'mmm'], [])
    expect(rows.map(r => r.stem)).toEqual(['vocals', 'other', 'aaa', 'mmm', 'zzz'])
  })

  it('только дорожки трека: правка на дорожку, которой нет у трека, строки не создаёт', () => {
    const rows = deskRows(['vocals'], [rec(-1, ['guitar'])])
    expect(rows.map(r => r.stem)).toEqual(['vocals'])
    expect(rows[0].edits).toEqual([])
  })

  it('правки раскладываются по stems: запись с двумя дорожками — в обеих, выключенная — тоже', () => {
    const both = rec(-1, ['vocals', 'drums'])
    const offBass = rec(-2, ['bass'], { off: true })
    const ins = rec(5, ['kick'], { instId: 'drumfill' })
    const rows = deskRows(['other', 'vocals', 'kick', 'bass', 'zzz', 'drums'], [both, offBass, ins])
    const by = Object.fromEntries(rows.map(r => [r.stem, r]))
    expect(ids(by.vocals.edits)).toEqual([-1])
    expect(ids(by.drums.edits)).toEqual([-1])
    expect(ids(by.bass.edits)).toEqual([-2])
    expect(by.bass.edits[0].off).toBe(true)
    expect(ids(by.kick.edits)).toEqual([5])
    expect(by.other.edits).toEqual([])
    expect(by.zzz.edits).toEqual([])
  })

  it('у дорожки несколько правок — все в её строке', () => {
    const rows = deskRows(['vocals'], [rec(-1, ['vocals']), rec(-2, ['vocals'], { off: true }), rec(-3, ['bass'])])
    expect(ids(rows[0].edits)).toEqual([-2, -1])
  })

  it('запись без stems не попадает ни в одну строку', () => {
    const rows = deskRows(['vocals', 'bass'], [{ childId: 3, instId: 'i-a', from: 0, to: 10 }])
    for (const r of rows) expect(r.edits).toEqual([])
  })

  it('нет дорожек → нет строк', () => {
    expect(deskRows([], [rec(-1, ['vocals'])])).toEqual([])
  })
})

describe('previewWindow — окно «было/стало» (ТК9)', () => {
  it('выделение берётся как есть', () => {
    expect(previewWindow({ from: 5, to: 9 }, 20, 100)).toEqual({ from: 5, to: 9 })
  })

  it('без выделения — 15 с от курсора', () => {
    expect(previewWindow(null, 20, 100)).toEqual({ from: 20, to: 35 })
    expect(previewWindow(undefined, 20, 100)).toEqual({ from: 20, to: 35 })
  })

  it('своя длина окна len', () => {
    expect(previewWindow(null, 20, 100, 10)).toEqual({ from: 20, to: 30 })
  })

  it('до конца меньше 3 с — окно сдвигается влево на полную длину', () => {
    expect(previewWindow(null, 98, 100)).toEqual({ from: 85, to: 100 })
    expect(previewWindow(null, 99.5, 100)).toEqual({ from: 85, to: 100 })
  })

  it('до конца 3 с и больше — окно обрезается по концу трека без сдвига', () => {
    expect(previewWindow(null, 90, 100)).toEqual({ from: 90, to: 100 })
    expect(previewWindow(null, 97, 100)).toEqual({ from: 97, to: 100 })
  })

  it('трек короче окна → окно — весь трек (min(len, dur))', () => {
    expect(previewWindow(null, 0, 10)).toEqual({ from: 0, to: 10 })
    expect(previewWindow(null, 8, 10)).toEqual({ from: 0, to: 10 })
  })

  it('длина трека неизвестна (0/undefined) → без обрезки', () => {
    expect(previewWindow(null, 95, 0)).toEqual({ from: 95, to: 110 })
    expect(previewWindow(null, 95, undefined)).toEqual({ from: 95, to: 110 })
  })
})

describe('applyWindow — окно записи в трек (ТК10)', () => {
  it('выделение → оно', () => {
    expect(applyWindow({ from: 12, to: 27 })).toEqual({ from: 12, to: 27 })
  })

  it('нет выделения → весь трек {from: 0, to: 0}', () => {
    expect(applyWindow(null)).toEqual({ from: 0, to: 0 })
    expect(applyWindow(undefined)).toEqual({ from: 0, to: 0 })
  })
})

describe('presetsFor — готовые цепочки для дорожки (ТК11)', () => {
  const presets = [
    { id: 'any-1', chain: [] },
    { id: 'guitar', stems: ['guitar', 'other'], chain: [] },
    { id: 'vocal', stems: ['vocals'], chain: [] },
    { id: 'any-2', chain: [] },
    { id: 'kick', stems: ['kick'], chain: [] },
  ]
  const idsOf = list => list.map(p => p.id)

  it('пресет без stems — для любой дорожки, со stems — только для своих; порядок исходный', () => {
    expect(idsOf(presetsFor('guitar', presets))).toEqual(['any-1', 'guitar', 'any-2'])
    expect(idsOf(presetsFor('other', presets))).toEqual(['any-1', 'guitar', 'any-2'])
    expect(idsOf(presetsFor('vocals', presets))).toEqual(['any-1', 'vocal', 'any-2'])
    expect(idsOf(presetsFor('kick', presets))).toEqual(['any-1', 'any-2', 'kick'])
  })

  it('дорожка, для которой своих пресетов нет, — только общие', () => {
    expect(idsOf(presetsFor('zzz', presets))).toEqual(['any-1', 'any-2'])
  })

  it('пустой список пресетов → пусто', () => {
    expect(presetsFor('vocals', [])).toEqual([])
  })
})
