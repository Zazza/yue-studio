<script setup>
// Yue Studio — корневой компонент: форма новой композиции, очередь/результаты,
// переключение страниц. Экранные компоненты и модалки — в ./components/.
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { api } from './api.js'
import { rackGroups, rackEffects, rackCompile } from './rack.js'
import { groups as builtinGroups, loadCustomGroups, saveCustomGroups } from './groups.js'
import { slotKeys, slotHints, durOptions, durTokens } from './slotOptions.js'
import { CHARACTER, characterPayload, characterLabel } from './character.js'
import { useI18n } from './i18n/index.js'
import { voiceDescriptor, normalizeVoiceParams } from './voiceLab.js'
import { defaultJobFilter, filterJobs, groupJobs, pageJobs, pageCount, folderNames, FOLDER_NONE } from './jobFilter.js'
import { styleRows } from './styleTags.js'
import { isResultJob, mixChildId, mixLabel } from './insertLabels.js'
import { useInserts } from './composables/useInserts.js'
// сервис «перепеть с места»: живёт всё время, как вклейки (студию закрывают)
import './composables/useRevoice.js'
import { SLOT_ORDER, buildStyleLine, dictStyle, cleanLyrics, effectiveLyrics } from './styleLogic.js'
import { usePlayer, fmtDur } from './composables/usePlayer.js'
import { useConfirm } from './composables/useConfirm.js'
import VSelect from './VSelect.vue'
import PlayerBar from './components/PlayerBar.vue'
import SettingsPage from './components/SettingsPage.vue'
import LibraryPage from './components/LibraryPage.vue'
import CorpusPage from './components/CorpusPage.vue'
import VoicesPage from './components/VoicesPage.vue'
import StudioPage from './components/StudioPage.vue'
import WelcomeModal from './components/WelcomeModal.vue'
import { WELCOME_KEY, welcomeTab, welcomeClosed } from './welcome.js'
import { RELEASE_NOTES } from './releaseNotes.js'
import logoUrl from './assets/logo.png'
import ConfirmModal from './components/ConfirmModal.vue'
import MetricsModal from './components/MetricsModal.vue'
import CopilotModal from './components/CopilotModal.vue'
import PlanModal from './components/PlanModal.vue'

const serverURL = ref('')

// тема: тёмная по умолчанию, светлая — [data-theme="light"]
const theme = ref(localStorage.getItem('yue_theme') || 'dark')
document.documentElement.setAttribute('data-theme', theme.value)
function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : 'dark'
  localStorage.setItem('yue_theme', theme.value)
  document.documentElement.setAttribute('data-theme', theme.value)
}

// размер окна: восстанавливаем сохранённый при запуске, изменения пишем с дебаунсом
// (вне wails — в браузере/тестах — рантайма нет, тихо пропускаем)
import { WindowGetSize, WindowSetSize } from './wailsjs/runtime/runtime.js'
try {
  const saved = JSON.parse(localStorage.getItem('yue_window') || 'null')
  if (saved && saved.w >= 900 && saved.h >= 600) WindowSetSize(saved.w, saved.h)
} catch { /* первый запуск или повреждённая запись — дефолт из main.go */ }
let winSizeTimer = 0
window.addEventListener('resize', () => {
  clearTimeout(winSizeTimer)
  winSizeTimer = setTimeout(async () => {
    try {
      const s = await WindowGetSize()
      if (s.w >= 900 && s.h >= 600) localStorage.setItem('yue_window', JSON.stringify({ w: s.w, h: s.h }))
    } catch { /* не wails — сохранять нечего */ }
  }, 700)
})

const { playerState, playBusy, isPlaying, playBtn, toggleArtifact, onVolume, onRefresh } = usePlayer()
const { locale, t, setLocale } = useI18n()
const healthTitle = computed(() => health.value
  ? t('app.health.up') + (health.value.model_loaded ? t('app.health.model') : '')
  : t('app.health.down'))
const statusLabelC = computed(() => ({
  queued: t('queue.status.queued'), running: t('queue.status.running'), done: t('queue.status.done'),
  error: t('queue.status.error'), canceled: t('queue.status.canceled'),
}))
// короткая метка группы строки стиля: жанр, ритм, голос и т.д.
const styleTagLabel = (slot) => t('style.tag.' + slot)
const { askConfirm } = useConfirm()

// ---------- Форма новой композиции ----------

const slots = ref({
  language: 'Russian',
  genre: '', rhythm: '', guitars: '', keys: '', vocals: '', mood: '', production: '',
  bpm: null,
})
const styleOverride = ref('')
const lyrics = ref(`[Verse]\n...\n\n[Chorus]\n...`)
const seed = ref(null)
const cot = ref('full')
const arcKind = ref('')  // драматургия: '' | build | wave | burst
// характер исполнения: смелость игры (температура) и точность по стилю/нотам (cfg)
const temperature = ref(CHARACTER.temperature.def)
const cfgScale = ref(CHARACTER.cfg.def)
const characterDefault = computed(() => !Object.keys(characterPayload(temperature.value, cfgScale.value)).length)
// «Дополнительно» свёрнуто — изменённое показывается в его заголовке
const advancedSummary = computed(() => {
  const parts = []
  if (seed.value) parts.push('seed ' + seed.value)
  if (cot.value !== 'full') parts.push(t('form.cot.' + cot.value))
  if (arcKind.value) parts.push(t('arc.' + arcKind.value))
  const ch = characterPayload(temperature.value, cfgScale.value)
  const chl = characterLabel(ch)
  if (chl) parts.push(chl)
  return parts.join(' · ')
})
function characterReset() {
  temperature.value = CHARACTER.temperature.def
  cfgScale.value = CHARACTER.cfg.def
}
// «без слов»: стих не нужен, на воркер уйдёт заглушка [Instrumental]
const noLyrics = ref(false)
// длительность инструментала: длина трека у YuE2 задаётся числом секций в тексте,
// явного параметра нет; диапазон размножает [Instrumental]-секции
const durMode = ref('auto')
const title = ref('')
const autoTranslate = ref(true)   // слоты по-русски → перевод в английскую строку стиля
const translateBusy = ref(false)
const submitting = ref(false)

// инструментальная стойка: [{id, effect}] — разворачивается в текст стиля
const rackSel = ref([])

function rackToggle(itemId) {
  const i = rackSel.value.findIndex(r => r.id === itemId)
  if (i >= 0) rackSel.value = rackSel.value.filter(r => r.id !== itemId)
  else rackSel.value = [...rackSel.value, { id: itemId, effect: '' }]
}

function rackSetEffect(itemId, effect) {
  rackSel.value = rackSel.value.map(r => (r.id === itemId ? { ...r, effect } : r))
}

// свой выпадающий список подсказок слотов (нативные datalist в WebView2 глючат)
const openSlot = ref('')
function slotFiltered(key) {
  const v = (slots.value[key] || '').trim().toLowerCase()
  const opts = slotHints[key] || []
  const list = v ? opts.filter(([ru]) => ru.toLowerCase().includes(v)) : opts
  return list.slice(0, 30)
}

const compiledStyle = computed(() =>
  styleOverride.value.trim() || buildStyleLine(slots.value, rackCompile(rackSel.value)))

// поле «Стиль одной строкой»: пока не трогали — показывает то, что собрано
// из слотов и стойки; первая правка фиксирует ручную версию (переопределяет форму)
const styleLine = computed({
  get: () => styleOverride.value || compiledStyle.value,
  set: (v) => { styleOverride.value = v },
})

// финальная строка стиля к отправке: русские значения из списков подставляются
// словарём, самописная кириллица переводится через Ollama; английское — как есть
const CYR = /[а-яё]/i

async function finalStyle() {
  let s
  if (styleOverride.value.trim()) {
    s = styleOverride.value.trim()
  } else {
    const parts = SLOT_ORDER.map(k => (slots.value[k] || '').trim())
      .filter(Boolean).map(dictStyle)
    const rack = rackCompile(rackSel.value)
    if (rack) parts.push(rack)
    if (slots.value.bpm) parts.push(slots.value.bpm + ' BPM')
    s = parts.join(', ')
  }
  if (!autoTranslate.value || !CYR.test(s)) return s
  translateBusy.value = true
  try {
    const r = await api.translate(s)
    return r.text
  } catch (e) {
    console.warn('translate failed, using as-is:', e)
    return s
  } finally { translateBusy.value = false }
}

function payload(extra = {}) {
  const abc = customAbc.value.trim()
  return {
    title: title.value,
    lyrics: effectiveLyrics(lyrics.value, noLyrics.value, durMode.value),
    seed: seed.value ? Number(seed.value) : 0,
    // драматургии и прикреплённому ABC нужен план: off его не строит — молча повышаем до melody
    cot: (arcKind.value || abc) && cot.value === 'off' ? 'melody' : cot.value,
    arc: arcKind.value,
    // потолок длины из селектора длительности (0 = бюджет воркера)
    max_tokens: durTokens[durMode.value] || 0,
    ...characterPayload(temperature.value, cfgScale.value),
    ...(abc ? { abc } : {}),
    ...extra,
  }
}

// guard отправки: стиль есть и стих есть (или инструментал)
const canSubmit = computed(() =>
  !!compiledStyle.value && (!!lyrics.value.trim() || noLyrics.value))

async function submit() {
  if (!canSubmit.value) return
  submitting.value = true
  submitErr.value = ''
  try {
    const id = await api.submit({ ...payload(), style: await finalStyle() })
    if (customAbc.value.trim()) inheritTrickMarks(id)
    newTrackPage.value = false   // к списку: трек уже в очереди
    await refresh()
  } catch (e) {
    submitErr.value = String(e?.message || e)
  } finally { submitting.value = false }
}

// черновик ~40 с: быстро послушать стиль, прежде чем рендерить полный трек
async function submitDraft() {
  if (!canSubmit.value) return
  submitting.value = true
  submitErr.value = ''
  try {
    const id = await api.submit({ ...payload({ draft: true }), style: await finalStyle() })
    if (customAbc.value.trim()) inheritTrickMarks(id)
    newTrackPage.value = false   // к списку: трек уже в очереди
    await refresh()
  } catch (e) {
    submitErr.value = String(e?.message || e)
  } finally { submitting.value = false }
}

async function submitFan(n) {
  if (!canSubmit.value) return
  submitting.value = true
  submitErr.value = ''
  try {
    await api.submitFan({ ...payload(), style: await finalStyle() }, n)
    newTrackPage.value = false   // к списку: трек уже в очереди
    await refresh()
  } catch (e) {
    submitErr.value = String(e?.message || e)
  } finally { submitting.value = false }
}

function applyPreset(p, mode) {
  title.value = p.name
  if (p.lyrics !== undefined) lyrics.value = p.lyrics
  if (p.lyrics === '[Instrumental]') noLyrics.value = true
  else if (p.lyrics !== undefined) noLyrics.value = false
  if (p.seed !== undefined) seed.value = p.seed
  cot.value = 'full'
  slots.value = {
    language: '', genre: '', rhythm: '', guitars: '', keys: '',
    vocals: '', mood: '', production: '', bpm: p.bpm || null,
  }
  if (mode === 'line' || !p.slots) {
    // точная строка (1:1) или свой стиль библиотеки — у него нет слотов
    styleOverride.value = p.style
  } else {
    styleOverride.value = ''
    for (const k of Object.keys(p.slots)) slots.value[k] = p.slots[k]
  }
}

// текущая форма → своя группа библиотеки: строка (или слоты+BPM) как есть
function saveStyleToLibrary() {
  const gid = 'c-from-form'
  if (!customGroups.value.find((g) => g.id === gid)) {
    customGroups.value = [...customGroups.value, { id: gid, name: 'Из формы', items: [] }]
  }
  const g = customGroups.value.find((x) => x.id === gid)
  const name = (title.value || '').trim() || 'стиль ' + new Date().toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
  const item = {
    id: 'c-' + Date.now().toString(36),
    name,
    style: styleOverride.value.trim() || compiledStyle.value,
  }
  if (!styleOverride.value.trim()) item.slots = { ...slots.value }   // раскладка по слотам сохранится
  const dup = g.items.find((i) => i.name === name)
  g.items = dup ? g.items.map((i) => (i.name === name ? item : i)) : [...g.items, item]
  saveCustomGroups(customGroups.value)
  libGroup.value = gid
  libStyle.value = item.id
}

// библиотека стилей на главной: группы → стили (двухуровневый комбобокс)
const customGroups = ref(loadCustomGroups())
const allGroups = computed(() => [...builtinGroups, ...customGroups.value])
const libGroup = ref('')
const libStyle = ref('')
const groupItems = computed(() => (allGroups.value.find((g) => g.id === libGroup.value) || { items: [] }).items)
const currentItem = computed(() => groupItems.value.find((i) => i.id === libStyle.value))
const libGroupOptions = computed(() => allGroups.value.map((g) => ({ value: g.id, label: `${g.name} (${(g.items || []).length})` })))
const libStyleOptions = computed(() => groupItems.value.map((i) => ({ value: i.id, label: i.name })))

function onLibGroupChange() { libStyle.value = '' }
function onLibStyleChange() { if (currentItem.value) applyPreset(currentItem.value, 'slots') }
function onLibExact() { if (currentItem.value) applyPreset(currentItem.value, 'line') }

// ---------- Джобы: очередь, статусы, действия ----------

const jobs = ref([])
const health = ref(null)
const stats = ref(null)
let timer = null

