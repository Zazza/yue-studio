// Тесты карточки internal-own-track, этап 1 «Пресеты звука», условия 6–7 (тест-кейсы
// ТК23–ТК25): чистая логика пресетов звука во фронте — правки студии → записи пресета,
// строка статуса пресета в карточке трека, выбор пресетов чипами в форме нового трека.
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { presetFromEdits, presetLine, togglePreset, withLevels } from './soundPresets.js'

// записи реестра правок в форме useInserts (у правок без рендера childId < 0)
const base = (childId, stems, over = {}) => ({
  childId, from: 0, to: 0, lead: 0, beat: 0, db: 0, stems,
  fadeIn: 0, fadeOut: 0, keepHighHz: 0, ...over,
})
const ENGINE = [{ type: 'eq', highpass_hz: 80 }, { type: 'comp' }]
const engineRec = (over = {}) => base(-1, ['bass'], { instId: 'engine', engine: ENGINE, label: 'Бас', db: -3, ...over })
const fxRec = (over = {}) => base(-2, ['vocals'], { instId: 'fx-soften', chain: 'soften', params: { strength: 0.6 }, ...over })
const STEPS = [{ chain: 'soften', params: { strength: 0.4 } }, { chain: 'level', params: { gain: 2 }, off: true }]
const pedalsRec = (over = {}) => base(-3, ['other', 'guitar'], { instId: 'pedals', steps: STEPS, label: 'Педали', db: 2, ...over })
const muteRec = (over = {}) => base(-4, ['drums'], { instId: 'mute-drums', db: -100, ...over })
const envRec = (over = {}) => base(-5, ['vocals'], { instId: 'env', envelope: [{ t: 0, db: 0 }, { t: 10, db: -6 }], ...over })
const insertRec = (over = {}) => ({ childId: 7, instId: 'i-a', from: 0, to: 0, lead: 0, beat: 0.5, db: -6, ...over })

describe('presetFromEdits — правки студии → пресет (ТК23)', () => {
  it('запись движка «весь трек» → {stems, engine, db}', () => {
    const { specs, skipped } = presetFromEdits([engineRec()])
    expect(specs).toEqual([{ stems: ['bass'], engine: ENGINE, db: -3 }])
    expect(skipped).toBe(0)
  })

  it('запись-эффект «весь трек» → {stems, chain, params, db}', () => {
    const { specs, skipped } = presetFromEdits([fxRec()])
    expect(specs).toEqual([{ stems: ['vocals'], chain: 'soften', params: { strength: 0.6 }, db: 0 }])
    expect(skipped).toBe(0)
  })

  it('педали «весь трек» → {stems, steps, db}, все дорожки записи сохраняются', () => {
    const { specs } = presetFromEdits([pedalsRec()])
    expect(specs).toEqual([{ stems: ['other', 'guitar'], steps: STEPS, db: 2 }])
  })

  it('несколько подходящих записей — в исходном порядке', () => {
    const { specs } = presetFromEdits([fxRec(), engineRec(), pedalsRec()])
    expect(specs.map(s => s.stems[0])).toEqual(['vocals', 'bass', 'other'])
  })

  it('выключенная запись не входит и не считается пропущенной', () => {
    const { specs, skipped } = presetFromEdits([engineRec({ off: true }), fxRec()])
    expect(specs.map(s => s.chain || 'engine')).toEqual(['soften'])
    expect(skipped).toBe(0)
  })

  it('запись с окном пропускается: from > 0, to > 0, оба', () => {
    const { specs, skipped } = presetFromEdits([
      engineRec({ from: 10, to: 0 }),
      fxRec({ from: 0, to: 30 }),
      pedalsRec({ from: 12, to: 27 }),
    ])
    expect(specs).toEqual([])
    expect(skipped).toBe(3)
  })

  it('вклейка (childId > 0) пропускается, даже на весь трек', () => {
    const { specs, skipped } = presetFromEdits([insertRec(), engineRec()])
    expect(specs.length).toBe(1)
    expect(specs[0].engine).toEqual(ENGINE)
    expect(skipped).toBe(1)
  })

  it('заглушение и громкость без engine/chain/steps пропускаются', () => {
    const { specs, skipped } = presetFromEdits([muteRec(), muteRec({ childId: -6, db: -6 })])
    expect(specs).toEqual([])
    expect(skipped).toBe(2)
  })

  it('линия громкости (envelope) пропускается', () => {
    const { specs, skipped } = presetFromEdits([envRec()])
    expect(specs).toEqual([])
    expect(skipped).toBe(1)
  })

  it('смешанный реестр: войдёт N, пропущено M (выключенные — ни там, ни там)', () => {
    const applied = [
      engineRec(), fxRec(), pedalsRec(),            // войдут 3
      engineRec({ childId: -8, off: true }),        // не войдёт, не пропущена
      fxRec({ childId: -9, from: 5, to: 9 }),       // окно
      insertRec(), muteRec(), envRec(),             // вклейка, заглушение, линия
      insertRec({ childId: 8, off: true }),         // выключенная вклейка — не считается
    ]
    const { specs, skipped } = presetFromEdits(applied)
    expect(specs.length).toBe(3)
    expect(skipped).toBe(4)
  })

  it('пустой реестр → пусто, ноль пропущенных', () => {
    expect(presetFromEdits([])).toEqual({ specs: [], skipped: 0 })
  })

  it('db по умолчанию 0, если у записи его нет', () => {
    const rec = engineRec()
    delete rec.db
    const { specs } = presetFromEdits([rec])
    expect(specs[0].db).toBe(0)
  })

  it('не меняет записи реестра', () => {
    const applied = [engineRec(), fxRec({ from: 3, to: 5 })]
    const copy = JSON.parse(JSON.stringify(applied))
    presetFromEdits(applied)
    expect(applied).toEqual(copy)
  })
})

