// Обёртки api.js: каждая импортированная из wailsjs функция там есть, и обёртки фраз «Инструментов» на месте
// (условие 96). Поймано вживую: правка api.js не записалась, сборка и тесты были зелёными, а на странице —
// «z.fxPhrases is not a function».
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test } from 'vitest'
import { api } from './api.js'
import * as App from './wailsjs/go/main/App.js'

const here = dirname(fileURLToPath(import.meta.url))

test('фразы «Инструментов»: fxPhrases, fxPhrase, playLoop — функции api', () => {
  for (const k of ['fxPhrases', 'fxPhrase', 'playLoop']) expect(typeof api[k], k).toBe('function')
})

test('всё, что api.js импортирует из wailsjs, там есть', () => {
  const src = readFileSync(join(here, 'api.js'), 'utf8')
  const m = /import\s*{([^}]*)}\s*from\s*'\.\/wailsjs\/go\/main\/App'/.exec(src)
  expect(m).toBeTruthy()
  const names = m[1].split(',').map((s) => s.trim()).filter(Boolean)
  const missing = names.filter((n) => typeof App[n] !== 'function')
  expect(missing).toEqual([])
})
