// Эффекты «одним кликом» (мастеринг, дыхание) с уровнем «легко / средне / сильно»:
// уровень — набор крутилок цепочки. «Средне» у мастеринга — проверенный профиль
// (дефолты цепочки, замер на #173); «легко» убавляет песок верхов — подъём верхов
// первым делает голос звонким и пискливым.

export const ONE_CLICK_LEVELS = ['light', 'medium', 'strong']

const PRESETS = {
  master: {
    light: { drive: 1.2, grit: 0.6, breath: 0.75 },
    medium: { drive: 1.5, grit: 1.3, breath: 1.5 },
    strong: { drive: 2.0, grit: 2.0, breath: 1.5 },
  },
  breathe: {
    light: { amount: 0.5 },
    medium: { amount: 1 },
    strong: { amount: 1.5 },
  },
}

export const ONE_CLICK_CHAINS = Object.keys(PRESETS)

// крутилки цепочки для уровня; неизвестный уровень — «средне», неизвестная цепочка — null
export function oneClickParams(chain, level) {
  const p = PRESETS[chain]
  if (!p) return null
  return { ...(p[level] || p.medium) }
}
