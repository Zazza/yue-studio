// Тесты карточки internal-own-track, этап 14 «Оркестр»: условия 113–114 (тест-кейс ТК143).
//   113 — готовые инструменты оркестра в fxPresets: группы orch-strings / orch-winds / orch-brass / orch-mallets
//     (партии по аккордам: stems ['synth'], блок synth с kit VSCO, стиль pad|arp|pulse|drone, октава),
//     orch-bass (stems ['bass'], блок bass с kit — виолончель, альт, контрабас, фагот, туба),
//     orch-lead (stems ['guitar','other','vocals'], блок bass с fmin_hz 80 / fmax_hz 1500 — скрипка, флейта,
//     гобой, кларнет, труба); без имён групп и исполнителей. Наборы частей — из условия 110.
//   114 — familyOf: stems из guitar/other/piano/vocals (хотя бы одна не vocals) → guitar; только vocals → null.
// Пресеты опознаются по полю group, а не по id. Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import { fxPresets } from './fxPresets.js'
import { PRESET_GROUPS, groupPresets } from './presetGroups.js'
import { familyOf } from './phraseLoop.js'

// условие 110: наборы VSCO и их части
const WIND_PARTS = ['sus', 'stac']
const KIT_PARTS = {
  'vsco-violin': ['solo', 'ens', 'pizz', 'spic'],
  'vsco-viola': ['ens', 'pizz', 'spic'],
  'vsco-cello': ['ens', 'pizz', 'spic'],
  'vsco-contrabass': ['sus', 'pizz', 'spic'],
  'vsco-harp': ['harp'],
  'vsco-flute': WIND_PARTS,
  'vsco-oboe': WIND_PARTS,
  'vsco-clarinet': WIND_PARTS,
  'vsco-bassoon': WIND_PARTS,
  'vsco-trumpet': WIND_PARTS,
  'vsco-horn': WIND_PARTS,
  'vsco-trombone': WIND_PARTS,
  'vsco-tuba': WIND_PARTS,
  'vsco-mallets': ['glock', 'marimba', 'xylo'],
}

// какие инструменты уместны в группе: для аккордовых групп — по смыслу названия группы («Струнные», «Медь» …);
// для orch-bass / orch-lead — перечень из условия 113 (проверяется, что он покрыт; лишние инструменты допустимы:
// условие перечисляет, но не ограничивает — см. отчёт теста)
const GROUP_KITS = {
  'orch-strings': ['vsco-violin', 'vsco-viola', 'vsco-cello', 'vsco-contrabass', 'vsco-harp'],
  'orch-winds': ['vsco-flute', 'vsco-oboe', 'vsco-clarinet', 'vsco-bassoon'],
  'orch-brass': ['vsco-trumpet', 'vsco-horn', 'vsco-trombone', 'vsco-tuba'],
  'orch-mallets': ['vsco-mallets'],
  'orch-bass': ['vsco-cello', 'vsco-viola', 'vsco-contrabass', 'vsco-bassoon', 'vsco-tuba'],
  'orch-lead': ['vsco-violin', 'vsco-flute', 'vsco-oboe', 'vsco-clarinet', 'vsco-trumpet'],
}
const CHORD_GROUPS = ['orch-strings', 'orch-winds', 'orch-brass', 'orch-mallets']
const ORCH_GROUPS = [...CHORD_GROUPS, 'orch-bass', 'orch-lead']
const STYLES = ['pad', 'arp', 'pulse', 'drone']

const inGroup = (g) => fxPresets.filter((p) => p.group === g)
const orch = fxPresets.filter((p) => typeof p.group === 'string' && p.group.startsWith('orch-'))
const kitBlocks = (p) => p.chain.filter((b) => (b.type === 'synth' || b.type === 'bass') && typeof b.kit === 'string')
const instrumentOf = (kit) => kit.split('/')[0]

