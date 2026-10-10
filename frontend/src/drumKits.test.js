// Тесты карточки internal-own-track, этап 7б «Наборы барабанов и синт-басы»: условия 57 и 58 (тест-кейс ТК93).
//   drumKits.js: DRUM_KITS [{id, name {ru,en}, note {ru,en}, parts}] — osdk (живой), tr808, tr909, linn, cr78,
//     simmons; parts: kick, snare — {kit}; toms — {kit (small), kit_mid, kit_low}; hh — {kit (closed),
//     kit_open, choke 1}; ride, crash — {kit}. DRUM_TREATMENTS [{id, name {ru,en}, chain}] — dry, room, gated,
//     tight, lofi.
//   trackDesk.js: rhythmSection(stemNames, presets, room, locale, kit = 'osdk') — room true/false или id
//     обработки; kit 'osdk' — прежние цепочки побайтно; другой набор — sampler с частями набора
//     (floor_db/dynamics/output_db — как у цепочек osdk той же части); бас — прежний bass-kit без обработки.
//   fxPresets.js: drums-<часть>-<набор> (group 'drum-kits', stems [часть]) для каждой машины; синт-басы —
//     блок bass с kit synthbass/*, group 'bass-kit', stems ['bass'] (туда же прежний bass-kit).
//   Уточнение ТК93 (условие 57а): обработка gated — reverb с gate_ms > 0, без блока gate.
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import blocks from './fxBlocks.json'
import { DRUM_KITS, DRUM_TREATMENTS } from './drumKits.js'
import { rhythmSection, ROOM } from './trackDesk.js'
import { fxPresets } from './fxPresets.js'
import { PRESET_GROUPS } from './presetGroups.js'

const KIT_IDS = ['osdk', 'tr808', 'tr909', 'linn', 'cr78', 'simmons']
const MACHINES = KIT_IDS.filter((k) => k !== 'osdk')
const DESK_PARTS = ['kick', 'snare', 'toms', 'hh', 'ride', 'crash']
// части наборов воркера (ТК90)
const SYNTH_PARTS = ['kick', 'snare', 'hh-closed', 'hh-open', 'ride', 'crash',
  'tom-small', 'tom-medium', 'tom-large', 'clap', 'rim', 'cowbell']
// прежние имена частей живого набора osdk (готовые «набором» до этапа 7б)
const OSDK_PARTS = {
  kick: { kit: 'osdk/kick' },
  snare: { kit: 'osdk/snare' },
  toms: { kit: 'osdk/tom-small', kit_mid: 'osdk/tom-medium', kit_low: 'osdk/tom-large' },
  hh: { kit: 'osdk/hh-closed', kit_open: 'osdk/hh-half', choke: 1 },
  ride: { kit: 'osdk/ride' },
  crash: { kit: 'osdk/crash' },
}
const machineParts = (id) => ({
  kick: { kit: `${id}/kick` },
  snare: { kit: `${id}/snare` },
  toms: { kit: `${id}/tom-small`, kit_mid: `${id}/tom-medium`, kit_low: `${id}/tom-large` },
  hh: { kit: `${id}/hh-closed`, kit_open: `${id}/hh-open`, choke: 1 },
  ride: { kit: `${id}/ride` },
  crash: { kit: `${id}/crash` },
})
// готовые «набором» до этапа 7б: дорожка → id пресета
const OLD_PRESET = {
  kick: 'drums-kick-kit', snare: 'drums-snare-kit', toms: 'drums-toms-kit', hh: 'drums-hh-kit',
  ride: 'drums-ride-kit', crash: 'drums-crash-kit', bass: 'bass-kit',
}
const preset = (id) => fxPresets.find((p) => p.id === id)
const kitOf = (id) => DRUM_KITS.find((k) => k.id === id)
const treatment = (id) => DRUM_TREATMENTS.find((t) => t.id === id)
const STEMS = ['kick', 'snare', 'hh', 'bass']
const ALL_STEMS = ['kick', 'snare', 'toms', 'hh', 'ride', 'crash', 'bass']
const clone = (v) => JSON.parse(JSON.stringify(v))

