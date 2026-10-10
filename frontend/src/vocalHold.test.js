// Тесты карточки internal-own-track, этап 11 «Голос: протянуть концы фраз»: условия 87–88 (тест-кейс ТК128).
// Приём vocalHold (applyTrick): нота, за которой в том же такте сразу идёт пауза z, удлиняется на
// min(длина паузы, полдоли) за счёт паузы; пауза укорачивается (до 0 — исчезает). Ритм начал нот,
// высоты, число нот, длина такта, аккорды — те же; вне выделения и другие голоса — без изменений;
// такт с дробными длинами (/2) не трогается. vocalHold — в revoiceSpecKinds. Кнопка — по исходнику.
// Написаны по карточке, без чтения реализации.
// Толкования: targets — как у vocalVary ({voice, bar}, bar — сквозной номер такта голоса);
// цель на голосе Ins игнорируется (приём только для Vocal); pickTargets для vocalHold берёт только
// вокальный голос, как для vocalVary (то же условие видимости кнопки).
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, it, expect } from 'vitest'
import { applyTrick, pickTargets } from './abcEdit.js'
import { revoiceSpecKinds } from './vocalParts.js'

const here = dirname(fileURLToPath(import.meta.url))

const plan = (vocalLine, insLine = 'E4E4E4E4|', unit = '1/16') => [
  'X:1', 'M:4/4', `L:${unit}`, 'K:Em', 'V: Vocal', vocalLine, 'V: Ins', insLine,
].join('\n')

// такты голоса: непустые чанки между | в строках после V: <name>
function voiceBars(abc, name) {
  const out = []
  let cur = null
  for (const line of abc.split('\n')) {
    const t = line.trim()
    if (t.startsWith('V:')) { cur = t.slice(2).trim().split(/\s+/)[0]; continue }
    if (t.startsWith('%')) continue
    if (cur === name && t.includes('|')) out.push(...t.split('|').map((c) => c.trim()).filter(Boolean))
  }
  return out
}

