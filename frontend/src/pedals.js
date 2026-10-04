// Доска педалей (блок «Педали» студии): массив педалей [{chain, params, off}] в
// порядке звука. Чистые функции — вход не меняют, возвращают новую доску;
// покрываются vitest.

// addPedal — педаль в конец доски с крутилками по умолчанию.
export function addPedal(board, chainId, defaults) {
  return [...(board || []), { chain: chainId, params: { ...(defaults || {}) }, off: false }]
}

export function removePedal(board, i) {
  return (board || []).filter((_, k) => k !== i)
}

// movePedal — сдвиг педали на dir (−1 — раньше в цепочке, +1 — позже); за края не уходит.
export function movePedal(board, i, dir) {
  const b = [...(board || [])]
  const j = i + dir
  if (i < 0 || i >= b.length || j < 0 || j >= b.length) return b
  ;[b[i], b[j]] = [b[j], b[i]]
  return b
}

// togglePedal — включить/выключить педаль (выключенная остаётся на доске).
export function togglePedal(board, i) {
  return (board || []).map((p, k) => (k === i ? { ...p, off: !p.off } : p))
}

export function setParam(board, i, id, v) {
  return (board || []).map((p, k) => (k === i ? { ...p, params: { ...p.params, [id]: v } } : p))
}

// boardSteps — шаги для API (Go dsp.Step): chain, params, off.
export function boardSteps(board) {
  return (board || []).map((p) => ({ chain: p.chain, params: { ...(p.params || {}) }, off: !!p.off }))
}

// applyPreset — доска из готового набора: крутилки — по умолчанию цепочки,
// поверх — значения набора; цепочки, которых нет в списке, пропускаются.
export function applyPreset(preset, chains) {
  const byId = new Map((chains || []).map((c) => [c.id, c]))
  const board = []
  for (const st of (preset && preset.steps) || []) {
    const c = byId.get(st.chain)
    if (!c) continue
    const defaults = {}
    for (const prm of c.params || []) defaults[prm.id] = prm.default
    board.push({ chain: st.chain, params: { ...defaults, ...(st.params || {}) }, off: !!st.off })
  }
  return board
}

// pedalChains — палитра «+ педаль»: только цепочки-педали.
export function pedalChains(chains) {
  return (chains || []).filter((c) => c.pedal)
}