// как было до этапа 7б (ТК37): готовая цепочка, при комнате — ROOM в конце; подпись — имя пресета (+ комната)
function before(stems, room, locale = 'ru') {
  return ALL_STEMS.filter((s) => stems.includes(s)).map((stem) => {
    const p = preset(OLD_PRESET[stem])
    const drum = stem !== 'bass'
    const chain = clone(p.chain)
    if (room && drum) chain.push({ ...ROOM })
    const name = p.name[locale] || p.name.ru
    return { stem, chain, label: name + (room && drum ? (locale === 'en' ? ' + room' : ' + комната') : '') }
  })
}

function inRange(v, spec) {
  if (typeof v !== 'number' || !Number.isFinite(v)) return false
  if (spec.zero_off && v === 0) return true
  return v >= spec.min && v <= spec.max
}

describe('DRUM_KITS (ТК93, условие 57)', () => {
  it('6 наборов: живой osdk и пять машин', () => {
    expect(DRUM_KITS.map((k) => k.id).sort()).toEqual([...KIT_IDS].sort())
    expect(DRUM_KITS.length).toBe(6)
  })

  it.each(KIT_IDS)('%s: name и note — ru и en', (id) => {
    const k = kitOf(id)
    for (const f of ['name', 'note']) {
      for (const lang of ['ru', 'en']) {
        expect(typeof k[f]?.[lang], `${f}.${lang}`).toBe('string')
        expect(k[f][lang].trim()).not.toBe('')
      }
    }
  })

  it.each(KIT_IDS)('%s: все 6 частей пульта', (id) => {
    expect(Object.keys(kitOf(id).parts).sort()).toEqual([...DESK_PARTS].sort())
  })

  it('osdk — прежние имена частей', () => {
    expect(kitOf('osdk').parts).toEqual(OSDK_PARTS)
  })

  it.each(MACHINES)('%s: части — «<id>/<часть>» воркера, тамы по размеру, хэт закрытый/открытый с choke', (id) => {
    const parts = kitOf(id).parts
    expect(parts).toEqual(machineParts(id))
    for (const p of Object.values(parts)) {
      for (const key of ['kit', 'kit_mid', 'kit_low', 'kit_open']) {
        if (p[key] === undefined) continue
        const [kit, part] = p[key].split('/')
        expect(kit).toBe(id)
        expect(SYNTH_PARTS).toContain(part)
      }
    }
  })
})

describe('DRUM_TREATMENTS (условие 57)', () => {
  it('обработки: сухо, комната, гейт-реверб, плотно, лоуфай', () => {
    expect(DRUM_TREATMENTS.map((t) => t.id).sort()).toEqual(['dry', 'gated', 'lofi', 'room', 'tight'])
  })

  it.each(['dry', 'room', 'gated', 'tight', 'lofi'])('%s: name ru/en, chain — список известных блоков в диапазоне', (id) => {
    const t = treatment(id)
    for (const lang of ['ru', 'en']) {
      expect(typeof t.name?.[lang]).toBe('string')
      expect(t.name[lang].trim()).not.toBe('')
    }
    expect(Array.isArray(t.chain)).toBe(true)
    for (const b of t.chain) {
      expect(Object.keys(blocks), `тип ${b.type}`).toContain(b.type)
      const spec = Object.fromEntries((blocks[b.type].params || []).map((p) => [p.id, p]))
      for (const [k, v] of Object.entries(b)) {
        if (k === 'type' || typeof v !== 'number') continue
        expect(spec[k], `${id}: ${b.type}.${k} нет в описании`).toBeDefined()
        expect(inRange(v, spec[k]), `${id}: ${b.type}.${k}=${v}`).toBe(true)
      }
    }
  })

  it('сухо — пустая цепочка; комната — прежняя ROOM', () => {
    expect(treatment('dry').chain).toEqual([])
    expect(treatment('room').chain).toEqual([ROOM])
  })

  it('гейт-реверб 80-х: reverb с обрезанным хвостом (gate_ms > 0), без блока gate (уточнение ТК93, условие 57а)', () => {
    const c = treatment('gated').chain
    const rv = c.filter((b) => b.type === 'reverb')
    expect(rv.length).toBeGreaterThan(0)
    for (const b of rv) {
      expect(typeof b.gate_ms).toBe('number')
      expect(b.gate_ms).toBeGreaterThan(0)
    }
    expect(c.some((b) => b.type === 'gate')).toBe(false)
  })

  it('плотно: компрессор и параллельная грязь (drive с mix < 1)', () => {
    const c = treatment('tight').chain
    expect(c.some((b) => b.type === 'comp' || b.type === 'glue')).toBe(true)
    expect(c.some((b) => b.type === 'drive' && (b.mix ?? 1) < 1)).toBe(true)
  })

  it('лоуфай: лента и срез верха', () => {
    const c = treatment('lofi').chain
    expect(c.some((b) => b.type === 'tape')).toBe(true)
    expect(c.some((b) => b.type === 'eq' && (b.lowpass_hz ?? 0) > 0)).toBe(true)
  })
})

