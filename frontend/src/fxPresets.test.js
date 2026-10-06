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
