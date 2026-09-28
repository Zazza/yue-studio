// Фильтры и пейджер списка треков. Чистая логика: список приходит от воркера
// уже упорядоченным от новых к старым, здесь только отсев и нарезка страниц.
export const QUEUE_PAGE_SIZE = 20

const DAY = 86_400_000

export function defaultJobFilter() {
  return { status: 'all', period: 'all', dur: 'all', draft: 'all', q: '' }
}

// возраст джобы: без распознаваемой даты — бесконечность (в период не попадает)
function ageMs(j, now) {
  const t = Date.parse(j.created_at || '')
  return Number.isFinite(t) ? now - t : Infinity
}

// коридоры длительности в секундах: проба <1 мин, короткий 1–3, средний 3–6, длинный >6
const DUR_BUCKETS = {
  draft: (d) => d < 60,
  short: (d) => d >= 60 && d <= 180,
  mid: (d) => d > 180 && d <= 360,
  long: (d) => d > 360,
}

export function filterJobs(jobs, f, now = Date.now()) {
  const q = (f.q || '').trim().toLowerCase()
  return (jobs || []).filter((j) => {
    if (f.status === 'active' && !['queued', 'running'].includes(j.status)) return false
    if (f.status === 'done' && j.status !== 'done') return false
    if (f.status === 'failed' && !['error', 'canceled'].includes(j.status)) return false
    if (f.period === 'today' && ageMs(j, now) > DAY) return false
    if (f.period === 'week' && ageMs(j, now) > 7 * DAY) return false
    if (f.period === 'month' && ageMs(j, now) > 30 * DAY) return false
    if (f.dur !== 'all' && !DUR_BUCKETS[f.dur](j.duration_sec || 0)) return false
    if (f.draft === 'only' && !j.draft) return false
    if (f.draft === 'hide' && j.draft) return false
    if (q && !(`${j.title || ''} ${j.style || ''}`.toLowerCase().includes(q))) return false
    return true
  })
}

export function pageCount(total, size = QUEUE_PAGE_SIZE) {
  return Math.max(1, Math.ceil((total || 0) / size))
}

// страница как по спецификации: выход за границы зажимается, размер — по умолчанию
export function pageJobs(jobs, page, size = QUEUE_PAGE_SIZE) {
  const p = Math.min(Math.max(1, page), pageCount(jobs.length, size))
  return (jobs || []).slice((p - 1) * size, p * size)
}
