// Группы готовых цепочек (поле group в fxPresets.js): подписи и раскладка списка по группам для пульта
// дорожек и страницы «Инструменты». Чистая логика без DOM.

export const PRESET_GROUPS = {
  'guitar-amp': { ru: 'Гитара через усилитель (нужен захват NAM)', en: 'Guitar through an amp (needs a NAM capture)' },
  'guitar-clean': { ru: 'Гитара: чистые', en: 'Guitar: clean' },
  'guitar-drive': { ru: 'Гитара: перегруз', en: 'Guitar: drive' },
  'guitar-space': { ru: 'Гитара: пространство и эффекты', en: 'Guitar: space and effects' },
  'bass-tone': { ru: 'Бас: звук (ноты те же)', en: 'Bass: tone (same notes)' },
  'bass-kit': { ru: 'Бас: замена нот сэмплами', en: 'Bass: notes replaced with samples' },
  'drum-kits': { ru: 'Барабаны: наборы', en: 'Drums: kits' },
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
