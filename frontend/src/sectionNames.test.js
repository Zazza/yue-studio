// Тесты подписей секций песни: свободные английские названия от YuE → подпись в интерфейсе.
import { describe, it, expect } from 'vitest'
import { sectionLabel } from './sectionNames.js'

describe('sectionLabel: русские названия базовых секций', () => {
  it.each([
    ['intro', 'вступление'],
    ['verse', 'куплет'],
    ['pre-chorus', 'предприпев'],
    ['prechorus', 'предприпев'],
    ['pre chorus', 'предприпев'],
    ['chorus', 'припев'],
    ['hook', 'припев'],
    ['bridge', 'бридж'],
    ['outro', 'концовка'],
    ['interlude', 'проигрыш'],
    ['instrumental', 'проигрыш'],
    ['breakdown', 'брейк'],
    ['drum fill', 'сбивка'],
    ['fill', 'сбивка'],
  ])('%s → %s', (raw, want) => {
    expect(sectionLabel(raw, 'ru')).toBe(want)
  })

  it('локаль по умолчанию — ru', () => {
    expect(sectionLabel('chorus')).toBe('припев')
    expect(sectionLabel('verse 2')).toBe('куплет 2')
  })
})

describe('sectionLabel: соло', () => {
  it.each([
    ['solo', 'соло'],
    ['guitar solo', 'соло гитары'],
    ['blues guitar solo', 'соло гитары'],
    ['piano solo', 'соло клавиш'],
    ['keys solo', 'соло клавиш'],
    ['synth solo', 'соло клавиш'],
    ['sax solo', 'соло'],
    ['Guitar Solo', 'соло гитары'],
  ])('%s → %s', (raw, want) => {
    expect(sectionLabel(raw, 'ru')).toBe(want)
  })
})

describe('sectionLabel: номер в конце сохраняется', () => {
  it.each([
    ['verse 2', 'куплет 2'],
    ['Chorus2', 'припев 2'],
    ['pre-chorus 3', 'предприпев 3'],
    ['guitar solo 2', 'соло гитары 2'],
    ['verse 10', 'куплет 10'],
  ])('%s → %s', (raw, want) => {
    expect(sectionLabel(raw, 'ru')).toBe(want)
  })
})

describe('sectionLabel: регистр и пробелы не важны', () => {
  it.each([
    [' CHORUS ', 'припев'],
    ['Verse', 'куплет'],
    ['  Pre-Chorus  ', 'предприпев'],
    ['DRUM   FILL', 'сбивка'],
    ['  verse   2 ', 'куплет 2'],
  ])('%j → %s', (raw, want) => {
    expect(sectionLabel(raw, 'ru')).toBe(want)
  })

  it('pre-chorus не путается с chorus', () => {
    expect(sectionLabel('pre-chorus', 'ru')).not.toBe('припев')
  })
})

describe('sectionLabel: неизвестная секция', () => {
  it('возвращается исходной строкой', () => {
    expect(sectionLabel('drop', 'ru')).toBe('drop')
  })

  it('обрезаются только пробелы по краям, регистр сохраняется', () => {
    expect(sectionLabel('  Drop  ', 'ru')).toBe('Drop')
  })
})

describe('sectionLabel: пустой ввод', () => {
  it.each([
    ['', 'ru'],
    [null, 'ru'],
    [undefined, 'ru'],
    ['', 'en'],
    [null, 'en'],
    [undefined, 'en'],
  ])('%j (%s) → пустая строка', (raw, locale) => {
    expect(sectionLabel(raw, locale)).toBe('')
  })

  it('без аргументов → пустая строка', () => {
    expect(sectionLabel()).toBe('')
  })
})

describe('sectionLabel: locale en', () => {
  it.each([
    ['Verse 2', 'Verse 2'],
    ['  Verse 2  ', 'Verse 2'],
    ['chorus', 'chorus'],
    [' CHORUS ', 'CHORUS'],
    ['blues guitar solo', 'blues guitar solo'],
    ['drop', 'drop'],
  ])('%j → %j', (raw, want) => {
    expect(sectionLabel(raw, 'en')).toBe(want)
  })
})
