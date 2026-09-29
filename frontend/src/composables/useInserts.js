// Слежение за вклейками инструментов — синглтон в module scope.
// Жить должно НЕ в компоненте студии: пока очередь гоняет 3-4 джобы,
// пользователь закрывает студию — компонент умирает, и готовые мини-рендеры
// никто не микширует («пересборка вышла голой»). Этот сервис живёт всегда.
import { ref } from 'vue'
import { api } from '../api.js'
import { sliceAbc, TRICK_INSTRUMENTS } from '../abcEdit.js'
import { clampDb, INSERT_DEFAULT_DB } from '../insertMix.js'

const KEY = 'yue_insert_queue'
// применённые вклейки по трекам: {parentId: [{childId, instId, from, to, lead,
// beat, db, aligned, score}]} — из них трек пересобирается с чистого оригинала
const APPLIED_KEY = 'yue_insert_applied'
const pending = ref(load(KEY, []))
const applied = ref(load(APPLIED_KEY, {}))

function load(key, dflt) {
  try { return JSON.parse(localStorage.getItem(key) || 'null') || dflt } catch { return dflt }
}
function save() {
  try {
    localStorage.setItem(KEY, JSON.stringify(pending.value))
    localStorage.setItem(APPLIED_KEY, JSON.stringify(applied.value))
  } catch { /* localStorage недоступен — живём в памяти до перезапуска */ }
}

// specs: [{parent, childId, instId, from, to, lead, beat, db, srcJob}]
function register(specs) {
  pending.value = [...pending.value, ...specs]
  save()
}
function byParent(parentId) {
  return pending.value.filter((s) => s.parent === parentId)
}
function appliedFor(parentId) {
  return applied.value[parentId] || []
}

// пересборки идут по одной: параллельные загрузки перетирали бы друг друга
let chain = Promise.resolve()
function rebuild(parentId) {
  const run = chain.then(() => doRebuild(parentId))
  chain = run.catch(() => {})
  return run
}
async function doRebuild(parentId) {
  const list = appliedFor(parentId)
  if (!list.length) return null
  const r = await api.rebuildInserts(parentId, list.map((it) => ({
    child_id: it.childId, from: it.from, to: it.to,
    lead: it.lead || 0, beat_sec: it.beat || 0, db: clampDb(it.db),
  })))
  // отчёт Go: встала ли вклейка по бочке или по плану (UI предупреждает).
  // Пишем в АКТУАЛЬНЫЙ реестр, а не в снимок до await: пока шла пересборка,
  // tick мог добавить готовую вклейку — снимок её бы затёр
  const cur = appliedFor(parentId)
  for (const rep of (r && r.inserts) || []) {
    const it = cur.find((x) => x.childId === rep.child_id)
    if (it) Object.assign(it, { aligned: rep.aligned, score: rep.score })
  }
  applied.value = { ...applied.value, [parentId]: [...cur] }
  save()
  return r
}
// перенос вклеек на новую версию трека (пересборка): применённые — с текущей
// громкостью, ещё рендерящиеся — как есть; вокальная перелепка не переносится
function carryTo(fromParent, toParent, srcJob) {
  const done = appliedFor(fromParent).map(({ aligned: _a, score: _s, ...it }) => it)
  // вклейка может быть и в реестре, и в очереди (tick ждёт её пересборку):
  // берём одну — из реестра, там актуальная громкость
  const inRegistry = new Set(done.map((it) => it.childId))
  const waiting = byParent(fromParent)
    .filter((s) => s.mode !== 'vocal-restyle' && !inRegistry.has(s.childId))
    .map(({ parent: _p, done: _d, dead: _x, ...it }) => it)
  register([...done, ...waiting].map((it) => ({ ...it, parent: toParent, srcJob })))
}

// файл свежего микса: Go называет его по последней вклейке реестра
function latestFile(parentId) {
  const list = appliedFor(parentId)
  return list.length ? `overdub-inst-${list[list.length - 1].childId}.flac` : null
}

