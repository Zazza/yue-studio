// Тесты карточки internal-own-track, этап 8а, условие 68 (тест-кейс ТК102): эталоны партий для паритета
// Go ↔ JS. Go-порты PartNotes / PercHits / BarsFromBeat (internal/studio) обязаны давать то же, что
// synthPart.partNotes / percPart.percHits / percPart.barsFromBeat; Go-тест (internal/studio/parts_test.go)
// сверяет их с internal/studio/testdata/parts_golden.json с точностью 1e-9.
//
// Файл эталонов генерирует этот тест: WRITE_GOLDEN=1 npx vitest run src/partsGolden.test.js — пишет файл;
// без переменной — сверяет файл с генерацией (как данные MCP: поменял JS — перегенерируй эталоны).
// Опции в эталоне записаны полностью (умолчания JS подставлены явно): Go получает их как есть.
//
// Набор случаев — из ТК102: стили pad/arp/pulse/drone, octave −1/0/2, sections, все рисунки, swing 0,2,
// accent 0/0,5/1, неизвестный аккорд, «D/F#», пустые такты; barsFromBeat — тоже.
// Не входит: partNotes с sections [] — у JS это «ни одного такта», а карточка (условие 68) для рецепта
// задаёт «пусто — все»; расхождение вынесено в отчёт test-author.
import { describe, it, expect } from 'vitest'
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import process from 'node:process'
import { partNotes } from './synthPart.js'
import { percHits, barsFromBeat, PERC_PATTERNS } from './percPart.js'

const GOLDEN = resolve(dirname(fileURLToPath(import.meta.url)), '../../internal/studio/testdata/parts_golden.json')

const bar = (start, end, chord, section) => ({ start, end, chord, section })

// песня по 2 с на такт (120 BPM): куплет, припев с повтором аккорда (тянется у drone), аккорд с басом
const SONG = [
  bar(0, 2, 'Am', 'verse'), bar(2, 4, 'F', 'verse'), bar(4, 6, 'C', 'chorus'), bar(6, 8, 'C', 'chorus'),
  bar(8, 10, 'G7', 'chorus'), bar(10, 12, 'D/F#', 'bridge'), bar(12, 14, 'Bbmaj7', 'bridge'), bar(14, 16, 'Esus4', 'outro'),
]
// трудные такты: неизвестный аккорд, пустой аккорд, нулевой и отрицательный такты, неровная длина
const ODD = [
  bar(0.25, 2.1, 'Dm', 'verse'), bar(2.1, 3.9, 'Xyz', 'verse'), bar(3.9, 5.7, '', 'verse'),
  bar(5.7, 5.7, 'C', 'chorus'), bar(5.7, 7.5, 'C#dim', 'chorus'), bar(7.5, 7.2, 'G', 'chorus'),
  bar(7.5, 9.3, 'Ebaug', 'chorus'), bar(9.3, 11.1, 'F#m7', 'outro'), bar(11.1, 12.9, 'A5', 'outro'),
  bar(12.9, 14.7, 'Bsus2', 'outro'), bar(14.7, 16.5, 'Gmin', 'outro'),
]
// drone: тот же аккорд подряд тянется; разрыв неизвестным аккордом и секцией прерывает
const DRONE = [
  bar(0, 2, 'E', 'verse'), bar(2, 4, 'E', 'verse'), bar(4, 6, 'Xyz', 'verse'), bar(6, 8, 'E', 'verse'),
  bar(8, 10, 'E', 'chorus'), bar(10, 12, 'A', 'chorus'), bar(12.5, 14.5, 'A', 'chorus'),
]

const synthCases = []
const synthCase = (name, bars, opts) => {
  const full = { style: 'pad', octave: 0, sections: null, ...opts }
  synthCases.push({ name, bars, opts: full, out: partNotes(bars, full) })
}
for (const style of ['pad', 'arp', 'pulse', 'drone']) {
  for (const octave of [-1, 0, 2]) synthCase(`song ${style} octave ${octave}`, SONG, { style, octave })
  synthCase(`odd ${style}`, ODD, { style })
  synthCase(`song ${style} chorus`, SONG, { style, sections: ['chorus'] })
  synthCase(`drone bars ${style}`, DRONE, { style })
  synthCase(`drone bars ${style} verse+chorus`, DRONE, { style, sections: ['verse', 'chorus'] })
}
synthCase('song pad sections verse+bridge', SONG, { style: 'pad', sections: ['verse', 'bridge'] })
synthCase('song pad no matching section', SONG, { style: 'pad', sections: ['solo'] })
synthCase('no bars', [], { style: 'pad' })

