// «Перепеть с места» по частям: чистая логика (тесты — vocalParts.test.js).

// приёмы, меняющие голос: только они уходят в план дубля «перепеть»
// (от дубля берётся лишь голос — музыкальные приёмы там бессмысленны)
export const revoiceSpecKinds = new Set(['octave', 'vocalUp', 'vocalVary'])

// источник голоса версии — рендер, чей голос в ней звучит (продолжения для
// «перепеть» берутся от него): voice_src версии; сгенерированный трек
// (роль не variant) — он сам; вариант без voice_src — источник родителя
export function voiceSource(job, jobsById) {
  const seen = new Set()
  let cur = job
  while (cur) {
    if (seen.has(cur.id)) return null   // цикл parent_id
    seen.add(cur.id)
    if (cur.voice_src) return cur.voice_src
    if (cur.role !== 'variant') return cur.id
    cur = cur.parent_id != null ? (jobsById || {})[cur.parent_id] : null
  }
  return null
}

// замена голоса в окне части голосом дубля (Go studio.SectionSpec, revoice):
// дубль — продолжение источника, его время совпадает со временем трека → lead = from
export function revoiceSpec(childId, from, to, beatSec) {
  return { child_id: childId, from, to, lead: from, beat_sec: beatSec, stems: ['vocals'], revoice: true }
}

// окно кончается там, где голос по плану молчит (иначе шов посреди фразы).
// bars — таймлайн ролла; такты голоса — с ключом Vocal в voices/rests
export function vocalEndsQuiet(bars, to) {
  const isVocal = (b) => [...Object.keys(b.voices || {}), ...Object.keys(b.rests || {})].some((k) => /vocal/i.test(k))
  const sings = (b) => Object.entries(b.voices || {}).some(([k, n]) => /vocal/i.test(k) && n > 0)
  const list = (bars || []).filter(isVocal)
  const end = Math.max(0, ...list.map((b) => b.end_sec))
  if (to >= end - 1e-6) return true
  const b = list.find((x) => x.start_sec <= to + 1e-6 && to < x.end_sec - 1e-6)
  return !b || !sings(b)
}
