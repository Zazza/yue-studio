import { describe, it, expect } from 'vitest'
import { addPoint, movePoint, removePoint, dbAt, dbToY, yToDb, hitPoint, isFlat } from './envelope.js'

// Тесты карточки internal-vocal-ride, условие 4: чистая логика линии громкости.
// Точки — { t, db }; дБ в диапазоне [−30, +12]; функции не мутируют вход.

const pts = () => [{ t: 1, db: 0 }, { t: 3, db: -12 }, { t: 5, db: 6 }]
const snap = a => JSON.parse(JSON.stringify(a))

describe('addPoint', () => {
  it('новый отсортированный массив, точка на своём месте', () => {
    const src = pts()
    const out = addPoint(src, 2, -3)
    expect(out.map(p => p.t)).toEqual([1, 2, 3, 5])
    expect(out[1]).toMatchObject({ t: 2, db: -3 })
  })

  it('в начало, в конец и в пустой', () => {
    expect(addPoint(pts(), 0, 1).map(p => p.t)).toEqual([0, 1, 3, 5])
    expect(addPoint(pts(), 9, 1).map(p => p.t)).toEqual([1, 3, 5, 9])
    expect(addPoint([], 4, -6)).toEqual([{ t: 4, db: -6 }])
  })

  it('вход не мутирован, возвращён новый массив', () => {
    const src = pts()
    const before = snap(src)
    const out = addPoint(src, 2, -3)
    expect(src).toEqual(before)
    expect(out).not.toBe(src)
  })
})

describe('movePoint', () => {
  it('двигает точку на новые t и дБ', () => {
    const out = movePoint(pts(), 1, 2.5, -6)
    expect(out[1]).toMatchObject({ t: 2.5, db: -6 })
    expect(out.map(p => p.t)).toEqual([1, 2.5, 5])
  })

  it('не перескакивает правого соседа: t зажат', () => {
    const out = movePoint(pts(), 1, 9, -6)
    expect(out[1].t).toBeLessThanOrEqual(5)
    expect(out[1].t).toBeGreaterThanOrEqual(1)
    expect(out.map(p => p.t)).toEqual([...out.map(p => p.t)].sort((a, b) => a - b))
    expect(out[2]).toMatchObject({ t: 5, db: 6 })   // сосед на месте
  })

  it('не перескакивает левого соседа: t зажат', () => {
    const out = movePoint(pts(), 1, 0, -6)
    expect(out[1].t).toBeGreaterThanOrEqual(1)
    expect(out[1].t).toBeLessThanOrEqual(5)
    expect(out[0]).toMatchObject({ t: 1, db: 0 })
  })

  it('крайние точки: последняя не уходит левее предпоследней, первая — правее второй', () => {
    expect(movePoint(pts(), 2, 2, 0)[2].t).toBeGreaterThanOrEqual(3)
    expect(movePoint(pts(), 0, 4, 0)[0].t).toBeLessThanOrEqual(3)
  })

  it('дБ зажат в [−30, +12]', () => {
    expect(movePoint(pts(), 1, 3, 40)[1].db).toBe(12)
    expect(movePoint(pts(), 1, 3, -100)[1].db).toBe(-30)
    expect(movePoint(pts(), 1, 3, 12)[1].db).toBe(12)
    expect(movePoint(pts(), 1, 3, -30)[1].db).toBe(-30)
  })

  it('вход не мутирован', () => {
    const src = pts()
    const before = snap(src)
    const out = movePoint(src, 1, 2, -6)
    expect(src).toEqual(before)
    expect(out).not.toBe(src)
  })
})

describe('removePoint', () => {
  it('убирает точку по индексу', () => {
    expect(removePoint(pts(), 1)).toEqual([{ t: 1, db: 0 }, { t: 5, db: 6 }])
    expect(removePoint([{ t: 1, db: 0 }], 0)).toEqual([])
  })

  it('вход не мутирован', () => {
    const src = pts()
    const before = snap(src)
    const out = removePoint(src, 0)
    expect(src).toEqual(before)
    expect(out).not.toBe(src)
  })
})

