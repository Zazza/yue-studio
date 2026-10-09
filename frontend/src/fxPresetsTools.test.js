// Тесты карточки internal-own-track, этап 7а «Гитары и бас»: условия 51–53 (тест-кейсы ТК86, ТК87).
//   ТК86 — готовые гитары без захвата (≥ 10, stems guitar/other, без amp, без файлов) и басы
//     (≥ 4, stems ['bass'], без sampler/bass); у пресетов с amp — непустой amp_hint; group — из
//     PRESET_GROUPS (presetGroups.js). Диапазоны параметров всех пресетов проверяет fxPresets.test.js
//     (describe.each по всему fxPresets) — новые пресеты попадают туда автоматически.
//   ТК87 — fillAmp(chain, amps, hints) из fxChain.js: подстановка захвата по подсказкам.
// Пресеты 7а опознаются по полю group и stems, а не по id (id заданы карточкой только у старых гитар).
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import blocks from './fxBlocks.json'
import { fxPresets } from './fxPresets.js'
import { fromWorkerChain, fillAmp } from './fxChain.js'
import { PRESET_GROUPS } from './presetGroups.js'

const has = (p, type) => p.chain.some((b) => b.type === type)
const blocksOf = (p, type) => p.chain.filter((b) => b.type === type)
const grouped = fxPresets.filter((p) => p.group !== undefined)

// «работает на голом воркере»: ни одной ссылки на загружаемый файл (захват, IR, набор)
function needsFiles(p) {
  for (const b of p.chain) {
    for (const s of blocks[b.type]?.strings || []) {
      if (!s.asset) continue
      if (s.required) return true                  // amp.model, sampler.kit, bass.kit
      if ((b[s.id] ?? '') !== '') return true      // cab.ir / reverb.ir с файлом
    }
  }
  return false
}

const guitars = grouped.filter((p) => (p.stems || []).includes('guitar') && !has(p, 'amp'))
const basses = grouped.filter((p) => JSON.stringify(p.stems) === JSON.stringify(['bass']))

describe('ТК86: готовые гитары без захвата (условие 51)', () => {
  it('не меньше 10 гитар с group, stems ⊇ guitar, без блока amp', () => {
    expect(guitars.length).toBeGreaterThanOrEqual(10)
  })

  it.each(guitars.map((p) => [p.id, p]))('%s: stems — guitar/other, работает без загруженных файлов', (_, p) => {
    for (const s of p.stems) expect(['guitar', 'other'], `${p.id}: дорожка ${s}`).toContain(s)
    expect(needsFiles(p), `${p.id}: цепочка требует файл (захват/IR/набор)`).toBe(false)
  })

  // характерные приёмы из списка условия 51 — каждый есть хотя бы у одной готовой гитары
  it.each([
    ['пост-панк: хорус и дилей', (p) => has(p, 'chorus') && has(p, 'delay')],
    ['сёрф: пружина и тремоло', (p) => has(p, 'spring') && has(p, 'tremolo')],
    ['шугейз: флэнжер и зал', (p) => has(p, 'flanger') && has(p, 'reverb')],
    ['индастриал-метал: гейт', (p) => has(p, 'gate')],
    ['альт-рок: фейзер', (p) => has(p, 'phaser')],
    ['лоуфай-кассета: лента', (p) => has(p, 'tape')],
    ['тремоло-пульс в стерео', (p) => blocksOf(p, 'tremolo').some((b) => (b.stereo ?? 0) > 0)],
  ])('есть гитара «%s»', (_, pred) => {
    expect(guitars.some(pred)).toBe(true)
  })
})

