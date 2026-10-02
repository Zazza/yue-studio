// Фильтры и пейджер списка треков. Чистая логика: список приходит от воркера
// уже упорядоченным от новых к старым, здесь только отсев и нарезка страниц.
export const QUEUE_PAGE_SIZE = 20

const DAY = 86_400_000

export function defaultJobFilter() {
  return { status: 'all', period: 'all', dur: 'all', draft: 'all', folder: 'all', q: '' }
}

// Папки песен: эти три есть всегда (даже пустые), свои — по именам у треков.
export const DEFAULT_FOLDERS = ['Альбом', 'Основы', 'Эксперименты']
// значение фильтра «только песни без папки»
export const FOLDER_NONE = '-'

const folderKey = (s) => String(s || '').trim().toLowerCase()

// список папок для выбора: сначала DEFAULT_FOLDERS в их порядке, затем свои
// по алфавиту; повтор без учёта регистра — одна папка (первое написание)
export function folderNames(jobs) {
  const seen = new Set(DEFAULT_FOLDERS.map(folderKey))
  const own = []
  for (const j of jobs || []) {
    const name = String(j.folder || '').trim()
    if (name && !seen.has(folderKey(name))) {
      seen.add(folderKey(name))
      own.push(name)
    }
  }
  own.sort((a, b) => a.localeCompare(b, 'ru'))
  return [...DEFAULT_FOLDERS, ...own]
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

// совпадение поиска: номер («425» или «#425» — точно) либо подстрока названия/стиля
function matchesQuery(j, q) {
  const num = /^#?(\d+)$/.exec(q)
  if (num && String(j.id) === num[1]) return true
  return `${j.title || ''} ${j.style || ''}`.toLowerCase().includes(q)
}

// children — версии песен ({ [rootId]: [...] }, как groupJobs().children):
// поиск смотрит и в них — песня видна, если совпала любая её версия
export function filterJobs(jobs, f, now = Date.now(), children = {}) {
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
    if (f.folder === FOLDER_NONE && folderKey(j.folder)) return false
    if (f.folder && f.folder !== 'all' && f.folder !== FOLDER_NONE && folderKey(j.folder) !== folderKey(f.folder)) return false
    if (q && !matchesQuery(j, q) && !(children[j.id] || []).some((k) => matchesQuery(k, q))) return false
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

// родитель производного трека: parent_id (кусок для вклейки, пересборка,
// проверка куска, вариант-трек) или overdub_of (овердаб-партия)
export function jobParent(j) {
  return j.parent_id || j.overdub_of || null
}

// группировка списка: производные треки прячутся под корневым предком
// (цепочка родителей до трека без родителя). Родитель вне списка (старый,
// удалён) — трек остаётся на верхнем уровне. Порядок сохраняется.
// → { top: [...], children: { [rootId]: [...] } }
export function groupJobs(jobs) {
  const byId = new Map((jobs || []).map((j) => [j.id, j]))
  const rootOf = (j) => {
    const seen = new Set()
    let cur = j
    while (jobParent(cur) && byId.has(jobParent(cur)) && !seen.has(cur.id)) {
      seen.add(cur.id)
      cur = byId.get(jobParent(cur))
    }
    return cur
  }
  const top = []
  const children = {}
  for (const j of jobs || []) {
    const root = rootOf(j)
    if (root.id === j.id) top.push(j)
    else (children[root.id] = children[root.id] || []).push(j)
  }
  // песня с последней правкой — наверх: активность = самое свежее создание
  // среди неё и её вложений (новая версия поднимает песню, а не прячется внизу)
  const last = (j) => [j, ...(children[j.id] || [])]
    .reduce((m, x) => (String(x.created_at || '') > m ? String(x.created_at || '') : m), '')
  top.sort((a, b) => last(b).localeCompare(last(a)) || b.id - a.id)
  return { top, children }
}