describe('dbAt — интерполяция как в Go', () => {
  const env = [{ t: 1, db: 0 }, { t: 3, db: -12 }]

  it('пусто → 0', () => {
    expect(dbAt([], 2)).toBe(0)
  })

  it('до первой точки — дБ первой, после последней — дБ последней', () => {
    expect(dbAt(env, 0)).toBe(0)
    expect(dbAt([{ t: 2, db: -6 }, { t: 4, db: 0 }], 0.5)).toBe(-6)
    expect(dbAt(env, 10)).toBe(-12)
  })

  it('между точками — линейно в дБ', () => {
    expect(dbAt(env, 2)).toBeCloseTo(-6, 9)
    expect(dbAt(env, 1.5)).toBeCloseTo(-3, 9)
    expect(dbAt(env, 1)).toBeCloseTo(0, 9)
    expect(dbAt(env, 3)).toBeCloseTo(-12, 9)
  })

  it('одна точка — её дБ везде', () => {
    expect(dbAt([{ t: 5, db: 6 }], 0)).toBe(6)
    expect(dbAt([{ t: 5, db: 6 }], 100)).toBe(6)
  })

  it('вход не мутирован', () => {
    const src = pts()
    const before = snap(src)
    dbAt(src, 2)
    expect(src).toEqual(before)
  })
})

describe('dbToY / yToDb', () => {
  it('+12 сверху (y = 0), −30 снизу (y = h)', () => {
    expect(dbToY(12, 420)).toBe(0)
    expect(dbToY(-30, 420)).toBe(420)
    expect(yToDb(0, 420)).toBe(12)
    expect(yToDb(420, 420)).toBe(-30)
  })

  it('линейно: 0 дБ — на 12/42 высоты', () => {
    expect(dbToY(0, 420)).toBeCloseTo(120, 9)
    expect(dbToY(-9, 420)).toBeCloseTo(210, 9)
    expect(yToDb(210, 420)).toBeCloseTo(-9, 9)
  })

  it('взаимно обратные', () => {
    for (const db of [-30, -17.5, -6, 0, 3.3, 12]) {
      expect(yToDb(dbToY(db, 137), 137)).toBeCloseTo(db, 9)
    }
    for (const y of [0, 11, 68.5, 137]) {
      expect(dbToY(yToDb(y, 137), 137)).toBeCloseTo(y, 9)
    }
  })
})

describe('hitPoint', () => {
  // 100 px на секунду, y — по dbToY с высотой 420
  const toX = t => t * 100
  const toY = db => dbToY(db, 420)
  const env = [{ t: 1, db: 0 }, { t: 1.1, db: 0 }, { t: 3, db: -12 }]

  it('попадание в радиусе → индекс точки', () => {
    expect(hitPoint(env, 300, toY(-12) + 3, toX, toY, 6)).toBe(2)
  })

  it('две точки в радиусе → ближайшая', () => {
    expect(hitPoint(env, 102, toY(0), toX, toY, 15)).toBe(0)
    expect(hitPoint(env, 108, toY(0), toX, toY, 15)).toBe(1)
  })

  it('мимо → −1; пусто → −1', () => {
    expect(hitPoint(env, 200, toY(0), toX, toY, 6)).toBe(-1)
    expect(hitPoint(env, 300, toY(-12) + 20, toX, toY, 6)).toBe(-1)
    expect(hitPoint([], 100, 100, toX, toY, 6)).toBe(-1)
  })

  it('расстояние по обеим осям: по диагонали за радиусом — мимо', () => {
    // dx = dy = 5 → расстояние ≈ 7.07 > 6
    expect(hitPoint([{ t: 3, db: -12 }], 305, toY(-12) + 5, toX, toY, 6)).toBe(-1)
    expect(hitPoint([{ t: 3, db: -12 }], 304, toY(-12) + 4, toX, toY, 6)).toBe(0)
  })

  it('вход не мутирован', () => {
    const before = snap(env)
    hitPoint(env, 100, toY(0), toX, toY, 6)
    expect(env).toEqual(before)
  })
})

describe('isFlat — нечего применять', () => {
  it('пусто или все точки 0 дБ → true', () => {
    expect(isFlat([])).toBe(true)
    expect(isFlat([{ t: 1, db: 0 }, { t: 5, db: 0 }])).toBe(true)
  })

  it('хоть одна точка не 0 дБ → false', () => {
    expect(isFlat([{ t: 1, db: 0 }, { t: 5, db: -0.5 }])).toBe(false)
    expect(isFlat([{ t: 1, db: 3 }])).toBe(false)
  })
})