describe('ТК86: готовые басы (условие 51)', () => {
  it('не меньше 4 басов с group, stems [bass]', () => {
    expect(basses.length).toBeGreaterThanOrEqual(4)
  })

  it.each(basses.map((p) => [p.id, p]))('%s: только звук — без sampler/bass (ноты не меняет)', (_, p) => {
    expect(has(p, 'sampler'), p.id).toBe(false)
    expect(has(p, 'bass'), p.id).toBe(false)
  })

  it.each([
    ['мелодичный с хорусом', (p) => has(p, 'chorus')],
    ['фузз: параллельный перегруз (mix < 1 — низ сохраняется)', (p) => blocksOf(p, 'drive').some((b) => (b.mix ?? 1) < 1)],
    ['медиатор: сжатый', (p) => has(p, 'comp') || has(p, 'glue')],
    ['даб: без верха (срез верха)', (p) => blocksOf(p, 'eq').some((b) => (b.lowpass_hz ?? 0) > 0)],
  ])('есть бас «%s»', (_, pred) => {
    expect(basses.some(pred)).toBe(true)
  })
})

describe('ТК86: group и названия (условия 51, 53, 53а)', () => {
  it('готовых с group есть (гитары и басы 7а)', () => {
    expect(grouped.length).toBeGreaterThanOrEqual(14)
  })

  it.each(grouped.map((p) => [p.id, p]))('%s: group — id из PRESET_GROUPS', (_, p) => {
    expect(typeof p.group).toBe('string')
    expect(Object.keys(PRESET_GROUPS), `${p.id}: группа ${p.group}`).toContain(p.group)
  })

  // этап 7: «название в списке — по звуку/модели, без имён групп»; в note «в духе …» можно
  it.each(grouped.map((p) => [p.id, p]))('%s: в названии нет имён групп', (_, p) => {
    const bands = /rammstein|nightwish|placebo|radiohead|new order|joy division|hook|хук/i
    for (const lang of ['ru', 'en']) expect(p.name[lang], `${p.id}: name.${lang}`).not.toMatch(bands)
  })
})

describe('ТК86: amp_hint у пресетов с усилителем (условие 52)', () => {
  const withAmp = fxPresets.filter((p) => has(p, 'amp'))

  it('пресеты с усилителем есть', () => {
    expect(withAmp.length).toBeGreaterThan(0)
  })

  it.each(withAmp.map((p) => [p.id, p]))('%s: amp_hint — непустой массив непустых строк', (_, p) => {
    expect(Array.isArray(p.amp_hint), `${p.id}: amp_hint`).toBe(true)
    expect(p.amp_hint.length).toBeGreaterThan(0)
    for (const h of p.amp_hint) {
      expect(typeof h).toBe('string')
      expect(h.trim()).not.toBe('')
    }
  })

  it.each([
    ['guitar-amp-clean', ['twin', 'fender', 'clean']],
    ['guitar-amp-hot', ['jcm900', 'jcm2000', 'jcm']],
    ['guitar-amp-spring', ['ac15', 'vox']],
  ])('%s: подсказки %j', (id, want) => {
    const p = fxPresets.find((x) => x.id === id)
    expect(p, `нет пресета ${id}`).toBeTruthy()
    expect(p.amp_hint).toEqual(want)
  })

  it('новый «хай-гейн» с усилителем: подсказки od2/jvm-od/5150/recto/bug', () => {
    const want = JSON.stringify(['od2', 'jvm-od', '5150', 'recto', 'bug'])
    expect(withAmp.some((p) => JSON.stringify(p.amp_hint) === want)).toBe(true)
  })
})

