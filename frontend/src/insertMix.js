// Громкость замены дорожки: дБ относительно старой дорожки в окне. Новая
// дорожка сначала выравнивается по уровню старой (Go, stemGain) — 0 дБ = как
// было; «выделить бас» — плюс (на плотном треке нужно до +12).
export const INSERT_DEFAULT_DB = 0
export const INSERT_MIN_DB = -24
export const INSERT_MAX_DB = 12

export function clampDb(db) {
  const n = typeof db === 'number' ? db : NaN
  if (!Number.isFinite(n)) return INSERT_DEFAULT_DB
  return Math.min(INSERT_MAX_DB, Math.max(INSERT_MIN_DB, n))
}
