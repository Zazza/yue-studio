// Звуковой движок из интерфейса без Vue: докачка наборов сэмплов и «применить цепочку» (короткое
// превью, затем запись в реестр пересборки). Вызывают EngineBox и InstrumentsPage.
import RU from './i18n/ru.js'
import { missingKits } from './fxChain.js'

export const PREVIEW_SEC = 10 // проверка цепочки перед записью — короткий кусок

const copy = (v) => JSON.parse(JSON.stringify(v)) // цепочка — данные JSON: копия без общих объектов

/** Наборы цепочки (kit/kit_open у sampler, kit у bass), которых нет среди kits воркера, — ставятся
 *  по одному, в порядке цепочки; onKit(имя) — перед установкой (подпись «качаю набор…»). */
export async function ensureKits(api, chain, kits, onKit) {
  const need = missingKits(chain, kits)
  for (const k of need) {
    if (onKit) onKit(k)
    await api.installFxKit(k)
  }
  return need
}

/** Применить цепочку движка к дорожке: снимок {jobId, dur, src, from, to, chain, label} берётся до
 *  первого await — пока качается набор, дорожку, пресет и трек могут сменить, а превью и запись
 *  должны быть про одно и то же. Старый воркер (нет превью) — ошибка: запрос сделал бы вариант,
 *  а такую запись пересборка отвергла бы. Возврат — запись для useInserts.addStemEngine.
 *  opts.oldMsg — текст ошибки «обновите воркер» на языке интерфейса; opts.onKit — как у ensureKits. */
export async function applyEngine(api, snap, opts = {}) {
  const s = { ...snap, chain: copy(snap.chain || []) }
  const oldMsg = opts.oldMsg || RU['instr.engine.old']
  const cfg = await api.workerConfig()
  if (!cfg || !cfg.fx_preview) throw new Error(oldMsg)
  await ensureKits(api, s.chain, ((await api.fxAssets()) || {}).kits, opts.onKit)
  // запись с цепочкой, которую воркер не примет (движок выключен, нет захвата, нет дорожки),
  // роняла бы каждую следующую пересборку — сначала короткое превью того же
  const end = s.to > s.from ? Math.min(s.to, s.from + PREVIEW_SEC) : Math.min(s.from + PREVIEW_SEC, s.dur || s.from + PREVIEW_SEC)
  const r = await api.applyFx(s.jobId, { source: s.src, chain: s.chain, from: s.from, to: end, output: 'solo', preview: true })
  if (!r || !String(r.file || '').startsWith('preview-fx-')) throw new Error(oldMsg)
  return { stem: s.src, chain: copy(s.chain), from: s.from, to: s.to, label: s.label }
}
