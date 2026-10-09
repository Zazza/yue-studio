// Поиск по пунктам селекта: пункт подходит, если в его подписи есть каждое
// слово запроса (подстрокой, без учёта регистра, «ё» = «е»). Пустой запрос —
// все пункты; порядок не меняется.
const norm = (s) => String(s ?? '').toLowerCase().replace(/ё/g, 'е')

export function matchOptions(options, query) {
  const words = norm(query).split(/\s+/).filter(Boolean)
  if (!words.length) return [...options]
  return options.filter((o) => {
    const label = norm(o.label) + (o.search ? ' ' + norm(o.search) : '')   // search — доп. текст поиска (семья, течение)
    return words.every((w) => label.includes(w))
  })
}
