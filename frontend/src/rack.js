// Инструментальная стойка: карточки по группам, выбор разворачивается
// в текст стиля автоматически — словесная кухня спрятана.
// rackSel: [{ id, effect }] — входит в параметры генерации.

export const rackGroups = [
  {
    id: 'rhythm', name: 'Ритм-секция',
    items: [
      { id: 'drums-tight', name: 'Барабаны: сухой панч', phrasing: 'tight dry punchy drums, close-miked' },
      { id: 'drums-room', name: 'Барабаны: комнатный', phrasing: 'roomy live drums, natural bleed' },
      { id: 'drums-machine', name: 'Драм-машина', phrasing: 'cheap drum machine, rigid electronic beat' },
      { id: 'bass-drive', name: 'Бас: напористый', phrasing: 'driving picked bass, forward in mix' },
      { id: 'bass-deep', name: 'Бас: глубокий', phrasing: 'deep round bass, subdued' },
    ],
  },
  {
    id: 'guitars', name: 'Гитары',
    items: [
      { id: 'gtr-fuzz', name: 'Гитара: фузз-стена', phrasing: 'fuzz wall of guitars' },
      { id: 'gtr-clean', name: 'Гитара: чистый арпеджио', phrasing: 'clean arpeggiated guitar' },
      { id: 'gtr-trem', name: 'Гитара: тремоло', phrasing: 'tremolo-picked guitar' },
      { id: 'gtr-acoustic', name: 'Акустика', phrasing: 'strummed acoustic guitar' },
      { id: 'gtr-slide', name: 'Слайд', phrasing: 'slide guitar lines' },
    ],
  },
  {
    id: 'keys', name: 'Клавиши',
    items: [
      { id: 'organ-hammond', name: 'Хэммонд', phrasing: 'Hammond organ swells' },
      { id: 'synth-pad', name: 'Синт-пэд', phrasing: 'warm analog synth pad' },
      { id: 'piano-upright', name: 'Пианино (прямое)', phrasing: 'detuned upright piano' },
      { id: 'rhodes', name: 'Родес', phrasing: 'Rhodes electric piano' },
    ],
  },
  {
    id: 'folk', name: 'Народные',
    items: [
      { id: 'flute', name: 'Флейта', phrasing: 'airy flute melody' },
      { id: 'accordion', name: 'Акордеон', phrasing: 'wistful accordion' },
      { id: 'violin', name: 'Скрипка', phrasing: 'folk violin, rough bowing' },
      { id: 'cello', name: 'Виолончель', phrasing: 'dark cello drones' },
    ],
  },
  {
    id: 'electro', name: 'Электроника',
    items: [
      { id: 'synth-bass', name: 'Синт-бас', phrasing: 'moogy synth bass' },
      { id: 'arp', name: 'Арпеджиатор', phrasing: 'sequenced synth arpeggio' },
      { id: 'noise', name: 'Шумовой слой', phrasing: 'tape hiss and noise layer' },
    ],
  },
  {
    id: 'special', name: 'Спецслои',
    items: [
      { id: 'choir', name: 'Бэк-вокал/хор', phrasing: 'distant choir backing vocals' },
      { id: 'glockenspiel', name: 'Колокольчики', phrasing: 'sparse glockenspiel' },
      { id: 'field', name: 'Полевые записи', phrasing: 'field recording interjections' },
    ],
  },
]

export const rackEffects = [
  { id: '', name: 'чисто' },
  { id: 'reverb', name: 'реверб' },
  { id: 'fuzz', name: 'фузз' },
  { id: 'tremolo', name: 'тремоло' },
  { id: 'phaser', name: 'фэйзер' },
  { id: 'wah', name: 'вау' },
  { id: 'echo', name: 'эхо' },
]

const effectPhrase = {
  reverb: 'drenched in reverb',
  fuzz: 'through fuzz',
  tremolo: 'with tremolo',
  phaser: 'with slow phaser',
  wah: 'with wah',
  echo: 'with tape echo',
}

export function allRackItems() {
  return rackGroups.flatMap(g => g.items)
}

// Стойка → фрагмент строки стиля
export function rackCompile(sel) {
  const items = allRackItems()
  return sel.map(({ id, effect }) => {
    const it = items.find(x => x.id === id)
    if (!it) return ''
    return it.phrasing + (effect && effectPhrase[effect] ? ` ${effectPhrase[effect]}` : '')
  }).filter(Boolean).join(', ')
}
