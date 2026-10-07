// Тесты карточки internal-instruments-page (страница «Инструменты»), тест-кейсы 1–2:
// fxChain.js — чистая логика редактируемой цепочки движка без DOM.
// Редактируемая цепочка — массив {type, on, params}; у eq в params ещё bands.
// Описание блоков — fxBlocks.json (копия worker/fx_blocks.json): умолчания, границы,
// zero_off, строки (model/ir), bands. Все функции возвращают новый массив, вход не меняют.
// Написаны по карточке и контракту, без чтения реализации.
import { describe, it, expect } from 'vitest'
import blocks from './fxBlocks.json'
import { fxPresets } from './fxPresets.js'
import {
  newBlock, addBlock, removeBlock, moveBlock, toggleBlock, setParam,
  toWorkerChain, fromWorkerChain, missingKits,
} from './fxChain.js'

const TYPES = ['gate', 'eq', 'comp', 'drive', 'amp', 'cab', 'reverb', 'delay', 'gain', 'sampler', 'bass']

// deepFreeze — любая мутация входа в строгом режиме модуля бросит исключение.
function deepFreeze(o) {
  if (o && typeof o === 'object' && !Object.isFrozen(o)) {
    Object.freeze(o)
    for (const v of Object.values(o)) deepFreeze(v)
  }
  return o
}

const param = (type, id) => blocks[type].params.find((p) => p.id === id)

// умолчания блока по описанию: числа — default, строки — default или ""
function defaults(type) {
  const out = {}
  for (const p of blocks[type].params) out[p.id] = p.default
  for (const s of blocks[type].strings || []) out[s.id] = s.default ?? ''
  return out
}

// цепочка из трёх блоков: gate, eq, reverb
const chain3 = () => deepFreeze([
  { type: 'gate', on: true, params: { ...defaults('gate') } },
  { type: 'eq', on: true, params: { ...defaults('eq'), bands: [] } },
  { type: 'reverb', on: true, params: { ...defaults('reverb') } },
])
const types = (c) => c.map((b) => b.type)

describe('описание блоков (fxBlocks.json)', () => {
  it('одиннадцать типов в порядке показа (sampler — после gain, bass — последним)', () => {
    expect(Object.keys(blocks)).toEqual(TYPES)
  })
})

describe('newBlock / addBlock: умолчания из описания', () => {
  it.each(TYPES)('%s: включён, все параметры с умолчаниями', (type) => {
    const b = newBlock(type, blocks)
    expect(b.type).toBe(type)
    expect(b.on).toBe(true)
    const want = defaults(type)
    for (const [k, v] of Object.entries(want)) expect(b.params[k]).toBe(v)
  })

  it('eq — пустой список полос', () => {
    expect(newBlock('eq', blocks).params.bands).toEqual([])
  })

  it('строка без умолчания (amp.model) — пустая строка; cab/reverb.ir — «встроенный» ""', () => {
    expect(newBlock('amp', blocks).params.model).toBe('')
    expect(newBlock('cab', blocks).params.ir).toBe('')
    expect(newBlock('reverb', blocks).params.ir).toBe('')
  })

  it('неизвестный тип → ошибка', () => {
    expect(() => newBlock('fuzz', blocks)).toThrow()
    expect(() => addBlock([], 'fuzz', blocks)).toThrow()
  })

  it('addBlock — в конец, вход не меняется', () => {
    const c = chain3()
    const out = addBlock(c, 'delay', blocks)
    expect(types(out)).toEqual(['gate', 'eq', 'reverb', 'delay'])
    expect(out[3]).toEqual(newBlock('delay', blocks))
    expect(types(c)).toEqual(['gate', 'eq', 'reverb'])
    expect(out).not.toBe(c)
  })

  it('addBlock в пустую цепочку', () => {
    expect(types(addBlock([], 'comp', blocks))).toEqual(['comp'])
  })

  it('два блока одного типа — независимые копии параметров', () => {
    const c = addBlock(addBlock([], 'drive', blocks), 'drive', blocks)
    const d = setParam(c, 0, 'gain_db', param('drive', 'gain_db').max, blocks)
    expect(d[1].params.gain_db).toBe(param('drive', 'gain_db').default)
  })
})

