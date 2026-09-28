// Тесты примерочной голосов: ручки → дескриптор, стенд прослушивания.
import { describe, it, expect } from 'vitest'
import {
  PARAMS_VERSION, VOICE_KNOBS, AUDITION_BED, VOICE_PRESETS, VOICE_ARCHETYPES,
  defaultVoiceParams, normalizeVoiceParams, voiceDescriptor,
  auditionStyle, auditionJob, needsTranslate,
} from './voiceLab.js'

describe('voiceDescriptor', () => {
  it('нейтральные ручки дают минимальную непустую строку', () => {
    expect(voiceDescriptor(defaultVoiceParams()))
      .toBe('female vocals, even delivery')
  })

  it('каждый уровень каждой ручки добавляет свой токен', () => {
    const roughs = ['female vocals, raspy, even delivery',
      'female vocals, gritty raspy, even delivery',
      'female vocals, gravelly growled, even delivery']
    for (let r = 1; r <= 3; r++) {
      expect(voiceDescriptor({ ...defaultVoiceParams(), rough: r })).toBe(roughs[r - 1])
    }
    expect(voiceDescriptor({ ...defaultVoiceParams(), creak: 1 }))
      .toBe('female vocals, creaky, even delivery')
    expect(voiceDescriptor({ ...defaultVoiceParams(), creak: 2 }))
      .toBe('female vocals, vocal fry, even delivery')
    expect(voiceDescriptor({ ...defaultVoiceParams(), delivery: 0 }))
      .toBe('female vocals, soft intimate delivery')
    expect(voiceDescriptor({ ...defaultVoiceParams(), delivery: 2 }))
      .toBe('female vocals, powerful delivery')
    expect(voiceDescriptor({ ...defaultVoiceParams(), delivery: 3 }))
      .toBe('female vocals, raw breaking delivery, on the verge of tears')
    expect(voiceDescriptor({ ...defaultVoiceParams(), breath: 1 }))
      .toBe('female vocals, breathy, even delivery')
    expect(voiceDescriptor({ ...defaultVoiceParams(), breath: 2 }))
      .toBe('female vocals, whispered, even delivery')
  })

  it('все регистры дают свой токен', () => {
    expect(voiceDescriptor({ ...defaultVoiceParams(), register: 'male' }))
      .toBe('male vocals, even delivery')
    expect(voiceDescriptor({ ...defaultVoiceParams(), register: 'male-high' }))
      .toBe('high tenor male vocals, even delivery')
    expect(voiceDescriptor({ ...defaultVoiceParams(), register: 'male-low' }))
      .toBe('deep resonant low male vocals, even delivery')
  })

  it('порядок фиксирован: регистр, скрип, грубость, дыхание, подача', () => {
    const d = voiceDescriptor({ register: 'male-low', rough: 2, creak: 2, delivery: 2, breath: 1, extra: '' })
    expect(d).toBe('deep resonant low male vocals, vocal fry, gritty raspy, breathy, powerful delivery')
  })

  it('экстремум: шёпот побеждает подачу «на разрыв»', () => {
    const d = voiceDescriptor({ ...defaultVoiceParams(), breath: 2, delivery: 3 })
    expect(d).toContain('hushed whispered delivery')
    expect(d).not.toContain('breaking')
    expect(d).not.toContain('powerful')
  })

  it('дыхание 1 + подача 3 — законная пара, оба токена остаются', () => {
    const d = voiceDescriptor({ ...defaultVoiceParams(), breath: 1, delivery: 3 })
    expect(d).toContain('breathy')
    expect(d).toContain('breaking')
  })

  it('extra добавляется последним, обрезанным; пустой — не оставляет хвоста', () => {
    expect(voiceDescriptor({ ...defaultVoiceParams(), extra: '  southern twang  ' }))
      .toBe('female vocals, even delivery, southern twang')
    expect(voiceDescriptor({ ...defaultVoiceParams(), extra: '   ' }).endsWith(',')).toBe(false)
  })

  it('детерминирован: одинаковые params → одинаковая строка', () => {
    const p = { register: 'male', rough: 3, creak: 1, delivery: 3, breath: 0, extra: 'x' }
    expect(voiceDescriptor(p)).toBe(voiceDescriptor(p))
  })
})

