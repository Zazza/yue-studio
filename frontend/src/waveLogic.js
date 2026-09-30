// Чистая математика волны громкости студии: пиксели↔секунды, прилипание
// к границам тактов, сетка тактов/секций по длине аудио, ход курсора
// между опросами позиции плеера. Без DOM — покрыто vitest.

// пиксель → секунда (и обратно): линейно по длительности, с прижатием к краям
export function secToPx(sec, durationSec, width) {
  if (!(durationSec > 0) || !(width > 0)) return 0
  const k = Math.min(Math.max(sec / durationSec, 0), 1)
  return k * width
}

export function pxToSec(px, durationSec, width) {
  if (!(durationSec > 0) || !(width > 0)) return 0
  const k = Math.min(Math.max(px / width, 0), 1)
  return k * durationSec
}

// прилипание к ближайшей границе такта (edges — отсортированные секунды);
// выключено или пусто — секунда как есть
export function snapSec(sec, edges, enabled) {
  if (!enabled || !edges || !edges.length) return sec
  let best = edges[0]
  let dist = Math.abs(sec - best)
  for (const e of edges) {
    const d = Math.abs(sec - e)
    if (d < dist) { dist = d; best = e }
  }
  return best
}

// границы прилипания из колонок ролла: начала тактов + конец последнего
export function posEdges(posTimes) {
  const out = []
  let lastEnd = -Infinity
  for (const t of posTimes || []) {
    if (!t || !isFinite(t.from)) continue
    out.push(t.from)
    lastEnd = Math.max(lastEnd, t.to)
  }
  if (isFinite(lastEnd)) out.push(lastEnd)
  return out
}

// сетка поверх волны из колонок ролла [{sec, section}]: линия на каждый такт,
// section != null — ещё и граница секции. Колонки за концом аудида не рисуются
// (трек бывает короче плана: аудио 2:20 при плане 3:30)
export function gridMarks(columns, durationSec) {
  const out = []
  let prev = null
  for (const c of columns || []) {
    if (!c || !(c.sec < durationSec)) break
    out.push({ sec: c.sec, section: c.section !== prev ? c.section : null })
    prev = c.section
  }
  return out
}

// секунды → колонки ролла (обратная связь выделения волны с роллом):
// lo — последняя колонка, начинающаяся не позже from; hi — последняя,
// начинающаяся раньше to. До первого такта — null.
export function secToPosRange(from, to, posTimes) {
  const cols = (posTimes || []).filter((t) => t && isFinite(t.from))
  if (!cols.length || !(to > from)) return null
  let lo = 0
  let hi = -1
  for (let i = 0; i < cols.length; i++) {
    if (cols[i].from <= from) lo = i
    if (cols[i].from < to) hi = i
  }
  if (hi < 0) return null
  return { lo: Math.min(lo, hi), hi: Math.max(lo, hi) }
}

// курсор между опросами плеера (позиция приходит раз в секунду): идём вперёд
// на прошедшее время, на паузе стоим, за концом прижаты к концу
export function cursorSec(lastPos, lastTsMs, playing, nowMs, durationSec) {
  const pos = playing ? (lastPos || 0) + Math.max(0, (nowMs - lastTsMs) / 1000) : (lastPos || 0)
  if (!(durationSec > 0)) return Math.max(0, pos)
  return Math.min(Math.max(pos, 0), durationSec)
}

// ---------- окно просмотра (зум волны) ----------

// минимальный охват окна: дальше — доли секунды на экран, клик теряет смысл
export const WAVE_MIN_SPAN = 1

// окно {t0, span} в мировых секундах: span ограничен [WAVE_MIN_SPAN, длительность],
// t0 — чтобы окно не вылезало за трек
export function clampWindow(win, durationSec) {
  if (!(durationSec > 0)) return { t0: 0, span: 1 }
  const span = Math.min(Math.max(win.span, WAVE_MIN_SPAN), durationSec)
  const t0 = Math.min(Math.max(win.t0, 0), durationSec - span)
  return { t0, span }
}

// зум колесом: factor > 1 — приблизить; точка anchorSec остаётся на той же
// доле окна (курсор «стоит на месте»)
export function zoomAt(win, durationSec, anchorSec, factor) {
  const span = Math.min(Math.max(win.span / factor, WAVE_MIN_SPAN), durationSec)
  const frac = Math.min(Math.max((anchorSec - win.t0) / win.span, 0), 1)
  return clampWindow({ t0: anchorSec - frac * span, span }, durationSec)
}

// панорама (shift+колесо / горизонтальное колесо) на deltaSec
export function panWindow(win, durationSec, deltaSec) {
  return clampWindow({ t0: win.t0 + deltaSec, span: win.span }, durationSec)
}

// маппинг пиксели ↔ секунды внутри окна (без clamp — сетку рисуем и за краем,
// клики не выходят за канву физически)
export function viewSecToPx(sec, win, width) {
  if (!(win.span > 0) || !(width > 0)) return 0
  return ((sec - win.t0) / win.span) * width
}

export function viewPxToSec(px, win, width) {
  if (!(win.span > 0) || !(width > 0)) return 0
  return win.t0 + (px / width) * win.span
}
