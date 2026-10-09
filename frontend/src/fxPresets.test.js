// Тесты карточки internal-instruments-page, тест-кейс 3 (условие 3): готовые цепочки
// движка fxPresets.js — [{id, name: {ru, en}, note: {ru, en}, chain}], chain — JSON
// воркера. Каждый пресет проходит проверку по описанию блоков воркера: снимок SPEC —
// fxBlocks.json (побайтная копия worker/fx_blocks.json, паритет сверяет тест воркера).
// Написаны по карточке и контракту, без чтения реализации.
import { describe, it, expect } from 'vitest'
import blocks from './fxBlocks.json'
import { fxPresets } from './fxPresets.js'

const cases = fxPresets.map((p) => [p.id, p])

// проверка числа по описанию параметра: [min, max] или 0 при zero_off
function inRange(v, spec) {
  if (typeof v !== 'number' || !Number.isFinite(v)) return false
  if (spec.zero_off && v === 0) return true
  return v >= spec.min && v <= spec.max
}

describe('fxPresets: набор', () => {
  it('минимум пять готовых цепочек (гитара-перегруз, гитара-чистая, голос, барабаны, мастер)', () => {
    expect(Array.isArray(fxPresets)).toBe(true)
    expect(fxPresets.length).toBeGreaterThanOrEqual(5)
  })

  it('id уникальны и непусты', () => {
    const ids = fxPresets.map((p) => p.id)
    for (const id of ids) {
      expect(typeof id).toBe('string')
      expect(id.trim()).not.toBe('')
    }
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('есть гитара-перегруз: NAM (amp), за ним лёгкий кабинет (cab со встроенным IR)', () => {
    const ok = fxPresets.some((p) => {
      const ai = p.chain.findIndex((b) => b.type === 'amp')
      if (ai < 0) return false
      return p.chain.slice(ai + 1).some((b) => b.type === 'cab' && (b.ir ?? '') === '')
    })
    expect(ok).toBe(true)
  })
})

describe.each(cases)('пресет %s', (_, p) => {
  it('name и note — ru и en, непустые', () => {
    for (const f of ['name', 'note']) {
      for (const lang of ['ru', 'en']) {
        expect(typeof p[f]?.[lang], `${f}.${lang}`).toBe('string')
        expect(p[f][lang].trim(), `${f}.${lang}`).not.toBe('')
      }
    }
  })

  it('цепочка — непустой список известных блоков', () => {
    expect(Array.isArray(p.chain)).toBe(true)
    expect(p.chain.length).toBeGreaterThan(0)
    for (const b of p.chain) expect(Object.keys(blocks), `тип ${b.type}`).toContain(b.type)
  })

  it('каждый параметр есть в описании блока, значение — в диапазоне и нужного вида', () => {
    for (const [i, b] of p.chain.entries()) {
      const d = blocks[b.type]
      const nums = Object.fromEntries(d.params.map((x) => [x.id, x]))
      const strs = Object.fromEntries((d.strings || []).map((x) => [x.id, x]))
      for (const [k, v] of Object.entries(b)) {
        if (k === 'type') continue
        const where = `блок ${i + 1} (${b.type}).${k}=${JSON.stringify(v)}`
        if (k === 'bands') {
          expect(d.bands, `${where}: полосы только у eq`).toBeTruthy()
          continue
        }
        if (k in nums) {
          expect(inRange(v, nums[k]), `${where} вне [${nums[k].min}, ${nums[k].max}]`).toBe(true)
        } else if (k in strs) {
          expect(typeof v, where).toBe('string')
        } else {
          throw new Error(`${where}: параметра нет в описании блока`)
        }
      }
    }
  })

  it('полосы eq: известные поля, значения в диапазонах', () => {
    for (const b of p.chain.filter((x) => x.bands !== undefined)) {
      expect(b.type).toBe('eq')
      expect(Array.isArray(b.bands)).toBe(true)
      for (const band of b.bands) {
        for (const [k, v] of Object.entries(band)) {
          const spec = blocks.eq.bands[k]
          expect(spec, `поле полосы ${k}`).toBeTruthy()
          expect(inRange(v, spec), `полоса ${k}=${v}`).toBe(true)
        }
      }
    }
  })

  // захваты NAM в поставку не входят (карточка, условие 3): у пресета обязательная строка —
  // ключ есть и это строка; пустая значит «выберите захват», страница подставит загруженный
  it('обязательные строки есть ключом (у amp — model, пустая — выбор захвата)', () => {
    for (const b of p.chain) {
      for (const s of blocks[b.type].strings || []) {
        if (!s.required) continue
        expect(typeof b[s.id], `${b.type}.${s.id}`).toBe('string')
      }
    }
  })
})

// Тесты карточки internal-studio-engine, условия 13–14. Условие 13: вместо «Гитара: перегруз
// (NAM)» (id guitar-crunch) — три гитарные цепочки: чистый усилитель + реверб (вход −6),
// перегруз погорячее (вход +12), Vox и пружина (вход +6, пружинный реверб); amp.model пустой
// (захват выбирает пользователь). Условие 14: готовые цепочки «Бочка: набор», «Малый: набор»
// с блоком sampler (kit — строка), блок sampler есть в описании блоков.
describe('fxPresets: гитары (условие 13) и наборы барабанов (условие 14)', () => {
  const amps = (p) => p.chain.filter((b) => b.type === 'amp')
  const ampPresets = fxPresets.filter((p) => amps(p).length > 0)
  const byInput = (db) => ampPresets.filter((p) => amps(p).some((b) => b.input_db === db))

  it('пресета guitar-crunch больше нет', () => {
    expect(fxPresets.map((p) => p.id)).not.toContain('guitar-crunch')
  })

  it('три гитарных пресета с усилителем: вход −6, +12 и +6 — разные цепочки', () => {
    const picked = [-6, 12, 6].map((db) => {
      const found = byInput(db)
      expect(found.length, `нет пресета с amp.input_db = ${db}`).toBeGreaterThan(0)
      return found[0].id
    })
    expect(new Set(picked).size).toBe(3)
  })

  it('у гитарных пресетов amp.model пустой — захват выбирает пользователь', () => {
    for (const db of [-6, 12, 6]) {
      for (const p of byInput(db)) {
        for (const b of amps(p)) expect(b.model, `${p.id}: amp.model`).toBe('')
      }
    }
  })

  it('чистый (вход −6) и Vox (вход +6) — с реверберацией после усилителя', () => {
    for (const db of [-6, 6]) {
      const ok = byInput(db).some((p) => {
        const ai = p.chain.findIndex((b) => b.type === 'amp')
        return p.chain.slice(ai + 1).some((b) => b.type === 'reverb')
      })
      expect(ok, `пресет с amp.input_db = ${db} без reverb после amp`).toBe(true)
    }
  })

  it('блок sampler есть в описании блоков: kit — строка-набор, обязательная', () => {
    expect(blocks.sampler, 'нет блока sampler в fxBlocks.json').toBeTruthy()
    const kit = (blocks.sampler.strings || []).find((s) => s.id === 'kit')
    expect(kit, 'нет строки kit').toBeTruthy()
    expect(kit.asset).toBe('kit')
    expect(kit.required).toBe(true)
  })

  it('готовые цепочки «Бочка: набор» и «Малый: набор» — с sampler, kit — строка', () => {
    const samplers = fxPresets.filter((p) => p.chain.some((b) => b.type === 'sampler'))
    expect(samplers.length).toBeGreaterThanOrEqual(2)
    for (const want of ['Бочка', 'Малый']) {
      expect(samplers.some((p) => p.name.ru.startsWith(want)), `нет пресета «${want}: набор»`).toBe(true)
    }
    for (const p of samplers) {
      for (const b of p.chain.filter((x) => x.type === 'sampler')) {
        expect(typeof b.kit, `${p.id}: sampler.kit`).toBe('string')
        // kit — «<набор>/<часть>» либо пусто (выбор набора на странице)
        if (b.kit !== '') expect(b.kit, `${p.id}: sampler.kit`).toMatch(/^[^/]+\/[^/]+$/)
      }
    }
  })
})

// Карточка internal-own-track, этап 0, условие 3 (тест-кейс ТК12): готовые цепочки знают
// дорожки — у каждого пресета поле stems (список дорожек), кроме мастер-склейки.
describe('fxPresets: дорожки пресетов (stems)', () => {
  // 'synth' — готовые синты поверх трека (internal-own-track, этап 4, условие 26)
  // 'perc' — готовые перкуссии поверх трека (этап 5, условие 33)
  const KNOWN = ['vocals', 'drums', 'kick', 'snare', 'toms', 'hh', 'ride', 'crash', 'bass', 'guitar', 'piano', 'other', 'synth', 'perc']
  const MASTER = 'master-glue'
  const byId = (id) => fxPresets.find((p) => p.id === id)

  it('мастер-склейка есть и она без stems — подходит всем', () => {
    expect(byId(MASTER), 'нет пресета master-glue').toBeTruthy()
    expect(byId(MASTER).stems).toBeUndefined()
  })

  it.each(fxPresets.filter((p) => p.id !== MASTER).map((p) => [p.id, p]))(
    'пресет %s: stems — непустой список известных дорожек', (_, p) => {
      expect(Array.isArray(p.stems), `${p.id}: stems не список`).toBe(true)
      expect(p.stems.length).toBeGreaterThan(0)
      for (const s of p.stems) expect(KNOWN, `${p.id}: неизвестная дорожка ${s}`).toContain(s)
    })

  it.each(['guitar', 'vocals', 'drums', 'kick', 'snare', 'hh', 'bass'])(
    'для дорожки %s есть хотя бы один свой пресет', (stem) => {
      expect(fxPresets.some((p) => (p.stems || []).includes(stem))).toBe(true)
    })

  // соответствие из условия 3: гитары → guitar/other; голос → vocals; барабаны-комната → drums;
  // бочка → kick; малый → snare; хэт → hh; райд → ride; крэш → crash; бас → bass; синт → other/piano
  it.each([
    ['guitar-amp-clean', ['guitar', 'other']],
    ['guitar-amp-hot', ['guitar', 'other']],
    ['guitar-amp-spring', ['guitar', 'other']],
    ['guitar-clean', ['guitar', 'other']],
    ['vocal-plate', ['vocals']],
    ['drums-room', ['drums']],
    ['drums-kick-kit', ['kick']],
    ['drums-snare-kit', ['snare']],
    ['drums-hh-kit', ['hh']],
    ['drums-ride-kit', ['ride']],
    ['drums-crash-kit', ['crash']],
    ['bass-kit', ['bass']],
    ['synth-delay', ['other', 'piano']],
  ])('пресет %s — дорожки %j', (id, want) => {
    const p = byId(id)
    expect(p, `нет пресета ${id}`).toBeTruthy()
    expect([...(p.stems || [])].sort()).toEqual([...want].sort())
  })
})

// Карточка internal-own-track, этап 4 «Синты», условие 26 (тест-кейс ТК52): восемь готовых
// синтов со stems ['synth'], у каждого стиль по умолчанию (pad/pulse/arp/drone) и октава
// (−2…+2), цепочка начинается с блока synth. Написаны по карточке, без чтения реализации.
describe('fxPresets: готовые синты (ТК52)', () => {
  const NAMES = [
    'Струнный ансамбль (Solina)', 'Пэд с хорусом (Juno)', 'Синт-бас (Moog)', 'Лид (Moog)',
    'Медь (CS-80)', 'Орган (Farfisa)', 'Орган (Vox Continental)', 'Игрушка (VL-Tone)',
  ]
  const synths = fxPresets.filter((p) => (p.stems || []).includes('synth'))

  it('восемь синтов со stems [synth]', () => {
    expect(synths.length).toBe(8)
    for (const p of synths) expect(p.stems, p.id).toEqual(['synth'])
  })

  it('названия — из карточки', () => {
    expect(synths.map((p) => p.name.ru).sort()).toEqual([...NAMES].sort())
  })

  it.each(synths.map((p) => [p.id, p]))('%s: style и octave', (_, p) => {
    expect(['pad', 'pulse', 'arp', 'drone']).toContain(p.style)
    expect(Number.isInteger(p.octave), `${p.id}: octave ${p.octave}`).toBe(true)
    expect(p.octave).toBeGreaterThanOrEqual(-2)
    expect(p.octave).toBeLessThanOrEqual(2)
  })

  it.each(synths.map((p) => [p.id, p]))('%s: цепочка начинается с synth', (_, p) => {
    expect(p.chain[0].type).toBe('synth')
  })

  it('синты не подсовываются обычным дорожкам: у прочих пресетов нет synth в цепочке', () => {
    for (const p of fxPresets.filter((x) => !(x.stems || []).includes('synth'))) {
      expect(p.chain.some((b) => b.type === 'synth'), p.id).toBe(false)
    }
  })
})