describe('normalizeVoiceParams', () => {
  it('клампит выходящие за пределы уровни', () => {
    const n = normalizeVoiceParams({ register: 'male', rough: 9, creak: -1, delivery: 99, breath: -3 })
    expect(n).toEqual({ register: 'male', rough: 3, creak: 0, delivery: 3, breath: 0, extra: '' })
  })

  it('неизвестный регистр и мусорные поля заменяются дефолтом', () => {
    const n = normalizeVoiceParams({ register: 'castrato', rough: 'нет' })
    expect(n.register).toBe('female')
    expect(n.rough).toBe(0)
  })

  it('валидный объект проходит без изменений', () => {
    const p = { register: 'male-high', rough: 1, creak: 1, delivery: 2, breath: 1, extra: 'twang' }
    expect(normalizeVoiceParams(p)).toEqual(p)
  })

  it('карточка с версией параметров нормализуется тем же путём', () => {
    const n = normalizeVoiceParams({ v: PARAMS_VERSION, register: 'male-low', rough: 2 })
    expect(n.register).toBe('male-low')
    expect(n.rough).toBe(2)
    expect(n.creak).toBe(0)
  })
})

describe('стенд прослушивания', () => {
  it('стиль: слоты стенда в порядке SLOT_ORDER, дескриптор в слоте vocals, BPM последним', () => {
    const line = auditionStyle('raspy male vocals')
    expect(line.indexOf('English')).toBe(0)
    for (const slot of ['energetic pop rock', 'driving mid-tempo beat', 'raspy male vocals', 'vocals up front']) {
      expect(line).toContain(slot)
    }
    expect(line.endsWith('110 BPM')).toBe(true)
    expect(line.indexOf('energetic pop rock')).toBeLessThan(line.indexOf('raspy male vocals'))
  })

  it('джоба: draft-превью с конкретным seed и фиксированным планом, без arc', () => {
    const job = auditionJob({ register: 'male', rough: 2 }, 1234567890123)
    expect(job.draft).toBe(true)
    expect(job.cot).toBe('full')
    expect(job.seed).toBe(1234567890123)
    expect(job.lyrics).toBe(AUDITION_BED.lyrics)
    expect(job.style).toContain('gritty raspy')
    expect(job.abc).toBe(AUDITION_BED.abc)
    expect('arc' in job).toBe(false)
  })

  it('лирика стенда: припев первым (без куплета) — вокал вступает раньше интро', () => {
    expect(AUDITION_BED.lyrics.startsWith('[Chorus]')).toBe(true)
    expect(AUDITION_BED.lyrics).not.toContain('[Verse]')
    expect(AUDITION_BED.lyrics.split('\n').filter(Boolean).length).toBeLessThanOrEqual(5)
  })

  it('план стенда: без интро, припев с первой доли, вокал поёт а не отдыхает', () => {
    const abc = AUDITION_BED.abc
    expect(abc.startsWith('X:1')).toBe(true)
    expect(abc).toContain('% chorus')
    expect(abc).not.toContain('% intro')
    // в первой вокальной строке после четвертной паузы сразу ноты (не z16)
    const firstVocal = abc.split('\n')[10]
    expect(firstVocal).toMatch(/^"Dm"z4[a-g]/)
  })
})

describe('needsTranslate', () => {
  it('кириллица требует перевода, английские теги — нет', () => {
    expect(needsTranslate('хриплый голос')).toBe(true)
    expect(needsTranslate('raspy vocals')).toBe(false)
    expect(needsTranslate('')).toBe(false)
  })
})