// токены такта (целые длины): нота/пауза + длительность
function tokens(bar) {
  const body = bar.replace(/"[^"]*"/g, '')
  const out = []
  const re = /(z|[A-Ga-g][',]*)(\d*)/g
  let m
  while ((m = re.exec(body))) out.push({ rest: m[1] === 'z', note: m[1], dur: m[2] ? Number(m[2]) : 1 })
  return out
}
const units = (bar) => tokens(bar).reduce((s, t) => s + t.dur, 0)
// начала нот (в единицах от начала такта) и их высоты
function onsets(bar) {
  const out = []
  let pos = 0
  for (const t of tokens(bar)) {
    if (!t.rest) out.push([pos, t.note])
    pos += t.dur
  }
  return out
}
const chords = (bar) => bar.match(/"[^"]*"/g) || []

const hold = (abc, targets) => applyTrick(abc, { kind: 'vocalHold', targets })
const vt = (...bars) => bars.map((bar) => ({ voice: 'Vocal', bar }))

describe('приём «протянуть концы фраз» (vocalHold), ТК128', () => {
  const SRC_BARS = [
    '"Em"E4z12',
    '"C"B4B2c4B4z2',
    'z2B2B2B2B2B2A2G2',
    '"Em"z4g2g2f4e4',
    '"Em"E4z12',
  ]
  const src = plan(SRC_BARS.join('|') + '|', 'E4z12|E4z12|E4z12|E4z12|E4z12|')
  const out = hold(src, vt(0, 1, 2, 3))
  const outBars = voiceBars(out, 'Vocal')

  it('нота перед паузой удлиняется на полдоли (2 единицы): "Em"E4z12 → "Em"E6z10', () => {
    expect(outBars[0]).toBe('"Em"E6z10')
  })

  it('пауза короче полдоли съедается целиком и исчезает: "C"B4B2c4B4z2 → "C"B4B2c4B6', () => {
    expect(outBars[1]).toBe('"C"B4B2c4B6')
  })

  it('нет паузы после нот — такт без изменений', () => {
    expect(outBars[2]).toBe('z2B2B2B2B2B2A2G2')
  })

  it('пауза только в начале такта — такт без изменений', () => {
    expect(outBars[3]).toBe('"Em"z4g2g2f4e4')
  })

  it('в каждом целевом такте сумма единиц, начала и высоты нот, аккорды — прежние', () => {
    for (const i of [0, 1, 2, 3]) {
      expect(units(outBars[i])).toBe(units(SRC_BARS[i]))
      expect(onsets(outBars[i])).toEqual(onsets(SRC_BARS[i]))
      expect(chords(outBars[i])).toEqual(chords(SRC_BARS[i]))
    }
  })

  it('число тактов голоса прежнее; такт вне targets — без изменений', () => {
    expect(outBars).toHaveLength(SRC_BARS.length)
    expect(outBars[4]).toBe('"Em"E4z12')
  })

  it('голос Ins не меняется — ни вне targets, ни как цель', () => {
    expect(voiceBars(out, 'Ins')).toEqual(voiceBars(src, 'Ins'))
    const withIns = hold(src, [{ voice: 'Ins', bar: 0 }, { voice: 'Vocal', bar: 0 }])
    expect(voiceBars(withIns, 'Ins')).toEqual(voiceBars(src, 'Ins'))
    expect(voiceBars(withIns, 'Vocal')[0]).toBe('"Em"E6z10')
  })

  it('такт с дробной длиной (B/2) — без изменений, даже если есть нота перед паузой', () => {
    const p = plan('"Em"B/2B/2B3z12|')
    expect(voiceBars(hold(p, vt(0)), 'Vocal')).toEqual(['"Em"B/2B/2B3z12'])
  })

  it('L:1/8: полдоли = 1 единица, E2z6 → E3z5', () => {
    const p = plan('E2z6|', 'E8|', '1/8')
    expect(voiceBars(hold(p, vt(0)), 'Vocal')).toEqual(['E3z5'])
  })

  it('пустой список целей — план без изменений', () => {
    expect(voiceBars(hold(src, []), 'Vocal')).toEqual(SRC_BARS)
  })
})

describe('vocalHold — для «перепеть с места» и выбора целей', () => {
  it('revoiceSpecKinds содержит vocalHold', () => {
    expect(revoiceSpecKinds.has('vocalHold')).toBe(true)
  })

  it('pickTargets: только вокальный голос, без вокала — null (как vocalVary)', () => {
    const bars = (n) => Array.from({ length: n }, (_, i) => ({ start_sec: i * 2, end_sec: i * 2 + 2 }))
    expect(pickTargets({ Vocal: bars(3), Ins: bars(3) }, 1, 2, 'vocalHold').targets)
      .toEqual([{ voice: 'Vocal', bar: 1 }, { voice: 'Vocal', bar: 2 }])
    expect(pickTargets({ Ins: bars(3) }, 0, 1, 'vocalHold')).toBeNull()
  })
})

describe('кнопка «голос: протянуть концы» в студии (условие 88)', () => {
  const vue = readFileSync(join(here, 'components', 'StudioPage.vue'), 'utf8')
  const ru = readFileSync(join(here, 'i18n', 'ru.js'), 'utf8')
  const en = readFileSync(join(here, 'i18n', 'en.js'), 'utf8')

  it('кнопка runTrick(\'vocalHold\') в той же группе «голос», что и vocalVary (видимость hasVocalSel)', () => {
    const start = vue.indexOf('<div v-if="hasVocalSel" class="trick-group">')
    expect(start).toBeGreaterThan(-1)
    const end = vue.indexOf('<div class="trick-group">', start + 1)
    const group = vue.slice(start, end > -1 ? end : undefined)
    expect(group).toContain("runTrick('vocalVary')")
    expect(group).toContain("runTrick('vocalHold')")
    expect(group).toContain("t('studio.trick.vocalHold.tip')")
  })

  it('подписи и подсказки ru/en', () => {
    expect(ru).toMatch(/'studio\.trick\.vocalHold':\s*'голос: протянуть концы'/)
    expect(ru).toContain("'studio.trick.vocalHold.tip'")
    expect(en).toContain("'studio.trick.vocalHold'")
    expect(en).toContain("'studio.trick.vocalHold.tip'")
  })
})
