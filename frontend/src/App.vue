<script setup>
// Yue Studio — корневой компонент: форма новой композиции, очередь/результаты,
// переключение страниц. Экранные компоненты и модалки — в ./components/.
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { api } from './api.js'
import { rackGroups, rackEffects, rackCompile } from './rack.js'
import { groups as builtinGroups, loadCustomGroups, saveCustomGroups } from './groups.js'
import { slotMeta, slotHints, durOptions } from './slotOptions.js'
import { SLOT_ORDER, buildStyleLine, dictStyle, cleanLyrics, effectiveLyrics } from './styleLogic.js'
import { usePlayer, fmtDur } from './composables/usePlayer.js'
import { useConfirm } from './composables/useConfirm.js'
import VSelect from './VSelect.vue'
import PlayerBar from './components/PlayerBar.vue'
import SettingsPage from './components/SettingsPage.vue'
import LibraryPage from './components/LibraryPage.vue'
import CorpusPage from './components/CorpusPage.vue'
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
    cot: cot.value,
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
  if (mode === 'line') {
    styleOverride.value = p.style   // точная строка калибровки, 1:1
  } else {
    styleOverride.value = ''
    for (const k of Object.keys(p.slots || {})) slots.value[k] = p.slots[k]
  }
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

const statusLabel = { queued: 'в очереди', running: 'генерируется', done: 'готово', error: 'ошибка', canceled: 'отменено' }

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
const studioJob = ref(null)

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

// ---------- Редактор плана (ABC) ----------

const planOpen = ref(false)
const planBusy = ref(false)
const planErr = ref('')
const planInfo = ref(null)   // {seed, seconds, truncated} — как план получен
const planModal = ref(null)
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

async function renderFromAbc(abc) {
  if (!abc || !abc.trim()) return
  submitting.value = true
  try {
    await api.submit({
      ...payload({
        abc: abc,
        cot: cot.value === 'off' ? 'melody' : cot.value,   // abc требует full|melody
        seed: seed.value ? Number(seed.value) : Math.floor(Math.random() * 1e9),
      }),
      style: await finalStyle(),
    })
    planOpen.value = false
    await refresh()
  } finally { submitting.value = false }
}

