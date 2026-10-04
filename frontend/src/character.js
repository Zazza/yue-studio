// Характер исполнения трека: температура (смелость игры) и cfg (точность по
// стилю и нотам). Слепое сравнение на плане #490: выше температура — игра
// оригинальнее; выше cfg — чётче, «по нотам», чище. Не «лучше», а другой
// характер — поэтому ручки, а не новый дефолт.

export const CHARACTER = {
  temperature: { min: 0.7, max: 1.4, step: 0.05, def: 1.0 },
  cfg: { min: 1.0, max: 3.5, step: 0.1, def: 1.5 },
}

// значение ручки → число для отправки или null (по умолчанию / мусор)
function knob(v, spec) {
  if (v === '' || v === null || v === undefined) return null
  const n = Number(v)
  if (!Number.isFinite(n)) return null
  const clamped = Math.min(spec.max, Math.max(spec.min, n))
  const stepped = Math.round(clamped / spec.step) * spec.step
  // округление до шага без хвостов float (1.15, а не 1.1500000000000001)
  const r = Number(stepped.toFixed(2))
  return r === spec.def ? null : r
}

// поля для submit: значение по умолчанию не отправляется — воркер решает сам
export function characterPayload(temperature, cfg) {
  const out = {}
  const t = knob(temperature, CHARACTER.temperature)
  const c = knob(cfg, CHARACTER.cfg)
  if (t !== null) out.temperature = t
  if (c !== null) out.cfg = c
  return out
}

// подпись на карточке трека: какой характер выбран (0/нет — пусто)
export function characterLabel(job) {
  const parts = []
  if (job && Number(job.temperature) > 0) parts.push('смелость ' + Number(job.temperature))
  if (job && Number(job.cfg) > 0) parts.push('точность ' + Number(job.cfg))
  return parts.join(' · ')
}
