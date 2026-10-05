// Тесты значков интерфейса: набор ICONS, имена значков в шаблонах и отсутствие цветных
// эмодзи в строках/шаблонах. Ловит класс бага «<AppIcon name="sav" />» — опечатка в имени
// даёт молча пустой значок, и «⚙️ в подписи» — эмодзи вместо значка AppIcon.
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test } from 'vitest'
import { ICONS } from './icons.js'
import RU from './i18n/ru.js'
import EN from './i18n/en.js'

const srcDir = dirname(fileURLToPath(import.meta.url))

// разрешённые текстовые символы-указатели в подсказках
const ALLOWED = new Set(['▶', '⚠'])
const PICTO = /\p{Extended_Pictographic}/gu

function walkVue(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walkVue(p, out)
    else if (name.endsWith('.vue')) out.push(p)
  }
  return out
}

const rel = (f) => f.replace(srcDir + '/', '')
const lineOf = (src, idx) => src.slice(0, idx).split('\n').length

// запрещённые символы в строке: [{ ch, index }]
const badPicto = (s) =>
  [...String(s).matchAll(PICTO)].filter((m) => !ALLOWED.has(m[0])).map((m) => ({ ch: m[0], index: m.index }))

// имена значков из исходника .vue: три формы спецификации
function iconNames(src) {
  const names = []
  for (const tag of src.matchAll(/<AppIcon\b([^<]*?)\/?>/g)) {
    const attrs = tag[1]
    // статично: name="x" (но не :name / v-bind:name)
    for (const m of attrs.matchAll(/(?<![:\w-])name="([^"]*)"/g)) names.push(m[1])
    // привязка: :name="… ? 'a' : 'b'" — литералы в позиции результата (всё выражение
    // или ветка тернарного оператора); операнды сравнений и аргументы функций —
    // не имена значков (theme === 'dark', isPlaying('m' + id))
    for (const m of attrs.matchAll(/(?:^|\s)(?::|v-bind:)name="([^"]*)"/g)) {
      for (const lit of m[1].matchAll(/(?:^|[?:])\s*'([^']*)'/g)) names.push(lit[1])
    }
  }
  // опции VSelect в <script>: icon: 'x'
  for (const m of src.matchAll(/\bicon:\s*'([^']*)'/g)) names.push(m[1])
  return names
}

// блок шаблона: от первого <template> до последнего </template> (вложенные <template v-if> внутри)
function templateBlock(src) {
  const start = src.indexOf('<template')
  const end = src.lastIndexOf('</template>')
  if (start < 0 || end < 0) return { text: '', offset: 0 }
  return { text: src.slice(start, end), offset: start }
}

test('ICONS — объект «имя → разметка SVG» с хотя бы одной фигурой в каждом значке', () => {
  expect(ICONS && typeof ICONS === 'object').toBe(true)
  const entries = Object.entries(ICONS)
  expect(entries.length).toBeGreaterThan(0)
  for (const [name, svg] of entries) {
    expect(typeof svg, `значок ${name}: не строка`).toBe('string')
    expect(svg.trim().length, `значок ${name}: пустая разметка`).toBeGreaterThan(0)
    expect(svg, `значок ${name}: нет фигуры SVG`).toMatch(/<(path|circle|rect|line|polyline|polygon)\b/)
  }
})

test('все имена значков из .vue (AppIcon name / :name, icon: в опциях) есть в ICONS', () => {
  const used = new Map() // имя → первое место использования
  for (const f of walkVue(srcDir)) {
    for (const n of iconNames(readFileSync(f, 'utf8'))) if (!used.has(n)) used.set(n, rel(f))
  }
  // страховка от сломанной регулярки: имён должно найтись заметно больше нуля
  expect(used.size, 'найдено слишком мало имён значков — регулярка сломана?').toBeGreaterThanOrEqual(10)
  const missing = [...used].filter(([n]) => !(n in ICONS)).map(([n, f]) => `${n} (${f})`)
  expect(missing, 'имена значков, которых нет в ICONS').toEqual([])
})

test('разбор имён: три формы находятся, :name без литералов и чужие атрибуты не дают имён', () => {
  const src = `
<template>
  <AppIcon name="a" />
  <AppIcon :name="on ? 'b' : 'c'" class="x" />
  <AppIcon :name="opt.icon" />
  <AppIcon :name="theme === 'dark' ? 'f' : 'g'" />
  <AppIcon :name="isPlaying('m' + id) ? 'h' : 'i'" />
  <AppIcon :name="'j'" />
  <AppIcon v-if="p.id === 'reset'" name="d" />
</template>
<script setup>
const opts = [{ value: 1, label: 'x', icon: 'e' }]
</script>`
  expect(iconNames(src).sort()).toEqual(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'])
})

test('строки ru/en без цветных эмодзи (кроме ▶ и ⚠)', () => {
  const bad = []
  for (const [lang, dict] of [['ru', RU], ['en', EN]]) {
    for (const [key, s] of Object.entries(dict)) {
      for (const { ch } of badPicto(s)) bad.push(`${lang}:${key} «${ch}» U+${ch.codePointAt(0).toString(16).toUpperCase()}`)
    }
  }
  expect(bad, 'эмодзи в строках интерфейса — нужен значок AppIcon').toEqual([])
})

test('шаблоны .vue без цветных эмодзи (кроме ▶ и ⚠), HTML-комментарии не считаются', () => {
  const bad = []
  for (const f of walkVue(srcDir)) {
    const src = readFileSync(f, 'utf8')
    const { text, offset } = templateBlock(src)
    // комментарии заменяем пробелами той же длины — номера строк не сдвигаются
    const clean = text.replace(/<!--[\s\S]*?-->/g, (c) => c.replace(/[^\n]/g, ' '))
    for (const { ch, index } of badPicto(clean)) {
      bad.push(`${rel(f)}:${lineOf(src, offset + index)} «${ch}» U+${ch.codePointAt(0).toString(16).toUpperCase()}`)
    }
  }
  expect(bad, 'эмодзи в шаблонах — нужен значок AppIcon').toEqual([])
})

test('проверка эмодзи пропускает ▶ и ⚠, ловит цветной символ', () => {
  expect(badPicto('▶ играть ⚠ внимание')).toEqual([])
  expect(badPicto('сохранить 💾').map((b) => b.ch)).toEqual(['💾'])
})