// Латинское слово с заглавной вне начала предложения — подозрение на имя исполнителя/группы/бренда.
// Разрешены: источник сэмплов (VSCO-2 Community Edition), лицензия CC0, названия нот (A, C#4, Bb2 …).
// «Christmas…» — праздник, не имя (ложное срабатывание на «Christmassy» в заметке колокольчиков).
const ALLOWED_CAPS = new Set(['VSCO', 'VSCO-2', 'Community', 'Edition', 'CC0', 'Christmas', 'Christmassy'])
const NOTE_RE = /^[A-G](#|b)?-?\d?$/
function suspiciousNames(text) {
  const cyr = /[а-яё]/i.test(text)
  const found = []
  for (const m of text.matchAll(/[A-Z][A-Za-z0-9#-]*/g)) {
    const word = m[0]
    if (m.index > 0 && /[A-Za-z0-9]/.test(text[m.index - 1])) continue // не начало слова
    if (ALLOWED_CAPS.has(word) || NOTE_RE.test(word)) continue
    const before = text.slice(0, m.index).trimEnd()
    const sentenceStart = before === '' || /[.!?:;(«"—→]$/.test(before)
    // в английском тексте заглавная в начале предложения — норма; в русском латиница с заглавной подозрительна всегда
    if (sentenceStart && !cyr) continue
    found.push(word)
  }
  return found
}

describe('условие 113: группы оркестра (ТК143)', () => {
  it.each(ORCH_GROUPS)('у группы %s есть подписи ru и en в PRESET_GROUPS', (g) => {
    expect(PRESET_GROUPS[g]).toBeDefined()
    expect(PRESET_GROUPS[g].ru?.trim()).toBeTruthy()
    expect(PRESET_GROUPS[g].en?.trim()).toBeTruthy()
  })

  it.each(ORCH_GROUPS)('в группе %s не меньше трёх пресетов', (g) => {
    expect(inGroup(g).length).toBeGreaterThanOrEqual(3)
  })

  it('groupPresets выводит все группы оркестра с подписью из PRESET_GROUPS', () => {
    const out = JSON.stringify(groupPresets(fxPresets, 'ru'))
    for (const g of ORCH_GROUPS) expect(out).toContain(PRESET_GROUPS[g].ru)
  })

  it('других групп orch-* нет (все — из перечня условия)', () => {
    expect([...new Set(orch.map((p) => p.group))].sort()).toEqual([...ORCH_GROUPS].sort())
  })
})

describe.each(ORCH_GROUPS)('условие 113: пресеты группы %s (ТК143)', (g) => {
  const list = inGroup(g)

  it.each(list.map((p) => [p.id, p]))('%s: блок synth/bass с kit vsco-<инструмент>/<часть> из условия 110', (_id, p) => {
    const kb = kitBlocks(p)
    expect(kb.length).toBeGreaterThanOrEqual(1)
    for (const b of kb) {
      expect(b.kit).toMatch(/^vsco-[a-z]+\/[a-z]+$/)
      const [inst, part] = b.kit.split('/')
      expect(Object.keys(KIT_PARTS)).toContain(inst)
      expect(KIT_PARTS[inst]).toContain(part)
      if (CHORD_GROUPS.includes(g)) expect(GROUP_KITS[g]).toContain(inst)
    }
  })

  it.each(list.map((p) => [p.id, p]))('%s: имена ru/en непустые', (_id, p) => {
    expect(p.name?.ru?.trim()).toBeTruthy()
    expect(p.name?.en?.trim()).toBeTruthy()
  })

  it.each(list.map((p) => [p.id, p]))('%s: в названии и заметке нет имён исполнителей/групп', (_id, p) => {
    const texts = [p.name?.ru, p.name?.en, p.note?.ru, p.note?.en].filter(Boolean)
    for (const t of texts) expect(suspiciousNames(t)).toEqual([])
  })
})

describe.each(CHORD_GROUPS)('условие 113: партии по аккордам — %s (ТК143)', (g) => {
  it.each(inGroup(g).map((p) => [p.id, p]))('%s: stems [synth], блок synth с kit, стиль и октава', (_id, p) => {
    expect(p.stems).toEqual(['synth'])
    expect(p.chain.some((b) => b.type === 'synth' && /^vsco-/.test(b.kit || ''))).toBe(true)
    expect(STYLES).toContain(p.style)
    expect(Number.isInteger(p.octave)).toBe(true)
    expect(p.octave).toBeGreaterThanOrEqual(-2)
    expect(p.octave).toBeLessThanOrEqual(2)
  })
})

describe('условие 113: Бас → оркестр (orch-bass, ТК143)', () => {
  it.each(inGroup('orch-bass').map((p) => [p.id, p]))('%s: stems [bass], блок bass с kit vsco-*', (_id, p) => {
    expect(p.stems).toEqual(['bass'])
    expect(p.chain.some((b) => b.type === 'bass' && /^vsco-/.test(b.kit || ''))).toBe(true)
  })
})

describe('условие 113: Мелодия → оркестр (orch-lead, ТК143)', () => {
  it.each(inGroup('orch-lead').map((p) => [p.id, p]))('%s: stems guitar/other/vocals, блок bass с fmin_hz 80 / fmax_hz 1500', (_id, p) => {
    expect([...p.stems].sort()).toEqual(['guitar', 'other', 'vocals'])
    const b = p.chain.find((x) => x.type === 'bass' && /^vsco-/.test(x.kit || ''))
    expect(b).toBeDefined()
    // диапазоны условия 112: fmin 20…1000, fmax 100…2000, fmin < fmax
    expect(b.fmin_hz).toBeGreaterThanOrEqual(20)
    expect(b.fmin_hz).toBeLessThanOrEqual(1000)
    expect(b.fmax_hz).toBeGreaterThanOrEqual(100)
    expect(b.fmax_hz).toBeLessThanOrEqual(2000)
    expect(b.fmin_hz).toBeLessThan(b.fmax_hz)
    // значения, названные в условии 113
    expect(b.fmin_hz).toBe(80)
    expect(b.fmax_hz).toBe(1500)
  })
})

describe('условие 113: перечни инструментов из карточки покрыты', () => {
  it.each([
    ['orch-bass', GROUP_KITS['orch-bass']],
    ['orch-lead', GROUP_KITS['orch-lead']],
  ])('в %s есть пресет на каждый инструмент из условия', (g, kits) => {
    const used = new Set(inGroup(g).flatMap((p) => kitBlocks(p).map((b) => instrumentOf(b.kit))))
    for (const k of kits) expect(used).toContain(k)
  })
})

describe('проверка «нет имён» сама различает (самоконтроль фильтра)', () => {
  it('ловит латинское имя в русском тексте и посреди английского предложения', () => {
    expect(suspiciousNames('Струнные как у Metallica')).toEqual(['Metallica'])
    expect(suspiciousNames('Strings like Metallica live')).toEqual(['Metallica'])
  })
  it('не ловит VSCO/CC0, ноты и заглавную в начале английского предложения', () => {
    expect(suspiciousNames('Сэмплы VSCO-2 Community Edition, CC0; нота A4 и C#3.')).toEqual([])
    expect(suspiciousNames('Violins: chords. Samples VSCO-2 Community Edition, CC0.')).toEqual([])
  })
})

describe('условие 114: familyOf для мелодии-замены (ТК143)', () => {
  it.each([
    [['guitar', 'other', 'vocals'], 'guitar'],
    [['other', 'vocals'], 'guitar'],
    [['piano', 'vocals'], 'guitar'],
    [['vocals'], null],
  ])('stems %j → %s', (stems, fam) => {
    expect(familyOf({ id: 'x', stems, chain: [] })).toBe(fam)
  })

  it('пресеты orch-lead попадают в семью guitar', () => {
    for (const p of inGroup('orch-lead')) expect(familyOf(p)).toBe('guitar')
  })
})
