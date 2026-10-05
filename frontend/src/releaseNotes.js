// «Что нового» — коротко и простыми словами, новые версии сверху. Запись
// добавляется при релизе вместе с разделом CHANGELOG (там — подробно для
// разработчиков). Первая запись = версия этой сборки (см. welcome.js).
export const RELEASE_NOTES = [
  {
    version: '0.8.0',
    ru: [
      'Кнопка «Создать партитуру» у треков без неё — после неё работает пиано-ролл.',
      'Свой значок приложения и единые значки вместо эмодзи.',
      'Строка состояния внизу: модель, видеопамять, очередь.',
      'Список треков — строками; раскрытый трек разбит на «Действия · Стиль · Версии».',
      'Педали для гитары и 23 новых эффекта против «плоского» звука.',
      'Гитара и клавиши — отдельными дорожками.',
      '«Характер» исполнения: смелее или точнее.',
      'Студия по шагам: слушать · править · звук · готово.',
      'Готовую ABC-партитуру можно прикрепить прямо в форме нового трека.',
      'Распознавание нот, дорожки и текст больше не мешают рендеру — общая очередь к видеокарте.',
    ],
    en: [
      '“Create score” for tracks without one — the piano roll works after it.',
      'An app icon of its own and one icon set instead of emoji.',
      'Status bar at the bottom: model, video memory, queue.',
      'Track list as rows; an opened track is split into “Actions · Style · Versions”.',
      'Guitar pedals and 23 new effects against a “flat” sound.',
      'Guitar and keys as separate stems.',
      'Performance “character”: bolder or more precise.',
      'Studio in steps: listen · edit · sound · done.',
      'A ready ABC score can be attached right in the new-track form.',
      'Note recognition, stems and lyrics no longer get in the way of rendering — one shared GPU queue.',
    ],
  },
]
