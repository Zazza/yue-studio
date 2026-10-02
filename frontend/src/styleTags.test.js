import { describe, it, expect } from 'vitest'
import { parseStyleTags, styleRows, styleTagOrder } from './styleTags.js'

// живой пример с карточки трека: пост-панк со всеми типовыми тегами YuE
const STYLE = [
  'English', 'post-punk', 'post-punk revival',
  'relentless driving urgent beat', 'motorik pulse',
  'interlocking minor arpeggios', 'staccato chords',
  'sparse restrained verses', 'explosive huge choruses with fuzz wall of guitars',
  'male vocals', 'powerful delivery', 'clean male tenor-baritone', 'wide range',
  'doomed romance', 'night city', 'dry tight punchy modern', '138 BPM',
].join(', ')

describe('parseStyleTags', () => {
  it('язык распознаётся по словарю', () => {
    expect(parseStyleTags(STYLE).language).toEqual(['English'])
  })

  it('жанр узнаёт и составной тег: «post-punk revival» содержит «post-punk»', () => {
    expect(parseStyleTags(STYLE).genre).toEqual(['post-punk', 'post-punk revival'])
  })

  it('BPM выделяется в отдельную группу', () => {
    expect(parseStyleTags(STYLE).bpm).toEqual(['138 BPM'])
    expect(parseStyleTags('fast grindcore, 90 BPM').bpm).toEqual(['90 BPM'])
  })

  it('вокальные фразы собираются вместе: голос, подача, диапазон', () => {
    const tags = parseStyleTags(STYLE)
    expect(tags.vocals).toEqual(['male vocals', 'powerful delivery', 'clean male tenor-baritone', 'wide range'])
  })

  it('части формы песни — куплеты/припевы/концовка — своей группой', () => {
    const tags = parseStyleTags('sparse restrained verses, hard stop ending')
    expect(tags.structure).toEqual(['sparse restrained verses', 'hard stop ending'])
  })

  it('нераспознанное не теряется, а попадает в прочее', () => {
    expect(parseStyleTags('cosmic llamas gravity').other).toEqual(['cosmic llamas gravity'])
  })

  it('пустая и мусорная строка не ломают разбор', () => {
    expect(parseStyleTags('')).toEqual({})
    expect(parseStyleTags('  ,  ,,  ')).toEqual({})
  })
})

describe('styleRows', () => {
  it('группы идут в порядке полей формы, форма/темп/прочее — в конце', () => {
    const rows = styleRows(STYLE).map(([slot]) => slot)
    expect(rows.indexOf('language')).toBeLessThan(rows.indexOf('genre'))
    expect(rows.indexOf('genre')).toBeLessThan(rows.indexOf('structure'))
    expect(rows.indexOf('structure')).toBeLessThan(rows.indexOf('bpm'))
    expect(styleTagOrder.indexOf('other')).toBe(styleTagOrder.length - 1)
  })

  it('пустые группы выбрасываются', () => {
    expect(styleRows('138 BPM')).toEqual([['bpm', ['138 BPM']]])
  })
})
