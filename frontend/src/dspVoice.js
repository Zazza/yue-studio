// Голосовые цепочки эффектов (мегафон, телефон, перегруз, слэпбэк): примочки
// «на голос» приходят из Go с флагом voice. Такой цепочке эффект почти всегда
// нужен на дорожке «голос», а не на весь микс — дорожка выбирается сама.
// Чистая логика без DOM — покрывается vitest.

// Дорожка эффекта при выборе цепочки: голосовая цепочка без выбранной дорожки
// сама переключается на «голос», цепочка с ключом (ducking от барабанов) — на
// «прочее»; осознанный выбор пользователя не перекрывает.
export function voiceTarget(chain, cur) {
  if (chain && chain.voice && !cur) return 'vocals'
  if (needsStem(chain) && !cur) return 'other'
  return cur || ''
}

// Цепочка с дорожкой-ключом (key) работает только эффектом на дорожку: у
// всего микса ключа нет.
export function needsStem(chain) {
  return !!(chain && chain.key)
}

// Цепочке нужна сетка темпа (Ритм-гейт, дилей, статтер) — показать «найти сетку».
export function hasGrid(chain) {
  return !!(chain && (chain.params || []).some((p) => p.id === 'bpm'))
}

// Значения крутилок цепочки по умолчанию (у голосовых обязательна доля
// эффекта mix — сухой/обработанный).
export function chainDefaults(chain) {
  const p = {}
  if (chain) for (const prm of chain.params || []) p[prm.id] = prm.default
  return p
}
