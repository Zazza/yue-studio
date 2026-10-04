// Окно быстрого превью эффекта: какой кусок трека слушать. Чистая логика без
// DOM — покрывается vitest.

export const PREVIEW_LEN = 15 // секунд, если места не выбрали
const DEFAULT_FROM = 20 // старое превью «с 20-й секунды» — когда нет ни выделения, ни курсора
const MIN_LEN = 1

// previewWindow — выделение на волне/ролле → оно (обрезанное по длине трека);
// нет выделения, есть курсор воспроизведения → 15 с от курсора (у конца трека —
// последние 15 с); ничего (null) — с 20-й секунды. Окно не короче секунды, но и
// не длиннее трека: трек короче секунды — весь. durSec 0/неизвестно — без обрезки по концу.
export function previewWindow(sel, cursorSec, durSec) {
  const end = durSec > 0 ? durSec : Infinity
  const clamp = (from, to) => {
    let f = Math.max(0, from)
    let t = Math.min(to, end)
    if (t - f < MIN_LEN) {
      t = Math.min(end, f + MIN_LEN)
      f = Math.max(0, t - MIN_LEN)
    }
    return { from: f, to: t }
  }
  if (sel && sel.to > sel.from) return clamp(sel.from, sel.to)
  if (cursorSec != null && cursorSec >= 0) {
    const from = Math.min(cursorSec, Math.max(0, end - PREVIEW_LEN))
    return clamp(from, from + PREVIEW_LEN)
  }
  const from = end > DEFAULT_FROM + PREVIEW_LEN ? DEFAULT_FROM : Math.max(0, end - PREVIEW_LEN)
  return clamp(from, from + PREVIEW_LEN)
}
