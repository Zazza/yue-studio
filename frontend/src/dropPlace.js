// Левый край выпадающего списка (fixed-координаты): по кнопке, а если список
// не влезает до правого края окна — сдвиг влево до отступа margin; шире окна —
// прижат к левому отступу. Без сдвига список у правого края окна (селект папки
// в развороте трека) сжимался до узкой полосы и резал названия.
export function dropLeft(btnLeft, dropWidth, viewportWidth, margin = 12) {
  const maxLeft = viewportWidth - margin - dropWidth
  return Math.max(margin, Math.min(btnLeft, maxLeft))
}
