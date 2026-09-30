<script setup>
// Yue Studio — корневой компонент: форма новой композиции, очередь/результаты,
// переключение страниц. Экранные компоненты и модалки — в ./components/.
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { api } from './api.js'
import { rackGroups, rackEffects, rackCompile } from './rack.js'
import { groups as builtinGroups, loadCustomGroups, saveCustomGroups } from './groups.js'
import { slotKeys, slotHints, durOptions, durTokens } from './slotOptions.js'
import { useI18n } from './i18n/index.js'
import { voiceDescriptor, normalizeVoiceParams } from './voiceLab.js'
import { defaultJobFilter, filterJobs, groupJobs, pageJobs, pageCount } from './jobFilter.js'
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

const { playerState, playBusy, isPlaying, playBtn, toggleArtifact, onVolume, onRefresh } = usePlayer()
const { locale, t, setLocale } = useI18n()
const healthTitle = computed(() => health.value
  ? t('app.health.up') + (health.value.model_loaded ? t('app.health.model') : '')
  : t('app.health.down'))
const statusLabelC = computed(() => ({
  queued: t('queue.status.queued'), running: t('queue.status.running'), done: t('queue.status.done'),
  error: t('queue.status.error'), canceled: t('queue.status.canceled'),
}))
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
  return {
    title: title.value,
    lyrics: effectiveLyrics(lyrics.value, noLyrics.value, durMode.value),
    seed: seed.value ? Number(seed.value) : 0,
    // драматургии нужен план: off его не строит — молча повышаем до melody
    cot: arcKind.value && cot.value === 'off' ? 'melody' : cot.value,
    arc: arcKind.value,
    // потолок длины из селектора длительности (0 = бюджет воркера)
    max_tokens: durTokens[durMode.value] || 0,
    ...extra,
  }
}

// guard отправки: стиль есть и стих есть (или инструментал)
const canSubmit = computed(() =>
  !!compiledStyle.value && (!!lyrics.value.trim() || noLyrics.value))

async function submit() {
  if (!canSubmit.value) return
  submitting.value = true
  try {
    await api.submit({ ...payload(), style: await finalStyle() })
    await refresh()
  } finally { submitting.value = false }
}

// черновик ~40 с: быстро послушать стиль, прежде чем рендерить полный трек
async function submitDraft() {
  if (!canSubmit.value) return
  submitting.value = true
  try {
    await api.submit({ ...payload({ draft: true }), style: await finalStyle() })
    await refresh()
  } finally { submitting.value = false }
}