describe('presetLine — строка пресета в карточке трека (ТК24)', () => {
  const st = (status, over = {}) => ({ id: 3, status, child_id: null, error: '', ...over })

  it('pending → «X» ждёт очереди, без повтора', () => {
    const line = presetLine(st('pending'), 'Transmission')
    expect(typeof line.icon).toBe('string')
    expect(line.icon.length).toBeGreaterThan(0)
    expect(line.text).toContain('Transmission')
    expect(line.text).toContain('ждёт')
    expect(line.retry).toBe(false)
  })

  // ревью s1: приложение закрыли посреди применения — статус застрял, повтор нужен и здесь
  it('running → «X» применяется, с повтором', () => {
    const line = presetLine(st('running'), 'Transmission')
    expect(line.icon.length).toBeGreaterThan(0)
    expect(line.text).toContain('Transmission')
    expect(line.text).toContain('применяется')
    expect(line.retry).toBe(true)
  })

  it('done → «X» → версия #N, без повтора', () => {
    const line = presetLine(st('done', { child_id: 512 }), 'Sex on Fire')
    expect(line.icon.length).toBeGreaterThan(0)
    expect(line.text).toContain('Sex on Fire')
    expect(line.text).toContain('#512')
    expect(line.retry).toBe(false)
  })

  it('error → «X»: причина, с повтором', () => {
    const line = presetLine(st('error', { error: 'нет дорожек: kick' }), 'Transmission')
    expect(line.icon.length).toBeGreaterThan(0)
    expect(line.text).toContain('Transmission')
    expect(line.text).toContain('нет дорожек: kick')
    expect(line.retry).toBe(true)
  })

  it('у всех четырёх статусов значки разные', () => {
    const icons = ['pending', 'running', 'done', 'error'].map(s => presetLine(st(s, { child_id: 1, error: 'e' }), 'X').icon)
    expect(new Set(icons).size).toBe(4)
  })
})

