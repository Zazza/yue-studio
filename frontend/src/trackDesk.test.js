// Тесты карточки internal-own-track, этап 0, условие 2 (тест-кейсы ТК8–ТК11): чистая
// логика пульта дорожек в студии — строки пульта по дорожкам трека, окно «было/стало»,
// окно записи в трек, готовые цепочки для дорожки. Окна — {from, to} в секундах.
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { deskRows, previewWindow, applyWindow, presetsFor, rhythmSection } from './trackDesk.js'
import { fxPresets } from './fxPresets.js'

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

// Карточка internal-own-track, этап 2, условия 15–17 (тест-кейс ТК37): ритм-секция одной
// записью. rhythmSection(stemNames, presets, room) → [{stem, chain, label}] в порядке kick,
// snare, toms, hh, ride, crash, bass — только для частей, которые есть у трека; цепочки —
// готовые «набором» из fxPresets; при room к каждой части барабанов (не баса) в конец —
// reverb «комната». Написаны по карточке, без чтения реализации.
describe('rhythmSection — ритм-секция набором (ТК37)', () => {
  const ROOM = { type: 'reverb', decay_s: 0.5, predelay_ms: 5, lowpass_hz: 7000, wet: 0.12 }
  // готовые цепочки «набором» по частям (id пресетов fxPresets)
  const PRESET_OF = {
    kick: 'drums-kick-kit',
    snare: 'drums-snare-kit',
    toms: 'drums-toms-kit',
    hh: 'drums-hh-kit',
    ride: 'drums-ride-kit',
    crash: 'drums-crash-kit',
    bass: 'bass-kit',
  }
  const presetChain = stem => fxPresets.find(p => p.id === PRESET_OF[stem]).chain
  const stemsOf = list => list.map(x => x.stem)
  const snapshot = () => JSON.parse(JSON.stringify(fxPresets))

  it('части трека → kick, snare, hh, bass по порядку; «прочее» пропускается', () => {
    const out = rhythmSection(['kick', 'snare', 'hh', 'bass', 'other'], fxPresets, true)
    expect(stemsOf(out)).toEqual(['kick', 'snare', 'hh', 'bass'])
  })

  it('порядок карточки не зависит от порядка дорожек трека', () => {
    const all = ['bass', 'crash', 'ride', 'hh', 'toms', 'snare', 'kick', 'vocals', 'drums', 'other']
    expect(stemsOf(rhythmSection(all, fxPresets, false))).toEqual(['kick', 'snare', 'toms', 'hh', 'ride', 'crash', 'bass'])
  })

  it('room: у частей барабанов последний блок — комната с параметрами карточки, перед ней — готовая цепочка', () => {
    const out = rhythmSection(['kick', 'snare', 'hh', 'bass', 'other'], fxPresets, true)
    for (const r of out.filter(x => x.stem !== 'bass')) {
      expect(r.chain.at(-1)).toMatchObject(ROOM)
      expect(r.chain.slice(0, -1)).toEqual(presetChain(r.stem))
    }
  })

  it('room: у баса комнаты нет — цепочка «бас-гитара набором» как есть', () => {
    const bass = rhythmSection(['kick', 'bass'], fxPresets, true).find(x => x.stem === 'bass')
    expect(bass.chain.some(b => b.type === 'reverb')).toBe(false)
    expect(bass.chain).toEqual(presetChain('bass'))
  })

  it('room=false — без комнаты: цепочки готовые как есть', () => {
    const out = rhythmSection(['kick', 'snare', 'hh', 'bass', 'other'], fxPresets, false)
    expect(out.length).toBe(4)
    for (const r of out) {
      expect(r.chain.some(b => b.type === 'reverb')).toBe(false)
      expect(r.chain).toEqual(presetChain(r.stem))
    }
  })

  it('у каждой записи есть подпись', () => {
    for (const r of rhythmSection(['kick', 'toms', 'bass'], fxPresets, true)) {
      expect(typeof r.label).toBe('string')
      expect(r.label.length).toBeGreaterThan(0)
    }
  })

  it('нет ни частей барабанов, ни баса → []', () => {
    expect(rhythmSection(['vocals', 'drums', 'other', 'guitar'], fxPresets, true)).toEqual([])
    expect(rhythmSection([], fxPresets, true)).toEqual([])
  })

  it('только бас (частей барабанов нет) → одна запись баса', () => {
    expect(stemsOf(rhythmSection(['vocals', 'bass', 'other'], fxPresets, true))).toEqual(['bass'])
  })

  it('toms есть → цепочка «Тамы: набор по высоте» (+ комната)', () => {
    const out = rhythmSection(['toms'], fxPresets, true)
    expect(stemsOf(out)).toEqual(['toms'])
    expect(out[0].chain.slice(0, -1)).toEqual(presetChain('toms'))
    expect(out[0].chain.at(-1)).toMatchObject(ROOM)
  })

  it('готовая цепочка тамов: stems [toms], sampler tom-small / tom-medium / tom-large', () => {
    const p = fxPresets.find(x => x.id === 'drums-toms-kit')
    expect(p).toBeTruthy()
    expect(p.stems).toEqual(['toms'])
    expect(p.name.ru).toBe('Тамы: набор по высоте')
    const smp = p.chain.find(b => b.type === 'sampler')
    expect(smp).toMatchObject({ kit: 'osdk/tom-small', kit_mid: 'osdk/tom-medium', kit_low: 'osdk/tom-large' })
  })

  it('пресеты не портятся: комната не дописывается в fxPresets, повторный вызов — тот же результат', () => {
    const before = snapshot()
    const a = rhythmSection(['kick', 'snare', 'toms', 'hh', 'ride', 'crash', 'bass'], fxPresets, true)
    const b = rhythmSection(['kick', 'snare', 'toms', 'hh', 'ride', 'crash', 'bass'], fxPresets, true)
    expect(snapshot()).toEqual(before)
    expect(b).toEqual(a)
    a[0].chain.push({ type: 'gain', gain_db: 1 })
    expect(snapshot()).toEqual(before)
  })
})
