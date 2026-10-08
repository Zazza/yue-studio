// Пресеты звука во фронте — чистая логика без DOM: правки студии → записи пресета, строка статуса
// пресета в карточке трека, выбор пресетов чипами в форме нового трека.
import RU from './i18n/ru.js'

// подстановка {имя} — как t() из i18n; по умолчанию русский словарь (тесты и вызов без t)
const ruT = (key, vars = {}) => Object.entries(vars).reduce((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), RU[key] ?? key)

const KIND = ['engine', 'chain', 'steps']

/** Активные правки «весь трек» с движком, эффектом или педалями → записи пресета
 *  [{stems, engine | chain+params | steps, db}]; skipped — сколько активных правок не вошло
 *  (с окном, вклейки, заглушения, линии громкости). Выключенные не считаются никак. */
export function presetFromEdits(applied) {
  const specs = []
  let skipped = 0
  for (const it of applied || []) {
    if (it.off) continue
    const whole = !(it.from > 0) && !(it.to > 0)
    const kind = KIND.find((k) => it[k] && (!Array.isArray(it[k]) || it[k].length))
    if (!whole || it.childId > 0 || !kind || it.envelope) { skipped++; continue }
    const spec = { stems: [...(it.stems || [])] }
    if (kind === 'engine') spec.engine = JSON.parse(JSON.stringify(it.engine))
    else if (kind === 'chain') Object.assign(spec, { chain: it.chain, params: { ...(it.params || {}) } })
    else spec.steps = JSON.parse(JSON.stringify(it.steps))
    spec.db = it.db || 0
    specs.push(spec)
  }
  return { specs, skipped }
}

const ICONS = { pending: '⏳', running: '⟳', done: '✓', error: '✕' }

/** Строка пресета у трека: {icon, text, retry}; повтор — у ошибки и у «применяется» (приложение закрыли
 *  посреди применения — статус застрял навсегда). t — перевод (по умолчанию ru). */
export function presetLine(state, presetName, t = ruT) {
  const status = (state && state.status) || 'pending'
  const vars = { name: presetName, id: state && state.child_id, error: (state && state.error) || '' }
  return {
    icon: ICONS[status] || ICONS.pending,
    text: t('preset.line.' + (ICONS[status] ? status : 'pending'), vars),
    retry: status === 'error' || status === 'running',
  }
}

/** Выбор пресета чипом: есть — убрать, нет — добавить в конец, если выбрано меньше max. */
export function togglePreset(ids, id, max = 3) {
  const cur = ids || []
  if (cur.includes(id)) return cur.filter((x) => x !== id)
  return cur.length >= max ? [...cur] : [...cur, id]
}