describe('rhythmSection с набором и обработкой (ТК93)', () => {
  it('без набора, сухо — побайтно как раньше', () => {
    expect(rhythmSection(STEMS, fxPresets, false)).toEqual(before(STEMS, false))
  })

  it('без набора, с комнатой — как раньше', () => {
    expect(rhythmSection(STEMS, fxPresets, true)).toEqual(before(STEMS, true))
  })

  it('kit osdk явно — как раньше; en-подпись как раньше', () => {
    expect(rhythmSection(STEMS, fxPresets, false, 'ru', 'osdk')).toEqual(before(STEMS, false))
    expect(rhythmSection(ALL_STEMS, fxPresets, true, 'en', 'osdk')).toEqual(before(ALL_STEMS, true, 'en'))
  })

  it('id обработки room/dry — те же цепочки, что true/false', () => {
    const chains = (list) => list.map((r) => [r.stem, r.chain])
    expect(chains(rhythmSection(ALL_STEMS, fxPresets, 'room'))).toEqual(chains(before(ALL_STEMS, true)))
    expect(chains(rhythmSection(ALL_STEMS, fxPresets, 'dry'))).toEqual(chains(before(ALL_STEMS, false)))
  })

  it('tr808 сухо: части — sampler набора с параметрами osdk той же части; бас — bass-kit', () => {
    const out = rhythmSection(ALL_STEMS, fxPresets, false, 'ru', 'tr808')
    expect(out.map((r) => r.stem)).toEqual(ALL_STEMS)
    const parts = machineParts('tr808')
    for (const r of out.filter((x) => x.stem !== 'bass')) {
      const old = preset(OLD_PRESET[r.stem]).chain[0]
      expect(r.chain.length, r.stem).toBe(1)
      const b = r.chain[0]
      expect(b.type).toBe('sampler')
      expect(b).toMatchObject(parts[r.stem])
      // усл. 84 (этап 10): у живого хэта громкость 0 (его поднимает эквалайзер), у хэта машин — прежние +4
      const want = (k) => (r.stem === 'hh' && k === 'output_db' ? 4 : old[k])
      for (const k of ['floor_db', 'dynamics', 'output_db']) expect(b[k], `${r.stem}.${k}`).toEqual(want(k))
      for (const k of ['kit', 'kit_mid', 'kit_low', 'kit_open', 'choke']) expect(b[k], `${r.stem}.${k}`).toEqual(parts[r.stem][k])
    }
    expect(out.find((r) => r.stem === 'kick').chain[0].kit).toBe('tr808/kick')
    const hh = out.find((r) => r.stem === 'hh').chain[0]
    expect(hh).toMatchObject({ kit: 'tr808/hh-closed', kit_open: 'tr808/hh-open', choke: 1 })
    expect(out.find((r) => r.stem === 'bass').chain).toEqual(preset('bass-kit').chain)
  })

  it('tr808 + гейт-реверб: к частям барабанов в конец цепочка обработки, к басу — нет', () => {
    const out = rhythmSection(STEMS, fxPresets, 'gated', 'ru', 'tr808')
    const gated = treatment('gated').chain
    for (const r of out.filter((x) => x.stem !== 'bass')) {
      expect(r.chain.slice(-gated.length)).toEqual(gated)
      expect(r.chain[0]).toMatchObject({ type: 'sampler', kit: machineParts('tr808')[r.stem].kit })
      const rv = r.chain.findIndex((b) => b.type === 'reverb')
      expect(rv).toBeGreaterThan(0)
      expect(r.chain[rv].gate_ms).toBeGreaterThan(0)
      // обработка не добавляет gate (свой gate цепочки части до сэмплера/реверба — не обработка)
      expect(r.chain.slice(rv + 1).some((b) => b.type === 'gate')).toBe(false)
    }
    const bass = out.find((r) => r.stem === 'bass').chain
    expect(bass).toEqual(preset('bass-kit').chain)
    expect(bass.some((b) => b.type === 'reverb' || b.type === 'gate')).toBe(false)
  })

  it.each(['tight', 'lofi'])('osdk + %s: прежняя цепочка части, за ней обработка', (id) => {
    const out = rhythmSection(STEMS, fxPresets, id, 'ru', 'osdk')
    const t = treatment(id).chain
    for (const r of out) {
      const old = preset(OLD_PRESET[r.stem]).chain
      if (r.stem === 'bass') expect(r.chain).toEqual(old)
      else expect(r.chain).toEqual([...old, ...t])
    }
  })

  it('неизвестная обработка — как «сухо»', () => {
    const chains = (list) => list.map((r) => [r.stem, r.chain])
    expect(chains(rhythmSection(STEMS, fxPresets, 'nope', 'ru', 'tr909')))
      .toEqual(chains(rhythmSection(STEMS, fxPresets, 'dry', 'ru', 'tr909')))
    expect(chains(rhythmSection(STEMS, fxPresets, 'nope'))).toEqual(chains(before(STEMS, false)))
  })

  it('подпись — имя набора; en — по-английски, ru — с именем набора по-русски', () => {
    const en = rhythmSection(ALL_STEMS, fxPresets, 'gated', 'en', 'linn')
    for (const r of en) {
      expect(typeof r.label).toBe('string')
      expect(r.label, r.label).not.toMatch(/[а-яё]/i)
      if (r.stem !== 'bass') expect(r.label).toContain(kitOf('linn').name.en)
    }
    const ru = rhythmSection(ALL_STEMS, fxPresets, 'gated', 'ru', 'linn')
    for (const r of ru.filter((x) => x.stem !== 'bass')) expect(r.label).toContain(kitOf('linn').name.ru)
    // подписи частей различаются (имя части в подписи)
    const labels = ru.filter((x) => x.stem !== 'bass').map((r) => r.label)
    expect(new Set(labels).size).toBe(labels.length)
  })

  it('готовые не мутируются', () => {
    const snap = clone(fxPresets)
    rhythmSection(ALL_STEMS, fxPresets, 'gated', 'ru', 'tr808')
    rhythmSection(ALL_STEMS, fxPresets, true)
    expect(fxPresets).toEqual(snap)
  })

  it('нет частей трека → []', () => {
    expect(rhythmSection(['vocals', 'other'], fxPresets, 'gated', 'ru', 'tr808')).toEqual([])
  })
})

