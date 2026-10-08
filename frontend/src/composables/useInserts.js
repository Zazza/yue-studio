// Слежение за вклейками инструментов (замены дорожек) — синглтон в module scope.
// Жить должно НЕ в компоненте студии: пока очередь гоняет 3-4 джобы,
// пользователь закрывает студию — компонент умирает, и готовые рендеры
// кусков никто не микширует («пересборка вышла голой»). Этот сервис живёт всегда.
import { ref } from 'vue'
import { api } from '../api.js'
import { sectionRequest, TRICK_INSTRUMENTS } from '../abcEdit.js'
import { clampDb, INSERT_DEFAULT_DB } from '../insertMix.js'

const KEY = 'yue_insert_queue'
// применённые вклейки по трекам: {parentId: [{childId, instId, from, to, lead,
// beat, db, stems, fadeIn, fadeOut, aligned, score}]} — из них трек
// пересобирается с чистого оригинала
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

// specs: [{parent, childId, instId, from, to, lead, beat, db, stems, fadeIn, fadeOut, srcJob}]
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
// сколько пересборок трека ждут или идут: пока их больше нуля, файл микса ещё старый —
// студия не даёт его слушать как «микс с правками» и делать из него версию
const building = ref({})
function rebuild(parentId) {
  building.value = { ...building.value, [parentId]: (building.value[parentId] || 0) + 1 }
  const run = chain.then(() => doRebuild(parentId)).finally(() => {
    building.value = { ...building.value, [parentId]: building.value[parentId] - 1 }
  })
  chain = run.catch(() => {})
  return run
}
const isBuilding = (parentId) => (building.value[parentId] || 0) > 0
async function doRebuild(parentId) {
  // выключенные записи остаются в реестре, но в пересборку не идут; активных нет — звучит оригинал
  const list = appliedFor(parentId).filter((it) => !it.off)
  if (!list.length) return { empty: true }
  const r = await api.rebuildSections(parentId, list.map((it) => ({
    child_id: it.childId > 0 ? it.childId : 0, from: it.from, to: it.to,
    // у «громкости дорожек» (childId ≤ 0) db без зажима: −100 — заглушить
    lead: it.lead || 0, beat_sec: it.beat || 0, db: it.childId > 0 ? clampDb(it.db) : it.db,
    stems: it.stems || [], fade_in: it.fadeIn || 0, fade_out: it.fadeOut || 0,
    keep_high_hz: it.keepHighHz || 0,
    // эффект на дорожку: цепочка и её крутилки
    ...(it.chain ? { chain: it.chain, params: it.params || {} } : {}),
    // доска педалей на дорожку: цепочка эффектов по порядку
    ...(it.steps ? { steps: it.steps } : {}),
    // линия громкости дорожки (по волне): точки {t, db} по всему треку
    ...(it.envelope ? { envelope: it.envelope } : {}),
    // цепочка звукового движка воркера: JSON-блоки как у fx_apply
    ...(it.engine ? { engine: it.engine } : {}),
    // добавление поверх трека (синт): исходная дорожка не вычитается
    ...(it.add ? { add: true } : {}),
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
// перенос правок на новую версию трека (пересборка): правки без рендера (эффекты, педали, движок,
// громкость, линии; childId ≤ 0) — сразу в реестр новой версии как есть (с off): в очереди тик не
// нашёл бы их джобу и потерял. Вклейки — через очередь: применённые с текущей громкостью,
// ещё рендерящиеся — как есть
function carryTo(fromParent, toParent, srcJob) {
  const all = appliedFor(fromParent).map(({ aligned: _a, score: _s, ...it }) => it)
  const own = all.filter((it) => !(it.childId > 0))
  if (own.length) {
    const have = new Set(appliedFor(toParent).map((it) => it.childId))
    const add = own.filter((it) => !have.has(it.childId)).map((it) => JSON.parse(JSON.stringify(it)))
    applied.value = { ...applied.value, [toParent]: [...appliedFor(toParent), ...add] }
  }
  const done = all.filter((it) => it.childId > 0)
  // вклейка может быть и в реестре, и в очереди (tick ждёт её пересборку):
  // берём одну — из реестра, там актуальная громкость
  const inRegistry = new Set(done.map((it) => it.childId))
  const waiting = byParent(fromParent).filter((s) => !inRegistry.has(s.childId) && s.childId > 0)
    .map(({ parent: _p, done: _d, dead: _x, ...it }) => it)
  register([...done, ...waiting].map((it) => ({ ...it, parent: toParent, srcJob })))
}

// файл свежего микса: Go называет его по последней активной вклейке реестра
function latestFile(parentId) {
  const list = appliedFor(parentId).filter((it) => !it.off && it.childId > 0)   // «заглушить» файла не именует
  return list.length ? `overdub-inst-${list[list.length - 1].childId}.flac` : null
}
// микс с правками для «▶ микс» студии: без вклеек (одни эффекты/громкости) Go называет его «-0»;
// активных записей нет — микса нет, звучит оригинал
function mixFile(parentId) {
  if (!appliedFor(parentId).some((it) => !it.off)) return null
  return latestFile(parentId) || 'overdub-inst-0.flac'
}

// выбрать один из вариантов рендера вклейки (alts) и пересобрать трек
async function selectAlt(parentId, childId, altId) {
  const it = appliedFor(parentId).find((x) => x.childId === childId)
  if (!it || !(it.alts || []).includes(altId)) return null
  it.childId = altId
  applied.value = { ...applied.value, [parentId]: [...appliedFor(parentId)] }
  save()
  return rebuild(parentId)
}

// «заглушить» (приём без рендера): сразу в реестр и пересборка. childId у
// таких записей — отрицательная метка времени (уникальна, в api уходит 0)
async function addMute(parentId, item) {
  return addMutes(parentId, [item])
}
// несколько «громкостей дорожек» разом — одна пересборка (кнопка «куплеты реже»)
let muteSeq = 0
async function addMutes(parentId, items) {
  const added = items.map(({ instId, from, to, stems, db = -100 }) => ({
    childId: -(Date.now() * 100 + (muteSeq++ % 100)), instId, from, to, lead: 0, beat: 0, db, stems,
    fadeIn: 0, fadeOut: 0, keepHighHz: 0,
  }))
  applied.value = { ...applied.value, [parentId]: [...appliedFor(parentId), ...added] }
  save()
  return rebuild(parentId)
}

// эффект на дорожку (стем) в окне: запись реестра как у «громкости дорожек»
// (childId < 0, в api уходит 0) + цепочка; копится вместе с вклейками
async function addStemFx(parentId, { stem, chain, params, from = 0, to = 0 }) {
  const item = {
    childId: -(Date.now() * 100 + (muteSeq++ % 100)), instId: 'fx-' + chain, from, to, lead: 0, beat: 0, db: 0,
    stems: [stem], fadeIn: 0, fadeOut: 0, keepHighHz: 0, chain, params: { ...(params || {}) },
  }
  applied.value = { ...applied.value, [parentId]: [...appliedFor(parentId), item] }
  save()
  return rebuild(parentId)
}

// доска педалей на дорожку в окне: одна запись реестра со списком шагов
// (эффекты по порядку, а не сумма отдельных); label — подпись в списке вставок
async function addStemPedals(parentId, { stem, steps, from = 0, to = 0, label = '' }) {
  const item = {
    childId: -(Date.now() * 100 + (muteSeq++ % 100)), instId: 'pedals', from, to, lead: 0, beat: 0, db: 0,
    stems: [stem], fadeIn: 0, fadeOut: 0, keepHighHz: 0, steps: (steps || []).map((s) => ({ ...s })), label,
  }
  applied.value = { ...applied.value, [parentId]: [...appliedFor(parentId), item] }
  save()
  return rebuild(parentId)
}

// цепочка звукового движка воркера на дорожку в окне: одна запись реестра,
// считается на воркере при пересборке; label — подпись в списке вставок
// add — добавить кусок поверх трека, исходную дорожку не вычитать (синт-партия на «mix» — без разделения)
async function addStemEngine(parentId, { stem, chain, from = 0, to = 0, label = '', add = false }) {
  const item = {
    childId: -(Date.now() * 100 + (muteSeq++ % 100)), instId: 'engine', from, to, lead: 0, beat: 0, db: 0,
    stems: [stem], fadeIn: 0, fadeOut: 0, keepHighHz: 0, engine: (chain || []).map((b) => ({ ...b })), label,
    ...(add ? { add: true } : {}),
  }
  applied.value = { ...applied.value, [parentId]: [...appliedFor(parentId), item] }
  save()
  return rebuild(parentId)
}

// несколько цепочек движка разом («ритм-секция набором») — одна пересборка
async function addStemEngines(parentId, items) {
  if (!(items || []).length) return null
  const added = items.map(({ stem, chain, from = 0, to = 0, label = '' }) => ({
    childId: -(Date.now() * 100 + (muteSeq++ % 100)), instId: 'engine', from, to, lead: 0, beat: 0, db: 0,
    stems: [stem], fadeIn: 0, fadeOut: 0, keepHighHz: 0, engine: (chain || []).map((b) => ({ ...b })), label,
  }))
  applied.value = { ...applied.value, [parentId]: [...appliedFor(parentId), ...added] }
  save()
  return rebuild(parentId)
}

// линия громкости дорожки (по волне): одна запись на дорожку — повторная
// заменяет прежнюю, пустая — убирает; эффекты и вклейки не трогаются
async function addStemEnvelope(parentId, { stem, envelope }) {
  const rest = appliedFor(parentId).filter((x) => !(x.envelope && (x.stems || [])[0] === stem))
  const item = envelope && envelope.length ? [{
    childId: -(Date.now() * 100 + (muteSeq++ % 100)), instId: 'env', from: 0, to: 0, lead: 0, beat: 0, db: 0,
    stems: [stem], fadeIn: 0, fadeOut: 0, keepHighHz: 0, envelope: envelope.map(({ t, db }) => ({ t, db })),
  }] : []
  applied.value = { ...applied.value, [parentId]: [...rest, ...item] }
  save()
  return rebuild(parentId)
}

// выключить/вернуть запись: остаётся в реестре (и в localStorage), пересборка без неё
async function setOff(parentId, childId, off) {
  const list = appliedFor(parentId)
  const it = list.find((x) => x.childId === childId)
  if (!it) return null
  it.off = !!off
  applied.value = { ...applied.value, [parentId]: [...list] }
  save()
  return rebuild(parentId)
}

// удалить запись; ожидающая спека той же вклейки уходит из очереди — иначе готовый рендер
// вернул бы её в реестр. Записи не было — пересборки нет
async function remove(parentId, childId) {
  const before = pending.value.length
  pending.value = pending.value.filter((s) => !(s.parent === parentId && s.childId === childId))
  const list = appliedFor(parentId)
  const rest = list.filter((x) => x.childId !== childId)
  if (rest.length === list.length) {
    if (pending.value.length !== before) save()
    return null
  }
  applied.value = { ...applied.value, [parentId]: rest }
  save()
  return rebuild(parentId)
}

// новая цепочка у записи движка: окно и дорожка прежние, подпись — новая, если дана
async function replaceEngine(parentId, childId, { chain, label } = {}) {
  const list = appliedFor(parentId)
  const it = list.find((x) => x.childId === childId)
  if (!it || !it.engine) throw new Error('это не запись звукового движка')
  it.engine = (chain || []).map((b) => ({ ...b }))
  if (label) it.label = label
  applied.value = { ...applied.value, [parentId]: [...list] }
  save()
  return rebuild(parentId)
}

async function setDb(parentId, childId, db) {
  const it = appliedFor(parentId).find((x) => x.childId === childId)
  if (!it) return null
  it.db = clampDb(db)
  save()
  return rebuild(parentId)
}

async function recreateChild(spec, src) {
  // рендер куска удалён из очереди — ставим такой же по плану исходной джобы
  const inst = TRICK_INSTRUMENTS.find((i) => i.id === spec.instId)
  if (!inst || !src) return null
  try {
    const abc = await api.jobAbcText(src.id, src.abc_file || 'score.abc')
    const req = sectionRequest(src, abc, inst, spec.from, spec.to ?? spec.from + 15)
    if (!req.abc.includes('|')) return null
    return api.submit(req)
  } catch { return null }  // воркер/план недоступны — спека уйдёт в dead ниже
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
      let child = find(spec.childId)
      if (!child && spec.instId) {
        const childId = await recreateChild(spec, find(spec.srcJob))
        if (childId) { spec.childId = childId; child = find(childId) }
      }
      if (!child) { spec.dead = true; continue }
      if (child.status === 'error' || child.status === 'canceled') { spec.dead = true; continue }
      if (child.status !== 'done') continue
      // спеку убрали (remove) пока тик ждал воркер — в реестр её не возвращаем
      if (!pending.value.includes(spec)) continue
      // повтор после сбоя пересборки: запись уже в реестре — не трогаем её
      // (громкость с ползунка и порядок сохраняются)
      const list = appliedFor(spec.parent)
      const prev = spec.replaces && list.find((x) => x.childId === spec.replaces)
      if (prev) {
        // «ещё вариант»: новый рендер становится текущим, прежние — в alts
        prev.alts = [...new Set([...(prev.alts || [prev.childId]), spec.childId])]
        prev.childId = spec.childId
        applied.value = { ...applied.value, [spec.parent]: [...list] }
      } else if (!list.some((x) => x.childId === spec.childId)) {
        applied.value = { ...applied.value, [spec.parent]: [...list, {
          childId: spec.childId, instId: spec.instId, from: spec.from,
          to: spec.to ?? spec.from + 15, lead: spec.lead || 0, beat: spec.beat || 0,
          db: spec.db ?? INSERT_DEFAULT_DB,
          stems: spec.stems || [], fadeIn: spec.fadeIn || 0, fadeOut: spec.fadeOut || 0,
          keepHighHz: spec.keepHighHz || 0, off: !!spec.off,
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
  return { pending, applied, register, byParent, appliedFor, rebuild, setDb, setOff, remove, replaceEngine, selectAlt, addMute, addMutes, addStemFx, addStemPedals, addStemEngine, addStemEngines, addStemEnvelope, carryTo, flush, latestFile, mixFile, isBuilding }
}
