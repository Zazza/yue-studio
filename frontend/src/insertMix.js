// Громкость вклеек инструмента: дБ относительно оригинала. Уровень партии
// сначала выравнивается по окну оригинала (Go, dsp.InsertGain), дБ — поверх.
export const INSERT_DEFAULT_DB = -6
export const INSERT_MIN_DB = -24
export const INSERT_MAX_DB = 6

export function clampDb(db) {
  const n = typeof db === 'number' ? db : NaN
  if (!Number.isFinite(n)) return INSERT_DEFAULT_DB
  return Math.min(INSERT_MAX_DB, Math.max(INSERT_MIN_DB, n))
}
