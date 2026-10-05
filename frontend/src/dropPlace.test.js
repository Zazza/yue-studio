// Тесты положения выпадающего списка селекта: левая координата под кнопкой.
import { describe, it, expect } from 'vitest'
import { dropLeft } from './dropPlace.js'

describe('dropLeft — список помещается', () => {
  it('выровнен по левому краю кнопки', () => {
    expect(dropLeft(100, 300, 1200)).toBe(100)
  })

  it('правый край ровно на viewportWidth − margin — сдвига нет', () => {
    // 888 + 300 = 1188 = 1200 − 12
    expect(dropLeft(888, 300, 1200)).toBe(888)
  })

  it('чуть левее границы — тоже без сдвига', () => {
    expect(dropLeft(887, 300, 1200)).toBe(887)
  })

  it('пустой список (ширина 0) остаётся у кнопки', () => {
    expect(dropLeft(100, 0, 1200)).toBe(100)
  })
})

describe('dropLeft — кнопка у правого края', () => {
  it('пример из спецификации: окно 1200, кнопка 1000, список 300 → 888', () => {
    expect(dropLeft(1000, 300, 1200)).toBe(888)
  })

  it('выход за границу на 1 px — сдвиг ровно до отступа справа', () => {
    expect(dropLeft(889, 300, 1200)).toBe(888)
  })

  it('кнопка за пределами окна — правый край списка всё равно в margin от края', () => {
    expect(dropLeft(1500, 300, 1200)).toBe(888)
  })
})

describe('dropLeft — список шире окна', () => {
  it('прижат к левому отступу', () => {
    expect(dropLeft(500, 1300, 1200)).toBe(12)
  })

  it('чуть шире доступной полосы — левее margin не уходит', () => {
    // доступно 1200 − 24 = 1176; 1190 дало бы 1200 − 12 − 1190 = −2 без зажима
    expect(dropLeft(500, 1190, 1200)).toBe(12)
  })

  it('ровно во всю доступную полосу — от margin', () => {
    expect(dropLeft(500, 1176, 1200)).toBe(12)
  })
})

describe('dropLeft — кнопка у левого края', () => {
  it('btnLeft меньше margin — результат = margin', () => {
    expect(dropLeft(5, 300, 1200)).toBe(12)
  })

  it('btnLeft = 0 — результат = margin', () => {
    expect(dropLeft(0, 300, 1200)).toBe(12)
  })

  it('отрицательный btnLeft (страница прокручена вбок) — результат = margin', () => {
    expect(dropLeft(-50, 300, 1200)).toBe(12)
  })

  it('btnLeft ровно margin — остаётся на месте', () => {
    expect(dropLeft(12, 300, 1200)).toBe(12)
  })
})

describe('dropLeft — параметр margin', () => {
  it('по умолчанию 12: совпадает с явным 12', () => {
    expect(dropLeft(1000, 300, 1200)).toBe(dropLeft(1000, 300, 1200, 12))
    expect(dropLeft(5, 300, 1200)).toBe(dropLeft(5, 300, 1200, 12))
  })

  it('явный margin учитывается при сдвиге влево', () => {
    expect(dropLeft(1000, 300, 1200, 20)).toBe(880)
  })

  it('явный margin учитывается у левого края', () => {
    expect(dropLeft(5, 300, 1200, 20)).toBe(20)
  })

  it('явный margin учитывается для списка шире окна', () => {
    expect(dropLeft(500, 1300, 1200, 20)).toBe(20)
  })

  it('margin = 0: список вплотную к правому краю окна', () => {
    expect(dropLeft(1000, 300, 1200, 0)).toBe(900)
  })
})
