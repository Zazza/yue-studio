import { describe, it, expect } from 'vitest'
import { secToPx, pxToSec, snapSec, posEdges, gridMarks, secToPosRange, cursorSec, clampWindow, zoomAt, panWindow, viewSecToPx, viewPxToSec } from './waveLogic.js'

describe('перевод пиксели ↔ секунды', () => {
  it('линейно и с округлением к краям', () => {
    expect(pxToSec(500, 200, 1000)).toBe(100)
    expect(secToPx(100, 200, 1000)).toBe(500)
    expect(pxToSec(-50, 200, 1000)).toBe(0)
    expect(pxToSec(5000, 200, 1000)).toBe(200)
    expect(secToPx(500, 200, 1000)).toBe(1000)
  })

  it('нулевая длительность/ширина — не делят на ноль', () => {
    expect(pxToSec(100, 0, 1000)).toBe(0)
    expect(secToPx(100, 200, 0)).toBe(0)
  })

  it('туда-обратно без потери точности', () => {
    const dur = 217.3
    for (const sec of [0, 1.5, 94.02, dur]) {
      expect(pxToSec(secToPx(sec, dur, 947), dur, 947)).toBeCloseTo(sec, 5)
    }
  })
})

describe('прилипание к тактам (snapSec)', () => {
  const edges = posEdges([{ from: 0, to: 2 }, { from: 2, to: 4 }, { from: 4, to: 6 }])
  // motorik 120: такт 4/4 = 2 с, 90 тактов → границы чётных секунд, 2:40 = 160
  const longEdges = posEdges(Array.from({ length: 90 }, (_, i) => ({ from: i * 2, to: (i + 1) * 2 })))

  it('тянет к ближайшей границе: 160.4 → 160', () => {
    expect(snapSec(160.4, longEdges, true)).toBe(160)
    expect(snapSec(160.9, [159, 160, 162], true)).toBe(160)
    expect(snapSec(1.4, edges, true)).toBe(2)
    expect(snapSec(0.9, edges, true)).toBe(0)
  })

  it('выключено — секунда как есть', () => {
    expect(snapSec(160.4, longEdges, false)).toBe(160.4)
  })

  it('точное попадание остаётся на месте', () => {
    expect(snapSec(2, edges, true)).toBe(2)
  })

  it('posEdges даёт начала тактов и конец последнего', () => {
    expect(edges).toEqual([0, 2, 4, 6])
  })
})

describe('клиппинг сетки по длине аудида (gridMarks)', () => {
  const columns = [
    { sec: 0, section: 'intro' }, { sec: 2, section: 'intro' },
    { sec: 4, section: 'verse' }, { sec: 6, section: 'verse' },
    { sec: 8, section: 'outro' },
  ]

  it('такты за концом аудио не рисуются (#135: аудио короче плана)', () => {
    const marks = gridMarks(columns, 7)
    expect(marks.map((m) => m.sec)).toEqual([0, 2, 4, 6])
  })

  it('граница секции помечается только на смене', () => {
    const marks = gridMarks(columns, 100)
    expect(marks[0].section).toBe('intro')
    expect(marks[1].section).toBeNull()
    expect(marks[2].section).toBe('verse')
    expect(marks[4].section).toBe('outro')
  })

  it('аудио длиннее плана — рисуется всё', () => {
    expect(gridMarks(columns, 100).length).toBe(5)
  })
})

describe('секунды → колонки ролла (secToPosRange)', () => {
  const posTimes = [{ from: 0, to: 2 }, { from: 2, to: 4 }, { from: 4, to: 6 }, { from: 6, to: 8 }]
  // motorik 120, такт 2 с, 105 тактов — масштаб кейса приёмки #135
  const longTimes = Array.from({ length: 105 }, (_, i) => ({ from: i * 2, to: (i + 1) * 2 }))

  it('выделение 2:40–2:48 при такте 2 с → колонки 80..83', () => {
    expect(secToPosRange(160, 168, longTimes)).toEqual({ lo: 80, hi: 83 })
  })

  it('внутри одного такта — lo == hi', () => {
    expect(secToPosRange(2.5, 3.5, posTimes)).toEqual({ lo: 1, hi: 1 })
  })

  it('до первого такта — null; инвертированный диапазон — null', () => {
    expect(secToPosRange(5, 5, posTimes)).toBeNull()
    expect(secToPosRange(3, 1, posTimes)).toBeNull()
  })

  it('за концом плана — прижат к последней колонке', () => {
    expect(secToPosRange(7.9, 100, posTimes)).toEqual({ lo: 3, hi: 3 })
  })
})

describe('курсор между опросами плеера (cursorSec)', () => {
  it('идёт вперёд на прошедшее от последнего опроса', () => {
    expect(cursorSec(10, 1000, true, 1500, 200)).toBeCloseTo(10.5, 5)
  })

  it('на паузе стоит на последней позиции', () => {
    expect(cursorSec(10, 1000, false, 9999, 200)).toBe(10)
  })

  it('за концом трека прижат к концу, опрос «из прошлого» не уводит назад', () => {
    expect(cursorSec(199.9, 1000, true, 5000, 200)).toBe(200)
    expect(cursorSec(50, 1000, true, 500, 200)).toBe(50)
  })
})

describe('окно просмотра волны (зум)', () => {
  const dur = 200

  it('clampWindow не даёт окну вылезти за трек и сжаться меньше секунды', () => {
    expect(clampWindow({ t0: -10, span: 500 }, dur)).toEqual({ t0: 0, span: 200 })
    expect(clampWindow({ t0: 150, span: 100 }, dur)).toEqual({ t0: 100, span: 100 })
    expect(clampWindow({ t0: 10, span: 0.2 }, dur)).toEqual({ t0: 10, span: 1 })
  })

  it('зум держит точку под курсором на месте', () => {
    const win = { t0: 0, span: 200 }
    const z = zoomAt(win, dur, 50, 4)   // ×4 под 50 с
    expect(z.span).toBe(50)
    // 50 с была на 1/4 окна — и осталась на 1/4
    expect((50 - z.t0) / z.span).toBeCloseTo(0.25, 5)
  })

  it('зум сильнее окна — клампится в границы', () => {
    const full = { t0: 0, span: 200 }
    expect(zoomAt(full, dur, 0, 1000)).toEqual({ t0: 0, span: 1 })
    const nearEnd = zoomAt(full, dur, 199.9, 1000)
    expect(nearEnd.span).toBe(1)
    expect(nearEnd.t0 + nearEnd.span).toBeLessThanOrEqual(dur)   // окно не вылезло за трек
    expect(zoomAt(full, dur, dur, 1000).t0).toBe(dur - 1)        // якорь на краю — окно к краю
  })

  it('панорама двигает окно и упирается в края', () => {
    expect(panWindow({ t0: 0, span: 50 }, dur, 30)).toEqual({ t0: 30, span: 50 })
    expect(panWindow({ t0: 0, span: 50 }, dur, -30)).toEqual({ t0: 0, span: 50 })
    expect(panWindow({ t0: 100, span: 50 }, dur, 100)).toEqual({ t0: 150, span: 50 })
  })

  it('маппинг внутри окна туда-обратно', () => {
    const win = { t0: 160, span: 8 }
    expect(viewSecToPx(160, win, 800)).toBe(0)
    expect(viewSecToPx(168, win, 800)).toBe(800)
    expect(viewPxToSec(400, win, 800)).toBe(164)
    expect(viewPxToSec(viewSecToPx(162.5, win, 947), win, 947)).toBeCloseTo(162.5, 5)
  })
})