describe('removeBlock', () => {
  it('убирает по индексу, вход не меняется', () => {
    const c = chain3()
    expect(types(removeBlock(c, 1))).toEqual(['gate', 'reverb'])
    expect(types(removeBlock(c, 0))).toEqual(['eq', 'reverb'])
    expect(types(removeBlock(c, 2))).toEqual(['gate', 'eq'])
    expect(types(c)).toEqual(['gate', 'eq', 'reverb'])
  })

  it('последний блок → пустая цепочка', () => {
    expect(removeBlock(deepFreeze([newBlock('gate', blocks)]), 0)).toEqual([])
  })
})

describe('moveBlock: вверх/вниз, на краях без изменений', () => {
  it('вверх', () => {
    expect(types(moveBlock(chain3(), 2, -1))).toEqual(['gate', 'reverb', 'eq'])
  })

  it('вниз', () => {
    expect(types(moveBlock(chain3(), 0, 1))).toEqual(['eq', 'gate', 'reverb'])
  })

  it('первый вверх — без изменений', () => {
    const c = chain3()
    expect(moveBlock(c, 0, -1)).toEqual(c)
  })

  it('последний вниз — без изменений', () => {
    const c = chain3()
    expect(moveBlock(c, 2, 1)).toEqual(c)
  })

  it('параметры едут вместе с блоком', () => {
    const c = deepFreeze(setParam(chain3(), 0, 'threshold_db', -60, blocks))
    const m = moveBlock(c, 0, 1)
    expect(m[1].type).toBe('gate')
    expect(m[1].params.threshold_db).toBe(-60)
  })
})

describe('toggleBlock: выключить без удаления', () => {
  it('выкл → вкл, параметры и порядок на месте', () => {
    const c = chain3()
    const off = toggleBlock(c, 1)
    expect(off[1].on).toBe(false)
    expect(off[1].params).toEqual(c[1].params)
    expect(types(off)).toEqual(types(c))
    expect(c[1].on).toBe(true)
    expect(toggleBlock(deepFreeze(off), 1)[1].on).toBe(true)
  })
})

describe('setParam: значение в пределах описания', () => {
  it('в диапазоне — как есть', () => {
    const p = param('gate', 'threshold_db')
    const mid = (p.min + p.max) / 2
    expect(setParam(chain3(), 0, 'threshold_db', mid, blocks)[0].params.threshold_db).toBe(mid)
  })

  it('выше max → max, ниже min → min', () => {
    const p = param('gate', 'threshold_db')
    expect(setParam(chain3(), 0, 'threshold_db', p.max + 100, blocks)[0].params.threshold_db).toBe(p.max)
    expect(setParam(chain3(), 0, 'threshold_db', p.min - 100, blocks)[0].params.threshold_db).toBe(p.min)
  })

  it('вход не меняется', () => {
    const c = chain3()
    const before = c[0].params.threshold_db
    setParam(c, 0, 'threshold_db', param('gate', 'threshold_db').max, blocks)
    expect(c[0].params.threshold_db).toBe(before)
  })

  it('каждый числовой параметр каждого блока прижимается к своим границам', () => {
    for (const type of TYPES) {
      const c = deepFreeze([newBlock(type, blocks)])
      for (const p of blocks[type].params) {
        expect(setParam(c, 0, p.id, p.max + 1e6, blocks)[0].params[p.id], `${type}.${p.id} max`).toBe(p.max)
        // отрицательное при zero_off контракт не описывает (0 или min) — не проверяем
        if (p.zero_off) continue
        const low = setParam(c, 0, p.id, p.min - 1e6, blocks)[0].params[p.id]
        expect(low, `${type}.${p.id} min`).toBe(p.min)
      }
    }
  })

  it('zero_off: 0 остаётся 0 (выкл), 0 < v < min → min', () => {
    const zeroOff = []
    for (const type of TYPES) {
      for (const p of blocks[type].params) if (p.zero_off) zeroOff.push([type, p])
    }
    // по контракту zero_off есть как минимум у eq.lowpass_hz
    expect(zeroOff.map(([t, p]) => `${t}.${p.id}`)).toContain('eq.lowpass_hz')
    for (const [type, p] of zeroOff) {
      const c = deepFreeze([newBlock(type, blocks)])
      expect(setParam(c, 0, p.id, 0, blocks)[0].params[p.id], `${type}.${p.id}=0`).toBe(0)
      if (p.min > 0) {
        expect(setParam(c, 0, p.id, p.min / 2, blocks)[0].params[p.id], `${type}.${p.id} мало`).toBe(p.min)
      }
      expect(setParam(c, 0, p.id, p.max * 2, blocks)[0].params[p.id]).toBe(p.max)
    }
  })

  it('строка ставится как есть', () => {
    const c = deepFreeze([newBlock('amp', blocks), newBlock('cab', blocks)])
    expect(setParam(c, 0, 'model', 'Plexi Lead.nam', blocks)[0].params.model).toBe('Plexi Lead.nam')
    expect(setParam(c, 1, 'ir', 'room.wav', blocks)[1].params.ir).toBe('room.wav')
    expect(setParam(c, 1, 'ir', '', blocks)[1].params.ir).toBe('')
  })
})