// фильтры и пейджер списка треков (логика — jobFilter.js, там же тесты)
const qf = ref(defaultJobFilter())
const qPage = ref(1)
// производные треки (куски для вклеек, пересборки, варианты) — под родителем
const grouped = computed(() => groupJobs(jobs.value))
const filteredJobs = computed(() => filterJobs(grouped.value.top, qf.value, Date.now(), grouped.value.children))
const openKids = ref(new Set())   // id родителей с раскрытыми вложениями
const openStyle = ref(new Set())  // id карточек с раскрытой таблицей стиля
const openJobs = ref(new Set())      // id треков с развёрнутыми строками (сколько угодно)
function toggleOpenJob(id) {
  const s = new Set(openJobs.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  openJobs.value = s
}
// открытые треки на текущей странице: «свернуть все» и приглушение закрытых строк
const openOnPage = computed(() => queuePage.value.filter((j) => openJobs.value.has(j.id)).length)
function collapseAllJobs() { openJobs.value = new Set() }
function toggleStyle(id) {
  const s = new Set(openStyle.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  openStyle.value = s
}
// выпадающее меню на <details>: выбор пункта его закрывает
function closeMenu(e) {
  const d = e.target.closest('details')
  if (d) d.open = false
}
// клик мимо открытого меню «⤓ скачать» / «⋯» — закрыть его
function closeMenusOutside(e) {
  for (const d of document.querySelectorAll('details.menu-pop[open]')) if (!d.contains(e.target)) d.open = false
}
onMounted(() => document.addEventListener('click', closeMenusOutside))
onUnmounted(() => document.removeEventListener('click', closeMenusOutside))
// готовые миксы с вклейками (файлы эффектов родителя overdub-inst-*) — по раскрытию
const kidMixes = ref({})
const insertsSvc = useInserts()
async function toggleKids(id) {
  const s = new Set(openKids.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  openKids.value = s
  if (s.has(id)) {
    try {
      const vs = (await api.dspVariants(id)) || []
      // свежий первым: текущий результат — последний микс (в нём все вклейки)
      const mixes = vs.filter((v) => mixChildId(v.file) != null)
        .sort((a, b) => String(b.created_at || '').localeCompare(String(a.created_at || '')))
      kidMixes.value = { ...kidMixes.value, [id]: mixes }
    } catch { /* варианты недоступны — покажем только треки-вложения */ }
  }
}
// что под «версии»: дочерние треки и готовые миксы (у трека может быть только микс —
// эффект на дорожку без дочерних треков)
const kidCount = (j) => (grouped.value.children[j.id] || []).length + (j.mixes || 0)
const kidResults = (id) => (grouped.value.children[id] || []).filter(isResultJob)
// основная версия песни (head_id у корня): её играет «▶» и открывает студия
function headOf(j) {
  if (!j.head_id) return j
  return (grouped.value.children[j.id] || []).find((k) => k.id === j.head_id) || j
}
async function makeHead(root, v) {
  try {
    await api.setHead(root.id, v.id === root.id ? 0 : v.id)
    await refresh()
  } catch (e) {
    alert(String(e))
  }
}
// подпись и папка песни: своё название вместо номера, папки «Альбом/Основы/…»
const titleEdit = ref(null) // { id, value } — трек, чьё название правится
function startRename(j) { titleEdit.value = { id: j.id, value: j.title || '' } }
async function saveRename(j) {
  const e = titleEdit.value
  titleEdit.value = null
  if (!e || e.id !== j.id || !e.value.trim() || e.value.trim() === j.title) return
  try {
    const upd = await api.renameJob(j.id, e.value.trim())
    j.title = upd.title
  } catch (err) {
    alert(String(err))
  }
}
async function moveToFolder(j, folder) {
  try {
    const upd = await api.setJobFolder(j.id, folder === FOLDER_NONE ? '' : folder)
    j.folder = upd.folder || ''
  } catch (err) {
    alert(String(err))
  }
}
// песня «играет», если играет любая её версия или материал
const songPlaying = (j) => [j, ...(grouped.value.children[j.id] || [])].some((v) => isPlaying('m' + v.id))
const folderList = computed(() => folderNames(grouped.value.top))
const qFolderOptions = computed(() => [
  { value: 'all', label: t('queue.folder.all') },
  ...folderList.value.map((f) => ({ value: f, label: f, icon: 'folder' })),
  { value: FOLDER_NONE, label: t('queue.folder.none') },
])
const FOLDER_NEW = '\u0000new'
const jobFolderOptions = computed(() => [
  { value: FOLDER_NONE, label: t('queue.folder.none') },
  ...folderList.value.map((f) => ({ value: f, label: f, icon: 'folder' })),
  { value: FOLDER_NEW, label: t('queue.folder.new'), icon: 'plus' },
])
// своя папка: выбор «новая папка…» открывает поле имени у этой песни
const folderNew = ref(null) // { id, value }
function pickFolder(j, v) {
  if (v === FOLDER_NEW) { folderNew.value = { id: j.id, value: '' }; return }
  moveToFolder(j, v)
}
async function saveNewFolder(j) {
  const e = folderNew.value
  folderNew.value = null
  if (e && e.id === j.id && e.value.trim()) await moveToFolder(j, e.value.trim())
}
// время создания: «2026-10-01T17:26:05» → «01.10 17:26» (год — если не текущий)
function fmtWhen(s) {
  const m = /^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})/.exec(String(s || ''))
  if (!m) return ''
  const year = m[1] === String(new Date().getFullYear()) ? '' : '.' + m[1]
  return `${m[3]}.${m[2]}${year} ${m[4]}:${m[5]}`
}
const kidMaterial = (id) => (grouped.value.children[id] || []).filter((k) => !isResultJob(k))
// подпись микса: по реестру вклеек, иначе сохранённая воркером (микс из MCP)
function mixName(parentId, v) {
  const label = mixLabel(v.file, { applied: insertsSvc.appliedFor(parentId), jobs: jobs.value,
    labelOf: (id) => t('studio.trick.inst.' + id), fmt: fmtDur })
  if (label) return t('studio.trick.inst.mix', { what: label })
  return v.label || t('studio.trick.inst.variant', { id: mixChildId(v.file) })
}
function playMix(parent, v) {
  toggleArtifact('mix' + parent.id + ':' + v.file, mixName(parent.id, v),
    () => api.playFile(parent.id, v.file, parent.duration_sec))
}
const qPageMax = computed(() => pageCount(filteredJobs.value.length))
const qPageNow = computed(() => Math.min(qPage.value, qPageMax.value))
const queuePage = computed(() => pageJobs(filteredJobs.value, qPage.value))
watch(qf, () => { qPage.value = 1 }, { deep: true })

const qStatusOptions = computed(() => [
  { value: 'all', label: t('queue.filter.status.all') },
  { value: 'active', label: t('queue.filter.status.active') },
  { value: 'done', label: t('queue.filter.status.done') },
  { value: 'failed', label: t('queue.filter.status.failed') },
])
const qPeriodOptions = computed(() => [
  { value: 'all', label: t('queue.filter.period.all') },
  { value: 'today', label: t('queue.filter.period.today') },
  { value: 'week', label: t('queue.filter.period.week') },
  { value: 'month', label: t('queue.filter.period.month') },
])
const qDurOptions = computed(() => [
  { value: 'all', label: t('queue.filter.dur.all') },
  { value: 'draft', label: t('queue.filter.dur.draft') },
  { value: 'short', label: t('queue.filter.dur.short') },
  { value: 'mid', label: t('queue.filter.dur.mid') },
  { value: 'long', label: t('queue.filter.dur.long') },
])
const qDraftOptions = computed(() => [
  { value: 'all', label: t('queue.filter.draft.all') },
  { value: 'only', label: t('queue.filter.draft.only') },
  { value: 'hide', label: t('queue.filter.draft.hide') },
])


async function refresh() {
  try {
    const [j, h] = await Promise.all([api.jobs(), api.status()])
    jobs.value = j || []
    health.value = h
  } catch {
    health.value = null
  }
  // сводка статус-бара отдельно: старый воркер без /stats не должна ронять обновление
  api.stats().then(s => { stats.value = s }).catch(() => { stats.value = null })
  try { playerState.value = await api.audioState() } catch {}
}
onRefresh(refresh)

async function togglePlay(j) {
  await toggleArtifact(`m${j.id}`, `#${j.id} ${j.title}`, () => api.playAudio(j.id))
}

async function cancel(id) {
  await api.cancel(id)
  refresh()
}
// метка «голос в инструментале»: где в треке без голоса звучит дорожка голоса
const leakTip = (j) => t('queue.vocalLeak.tip', { at: j.vocal_leak.split(',').map((s) => fmtDur(Number(s))).join(', ') })
// «повторить» упавшую или отменённую джобу: снова в очередь с теми же параметрами
const canRetry = (j) => j.status === 'error' || j.status === 'canceled'
async function retry(j) {
  try {
    await api.retryJob(j.id)
  } catch (e) {
    alert(String(e))
  }
  refresh()
}

function deleteJob(j) {
  askConfirm(`Удалить результат #${j.id} «${j.title}»?`,
    'Удалится запись и все файлы (аудио, партитура, стемы, превью) на сервере. Отменить нельзя.',
    async () => {
      try { await api.deleteJob(j.id) } catch (e) { alert(String(e)) }
      refresh()
    })
}

function reuseJob(j) {
  title.value = j.title
  styleOverride.value = j.style
  lyrics.value = j.lyrics
  seed.value = j.seed || null
  cot.value = j.cot === 'melody' ? 'melody' : 'full'   // «без плана» в форме нет
  temperature.value = j.temperature || CHARACTER.temperature.def
  cfgScale.value = j.cfg || CHARACTER.cfg.def
  openNewTrack()
}

function openListen(j) {
  api.openURL(`${serverURL.value}/listen/${j.id}`)
}

async function saveMp3(j) {
  if (!j.mp3_file) {
    playBusy.value = { ...playBusy.value, ['mp3' + j.id]: true }
    try {
      const r = await api.ensureMp3(j.id)
      j.mp3_file = (r && r.file) || 'audio.mp3'
    } catch (e) {
      planErr.value = 'mp3: ' + e
      planOpen.value = true
      return
    } finally { playBusy.value = { ...playBusy.value, ['mp3' + j.id]: false } }
  }
  await saveAudio(j, j.mp3_file || 'audio.mp3')
}

async function saveAudio(j, file) {
  const f = file || j.audio_file
  if (f) await api.saveAudio(j.id, f)
}

// ---------- Страницы ----------

const settingsPage = ref(false)
const libraryPage = ref(false)
const corpusPage = ref(false)
const voicesPage = ref(false)
const studioJob = ref(null)
// главная — список треков; форма нового трека — отдельная страница
const newTrackPage = ref(false)

function closeOverlays() {
  libraryPage.value = false; settingsPage.value = false
  corpusPage.value = false; voicesPage.value = false; navOpen.value = false
}

// «＋ Новый трек»: модалка поверх любого экрана (список, студия, библиотека)
function openNewTrack() {
  settingsPage.value = false; corpusPage.value = false; voicesPage.value = false; navOpen.value = false
  newTrackPage.value = true
}

// Esc закрывает модалку нового трека — но не из-под открытых поверх плана/копайтера/библиотеки
function onNewTrackKey(e) {
  if (e.key === 'Escape' && newTrackPage.value && !planOpen.value && !copOpen.value && !libraryPage.value) newTrackPage.value = false
}
onMounted(() => window.addEventListener('keydown', onNewTrackKey))
onUnmounted(() => window.removeEventListener('keydown', onNewTrackKey))

// на главную — список треков (клик по названию приложения)
function goHome() {
  closeOverlays()
  studioJob.value = null
  newTrackPage.value = false
}

// страницы в меню «⋮»: треки/голоса переключаются, настройки просто открываются
const navOpen = ref(false)
// окно «Что это / Что нового»: при запуске — по welcome.js, из меню — всегда
const APP_VERSION = RELEASE_NOTES[0]?.version || ''
function readWelcome() {
  try { return JSON.parse(localStorage.getItem(WELCOME_KEY) || 'null') } catch { return null }
}
const welcome = ref(welcomeTab(readWelcome(), APP_VERSION))   // 'about' | 'news' | null
function closeWelcome(dontShow) {
  try {
    localStorage.setItem(WELCOME_KEY, JSON.stringify(welcomeClosed(readWelcome(), APP_VERSION, dontShow)))
  } catch { /* без localStorage окно просто покажется снова */ }
  welcome.value = null
}

function navGo(page) {
  navOpen.value = false
  libraryPage.value = false
  if (page === 'tracks') {
    corpusPage.value = !corpusPage.value; settingsPage.value = false; voicesPage.value = false
  } else if (page === 'voices') {
    voicesPage.value = !voicesPage.value; corpusPage.value = false; settingsPage.value = false
  } else {
    settingsPage.value = true; corpusPage.value = false; voicesPage.value = false
  }
}

// импорт своего трека (кнопка на странице «свои треки»)
async function onImported(r) {
  await refresh()
  const j = (jobs.value || []).find((x) => x.id === r.id)
  if (j) studioJob.value = j
  if (r.transcribe_error) {
    planErr.value = 'Трек импортирован, но транскрипция не удалась (не хватило памяти GPU — модель занята). ' +
      'Стемы, минус и эффекты работают; ролл и овердаб появятся, когда GPU освободится — импортируйте трек заново.'
    planOpen.value = true
  }
}

// стиль из профиля корпуса → своя группа «Из корпусов» (создаётся при первом разе)
function profileStyleToLibrary({ corpus, profile }) {
  const gid = 'c-corpus'
  if (!customGroups.value.find((g) => g.id === gid)) {
    customGroups.value = [...customGroups.value, { id: gid, name: 'Из корпусов', items: [] }]
  }
  const g = customGroups.value.find((x) => x.id === gid)
  const name = (corpus.name || 'профиль').trim()
  const dup = g.items.find((i) => i.name === name)
  const item = { id: dup ? dup.id : 'c-' + Date.now().toString(36), name, style: profile.style || '', bpm: profile.tempo_median || null }
  g.items = dup ? g.items.map((i) => (i.name === name ? item : i)) : [...g.items, item]
  saveCustomGroups(customGroups.value)
  libGroup.value = gid
  libStyle.value = item.id
  corpusPage.value = false
  libraryPage.value = true
}

function applyProfileStyle(style) {
  styleOverride.value = style
  openNewTrack()   // применяем — к форме нового трека
}

// голос из примерочной → форма: дескриптор в слот вокала + seed карточки
// (точная строка стиля очищается, иначе она перекрыла бы слоты)
function applyVoice({ vocals, seed }) {
  slots.value.vocals = vocals
  styleOverride.value = ''
  seed.value = seed || null
  openNewTrack()
}

// сохранённые голоса прямо в форме: выбор карточки = слот вокала + seed
const voiceCards = ref([])
const voicePick = ref('')

async function loadVoiceCards() {
  try { voiceCards.value = (await api.voices()) || [] } catch { voiceCards.value = [] }
}

const voiceOptions = computed(() => voiceCards.value.map((v) => ({ value: String(v.id), label: v.name })))

function onVoicePick() {
  const v = voiceCards.value.find((x) => String(x.id) === voicePick.value)
  if (!v) return
  try {
    const p = normalizeVoiceParams(JSON.parse(v.params || '{}'))
    applyVoice({ vocals: voiceDescriptor(p), seed: v.seed })
  } catch { /* битые params карточки — молча пропускаем */ }
}

// ---------- Редактор плана (ABC) ----------

const planOpen = ref(false)
const planBusy = ref(false)
const planErr = ref('')
const planInfo = ref(null)   // {seed, seconds, truncated} — как план получен
const planAbc = ref('')
const customAbc = ref('')    // прикреплённый план: скелет трека в форме
const abcOpen = ref(false)
const submitErr = ref('')

function setPlanAbc(abc, info) {
  planAbc.value = abc
  planInfo.value = info
  planErr.value = ''
  planOpen.value = true
}

async function makePlan() {
  if (!canSubmit.value) return
  planBusy.value = true
  planErr.value = ''
  planOpen.value = true
  const usedSeed = seed.value ? Number(seed.value) : Math.floor(Math.random() * 1e9)
  try {
    const r = await api.plan({
      style: await finalStyle(),
      lyrics: cleanLyrics(lyrics.value),
      seed: usedSeed,
      cot: cot.value === 'off' ? 'full' : cot.value,
    })
    setPlanAbc(r.abc, { seed: usedSeed, seconds: r.seconds, truncated: r.truncated })
  } catch (e) {
    planErr.value = String(e)
  } finally { planBusy.value = false }
}

// план из диалога → прикрепить к форме: скелет из ABC, а жанр/инструменты/ритм,
// дугу и сид задаём настройками; запуск — главной кнопкой генерации
function usePlanAbc(abc) {
  customAbc.value = (abc || '').trim()
  submitErr.value = ''
  planOpen.value = false
  openNewTrack()   // план мог открыться не из формы (свои треки) — показать форму с ним
}

// «ноты» — итоговый план: с прикреплённым показываем его, без — генерируем новый
function notesOpen() {
  if (customAbc.value.trim()) {
    setPlanAbc(customAbc.value, {})
    return
  }
  makePlan()
}

// метки приёмов переезжают в новую версию трека: правки плана накопительные,
// ролл ребёнка показывает, что уже ломали (формат — состояние студии, см. StudioPage)
function inheritTrickMarks(newJobId) {
  const marks = planInfo.value && planInfo.value.marks
  if (!marks || !marks.length || !newJobId) return
  try {
    const key = 'yue_studio_state'
    const st = JSON.parse(localStorage.getItem(key) || '{}')
    st.jobs = st.jobs || {}
    st.jobs[newJobId] = { ...(st.jobs[newJobId] || {}), trickMarks: marks }
    localStorage.setItem(key, JSON.stringify(st))
  } catch {}
}

async function loadJobAbc(j) {
  if (!j.abc_file) return
  planErr.value = ''
  try {
    const abc = await api.jobAbcText(j.id, j.abc_file)
    setPlanAbc(abc, { seed: j.seed, seconds: null, truncated: false, fromJob: j.id })
  } catch (e) {
    planErr.value = String(e)
    planOpen.value = true
  }
}

// Remix: трек → ABC (SheetSage2 на воркере)
async function transcribeFromTrack() {
  planErr.value = ''
  planBusy.value = true
  planOpen.value = true
  try {
    const r = await api.transcribeFile()
    if (!r) { planOpen.value = false; return }   // диалог отменён
    setPlanAbc(r.abc, { seed: null, seconds: r.seconds, truncated: false, fromTrack: true })
  } catch (e) {
    planErr.value = String(e)
  } finally { planBusy.value = false }
}

// ---------- Копайтер ----------

const copOpen = ref(false)
function onCopInsert(text) { lyrics.value = text }

// ---------- Лирика: whisper-распознавание и адаптация-перевод ----------

const lyrBusy = ref(false) // 'rec' | 'adapt' | ''
const lyrErr = ref('')
async function recognizeLyrics() {
  lyrErr.value = ''
  lyrBusy.value = 'rec'
  try {
    const r = await api.recognizeLyrics()
    if (r && r.text) lyrics.value = r.text
  } catch (e) {
    lyrErr.value = String(e)
  } finally {
    lyrBusy.value = ''
  }
}
async function adaptLyrics() {
  if (!lyrics.value.trim()) return
  lyrErr.value = ''
  lyrBusy.value = 'adapt'
  try {
    const r = await api.adaptLyrics(lyrics.value)
    if (r && r.text) lyrics.value = r.text
  } catch (e) {
    lyrErr.value = String(e)
  } finally {
    lyrBusy.value = ''
  }
}

// ---------- Метрики ----------

const metricsModal = ref(null)
function openMetrics(j, preset) {
  metricsModal.value.openFor(j, preset)
}

onMounted(async () => {
  onVolume()
  serverURL.value = await api.getServerURL()
  refresh()
  loadVoiceCards()
  timer = setInterval(refresh, 3000)
  window.addEventListener('click', onWindowClick)
})
onUnmounted(() => { clearInterval(timer); window.removeEventListener('click', onWindowClick) })

function progressTip(j) {
  const parts = [t('queue.progress.' + (j.stage || 'plan'))]
  if (j.tokens) parts.push(j.tokens + ' tok')
  if (j.tok_per_s) parts.push(j.tok_per_s + ' т/с')
  if (j.elapsed_sec) parts.push(fmtDur(j.elapsed_sec))
  return parts.join(' · ')
}

function onWindowClick(e) {
  if (!e.target.closest('.slot-box')) openSlot.value = ''
  if (!e.target.closest('.nav-wrap')) navOpen.value = false
}
</script>

<template>
  <header>
    <h1 class="home-link" :title="t('nav.home.tip')" @click="goHome"><img class="app-logo" :src="logoUrl" alt="" width="26" height="26">{{ t('app.title') }}</h1>
    <span class="health-dot" :class="health ? 'up' : 'down'" :title="healthTitle"></span>
    <button class="icon-btn" @click="toggleTheme" :title="theme === 'dark' ? t('app.theme.light') : t('app.theme.dark')"><AppIcon :name="theme === 'dark' ? 'sun' : 'moon'" /></button>
    <button class="icon-btn" @click="setLocale(locale === 'ru' ? 'en' : 'ru')"
            :title="locale === 'ru' ? 'Switch to English' : 'Переключить на русский'">{{ locale === 'ru' ? 'EN' : 'RU' }}</button>
    <div class="player-center"><PlayerBar :jobs="jobs" @play-job="togglePlay" @refresh="refresh" /></div>
    <!-- справа, у меню; плеер между ними забирает всю свободную ширину -->
    <button class="primary new-track-btn" :class="{ on: newTrackPage }"
            :title="t('nav.new.tip')" @click="openNewTrack"><AppIcon name="plus" /><span class="new-track-txt"> {{ t('nav.new.short') }}</span></button>
    <div class="nav-wrap">
      <button class="icon-btn" :title="t('nav.menu.tip')" @click.stop="navOpen = !navOpen"><AppIcon name="more-v" /></button>
      <ul v-if="navOpen" class="nav-menu">
        <li :title="t('nav.tracks.tip')" @click="navGo('tracks')"><AppIcon class="nav-ico" name="music" />{{ t('nav.tracks') }}</li>
        <li :title="t('nav.voices.tip')" @click="navGo('voices')"><AppIcon class="nav-ico" name="mic" />{{ t('nav.voices') }}</li>
        <li :title="t('nav.settings.tip')" @click="navGo('settings')"><AppIcon class="nav-ico" name="sliders" />{{ t('nav.settings') }}</li>
        <li :title="t('nav.about.tip')" @click="navOpen = false; welcome = 'about'"><AppIcon class="nav-ico" name="info" />{{ t('nav.about') }}</li>
      </ul>
    </div>
  </header>

  <!-- всё под шапкой прокручивается само: шапка с плеером всегда на виду -->
  <div class="app-body">
  <StudioPage v-if="studioJob" :job="studioJob" :auto-translate="autoTranslate"
              @close="studioJob = null; refresh()"
              @open-metrics="openMetrics" />

  <main v-else>
    <!-- страница треков: без панельной обёртки и заголовка «Треки» —
         страница и есть список; рамка у таблицы своя, двойная рамка не нужна -->
    <section class="track-list">
      <p v-if="!jobs.length" class="muted">{{ t('common.empty') }}
        <button class="primary" @click="openNewTrack"><AppIcon name="plus" /> {{ t('nav.new') }}</button></p>
      <template v-else>
        <div class="queue-tools">
          <VSelect v-model="qf.status" :options="qStatusOptions" />
          <VSelect v-model="qf.period" :options="qPeriodOptions" />
          <VSelect v-model="qf.dur" :options="qDurOptions" />
          <VSelect v-model="qf.draft" :options="qDraftOptions" />
          <VSelect v-model="qf.folder" :options="qFolderOptions" :title="t('queue.folder.tip')" />
          <input v-model="qf.q" :placeholder="t('queue.filter.search')" />
        </div>
        <p class="muted">{{ filteredJobs.length
          ? t('queue.filter.shown', { shown: queuePage.length, total: filteredJobs.length })
          : t('queue.filter.none') }}<template v-if="filteredJobs.length > queuePage.length && qPageMax > 1"> · {{ t('queue.filter.page', { page: qPageNow, max: qPageMax }) }}</template>
          <button v-if="openOnPage >= 2" class="ghost small-btn collapse-all" @click="collapseAllJobs">{{ t('queue.collapseAll') }}</button></p>
        <!-- таблица треков: общая рамка, строки на одной сетке; шапки колонок нет —
             всё самоочевидно, неоднозначное (версии, статус) — в подсказках -->
        <div v-if="queuePage.length" class="job-table" :class="{ 'has-open': openOnPage }">
        <article v-for="j in queuePage" :key="j.id" class="job"
                 :class="[j.status, { playing: songPlaying(j), open: openJobs.has(j.id) }]">
        <!-- строка трека: плоский список как в альбомном буклете — номер моно, круглая
             play-кнопка, крупное название, справа лёгкая моно-мета; клик — разворот -->
        <div class="job-row" :class="{ open: openJobs.has(j.id) }" @click="toggleOpenJob(j.id)">
          <span class="tl-num" :title="'#' + j.id">{{ j.id }}</span>
          <span class="tl-play-cell">
            <button v-if="j.status === 'done' && j.audio_file" class="tl-play"
                    :class="{ 'is-playing': isPlaying('m' + headOf(j).id) }" :disabled="playBusy['m' + headOf(j).id]"
                    :title="t('queue.play')" @click.stop="togglePlay(headOf(j))">
              <AppIcon :name="isPlaying('m' + headOf(j).id) ? 'pause' : 'play'" /></button>
            <span v-else class="status-mini" :class="j.status" :title="statusLabelC[j.status] || j.status"></span>
          </span>
          <span class="job-name" :title="j.title">{{ j.title }}
            <span v-if="j.draft" class="badge draft">{{ t('queue.draft') }}</span>
            <span v-else-if="j.status === 'error'" class="status" :class="j.status">{{ statusLabelC[j.status] || j.status }}</span>
            <span v-else-if="j.status === 'queued'" class="status" :class="j.status">{{ statusLabelC[j.status] || j.status }}</span>
            <span v-else-if="j.status === 'done' && headOf(j).vocal_leak" class="badge warn" :title="leakTip(headOf(j))"><AppIcon name="alert" /></span>
            <!-- идущая джоба: прогресс прямо в строке -->
            <span v-if="j.status === 'running'" class="job-progress" :title="progressTip(j)">
              <span class="progress-track slim" :class="{ indet: j.progress_pct == null }">
                <span v-if="j.progress_pct != null" class="progress-fill" :style="{ width: j.progress_pct + '%' }"></span>
              </span>
              <span class="muted progress-label">
                <template v-if="j.progress_pct != null">{{ j.progress_pct }}%</template>
                <template v-else>{{ t('queue.progress.' + (j.stage || 'plan')) }}</template>
                <template v-if="j.tok_per_s"> · {{ j.tok_per_s }} {{ t('queue.progress.tps') }}</template>
              </span>
            </span>
          </span>
          <span v-if="kidCount(j)" class="tl-pill kids-badge" :title="t('queue.kids.tip')"
                @click.stop="if (!openJobs.has(j.id)) toggleOpenJob(j.id); if (!openKids.has(j.id)) toggleKids(j.id)">{{ kidCount(j) }}</span>
          <span v-if="j.duration_sec" class="tl-meta col-dur">{{ fmtDur(j.duration_sec) }}</span>
          <span v-if="fmtWhen(j.created_at)" class="tl-meta job-when" :title="j.created_at">{{ fmtWhen(j.created_at) }}</span>
          <span v-if="(j.folder || '').trim()" class="tl-pill job-folder">{{ j.folder.trim() }}</span>
          <button class="ghost icon job-caret" :title="t('queue.details.tip')">{{ openJobs.has(j.id) ? '▾' : '▸' }}</button>
        </div>
        <!-- разворот: служебное, стиль, версии, действия — всё, что было в карточке -->
        <div v-if="openJobs.has(j.id)" class="job-detail">
          <div class="job-title-row">
            <input v-if="titleEdit && titleEdit.id === j.id" v-model="titleEdit.value" class="title-edit"
                   @keydown.enter="saveRename(j)" @keydown.esc="titleEdit = null" @blur="saveRename(j)" />
            <button v-if="!(titleEdit && titleEdit.id === j.id)" class="ghost icon" :title="t('queue.rename.tip')" @click="startRename(j)"><AppIcon name="pencil" /></button>
            <!-- название уже в строке таблицы — здесь только ✎ и мета: сид, характер, свой ABC, head -->
            <span class="job-meta-extra">
              <span v-if="j.seed" class="muted">seed {{ j.seed }}</span>
              <span v-if="j.cot && j.cot !== 'full'" class="muted" :title="t('queue.cot.tip')">{{ t('queue.cot.' + j.cot) }}</span>
              <span v-if="characterLabel(j)" class="muted" :title="t('character.title')">{{ characterLabel(j) }}</span>
              <span v-if="j.req_abc" class="badge" :title="t('form.abc.tip')">свой ABC</span>
              <span v-if="headOf(j) !== j" class="badge current" :title="headOf(j).title"><AppIcon name="star-fill" /> {{ t('queue.head.badge', { id: headOf(j).id }) }}</span>
              <span v-if="j.status === 'error'" class="status error">{{ statusLabelC.error }}</span>
            </span>
            <span class="spacer"></span>
            <input v-if="folderNew && folderNew.id === j.id" v-model="folderNew.value" class="title-edit"
                   :placeholder="t('queue.folder.new.ph')" @keydown.enter="saveNewFolder(j)"
                   @keydown.esc="folderNew = null" @blur="saveNewFolder(j)" />
            <VSelect v-else :model-value="(j.folder || '').trim() || FOLDER_NONE" :options="jobFolderOptions" style="max-width: 200px"
                     :title="t('queue.folder.move.tip')" @update:model-value="(v) => pickFolder(j, v)" />
            <button v-if="j.status === 'queued' || j.status === 'running'" class="ghost small-btn"
                    :title="t('queue.cancel.tip')" @click="cancel(j.id)">{{ t('queue.cancel') }}</button>
            <button v-if="!(j.status === 'done' && j.audio_file)" class="ghost icon del" :title="t('queue.delete.tip')" @click="deleteJob(j)"><AppIcon name="x" /></button>
            <button v-if="!(j.status === 'done' && j.audio_file)" class="ghost icon" :title="t('queue.repeat.tip')" @click="reuseJob(j)"><AppIcon name="repeat" /></button>
          </div>
          <div class="job-body">
            <!-- разворот секциями: подпись слева, содержимое справа; действия — первыми -->
            <div v-if="j.status === 'done' && j.audio_file" class="job-sec">
              <span class="job-sec-h">{{ t('queue.sec.actions') }}</span>
          <div v-if="j.status === 'done' && j.audio_file" class="job-actions job-sec-body">
            <button class="play-main" :class="{ 'is-playing': isPlaying('m' + headOf(j).id) }" :disabled="playBusy['m' + headOf(j).id]" @click="togglePlay(headOf(j))">
              <template v-if="playBtn('m' + headOf(j).id) === '…'">{{ t('queue.loading') }}</template>
            <template v-else><AppIcon :name="isPlaying('m' + headOf(j).id) ? 'stop' : 'play'" /> {{ isPlaying('m' + headOf(j).id) ? t('queue.stop') : t('queue.play') }}</template>
            </button>
            <button v-if="isPlaying('m' + headOf(j).id) && playerState.playing" class="ghost" @click="api.toggleAudio()"><AppIcon name="pause" /></button>
            <!-- всё на карточке — про основную версию песни: играть, скачать, ноты -->
            <button v-if="j.status === 'done'" class="ghost" @click="studioJob = headOf(j)">{{ t('queue.studio') }}</button>
            <details class="menu-pop">
              <summary class="ghost-btn">{{ t('queue.download') }}</summary>
              <ul class="nav-menu" @click="closeMenu">
                <li :title="t('queue.save.flac.tip')" @click="saveAudio(headOf(j))">flac</li>
                <li :title="t('queue.save.mp3.tip')" @click="!playBusy['mp3' + headOf(j).id] && saveMp3(headOf(j))">mp3</li>
                <li v-if="headOf(j).abc_file" :title="t('queue.notes.save.tip')" @click="saveAudio(headOf(j), headOf(j).abc_file)">{{ t('queue.download.abc') }}</li>
              </ul>
            </details>
            <span class="spacer"></span>
            <details class="menu-pop">
              <summary class="ghost-btn" :title="t('queue.more.tip')"><AppIcon name="more-h" /></summary>
              <ul class="nav-menu" @click="closeMenu">
                <li :title="t('queue.browser.tip')" @click="openListen(headOf(j))"><AppIcon class="nav-ico" name="globe" />{{ t('queue.browser') }}</li>
                <li v-if="headOf(j).abc_file" :title="t('queue.notes.tip')" @click="loadJobAbc(headOf(j))"><AppIcon class="nav-ico" name="music" />{{ t('queue.notes') }}</li>
                <li :title="t('queue.repeat.tip')" @click="reuseJob(j)"><AppIcon class="nav-ico" name="repeat" />{{ t('queue.repeat') }}</li>
                <li class="danger" :title="t('queue.delete.tip')" @click="deleteJob(j)"><AppIcon class="nav-ico" name="x" />{{ t('queue.delete') }}</li>
              </ul>
            </details>
          </div>
            </div>
            <!-- стиль по смыслу: язык, жанр, ритм… — таблица «что где»; подпись = переключатель -->
            <div v-if="j.style" class="job-sec">
              <button class="ghost job-sec-h job-sec-toggle" :title="t('queue.style.tip')" @click="toggleStyle(j.id)">{{ openStyle.has(j.id) ? '▾' : '▸' }} {{ t('queue.sec.style') }}</button>
              <div class="job-sec-body">
                <span v-if="!openStyle.has(j.id)" class="style-sum" :title="j.style">{{ styleRows(j.style).map(([, parts]) => parts.join(', ')).join(' · ') }}</span>
                <div v-else class="style-tags" :title="j.style">
                  <template v-for="[slot, parts] in styleRows(j.style)" :key="slot">
                    <span class="style-tag-key">{{ styleTagLabel(slot) }}</span>
                    <span class="style-tag-val">{{ parts.join(', ') }}</span>
                  </template>
                </div>
              </div>
            </div>
            <!-- версии/миксы/материал: подпись = переключатель, стрелка = состояние -->
            <div v-if="kidCount(j)" class="job-sec">
              <button class="ghost job-sec-h job-sec-toggle" :class="{ on: openKids.has(j.id) }"
                      :title="t('queue.kids.tip')" @click="toggleKids(j.id)">
                {{ openKids.has(j.id) ? '▾' : '▸' }} {{ t('queue.sec.versions') }} · {{ kidCount(j) }}</button>
              <div class="job-sec-body">
                <div v-if="openKids.has(j.id)" class="job-kids">
                <div v-for="v in [j, ...kidResults(j.id)]" :key="v.id" class="job-kid" :class="{ current: headOf(j).id === v.id, playing: isPlaying('m' + v.id) }">
                  <span v-if="headOf(j).id === v.id" class="badge current"><AppIcon name="star-fill" /> {{ t('queue.head.main') }}</span>
                  <button v-else-if="v.status === 'done'" class="ghost small-btn" :title="t('queue.head.make.tip')"
                          @click="makeHead(j, v)"><AppIcon name="star" /> {{ t('queue.head.make') }}</button>
                  <span class="muted">#{{ v.id }}</span>
                  <span>{{ v.id === j.id ? t('queue.kids.original') : v.title }}</span>
                  <span v-if="v.id !== j.id" class="badge">{{ t('queue.role.' + v.role) }}</span>
                  <span v-if="v.vocal_leak" class="badge warn" :title="leakTip(v)"><AppIcon name="alert" /></span>
                  <span v-if="v.id !== j.id" class="status" :class="v.status">{{ statusLabelC[v.status] || v.status }}</span>
                  <span v-if="v.duration_sec" class="muted">{{ fmtDur(v.duration_sec) }}</span>
                  <span v-if="fmtWhen(v.created_at)" class="muted" :title="v.created_at">{{ fmtWhen(v.created_at) }}</span>
                  <span class="spacer"></span>
                  <button v-if="v.status === 'done' && v.audio_file" class="ghost small-btn" :class="{ 'is-playing': isPlaying('m' + v.id) }" @click="togglePlay(v)">
                    <AppIcon :name="isPlaying('m' + v.id) ? 'stop' : 'play'" /> {{ isPlaying('m' + v.id) ? t('queue.stop') : t('queue.play') }}
                  </button>
                  <button v-if="v.status === 'done'" class="ghost small-btn" @click="studioJob = v">студия →</button>
                  <!-- подсказка — причина падения версии, если она есть -->
                  <button v-if="v.id !== j.id && canRetry(v)" class="ghost small-btn" :title="v.error || t('queue.retry.tip')" @click="retry(v)">{{ t('queue.retry') }}</button>
                  <button v-if="v.id !== j.id && v.status !== 'running'" class="ghost icon del" :title="t('queue.delete.tip')" @click="deleteJob(v)"><AppIcon name="x" /></button>
                </div>
                <details v-if="(kidMixes[j.id] || []).length" class="job-material">
                  <summary>{{ t('queue.kids.mixes', { n: kidMixes[j.id].length }) }}</summary>
                  <div v-for="(v, n) in kidMixes[j.id]" :key="v.file" class="job-kid">
                    <span v-if="n === 0" class="badge">{{ t('queue.kids.latest') }}</span>
                    <span>{{ mixName(j.id, v) }}</span>
                    <span v-if="fmtWhen(v.created_at)" class="muted" :title="v.created_at">{{ fmtWhen(v.created_at) }}</span>
                    <span class="spacer"></span>
                    <button class="ghost small-btn" @click="playMix(j, v)">{{ playBtn('mix' + j.id + ':' + v.file) }}</button>
                  </div>
                </details>
                <details v-if="kidMaterial(j.id).length" class="job-material material">
                  <summary>{{ t('queue.kids.material', { n: kidMaterial(j.id).length }) }}</summary>
                  <div v-for="k in kidMaterial(j.id)" :key="k.id" class="job-kid" :class="{ playing: isPlaying('m' + k.id) }">
                    <span>#{{ k.id }}</span>
                    <span>{{ k.title }}</span>
                    <span class="badge">{{ t('queue.role.' + (k.role || (k.overdub_of ? 'overdub' : 'other'))) }}</span>
                    <span>{{ statusLabelC[k.status] || k.status }}</span>
                    <span v-if="fmtWhen(k.created_at)" class="muted" :title="k.created_at">{{ fmtWhen(k.created_at) }}</span>
                    <span class="spacer"></span>
                    <button v-if="k.status === 'done' && k.audio_file" class="ghost small-btn" :class="{ 'is-playing': isPlaying('m' + k.id) }" @click="togglePlay(k)">
                      <AppIcon :name="isPlaying('m' + k.id) ? 'stop' : 'play'" /> {{ isPlaying('m' + k.id) ? t('queue.stop') : t('queue.play') }}
                    </button>
                    <button v-if="k.status !== 'running'" class="ghost icon del" :title="t('queue.delete.tip')" @click="deleteJob(k)"><AppIcon name="x" /></button>
                  </div>
                </details>
              </div>
              </div>
            </div>
          <p v-if="j.error" class="error">{{ j.error }}</p>
          <button v-if="canRetry(j)" class="ghost small-btn" :title="t('queue.retry.tip')" @click="retry(j)">{{ t('queue.retry') }}</button>
          </div>
        </div>
      </article>
        </div><!-- /job-table -->
        <div v-if="qPageMax > 1" class="pager">
          <button class="ghost small-btn" :disabled="qPageNow <= 1" @click="qPage = qPageNow - 1">←</button>
          <span class="muted">{{ qPageNow }} / {{ qPageMax }}</span>
          <button class="ghost small-btn" :disabled="qPageNow >= qPageMax" @click="qPage = qPageNow + 1">→</button>
        </div>
      </template>
    </section>
  </main>
  </div>

  <!-- статус-бар: зеркало шапки снизу — модель, VRAM, очередь рендеров, очередь к GPU, текущая джоба -->
  <footer v-if="stats" class="statusbar">
    <span class="health-dot" :class="stats.model_loaded ? 'up' : 'down'"
          :title="stats.model_loaded ? t('footer.model.loaded') : t('footer.model.cold')"></span>
    <span v-if="stats.vram_total" class="sb-vram" :title="t('footer.vram.tip')">
      <span class="sb-vram-bar">
        <span class="sb-vram-fill" :class="{ hot: stats.vram_used / stats.vram_total > 0.9 }"
              :style="{ width: Math.min(100, 100 * stats.vram_used / stats.vram_total) + '%' }"></span>
      </span>
      <span class="sb-text">{{ (stats.vram_used / 1024).toFixed(1) }}/{{ Math.round(stats.vram_total / 1024) }} {{ t('footer.vram.gb') }}</span>
    </span>
    <span v-if="stats.queue && stats.queue.queued" class="sb-text">· {{ t('footer.queue', { n: stats.queue.queued }) }}</span>
    <span v-if="stats.gpu_waiting" class="sb-text">· {{ t('footer.gpu_queue', { n: stats.gpu_waiting }) }}</span>
    <span v-if="stats.running" class="sb-text sb-job" :title="stats.running.title">
      · #{{ stats.running.job_id }}
      <template v-if="stats.running.progress_pct != null">{{ stats.running.progress_pct }}%</template>
      <template v-else>{{ t('queue.progress.' + (stats.running.stage || 'plan')) }}</template>
      <template v-if="stats.running.tok_per_s"> · {{ stats.running.tok_per_s }} {{ t('queue.progress.tps') }}</template>
    </span>
  </footer>

  <!-- страницы-модалки поверх основного контента: ✕/Esc/клик по фону закрывают -->
  <!-- библиотека — после формы нового трека: открывается поверх неё, закрылась — форма на месте -->
  <!-- новый трек — такая же страница-модалка, как голоса и свои треки -->
  <div v-if="newTrackPage" class="modal-backdrop page-backdrop" @click.self="newTrackPage = false">
    <section class="panel page-modal form newtrack-modal">
      <div class="page-modal-head">
        <h2>{{ t('form.title') }}</h2>
        <button class="ghost icon" :title="t('common.close')" @click="newTrackPage = false"><AppIcon name="x" /></button>
      </div>
      <div class="page-modal-body">
        <div class="lib-row" :title="t('form.lib.tip')">
          <VSelect v-model="libGroup" :options="libGroupOptions" :placeholder="t('form.lib.group')" @update:model-value="onLibGroupChange()" />
          <VSelect v-model="libStyle" :options="libStyleOptions" :disabled="!libGroup" :placeholder="t('form.lib.style')" @update:model-value="onLibStyleChange()" />
          <button v-if="currentItem" class="ghost small-btn" :title="t('form.lib.exact.tip')" @click="onLibExact">{{ t('form.lib.exact') }}</button>
          <button class="ghost small-btn" :title="t('form.lib.save.tip')" @click="saveStyleToLibrary">{{ t('form.lib.save') }}</button>
          <button class="ghost small-btn" :title="t('form.lib.manage.tip')" @click="libraryPage = true"><AppIcon name="sliders" /></button>
        </div>
        <input v-model="title" :placeholder="t('form.name')" style="margin-top:8px" />

        <!-- «Импорт ABC»: готовый план как скелет трека; жанр, инструменты, ритм — из формы -->
        <div class="abc-row" :title="t('form.abc.tip')">
          <button class="toggle small-btn" :class="{ on: abcOpen || !!customAbc.trim() }"
                  @click="abcOpen = !abcOpen">{{ t('form.abc.btn') }}</button>
          <span v-if="customAbc.trim() && !abcOpen" class="muted">
            {{ t('form.abc.on') }} · {{ customAbc.trim().split('\n').length }} {{ t('form.abc.lines') }}
          </span>
          <button v-if="customAbc.trim()" class="ghost small-btn" @click="customAbc = ''">{{ t('form.abc.clear') }}</button>
        </div>
        <textarea v-if="abcOpen" v-model="customAbc" rows="8" class="abc" spellcheck="false"
                  :placeholder="t('form.abc.ph')"></textarea>

        <div class="slots">
          <div v-for="key in slotKeys" :key="key" class="slot-box" :title="t('slot.' + key + '.tip')">
            <span>{{ t('slot.' + key + '.label') }}</span>
            <input v-model="slots[key]" :placeholder="t('slot.' + key + '.hint')"
                   @focus="openSlot = key" @click.stop @input="openSlot = key" />
            <ul v-if="openSlot === key && slotFiltered(key).length" class="slot-drop">
              <li v-for="[ru] in slotFiltered(key)" :key="ru"
                  @mousedown.prevent="slots[key] = ru; openSlot = ''">{{ ru }}</li>
            </ul>
          </div>
          <label>
            <span>BPM</span>
            <input v-model.number="slots.bpm" type="number" min="40" max="250" placeholder="—" />
          </label>
        </div>

        <div class="lib-row" :title="t('form.voice.tip')">
          <VSelect v-model="voicePick" :options="voiceOptions" :disabled="!voiceCards.length"
                   :placeholder="voiceCards.length ? t('form.voice.ph') : t('form.voice.empty')"
                   @update:model-value="onVoicePick()" />
        </div>

        <details>
          <summary>
            {{ t('form.styleline') }}
            <span v-if="styleOverride" class="muted">{{ t('form.styleline.manual') }}</span>
            <span v-else class="muted">{{ t('form.styleline.auto') }}</span>
          </summary>
          <textarea v-model="styleLine" rows="3" :placeholder="t('form.styleline.ph')"></textarea>
          <button v-if="styleOverride" class="ghost small-btn" style="margin-top:6px"
                  :title="t('form.styleline.follow.tip')"
                  @click="styleOverride = ''">{{ t('form.styleline.follow') }}</button>
        </details>

        <details>
          <summary>{{ t('form.rack') }} <span class="muted">({{ rackSel.length }})</span></summary>
          <div class="rack">
            <div v-for="g in rackGroups" :key="g.id" class="rack-group">
              <div class="rack-gname">{{ g.name }}</div>
              <div v-for="it in g.items" :key="it.id" class="rack-item">
                <label class="rack-pick">
                  <input type="checkbox" :checked="rackSel.some(r => r.id === it.id)" @change="rackToggle(it.id)" />
                  {{ it.name }}
                </label>
                <VSelect v-if="rackSel.some(r => r.id === it.id)"
                        :model-value="rackSel.find(r => r.id === it.id).effect"
                        :options="rackEffects.map((e) => ({ value: e.id, label: e.name }))"
                        style="max-width: 150px"
                        @update:model-value="(v) => rackSetEffect(it.id, v)" />
              </div>
            </div>
          </div>
        </details>

        <div class="lyrics-head">
          <label class="lyrics-label">{{ t('form.lyrics') }}</label>
          <span class="lyrics-tools">
            <button class="toggle" :class="{ on: noLyrics }" :title="t('form.nowords.tip')" @click="noLyrics = !noLyrics">{{ t('form.nowords') }}</button>
            <select v-if="noLyrics" v-model="durMode" class="dur-select" title="Длина инструментала задаётся числом секций [Instrumental]">
              <option v-for="o in durOptions" :key="o.id" :value="o.id">{{ t('dur.' + o.id) }}</option>
            </select>
            <button v-if="!noLyrics" class="ghost small-btn" @click="copOpen = true"><AppIcon name="pencil" /> {{ t('form.copilot') }}</button>
            <button v-if="!noLyrics" class="ghost small-btn" :disabled="!!lyrBusy"
                    :title="t('lyrics.rec.tip')" @click="recognizeLyrics">
              <template v-if="lyrBusy === 'rec'">…</template><template v-else><AppIcon name="music" /> {{ t('lyrics.rec') }}</template></button>
            <button v-if="!noLyrics" class="ghost small-btn" :disabled="!!lyrBusy || !lyrics.trim()"
                    :title="t('lyrics.adapt.tip')" @click="adaptLyrics">
              {{ lyrBusy === 'adapt' ? '…' : t('lyrics.adapt') }}</button>
            <span v-if="lyrErr" class="error">{{ lyrErr }}</span>
          </span>
        </div>
        <textarea v-model="lyrics" rows="10" :disabled="noLyrics" :placeholder="noLyrics ? t('form.nowords.ph') : ''"></textarea>

        <!-- итоговая строка стиля, которая уйдёт модели, и её перевод на английский -->
        <div class="row style-out">
          <span class="style-out-cap">{{ t('form.styleout') }}:</span>
          <span class="compiled" :title="compiledStyle">{{ translateBusy ? t('form.translating') : (compiledStyle || t('form.style.empty')) }}</span>
          <label class="autotr" :title="t('form.autotr.tip')">
            <input type="checkbox" v-model="autoTranslate" /> {{ t('form.autotr') }}
          </label>
        </div>

        <!-- редкие настройки свёрнуты; изменённое видно в заголовке, чтобы не забыть -->
        <details class="form-advanced">
          <summary>{{ t('form.advanced') }}<span v-if="advancedSummary" class="muted"> · {{ advancedSummary }}</span></summary>
          <div class="row">
            <label class="seed" :title="t('form.seed.tip')">seed <input v-model.number="seed" type="number" :placeholder="t('form.seed.ph')" /></label>
            <!-- «без плана» в форме нет: он отключает студию (ноты, приёмы, драматургию); в API/MCP остался -->
            <div class="cot-radios" :title="t('form.cot.tip')">
              <span class="arc-title">{{ t('form.cot.title') }}:</span>
              <button v-for="c in ['full', 'melody']" :key="c" class="toggle small-btn"
                      :class="{ on: cot === c }" @click="cot = c">{{ t('form.cot.' + c) }}</button>
            </div>
          </div>

          <div class="row arc-row">
            <span class="arc-title">{{ t('arc.title') }}:</span>
            <button v-for="a in ['', 'build', 'wave', 'burst']" :key="a" class="toggle small-btn"
                    :class="{ on: arcKind === a }" :title="t('arc.' + (a || 'flat') + '.tip')"
                    @click="arcKind = a">{{ t('arc.' + (a || 'flat')) }}</button>
          </div>

          <div class="row character-row">
            <span class="arc-title">{{ t('character.title') }}:</span>
            <label class="character-knob" :title="t('character.temperature.tip')">
              {{ t('character.temperature') }}
              <input type="range" v-model.number="temperature" :min="CHARACTER.temperature.min"
                     :max="CHARACTER.temperature.max" :step="CHARACTER.temperature.step" />
              <span class="character-val">{{ temperature }}</span>
            </label>
            <label class="character-knob" :title="t('character.cfg.tip')">
              {{ t('character.cfg') }}
              <input type="range" v-model.number="cfgScale" :min="CHARACTER.cfg.min"
                     :max="CHARACTER.cfg.max" :step="CHARACTER.cfg.step" />
              <span class="character-val">{{ cfgScale }}</span>
            </label>
            <button class="ghost small-btn" :disabled="characterDefault" @click="characterReset">{{ t('character.reset') }}</button>
          </div>
        </details>

        <div class="actions">
          <button class="primary" :disabled="submitting || !canSubmit" @click="submit">
            {{ submitting ? t('form.submitting') : t('form.submit') }}
          </button>
          <button class="primary alt" :disabled="submitting || !canSubmit" @click="submitFan(5)" :title="t('form.fan.tip')">
            {{ t('form.fan') }}
          </button>
          <button class="ghost" :disabled="planBusy || !canSubmit" @click="notesOpen"
                  :title="t('form.notes.tip')">
            {{ planBusy ? t('form.planning') : t('form.notes') }}
          </button>
          <button class="ghost" :disabled="submitting || !canSubmit" @click="submitDraft"
                  :title="t('form.draft.tip')">
            {{ t('form.draft') }}
          </button>
          <span v-if="submitErr" class="error submit-err" :title="submitErr">{{ submitErr }}</span>
        </div>
      </div>
    </section>
  </div>

  <SettingsPage v-if="settingsPage" v-model:server-url="serverURL"
                @close="settingsPage = false" @saved="refresh" />
  <CorpusPage v-if="corpusPage"
              @close="corpusPage = false"
              @imported="onImported"
              @apply-style="applyProfileStyle"
              @apply-abc="(abc) => setPlanAbc(abc, { seed: null, seconds: null, truncated: false, fromProfile: true })"
              @style-to-library="profileStyleToLibrary" />
  <VoicesPage v-if="voicesPage"
              @close="voicesPage = false; loadVoiceCards()"
              @apply-voice="applyVoice" />

  <LibraryPage v-if="libraryPage" @close="libraryPage = false" />

  <PlanModal v-model:abc="planAbc" :open="planOpen" :busy="planBusy" :err="planErr"
             :info="planInfo"
             @close="planOpen = false" @use="usePlanAbc" @new-plan="makePlan" @from-track="transcribeFromTrack" />
  <CopilotModal :open="copOpen" :style="compiledStyle" :example="lyrics" :lang="slots.language" :slots="slots"
                @close="copOpen = false" @insert="onCopInsert" />
  <MetricsModal ref="metricsModal" :jobs="jobs" />
  <WelcomeModal v-if="welcome" :tab="welcome" @close="closeWelcome" />
  <ConfirmModal />
</template>

<style>
/* Стили приложения — глобальные: экранные компоненты (components/) рендерятся
   внутри этого корня и пользуются теми же классами. */
/* раскладка окна: шапка сверху неподвижна, под ней .app-body со своей прокруткой */
html, body, #app { height: 100%; }
body { overflow: hidden; }
#app { display: flex; flex-direction: column; }
.app-body { flex: 1 1 auto; min-height: 0; overflow: auto; }
header { flex: 0 0 auto; }
header {
  /* выше панелей main (стекинг-контексты из backdrop-filter), но ниже модалок (z-index 70) */
  /* одна строка при любой ширине окна (min 900): на узком ужимается второстепенное, плеер не переносится */
  position: relative; z-index: 5; flex-wrap: nowrap;
  display: flex; align-items: center; gap: 14px; padding: 10px 16px;
  background: color-mix(in srgb, var(--panel2) 78%, transparent);
  backdrop-filter: blur(14px) saturate(1.15);
  -webkit-backdrop-filter: blur(14px) saturate(1.15);
  border-bottom: 1px solid var(--bevel-lo);
  box-shadow: 0 1px 4px rgba(0,0,0,.35);
}
h1 {
  font-size: 16px; margin: 0; letter-spacing: 2px; text-transform: uppercase;
  color: var(--lcd-text); text-shadow: 0 0 10px var(--lcd-glow);
}
h2 {
  font-size: 13px; margin: -16px -16px 12px; padding: 6px 12px;
  color: var(--text); text-transform: uppercase; letter-spacing: 1.5px;
  background: linear-gradient(180deg, var(--panel3), var(--panel2));
  border-bottom: 1px solid var(--bevel-lo);
  text-shadow: 1px 1px 0 rgba(0,0,0,.4);
}
.page-head h2 { margin: 0; border-bottom: none; background: none; text-shadow: none; padding: 0; }
.icon-btn {
  background: none; border: none; box-shadow: none; cursor: pointer;
  color: var(--muted); font-size: 15px; padding: 2px 0; font-weight: 400;
  /* все иконки шапки — одинаковые квадраты: ☀/☾, EN/RU, ⋮ */
  width: 30px; height: 26px; line-height: 22px; text-align: center;
}
.icon-btn:hover { color: var(--text); filter: none; transform: none; }
.health-dot {
  width: 10px; height: 10px; border-radius: 50%; flex: none;
  border: 1px solid var(--bevel-lo); cursor: help;
}
.health-dot.up { background: var(--ok); box-shadow: 0 0 8px var(--ok); }
.health-dot.down { background: var(--err); box-shadow: 0 0 8px var(--err); }
/* статус-бар — зеркало шапки снизу: модель, VRAM, очереди, текущая джоба */
.statusbar {
  position: relative; z-index: 5; flex: 0 0 auto;
  display: flex; align-items: center; gap: 10px; padding: 4px 16px;
  background: color-mix(in srgb, var(--panel2) 78%, transparent);
  backdrop-filter: blur(14px) saturate(1.15);
  -webkit-backdrop-filter: blur(14px) saturate(1.15);
  border-top: 1px solid var(--bevel-lo);
  box-shadow: 0 -1px 4px rgba(0,0,0,.35);
  font-size: 11.5px; color: var(--muted);
}
.statusbar .health-dot { width: 8px; height: 8px; }
.sb-text { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.sb-job { max-width: 46ch; }
.sb-vram { display: flex; align-items: center; gap: 6px; }
.sb-vram-bar {
  width: 80px; height: 5px; border-radius: 3px; overflow: hidden; flex: none;
  background: var(--bevel-lo);
}
.sb-vram-fill { display: block; height: 100%; background: var(--ok); border-radius: 3px; }
.sb-vram-fill.hot { background: var(--err); }
.settings-page { display: flex; justify-content: center; align-items: flex-start; }
.panel.lib { position: relative; z-index: 3; } /* выпадашки VSelect выше соседних панелей (стекинг-контексты из backdrop-filter) */
.panel { width: 640px; max-width: 100%; display: flex; flex-direction: column; gap: 12px; }
/* очередь — правая колонка сетки: растягивается на всё свободное место */
.track-list { width: auto; }
/* приёмы студии — группы по смыслу: подпись сверху, кнопки заворачиваются
   внутри своей карточки; фон плотный, группа читается на фоне секции */
.trick-bar { display: flex; flex-wrap: wrap; gap: 8px; align-items: stretch; margin-top: 0; }
.trick-group {
  display: flex; flex-direction: column; gap: 5px;
  padding: 6px 8px 7px; border: 1px solid var(--border); border-radius: 6px;
  background: var(--panel2);
}
.trick-cap {
  font-size: 10px; text-transform: uppercase; letter-spacing: .5px;
  color: var(--muted); user-select: none; white-space: nowrap;
}
.trick-btns { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.trick-btns .cont-style { min-width: 170px; flex: 0 1 220px; }
.trick-group.assemble { flex-basis: 100%; flex-direction: row; align-items: center; gap: 10px; }
.trick-group.assemble .trick-btns { flex: 1; }
.trick-hint { font-size: 11px; margin: 4px 0 0; }
/* живой отклик: пульсирующая нота, пока кусок генерится */
.pulse { display: inline-block; animation: trickpulse 1.2s ease-in-out infinite; }
@keyframes trickpulse { 0%, 100% { opacity: .35 } 50% { opacity: 1 } }
/* такты с приёмами: точка в углу + рамка акцентом, метка живёт и в выделении */
.roll-cell.trick { border-color: var(--accent); box-shadow: inset 0 0 0 1px var(--accent); }
.roll-cell.trick::after { content: ''; position: absolute; top: 1px; right: 1px; width: 4px; height: 4px; border-radius: 50%; background: var(--accent); }
.plan-marks { font-size: 13px; margin: 0; color: var(--accent); }
.plan-marks .muted { display: block; font-size: 12px; }
.queue-tools { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
.queue-tools input { flex: 1 1 150px; min-width: 0; }
.pager { display: flex; align-items: center; gap: 10px; justify-content: center; margin-top: 6px; font-size: 13px; }
.nav-wrap { position: relative; }
.nav-menu {
  position: absolute; top: 100%; right: 0; z-index: 40; margin: 2px 0 0; padding: 4px 0;
  list-style: none; background: var(--panel); border: 1px solid var(--border); border-radius: 6px;
  min-width: 175px; box-shadow: 0 8px 24px rgba(0,0,0,.4); white-space: nowrap;
}
.nav-menu li { padding: 6px 12px; font-size: 13px; cursor: pointer; display: flex; align-items: center; gap: 8px; }
.nav-menu li:hover { background: var(--panel2); }
/* колонка под иконку фиксированной ширины — подписи выровнены, эмодзи разной ширины не толкают текст */
.nav-ico { flex: none; width: 20px; text-align: center; }
.set-h { margin: 14px 0 2px; font-size: 13px; }
.set-row { display: flex; align-items: center; gap: 10px; }
.set-row input, .set-row select { flex: 1; min-width: 0; width: 100%; }
/* галочки и переключатели в строке настроек не растягиваются в полосу */
.set-row input[type=checkbox], .set-row input[type=radio] { flex: none; width: auto; }
.slot-box { display: flex; flex-direction: column; position: relative; }
.slot-box > span { font-size: 11px; color: var(--muted); margin-bottom: 2px; }
.slot-drop {
  position: absolute; top: 100%; left: 0; right: 0; z-index: 30; margin: 2px 0 0; padding: 4px 0;
  list-style: none; background: var(--panel); border: 1px solid var(--border); border-radius: 6px;
  max-height: 180px; overflow-y: auto; box-shadow: 0 8px 24px rgba(0,0,0,.4);
}
.slot-drop li { padding: 4px 10px; font-size: 12px; cursor: pointer; }
.slot-drop li:hover { background: var(--panel2); }
.lib-actions { margin-top: 8px; display: flex; gap: 8px; }
.lib-row { display: flex; gap: 6px; align-items: center; }
.lib-row > :first-child { flex: 1.1; min-width: 0; }
.lib-row > :nth-child(2) { flex: 1.6; min-width: 0; }
.lib-row .small-btn { flex: none; padding: 4px 8px; }
.lib .select, .lib select { width: 100%; }
.lib-group { margin-bottom: 10px; }
.lib-group-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.lib-group-items { font-size: 11px; line-height: 1.5; margin-top: 2px; }
.lib-style-row { display: flex; align-items: center; gap: 8px; font-size: 13px; padding: 2px 0; }
.lib-name-input { max-width: 200px; font-size: 13px; padding: 3px 8px; }
.lib-style-text { flex: 1; font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.set-label { width: 110px; flex: none; font-size: 12px; color: var(--muted); }
.set-hint { font-size: 12px; }
.set-info { font-size: 11px; line-height: 1.6; word-break: break-all; }
.set-actions { display: flex; gap: 8px; margin-top: 8px; }
.ok { color: var(--ok); font-size: 12px; }
/* плеер — гибкая часть шапки: всё место между левыми кнопками и «＋ Новый трек»
   (раньше — абсолютный центр фиксированной ширины: полоса короткая, по краям пусто) */
.player-center { flex: 1 1 auto; min-width: 0; }
/* узкое окно: меньше промежутки, короче громкость и название, у «＋ Новый трек» — только «＋» */
@media (max-width: 1150px) {
  header { gap: 8px; padding: 8px 12px; }
  /* header-префикс: базовые правила плеера стоят ниже и иначе перебили бы эти */
  header .playerbar { gap: 5px; }
  header .playerbar .vol input { width: 50px; }
  header .playerbar .now { max-width: 120px; }
  h1 { letter-spacing: 1px; }
}
@media (max-width: 1000px) {
  .new-track-txt { display: none; }
  .new-track-btn { padding: 6px 12px; font-size: 16px; }
}
.new-track-btn { margin-left: auto; }
.playerbar { display: flex; align-items: center; gap: 8px; font-size: 12px; }
/* полоса прокрутки растягивается на свободное место шапки; под ней —
   метки минут-секунд: плеер здесь — навигация по треку, не просто индикатор */
.playerbar .seek-wrap { flex: 1 1 auto; min-width: 80px; display: flex; flex-direction: column; }
.playerbar .seek { width: 100%; }
.seek-ticks { position: relative; height: 11px; margin-top: -1px; }
.seek-tick {
  position: absolute; transform: translateX(-50%); top: 0;
  font-size: 9px; color: var(--muted); font-variant-numeric: tabular-nums;
  pointer-events: none; user-select: none;
}
.playerbar .vol { display: flex; align-items: center; gap: 4px; color: var(--muted); }
.playerbar .vol input { width: 80px; }
/* название — фиксированной ширины: длина названия не двигает полосу прокрутки;
   длинное бежит строкой (две копии подряд, сдвиг на половину — без шва) */
/* ширина по названию (не больше 220 px, длиннее — бегущая строка): короткое «#523» не оставляет дыры */
.playerbar .now { flex: 0 1 auto; max-width: 220px; min-width: 0; overflow: hidden; white-space: nowrap; }
.playerbar .now-track { display: inline-flex; }
.playerbar .now-track.scroll { animation: now-marquee linear infinite; }
.playerbar .now-track.scroll .now-text { padding-right: 3em; }
.playerbar .now:hover .now-track { animation-play-state: paused; }
@keyframes now-marquee { from { transform: translateX(0); } to { transform: translateX(-50%); } }
@media (prefers-reduced-motion: reduce) {
  .playerbar .now-track.scroll { animation: none; }
  .playerbar .now { text-overflow: ellipsis; }
}
.playerbar .pos {
  font-variant-numeric: tabular-nums; white-space: nowrap; flex: none;
  /* ширина под самую длинную надпись («10:00 / 10:00»): «→ 1:23» при наведении
     короче — без фиксированной ширины полоса прокрутки рядом прыгала */
  width: 13ch; box-sizing: content-box; text-align: center;
  background: var(--lcd-bg); color: var(--lcd-text);
  font-family: 'DejaVu Sans Mono', 'Consolas', monospace;
  padding: 2px 8px; border-radius: 2px; border: 1px solid var(--border);
  box-shadow: inset 0 0 6px rgba(0,0,0,.55); text-shadow: 0 0 6px var(--lcd-glow);
}
.playerbar .ghost {
  min-width: 30px; width: auto; height: 24px; padding: 0 4px;
  display: inline-flex; align-items: center; justify-content: center;
  color: var(--text);
  background: linear-gradient(180deg, var(--panel3), var(--panel2));
  border: 1px solid var(--border);
  border-top-color: var(--bevel-hi);
  border-left-color: var(--bevel-hi);
  font-size: 12px; line-height: 1;
}
.playerbar .ghost:hover { color: var(--lcd-text); border-color: var(--lcd-text); }
.spacer { flex: 1; }

main {
  display: grid;
  /* одна страница за раз: список треков или форма нового трека */
  grid-template-columns: minmax(0, 1fr);
  gap: 20px; padding: 20px 24px; max-width: 1800px; margin: 0 auto;
}
/* тело модалки прокручивается само: части формы не сжимаются (иначе стих — в одну строку) */
.newtrack-modal .page-modal-body > * { flex-shrink: 0; }
.home-link { cursor: pointer; display: inline-flex; align-items: center; gap: 8px; white-space: nowrap; }
/* значок приложения в шапке — тот же, что иконка окна (build/appicon.png) */
.app-logo { width: 26px; height: 26px; flex: none; filter: drop-shadow(0 0 4px var(--lcd-glow)); }
.new-track-btn { font-weight: 600; padding: 6px 14px; }
.new-track-btn.on { outline: 2px solid var(--accent, currentColor); outline-offset: 1px; }
.left-col { display: flex; flex-direction: column; gap: 20px; min-width: 0; }
.left-col input, .left-col textarea { min-width: 0; max-width: 100%; }
.panel {
  background: color-mix(in srgb, var(--panel) 88%, transparent);
  backdrop-filter: blur(14px) saturate(1.12);
  -webkit-backdrop-filter: blur(14px) saturate(1.12);
  border-radius: 2px; padding: 16px;
  border: 1px solid var(--border);
  border-top-color: var(--bevel-hi); border-left-color: var(--bevel-hi);
  border-bottom-color: var(--bevel-lo); border-right-color: var(--bevel-lo);
  box-shadow: inset 1px 1px 0 rgba(255,255,255,.05), 0 2px 6px rgba(0,0,0,.25);
}

.ghost.icon.del { color: var(--muted); }
.ghost.icon.del:hover { color: var(--err); }
.primary.danger { background: var(--err); }
.confirm-modal { width: min(480px, 92vw); }

.slots { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 8px 12px; margin: 12px 0; }
.slots label { display: flex; flex-direction: column; gap: 3px; font-size: 12px; color: var(--muted); }
.slots input { font-size: 13px; }

details { margin: 8px 0; color: var(--muted); font-size: 13px; }
details textarea { width: 100%; margin-top: 6px; }

.lyrics-label { display: block; font-size: 12px; color: var(--muted); margin: 10px 0 4px; }
textarea { width: 100%; resize: vertical; font-family: inherit; }

.row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin: 10px 0 6px; font-size: 12px; }
.lyrics-tools { display: flex; gap: 6px; align-items: center; }
.lyrics-tools .dur-select { max-width: 110px; font-size: 12px; }
.page-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
button.toggle { background: transparent; border: 1px solid var(--border); color: var(--muted); font-weight: 400; font-size: 12px; padding: 3px 10px; }
button.toggle.on { border-color: var(--accent); color: var(--accent); font-weight: 600; }
.seed input { width: 110px; min-width: 0; }
.arc-row { align-items: center; gap: 6px; }
.arc-title { font-size: 12px; color: var(--muted); }
.character-row { align-items: center; gap: 12px; flex-wrap: wrap; }
.character-knob { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; }
.character-knob input[type=range] { width: 110px; }
.character-val { min-width: 2.5em; font-variant-numeric: tabular-nums; }
.form-advanced > summary { cursor: pointer; font-size: 13px; color: var(--muted); padding: 4px 0; }
.form-advanced[open] > summary { margin-bottom: 6px; }
.cot-radios { display: flex; align-items: center; gap: 10px; font-size: 12px; color: var(--muted); }
.cot-radios label { display: flex; align-items: center; gap: 4px; cursor: pointer; }
.cot-title { font-weight: 600; }
.style-out { align-items: center; gap: 8px; flex-wrap: nowrap; }
.style-out-cap { flex: none; font-size: 12px; color: var(--muted); }
.compiled { flex: 1; min-width: 0; font-size: 12px; color: var(--text); margin: 0; word-break: break-word; }
.autotr { flex: none; font-size: 12px; color: var(--muted); display: flex; align-items: center; gap: 4px; cursor: pointer; }

.actions { display: flex; gap: 8px; }
.actions .primary { flex: 0 0 auto; }
.primary {
  background: var(--play-grad);
  color: var(--play-fg); padding: 6px 16px; text-shadow: none;
}
.primary.alt {
  background: linear-gradient(180deg, var(--panel3), var(--panel2));
  border: 1px solid var(--lcd-text); color: var(--lcd-text);
}
/* светлая: янтарная обводка alt-кнопки на белом блёклая — акцент темы */
[data-theme="light"] .primary.alt { border-color: var(--accent); color: var(--accent); }

.progress-wrap { display: inline-flex; align-items: center; gap: 8px; min-width: 180px; }
.progress-track { display: inline-block; width: 110px; height: 8px; border-radius: 4px;
  background: var(--panel2); border: 1px solid var(--border); overflow: hidden; position: relative; }
.progress-fill { display: block; height: 100%;
  background: linear-gradient(90deg, var(--run), var(--ok)); transition: width 1s linear; }
.progress-track.indet::after { content: ''; position: absolute; inset: 0 auto 0 -30%; width: 30%;
  background: var(--run); border-radius: 4px; animation: progress-slide 1.2s ease-in-out infinite; }
@keyframes progress-slide { to { left: 100%; } }
.progress-label { font-size: 11px; white-space: nowrap; }

/* треклист — плоский список буклетного типа: без общей рамки и зебры,
   строки разделены тонкой линией снизу; мета — моноширинным приглушённым */
.job-table { margin-bottom: 10px; }
.job {
  border: none; border-radius: 0; padding: 0; margin-bottom: 0;
  background: transparent; display: flex; flex-direction: column; overflow: hidden;
}
.job > .job-row { border-bottom: 1px solid color-mix(in srgb, var(--border) 55%, transparent); }
/* строка трека: номер, круглая play-кнопка, название, справа лёгкая мета */
.job-row {
  display: flex; align-items: center; gap: 12px;
  padding: 10px 8px; cursor: pointer; user-select: none;
}
.job-row:hover { background: color-mix(in srgb, var(--panel2) 70%, transparent); }
/* открытый трек: полоса-акцент вдоль строки + название цветом — видно издалека */
.job.open { box-shadow: inset 3px 0 0 var(--accent); }
.job.open > .job-row { background: var(--panel2); }
.job.open > .job-row .job-name { color: var(--accent); }
.job-row.open { margin-bottom: 8px; }
.job-caret { font-size: 13px; color: var(--muted); flex: none; }
.job-row:hover .job-caret { color: var(--text); }
.job.open .job-caret { color: var(--accent); }
.tl-num {
  font: 500 12.5px 'IBM Plex Mono', 'DejaVu Sans Mono', 'Consolas', monospace;
  color: var(--muted); flex: none; width: 34px; text-align: right;
}
.tl-play-cell { flex: none; width: 32px; height: 32px; display: flex; align-items: center; justify-content: center; }
/* круглая play-кнопка: контур, заливается акцентом при наведении/воспроизведении */
.tl-play {
  width: 30px; height: 30px; border-radius: 50%; padding: 0;
  border: 1px solid var(--border); background: transparent; color: var(--accent);
  display: flex; align-items: center; justify-content: center; cursor: pointer;
}
.tl-play:hover, .tl-play.is-playing { background: var(--accent); border-color: var(--accent); color: #fff; }
.tl-play:disabled { opacity: .5; cursor: default; }
.job-name { font-weight: 600; font-size: 15px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; flex: 1; }
.job-name .badge, .job-name .status { margin-left: 8px; }
.job-progress { display: inline-flex; align-items: center; gap: 6px; min-width: 0; margin-left: 8px; }
.progress-track.slim { width: 76px; height: 5px; flex: none; }
/* мета справа — моноширинная, приглушённая, не спорит с названием */
.tl-meta { font: 12.5px 'IBM Plex Mono', 'DejaVu Sans Mono', 'Consolas', monospace; color: var(--muted); white-space: nowrap; flex: none; }
.tl-pill {
  font: 12px 'IBM Plex Mono', 'DejaVu Sans Mono', 'Consolas', monospace; color: var(--muted);
  border: 1px solid var(--border); border-radius: 999px; padding: 2px 9px; flex: none; white-space: nowrap;
}
.tl-pill:hover { color: var(--accent); border-color: var(--accent); }
.kids-badge { cursor: pointer; }
/* точка-статус вместо чипа, когда играть нельзя */
.status-mini { width: 8px; height: 8px; border-radius: 50%; flex: none; background: var(--muted); }
.status-mini.running { background: var(--run); animation: trickpulse 1.2s ease-in-out infinite; }
.status-mini.error { background: var(--err); }
.status-mini.queued { background: var(--muted); }
/* разворот — вложенная карточка: отступ слева по линии названия (12px поля строки +
   30px колонки ▶), своя рамка и фон — видно, к какой строке относится, и
   несколько открытых треков не сливаются с соседями */
.job-detail { display: flex; flex-direction: column;
  margin: 0 12px 10px 42px; border: 1px solid var(--border); border-radius: 6px;
  background: var(--panel); overflow: hidden; }
/* открытый трек отодвинут от соседних строк — читается отдельным блоком */
.job-table .job.open { margin: 6px 0; border-top: 1px solid var(--border); border-bottom: 1px solid var(--border); }
.job-table .job.open:first-child { margin-top: 0; border-top: none; }
.job-table .job.open:last-child { margin-bottom: 0; border-bottom: none; }
.job-detail .job-meta-extra { display: flex; align-items: center; gap: 6px 10px; flex-wrap: wrap;
  font-size: 12px; color: var(--muted); margin-left: 10px; min-width: 0; }
/* название — шапка карточки во всю ширину */
.job-title-row {
  display: flex; align-items: center; gap: 8px; padding: 7px 12px;
  background: var(--panel); border-bottom: 1px solid var(--border);
  box-shadow: inset 1px 1px 0 rgba(255,255,255,.05);
}
.job-title-row .title-edit { flex: 1; min-width: 280px; font-weight: 600; }
.job-body { padding: 4px 12px 8px; }
/* секции разворота: подпись слева (одна ширина колонки), содержимое справа */
.job-sec { display: grid; grid-template-columns: 132px minmax(0, 1fr); gap: 10px; align-items: baseline;
  padding: 7px 0; }
.job-sec + .job-sec { border-top: 1px dashed var(--border); }
.job-sec-h { font-size: 11px; text-transform: uppercase; letter-spacing: .4px; color: var(--muted);
  white-space: nowrap; user-select: none; }
/* подпись-переключатель — маленькая ghost-кнопка (рамка и фон — от button.ghost в style.css) */
span.job-sec-h { padding: 4px 11px; }   /* текст подписи — на одной линии с кнопками-подписями */
button.job-sec-h { padding: 3px 10px; text-align: left; justify-self: start; }
button.job-sec-h:hover, button.job-sec-h.on { color: var(--text); }
.job-sec-body { min-width: 0; }
.job-sec-body .style-tags { margin: 0; }
.job-sec-body .style-sum { display: block; }
.job-sec-body .job-kids { margin: 0; }
/* пока открыт хоть один трек, закрытые строки приглушены — взгляд держится на открытом */
.job-table.has-open .job:not(.open) { opacity: .6; transition: opacity .15s; }
.job-table.has-open .job:not(.open):hover { opacity: 1; }
.collapse-all { margin-left: 8px; }
.status { font-size: 12px; padding: 2px 8px; border-radius: 10px; background: var(--border); }
.status.done { background: rgba(255,190,61,.16); color: var(--lcd-text); text-shadow: 0 0 8px var(--lcd-glow); }
/* светлая: чип «готово» — мини-тёмный LCD, янтарный текст на белом не читается */
[data-theme="light"] .status.done { background: var(--lcd-bg); }
.status.running { background: rgba(217,160,61,.2); color: var(--run); }
.status.error { background: rgba(224,93,61,.2); color: var(--err); }
/* строка стиля — таблица «что где»: колонка меток и колонка значений */
.style-sum { font-size: 12px; color: var(--muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
/* выпадающее меню на <details>: кнопка-summary, список — как меню в шапке */
.menu-pop { position: relative; }
.menu-pop > summary { list-style: none; cursor: pointer; }
.menu-pop > summary::-webkit-details-marker { display: none; }
.menu-pop > summary.ghost-btn {
  display: inline-block; padding: 4px 10px; font-size: 12px; color: var(--muted); user-select: none;
  background: linear-gradient(180deg, var(--panel2), var(--panel));
  border: 1px solid var(--btn-border); border-top-color: var(--btn-bevel-hi);
}
.menu-pop > summary.ghost-btn:hover, .menu-pop[open] > summary.ghost-btn { color: var(--text); filter: brightness(1.1); }
.menu-pop > .nav-menu { top: auto; bottom: 100%; margin: 0 0 2px; }
.menu-pop .nav-menu li.danger { color: var(--err, #e5534b); }
.style-tags { display: grid; grid-template-columns: max-content 1fr; gap: 3px 12px; margin: 6px 0; align-items: baseline; }
.style-tag-key {
  font-size: 11px; text-transform: uppercase; letter-spacing: .4px;
  color: var(--muted); user-select: none; white-space: nowrap;
}
.style-tag-val { font-size: 12px; color: var(--muted); min-width: 0; }
.error { color: var(--err); font-size: 13px; }
/* действия трека — первая секция разворота */
.job-actions { display: flex; flex-wrap: wrap; gap: 6px 8px; align-items: center; }
.job-actions button { min-width: 0; }
.job .play-main { flex: none; }
.job-actions button { font-size: 12px; padding: 4px 10px; }
.muted { color: var(--muted); }
.badge.draft { background: var(--badge-bg); color: var(--badge-fg); font-style: italic; }
.badge { font-size: 11px; padding: 1px 7px; border-radius: 9px; background: var(--badge-bg); color: var(--badge-fg); }
.play-main {
  background: var(--play-grad);
  color: var(--play-fg); padding: 6px 16px; min-width: 44px;
  text-shadow: none;
}
.ghost.icon { padding: 2px 8px; font-size: 13px; }

/* диалоги (копайтер/план/метрики/подтверждение) — выше страниц-модалок (z-index 60):
   страница-модалка сама есть modal-backdrop+page-backdrop, и page-backdrop ниже в файле возвращает ей 60 */
.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,.55); display: flex; align-items: center; justify-content: center; z-index: 70; }
.modal { background: var(--panel); border: 1px solid var(--border); border-radius: 12px; padding: 18px 20px; width: min(760px, 92vw); max-height: 90vh; display: flex; flex-direction: column; gap: 10px; }
/* страницы-модалки (настройки/свои треки/голоса) поверх основного контента:
   закрываются ✕, Esc и кликом по фону — кнопка «Вернуться» не нужна */
.page-backdrop { z-index: 60; padding: 24px; }
.page-modal { width: min(1100px, 96vw); max-width: 96vw; max-height: calc(100vh - 48px); overflow: hidden; }
.page-modal-head {
  display: flex; align-items: center; gap: 10px;
  margin: -16px -16px 0; padding: 6px 12px;
  background: linear-gradient(180deg, var(--panel3), var(--panel2));
  border-bottom: 1px solid var(--bevel-lo);
}
.page-modal-head h2 { margin: 0; padding: 0; background: none; border: none; flex: 1; }
/* боковой паддинг тела = паддингу панели: внутренние h2 (поля −16px для стыковки
   с панелью) ложатся вровень с шапкой и не выпирают горизонтальным скроллом */
.page-modal-body { overflow-y: auto; min-height: 0; display: flex; flex-direction: column; gap: 12px; padding: 12px 16px 8px; }
.modal-head { display: flex; align-items: baseline; gap: 12px; }
.modal-head h2 { margin: 0; }
.plan-meta { font-size: 12px; }
.abc { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; line-height: 1.45; }
.abc-row { display: flex; align-items: center; gap: 8px; margin-top: 8px; }
.submit-err { max-width: 40%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; align-self: center; }
.modal-actions { display: flex; gap: 8px; }

.metrics-modal { width: min(640px, 92vw); }
.cmp-row { display: flex; align-items: center; gap: 10px; font-size: 13px; flex-wrap: wrap; }
.cmp-row select { max-width: 320px; }
.metrics-table { display: flex; flex-direction: column; gap: 2px; font-size: 13px; }
.mrow { display: grid; grid-template-columns: 1.6fr 1fr 1fr .8fr; gap: 8px; padding: 3px 6px; border-radius: 4px; }
.mrow:nth-child(odd) { background: var(--panel2); }
.mrow.head { color: var(--muted); font-size: 11px; text-transform: uppercase; letter-spacing: .5px; }
.mrow span:not(:first-child) { text-align: right; font-variant-numeric: tabular-nums; }
.mrow.head span:first-child { text-align: left; }
.delta { color: var(--run); }
.delta:empty { color: transparent; }

.dsp-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.dsp-row select { max-width: 220px; }
.dsp-note { font-size: 12px; margin: 0; }
.dsp-params { display: flex; flex-direction: column; gap: 4px; }
.dsp-params label { display: grid; grid-template-columns: 190px 1fr 52px; align-items: center; gap: 10px; font-size: 12px; color: var(--muted); }
.dsp-params input[type="range"] { width: 100%; }
.dsp-pval { text-align: right; font-variant-numeric: tabular-nums; color: inherit; }
.dsp-variant { display: flex; align-items: center; gap: 10px; font-size: 12px; flex-wrap: wrap; }
.dsp-variant .deltas { font-variant-numeric: tabular-nums; }
.play-mini { padding: 2px 9px; }
.play-mini.stop { color: var(--err); font-weight: bold; }
.primary.small { padding: 4px 12px; font-size: 12px; }

.lyrics-head { display: flex; align-items: center; gap: 10px; margin: 10px 0 4px; }
.lyrics-head .lyrics-label { margin: 0; flex: 1; }
.small-btn { font-size: 12px; padding: 3px 10px; }
.cop-instruction { margin: 10px 0; font-size: 12px; }
.cop-instruction summary { cursor: pointer; color: var(--muted); margin-bottom: 4px; }
.cop-instruction textarea { width: 100%; }
.cop-modal { width: min(620px, 92vw); }
.cop-form { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.cop-form input[type="text"], .cop-form input:not([type]) { flex: 1; min-width: 220px; }
.cop-example { font-size: 12px; color: var(--muted); display: flex; align-items: center; gap: 5px; }

/* стойка */
.rack { display: flex; flex-direction: column; gap: 10px; margin-top: 8px; }
.rack-gname { font-size: 11px; text-transform: uppercase; letter-spacing: .5px; color: var(--muted); margin-bottom: 4px; }
.rack-item { display: flex; align-items: center; gap: 8px; font-size: 13px; padding: 2px 0; }
.rack-pick { display: flex; align-items: center; gap: 6px; flex: 1; cursor: pointer; }
.rack-item select { font-size: 12px; }

/* профили корпуса */
.corpus-new { display: flex; gap: 8px; }
.corpus-new input { flex: 1; }
.corpus-item { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; border-top: 1px solid var(--border); padding: 8px 0 4px; margin-top: 8px; font-size: 13px; }
.corpus-profile { width: 100%; font-size: 12px; }
.corpus-profile p { margin: 4px 0; }
.corpus-actions { display: flex; gap: 8px; }
.voice-hint { font-size: 12px; margin: 6px 0 0; }
.voice-presets { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin: 8px 0 10px; font-size: 12px; }
.voice-preview { font-size: 12px; margin: 6px 0 0; word-break: break-word; }
.voice-card-desc { flex: 1 1 240px; min-width: 0; word-break: break-word; font-size: 12px; }
.corpus-tracks { width: 100%; display: flex; flex-direction: column; gap: 4px; margin-top: 4px; }
.corpus-track { font-size: 12px; display: flex; flex-wrap: wrap; gap: 6px; align-items: baseline; }
.corpus-track .track-lyrics { width: 100%; margin: 0; color: var(--muted); font-style: italic; opacity: .8; }

/* пиано-ролл */
/* контейнер секций студии (бывшая рамка-«форма» убрана: блоки на фоне страницы).
   min-width: 0 — иначе минимальная ширина ролл-сетки (сотни тактов) растягивает
   студию шире окна вместо прокрутки внутри roll-scroll */
.roll-block { margin-top: 8px; padding: 0; min-width: 0; overflow-x: auto; }
.roll-voices { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-bottom: 10px; font-size: 12px; }
.studio-page { justify-content: stretch; }
/* студия — без общей панели (секции-блоки лежат прямо на фоне страницы,
   не «форма в форме»); заголовок — строкой, без плашки h2 */
.studio-page .studio-sheet { width: auto; max-width: none; flex: 1; margin: 0 16px; min-width: 0; padding-bottom: 12px; }
.studio-sheet > h2, .studio-top > h2 {
  margin: 8px 2px 2px; padding: 0; background: none; border: none; text-shadow: none;
}
.studio-top { display: flex; align-items: center; gap: 12px; }
/* полоса шагов студии прилипает к верху при прокрутке; активный шаг — по видимому разделу */
.studio-steps {
  position: sticky; top: 0; z-index: 4; display: flex; gap: 6px; flex-wrap: wrap;
  margin: 10px 0 4px; padding: 6px 0; background: var(--bg);
}
.studio-steps .on { color: var(--lcd-text); border-color: var(--lcd-text); }
/* в светлой теме янтарь ЖК на светлом не читается — активный шаг акцентным цветом */
[data-theme="light"] .studio-steps .on { color: var(--accent); border-color: var(--accent); }
.studio-step { margin: 22px 2px 8px; font-size: 14px; letter-spacing: 1px; text-transform: uppercase; scroll-margin-top: 52px; }
.studio-step .muted { text-transform: none; letter-spacing: 0; font-size: 12px; font-weight: 400; }
.studio-top > h2 { margin: 0; flex: 1; min-width: 0; }
.corpus-list { max-width: none; }
/* секции студии (звук/план/правки/вклейки/стемы/овердаб/эффекты) — рамка
   со шапкой с фоном, как заголовок карточки трека */
.studio-box {
  border: 1px solid var(--border); border-radius: 6px; margin-top: 12px;
  overflow: hidden; background: color-mix(in srgb, var(--panel) 70%, transparent);
}
.studio-box-head {
  display: flex; align-items: center; gap: 10px; padding: 6px 10px;
  background: var(--panel); border-bottom: 1px solid var(--border);
  box-shadow: inset 1px 1px 0 rgba(255,255,255,.05);
  font-size: 12px; font-weight: 600; letter-spacing: .3px; text-transform: uppercase; color: var(--muted);
}
.studio-box-hint { font-weight: 400; text-transform: none; letter-spacing: 0; font-size: 11px; }
.studio-box-tools { margin-left: auto; display: inline-flex; gap: 4px; align-items: center; }
.studio-box-body { padding: 8px 10px; }
/* раскрываемые секции (овердаб, эффекты): стрелка вместо маркера details */
details.studio-box > summary { cursor: pointer; list-style: none; }
details.studio-box > summary::-webkit-details-marker { display: none; }
details.studio-box > summary::before { content: '▸'; color: var(--muted); transition: transform .15s; }
details.studio-box[open] > summary::before { transform: rotate(90deg); }
details.studio-box[open] > .studio-box-head { margin-bottom: 0; }
.abc-help { margin-bottom: 8px; font-size: 12px; }
.abc-help summary { cursor: pointer; color: var(--muted); }
.abc-help p { margin: 6px 0 0; line-height: 1.6; }
.roll-voice-edit { display: flex; align-items: center; gap: 4px; }
.stems-inline { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; font-size: 12px; }
.stem-toggle { display: inline-flex; }
.roll-meta { margin: 0 0 8px; font-size: 12px; }
.roll-grid { display: grid; gap: 2px; font-size: 10px; user-select: none; }
.roll-grid + .roll-grid { margin-top: 8px; }
.roll-voice { font-size: 10px; color: var(--muted); white-space: nowrap; overflow: hidden; }
.roll-sec { font-size: 9px; color: var(--muted); white-space: nowrap; overflow: visible; }
/* начало секции: подпись целиком (поверх соседних пустых ячеек) и метка-граница */
.roll-sec.start { border-left: 1px solid var(--border); padding-left: 2px; position: relative; z-index: 1; }
.roll-cell { height: 18px; border-radius: 3px; cursor: pointer; border: 1px solid transparent; position: relative; }
.roll-cell.d0 { background: var(--panel); }
.roll-cell.d1 { background: rgba(120,140,255,.18); }
.roll-cell.d2 { background: rgba(120,140,255,.42); }
.roll-cell.d3 { background: rgba(120,140,255,.72); }
.roll-cell.sel { border-color: var(--ok); box-shadow: 0 0 0 1px var(--ok); }
.roll-cell.off { opacity: .3; }
.roll-chord { font-size: 9px; color: var(--muted); text-align: center; overflow: hidden; }

/* волна громкости (первая канва проекта): сетка/выделение/курсор рисует
   canvas, спектрограмма — <img> воркера под ним, ось X у обоих 0..длительность */
/* студия: секции-блоки .studio-box выше; зум ролла — вправо в шапке секции */
.roll-zoom { margin-left: auto; display: inline-flex; gap: 4px; }
.wave-toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 6px; font-size: 12px; }
/* выбор цвета волны: кружки-свотчи, активный — с обводкой */
.wave-colors { display: inline-flex; align-items: center; gap: 5px; }
.wave-swatch {
  width: 15px; height: 15px; min-width: 15px; padding: 0; border-radius: 50%;
  border: 1px solid var(--border); cursor: pointer;
}
.wave-swatch.on { outline: 2px solid var(--text); outline-offset: 1px; }
.wave-toolbar .ghost.on { border-color: var(--accent); color: var(--accent); font-weight: 600; }
.wave-snap { display: inline-flex; align-items: center; gap: 4px; color: var(--muted); cursor: pointer; }
.wave-hint { margin-left: auto; font-weight: 400; text-transform: none; letter-spacing: 0; }
.wave-wrap { position: relative; height: 96px; border: 1px solid var(--border); border-radius: 8px; background: var(--panel2); overflow: hidden; }
.wave-spectrum { position: absolute; top: 0; height: 100%; object-fit: fill; }
.wave-empty { position: absolute; inset: 0; }
.wave-canvas { position: absolute; inset: 0; width: 100%; height: 100%; cursor: crosshair; touch-action: none; }
.wave-zoom { position: absolute; top: 4px; right: 6px; display: inline-flex; gap: 4px; align-items: center; font-size: 11px; }
.wave-scroll { position: relative; height: 10px; margin-top: 3px; border: 1px solid var(--border); border-radius: 5px; background: var(--panel); cursor: grab; user-select: none; touch-action: none; }
.wave-thumb { position: absolute; top: 1px; bottom: 1px; border-radius: 4px; background: var(--accent); opacity: .55; }
.wave-scroll:active .wave-thumb, .wave-scroll:hover .wave-thumb { opacity: .85; }

/* прокрутка ролла: только вокруг сетки тактов — скроллбар прямо под ней;
   нативный скроллбар webkit тонет в тёмной теме — стилизуем явно */
.roll-scroll { overflow-x: auto; }
.roll-scroll::-webkit-scrollbar { height: 10px; }
.roll-scroll::-webkit-scrollbar-track { background: var(--panel); border-radius: 5px; }
.roll-scroll::-webkit-scrollbar-thumb { background: var(--accent); border-radius: 5px; }

/* овердаб */
.od-chips { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 8px; }
.od-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.od-style { flex: 1; min-width: 240px; }
.od-gain { font-size: 12px; color: var(--muted); display: flex; align-items: center; gap: 6px; }

/* стемы */
.stem-row { display: flex; align-items: center; gap: 10px; font-size: 13px; }
</style>
