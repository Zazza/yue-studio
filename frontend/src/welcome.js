// Окно «О приложении / Что нового» при запуске. Состояние — в localStorage:
// { dismissed: «больше не показывать», seenVersion: версия, чьи новости уже видели }.
// Текущая версия — самая свежая запись RELEASE_NOTES (список едет внутри сборки).
export const WELCOME_KEY = 'yue_welcome'

// Какую вкладку открыть при запуске: 'news' — вышла версия, новости которой ещё
// не видели (даже после «больше не показывать»); 'about' — пока не отказались;
// null — не показывать. Самый первый запуск — 'about', а не новости.
export function welcomeTab(state, currentVersion) {
  const s = state || {}
  const fresh = !s.seenVersion
  const newVersion = !!currentVersion && !fresh && s.seenVersion !== currentVersion
  if (newVersion) return 'news'
  return s.dismissed ? null : 'about'
}

// Состояние после закрытия окна: новости текущей версии прочитаны;
// «больше не показывать» — навсегда (кроме новостей следующих версий).
export function welcomeClosed(state, currentVersion, dontShow) {
  const s = state || {}
  return { dismissed: !!s.dismissed || !!dontShow, seenVersion: currentVersion || s.seenVersion || '' }
}