describe('toWorkerChain: JSON для воркера', () => {
  it('формат {type, ...params}, без поля on', () => {
    const c = deepFreeze([newBlock('gate', blocks)])
    expect(toWorkerChain(c)).toEqual([{ type: 'gate', ...defaults('gate') }])
  })

  it('выключенный блок не уходит', () => {
    const c = deepFreeze(toggleBlock(chain3(), 1))
    expect(toWorkerChain(c).map((b) => b.type)).toEqual(['gate', 'reverb'])
  })

  it('все выключены / пусто → пустой список', () => {
    expect(toWorkerChain([])).toEqual([])
    expect(toWorkerChain(deepFreeze(toggleBlock([newBlock('gate', blocks)], 0)))).toEqual([])
  })

  it('порядок блоков сохраняется', () => {
    const c = deepFreeze(moveBlock(chain3(), 2, -1))
    expect(toWorkerChain(c).map((b) => b.type)).toEqual(['gate', 'reverb', 'eq'])
  })

  it('у eq полосы — в bands', () => {
    const bands = [{ freq_hz: 3000, gain_db: 2.5, q: 1 }]
    const c = deepFreeze([{ type: 'eq', on: true, params: { ...defaults('eq'), bands } }])
    expect(toWorkerChain(c)).toEqual([{ type: 'eq', ...defaults('eq'), bands }])
  })

  it('отредактированные значения и строки уходят', () => {
    let c = [newBlock('amp', blocks), newBlock('cab', blocks)]
    c = setParam(c, 0, 'model', 'Plexi Lead.nam', blocks)
    c = setParam(c, 0, 'input_db', -6, blocks)
    const out = toWorkerChain(deepFreeze(c))
    expect(out[0]).toEqual({ type: 'amp', ...defaults('amp'), model: 'Plexi Lead.nam', input_db: -6 })
    expect(out[1]).toEqual({ type: 'cab', ...defaults('cab') })
  })
})

describe('fromWorkerChain', () => {
  it('блоки включены, недостающие параметры — умолчания', () => {
    const c = fromWorkerChain(deepFreeze([{ type: 'reverb', wet: 0.5 }, { type: 'gate' }]), blocks)
    expect(types(c)).toEqual(['reverb', 'gate'])
    expect(c.every((b) => b.on === true)).toBe(true)
    expect(c[0].params).toEqual({ ...defaults('reverb'), wet: 0.5 })
    expect(c[1].params).toEqual(defaults('gate'))
  })

  it('eq без полос → bands []', () => {
    expect(fromWorkerChain([{ type: 'eq' }], blocks)[0].params.bands).toEqual([])
  })

  it('пустой JSON → пустая цепочка', () => {
    expect(fromWorkerChain([], blocks)).toEqual([])
  })
})

// ---------- ТК2: из пресета и обратно — тот же JSON ----------

// заполнение умолчаний блока воркера (как parse_chain): числа, строки, полосы eq
function fill(b) {
  const out = { type: b.type, ...defaults(b.type), ...b }
  if (b.type === 'eq') {
    const bd = blocks.eq.bands
    out.bands = (b.bands || []).map((x) => {
      const band = {}
      for (const k of Object.keys(bd)) band[k] = x[k] ?? bd[k].default
      return band
    })
  }
  return out
}

describe('ТК2: пресет → редактируемая цепочка → JSON', () => {
  it('пресеты есть', () => {
    expect(fxPresets.length).toBeGreaterThan(0)
  })

  it.each(fxPresets.map((p) => [p.id, p]))('%s: туда-обратно — тот же JSON (с умолчаниями)', (_, p) => {
    const chain = deepFreeze(JSON.parse(JSON.stringify(p.chain)))
    const editable = fromWorkerChain(chain, blocks)
    expect(editable.length).toBe(p.chain.length)
    expect(toWorkerChain(editable)).toEqual(p.chain.map(fill))
  })

  it('JSON → цепочка → JSON — неподвижная точка', () => {
    for (const p of fxPresets) {
      const once = toWorkerChain(fromWorkerChain(p.chain, blocks))
      const twice = toWorkerChain(fromWorkerChain(deepFreeze(JSON.parse(JSON.stringify(once))), blocks))
      expect(twice).toEqual(once)
    }
  })
})

