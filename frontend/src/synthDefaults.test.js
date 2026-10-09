// Тесты карточки internal-own-track, условие 37 «Синт слышен» (тест-кейс ТК63):
// готовые пэды (style pad: Solina, Juno, CS-80, Farfisa, Vox) — octave 1 (на октаву выше гитар);
// Solina — cutoff_hz 6000 у блока synth; rel_db блока synth по умолчанию −6.
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { fxPresets } from './fxPresets.js'
import BLOCKS from './fxBlocks.json'

const synths = fxPresets.filter((p) => (p.stems || []).includes('synth'))
const pads = synths.filter((p) => p.style === 'pad')

describe('синт слышен: умолчания (ТК63)', () => {
  it('пэдов пять (Solina, Juno, CS-80, Farfisa, Vox)', () => {
    expect(pads.length).toBe(5)
  })

  it.each(pads.map((p) => [p.id, p]))('%s: пэд — octave 1', (_id, p) => {
    expect(p.octave).toBe(1)
  })

  it('Solina — cutoff_hz 6000 у блока synth', () => {
    const solina = fxPresets.find((p) => p.id === 'synth-solina')
    expect(solina).toBeDefined()
    const synth = solina.chain.find((b) => b.type === 'synth')
    expect(synth).toBeDefined()
    expect(synth.cutoff_hz).toBe(6000)
  })

  it('fxBlocks: synth rel_db по умолчанию −6', () => {
    const rel = BLOCKS.synth.params.find((p) => p.id === 'rel_db')
    expect(rel).toBeDefined()
    expect(rel.default).toBe(-6)
  })
})
