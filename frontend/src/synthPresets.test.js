// Тесты карточки internal-own-track, этап 7в «Клавиши и синты»: условия 63–64 (тест-кейс ТК98).
//   ТК98 — synth-пресеты (stems ['synth'] с блоком synth): у каждого group из PRESET_GROUPS — одна из
//     synth-pad, synth-organ, synth-keys, synth-lead, synth-toy; синтов не меньше 20; среди них — с kit
//     «salamander/piano» и «salamander/piano-soft», с osc1 5 (FM) не меньше 4, с tremolo stereo > 0 (Leslie)
//     не меньше 1; прежние 8 id на месте; groupPresets по синтам даёт 5 групп.
//   Условие 63: указание автора набора Salamander (Alexander Holm, CC-BY) — в подсказке пресета с этим набором.
// Диапазоны параметров всех пресетов проверяет fxPresets.test.js (describe.each по fxPresets).
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { fxPresets } from './fxPresets.js'
import { PRESET_GROUPS, groupPresets } from './presetGroups.js'

const SYNTH_GROUPS = ['synth-pad', 'synth-organ', 'synth-keys', 'synth-lead', 'synth-toy']
const OLD_IDS = ['synth-solina', 'synth-juno', 'synth-moog-bass', 'synth-moog-lead', 'synth-cs80-brass',
  'synth-farfisa', 'synth-vox-continental', 'synth-vltone']

const synthBlock = (p) => p.chain.find((b) => b.type === 'synth')
const synths = fxPresets.filter((p) => JSON.stringify(p.stems) === JSON.stringify(['synth']) && synthBlock(p))
const withKit = (kit) => synths.filter((p) => synthBlock(p).kit === kit)

describe('ТК98: группы синтов (условие 64)', () => {
  it.each(SYNTH_GROUPS)('группа %s есть в PRESET_GROUPS с подписями ru/en', (g) => {
    expect(PRESET_GROUPS[g]).toBeDefined()
    expect(PRESET_GROUPS[g].ru).toBeTruthy()
    expect(PRESET_GROUPS[g].en).toBeTruthy()
  })

  it('не меньше 20 синтов', () => {
    expect(synths.length).toBeGreaterThanOrEqual(20)
  })

  it.each(synths.map((p) => [p.id, p]))('%s: group — одна из групп синтов', (_, p) => {
    expect(SYNTH_GROUPS).toContain(p.group)
  })

  it('groupPresets по синтам — ровно 5 групп синтов, каждая непустая', () => {
    const groups = groupPresets(synths)
    expect(groups.map((g) => g.group).sort()).toEqual([...SYNTH_GROUPS].sort())
    for (const g of groups) {
      expect(g.items.length).toBeGreaterThan(0)
      expect(g.label).toBe(PRESET_GROUPS[g.group].ru)
    }
  })

  it.each(OLD_IDS)('прежний синт %s на месте', (id) => {
    expect(synths.map((p) => p.id)).toContain(id)
  })
})

describe('ТК98: состав синтов (условия 61–64)', () => {
  it.each(['salamander/piano', 'salamander/piano-soft'])('есть синт с kit %s', (kit) => {
    expect(withKit(kit).length).toBeGreaterThanOrEqual(1)
  })

  it('не меньше 4 синтов на FM-генераторе (osc1 5)', () => {
    expect(synths.filter((p) => synthBlock(p).osc1 === 5).length).toBeGreaterThanOrEqual(4)
  })

  it('есть орган с вращающимся динамиком: tremolo stereo > 0', () => {
    const leslie = synths.filter((p) => p.chain.some((b) => b.type === 'tremolo' && b.stereo > 0))
    expect(leslie.length).toBeGreaterThanOrEqual(1)
  })
})

describe('Условие 63: автор набора Salamander в подсказке пресета', () => {
  const pianos = synths.filter((p) => String(synthBlock(p).kit || '').startsWith('salamander/'))

  it('пресеты с набором salamander есть', () => {
    expect(pianos.length).toBeGreaterThanOrEqual(2)
  })

  it.each(pianos.map((p) => [p.id, p]))('%s: note ru/en называет автора и лицензию', (_, p) => {
    for (const loc of ['ru', 'en']) {
      const note = p.note?.[loc] || ''
      expect(note, `${p.id} note.${loc}`).toMatch(/Alexander Holm/)
      expect(note, `${p.id} note.${loc}`).toMatch(/CC[- ]?BY/i)
    }
  })
})