async function submitFan(n) {
  if (!canSubmit.value) return
  submitting.value = true
  try {
    await api.submitFan({ ...payload(), style: await finalStyle() }, n)
    await refresh()
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
let timer = null

// фильтры и пейджер списка треков (логика — jobFilter.js, там же тесты)
const qf = ref(defaultJobFilter())
const qPage = ref(1)
// производные треки (куски для вклеек, пересборки, варианты) — под родителем
const grouped = computed(() => groupJobs(jobs.value))
const filteredJobs = computed(() => filterJobs(grouped.value.top, qf.value))
const openKids = ref(new Set())   // id родителей с раскрытыми вложениями
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
const kidMaterial = (id) => (grouped.value.children[id] || []).filter((k) => !isResultJob(k))
function mixName(parentId, file) {
  const label = mixLabel(file, { applied: insertsSvc.appliedFor(parentId), jobs: jobs.value,
    labelOf: (id) => t('studio.trick.inst.' + id), fmt: fmtDur })
  return label ? t('studio.trick.inst.mix', { what: label })
    : t('studio.trick.inst.variant', { id: mixChildId(file) })
}
function playMix(parent, v) {
  toggleArtifact('mix' + parent.id + ':' + v.file, mixName(parent.id, v.file),
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
  try { playerState.value = await api.audioState() } catch {}
}
onRefresh(refresh)

async function togglePlay(j) {
  await toggleArtifact(`m${j.id}`, `#${j.id} ${j.title}`, () => api.playAudio(j.id))
}

async function stopAll() {
  await api.stopAudio()
  await refresh()
}

async function cancel(id) {
  await api.cancel(id)
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
  cot.value = ['full', 'melody', 'off'].includes(j.cot) ? j.cot : 'full'
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

// страницы в меню «⋮»: треки/голоса переключаются, настройки просто открываются
const navOpen = ref(false)

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
  corpusPage.value = false   // применяем — возвращаемся к основной форме
}

// голос из примерочной → форма: дескриптор в слот вокала + seed карточки
// (точная строка стиля очищается, иначе она перекрыла бы слоты)
function applyVoice({ vocals, seed }) {
  slots.value.vocals = vocals
  styleOverride.value = ''
  seed.value = seed || null
  voicesPage.value = false
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

async function renderFromAbc(abc, draft = false) {
  if (!abc || !abc.trim()) return
  submitting.value = true
  try {
    const id = await api.submit({
      ...payload({
        abc: abc,
        cot: cot.value === 'off' ? 'melody' : cot.value,   // abc требует full|melody
        seed: seed.value ? Number(seed.value) : Math.floor(Math.random() * 1e9),
        draft,
      }),
      style: await finalStyle(),
    })
    inheritTrickMarks(id)
    planOpen.value = false
    await refresh()
  } finally { submitting.value = false }
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
    <h1>{{ t('app.title') }}</h1>
    <span class="health-dot" :class="health ? 'up' : 'down'" :title="healthTitle"></span>
    <button class="icon-btn" @click="toggleTheme" :title="theme === 'dark' ? t('app.theme.light') : t('app.theme.dark')">{{ theme === 'dark' ? '☀' : '☾' }}</button>
    <button class="icon-btn" @click="setLocale(locale === 'ru' ? 'en' : 'ru')"
            :title="locale === 'ru' ? 'Switch to English' : 'Переключить на русский'">{{ locale === 'ru' ? 'EN' : 'RU' }}</button>
    <div class="player-center"><PlayerBar :jobs="jobs" @play-job="togglePlay" @refresh="refresh" /></div>
    <span class="spacer"></span>
    <div class="nav-wrap">
      <button class="icon-btn" :title="t('nav.menu.tip')" @click.stop="navOpen = !navOpen">⋮</button>
      <ul v-if="navOpen" class="nav-menu">
        <li :title="t('nav.tracks.tip')" @click="navGo('tracks')"><span class="nav-ico">🎵</span>{{ t('nav.tracks') }}</li>
        <li :title="t('nav.voices.tip')" @click="navGo('voices')"><span class="nav-ico">🎤</span>{{ t('nav.voices') }}</li>
        <li :title="t('nav.settings.tip')" @click="navGo('settings')"><span class="nav-ico">⚙</span>{{ t('nav.settings') }}</li>
      </ul>
    </div>
  </header>

  <SettingsPage v-if="settingsPage" v-model:server-url="serverURL"
                @close="settingsPage = false" @saved="refresh" />
  <LibraryPage v-else-if="libraryPage" @close="libraryPage = false" />
  <CorpusPage v-else-if="corpusPage"
              @close="corpusPage = false"
              @imported="onImported"
              @apply-style="applyProfileStyle"
              @apply-abc="(abc) => setPlanAbc(abc, { seed: null, seconds: null, truncated: false, fromProfile: true })"
              @style-to-library="profileStyleToLibrary" />
  <VoicesPage v-else-if="voicesPage"
              @close="voicesPage = false; loadVoiceCards()"
              @apply-voice="applyVoice" />
  <StudioPage v-else-if="studioJob" :job="studioJob" :auto-translate="autoTranslate"
              @close="studioJob = null; refresh()"
              @open-metrics="openMetrics" />

  <main v-else>
    <div class="left-col">
      <section class="panel form">
        <h2>{{ t('form.title') }}</h2>
        <div class="lib-row" :title="t('form.lib.tip')">
          <VSelect v-model="libGroup" :options="libGroupOptions" :placeholder="t('form.lib.group')" @update:model-value="onLibGroupChange()" />
          <VSelect v-model="libStyle" :options="libStyleOptions" :disabled="!libGroup" :placeholder="t('form.lib.style')" @update:model-value="onLibStyleChange()" />
          <button v-if="currentItem" class="ghost small-btn" :title="t('form.lib.exact.tip')" @click="onLibExact">{{ t('form.lib.exact') }}</button>
          <button class="ghost small-btn" :title="t('form.lib.save.tip')" @click="saveStyleToLibrary">{{ t('form.lib.save') }}</button>
          <button class="ghost small-btn" :title="t('form.lib.manage.tip')" @click="libraryPage = true">{{ t('form.lib.manage') }}</button>
        </div>
        <input v-model="title" :placeholder="t('form.name')" style="margin-top:8px" />

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
            <button v-if="!noLyrics" class="ghost small-btn" @click="copOpen = true">{{ t('form.copilot') }}</button>
            <button v-if="!noLyrics" class="ghost small-btn" :disabled="!!lyrBusy"
                    :title="t('lyrics.rec.tip')" @click="recognizeLyrics">
              {{ lyrBusy === 'rec' ? '…' : t('lyrics.rec') }}</button>
            <button v-if="!noLyrics" class="ghost small-btn" :disabled="!!lyrBusy || !lyrics.trim()"
                    :title="t('lyrics.adapt.tip')" @click="adaptLyrics">
              {{ lyrBusy === 'adapt' ? '…' : t('lyrics.adapt') }}</button>
            <span v-if="lyrErr" class="error">{{ lyrErr }}</span>
          </span>
        </div>
        <textarea v-model="lyrics" rows="10" :disabled="noLyrics" :placeholder="noLyrics ? t('form.nowords.ph') : ''"></textarea>

        <div class="row">
          <label class="seed">seed <input v-model.number="seed" type="number" :placeholder="t('form.name')" /></label>
          <div class="cot-radios" :title="t('form.cot.tip')">
            <span class="cot-title">генерация:</span>
            <label><input type="radio" value="full" v-model="cot" /> полная</label>
            <label><input type="radio" value="melody" v-model="cot" /> мелодия</label>
            <label><input type="radio" value="off" v-model="cot" /> без размышлений</label>
          </div>
          <label class="autotr" title="Слоты можно писать по-русски: перед отправкой строка стиля переводится в английский через Ollama (qwen2.5). Модель обучена на английских тегах.">
            <input type="checkbox" v-model="autoTranslate" /> рус → eng
          </label>
          <span class="compiled" :title="compiledStyle">{{ translateBusy ? t('form.translating') : (compiledStyle ? '→ ' + compiledStyle : t('form.style.empty')) }}</span>
        </div>

        <div class="row arc-row">
          <span class="arc-title">{{ t('arc.title') }}:</span>
          <button v-for="a in ['', 'build', 'wave', 'burst']" :key="a" class="toggle small-btn"
                  :class="{ on: arcKind === a }" :title="t('arc.' + (a || 'flat') + '.tip')"
                  @click="arcKind = a">{{ t('arc.' + (a || 'flat')) }}</button>
        </div>

        <div class="actions">
          <button class="primary" :disabled="submitting || !canSubmit" @click="submit">
            {{ submitting ? t('form.submitting') : t('form.submit') }}
          </button>
          <button class="primary alt" :disabled="submitting || !canSubmit" @click="submitFan(5)" :title="t('form.fan.tip')">
            {{ t('form.fan') }}
          </button>
          <button class="ghost" :disabled="planBusy || !canSubmit" @click="makePlan"
                  :title="t('form.notes.tip')">
            {{ planBusy ? t('form.planning') : t('form.notes') }}
          </button>
          <button class="ghost" :disabled="submitting || !canSubmit" @click="submitDraft"
                  :title="t('form.draft.tip')">
            {{ t('form.draft') }}
          </button>
        </div>
      </section>
    </div>

    <section class="panel list">
      <h2>{{ t('queue.title') }}</h2>
      <p v-if="!jobs.length" class="muted">{{ t('common.empty') }}</p>
      <template v-else>
        <div class="queue-tools">
          <VSelect v-model="qf.status" :options="qStatusOptions" />
          <VSelect v-model="qf.period" :options="qPeriodOptions" />
          <VSelect v-model="qf.dur" :options="qDurOptions" />
          <VSelect v-model="qf.draft" :options="qDraftOptions" />
          <input v-model="qf.q" :placeholder="t('queue.filter.search')" />
        </div>
        <p class="muted">{{ filteredJobs.length
          ? t('queue.filter.shown', { shown: queuePage.length, total: filteredJobs.length })
          : t('queue.filter.none') }}<template v-if="filteredJobs.length > queuePage.length && qPageMax > 1"> · {{ t('queue.filter.page', { page: qPageNow, max: qPageMax }) }}</template></p>
        <article v-for="j in queuePage" :key="j.id" class="job" :class="j.status">
        <div class="job-head">
          <strong>#{{ j.id }} {{ j.title }}</strong>
          <span v-if="headOf(j) !== j" class="badge current" :title="headOf(j).title">★ {{ t('queue.head.badge', { id: headOf(j).id }) }}</span>
          <span class="status" :class="j.status">{{ statusLabelC[j.status] || j.status }}</span>
          <span v-if="j.draft" class="badge draft">{{ t('queue.draft') }}</span>
          <span v-if="j.duration_sec" class="muted">{{ fmtDur(j.duration_sec) }}</span>
          <span v-if="j.seed" class="muted">seed {{ j.seed }}</span>
          <span v-if="j.cot && j.cot !== 'full'" class="muted">cot {{ j.cot }}</span>
          <span v-if="j.req_abc" class="badge" :title="t('plan.render')">свой ABC</span>
          <span v-if="j.status === 'running'" class="progress-wrap" role="progressbar"
                :aria-valuenow="j.progress_pct ?? undefined" :title="progressTip(j)">
            <span class="progress-track" :class="{ indet: j.progress_pct == null }">
              <span v-if="j.progress_pct != null" class="progress-fill" :style="{ width: j.progress_pct + '%' }"></span>
            </span>
            <span class="progress-label muted">
              <!-- есть процент — слово стадии не нужно: «41% · 82.5 т/с» -->
              <template v-if="j.progress_pct != null">{{ j.progress_pct }}%</template>
              <template v-else>{{ t('queue.progress.' + (j.stage || 'plan')) }}</template>
              <template v-if="j.tok_per_s"> · {{ j.tok_per_s }} {{ t('queue.progress.tps') }}</template>
            </span>
          </span>
          <button v-if="j.status === 'queued' || j.status === 'running'" class="ghost small-btn"
                  :title="t('queue.cancel.tip')" @click="cancel(j.id)">{{ t('queue.cancel') }}</button>
          <button v-if="(grouped.children[j.id] || []).length" class="ghost small-btn" :class="{ on: openKids.has(j.id) }"
                  :title="t('queue.kids.tip')" @click="toggleKids(j.id)">📎 {{ grouped.children[j.id].length }}</button>
          <span class="spacer"></span>
          <button v-if="j.status !== 'running'" class="ghost icon del" :title="t('queue.delete.tip')" @click="deleteJob(j)">✕</button>
          <button class="ghost icon" :title="t('queue.repeat.tip')" @click="reuseJob(j)">↺</button>
        </div>
        <p class="muted style">{{ j.style }}</p>
        <div v-if="openKids.has(j.id)" class="job-kids">
          <span class="muted">{{ t('queue.kids.versions') }}</span>
          <div v-for="v in [j, ...kidResults(j.id)]" :key="v.id" class="job-kid" :class="{ current: headOf(j).id === v.id }">
            <span v-if="headOf(j).id === v.id" class="badge current">★ {{ t('queue.head.main') }}</span>
            <button v-else-if="v.status === 'done'" class="ghost small-btn" :title="t('queue.head.make.tip')"
                    @click="makeHead(j, v)">☆ {{ t('queue.head.make') }}</button>
            <span class="muted">#{{ v.id }}</span>
            <span>{{ v.id === j.id ? t('queue.kids.original') : v.title }}</span>
            <span v-if="v.id !== j.id" class="badge">{{ t('queue.role.' + v.role) }}</span>
            <span v-if="v.id !== j.id" class="status" :class="v.status">{{ statusLabelC[v.status] || v.status }}</span>
            <span v-if="v.duration_sec" class="muted">{{ fmtDur(v.duration_sec) }}</span>
            <span class="spacer"></span>
            <button v-if="v.status === 'done' && v.audio_file" class="ghost small-btn" @click="togglePlay(v)">
              {{ isPlaying('m' + v.id) ? t('queue.stop') : t('queue.play') }}
            </button>
            <button v-if="v.status === 'done'" class="ghost small-btn" @click="studioJob = v">студия →</button>
            <button v-if="v.id !== j.id && v.status !== 'running'" class="ghost icon del" :title="t('queue.delete.tip')" @click="deleteJob(v)">✕</button>
          </div>
          <details v-if="(kidMixes[j.id] || []).length" class="job-material">
            <summary>{{ t('queue.kids.mixes', { n: kidMixes[j.id].length }) }}</summary>
            <div v-for="(v, n) in kidMixes[j.id]" :key="v.file" class="job-kid">
              <span v-if="n === 0" class="badge">{{ t('queue.kids.latest') }}</span>
              <span>{{ mixName(j.id, v.file) }}</span>
              <span v-if="v.created_at" class="muted">{{ String(v.created_at).replace('T', ' ').slice(0, 16) }}</span>
              <span class="spacer"></span>
              <button class="ghost small-btn" @click="playMix(j, v)">{{ playBtn('mix' + j.id + ':' + v.file) }}</button>
            </div>
          </details>
          <details v-if="kidMaterial(j.id).length" class="job-material material">
            <summary>{{ t('queue.kids.material', { n: kidMaterial(j.id).length }) }}</summary>
            <div v-for="k in kidMaterial(j.id)" :key="k.id" class="job-kid">
              <span>#{{ k.id }}</span>
              <span>{{ k.title }}</span>
              <span class="badge">{{ t('queue.role.' + (k.role || (k.overdub_of ? 'overdub' : 'other'))) }}</span>
              <span>{{ statusLabelC[k.status] || k.status }}</span>
              <span class="spacer"></span>
              <button v-if="k.status === 'done' && k.audio_file" class="ghost small-btn" @click="togglePlay(k)">
                {{ isPlaying('m' + k.id) ? t('queue.stop') : t('queue.play') }}
              </button>
              <button v-if="k.status !== 'running'" class="ghost icon del" :title="t('queue.delete.tip')" @click="deleteJob(k)">✕</button>
            </div>
          </details>
        </div>
        <p v-if="j.error" class="error">{{ j.error }}</p>
        <div v-if="j.status === 'done' && j.audio_file" class="job-actions">
          <button class="play-main" :disabled="playBusy['m' + headOf(j).id]" @click="togglePlay(headOf(j))">
            {{ playBtn('m' + headOf(j).id) === '…' ? t('queue.loading') : (isPlaying('m' + headOf(j).id) ? t('queue.stop') : t('queue.play')) }}
          </button>
          <button class="ghost stopbtn" :title="t('player.stop')" @click="stopAll()">■</button>
          <button v-if="isPlaying('m' + headOf(j).id) && playerState.playing" class="ghost" @click="api.toggleAudio()">⏸</button>
          <!-- всё на карточке — про основную версию песни: играть, скачать, ноты -->
          <button class="ghost" @click="openListen(headOf(j))" :title="t('queue.browser.tip')">{{ t('queue.browser') }}</button>
          <button v-if="j.status === 'done'" class="ghost" @click="studioJob = headOf(j)">студия →</button>
          <button class="ghost icon" :title="t('queue.save.flac.tip')" @click="saveAudio(headOf(j))">{{ t('queue.save.flac') }}</button>
          <button class="ghost icon" :disabled="playBusy['mp3' + headOf(j).id]" :title="t('queue.save.mp3.tip')" @click="saveMp3(headOf(j))">{{ t('queue.save.mp3') }}</button>
          <button v-if="headOf(j).abc_file" class="ghost" @click="loadJobAbc(headOf(j))"
                  :title="t('queue.notes.tip')">{{ t('queue.notes') }}</button>
          <button v-if="headOf(j).abc_file" class="ghost icon" :title="t('queue.notes.save.tip')" @click="saveAudio(headOf(j), headOf(j).abc_file)">⤓ abc</button>
        </div>
      </article>
        <div v-if="qPageMax > 1" class="pager">
          <button class="ghost small-btn" :disabled="qPageNow <= 1" @click="qPage = qPageNow - 1">←</button>
          <span class="muted">{{ qPageNow }} / {{ qPageMax }}</span>
          <button class="ghost small-btn" :disabled="qPageNow >= qPageMax" @click="qPage = qPageNow + 1">→</button>
        </div>
      </template>
    </section>
  </main>

  <PlanModal v-model:abc="planAbc" :open="planOpen" :busy="planBusy" :err="planErr"
             :submitting="submitting" :info="planInfo"
             @close="planOpen = false" @render="renderFromAbc" @new-plan="makePlan" @from-track="transcribeFromTrack" />
  <CopilotModal :open="copOpen" :style="compiledStyle" :example="lyrics" :lang="slots.language" :slots="slots"
                @close="copOpen = false" @insert="onCopInsert" />
  <MetricsModal ref="metricsModal" :jobs="jobs" />
  <ConfirmModal />
</template>

<style>
/* Стили приложения — глобальные: экранные компоненты (components/) рендерятся
   внутри этого корня и пользуются теми же классами. */
header {
  /* выше панелей main (стекинг-контексты из backdrop-filter), но ниже модалок (z-index 10) */
  position: relative; z-index: 5; flex-wrap: wrap;
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
.settings-page { display: flex; justify-content: center; align-items: flex-start; }
.panel.lib { position: relative; z-index: 3; } /* выпадашки VSelect выше соседних панелей (стекинг-контексты из backdrop-filter) */
.panel { width: 640px; max-width: 100%; display: flex; flex-direction: column; gap: 12px; }
/* очередь — правая колонка сетки: растягивается на всё свободное место */
.panel.list { width: auto; }
.trick-row { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; margin-top: 8px; font-size: 12px; }
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
.settings-page .ok { color: var(--ok); font-size: 12px; }
.player-center {
  position: absolute; left: 50%; top: 50%; transform: translate(-50%, -50%);
  max-width: min(56vw, 720px); min-width: 0; z-index: 2;
}
@media (max-width: 1150px) {
  .player-center {
    position: static; transform: none; order: 9; flex-basis: 100%;
    justify-content: flex-start; z-index: auto;
  }
}
.playerbar { display: flex; align-items: center; gap: 8px; font-size: 12px; }
.playerbar .seek { width: 180px; }
.playerbar .vol { display: flex; align-items: center; gap: 4px; color: var(--muted); }
.playerbar .vol input { width: 80px; }
.playerbar .now { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.playerbar .pos {
  font-variant-numeric: tabular-nums; white-space: nowrap; flex: none;
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
  /* левая колонка (форма) фиксированная — очередь забирает всё остальное */
  grid-template-columns: 520px minmax(340px, 1fr);
  gap: 20px; padding: 20px 24px; max-width: 1800px; margin: 0 auto;
}
@media (max-width: 780px) { main { grid-template-columns: minmax(0, 1fr); } }
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
.cot-radios { display: flex; align-items: center; gap: 10px; font-size: 12px; color: var(--muted); }
.cot-radios label { display: flex; align-items: center; gap: 4px; cursor: pointer; }
.cot-title { font-weight: 600; }
.compiled { font-size: 11px; color: var(--muted); margin: 0 0 10px; word-break: break-word; }
.autotr { font-size: 12px; color: var(--muted); display: flex; align-items: center; gap: 4px; cursor: pointer; }

.actions { display: flex; gap: 8px; }
.actions .primary { flex: 0 0 auto; }
.primary {
  background: linear-gradient(180deg, #ffd27a, var(--lcd-text) 45%, #c98a1a);
  color: #1a1408; padding: 6px 16px; text-shadow: none;
}
.primary.alt {
  background: linear-gradient(180deg, var(--panel3), var(--panel2));
  border: 1px solid var(--lcd-text); color: var(--lcd-text);
}

.progress-wrap { display: inline-flex; align-items: center; gap: 8px; min-width: 180px; }
.progress-track { display: inline-block; width: 110px; height: 8px; border-radius: 4px;
  background: var(--panel2); border: 1px solid var(--border); overflow: hidden; position: relative; }
.progress-fill { display: block; height: 100%;
  background: linear-gradient(90deg, var(--run), var(--ok)); transition: width 1s linear; }
.progress-track.indet::after { content: ''; position: absolute; inset: 0 auto 0 -30%; width: 30%;
  background: var(--run); border-radius: 4px; animation: progress-slide 1.2s ease-in-out infinite; }
@keyframes progress-slide { to { left: 100%; } }
.progress-label { font-size: 11px; white-space: nowrap; }

.job { border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; margin-bottom: 10px; background: var(--panel2); }
.job-head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.status { font-size: 12px; padding: 2px 8px; border-radius: 10px; background: var(--border); }
.status.done { background: rgba(255,190,61,.16); color: var(--lcd-text); text-shadow: 0 0 8px var(--lcd-glow); }
.status.running { background: rgba(217,160,61,.2); color: var(--run); }
.status.error { background: rgba(224,93,61,.2); color: var(--err); }
.style { font-size: 12px; margin: 6px 0; }
.error { color: var(--err); font-size: 13px; }
.job-actions { display: flex; flex-wrap: wrap; gap: 6px 8px; margin-top: 6px; align-items: center; }
.job-actions button { min-width: 0; }
.job .play-main { flex: none; }
.job-actions button { font-size: 12px; padding: 4px 10px; }
.muted { color: var(--muted); }
.badge.draft { background: rgba(120,140,255,.15); color: #9aa5ff; font-style: italic; }
.badge { font-size: 11px; padding: 1px 7px; border-radius: 9px; background: rgba(120,140,255,.15); color: #9aa5ff; }
.play-main {
  background: linear-gradient(180deg, #ffd27a, var(--lcd-text) 45%, #c98a1a);
  color: #1a1408; padding: 6px 16px; min-width: 44px;
  text-shadow: none;
}
.ghost.icon { padding: 2px 8px; font-size: 13px; }

.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,.55); display: flex; align-items: center; justify-content: center; z-index: 10; }
.modal { background: var(--panel); border: 1px solid var(--border); border-radius: 12px; padding: 18px 20px; width: min(760px, 92vw); max-height: 90vh; display: flex; flex-direction: column; gap: 10px; }
.modal-head { display: flex; align-items: baseline; gap: 12px; }
.modal-head h2 { margin: 0; }
.plan-meta { font-size: 12px; }
.abc { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; line-height: 1.45; }
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
.stopbtn { color: var(--err); font-weight: bold; }
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
.roll-block { margin-top: 8px; padding: 10px; border: 1px dashed var(--border); border-radius: 8px; overflow-x: auto; }
.roll-voices { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-bottom: 10px; font-size: 12px; }
.studio-page .roll-block { border: none; padding: 0; overflow-x: auto; }
.studio-page, .corpus-page-wide, .settings-wide { justify-content: stretch; }
.studio-page .panel, .corpus-page-wide .panel, .settings-wide .panel { width: auto; max-width: none; flex: 1; margin: 0 16px; }
.corpus-list { max-width: none; }
.studio-sec { margin-top: 10px; padding-top: 6px; border-top: 1px dashed var(--border); }
.studio-sec summary { cursor: pointer; font-size: 13px; margin-bottom: 6px; }
.abc-help { margin-bottom: 8px; font-size: 12px; }
.abc-help summary { cursor: pointer; color: var(--muted); }
.abc-help p { margin: 6px 0 0; line-height: 1.6; }
.roll-voice-edit { display: flex; align-items: center; gap: 4px; }
.roll-stems { margin-top: 10px; padding-top: 8px; border-top: 1px dashed var(--border); }
.stems-inline { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; font-size: 12px; }
.stem-toggle { display: inline-flex; }
.roll-meta { margin: 0 0 8px; font-size: 12px; }
.roll-grid { display: grid; gap: 2px; font-size: 10px; user-select: none; }
.roll-grid + .roll-grid { margin-top: 8px; }
.roll-voice { font-size: 10px; color: var(--muted); white-space: nowrap; overflow: hidden; }
.roll-sec { font-size: 9px; color: var(--muted); text-align: center; overflow: hidden; }
.roll-cell { height: 18px; border-radius: 3px; cursor: pointer; border: 1px solid transparent; position: relative; }
.roll-cell.d0 { background: var(--panel); }
.roll-cell.d1 { background: rgba(120,140,255,.18); }
.roll-cell.d2 { background: rgba(120,140,255,.42); }
.roll-cell.d3 { background: rgba(120,140,255,.72); }
.roll-cell.sel { border-color: var(--ok); box-shadow: 0 0 0 1px var(--ok); }
.roll-cell.off { opacity: .3; }
.roll-chord { font-size: 9px; color: var(--muted); text-align: center; overflow: hidden; }
.roll-actions { display: flex; align-items: center; gap: 10px; margin-top: 8px; font-size: 12px; }

/* волна громкости (первая канва проекта): сетка/выделение/курсор рисует
   canvas, спектрограмма — <img> воркера под ним, ось X у обоих 0..длительность */
.panel-caption { display: flex; align-items: baseline; gap: 10px; margin: 10px 0 4px; font-size: 12px; font-weight: 600; letter-spacing: .3px; text-transform: uppercase; color: var(--muted); }
.roll-zoom { margin-left: auto; display: inline-flex; gap: 4px; }
.wave-panel { margin: 0 0 6px; }
.wave-panel .panel-caption { margin-top: 0; }
.wave-toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 6px; font-size: 12px; }
.wave-toolbar .ghost.on { border-color: var(--accent); color: var(--accent); font-weight: 600; }
.wave-snap { display: inline-flex; align-items: center; gap: 4px; color: var(--muted); cursor: pointer; }
.wave-hint { margin-left: auto; font-weight: 400; text-transform: none; letter-spacing: 0; }
.wave-wrap { position: relative; height: 96px; border: 1px solid var(--border); border-radius: 8px; background: var(--panel2); overflow: hidden; }
.wave-spectrum { position: absolute; top: 0; height: 100%; object-fit: fill; }
.wave-empty { position: absolute; inset: 0; }
.wave-canvas { position: absolute; inset: 0; width: 100%; height: 100%; cursor: crosshair; touch-action: none; }
.wave-zoom { position: absolute; top: 4px; right: 6px; display: inline-flex; gap: 4px; align-items: center; font-size: 11px; }

/* овердаб */
.od-chips { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 8px; }
.od-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.od-style { flex: 1; min-width: 240px; }
.od-gain { font-size: 12px; color: var(--muted); display: flex; align-items: center; gap: 6px; }

/* стемы */
.stem-row { display: flex; align-items: center; gap: 10px; font-size: 13px; }
</style>