describe('ТК87: fillAmp(chain, amps, hints) (условие 52)', () => {
  const BUG = 'Bug333 Clean.nam'
  const TWIN = 'Tim R Fender TwinVerb Norm Bright.nam'
  const JCM = 'Tim R JCM2000 Crunch.nam'
  const AMPS = [{ name: BUG }, { name: TWIN }, { name: JCM }]
  const chain = () => fromWorkerChain([
    { type: 'gate' }, { type: 'amp', model: '', input_db: 6 }, { type: 'cab' }, { type: 'reverb' },
  ], blocks)
  const ampModel = (c) => c.find((b) => b.type === 'amp').params.model

  it("hints ['twin','fender'] → захват Twin", () => {
    expect(ampModel(fillAmp(chain(), AMPS, ['twin', 'fender']))).toBe(TWIN)
  })

  it("hints ['jcm900','jcm'] → первая не нашлась, вторая — JCM2000", () => {
    expect(ampModel(fillAmp(chain(), AMPS, ['jcm900', 'jcm']))).toBe(JCM)
  })

  it('раньше идёт подсказка с меньшим номером, а не захват выше в списке', () => {
    // 'bug' подходит первому захвату, но подсказка 'jcm' стоит раньше
    expect(ampModel(fillAmp(chain(), AMPS, ['jcm', 'bug']))).toBe(JCM)
  })

  it("hints ['ac15'] → совпадений нет → первый захват (Bug333)", () => {
    expect(ampModel(fillAmp(chain(), AMPS, ['ac15']))).toBe(BUG)
  })

  it('hints не задан или пуст → первый захват', () => {
    expect(ampModel(fillAmp(chain(), AMPS))).toBe(BUG)
    expect(ampModel(fillAmp(chain(), AMPS, []))).toBe(BUG)
  })

  it("регистр не важен: 'TWIN' → Twin", () => {
    expect(ampModel(fillAmp(chain(), AMPS, ['TWIN']))).toBe(TWIN)
  })

  it('amps [] → цепочка как была', () => {
    const c = chain()
    expect(fillAmp(c, [], ['twin'])).toEqual(c)
  })

  it("заполненный model ('X.nam') не трогается", () => {
    const c = fromWorkerChain([{ type: 'amp', model: 'X.nam' }, { type: 'amp', model: '' }], blocks)
    const out = fillAmp(c, AMPS, ['twin'])
    expect(out[0].params.model).toBe('X.nam')
    expect(out[1].params.model).toBe(TWIN)
  })

  it('прочие блоки и параметры усилителя не меняются', () => {
    const c = chain()
    const out = fillAmp(c, AMPS, ['twin'])
    expect(out.length).toBe(c.length)
    for (const i of [0, 2, 3]) expect(out[i]).toEqual(c[i])
    const rest = { ...out[1].params }
    const rest0 = { ...c[1].params }
    delete rest.model
    delete rest0.model
    expect(rest).toEqual(rest0)
    expect(out[1].type).toBe('amp')
    expect(out[1].on).toBe(c[1].on)
  })

  it('входная цепочка, amps и hints не мутируются', () => {
    const c = chain()
    const before = JSON.parse(JSON.stringify(c))
    const amps = JSON.parse(JSON.stringify(AMPS))
    const hints = ['twin', 'fender']
    fillAmp(c, amps, hints)
    expect(c).toEqual(before)
    expect(amps).toEqual(AMPS)
    expect(hints).toEqual(['twin', 'fender'])
  })
})

// условие 52: «TrackDesk и InstrumentsPage зовут её (дубли fillAmp убираются)»; условие 53 — оба
// экрана показывают готовые по группам. Проверка по исходникам: своих копий fillAmp нет,
// fillAmp берётся из fxChain.js, группировка — из presetGroups.js.
describe('экраны используют общие fillAmp и groupPresets (условия 52, 53)', () => {
  const src = (rel) => readFileSync(new URL(rel, import.meta.url), 'utf8')
  it.each(['./components/TrackDesk.vue', './components/InstrumentsPage.vue'])('%s', (rel) => {
    const s = src(rel)
    expect(s, 'своя копия fillAmp').not.toMatch(/function\s+fillAmp\s*\(/)
    expect(s).toMatch(/import\s*\{[^}]*\bfillAmp\b[^}]*\}\s*from\s*['"]\.\.\/fxChain\.js['"]/)
    expect(s, 'подсказки пресета не передаются').toMatch(/amp_hint/)
    expect(s).toMatch(/import\s*\{[^}]*\bgroupPresets\b[^}]*\}\s*from\s*['"]\.\.\/presetGroups\.js['"]/)
  })
})
