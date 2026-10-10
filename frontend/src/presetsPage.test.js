// Тесты страницы «Пресеты звука» по исходникам: отдельная страница с пунктом в меню ⋮,
// библиотека пресетов переехала туда со страницы «Инструменты», подписи меню в обоих
// словарях и раздел в docs/ui-guide.md. Ловит «страницу сделали, а пункт меню забыли»
// и «пресеты остались дублем на странице Инструменты».
import { readFileSync, existsSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test } from 'vitest'
import RU from './i18n/ru.js'
import EN from './i18n/en.js'

const srcDir = dirname(fileURLToPath(import.meta.url))
const repoDir = join(srcDir, '..', '..')
const read = (p) => readFileSync(p, 'utf8')

// блок шаблона: от первого <template> до последнего </template>
function templateOf(src) {
  const start = src.indexOf('<template')
  const end = src.lastIndexOf('</template>')
  return start < 0 || end < 0 ? '' : src.slice(start, end)
}

// блок <script>: всё до первого <template> (импорты лежат там)
function scriptOf(src) {
  const m = src.match(/<script\b[^>]*>([\s\S]*?)<\/script>/g)
  return m ? m.join('\n') : ''
}

// импорт компонента по имени из файла .vue (любой относительный путь)
const importsComponent = (src, name) =>
  new RegExp(`import\\s+${name}\\s+from\\s+['"][^'"]*${name}\\.vue['"]`).test(scriptOf(src))

// тег компонента в шаблоне: <Name ...> или <Name/>, а также kebab-case
function rendersComponent(src, name) {
  const kebab = name.replace(/([a-z])([A-Z])/g, '$1-$2').toLowerCase()
  return new RegExp(`<(${name}|${kebab})[\\s/>]`).test(templateOf(src))
}

const APP = join(srcDir, 'App.vue')
const PRESETS_PAGE = join(srcDir, 'components', 'PresetsPage.vue')
const INSTRUMENTS_PAGE = join(srcDir, 'components', 'InstrumentsPage.vue')
const UI_GUIDE = join(repoDir, 'docs', 'ui-guide.md')

// --- Условие 89: страница и пункт меню ---

test('в меню ⋮ App.vue есть пункт li, открывающий страницу пресетов: navGo(\'presets\')', () => {
  const tpl = templateOf(read(APP))
  const items = [...tpl.matchAll(/<li\b[^>]*>/g)].map((m) => m[0])
  const presetsItems = items.filter((li) => /@click="navGo\(\s*'presets'\s*\)"/.test(li))
  expect(presetsItems, 'нет <li @click="navGo(\'presets\')"> в шаблоне App.vue').toHaveLength(1)
})

test('страница PresetsPage.vue существует, импортирует и рендерит PresetLibrary', () => {
  expect(existsSync(PRESETS_PAGE), 'нет файла components/PresetsPage.vue').toBe(true)
  const src = read(PRESETS_PAGE)
  expect(importsComponent(src, 'PresetLibrary'), 'PresetsPage не импортирует PresetLibrary').toBe(true)
  expect(rendersComponent(src, 'PresetLibrary'), 'PresetsPage не рендерит <PresetLibrary>').toBe(true)
})

test('App.vue импортирует и рендерит PresetsPage', () => {
  const src = read(APP)
  expect(importsComponent(src, 'PresetsPage'), 'App.vue не импортирует PresetsPage').toBe(true)
  expect(rendersComponent(src, 'PresetsPage'), 'App.vue не рендерит <PresetsPage>').toBe(true)
})

test('на странице «Инструменты» раздела пресетов нет: PresetLibrary не импортируется и не рендерится', () => {
  const src = read(INSTRUMENTS_PAGE)
  expect(importsComponent(src, 'PresetLibrary'), 'InstrumentsPage всё ещё импортирует PresetLibrary').toBe(false)
  expect(rendersComponent(src, 'PresetLibrary'), 'InstrumentsPage всё ещё рендерит <PresetLibrary>').toBe(false)
})

// --- ТК129: подписи и документация ---

test.each([
  ['ru', RU],
  ['en', EN],
])('словарь %s: есть непустые nav.presets и nav.presets.tip', (_lang, dict) => {
  for (const key of ['nav.presets', 'nav.presets.tip']) {
    expect(typeof dict[key], `нет ключа ${key}`).toBe('string')
    expect(dict[key].trim().length, `пустой ${key}`).toBeGreaterThan(0)
  }
})

// разделы markdown уровня ##: [{ title, body }]
function h2Sections(md) {
  const lines = md.split('\n')
  const out = []
  for (const line of lines) {
    const m = line.match(/^##\s+(.*)$/)
    if (m) out.push({ title: m[1].trim(), body: '' })
    else if (out.length) out[out.length - 1].body += line + '\n'
  }
  return out
}

test('docs/ui-guide.md: есть раздел «## Пресеты звука»', () => {
  const sections = h2Sections(read(UI_GUIDE))
  expect(sections.some((s) => s.title.startsWith('Пресеты звука'))).toBe(true)
})

test('docs/ui-guide.md: в разделе «## Инструменты» слов «Пресеты звука» нет', () => {
  const sections = h2Sections(read(UI_GUIDE))
  const instr = sections.filter((s) => s.title.startsWith('Инструменты'))
  expect(instr.length, 'нет раздела «## Инструменты»').toBeGreaterThan(0)
  for (const s of instr) expect(s.body).not.toMatch(/Пресеты звука/i)
})
