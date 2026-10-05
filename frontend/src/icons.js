// Набор значков интерфейса: внутренняя разметка SVG на сетке 24×24, линии
// stroke="currentColor" (цвет — от текста, значит от темы). Заливку задают
// только сплошные значки (play/pause/star-fill/точки меню).
// Вместо цветных эмодзи: те рисуются системным шрифтом своими цветами,
// выбиваются из темы, а без шрифта эмодзи в Linux — пустые квадраты.
const F = 'fill="currentColor" stroke="none"'

export const ICONS = {
  x: '<path d="M18 6 6 18M6 6l12 12"/>',
  info: `<circle cx="12" cy="12" r="9"/><path d="M12 11v6"/><circle cx="12" cy="7.6" r="1.1" ${F}/>`,
  plus: '<path d="M12 5v14M5 12h14"/>',
  alert: `<path d="M10.3 3.9 2.4 18a2 2 0 0 0 1.7 3h15.8a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/><path d="M12 9v4"/><circle cx="12" cy="17" r="1.1" ${F}/>`,
  play: `<path d="M7 4.5v15l12.5-7.5z" ${F}/>`,
  stop: `<rect x="6" y="6" width="12" height="12" rx="1.5" ${F}/>`,
  pause: `<rect x="6" y="4.5" width="4" height="15" rx="1" ${F}/><rect x="14" y="4.5" width="4" height="15" rx="1" ${F}/>`,
  pencil: '<path d="M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16z"/><path d="m13.5 6.5 4 4"/>',
  repeat: '<path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/>',
  star: '<path d="m12 3 2.8 5.7 6.2.9-4.5 4.4 1.1 6.2L12 17.3l-5.6 2.9 1.1-6.2L3 9.6l6.2-.9z"/>',
  'star-fill': `<path d="m12 3 2.8 5.7 6.2.9-4.5 4.4 1.1 6.2L12 17.3l-5.6 2.9 1.1-6.2L3 9.6l6.2-.9z" ${F}/>`,
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  moon: '<path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/>',
  'more-v': `<circle cx="12" cy="5" r="1.8" ${F}/><circle cx="12" cy="12" r="1.8" ${F}/><circle cx="12" cy="19" r="1.8" ${F}/>`,
  'more-h': `<circle cx="5" cy="12" r="1.8" ${F}/><circle cx="12" cy="12" r="1.8" ${F}/><circle cx="19" cy="12" r="1.8" ${F}/>`,
  music: '<path d="M9 18V5l11-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="17" cy="16" r="3"/>',
  mic: '<rect x="9" y="2" width="6" height="12" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v4M8 22h8"/>',
  sliders: '<path d="M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12"/><circle cx="16" cy="6" r="2"/><circle cx="10" cy="12" r="2"/><circle cx="18" cy="18" r="2"/>',
  globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/>',
  folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
  volume: '<path d="M4 9v6h4l5 4V5L8 9z"/><path d="M16.5 8.5a5 5 0 0 1 0 7M19 6a8.5 8.5 0 0 1 0 12"/>',
  save: '<path d="M5 3h11l3 3v13a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z"/><path d="M7 3v5h8V3M7 21v-7h10v7"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/>',
  target: `<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="5"/><circle cx="12" cy="12" r="1.2" ${F}/>`,
  flask: '<path d="M9 3h6M10 3v6L4.5 18.5A1.7 1.7 0 0 0 6 21h12a1.7 1.7 0 0 0 1.5-2.5L14 9V3"/><path d="M7 15h10"/>',
  disc: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="2.5"/>',
  wind: '<path d="M3 8h11a3 3 0 1 0-3-3M3 12h15a3 3 0 1 1-3 3M3 16h7"/>',
  pedal: '<rect x="5" y="3" width="14" height="18" rx="2"/><circle cx="9" cy="7.5" r="1.5"/><circle cx="15" cy="7.5" r="1.5"/><circle cx="12" cy="15.5" r="2.5"/>',
  eraser: '<path d="m7 21-4-4L14 6l6 6-9 9z"/><path d="M11 21h10M9.5 10.5l6 6"/>',
}
