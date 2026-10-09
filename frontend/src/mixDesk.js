// Сведение и мастер студии — чистая логика (без DOM): место дорожки в стерео (панорама, ширина),
// запись мастера в реестре пересборки, подписи. Сам звук считает пересборка: место — матрицей на все
// правки дорожки (Go), мастер — блоками движка glue/limiter на воркере.

export const PAN_MIN = -1
export const PAN_MAX = 1
export const WIDTH_MIN = 0
export const WIDTH_MAX = 2
export const CENTER = Object.freeze({ pan: 0, width: 1 })

let seq = 0
// отрицательный номер записи без рендера — как у прочих правок реестра (childId ≤ 0)
const ownId = () => -(Date.now() * 100 + (seq++ % 100))

const clamp = (v, lo, hi, dflt) => (Number.isFinite(Number(v)) ? Math.min(hi, Math.max(lo, Number(v))) : dflt)

/** Место {pan, width} в пределах; width не задан — 1 (как есть), а не 0 (моно). */
export function normPlace(place) {
  const p = place || {}
  return { pan: clamp(p.pan, PAN_MIN, PAN_MAX, 0), width: clamp(p.width ?? 1, WIDTH_MIN, WIDTH_MAX, 1) }
}

export const isCenter = (place) => {
  const p = normPlace(place)
  return p.pan === 0 && p.width === 1
}

const num = (v) => String(Math.round(v * 100) / 100).replace('.', ',')

const RU_WORDS = { center: 'центр', right: 'вправо', left: 'влево', width: 'ширина' }

/** «центр», «30 % вправо», «влево, ширина 1,4» (край — без процентов); words — слова другого языка. */
export function placeLabel(place, words = RU_WORDS) {
  const { pan, width } = normPlace(place)
  let side = words.center
  if (pan !== 0) {
    const dir = pan > 0 ? words.right : words.left
    side = Math.abs(pan) === 1 ? dir : `${Math.round(Math.abs(pan) * 100)} % ${dir}`
  }
  return width === 1 ? side : `${side}, ${words.width} ${num(width)}`
}

/** Запись «место дорожки»: место без обработки на одну дорожку. */
export function isPlaceRecord(it) {
  return !!(it && it.place && !it.engine && !it.chain && !it.steps && !it.envelope && !it.add && !it.master &&
    !(it.childId > 0) && (it.stems || []).length === 1)
}

export const isMasterRecord = (it) => !!(it && it.master && it.engine)

/** Место дорожки stem в реестре: запись этой дорожки заменяется (на её месте в списке), центр — убирается. */
export function withStemPlace(list, stem, place) {
  const cur = list || []
  const at = cur.findIndex((it) => isPlaceRecord(it) && it.stems[0] === stem)
  const rest = cur.filter((it) => !(isPlaceRecord(it) && it.stems[0] === stem))
  if (isCenter(place)) return rest
  const rec = {
    childId: ownId(), instId: 'place', from: 0, to: 0, lead: 0, beat: 0, db: 0,
    stems: [stem], fadeIn: 0, fadeOut: 0, keepHighHz: 0, place: normPlace(place),
  }
  const out = [...rest]
  out.splice(at < 0 ? out.length : at, 0, rec)
  return out
}

/** Место дорожки в реестре (нет записи — центр). */
export function stemPlace(list, stem) {
  const it = (list || []).find((x) => isPlaceRecord(x) && x.stems[0] === stem)
  return it ? normPlace(it.place) : { ...CENTER }
}

/** Мастер трека: одна запись (новая заменяет прежнюю), весь трек. */
export function withMaster(list, engine, label = '') {
  const rest = (list || []).filter((it) => !it.master)
  return [...rest, {
    childId: ownId(), instId: 'master', from: 0, to: 0, lead: 0, beat: 0, db: 0,
    stems: [], fadeIn: 0, fadeOut: 0, keepHighHz: 0, master: true,
    engine: (engine || []).map((b) => ({ ...b })), label,
  }]
}

/** Строка о громкости после мастера: «−14,0 LUFS, пик −1,0 dBTP»; метрик нет — пусто. */
export function masterLine(metrics) {
  const m = metrics || {}
  if (typeof m.lufs !== 'number') return ''
  const f = (v) => v.toFixed(1).replace('.', ',').replace('-', '−')
  return typeof m.true_peak_db === 'number' ? `${f(m.lufs)} LUFS, пик ${f(m.true_peak_db)} dBTP` : `${f(m.lufs)} LUFS`
}

/** Цепочка мастера с целью и потолком: limiter (на своём месте, прочие параметры целы) получает target_lufs
 *  (0 — не менять) и ceiling_db; нет limiter, а цель задана или limiter нужен (потолок без смены громкости) —
 *  дописывается в конец; ни цели, ни limiter — limiter убирается.
 *  Исходная цепочка не меняется. */
export function masterChain(chain, { target = 0, ceiling = -1, limiter = false } = {}) {
  let out = (chain || []).map((b) => ({ ...b }))
  // ни цели, ни ограничителя — ограничитель убирается (галочка «ограничитель» снята)
  if (!target && !limiter) out = out.filter((b) => b.type !== 'limiter')
  const lim = out.findIndex((b) => b.type === 'limiter')
  if (lim >= 0) {
    out[lim] = { ...out[lim], target_lufs: target || 0, ceiling_db: ceiling }
  } else if (target || limiter) {
    out.push({ type: 'limiter', target_lufs: target || 0, ceiling_db: ceiling })
  }
  return out
}

/** «Микс без мастера» рядом с миксом студии (кладёт пересборка с мастером). */
export const premasterFile = (mix) => String(mix || '').replace('overdub-inst-', 'overdub-premaster-')
