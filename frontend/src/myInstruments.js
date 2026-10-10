// Свои инструменты (страница «Инструменты»; выбор — и в студии): цепочка движка под своим именем на основе готовой.
// Чистая логика без DOM: общий список «готовые + свои» и тело для воркера (/fx/instruments).

export const EXTRA_KEYS = ['style', 'octave', 'pattern', 'swing', 'accent', 'amp_hint', 'place']

// готовые как есть, затем свои: groupPresets сохраняет порядок внутри группы — свой встаёт внизу группы готовой,
// а группа, которой у готовых нет, — отдельной группой в конце
export function mergeInstruments(builtin, mine) {
  const byId = new Map((builtin || []).map((p) => [p.id, p]))
  const own = (mine || []).map((m) => {
    const base = byId.get(m.base)
    const extra = {}
    for (const k of EXTRA_KEYS) if (m.extra && m.extra[k] !== undefined) extra[k] = m.extra[k]
    return {
      ...extra,
      id: `my-${m.id}`, mine: true, wid: m.id, base: m.base || '',
      name: { ru: `${m.name} (мой)`, en: `${m.name} (mine)` }, title: m.name,
      note: base
        ? { ru: `свой, на основе «${base.name.ru}»`, en: `own, based on “${base.name.en || base.name.ru}”` }
        : { ru: 'свой', en: 'own' },
      group: m.group || '', stems: m.stems || [], chain: m.chain || [],
    }
  })
  return [...(builtin || []), ...own]
}

// тело для воркера: на основе готовой — её id; свой пересохраняется с прежней основой
export function instrumentFromPreset(p, chain, name) {
  const extra = {}
  for (const k of EXTRA_KEYS) if (p && p[k] !== undefined) extra[k] = p[k]
  return {
    name, base: p ? (p.mine ? p.base || '' : p.id) : '', group: (p && p.group) || '',
    stems: (p && p.stems) || [], chain, extra,
  }
}