describe('инварианты данных', () => {
  it('у каждой ручки задан максимум больше нуля', () => {
    for (const k of VOICE_KNOBS) expect(k.max).toBeGreaterThan(0)
  })
})

describe('быстрые пресеты', () => {
  it('id уникальны, все пресеты нормализуются без потерь', () => {
    expect(new Set(VOICE_PRESETS.map(p => p.id)).size).toBe(VOICE_PRESETS.length)
    for (const p of VOICE_PRESETS) {
      const n = normalizeVoiceParams(p.params)
      expect(n, p.id).toEqual({ ...defaultVoiceParams(), ...p.params, extra: (p.params.extra || '').trim() })
    }
  })

  it('пресеты несут ожидаемые теги в дескрипторе', () => {
    const byId = Object.fromEntries(VOICE_PRESETS.map(p => [p.id, voiceDescriptor(p.params)]))
    expect(byId.operatic).toContain('operatic soprano')
    expect(byId.operatic).toContain('powerful delivery')
    expect(byId['dark-drama']).toContain('dramatic dark')
    expect(byId.scream).toContain('screamed chorus')
    expect(byId.scream).toContain('gritty raspy')
    expect(byId.whisper).toContain('whispered')
    expect(byId['velvet-low']).toContain('deep resonant low male vocals')
    expect(byId.reset).toBe('female vocals, even delivery')
  })
})

describe('архотипы «в духе»', () => {
  it('id уникальны и не пересекаются с пресетами, все нормализуются без потерь', () => {
    const ids = [...VOICE_PRESETS.map(p => p.id), ...VOICE_ARCHETYPES.map(a => a.id)]
    expect(new Set(ids).size).toBe(ids.length)
    for (const a of VOICE_ARCHETYPES) {
      expect(normalizeVoiceParams(a.params), a.id)
        .toEqual({ ...defaultVoiceParams(), ...a.params })
    }
  })

  it('имена артистов не попадают в дескриптор — только обезличенные клише-теги', () => {
    const banned = ['letov', 'yorke', 'white', 'placebo', 'interpol', 'zemfira',
      'mumiy', 'cobain', 'cave', 'winehouse', 'bjork', 'reed', 'vysocky', 'greb']
    for (const a of VOICE_ARCHETYPES) {
      const d = voiceDescriptor(a.params).toLowerCase()
      for (const name of banned) expect(d, `${a.id}/${name}`).not.toContain(name)
    }
  })

  it('дескриптор любого пресета/архотипа — английский (перевод не нужен)', () => {
    for (const p of [...VOICE_PRESETS, ...VOICE_ARCHETYPES]) {
      expect(needsTranslate(voiceDescriptor(p.params)), p.id).toBe(false)
    }
  })

  it('ключевые теги архотипов на месте', () => {
    const byId = Object.fromEntries(VOICE_ARCHETYPES.map(a => [a.id, voiceDescriptor(a.params)]))
    expect(byId.letov).toContain('shouted')
    expect(byId.letov).toContain('off-key')
    expect(byId['thom-yorke']).toContain('nasal')
    expect(byId['thom-yorke']).toContain('falsetto')
    expect(byId['jack-white']).toContain('whiny wail')
    expect(byId.placebo).toContain('androgynous')
    expect(byId.interpol).toContain('deadpan baritone')
    expect(byId.zemfira).toContain('husky')
    expect(byId['mumiy-troll']).toContain('purring')
    expect(byId.cobain).toContain('grunge whine')
    expect(byId['nick-cave']).toContain('deep baritone')
    expect(byId['amy-winehouse']).toContain('smoky soul')
    expect(byId.bjork).toContain('child-like')
    expect(byId['lou-reed']).toContain('talk-singing')
    expect(byId.vysocky).toContain('rasping')
    expect(byId.bg).toContain('detached')
  })
})