describe('togglePreset — выбор пресетов чипами (ТК25)', () => {
  it('добавляет в конец, в порядке выбора', () => {
    let ids = []
    ids = togglePreset(ids, 5)
    ids = togglePreset(ids, 2)
    ids = togglePreset(ids, 9)
    expect(ids).toEqual([5, 2, 9])
  })

  it('повторное нажатие убирает, порядок остальных сохраняется', () => {
    expect(togglePreset([5, 2, 9], 2)).toEqual([5, 9])
  })

  it('четвёртый не добавляется', () => {
    expect(togglePreset([1, 2, 3], 4)).toEqual([1, 2, 3])
  })

  it('при полном выборе убрать можно, затем добавить другой', () => {
    const less = togglePreset([1, 2, 3], 1)
    expect(less).toEqual([2, 3])
    expect(togglePreset(less, 4)).toEqual([2, 3, 4])
  })

  it('свой предел max', () => {
    expect(togglePreset([1], 2, 1)).toEqual([1])
    expect(togglePreset([], 2, 1)).toEqual([2])
  })

  it('возвращает новый массив, исходный не меняется', () => {
    const ids = [1, 2]
    const out = togglePreset(ids, 3)
    expect(out).not.toBe(ids)
    expect(ids).toEqual([1, 2])
    const out2 = togglePreset(ids, 1)
    expect(out2).not.toBe(ids)
    expect(ids).toEqual([1, 2])
  })
})

// Этап 1б, условие 12 (ТК31): withLevels(preset, levels) — копия пресета с громкостью
// записей из ползунков студии; levels {индекс записи: дБ}, зажим −24…24; исходный не меняется.
describe('withLevels — громкость записей пресета при применении (ТК31)', () => {
  const preset = () => ({
    id: 7, name: 'Пост-панк', builtin: true, target_lufs: -13,
    specs: [
      { stems: ['bass'], engine: [{ type: 'bass', kit: 'growlybass/bass', output_db: 0 }], db: -3 },
      { stems: ['vocals'], chain: 'eq', params: { high: 2, highf: 8000 }, db: 0 },
      { stems: ['other', 'guitar'], steps: [{ chain: 'soften', params: { strength: 0.4 } }], db: 1 },
    ],
    final: [{ chain: 'width', params: { width: 1.1, bass: 120 } }],
  })

  it('{0: 3} меняет db записи 0 в копии, прочее как было', () => {
    const p = preset()
    const out = withLevels(p, { 0: 3 })
    expect(out.specs[0].db).toBe(3)
    expect(out.specs[1].db).toBe(0)
    expect(out.specs[2].db).toBe(1)
    const want = preset()
    want.specs[0].db = 3
    expect(out).toEqual(want)
  })

  it('исходный пресет не меняется', () => {
    const p = preset()
    withLevels(p, { 0: 3, 2: -6 })
    expect(p).toEqual(preset())
  })

  it('копия глубокая: правка копии не трогает исходный', () => {
    const p = preset()
    const out = withLevels(p, { 1: 2 })
    out.specs[1].params.high = 99
    out.specs[0].engine[0].output_db = 5
    out.final[0].params.width = 2
    out.specs.push({ stems: ['drums'], chain: 'eq', params: {}, db: 0 })
    expect(p).toEqual(preset())
  })

  it('40 → 24, −40 → −24 (зажим)', () => {
    expect(withLevels(preset(), { 0: 40 }).specs[0].db).toBe(24)
    expect(withLevels(preset(), { 0: -40 }).specs[0].db).toBe(-24)
  })

  it('края −24 и 24 — как есть', () => {
    const out = withLevels(preset(), { 0: 24, 1: -24 })
    expect(out.specs[0].db).toBe(24)
    expect(out.specs[1].db).toBe(-24)
  })

  it('индекс вне записей игнорируется', () => {
    const out = withLevels(preset(), { 3: 5, 9: 5, '-1': 5 })
    expect(out).toEqual(preset())
    expect(out.specs).toHaveLength(3)
  })

  it('ключи-строки (из JSON/ползунков) работают как числа', () => {
    expect(withLevels(preset(), { '2': -4 }).specs[2].db).toBe(-4)
  })

  it('пустые levels — глубокая копия как есть', () => {
    const p = preset()
    const out = withLevels(p, {})
    expect(out).toEqual(p)
    expect(out).not.toBe(p)
    expect(out.specs).not.toBe(p.specs)
    expect(out.specs[0]).not.toBe(p.specs[0])
    expect(out.final).not.toBe(p.final)
  })

  it('запись без db получает уровень из levels', () => {
    const p = preset()
    delete p.specs[1].db
    expect(withLevels(p, { 1: 2 }).specs[1].db).toBe(2)
  })
})
