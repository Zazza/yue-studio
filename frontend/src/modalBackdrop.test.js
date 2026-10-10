// Карточка internal-own-track, этап 12б, условие 92 / ТК131: модалки и
// страницы-модалки не закрываются кликом мимо окна. По исходникам: во всех
// .vue в src/ (рекурсивно, кроме сгенерированного src/wailsjs) у элемента
// с классом modal-backdrop нет обработчика нажатия мыши (ни .self, ни без),
// а у каждого файла с подложкой есть кнопка закрытия вне подложки.
import { readdirSync, readFileSync } from 'node:fs'
import { join, dirname, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))

function vueFiles(dir) {
  const out = []
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name)
    if (e.isDirectory()) {
      if (relative(here, p) === 'wailsjs') continue
      out.push(...vueFiles(p))
    } else if (e.name.endsWith('.vue')) {
      out.push(p)
    }
  }
  return out
}

// Шаблон компонента: от первого <template> до последнего </template>
// (вложенные <template v-if> остаются внутри).
function templateOf(src) {
  const start = src.indexOf('<template')
  const end = src.lastIndexOf('</template>')
  if (start < 0 || end < 0) return ''
  return src.slice(start, end)
}

// Открывающие теги шаблона с атрибутами. Кавычки учитываются, чтобы
// выражения вида "a > b" внутри атрибута не обрывали тег.
function openTags(tpl) {
  const tags = []
  let i = 0
  while (i < tpl.length) {
    if (tpl.startsWith('<!--', i)) {
      const e = tpl.indexOf('-->', i)
      i = e < 0 ? tpl.length : e + 3
      continue
    }
    if (tpl[i] === '<' && /[A-Za-z]/.test(tpl[i + 1] || '')) {
      const nameMatch = /^<([A-Za-z][\w-]*)/.exec(tpl.slice(i))
      let j = i + nameMatch[0].length
      let quote = null
      while (j < tpl.length) {
        const c = tpl[j]
        if (quote) {
          if (c === quote) quote = null
        } else if (c === '"' || c === "'") {
          quote = c
        } else if (c === '>') {
          break
        }
        j++
      }
      const attrsSrc = tpl.slice(i + nameMatch[0].length, j)
      tags.push({ name: nameMatch[1], attrs: parseAttrs(attrsSrc), raw: tpl.slice(i, j + 1) })
      i = j + 1
      continue
    }
    i++
  }
  return tags
}

function parseAttrs(s) {
  const attrs = []
  const re = /([^\s=/]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?/g
  let m
  while ((m = re.exec(s))) attrs.push({ name: m[1], value: m[2] ?? m[3] ?? m[4] ?? '' })
  return attrs
}

function staticClasses(tag) {
  return tag.attrs
    .filter((a) => a.name === 'class')
    .flatMap((a) => a.value.split(/\s+/))
    .filter(Boolean)
}

function isBackdrop(tag) {
  if (staticClasses(tag).includes('modal-backdrop')) return true
  // :class / v-bind:class с упоминанием modal-backdrop тоже считаем подложкой
  return tag.attrs.some((a) => /^(:|v-bind:)class$/.test(a.name) && a.value.includes('modal-backdrop'))
}

// Обработчики нажатия мыши/указателя: click, mousedown и их родня
// (любой из них даёт то же «закрылось от случайного клика»).
const POINTER_EVENTS = ['click', 'dblclick', 'mousedown', 'mouseup', 'pointerdown', 'pointerup']
function pointerHandlers(tag) {
  return tag.attrs.filter((a) => {
    const m = /^(?:@|v-on:)([\w-]+)/.exec(a.name)
    if (m) return POINTER_EVENTS.includes(m[1])
    // v-on="{ click: ... }" — объектная форма
    if (a.name === 'v-on') return POINTER_EVENTS.some((ev) => new RegExp(`\\b${ev}\\b`).test(a.value))
    return false
  })
}

function closeExpressions(backdrop) {
  const exprs = [/emit\(\s*['"]close['"]\s*\)/, /^\s*close(\s*\(\s*\))?\s*$/, /\bcancelConfirm\b/, /\bopen\s*=\s*false\b/]
  const vif = backdrop.attrs.find((a) => a.name === 'v-if')
  if (vif && /^\s*[\w.]+\s*$/.test(vif.value)) {
    const v = vif.value.trim().replace(/\./g, '\\.')
    exprs.push(new RegExp(`(^|[^\\w.])${v}\\s*=\\s*false\\b`))
  }
  return exprs
}

const files = vueFiles(here)
  .map((p) => ({ rel: relative(here, p), tags: openTags(templateOf(readFileSync(p, 'utf8'))) }))
  .filter((f) => f.tags.some(isBackdrop))

test('подложки модалок найдены (обход исходников работает)', () => {
  const names = files.map((f) => f.rel)
  // Те, что названы в условии 91, — подложка у них есть.
  for (const n of [
    'App.vue',
    'components/WelcomeModal.vue',
    'components/PlanModal.vue',
    'components/PresetsPage.vue',
    'components/CopilotModal.vue',
    'components/CorpusPage.vue',
    'components/MetricsModal.vue',
    'components/InstrumentsPage.vue',
    'components/ConfirmModal.vue',
    'components/LibraryPage.vue',
    'components/VoicesPage.vue',
    'components/SettingsPage.vue',
  ]) {
    expect(names, `нет подложки modal-backdrop в ${n}`).toContain(n)
  }
  expect(names.some((n) => n.startsWith('wailsjs'))).toBe(false)
})

describe('клик по подложке не закрывает модалку (ТК131)', () => {
  for (const f of files) {
    test(`${f.rel}: у .modal-backdrop нет обработчика клика/нажатия`, () => {
      for (const tag of f.tags.filter(isBackdrop)) {
        const bad = pointerHandlers(tag).map((a) => `${a.name}="${a.value}"`)
        expect(bad, `${f.rel}: ${tag.raw}`).toEqual([])
      }
    })

    test(`${f.rel}: есть кнопка закрытия вне подложки`, () => {
      for (const backdrop of f.tags.filter(isBackdrop)) {
        const exprs = closeExpressions(backdrop)
        const closer = f.tags.some(
          (tag) =>
            tag !== backdrop &&
            !isBackdrop(tag) &&
            tag.attrs.some(
              (a) => /^(?:@|v-on:)click(\.|$)/.test(a.name) && exprs.some((re) => re.test(a.value)),
            ),
        )
        expect(closer, `${f.rel}: нет кнопки закрытия для ${backdrop.raw}`).toBe(true)
      }
    })
  }
})
