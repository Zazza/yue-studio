// Тесты поиска по пунктам выпадающего списка — по спецификации matchOptions.
import { describe, it, expect } from 'vitest'
import { matchOptions } from './optionFilter.js'

const o = (value, label, over = {}) => ({ value, label, ...over })
const values = list => list.map(x => x.value)

describe('пустой запрос', () => {
  const opts = [o(1, 'Rock'), o(2, 'Блюз'), o(3, '#331 Bondage Fairies')]

  it('пустая строка, пробелы и undefined — все пункты в исходном порядке', () => {
    expect(values(matchOptions(opts, ''))).toEqual([1, 2, 3])
    expect(values(matchOptions(opts, '   \t '))).toEqual([1, 2, 3])
    expect(values(matchOptions(opts, undefined))).toEqual([1, 2, 3])
  })

  it('пустой список пунктов — пустой результат', () => {
    expect(matchOptions([], 'rock')).toEqual([])
    expect(matchOptions([], '')).toEqual([])
  })
})

describe('совпадение по словам', () => {
  it('каждое слово запроса должно найтись в подписи', () => {
    const opts = [o(1, 'heavy rock'), o(2, 'rock ballad'), o(3, 'heavy metal')]
    expect(values(matchOptions(opts, 'heavy rock'))).toEqual([1])
    expect(values(matchOptions(opts, 'rock'))).toEqual([1, 2])
  })

  it('порядок слов в запросе не важен', () => {
    const opts = [o(1, '#331 Bondage Fairies'), o(2, '#214 моторик')]
    expect(values(matchOptions(opts, 'bondage 331'))).toEqual([1])
    expect(values(matchOptions(opts, 'fairies #331'))).toEqual([1])
  })

  it('слово ищется как подстрока, не только с начала слова', () => {
    const opts = [o(1, '#331 a'), o(2, '#1331 x'), o(3, '#214 b')]
    expect(values(matchOptions(opts, '#331'))).toEqual([1])
    expect(values(matchOptions(opts, '331'))).toEqual([1, 2])
    expect(values(matchOptions(opts, 'airi'))).toEqual([])
    expect(values(matchOptions([o(1, 'Bondage Fairies')], 'airi'))).toEqual([1])
  })

  it('несколько пробелов между словами не мешают', () => {
    const opts = [o(1, 'heavy rock'), o(2, 'heavy metal')]
    expect(values(matchOptions(opts, '  heavy    rock  '))).toEqual([1])
  })

  it('ничего не нашлось — пустой массив', () => {
    expect(matchOptions([o(1, 'rock')], 'jazz')).toEqual([])
  })

  it('слово, которого нет, отсекает пункт, даже если остальные совпали', () => {
    expect(matchOptions([o(1, 'heavy rock')], 'heavy rock jazz')).toEqual([])
  })
})

describe('регистр и ё', () => {
  it('регистр не важен — латиница и кириллица', () => {
    const opts = [o(1, 'Bondage Fairies'), o(2, 'Медленный Блюз')]
    expect(values(matchOptions(opts, 'BONDAGE'))).toEqual([1])
    expect(values(matchOptions(opts, 'мЕДЛЕННЫЙ блЮЗ'))).toEqual([2])
  })

  it('ё в подписи находится по е в запросе', () => {
    expect(values(matchOptions([o(1, 'Тёплый вокал')], 'теплый'))).toEqual([1])
    expect(values(matchOptions([o(1, 'ЁЛКА')], 'елка'))).toEqual([1])
  })

  it('ё в запросе находит е в подписи', () => {
    expect(values(matchOptions([o(1, 'Теплый вокал')], 'тёплый'))).toEqual([1])
    expect(values(matchOptions([o(1, 'елка')], 'Ёлка'))).toEqual([1])
  })
})

describe('краевые случаи пунктов', () => {
  it('порядок результата — как во входном массиве', () => {
    const opts = [o('c', 'rock 3'), o('a', 'rock 1'), o('x', 'jazz'), o('b', 'rock 2')]
    expect(values(matchOptions(opts, 'rock'))).toEqual(['c', 'a', 'b'])
  })

  it('пункт без подписи или с null — как пустая строка: только при пустом запросе', () => {
    const opts = [{ value: 1 }, o(2, null), o(3, 'rock')]
    expect(values(matchOptions(opts, ''))).toEqual([1, 2, 3])
    expect(values(matchOptions(opts, 'rock'))).toEqual([3])
    expect(values(matchOptions(opts, 'null'))).toEqual([])
    expect(values(matchOptions(opts, 'undefined'))).toEqual([])
  })

  it('отключённые пункты фильтруются по тому же правилу, а не выкидываются', () => {
    const opts = [o(1, 'rock', { disabled: true }), o(2, 'jazz', { disabled: true }), o(3, 'rock live')]
    const res = matchOptions(opts, 'rock')
    expect(values(res)).toEqual([1, 3])
    expect(res[0].disabled).toBe(true)
    expect(values(matchOptions(opts, ''))).toEqual([1, 2, 3])
  })

  it('возвращаются сами пункты со всеми полями', () => {
    const a = o(1, 'rock', { disabled: false, extra: 'x' })
    expect(matchOptions([a], 'rock')[0]).toEqual(a)
  })
})

describe('входные данные не меняются', () => {
  it('исходный массив и пункты не мутируются, результат — новый массив', () => {
    const opts = [o(1, 'Тёплый Rock'), o(2, 'jazz'), o(3, null)]
    const snapshot = JSON.parse(JSON.stringify(opts))
    const res = matchOptions(opts, 'ТЕПЛЫЙ')
    expect(opts).toEqual(snapshot)
    expect(res).not.toBe(opts)
    const all = matchOptions(opts, '')
    expect(all).not.toBe(opts)
    expect(opts).toEqual(snapshot)
  })
})
