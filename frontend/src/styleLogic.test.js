// Тесты чистой логики стиля/стиха — по спецификации, как должно работать.
import { describe, it, expect } from 'vitest'
import {
  buildStyleLine, dictStyle, cleanLyrics, effectiveLyrics, instrumentalLyrics, SLOT_ORDER,
} from './styleLogic.js'
import { slotOptions, slotKeys } from './slotOptions.js'

describe('buildStyleLine', () => {
  it('склеивает непустые слоты в порядке формы, BPM последним', () => {
    const line = buildStyleLine({
      language: 'Russian', genre: 'блюз', rhythm: '', guitars: 'слайд-гитара',
      keys: '', vocals: '', mood: 'грустно', production: 'чистая студия', bpm: 90,
    })
    expect(line).toBe('Russian, блюз, слайд-гитара, грустно, чистая студия, 90 BPM')
  })

  it('пропускает bpm=0/null и пустую стойку', () => {
    expect(buildStyleLine({ genre: 'панк', bpm: 0, bpm2: null }, '')).toBe('панк')
    expect(buildStyleLine({ genre: 'панк', bpm: null })).toBe('панк')
  })

  it('строка стойки идёт после слотов, до BPM', () => {
    const line = buildStyleLine({ genre: 'рок', bpm: 120 }, 'fuzz lead guitar')
    expect(line).toBe('рок, fuzz lead guitar, 120 BPM')
  })

  it('пустая форма → пустая строка (кнопки отправки заблокированы)', () => {
    expect(buildStyleLine({})).toBe('')
  })
})

describe('dictStyle', () => {
  it('известное русское значение заменяется английским тегом', () => {
    expect(dictStyle('блюз')).toBe('blues')
    expect(dictStyle('Блюз')).toBe('blues')   // регистронезависимо
  })

  it('самописное значение проходит как есть (уйдёт в автоперевод)', () => {
    expect(dictStyle('марши-фанфары космодрома')).toBe('марши-фанфары космодрома')
  })

  it('английское проходит без изменений', () => {
    expect(dictStyle('doom metal')).toBe('doom metal')
  })

  it('каждое значение словаря переводится само в себя (целостность словаря)', () => {
    for (const [ru, en] of slotOptions.genre) {
      expect(dictStyle(ru), ru).toBe(en)
    }
  })
})

describe('cleanLyrics', () => {
  it('вырезает строки-пометки с #, остальные сохраняет как есть', () => {
    const src = '[Verse]\n# ударения: сАмо\nтекст строки\n  # отступ-пометка\n[Chorus]\nприпев'
    expect(cleanLyrics(src)).toBe('[Verse]\nтекст строки\n[Chorus]\nприпев')
  })

  it('пустой текст → пустой текст', () => {
    expect(cleanLyrics('')).toBe('')
  })
})

describe('instrumentalLyrics / effectiveLyrics', () => {
  it('auto = одна секция [Instrumental]', () => {
    expect(instrumentalLyrics('auto')).toBe('[Instrumental]')
  })

  it('каждый режим длительности даёт заявленное число секций', () => {
    // spec: n секций, разделённых пустой строкой
    const cases = { s1: 3, s2: 8, s3: 16, s4: 36 }
    for (const [id, n] of Object.entries(cases)) {
      expect(instrumentalLyrics(id).split('\n\n').length, id).toBe(n)
    }
  })

  it('неизвестный режим деградирует к auto (1 секция)', () => {
    expect(instrumentalLyrics('no-such-mode')).toBe('[Instrumental]')
  })

  it('effectiveLyrics: без слов — заглушка, со словами — чистка #', () => {
    expect(effectiveLyrics('[Verse]\nx\n# пометка', true, 'auto')).toBe('[Instrumental]')
    expect(effectiveLyrics('[Verse]\nтекст\n# пометка', false, 'auto')).toBe('[Verse]\nтекст')
  })
})

describe('целостность данных слотов', () => {
  it('slotMeta покрывает ровно те поля, что идут в строку стиля (без bpm)', () => {
    const metaKeys = [...slotKeys].sort()
    expect(metaKeys).toEqual([...SLOT_ORDER].sort())
  })

  it('каждая опция — пара непустых [ru, en]', () => {
    for (const [key, opts] of Object.entries(slotOptions)) {
      expect(opts.length, key).toBeGreaterThan(0)
      for (const [ru, en] of opts) {
        expect(String(ru).trim(), `${key}: ${ru}`).not.toBe('')
        expect(String(en).trim(), `${key}: ${ru}`).not.toBe('')
      }
    }
  })

  it('русские значения не дублируются внутри одного поля', () => {
    for (const [key, opts] of Object.entries(slotOptions)) {
      const rus = opts.map(([ru]) => ru.trim().toLowerCase())
      expect(new Set(rus).size, key).toBe(rus.length)
    }
  })
})