const percCases = []
const percCase = (name, bars, opts) => {
  const full = { pattern: 'eighths', sections: null, swing: 0, accent: 1, ...opts }
  percCases.push({ name, bars, opts: full, out: percHits(bars, full) })
}
for (const pattern of PERC_PATTERNS) {
  percCase(`song ${pattern}`, SONG, { pattern })
  percCase(`odd ${pattern}`, ODD, { pattern })
  percCase(`song ${pattern} swing 0.2`, SONG, { pattern, swing: 0.2 })
  for (const accent of [0, 0.5, 1]) percCase(`song ${pattern} accent ${accent}`, SONG, { pattern, accent })
}
percCase('song sixteenths swing 0.2 accent 0.5 chorus', SONG, { pattern: 'sixteenths', swing: 0.2, accent: 0.5, sections: ['chorus'] })
percCase('song backbeat sections empty = all', SONG, { pattern: 'backbeat', sections: [] })
percCase('song eighths sections bridge+outro', SONG, { pattern: 'eighths', sections: ['bridge', 'outro'] })
percCase('song offbeat swing 0.5', SONG, { pattern: 'offbeat', swing: 0.5 })
percCase('no bars', [], { pattern: 'fours' })

const beatCases = []
const beatCase = (name, grid, dur, shift) => beatCases.push({ name, grid, dur, shift, out: barsFromBeat(grid, dur, shift) })
beatCase('120 bpm 8 s', { bpm: 120, offset: 0 }, 8, 0)
beatCase('120 bpm offset 0.3 partial last', { bpm: 120, offset: 0.3 }, 7.1, 0)
for (const shift of [1, 2, 3]) beatCase(`97.3 bpm offset 0.41 shift ${shift}`, { bpm: 97.3, offset: 0.41 }, 20.5, shift)
beatCase('shift 5 clamps to 3', { bpm: 128, offset: 0.1 }, 10, 5)
beatCase('shift -1 clamps to 0', { bpm: 128, offset: 0.1 }, 10, -1)
beatCase('negative offset clamps to 0', { bpm: 140, offset: -0.2 }, 6, 0)
beatCase('dur exactly on bar edge', { bpm: 120, offset: 0 }, 6, 0)
beatCase('bpm 0 → none', { bpm: 0, offset: 0 }, 8, 0)
beatCase('dur 0 → none', { bpm: 120, offset: 0 }, 0, 0)

const golden = { partNotes: synthCases, percHits: percCases, barsFromBeat: beatCases }
// по случаю на строку: эталон читается и сравнивается в диффе по случаям
const section = (k) => `"${k}": [\n${golden[k].map((c) => JSON.stringify(c)).join(',\n')}\n]`
const text = `{\n${Object.keys(golden).map(section).join(',\n')}\n}\n`

describe('эталоны партий для Go (ТК102)', () => {
  it('случаи не пустые: есть ноты, удары и такты', () => {
    expect(synthCases.filter((c) => c.out.length).length).toBeGreaterThan(10)
    expect(percCases.filter((c) => c.out.length).length).toBeGreaterThan(10)
    expect(beatCases.filter((c) => c.out.length).length).toBeGreaterThan(5)
  })

  it('internal/studio/testdata/parts_golden.json совпадает с генерацией', () => {
    if (process.env.WRITE_GOLDEN === '1') {
      mkdirSync(dirname(GOLDEN), { recursive: true })
      writeFileSync(GOLDEN, text)
    }
    let have
    try {
      have = readFileSync(GOLDEN, 'utf8')
    } catch {
      have = '' // файла нет — сверка ниже упадёт с подсказкой
    }
    expect(have === text, 'эталоны устарели: WRITE_GOLDEN=1 npx vitest run src/partsGolden.test.js').toBe(true)
  })
})
