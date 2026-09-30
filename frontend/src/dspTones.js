// «Найти свист»: найденные тона → крутилки эффекта «Убрать свист» (чистая логика).

// до трёх тонов за проход: самый заметный — freq, следующие — freq2/freq3
// (шаг крутилки 5 Гц); выделение на волне/ролле задаёт окно start/end (шаг 0.5 с)
export function applyFoundTones(params, tones, sel) {
  const hz = (i) => (tones && tones[i] ? Math.round(tones[i].hz / 5) * 5 : 0)
  const next = { ...(params || {}), freq: hz(0), freq2: hz(1), freq3: hz(2) }
  if (sel) Object.assign(next, { start: Math.floor(sel.from * 2) / 2, end: Math.ceil(sel.to * 2) / 2 })
  return next
}