async function loadJobAbc(j) {
  if (!j.abc_file) return
  planErr.value = ''
  try {
    const r = await fetch(`/audio/${j.id}/${j.abc_file}`)
    setPlanAbc(await r.text(), { seed: j.seed, seconds: null, truncated: false, fromJob: j.id })
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

// ---------- Метрики ----------

const metricsModal = ref(null)
function openMetrics(j, preset) {
  metricsModal.value.openFor(j, preset)
}

onMounted(async () => {
  onVolume()
  serverURL.value = await api.getServerURL()
  refresh()
  timer = setInterval(refresh, 3000)
  window.addEventListener('click', onWindowClick)
})
onUnmounted(() => { clearInterval(timer); window.removeEventListener('click', onWindowClick) })

function onWindowClick(e) {
  if (!e.target.closest('.slot-box')) openSlot.value = ''
}
</script>

<template>
  <header>
    <h1>Yue Studio</h1>
    <span class="health" :class="health ? 'up' : 'down'">
      {{ health ? 'сервер: готов' + (health.model_loaded ? ' (модель в памяти)' : '') : 'сервер недоступен' }}
    </span>
    <PlayerBar :jobs="jobs" @play-job="togglePlay" @refresh="refresh" />
    <span class="spacer"></span>
    <button class="ghost" @click="corpusPage = !corpusPage; libraryPage = false; settingsPage = false" title="Свои треки: импорт в студию (стемы, минус, эффекты, овердаб) и профили исполнителей из корпусов треков">свои треки</button>
    <button class="ghost" @click="toggleTheme" :title="theme === 'dark' ? 'Светлая тема' : 'Тёмная тема'">{{ theme === 'dark' ? '☀' : '☾' }}</button>
    <button class="ghost" @click="settingsPage = true; libraryPage = false; corpusPage = false" title="Страница настроек">⚙ настройки</button>
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
  <StudioPage v-else-if="studioJob" :job="studioJob" :auto-translate="autoTranslate"
              @close="studioJob = null; refresh()"
              @open-metrics="openMetrics"
              @voices-to-plan="(abc) => setPlanAbc(abc, { seed: studioJob.seed, seconds: null, truncated: false, fromJob: studioJob.id })" />

  <main v-else>
    <div class="left-col">
      <section class="panel lib">
        <h2>Библиотека стилей</h2>
        <VSelect v-model="libGroup" :options="libGroupOptions" placeholder="— группа —" @update:model-value="onLibGroupChange()" />
        <div style="margin-top:8px"></div>
        <VSelect v-model="libStyle" :options="libStyleOptions" :disabled="!libGroup" placeholder="— стиль —" @update:model-value="onLibStyleChange()" />
        <div v-if="currentItem" class="lib-actions">
          <button class="ghost" title="Применить точной строкой калибровки (1:1)" @click="onLibExact">строкой 1:1</button>
        </div>
        <p class="muted" style="margin:8px 0 0">
          <a @click="libraryPage = true" style="cursor:pointer">свои группы →</a>
        </p>
      </section>

      <section class="panel form">
        <h2>Новая композиция</h2>
        <input v-model="title" placeholder="Название" />

        <div class="slots">
          <div v-for="[key, label, hint, tip] in slotMeta" :key="key" class="slot-box" :title="tip">
            <span>{{ label }}</span>
            <input v-model="slots[key]" :placeholder="hint"
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

        <details>
          <summary>
            Стиль одной строкой
            <span v-if="styleOverride" class="muted">· переопределено вручную</span>
            <span v-else class="muted">· показывает выбранное выше</span>
          </summary>
          <textarea v-model="styleLine" rows="3" placeholder="заполни слоты выше — строка соберётся сама; или впиши свою (тогда она главнее)"></textarea>
          <button v-if="styleOverride" class="ghost small-btn" style="margin-top:6px"
                  title="Стереть ручную версию и снова собирать строку из слотов и стойки"
                  @click="styleOverride = ''">следовать за слотами</button>
        </details>

        <details>
          <summary>Инструментальная стойка <span class="muted">({{ rackSel.length }})</span></summary>
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
          <label class="lyrics-label">Стих (с тегами [Verse] / [Chorus] / [Outro]; строки с # — пометки, не поются)</label>
          <span class="lyrics-tools">
            <button class="toggle" :class="{ on: noLyrics }" title="Инструментал: стих не нужен, вместо него на воркер уйдёт техническая заглушка [Instrumental]" @click="noLyrics = !noLyrics">без слов</button>
            <select v-if="noLyrics" v-model="durMode" class="dur-select" title="Длина инструментала задаётся числом секций [Instrumental]">
              <option v-for="o in durOptions" :key="o.id" :value="o.id">{{ o.label }}</option>
            </select>
            <button v-if="!noLyrics" class="ghost small-btn" @click="copOpen = true">✎ копайтер…</button>
          </span>
        </div>
        <textarea v-model="lyrics" rows="10" :disabled="noLyrics" :placeholder="noLyrics ? 'не нужно: выбран инструментал' : ''"></textarea>

        <div class="row">
          <label class="seed">seed <input v-model.number="seed" type="number" placeholder="случайный" /></label>
          <div class="cot-radios" title="Как модель «думает» перед генерацией: полный разбор (структура лучше, дольше), только мелодия, или без размышлений (быстрее, структура проще). Для плана/ABC нужен полный или мелодия.">
            <span class="cot-title">генерация:</span>
            <label><input type="radio" value="full" v-model="cot" /> полная</label>
            <label><input type="radio" value="melody" v-model="cot" /> мелодия</label>
            <label><input type="radio" value="off" v-model="cot" /> без размышлений</label>
          </div>
          <label class="autotr" title="Слоты можно писать по-русски: перед отправкой строка стиля переводится в английский через Ollama (qwen2.5). Модель обучена на английских тегах.">
            <input type="checkbox" v-model="autoTranslate" /> рус → eng
          </label>
          <span class="compiled" :title="compiledStyle">→ {{ translateBusy ? 'перевожу…' : (compiledStyle || 'заполни слоты или строку стиля') }}</span>
        </div>

        <div class="actions">
          <button class="primary" :disabled="submitting || !canSubmit" @click="submit">
            {{ submitting ? '...' : 'В очередь' }}
          </button>
          <button class="primary alt" :disabled="submitting || !canSubmit" @click="submitFan(5)" title="5 джоб с сидами base+0..4 (best-of-N)">
            ×5 сидов
          </button>
          <button class="ghost" :disabled="planBusy || !canSubmit" @click="makePlan"
                  title="Показать план трека в нотной записи до рендера: можно поправить ноты и сгенерировать по ним">
            {{ planBusy ? 'план…' : 'ноты →' }}
          </button>
        </div>
      </section>
    </div>

    <section class="panel list">
      <h2>Очередь и результаты</h2>
      <p v-if="!jobs.length" class="muted">Пока пусто. Можно сгенерировать трек или загрузить свой — кнопка «свои треки» в шапке.</p>
      <article v-for="j in jobs" :key="j.id" class="job" :class="j.status">
        <div class="job-head">
          <strong>#{{ j.id }} {{ j.title }}</strong>
          <span class="status" :class="j.status">{{ statusLabel[j.status] || j.status }}</span>
          <span v-if="j.duration_sec" class="muted">{{ fmtDur(j.duration_sec) }}</span>
          <span v-if="j.seed" class="muted">seed {{ j.seed }}</span>
          <span v-if="j.cot && j.cot !== 'full'" class="muted">cot {{ j.cot }}</span>
          <span v-if="j.req_abc" class="badge" title="Рендер по своему ABC">свой ABC</span>
          <button v-if="j.status === 'queued'" class="ghost" @click="cancel(j.id)">отменить</button>
          <span class="spacer"></span>
          <button v-if="j.status !== 'running'" class="ghost icon del" title="Удалить результат" @click="deleteJob(j)">✕</button>
          <button class="ghost icon" title="Повторить с этими параметрами" @click="reuseJob(j)">↺</button>
        </div>
        <p class="muted style">{{ j.style }}</p>
        <p v-if="j.error" class="error">{{ j.error }}</p>
        <div v-if="j.status === 'done' && j.audio_file" class="job-actions">
          <button class="play-main" :disabled="playBusy['m' + j.id]" @click="togglePlay(j)">
            {{ playBtn('m' + j.id) === '…' ? 'загрузка…' : (isPlaying('m' + j.id) ? '■ стоп' : '▶ играть') }}
          </button>
          <button class="ghost stopbtn" title="Остановить воспроизведение" @click="stopAll()">■</button>
          <button v-if="isPlaying('m' + j.id) && playerState.playing" class="ghost" @click="api.toggleAudio()">⏸</button>
          <button class="ghost" @click="openListen(j)" title="Страница прослушивания в браузере">в браузере</button>
          <button v-if="j.status === 'done'" class="ghost" @click="studioJob = j">студия →</button>
          <button class="ghost icon" title="Сохранить без потерь (flac)" @click="saveAudio(j)">⤓ flac</button>
          <button class="ghost icon" :disabled="playBusy['mp3' + j.id]" title="Сохранить mp3 320 kbps (для импортированных — конвертируется при первом сохранении)" @click="saveMp3(j)">⤓ mp3</button>
          <button v-if="j.abc_file" class="ghost" @click="loadJobAbc(j)"
                  title="Нотная запись трека (ABC) — для продвинутых. Обычный путь: стили и студия, сюда можно не заходить">ноты (ABC)</button>
          <button v-if="j.abc_file" class="ghost icon" title="Сохранить партитуру (score.abc) в файл" @click="saveAudio(j, j.abc_file)">⤓ abc</button>
        </div>
      </article>
    </section>
  </main>

  <PlanModal v-model:abc="planAbc" :open="planOpen" :busy="planBusy" :err="planErr"
             :submitting="submitting" :info="planInfo"
             @close="planOpen = false" @render="renderFromAbc" @new-plan="makePlan" @from-track="transcribeFromTrack" />
  <CopilotModal :open="copOpen" :style="compiledStyle" :example="lyrics" :lang="slots.language"
                @close="copOpen = false" @insert="onCopInsert" />
  <MetricsModal ref="metricsModal" :jobs="jobs" />
  <ConfirmModal />
</template>

<style>
/* Стили приложения — глобальные: экранные компоненты (components/) рендерятся
   внутри этого корня и пользуются теми же классами. */
header {
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
.health { font-size: 12px; color: var(--muted); }
.health.up { color: var(--ok); text-shadow: 0 0 8px var(--lcd-glow); }
.health.down { color: var(--err); }
.settings-page { display: flex; justify-content: center; align-items: flex-start; }
.settings-page .panel { width: 640px; max-width: 100%; display: flex; flex-direction: column; gap: 12px; }
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
.playerbar { display: flex; align-items: center; gap: 8px; font-size: 12px; }
.playerbar .seek { width: 180px; }
.playerbar .vol { display: flex; align-items: center; gap: 4px; color: var(--muted); }
.playerbar .vol input { width: 80px; }
.playerbar .now { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.playerbar .pos {
  font-variant-numeric: tabular-nums;
  background: var(--lcd-bg); color: var(--lcd-text);
  font-family: 'DejaVu Sans Mono', 'Consolas', monospace;
  padding: 2px 8px; border-radius: 2px; border: 1px solid var(--border);
  box-shadow: inset 0 0 6px rgba(0,0,0,.55); text-shadow: 0 0 6px var(--lcd-glow);
}
.playerbar .ghost { padding: 2px 8px; }
.spacer { flex: 1; }

main { display: grid; grid-template-columns: 440px 1fr; gap: 20px; padding: 20px 24px; max-width: 1400px; margin: 0 auto; }
@media (max-width: 1000px) { main { grid-template-columns: 1fr; } }
.left-col { display: flex; flex-direction: column; gap: 20px; }
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

.slots { display: grid; grid-template-columns: 1fr 1fr; gap: 8px 12px; margin: 12px 0; }
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
.seed input { width: 110px; }
.cot-radios { display: flex; align-items: center; gap: 10px; font-size: 12px; color: var(--muted); }
.cot-radios label { display: flex; align-items: center; gap: 4px; cursor: pointer; }
.cot-title { font-weight: 600; }
.compiled { font-size: 11px; color: var(--muted); margin: 0 0 10px; word-break: break-word; }
.autotr { font-size: 12px; color: var(--muted); display: flex; align-items: center; gap: 4px; cursor: pointer; }

.actions { display: flex; gap: 8px; }
.actions .primary { flex: 0 0 auto; }
.primary { background: var(--ok); padding: 6px 16px; }
.primary.alt { background: var(--panel2); border: 1px solid var(--ok); color: var(--ok); }

.job { border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; margin-bottom: 10px; background: var(--panel2); }
.job-head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.status { font-size: 12px; padding: 2px 8px; border-radius: 10px; background: var(--border); }
.status.done { background: rgba(255,190,61,.16); color: var(--lcd-text); text-shadow: 0 0 8px var(--lcd-glow); }
.status.running { background: rgba(217,160,61,.2); color: var(--run); }
.status.error { background: rgba(224,93,61,.2); color: var(--err); }
.style { font-size: 12px; margin: 6px 0; }
.error { color: var(--err); font-size: 13px; }
.job-actions { display: flex; gap: 8px; margin-top: 6px; align-items: center; }
.job-actions button { font-size: 12px; padding: 4px 10px; }
.muted { color: var(--muted); }
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
.corpus-tracks { width: 100%; display: flex; flex-direction: column; gap: 4px; margin-top: 4px; }
.corpus-track { font-size: 12px; display: flex; flex-wrap: wrap; gap: 6px; align-items: baseline; }
.corpus-track .track-lyrics { width: 100%; margin: 0; color: var(--muted); font-style: italic; opacity: .8; }

/* пиано-ролл */
.roll-block { margin-top: 8px; padding: 10px; border: 1px dashed var(--border); border-radius: 8px; overflow-x: auto; }
.roll-voices { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-bottom: 10px; font-size: 12px; }
.studio-page .roll-block { border: none; padding: 0; overflow: visible; }
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
.roll-cell { height: 18px; border-radius: 3px; cursor: pointer; border: 1px solid transparent; }
.roll-cell.d0 { background: var(--panel); }
.roll-cell.d1 { background: rgba(120,140,255,.18); }
.roll-cell.d2 { background: rgba(120,140,255,.42); }
.roll-cell.d3 { background: rgba(120,140,255,.72); }
.roll-cell.sel { border-color: var(--ok); box-shadow: 0 0 0 1px var(--ok); }
.roll-cell.off { opacity: .3; }
.roll-chord { font-size: 9px; color: var(--muted); text-align: center; overflow: hidden; }
.roll-actions { display: flex; align-items: center; gap: 10px; margin-top: 8px; font-size: 12px; }

/* овердаб */
.od-chips { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 8px; }
.od-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.od-style { flex: 1; min-width: 240px; }
.od-gain { font-size: 12px; color: var(--muted); display: flex; align-items: center; gap: 6px; }

/* стемы */
.stem-row { display: flex; align-items: center; gap: 10px; font-size: 13px; }
</style>
