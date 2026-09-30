// «Перепеть с места» по частям — синглтон в module scope (как useInserts):
// дубль-продолжение от источника голоса рендерится минуты, студию за это время
// закрывают. По готовности дубля его голос подставляется в версию трека только
// в окне части (Revoice), результат становится новой версией под «📎».
import { ref } from 'vue'
import { api } from '../api.js'
import { revoiceSpec } from '../vocalParts.js'

const KEY = 'yue_revoice_queue'
// [{parent, child, from, to, beat, voiceSrc, title}]
const pending = ref(load())

function load() {
  try { return JSON.parse(localStorage.getItem(KEY) || 'null') || [] } catch { return [] }
}
function save() {
  try { localStorage.setItem(KEY, JSON.stringify(pending.value)) } catch { /* без localStorage — в памяти */ }
}

function register(items) {
  pending.value = [...pending.value, ...items]
  save()
}

// один проход за раз; подстановки по одной (каждая качает стемы и гоняет ffmpeg)
let running = null
function tick() {
  if (!running) running = tickOnce().finally(() => { running = null })
  return running
}

async function tickOnce() {
  if (!pending.value.length) return
  let jobs
  try { jobs = await api.jobs() } catch { return }   // воркер недоступен — следующий тик
  const find = (id) => (jobs || []).find((x) => x.id === id)
  for (const it of pending.value) {
    const child = find(it.child)
    if (!child || child.status === 'error' || child.status === 'canceled' || !find(it.parent)) {
      it.dead = true
      continue
    }
    if (child.status !== 'done') continue
    try {
      const r = await api.rebuildSections(it.parent, [revoiceSpec(it.child, it.from, it.to, it.beat)])
      it.track = await api.variantToTrack(it.parent, r.variant.file, it.title, it.voiceSrc)
      it.done = true
    } catch (e) {
      // сеть/воркер моргнул — повтор на следующем тике; причину видно в консоли
      console.warn('revoice', it.child, e)
    }
  }
  pending.value = pending.value.filter((x) => !x.done && !x.dead)
  save()
}

setInterval(tick, 5000)

export function useRevoice() {
  return { pending, register, tick }
}
