// Установка наборов сэмплов на воркере: какой набор ставится сейчас (общий флаг для страницы «Инструменты» и студии)
// и строка прогресса из GET /fx/kits/progress. Набор качается при первом выборе инструмента — минуту и дольше, без
// строки прогресса казалось, что ничего не происходит.
import { ref } from 'vue'

export const kitInstalling = ref('')   // имя набора, который ставится; '' — ничего

/** Поставить набор, пока идёт установка — kitInstalling = name (строку показывает KitProgress). */
export async function installKit(api, name) {
  kitInstalling.value = name
  try {
    return await api.installFxKit(name)
  } finally {
    kitInstalling.value = ''
  }
}

/** Строка прогресса: {name, part, done, total, bytes} → «качаю набор «vsco-violin»: часть ens, 12 из 45 файлов,
 *  18 МБ»; пусто/нет данных — ''. t — перевод (ключ kit.progress). */
export function kitProgressText(p, t) {
  if (!p || !p.name) return ''
  const mb = (Number(p.bytes) || 0) / 1e6
  return t('kit.progress', { name: p.name, part: p.part || '', done: p.done || 0, total: p.total || 0,
    mb: mb >= 10 ? Math.round(mb) : mb.toFixed(1) })
}
