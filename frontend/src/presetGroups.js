// Группы готовых цепочек (поле group в fxPresets.js): подписи и раскладка списка по группам для пульта
// дорожек и страницы «Инструменты». Чистая логика без DOM.

export const PRESET_GROUPS = {
  'guitar-amp': { ru: 'Гитара через усилитель (нужен захват NAM)', en: 'Guitar through an amp (needs a NAM capture)' },
  'guitar-clean': { ru: 'Гитара: чистые', en: 'Guitar: clean' },
  'guitar-drive': { ru: 'Гитара: перегруз', en: 'Guitar: drive' },
  'guitar-space': { ru: 'Гитара: пространство и эффекты', en: 'Guitar: space and effects' },
  'bass-tone': { ru: 'Бас: звук (ноты те же)', en: 'Bass: tone (same notes)' },
  'bass-genre': { ru: 'Бас: под жанр', en: 'Bass: by genre' },
  'bass-kit': { ru: 'Бас: замена нот сэмплами', en: 'Bass: notes replaced with samples' },
  'drum-kits': { ru: 'Барабаны: наборы', en: 'Drums: kits' },
  'synth-pad': { ru: 'Пэды и струнные', en: 'Pads and strings' },
  'synth-organ': { ru: 'Органы', en: 'Organs' },
  'synth-keys': { ru: 'Клавиши', en: 'Keys' },
  'synth-lead': { ru: 'Лиды и басы', en: 'Leads and basses' },
  'synth-toy': { ru: 'Игрушки и колокольчики', en: 'Toys and bells' },
  perc: { ru: 'Перкуссия по сетке', en: 'Percussion on the grid' },
  'orch-strings': { ru: 'Струнные', en: 'Strings' },
  'orch-winds': { ru: 'Деревянные духовые', en: 'Woodwinds' },
  'orch-brass': { ru: 'Медь', en: 'Brass' },
  'orch-mallets': { ru: 'Колокольчики и маримба', en: 'Mallets' },
  'orch-bass': { ru: 'Бас → оркестр', en: 'Bass → orchestra' },
  'orch-lead': { ru: 'Мелодия → оркестр', en: 'Melody → orchestra' },
}

/** Список по группам: [{group, label, items}] в порядке первого появления группы, внутри — исходный порядок;
 *  пресет без group — в группе '' с пустой подписью. */
export function groupPresets(list, locale = 'ru') {
  const out = []
  const at = new Map()
  for (const p of list || []) {
    const g = p.group || ''
    if (!at.has(g)) {
      const l = PRESET_GROUPS[g]
      at.set(g, out.length)
      out.push({ group: g, label: l ? (l[locale] || l.ru) : '', items: [] })
    }
    out[at.get(g)].items.push(p)
  }
  return out
}

// Страница «Инструменты» (этап 12): два списка вместо стены кнопок — группа и инструмент в ней.
// groupOptions — пункты списка групп по порядку groupPresets; группа без подписи — firstLabel.
export function groupOptions(groups, firstLabel = '') {
  return (groups || []).map((g) => ({ value: g.group, label: g.label || firstLabel }))
}

// itemOptions — пункты выбранной группы (элемент groupPresets): {value: id, label, title} на языке locale
export function itemOptions(group, locale = 'ru') {
  const tr = (l) => (l ? l[locale] || l.ru || '' : '')
  return ((group && group.items) || []).map((p) => ({ value: p.id, label: tr(p.name), title: tr(p.note) }))
}

// groupOf — группа, где лежит пресет id; нет такого — первая; групп нет — ''
export function groupOf(groups, id) {
  const list = groups || []
  const g = list.find((x) => x.items.some((p) => p.id === id))
  return g ? g.group : list.length ? list[0].group : ''
}
