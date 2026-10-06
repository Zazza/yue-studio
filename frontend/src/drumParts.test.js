// Карточка internal-roformer-stems, тест-кейс 10: части барабанов в студии.
// Подписи частей (ru/en) — ключи studio.dsp.target.<часть>; паритет словарей
// в целом проверяет i18n.test.js, здесь — что ключи частей есть в обоих и
// русские подписи те, что в условии 7. Переключатели «минуса» — только
// основные дорожки: части барабанов в сумму трека не входят.
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test } from 'vitest'
import RU from './i18n/ru.js'
import EN from './i18n/en.js'

const here = dirname(fileURLToPath(import.meta.url))

const PARTS = ['kick', 'snare', 'toms', 'hh', 'ride', 'crash']
const RU_LABELS = {
  kick: 'бочка',
  snare: 'малый барабан',
  toms: 'томы',
  hh: 'хай-хэт',
  ride: 'райд',
  crash: 'крэш',
}

test('подписи частей барабанов есть в ru и en', () => {
  for (const p of PARTS) {
    const key = `studio.dsp.target.${p}`
    for (const [lang, dict] of [['ru', RU], ['en', EN]]) {
      expect(typeof dict[key], `${lang}: нет ${key}`).toBe('string')
      expect(dict[key].trim(), `${lang}: пустая подпись ${key}`).not.toBe('')
      expect(dict[key], `${lang}: подпись ${key} — сырой ключ`).not.toBe(key)
    }
  }
})

test('русские подписи частей — из условия карточки', () => {
  for (const p of PARTS) {
    expect(RU[`studio.dsp.target.${p}`].toLowerCase()).toBe(RU_LABELS[p])
  }
})

test('английские подписи частей различимы между собой', () => {
  const en = PARTS.map((p) => EN[`studio.dsp.target.${p}`].toLowerCase())
  expect(new Set(en).size).toBe(PARTS.length)
})

test('переключатели «минуса» — только основные дорожки, без частей барабанов', () => {
  const src = readFileSync(join(here, 'components', 'StudioPage.vue'), 'utf8')
  const lists = [...src.matchAll(/<[^>]*v-for="\w+ in \[([^\]]*)\]"[^>]*class="[^"]*stem-toggle[^"]*"/g)]
    .concat([...src.matchAll(/<[^>]*class="[^"]*stem-toggle[^"]*"[^>]*v-for="\w+ in \[([^\]]*)\]"/g)])
  expect(lists.length, 'не найден список переключателей stem-toggle').toBeGreaterThan(0)
  for (const m of lists) {
    const names = [...m[1].matchAll(/'(\w+)'/g)].map((x) => x[1])
    expect(names).toEqual(['drums', 'bass', 'other', 'vocals'])
  }
})