// Условие 14 (internal-studio-engine, этап 5а), регрессия кросс-ревью:
// missingKits(workerChain, kits) — какие наборы надо скачать перед расчётом.
// workerChain — цепочка для воркера ({type, ...параметры}); kits — список из
// GET /fx/assets ([{name: '<набор>/<часть>', samples}]). Ответ — имена наборов
// (часть до «/»), которых нет, без повторов, в порядке появления в цепочке.
describe('missingKits: недостающие наборы sampler', () => {
  const kits = () => deepFreeze([{ name: 'osdk/kick', samples: 3 }, { name: 'osdk/snare', samples: 2 }])

  it('набор есть → []', () => {
    const chain = deepFreeze([{ type: 'sampler', kit: 'osdk/kick', floor_db: -18, output_db: 0 }])
    expect(missingKits(chain, kits())).toEqual([])
  })

  it('набора нет → имя набора до «/»', () => {
    const chain = deepFreeze([{ type: 'sampler', kit: 'osdk/kick' }])
    expect(missingKits(chain, [])).toEqual(['osdk'])
  })

  it('часть набора не загружена → нужен набор', () => {
    const chain = deepFreeze([{ type: 'sampler', kit: 'osdk/toms' }])
    expect(missingKits(chain, kits())).toEqual(['osdk'])
  })

  it('без повторов: две части одного набора → одно имя', () => {
    const chain = deepFreeze([
      { type: 'sampler', kit: 'osdk/kick' },
      { type: 'gain', gain_db: 3 },
      { type: 'sampler', kit: 'osdk/snare' },
      { type: 'sampler', kit: 'osdk/kick' },
    ])
    expect(missingKits(chain, [])).toEqual(['osdk'])
  })

  it('разные недостающие наборы — каждый по разу, в порядке цепочки', () => {
    const chain = deepFreeze([
      { type: 'sampler', kit: 'other/kick' },
      { type: 'sampler', kit: 'osdk/kick' },
      { type: 'sampler', kit: 'third/snare' },
      { type: 'sampler', kit: 'other/snare' },
    ])
    expect(missingKits(chain, kits())).toEqual(['other', 'third'])
  })

  it('блоки не sampler и sampler без kit игнорируются', () => {
    const chain = deepFreeze([
      { type: 'amp', model: 'osdk/kick' },
      { type: 'cab', ir: 'nope/room.wav' },
      { type: 'reverb', kit: 'nope/kick' },
      { type: 'sampler' },
      { type: 'sampler', kit: '' },
    ])
    expect(missingKits(chain, [])).toEqual([])
  })

  it('пустая цепочка → []', () => {
    expect(missingKits([], [])).toEqual([])
  })
})

// Условие 15 (internal-studio-engine, этап 5б), ТК15: автоскачивание набора — и для блока bass.
describe('missingKits: недостающие наборы bass', () => {
  it('bass с kit growlybass/bass, набора нет → [\'growlybass\']', () => {
    const chain = deepFreeze([{ type: 'bass', kit: 'growlybass/bass', division: 2, floor_db: -20, output_db: 0 }])
    expect(missingKits(chain, [])).toEqual(['growlybass'])
    expect(missingKits(chain, deepFreeze([{ name: 'osdk/kick', samples: 3 }]))).toEqual(['growlybass'])
  })

  it('набор bass загружен → []', () => {
    const chain = deepFreeze([{ type: 'bass', kit: 'growlybass/bass' }])
    expect(missingKits(chain, deepFreeze([{ name: 'growlybass/bass', samples: 40 }]))).toEqual([])
  })

  it('sampler и bass в одной цепочке — оба набора, в порядке цепочки', () => {
    const chain = deepFreeze([
      { type: 'bass', kit: 'growlybass/bass' },
      { type: 'sampler', kit: 'osdk/kick' },
    ])
    expect(missingKits(chain, [])).toEqual(['growlybass', 'osdk'])
  })

  it('bass без kit игнорируется', () => {
    expect(missingKits(deepFreeze([{ type: 'bass' }, { type: 'bass', kit: '' }]), [])).toEqual([])
  })
})
