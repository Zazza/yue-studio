import { describe, it, expect } from 'vitest'
import { addPoint, movePoint, removePoint, dbAt, dbToY, yToDb, hitPoint, isFlat, bumpRange } from './envelope.js'

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

describe('bumpRange — поднять/опустить участок линии', () => {
  const F = 0.2   // fade по умолчанию
  const sloped = () => [{ t: 0, db: -10 }, { t: 10, db: 0 }, { t: 30, db: -20 }]
  const between = (v, a, b) => {
    expect(v).toBeGreaterThanOrEqual(Math.min(a, b) - 1e-9)
    expect(v).toBeLessThanOrEqual(Math.max(a, b) + 1e-9)
  }
  const sortedT = out => {
    const ts = out.map(p => p.t)
    expect(ts).toEqual([...ts].sort((a, b) => a - b))
  }

  it('новый отсортированный массив {t, db}, вход не мутирован', () => {
    const src = sloped()
    const before = snap(src)
    const out = bumpRange(src, 5, 15, 3)
    expect(src).toEqual(before)
    expect(out).not.toBe(src)
    sortedT(out)
    for (const p of out) {
      expect(typeof p.t).toBe('number')
      expect(typeof p.db).toBe('number')
    }
  })

  it('внутри [from, to] линия = старая + delta (на наклонной линии)', () => {
    const src = sloped()
    const out = bumpRange(src, 5, 15, 3)
    for (const t of [5, 6.3, 9.99, 10, 12.5, 15]) {
      expect(dbAt(out, t)).toBeCloseTo(dbAt(src, t) + 3, 6)
    }
  })

  it('внутри участка — опускание отрицательной delta', () => {
    const src = sloped()
    const out = bumpRange(src, 5, 15, -4)
    for (const t of [5, 8, 10, 15]) {
      expect(dbAt(out, t)).toBeCloseTo(dbAt(src, t) - 4, 6)
    }
  })

  it('вне [from − fade, to + fade] линия не меняется', () => {
    const src = sloped()
    const out = bumpRange(src, 5, 15, 3)
    for (const t of [0, 1, 4.5, 5 - F - 0.01, 15 + F + 0.01, 16, 20, 30, 40]) {
      expect(dbAt(out, t)).toBeCloseTo(dbAt(src, t), 6)
    }
  })

  it('вне участка не меняется и при точках внутри/снаружи, своём fade', () => {
    const src = [{ t: 1, db: -6 }, { t: 4, db: 2 }, { t: 6, db: -3 }, { t: 7, db: 4 }, { t: 12, db: -8 }]
    const out = bumpRange(src, 5, 8, 5, 1)
    for (const t of [0, 1, 2.5, 3.9, 4 - 0.01, 9 + 0.01, 10, 12, 20]) {
      expect(dbAt(out, t)).toBeCloseTo(dbAt(src, t), 6)
    }
    for (const t of [5, 6, 6.5, 7, 8]) {
      expect(dbAt(out, t)).toBeCloseTo(dbAt(src, t) + 5, 6)
    }
  })

  it('существующие точки внутри участка сдвинуты на delta', () => {
    const src = [{ t: 0, db: 0 }, { t: 6, db: -3 }, { t: 7, db: 4 }, { t: 20, db: 0 }]
    const out = bumpRange(src, 5, 8, 2)
    for (const p of [{ t: 6, db: -1 }, { t: 7, db: 6 }]) {
      const q = out.find(o => Math.abs(o.t - p.t) < 1e-9)
      expect(q).toBeDefined()
      expect(q.db).toBeCloseTo(p.db, 6)
    }
  })

  it('переход слева и справа — между старым и новым, монотонно, без скачка', () => {
    const out = bumpRange([{ t: 0, db: 0 }, { t: 100, db: 0 }], 10, 20, 6, 1)
    const N = 50
    let prev = dbAt(out, 9)
    for (let i = 0; i <= N; i++) {
      const v = dbAt(out, 9 + i / N)
      between(v, 0, 6)
      expect(v).toBeGreaterThanOrEqual(prev - 1e-9)   // растёт к участку
      expect(Math.abs(v - prev)).toBeLessThan(1)      // нет скачка
      prev = v
    }
    prev = dbAt(out, 20)
    for (let i = 0; i <= N; i++) {
      const v = dbAt(out, 20 + i / N)
      between(v, 0, 6)
      expect(v).toBeLessThanOrEqual(prev + 1e-9)      // спадает от участка
      expect(Math.abs(v - prev)).toBeLessThan(1)
      prev = v
    }
    expect(dbAt(out, 9.5)).toBeGreaterThan(0)
    expect(dbAt(out, 9.5)).toBeLessThan(6)
    expect(dbAt(out, 20.5)).toBeGreaterThan(0)
    expect(dbAt(out, 20.5)).toBeLessThan(6)
  })

  it('переход на наклонной линии — между старым и старым + delta', () => {
    const src = sloped()
    const out = bumpRange(src, 5, 15, 3, 1)
    for (const t of [4, 4.25, 4.5, 4.75, 5, 15, 15.3, 15.6, 16]) {
      between(dbAt(out, t), dbAt(src, t), dbAt(src, t) + 3)
    }
  })

  it('пустая линия = 0 дБ: 0 вне участка, +delta внутри', () => {
    const out = bumpRange([], 10, 20, 3)
    for (const t of [0, 5, 9.7, 20.3, 50, 100]) {
      expect(dbAt(out, t)).toBeCloseTo(0, 6)
    }
    for (const t of [10, 12, 15, 20]) {
      expect(dbAt(out, t)).toBeCloseTo(3, 6)
    }
  })

  it('клип: результат зажат в [−30, +12]', () => {
    const hi = bumpRange([{ t: 0, db: 10 }, { t: 50, db: 10 }], 10, 20, 6)
    for (const t of [10, 15, 20]) expect(dbAt(hi, t)).toBeCloseTo(12, 6)
    const lo = bumpRange([{ t: 0, db: -28 }, { t: 50, db: -28 }], 10, 20, -6)
    for (const t of [10, 15, 20]) expect(dbAt(lo, t)).toBeCloseTo(-30, 6)
    for (const p of [...hi, ...lo]) {
      expect(p.db).toBeLessThanOrEqual(12)
      expect(p.db).toBeGreaterThanOrEqual(-30)
    }
    // вне участка клип ничего не трогает
    expect(dbAt(hi, 5)).toBeCloseTo(10, 6)
    expect(dbAt(lo, 30)).toBeCloseTo(-28, 6)
  })

  it('from = 0: левый переход обрезан по 0, в t = 0 уже +delta', () => {
    const src = [{ t: 0, db: -2 }, { t: 10, db: -2 }]
    const out = bumpRange(src, 0, 3, 4)
    for (const p of out) expect(p.t).toBeGreaterThanOrEqual(0)
    for (const t of [0, 1, 3]) expect(dbAt(out, t)).toBeCloseTo(2, 6)
    expect(dbAt(out, 5)).toBeCloseTo(-2, 6)
  })

  it('from − fade < 0: t всех точек ≥ 0, внутри +delta', () => {
    const out = bumpRange([], 0.1, 2, -5)
    for (const p of out) expect(p.t).toBeGreaterThanOrEqual(0)
    for (const t of [0.1, 1, 2]) expect(dbAt(out, t)).toBeCloseTo(-5, 6)
    expect(dbAt(out, 3)).toBeCloseTo(0, 6)
  })

  it('непересекающиеся участки независимы: второй подъём не меняет первый', () => {
    const src = sloped()
    const once = bumpRange(src, 2, 5, 3)
    const twice = bumpRange(once, 15, 20, -4)
    for (const t of [1, 2, 3.5, 5, 5.1, 8]) {
      expect(dbAt(twice, t)).toBeCloseTo(dbAt(once, t), 6)
    }
    for (const t of [2, 3.5, 5]) expect(dbAt(twice, t)).toBeCloseTo(dbAt(src, t) + 3, 6)
    for (const t of [15, 17, 20]) expect(dbAt(twice, t)).toBeCloseTo(dbAt(src, t) - 4, 6)
  })

  it('все t в результате различны, в т.ч. когда точки совпадают с границами', () => {
    const cases = [
      bumpRange(sloped(), 5, 15, 3),
      bumpRange([], 10, 20, 3),
      bumpRange([{ t: 4.8, db: 1 }, { t: 5, db: 0 }, { t: 15, db: -1 }, { t: 15.2, db: 2 }], 5, 15, 3),
      bumpRange([{ t: 0, db: 0 }, { t: 1, db: 0 }], 0, 1, 2),
      bumpRange(bumpRange([], 2, 5, 3), 5, 8, 3),   // соприкасающиеся участки
    ]
    for (const out of cases) {
      sortedT(out)
      expect(new Set(out.map(p => p.t)).size).toBe(out.length)
    }
  })

  it('вырожденный участок (to <= from) — вход без изменений', () => {
    const src = pts()
    const before = snap(src)
    expect(bumpRange(src, 4, 4, 3)).toEqual(before)
    expect(bumpRange(src, 4, 2, 3)).toEqual(before)
    expect(bumpRange([], 4, 2, 3)).toEqual([])
    expect(src).toEqual(before)
  })
})
