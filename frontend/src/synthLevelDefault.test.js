// Тесты карточки internal-own-track, этап 9, условие 80 (тест-кейс ТК117, часть фронта):
// умолчание rel_db синта −8 (было −6) — блок synth в описании блоков, студия «Синт по аккордам»
// (запасное значение ползунка, когда в цепочке нет блока synth) и подписи; у perc прежний −14.
// Побайтная сверка копий fx_blocks (фронт, MCP) с worker/fx_blocks.json уже есть в tails.test.js (ТК83) —
// здесь не дублируется. Написаны по карточке, без чтения новой реализации.
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import BLOCKS from './fxBlocks.json'
import ru from './i18n/ru.js'
import en from './i18n/en.js'

const src = readFileSync(new URL('./components/StudioSynth.vue', import.meta.url), 'utf8')
const def = (type, id) => (BLOCKS[type].params.find((p) => p.id === id) || {}).default

describe('умолчание rel_db синта −8 (ТК117, условие 80)', () => {
  it('fxBlocks: synth rel_db по умолчанию −8', () => {
    expect(def('synth', 'rel_db')).toBe(-8)
  })

  it('fxBlocks: perc rel_db по умолчанию прежний −14', () => {
    expect(def('perc', 'rel_db')).toBe(-14)
  })

  it('StudioSynth: запасное значение громкости партии (нет блока synth) — −8', () => {
    // запасное значение rel_db по исходнику: «… params.rel_db : N», «?? N» или «|| N»
    const found = [...src.matchAll(/rel_db\s*(?:\?\?|\|\||:)\s*(-?\d+(?:\.\d+)?)/g)].map((m) => Number(m[1]))
    expect(found.length).toBeGreaterThan(0)
    for (const v of found) expect(v).toBe(-8)
  })

  it.each([['ru', ru], ['en', en]])('%s: подпись громкости синта — про −8, не −6', (_l, dict) => {
    const tip = dict['synth.level.tip']
    expect(tip).toBeDefined()
    expect(tip).toMatch(/[−-]8/)
    expect(tip).not.toMatch(/[−-]6(?!\d)/)
  })
})
