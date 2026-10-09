// Тесты карточки internal-own-track, этап 6 «Сведение и мастер», условия 45–46 (тест-кейс ТК75):
// чистая логика пульта mixDesk.js и «Сохранить правки как пресет».
//   placeLabel(place) — подпись места по-русски: «центр», «30 % вправо», «влево, ширина 1,4»;
//   withStemPlace(list, stem, place) — новый реестр: запись «место» {stems:[stem], place, childId<0,
//     from 0, to 0}, одна на дорожку (вторая заменяет), {pan 0, width 1} — убирает;
//   withMaster(list, engine, label) — одна запись мастера {master:true, engine, label, childId<0,
//     from 0, to 0, stems: []}, новая заменяет прежнюю;
//   presetFromEdits(applied) → {specs, skipped, master}: «место» → spec {stems, place, db: 0}
//     (без engine/chain/steps), мастер → master (цепочка), добавление с place — place в spec.
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { placeLabel, withStemPlace, withMaster, masterChain } from './mixDesk.js'
import { presetFromEdits } from './soundPresets.js'

const ENGINE = [{ type: 'eq', highpass_hz: 80 }]
const MASTER = [{ type: 'glue', threshold_db: -18, ratio: 2 }, { type: 'limiter', ceiling_db: -1, target_lufs: -14 }]
const engineRec = (over = {}) => ({
  childId: -1, from: 0, to: 0, lead: 0, beat: 0, db: 0, stems: ['other'],
  fadeIn: 0, fadeOut: 0, keepHighHz: 0, instId: 'engine', engine: ENGINE, label: 'Гитара', ...over,
})
const insertRec = (over = {}) => ({ childId: 7, instId: 'i-a', from: 10, to: 20, lead: 0, beat: 0.5, db: -6, stems: ['drums'], ...over })
const placeRecs = (list) => list.filter((r) => r.place && !r.master && !r.engine && !r.chain && !r.steps)
const masters = (list) => list.filter((r) => r.master)

describe('placeLabel (ТК75)', () => {
  it('центр и ширина 1 — «центр»', () => {
    expect(placeLabel({ pan: 0, width: 1 })).toBe('центр')
  })

  it('0,3 — «30 % вправо», −0,3 — «30 % влево»', () => {
    expect(placeLabel({ pan: 0.3, width: 1 })).toBe('30 % вправо')
    expect(placeLabel({ pan: -0.3, width: 1 })).toBe('30 % влево')
  })

  it('крайнее положение с шириной — «влево, ширина 1,4»; зеркально — «вправо»', () => {
    expect(placeLabel({ pan: -1, width: 1.4 })).toBe('влево, ширина 1,4')
    expect(placeLabel({ pan: 1, width: 1 })).toBe('вправо')
  })
})

describe('withStemPlace (ТК75, усл. 46а)', () => {
  it('создаёт запись «место» {stems:[X], place, childId < 0, from 0, to 0}', () => {
    const list = [engineRec(), insertRec()]
    const out = withStemPlace(list, 'other', { pan: 0.3, width: 1.4 })
    const recs = placeRecs(out)
    expect(recs.length).toBe(1)
    const r = recs[0]
    expect(r.stems).toEqual(['other'])
    expect(r.place).toEqual({ pan: 0.3, width: 1.4 })
    expect(r.childId).toBeLessThan(0)
    expect(r.from).toBe(0)
    expect(r.to).toBe(0)
    // прочие записи на месте, у новой — свой childId
    expect(out).toContainEqual(engineRec())
    expect(out).toContainEqual(insertRec())
    expect(out.length).toBe(3)
    expect(new Set(out.map((x) => x.childId)).size).toBe(out.length)
  })

  it('не меняет исходный список', () => {
    const list = [engineRec()]
    const copy = JSON.parse(JSON.stringify(list))
    withStemPlace(list, 'other', { pan: 0.5, width: 1 })
    expect(list).toEqual(copy)
  })

  it('вторая запись на ту же дорожку заменяет первую (одна на дорожку)', () => {
    let list = withStemPlace([], 'other', { pan: 0.3, width: 1 })
    list = withStemPlace(list, 'other', { pan: -0.6, width: 0.8 })
    const recs = placeRecs(list)
    expect(recs.length).toBe(1)
    expect(recs[0].place).toEqual({ pan: -0.6, width: 0.8 })
  })

  it('разные дорожки — по записи на каждую', () => {
    let list = withStemPlace([], 'other', { pan: 0.3, width: 1 })
    list = withStemPlace(list, 'drums', { pan: 0, width: 1.5 })
    expect(placeRecs(list).map((r) => r.stems[0]).sort()).toEqual(['drums', 'other'])
  })

  it('центр и ширина 1 — запись убирается, остальное не трогается', () => {
    let list = withStemPlace([engineRec()], 'other', { pan: 0.3, width: 1 })
    list = withStemPlace(list, 'other', { pan: 0, width: 1 })
    expect(placeRecs(list)).toEqual([])
    expect(list).toEqual([engineRec()])
  })

  it('центр и 1 без прежней записи — список без изменений', () => {
    expect(withStemPlace([engineRec()], 'bass', { pan: 0, width: 1 })).toEqual([engineRec()])
  })
})

