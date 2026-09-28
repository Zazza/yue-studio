// Слежение за вклейками инструментов — синглтон в module scope.
// Жить должно НЕ в компоненте студии: пока очередь гоняет 3-4 джобы,
// пользователь закрывает студию — компонент умирает, и готовые мини-рендеры
// никто не микширует («пересборка вышла голой»). Этот сервис живёт всегда.
import { ref } from 'vue'
import { api } from '../api.js'
import { sliceAbc, TRICK_INSTRUMENTS } from '../abcEdit.js'

const KEY = 'yue_insert_queue'
const pending = ref(load())

function load() {
  try { return JSON.parse(localStorage.getItem(KEY) || '[]') } catch { return [] }
}
function save() {
  try { localStorage.setItem(KEY, JSON.stringify(pending.value)) } catch {}
}

// specs: [{parent, childId, instId, from, to, gain, srcJob}]
function register(specs) {
  pending.value = [...pending.value, ...specs]
  save()
}
function byParent(parentId) {
  return pending.value.filter((s) => s.parent === parentId)
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

let busy = false
async function tick() {
  if (busy || !pending.value.length) return
  busy = true
  try {
    const jobs = await api.jobs()
    const find = (id) => (jobs || []).find((x) => x.id === id)
    for (const spec of pending.value) {
      let child = find(spec.childId)
      if (!child && spec.instId) {
        const childId = await recreateChild(spec)
        if (childId) { spec.childId = childId; child = find(childId) }
      }
      if (!child) { spec.dead = true; continue }
      if (child.status === 'error' || child.status === 'canceled') { spec.dead = true; continue }
      if (child.status !== 'done') continue
      const dur = spec.to && spec.to > spec.from ? spec.to - spec.from : 0
      try {
        await api.mixInstrument(spec.parent, spec.childId, spec.from, dur, spec.gain ?? 0.5)
        spec.done = true
      } catch { /* сеть/воркер моргнул — попробуем на следующем тике */ }
    }
    pending.value = pending.value.filter((s) => !s.done && !s.dead)
    save()
  } catch { /* jobs недоступны — на следующем тике */ } finally { busy = false }
}

setInterval(tick, 3000)

export function useInserts() {
  return { pending, register, byParent }
}
