// Тесты подписей миксов вклеек: файл overdub-inst-<N>.flac → понятная подпись.
import { describe, it, expect } from 'vitest'
import { mixLabel, mixChildId, instIdFromTitle } from './insertLabels.js'
import { insertTitle, insertWindow } from './insertLabels.js'
import { TRICK_INSTRUMENTS } from './abcEdit.js'

// подпись инструмента и формат времени — от вызывающего (i18n и m:ss)
const labelOf = id => `L(${id})`
const fmt = s => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}`
const inst1 = TRICK_INSTRUMENTS[0].id
const inst2 = TRICK_INSTRUMENTS[1].id

describe('mixChildId', () => {
  it('номер рендера из имени файла микса, прочие файлы → null', () => {
    expect(mixChildId('overdub-inst-191.flac')).toBe(191)
    expect(mixChildId('out.wav')).toBeNull()
    expect(mixChildId('overdub-inst-abc.flac')).toBeNull()
    expect(mixChildId('')).toBeNull()
    expect(mixChildId(undefined)).toBeNull()
  })
})

describe('instIdFromTitle', () => {
  it('хвост названия после « · » — id или подпись инструмента', () => {
    expect(instIdFromTitle(`Песня · ${inst1}`, labelOf)).toBe(inst1)
    expect(instIdFromTitle(`Песня · ${labelOf(inst2)}`, labelOf)).toBe(inst2)
  })

  it('без хвоста или с неизвестным хвостом → null', () => {
    expect(instIdFromTitle('Песня', labelOf)).toBeNull()
    expect(instIdFromTitle('Песня · нечто', labelOf)).toBeNull()
    expect(instIdFromTitle(null, labelOf)).toBeNull()
  })
})

describe('mixLabel', () => {
  const applied = [
    { childId: 10, instId: inst1, from: 204, to: 208 },
    { childId: 11, instId: inst2, from: 240, to: 256, alts: [12] },
  ]

  it('файл не микс вклейки → null', () => {
    expect(mixLabel('out.wav', { applied, jobs: [], labelOf, fmt })).toBeNull()
  })

  it('микс из реестра: все вклейки родителя «инструмент окно» через « + »', () => {
    expect(mixLabel('overdub-inst-11.flac', { applied, jobs: [], labelOf, fmt }))
      .toBe(`L(${inst1}) 3:24–3:28 + L(${inst2}) 4:00–4:16`)
  })

  it('файл по альтернативному варианту вклейки — тоже из реестра', () => {
    expect(mixLabel('overdub-inst-12.flac', { applied, jobs: [], labelOf, fmt }))
      .toBe(`L(${inst1}) 3:24–3:28 + L(${inst2}) 4:00–4:16`)
  })

  it('старый файл вне реестра — подпись инструмента из названия рендера', () => {
    const jobs = [{ id: 191, title: `Песня · ${inst1}` }]
    expect(mixLabel('overdub-inst-191.flac', { applied: [], jobs, labelOf, fmt })).toBe(`L(${inst1})`)
  })

  it('вне реестра и без узнаваемого рендера (удалён / чужое название) → null', () => {
    expect(mixLabel('overdub-inst-191.flac', { applied: [], jobs: [], labelOf, fmt })).toBeNull()
    expect(mixLabel('overdub-inst-191.flac', { applied: [], jobs: [{ id: 191, title: 'Песня' }], labelOf, fmt })).toBeNull()
  })

  it('без applied/jobs в опциях — не падает', () => {
    expect(mixLabel('overdub-inst-5.flac', { labelOf, fmt })).toBeNull()
  })
})

describe('insertTitle / insertWindow — строка эффекта на дорожку в списке', () => {
  const names = { chainName: (c) => ({ soften: 'Смягчить звон' })[c] || c, stemName: (s) => ({ vocals: 'голос' })[s] || s,
    instName: (id) => 'приём ' + id }
  const w = { fmt: (s) => `${Math.floor(s / 60)}:${String(Math.round(s % 60)).padStart(2, '0')}`, toEnd: 'конец', whole: 'весь трек' }
  it('эффект на дорожку — «эффект · дорожка», вклейка — название приёма', () => {
    expect(insertTitle({ chain: 'soften', stems: ['vocals'] }, names)).toBe('Смягчить звон · голос')
    expect(insertTitle({ instId: 'drumsup' }, names)).toBe('приём drumsup')
  })
  it('окно: обычное, до конца, весь трек', () => {
    expect(insertWindow({ from: 176, to: 190 }, w)).toBe('2:56–3:10')
    expect(insertWindow({ from: 176, to: 0 }, w)).toBe('2:56–конец')
    expect(insertWindow({ from: 0, to: 0 }, w)).toBe('весь трек')
  })
})