describe('withMaster (ТК75, усл. 46в)', () => {
  it('одна запись мастера {master, engine, label, childId < 0, from 0, to 0, stems []}', () => {
    const out = withMaster([engineRec()], MASTER, 'Стриминг −14 LUFS')
    const m = masters(out)
    expect(m.length).toBe(1)
    expect(m[0]).toMatchObject({ master: true, engine: MASTER, label: 'Стриминг −14 LUFS', from: 0, to: 0, stems: [] })
    expect(m[0].childId).toBeLessThan(0)
    expect(m[0].childId).not.toBe(engineRec().childId)
    expect(out).toContainEqual(engineRec())
  })

  it('новый мастер заменяет прежний', () => {
    let list = withMaster([engineRec()], MASTER, 'Стриминг −14 LUFS')
    const glue = [{ type: 'glue' }]
    list = withMaster(list, glue, 'Мягкая склейка')
    const m = masters(list)
    expect(m.length).toBe(1)
    expect(m[0].engine).toEqual(glue)
    expect(m[0].label).toBe('Мягкая склейка')
    expect(list.length).toBe(2)
  })

  it('не меняет исходный список', () => {
    const list = [engineRec()]
    withMaster(list, MASTER, 'м')
    expect(list).toEqual([engineRec()])
  })
})

describe('presetFromEdits: место и мастер (ТК75, усл. 45)', () => {
  it('запись «место» → spec {stems, place, db: 0} без engine/chain/steps, не пропущена', () => {
    const list = withStemPlace([], 'other', { pan: 0.3, width: 1.4 })
    const { specs, skipped } = presetFromEdits(list)
    expect(specs).toEqual([{ stems: ['other'], place: { pan: 0.3, width: 1.4 }, db: 0 }])
    expect(skipped).toBe(0)
  })

  it('запись мастера → master (цепочка), в specs не попадает и не пропущена', () => {
    const list = withMaster([engineRec()], MASTER, 'Стриминг −14 LUFS')
    const { specs, skipped, master } = presetFromEdits(list)
    expect(master).toEqual(MASTER)
    expect(specs).toEqual([{ stems: ['other'], engine: ENGINE, db: 0 }])
    expect(skipped).toBe(0)
  })

  it('выключенный мастер — master пуст', () => {
    const list = withMaster([], MASTER, 'м').map((r) => ({ ...r, off: true }))
    expect(presetFromEdits(list).master).toEqual([])
  })

  it('без мастера — master []', () => {
    expect(presetFromEdits([engineRec()]).master).toEqual([])
  })

  it('запись-добавление (партия) в пресет не идёт — пропущена (усл. 45а)', () => {
    const add = engineRec({ childId: -3, stems: ['mix'], add: true, place: { pan: -0.3, width: 1.5 } })
    const { specs, skipped } = presetFromEdits([add])
    expect(specs).toEqual([])
    expect(skipped).toBe(1)
  })
})

// ТК79д / усл. 46г: «изменить» мастер — limiter остаётся на своём месте со всеми параметрами;
// цель и потолок меняют только его target_lufs/ceiling_db; без цели и без галочки — убирается;
// нет limiter, а цель есть — дописывается в конец.
describe('masterChain (ТК79д, усл. 46г)', () => {
  const GLUE = { type: 'glue', threshold_db: -18, ratio: 2 }
  const GAIN = { type: 'gain', gain_db: -1 }

  it('[glue, limiter{release_ms 500}, gain] + цель −12 → limiter на месте 1, release_ms 500, target −12', () => {
    const chain = [GLUE, { type: 'limiter', release_ms: 500 }, GAIN]
    const before = JSON.parse(JSON.stringify(chain))
    const out = masterChain(chain, { target: -12, ceiling: -1.5, limiter: true })
    expect(out.length).toBe(3)
    expect(out[0]).toEqual(GLUE)
    expect(out[2]).toEqual(GAIN)
    expect(out[1]).toMatchObject({ type: 'limiter', release_ms: 500, target_lufs: -12, ceiling_db: -1.5 })
    expect(chain).toEqual(before)
  })

  it('цель 0 и без галочки «ограничитель» — limiter убран, остальное по порядку', () => {
    const chain = [GLUE, { type: 'limiter', release_ms: 500, target_lufs: -14 }, GAIN]
    const out = masterChain(chain, { target: 0, ceiling: -1, limiter: false })
    expect(out).toEqual([GLUE, GAIN])
  })

  it('limiter нет, цель −14 — дописан в конец с target −14', () => {
    const out = masterChain([GLUE, GAIN], { target: -14, ceiling: -1, limiter: false })
    expect(out.length).toBe(3)
    expect(out.slice(0, 2)).toEqual([GLUE, GAIN])
    expect(out[2]).toMatchObject({ type: 'limiter', target_lufs: -14 })
  })
})