describe('fxPresets: готовые по частям наборов и синт-басы (условия 57, 58)', () => {
  it('группы drum-kits и bass-kit есть в PRESET_GROUPS с подписями карточки', () => {
    expect(PRESET_GROUPS['drum-kits']?.ru).toBe('Барабаны: наборы')
    expect(PRESET_GROUPS['bass-kit']?.ru).toBe('Бас: замена нот сэмплами')
    for (const g of ['drum-kits', 'bass-kit']) {
      expect(typeof PRESET_GROUPS[g].en).toBe('string')
      expect(PRESET_GROUPS[g].en.trim()).not.toBe('')
    }
  })

  const pairs = MACHINES.flatMap((kit) => DESK_PARTS.map((part) => [kit, part]))
  it.each(pairs)('%s / %s: пресет drums-<часть>-<набор>, group drum-kits, stems [часть], sampler набора', (kit, part) => {
    const p = preset(`drums-${part}-${kit}`)
    expect(p, `нет drums-${part}-${kit}`).toBeDefined()
    expect(p.group).toBe('drum-kits')
    expect(p.stems).toEqual([part])
    const s = p.chain.find((b) => b.type === 'sampler')
    expect(s).toBeDefined()
    expect(s).toMatchObject(machineParts(kit)[part])
  })

  it('синт-басы: moog, sub808, acid — блок bass с kit synthbass/*, group bass-kit, stems [bass]', () => {
    for (const part of ['moog', 'sub808', 'acid']) {
      const list = fxPresets.filter((p) => p.chain.some((b) => b.type === 'bass' && b.kit === `synthbass/${part}`))
      expect(list.length, part).toBeGreaterThanOrEqual(1)
      for (const p of list) {
        expect(p.group, p.id).toBe('bass-kit')
        expect(p.stems, p.id).toEqual(['bass'])
      }
    }
  })

  it('прежний bass-kit — в группе bass-kit', () => {
    expect(preset('bass-kit').group).toBe('bass-kit')
  })
})
