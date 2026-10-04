// Тесты карточки internal-guitar-pedals, условие 2.1: операции доски педалей —
// чистые функции, вход не мутируют. Доска = массив педалей [{chain, params, off}].
// Написаны по карточке, без чтения реализации.
import { describe, it, expect } from 'vitest'
import {
  addPedal, removePedal, movePedal, togglePedal, setParam,
  boardSteps, applyPreset, pedalChains,
} from './pedals.js'

// deepFreeze — любая мутация входа в строгом режиме модуля бросит исключение.
function deepFreeze(o) {
  if (o && typeof o === 'object' && !Object.isFrozen(o)) {
    Object.freeze(o)
    for (const v of Object.values(o)) deepFreeze(v)
  }
  return o
}

const board3 = () => deepFreeze([
  { chain: 'od-ts', params: { drive: 5, tone: 0.5 }, off: false },
  { chain: 'chorus', params: { rate: 0.6 }, off: true },
  { chain: 'reverb-room', params: { wet: 0.3 }, off: false },
])

const chains = [
  { id: 'od-ts', name: 'Овердрайв', pedal: true,
    params: [{ id: 'drive', default: 4 }, { id: 'tone', default: 0.5 }, { id: 'level', default: 0 }] },
  { id: 'chorus', name: 'Хорус', pedal: true, params: [{ id: 'rate', default: 0.6 }, { id: 'mix', default: 0.5 }] },
  { id: 'wall', name: 'Стена', params: [{ id: 'wall', default: 0.5 }] },
  { id: 'dewhistle', name: 'Убрать свист', pedal: false, params: [{ id: 'freq', default: 3000 }] },
]

const order = b => b.map(p => p.chain)

describe('addPedal', () => {
  it('новая педаль в конец, params — копия defaults, off false', () => {
    const defaults = deepFreeze({ drive: 4, tone: 0.5 })
    const b = addPedal(board3(), 'fuzz-muff', defaults)
    expect(b).toHaveLength(4)
    expect(b[3]).toEqual({ chain: 'fuzz-muff', params: { drive: 4, tone: 0.5 }, off: false })
    expect(order(b).slice(0, 3)).toEqual(['od-ts', 'chorus', 'reverb-room'])
  })

  it('params — копия: правка педали не меняет defaults', () => {
    const defaults = { drive: 4 }
    const b = addPedal([], 'od-ts', defaults)
    expect(b[0].params).not.toBe(defaults)
    b[0].params.drive = 9
    expect(defaults.drive).toBe(4)
  })

  it('на пустую доску — одна педаль', () => {
    expect(addPedal([], 'boost', {})).toEqual([{ chain: 'boost', params: {}, off: false }])
  })

  it('одну цепочку можно поставить дважды', () => {
    const b = addPedal(addPedal([], 'od-ts', {}), 'od-ts', {})
    expect(order(b)).toEqual(['od-ts', 'od-ts'])
  })
})

describe('removePedal', () => {
  it('убирает педаль по индексу, остальные по порядку', () => {
    expect(order(removePedal(board3(), 1))).toEqual(['od-ts', 'reverb-room'])
  })

  it('индекс вне доски — доска без изменений', () => {
    expect(removePedal(board3(), 7)).toEqual(board3())
    expect(removePedal(board3(), -1)).toEqual(board3())
  })
})

describe('movePedal', () => {
  it('dir +1 — на место вправо, dir −1 — влево', () => {
    expect(order(movePedal(board3(), 0, 1))).toEqual(['chorus', 'od-ts', 'reverb-room'])
    expect(order(movePedal(board3(), 2, -1))).toEqual(['od-ts', 'reverb-room', 'chorus'])
  })

  it('за края не уходит: первая влево и последняя вправо — без изменений', () => {
    expect(movePedal(board3(), 0, -1)).toEqual(board3())
    expect(movePedal(board3(), 2, 1)).toEqual(board3())
  })

  it('педаль переезжает целиком (params и off с ней)', () => {
    const b = movePedal(board3(), 1, 1)
    expect(b[2]).toEqual({ chain: 'chorus', params: { rate: 0.6 }, off: true })
  })
})

describe('togglePedal', () => {
  it('переключает off только у выбранной педали, туда и обратно', () => {
    const b = togglePedal(board3(), 0)
    expect(b[0].off).toBe(true)
    expect(b[1].off).toBe(true)
    expect(b[2].off).toBe(false)
    expect(togglePedal(b, 0)[0].off).toBe(false)
    expect(togglePedal(board3(), 1)[1].off).toBe(false)
  })
})