async function setDb(parentId, childId, db) {
  const it = appliedFor(parentId).find((x) => x.childId === childId)
  if (!it) return null
  it.db = clampDb(db)
  save()
  return rebuild(parentId)
}

async function recreateChild(spec) {
  // мини-рендер удалён из очереди — режем такой же из плана исходной джобы
  const inst = TRICK_INSTRUMENTS.find((i) => i.id === spec.instId)
  if (!inst || !spec.srcJob) return null
  try {
    const abc = await api.jobAbcText(spec.srcJob, 'score.abc')
    const plan = sliceAbc(abc, spec.from, spec.to ?? spec.from + 15, 1)
    if (!plan.includes('|')) return null
    return api.submit({
      title: 'вклейка · ' + inst.id,
      style: `solo ${inst.en}, sparse quiet ${inst.en} line, no drums, no vocals`,
      lyrics: '[Instrumental]',
      seed: Math.floor(Math.random() * 1e9),
      cot: 'melody',
      abc: plan,
      draft: ((spec.to ?? spec.from + 15) - spec.from) < 15,
    })
  } catch { return null }
}

// один проход очереди за раз: таймер и flush() (кнопка ▶ студии) делят его
let running = null
function tick() {
  if (!running) running = tickOnce().finally(() => { running = null })
  return running
}
// немедленно обработать готовые партии (идущий проход — дождаться и пройти ещё
// раз). Список джоб не получен — ошибка вызывающему: ▶ покажет её, а не сыграет
// прошлый микс без новой вклейки. Таймерный проход результат игнорирует.
async function flush() {
  if (running) await running
  const res = await tick()
  if (res && res.error) throw res.error
}

async function tickOnce() {
  if (!pending.value.length) return
  try {
    const jobs = await api.jobs()
    const find = (id) => (jobs || []).find((x) => x.id === id)
    for (const spec of pending.value) {
      if (spec.mode === 'vocal-restyle') {
        // вокальная перелепка: инструментальный ререндер готов → родной
        // вокал (stem-vocals родителя) поверх нового аккомпанемента
        const child = find(spec.childId)
        if (child && child.status === 'done') {
          try {
            await api.mixVocalsOver(spec.childId, spec.parent)
            spec.done = true
          } catch { /* повторим на следующем тике */ }
        }
        continue
      }
      let child = find(spec.childId)
      if (!child && spec.instId) {
        const childId = await recreateChild(spec)
        if (childId) { spec.childId = childId; child = find(childId) }
      }
      if (!child) { spec.dead = true; continue }
      if (child.status === 'error' || child.status === 'canceled') { spec.dead = true; continue }
      if (child.status !== 'done') continue
      // повтор после сбоя пересборки: запись уже в реестре — не трогаем её
      // (громкость с ползунка и порядок сохраняются)
      const list = appliedFor(spec.parent)
      if (!list.some((x) => x.childId === spec.childId)) {
        applied.value = { ...applied.value, [spec.parent]: [...list, {
          childId: spec.childId, instId: spec.instId, from: spec.from,
          to: spec.to ?? spec.from + 15, lead: spec.lead || 0, beat: spec.beat || 0,
          db: spec.db ?? INSERT_DEFAULT_DB,
        }] }
      }
      try {
        await rebuild(spec.parent)
        spec.done = true
      } catch { /* сеть/воркер моргнул — вклейка уже в реестре, пересборка на следующем тике */ }
    }
    pending.value = pending.value.filter((s) => !s.done && !s.dead)
    save()
  } catch (error) {
    return { error }   // jobs недоступны: таймер подождёт следующего тика, flush сообщит
  }
}

setInterval(tick, 3000)

export function useInserts() {
  return { pending, applied, register, byParent, appliedFor, rebuild, setDb, carryTo, flush, latestFile }
}
