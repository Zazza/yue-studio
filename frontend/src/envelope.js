// Линия громкости по волне: точки {t (с), db}, между ними — линейно в дБ
// (как dsp.EnvelopeGraph в Go). Функции чистые и не мутируют вход.

export const ENV_MIN_DB = -30
export const ENV_MAX_DB = 12

const clampDb = (db) => Math.min(ENV_MAX_DB, Math.max(ENV_MIN_DB, db))

export function addPoint(pts, t, db) {
  return [...pts, { t, db: clampDb(db) }].sort((a, b) => a.t - b.t)
}

// точка не перескакивает соседей и не встаёт с ними в одно время (Go из
// точек с одним t оставит одну — нарисованная ступенька не прозвучала бы)
const ENV_GAP_SEC = 0.01
export function movePoint(pts, i, t, db) {
  const lo = i > 0 ? pts[i - 1].t + ENV_GAP_SEC : 0
  const hi = i < pts.length - 1 ? pts[i + 1].t - ENV_GAP_SEC : Infinity
  return pts.map((p, k) => (k === i ? { t: Math.min(hi, Math.max(lo, t)), db: clampDb(db) } : p))
}

export function removePoint(pts, i) {
  return pts.filter((_, k) => k !== i)
}

// дБ линии в момент t: до первой точки — её дБ, после последней — её дБ
export function dbAt(pts, t) {
  if (!pts.length) return 0
  if (t <= pts[0].t) return pts[0].db
  for (let i = 0; i + 1 < pts.length; i++) {
    const a = pts[i], b = pts[i + 1]
    if (t < b.t) return a.db + (t - a.t) * (b.db - a.db) / (b.t - a.t)
  }
  return pts[pts.length - 1].db
}

// вертикаль канвы: +12 дБ — верх (y = 0), −30 дБ — низ (y = h)
export function dbToY(db, h) {
  return (ENV_MAX_DB - db) / (ENV_MAX_DB - ENV_MIN_DB) * h
}

export function yToDb(y, h) {
  return ENV_MAX_DB - y / h * (ENV_MAX_DB - ENV_MIN_DB)
}

// индекс ближайшей точки в радиусе r пикселей, иначе −1
export function hitPoint(pts, px, py, toX, toY, r) {
  let best = -1, bestD = r
  pts.forEach((p, i) => {
    const d = Math.hypot(toX(p.t) - px, toY(p.db) - py)
    if (d <= bestD) { best = i; bestD = d }
  })
  return best
}

export function isFlat(pts) {
  return pts.every((p) => p.db === 0)
}