describe('setParam', () => {
  it('меняет один параметр выбранной педали, прочие на месте', () => {
    const b = setParam(board3(), 0, 'drive', 8)
    expect(b[0].params).toEqual({ drive: 8, tone: 0.5 })
    expect(b[1]).toEqual(board3()[1])
    expect(b[2]).toEqual(board3()[2])
  })

  it('новый ключ параметра добавляется', () => {
    expect(setParam(board3(), 2, 'size', 1.2)[2].params).toEqual({ wet: 0.3, size: 1.2 })
  })

  it('значение 0 записывается (не теряется как «пусто»)', () => {
    expect(setParam(board3(), 0, 'drive', 0)[0].params.drive).toBe(0)
  })
})

describe('boardSteps', () => {
  it('доска → шаги API {chain, params, off} по порядку, Off сохраняется', () => {
    const steps = boardSteps(board3())
    expect(steps).toHaveLength(3)
    steps.forEach((s, i) => {
      expect(s.chain).toBe(board3()[i].chain)
      expect(s.params).toEqual(board3()[i].params)
      expect(!!s.off).toBe(board3()[i].off)
    })
    expect(steps[1].off).toBe(true)
  })

  it('пустая доска — пустой список шагов', () => {
    expect(boardSteps([])).toEqual([])
  })
})

describe('applyPreset', () => {
  it('набор → доска: params = значения по умолчанию цепочки, поверх — params набора', () => {
    const preset = deepFreeze({ id: 'p', name: 'P', note: 'n',
      steps: [{ chain: 'od-ts', params: { drive: 7 } }, { chain: 'chorus' }] })
    const b = applyPreset(preset, deepFreeze(chains))
    expect(order(b)).toEqual(['od-ts', 'chorus'])
    expect(b[0].params).toEqual({ drive: 7, tone: 0.5, level: 0 })
    expect(b[1].params).toEqual({ rate: 0.6, mix: 0.5 })
    expect(b[0].off).toBe(false)
    expect(b[1].off).toBe(false)
  })

  it('неизвестные цепочки пропускаются, порядок прочих сохраняется', () => {
    const preset = { id: 'p', name: 'P', note: 'n',
      steps: [{ chain: 'no-such' }, { chain: 'chorus' }, { chain: 'gone' }, { chain: 'od-ts' }] }
    expect(order(applyPreset(preset, chains))).toEqual(['chorus', 'od-ts'])
  })

  it('набор без известных цепочек — пустая доска', () => {
    expect(applyPreset({ id: 'p', name: 'P', note: 'n', steps: [{ chain: 'x' }] }, chains)).toEqual([])
  })

  it('доски от одного набора независимы (params не общие)', () => {
    const preset = { id: 'p', name: 'P', note: 'n', steps: [{ chain: 'od-ts', params: { drive: 7 } }] }
    const a = applyPreset(preset, chains)
    a[0].params.drive = 1
    expect(preset.steps[0].params.drive).toBe(7)
    expect(applyPreset(preset, chains)[0].params.drive).toBe(7)
  })
})

describe('pedalChains', () => {
  it('только цепочки с pedal, порядок сохраняется', () => {
    expect(pedalChains(deepFreeze(chains)).map(c => c.id)).toEqual(['od-ts', 'chorus'])
  })

  it('пустой список — пусто', () => {
    expect(pedalChains([])).toEqual([])
  })
})

describe('вход не мутируется', () => {
  it('все операции над замороженной доской не бросают и не меняют её', () => {
    const b = board3()
    const snap = JSON.stringify(b)
    addPedal(b, 'boost', {})
    removePedal(b, 0)
    movePedal(b, 0, 1)
    togglePedal(b, 0)
    setParam(b, 0, 'drive', 1)
    boardSteps(b)
    expect(JSON.stringify(b)).toBe(snap)
  })

  it('операции возвращают новый массив, а не тот же', () => {
    const b = board3()
    expect(addPedal(b, 'boost', {})).not.toBe(b)
    expect(togglePedal(b, 0)).not.toBe(b)
    expect(setParam(b, 0, 'drive', 1)).not.toBe(b)
    expect(movePedal(b, 0, 1)).not.toBe(b)
  })
})
