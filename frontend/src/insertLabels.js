// Понятные подписи результатов вклеек. Файл микса overdub-inst-<N>.flac назван
// по номеру рендера куска N — «вклейка инструмента #191» ничего не говорила.
// Подпись берём из реестра вклеек (инструмент + окно), для старых файлов вне
// реестра — из названия рендера («… · drumfill» / «… · сбивка»).
import { TRICK_INSTRUMENTS } from './abcEdit.js'

// роли производных треков: результаты слушают, материал — сырьё для вклеек
export const RESULT_ROLES = ['variant', 'rebuild', 'continue', 'voice']

export function isResultJob(j) {
  return RESULT_ROLES.includes(j.role)
}

// id инструмента по названию рендера куска: хвост после « · » — id или подпись
export function instIdFromTitle(title, labelOf) {
  for (const part of String(title || '').split(' · ').slice(1).map((x) => x.trim())) {
    const hit = TRICK_INSTRUMENTS.find((i) => i.id === part || labelOf(i.id) === part)
    if (hit) return hit.id
  }
  return null
}

// номер рендера из имени файла микса; не вклейка — null
export function mixChildId(file) {
  const m = String(file || '').match(/^overdub-inst-(\d+)\.flac$/)
  return m ? Number(m[1]) : null
}

// подпись микса: «сбивка 3:24–3:28 + электрогитара 4:00–4:16».
// applied — вклейки родителя из реестра; jobs — список треков (для старых файлов)
export function mixLabel(file, { applied = [], jobs = [], labelOf, fmt }) {
  const id = mixChildId(file)
  if (id == null) return null
  const inReg = applied.some((it) => it.childId === id || (it.alts || []).includes(id))
  if (inReg) {
    return applied.map((it) => `${labelOf(it.instId)} ${fmt(it.from)}–${fmt(it.to)}`).join(' + ')
  }
  const job = jobs.find((j) => j.id === id)
  const inst = job && instIdFromTitle(job.title, labelOf)
  return inst ? labelOf(inst) : null
}

// строка реестра вклеек: у эффекта на дорожку — «эффект · дорожка», у вклейки/
// заглушки — название приёма
export function insertTitle(it, { chainName, stemName, instName }) {
  if (it.chain) return `${chainName(it.chain)} · ${(it.stems || []).map(stemName).join(', ')}`
  // цепочка звукового движка: подпись записи или типы блоков по порядку
  if (it.engine) {
    const what = it.label || 'Движок: ' + it.engine.map((b) => b.type).join(' → ')
    return `${what} · ${(it.stems || []).map(stemName).join(', ')}`
  }
  // доска педалей: подпись набора/доски или названия педалей по порядку
  if (it.steps) {
    const what = it.label || it.steps.filter((s) => !s.off).map((s) => chainName(s.chain)).join(' → ')
    return `${what} · ${(it.stems || []).map(stemName).join(', ')}`
  }
  return instName(it.instId)
}

// окно записи: to ≤ 0 — «до конца», весь трек — отдельной подписью
export function insertWindow(it, { fmt, toEnd, whole }) {
  if (!(it.to > 0)) return it.from > 0 ? `${fmt(it.from)}–${toEnd}` : whole
  return `${fmt(it.from)}–${fmt(it.to)}`
}
