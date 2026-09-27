<script setup>
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { api } from './api.js'
import { presets } from './presets.js'
import { rackGroups, rackEffects, rackCompile, allRackItems } from './rack.js'

const serverURL = ref('')
const serverURLEdit = ref(false)

const slots = ref({
  language: 'Russian',
  genre: '',
  rhythm: '',
  guitars: '',
  keys: '',
  vocals: '',
  mood: '',
  production: '',
  bpm: null,
})
const styleOverride = ref('')
const lyrics = ref(`[Verse]\n...\n\n[Chorus]\n...`)
const seed = ref(null)
const cot = ref('full')
const title = ref('')
const autoTranslate = ref(true)   // слоты по-русски → перевод в английскую строку стиля
const translateBusy = ref(false)
const submitting = ref(false)
const jobs = ref([])
const health = ref(null)
const playerState = ref({ playing: false, position_sec: 0, duration_sec: 0, job_id: 0 })
let timer = null

// инструментальная стойка: [{id, effect}] — разворачивается в текст стиля
const rackSel = ref([])
const nowPlaying = ref('')   // подпись встроенного плеера

// подтверждение действия (общая модалка)
const confirmOpen = ref(false)
const confirmTitle = ref('')
const confirmBody = ref('')
let confirmFn = null

function askConfirm(title, body, fn) {
  confirmTitle.value = title
  confirmBody.value = body
  confirmFn = fn
  confirmOpen.value = true
}

function doConfirm() {
  confirmOpen.value = false
  if (confirmFn) { const f = confirmFn; confirmFn = null; f() }
}

// скрытие пресетов (локально, с восстановлением)
const hiddenPresets = ref(JSON.parse(localStorage.getItem('yue_hidden_presets') || '[]'))
const visiblePresets = computed(() => presets.filter(p => !hiddenPresets.value.includes(p.id)))

function hidePreset(p) {
  askConfirm(`Убрать пресет «${p.name}»?`,
    'Пресет скрывается из библиотеки (это обратимо: под списком появится «восстановить все»).',
    () => {
      hiddenPresets.value = [...hiddenPresets.value, p.id]
      localStorage.setItem('yue_hidden_presets', JSON.stringify(hiddenPresets.value))
    })
}

function restorePresets() {
  hiddenPresets.value = []
  localStorage.removeItem('yue_hidden_presets')
}

async function deleteJob(j) {
  askConfirm(`Удалить результат #${j.id} «${j.title}»?`,
    'Удалится запись и все файлы (аудио, партитура, стемы, превью) на сервере 184. Отменить нельзя.',
    async () => {
      try { await api.deleteJob(j.id) } catch (e) { alert(String(e)) }
      refresh()
    })
}

function rackToggle(itemId) {
  const i = rackSel.value.findIndex(r => r.id === itemId)
  if (i >= 0) rackSel.value = rackSel.value.filter(r => r.id !== itemId)
  else rackSel.value = [...rackSel.value, { id: itemId, effect: '' }]
}

function rackSetEffect(itemId, effect) {
  rackSel.value = rackSel.value.map(r => (r.id === itemId ? { ...r, effect } : r))
}

// пиано-ролл: расклад по тактам + превью фрагментов
const rollOpen = ref({})
const rollData = ref({})   // jobId -> parsed score
const rollBusy = ref({})
const rollErr = ref({})
const rollSel = ref({})    // jobId -> {a: barIdx, b: barIdx}
let rollDrag = false
const previewBusy = ref({})

// такты строками по 32 — иначе сетка шириной в сотни тактов
function rollChunks(j) {
  const bars = rollData.value[j.id] && rollData.value[j.id].bars
  if (!bars) return []
  const out = []
  for (let i = 0; i < bars.length; i += 32) out.push(bars.slice(i, i + 32))
  return out
}

function onWindowMouseup() { rollDrag = false }

// овердаб
const odOpen = ref({})
const odStyle = ref({})
const odGain = ref({})
const odBusy = ref({})

// единый тумблер воспроизведения: ▶ (…загрузка) ■
// key — уникальный идентификатор артефакта: 'm15' — трек, 's15:stem-bass' — стем
const playBusy = ref({})
const nowPlayingKey = ref('')

function isPlaying(key) {
  return playerState.value.job_id > 0 && nowPlayingKey.value === key
}

async function toggleArtifact(j, key, label, play) {
  if (playBusy.value[key]) return
  if (isPlaying(key)) {
    await api.stopAudio()
    nowPlayingKey.value = ''
    setTimeout(refresh, 200)
    return
  }
  playBusy.value = { ...playBusy.value, [key]: true }
  nowPlaying.value = label
  try {
    await play()
    nowPlayingKey.value = key
  } catch (e) {
    nowPlaying.value = `звук: ${String(e)}`
  } finally {
    playBusy.value = { ...playBusy.value, [key]: false }
    setTimeout(refresh, 300)
  }
}

function playBtn(key) {
  if (playBusy.value[key]) return '…'
  return isPlaying(key) ? '■' : '▶'
}

// стемы
const stemsOpen = ref({})
const stemsList = ref({})
const stemsBusy = ref({})

// профиль из корпуса
const corpora = ref([])
const corpusName = ref('')
const corpusBusy = ref(false)
const corpusErr = ref('')
const corpusProfile = ref({}) // id -> profile
const corpusTracks = ref({}) // id -> разбор треков

async function loadCorpora() {
  try {
    corpora.value = (await api.corpusList()) || []
    for (const c of corpora.value) {
      if (c.tracks > 0 && !corpusTracks.value[c.id]) {
        try { corpusTracks.value = { ...corpusTracks.value, [c.id]: await api.corpusTracks(c.id) } } catch {}
      }
    }
  } catch {}
}

// редактор плана (ABC до рендера)
const planOpen = ref(false)
const planAbc = ref('')
const planBusy = ref(false)
const planInfo = ref(null)   // {seed, seconds, truncated} — как план получен
const planErr = ref('')

// DSP-метрики + дельта до референса/другой джобы
const metricsOpen = ref(false)
const metricsJob = ref(null)
const metricsTitle = ref('')   // подзаголовок, если показываем не исходник (DSP-вариант)
const metrics = ref(null)
const metricsBusy = ref(false)
const metricsErr = ref('')
const refs = ref([])
const cmpTarget = ref('')    // '' | 'ref:<id>' | 'job:<id>'
const cmpMetrics = ref(null)
const cmpBusy = ref(false)
const addingRef = ref(false)

const metricRows = [
  ['tempo_bpm', 'Темп', 'BPM', 1],
  ['crest_db', 'Крест-фактор', 'dB', 1],
  ['dyn_range_db', 'Дин. диапазон', 'dB', 1],
  ['rms_p95_db', 'RMS p95', 'dBFS', 1],
  ['noise_floor_db', 'Шумовое дно', 'dBFS', 1],
  ['signal_noise_db', 'Сигнал/шум-полотно', 'dB', 1],
  ['clip_pct', 'Сэмплов у потолка', '%', 2],
  ['centroid_hz', 'Центроид', 'Гц', 0],
  ['f95_hz', '95% энергии ниже', 'Гц', 0],
  ['flatness_loud', 'Флэтнес в громких', '', 3],
  ['stereo_corr', 'Стерео-корреляция', '', 2],
  ['bands.bass', 'Бас <150 Гц', '%', 1],
  ['bands.low_mid', 'Низ-середина 150–500', '%', 1],
  ['bands.mid', 'Середина 0.5–2к', '%', 1],
  ['bands.high', 'Верх 2–8к', '%', 1],
  ['bands.air', 'Воздух 8к+', '%', 1],
]

function metricVal(m, key) {
  if (!m) return null
  if (key.startsWith('bands.')) return m.bands ? m.bands[key.slice(6)] : null
  return m[key]
}

function fmtMetric(v, dec, unit) {
  if (v === null || v === undefined) return '—'
  const num = Number(v).toFixed(dec)
  return num + (unit ? ' ' + unit : '')
}

function deltaStr(key, dec) {
  const a = metricVal(metrics.value, key)
  const b = metricVal(cmpMetrics.value, key)
  if (a === null || a === undefined || b === null || b === undefined) return ''
  const d = Number(a) - Number(b)
  return (d > 0 ? '+' : '') + d.toFixed(dec)
}

const doneJobs = computed(() => jobs.value.filter(j => j.status === 'done' && j.id !== (metricsJob.value && metricsJob.value.id)))

function refName(id) {
  return String(id).replace(/^\d+-/, '')
}

async function openMetrics(j) {
  metricsJob.value = j
  metricsTitle.value = ''
  metricsOpen.value = true
  metricsErr.value = ''
  metrics.value = null
  cmpTarget.value = ''
  cmpMetrics.value = null
  metricsBusy.value = true
  try {
    loadRefs()
    metrics.value = await api.analyze(j.id)
  } catch (e) {
    metricsErr.value = String(e)
  } finally {
    metricsBusy.value = false
  }
}

async function loadRefs() {
  try { refs.value = (await api.references()) || [] } catch {}
}

async function onCmpChange() {
  cmpMetrics.value = null
  if (!cmpTarget.value) return
  cmpBusy.value = true
  try {
    const [kind, id] = cmpTarget.value.split(':')
    if (kind === 'ref') {
      const r = refs.value.find(x => x.id === id)
      cmpMetrics.value = r ? r.metrics : null
    } else if (kind === 'job') {
      cmpMetrics.value = await api.analyze(Number(id))
    }
  } catch (e) {
    metricsErr.value = String(e)
  } finally {
    cmpBusy.value = false
  }
}

async function addReference() {
  addingRef.value = true
  try {
    const r = await api.addReference()
    if (r) {
      await loadRefs()
      cmpTarget.value = 'ref:' + r.id
      await onCmpChange()
    }
  } finally { addingRef.value = false }
}

// DSP-цепочки (ffmpeg на ПК): выбор, крутилки, варианты с дельтами
const dspChains = ref([])
const dspOpen = ref({})     // jobId -> bool (блок раскрыт)
const dspSel = ref({})      // jobId -> chainId
const dspParams = ref({})   // jobId -> {paramId: value}
const dspBusy = ref({})     // jobId -> bool
const dspVariants = ref({}) // jobId -> [{file, created_at, metrics}]
const jobMetrics = ref({})  // jobId -> метрики исходника (для инлайн-дельт)

function curChain(j) {
  return dspChains.value.find(c => c.id === dspSel.value[j.id]) || null
}

function chainLabel(file) {
  const id = String(file).replace(/^dsp-/, '').replace(/\.flac$/, '')
  const c = dspChains.value.find(x => x.id === id)
  return c ? c.name : id
}

async function toggleDsp(j) {
  const open = !dspOpen.value[j.id]
  dspOpen.value = { ...dspOpen.value, [j.id]: open }
  if (open) {
    if (!dspChains.value.length) {
      try { dspChains.value = (await api.dspChains()) || [] } catch {}
    }
    await reloadDsp(j)
  }
}

async function reloadDsp(j) {
  try {
    if (!dspVariants.value[j.id] || !jobMetrics.value[j.id]) {
      const [vs, m] = await Promise.all([
        dspVariants.value[j.id] ? Promise.resolve(null) : api.dspVariants(j.id),
        jobMetrics.value[j.id] ? Promise.resolve(null) : api.analyze(j.id),
      ])
      if (vs) dspVariants.value = { ...dspVariants.value, [j.id]: vs || [] }
      if (m) jobMetrics.value = { ...jobMetrics.value, [j.id]: m }
    } else {
      const vs = await api.dspVariants(j.id)
      dspVariants.value = { ...dspVariants.value, [j.id]: vs || [] }
    }
  } catch {}
}

function selChain(j, chainId) {
  dspSel.value = { ...dspSel.value, [j.id]: chainId }
  const c = dspChains.value.find(x => x.id === chainId)
  const p = {}
  if (c) for (const prm of c.params) p[prm.id] = prm.default
  dspParams.value = { ...dspParams.value, [j.id]: p }
}

async function applyDsp(j) {
  const c = curChain(j)
  if (!c) return
  dspBusy.value = { ...dspBusy.value, [j.id]: true }
  try {
    if (!jobMetrics.value[j.id]) {
      jobMetrics.value = { ...jobMetrics.value, [j.id]: await api.analyze(j.id) }
    }
    await api.applyDsp(j.id, c.id, dspParams.value[j.id] || {})
    const vs = await api.dspVariants(j.id)
    dspVariants.value = { ...dspVariants.value, [j.id]: vs || [] }
  } catch (e) {
    metricsErr.value = String(e)
  } finally {
    dspBusy.value = { ...dspBusy.value, [j.id]: false }
  }
}

function num(v) { return (v === null || v === undefined) ? null : Number(v) }

function vDelta(j, v, key, dec = 1) {
  const a = jobMetrics.value[j.id]
  if (!a || !v.metrics) return ''
  let av = a[key], bv = v.metrics[key]
  if (key.startsWith('bands.')) {
    const k = key.slice(6)
    av = a.bands && a.bands[k]
    bv = v.metrics.bands && v.metrics.bands[k]
  }
  if (av === null || av === undefined || bv === null || bv === undefined) return ''
  const d = Number(bv) - Number(av)   // вариант минус исходник
  return (d > 0 ? '+' : '') + d.toFixed(dec)
}

function playVariant(j, v) {
  toggleArtifact(j, `v${j.id}:${v.file}`, `${chainLabel(v.file)} · #${j.id}`,
    () => api.playFile(j.id, v.file, j.duration_sec))
}

async function openVariantMetrics(j, v) {
  metricsJob.value = j
  metricsOpen.value = true
  metricsErr.value = ''
  metrics.value = v.metrics
  metricsTitle.value = `${chainLabel(v.file)} · #${j.id}`
  if (!jobMetrics.value[j.id]) {
    try { jobMetrics.value = { ...jobMetrics.value, [j.id]: await api.analyze(j.id) } } catch {}
  }
  cmpTarget.value = 'job:' + j.id
  await onCmpChange()
}

const slotMeta = [
  ['language', 'Язык', 'Russian / English / ...'],
  ['genre', 'Жанр', 'dark post-punk, Siberian punk...'],
  ['rhythm', 'Ритм-секция', 'driving bass, drum machine...'],
  ['guitars', 'Гитары', 'fuzz drone, clean arpeggio...'],
  ['keys', 'Клавиши/другое', 'Hammond organ, synth pad...'],
  ['vocals', 'Вокал', 'flat half-spoken male, buried...'],
  ['mood', 'Настроение', 'bleak hopelessness...'],
  ['production', 'Продакшн', 'lo-fi home tape murk...'],
]

// известные значения для подсказок полей: откалиброванные (из пресетов)
// + рабочие расширения; поле остаётся свободным, список — только подсказка
// подсказки полей: [русское название, английская формулировка для YuE].
// Поле остаётся свободным: выбранное из списка хранится по-русски, при отправке
// заменяется на английский вариант; самописное с кириллицей идёт в автоперевод.
const slotOptions = {
  language: [['Русский', 'Russian'], ['English', 'English']],
  genre: [
    ['1960-е психоделический блюз-рок', '1960s psychedelic blues rock'],
    ['1980-е сибирский андеграунд, панк-энергия поверх пост-панк тоски', '1980s Siberian underground rock, punk energy over post-punk gloom'],
    ['1980-е сибирский андеграунд, медленный минорный отпевальный рок', '1980s Siberian underground rock, slow minor key dirge'],
    ['acid folk', 'acid folk'],
    ['агрессивный пост-панк', 'aggressive post-punk'],
    ['эмбиент', 'ambient'],
    ['арт-панк', 'art punk'],
    ['атмосферный блэк-метал', 'atmospheric black metal'],
    ['баллада', 'ballad'],
    ['бибоп', 'bebop'],
    ['унылый лоу-фай сибирский панк', 'bleak lo-fi Siberian punk'],
    ['унылая медленная серая песня', 'bleak slow grey song'],
    ['блюграсс', 'bluegrass'],
    ['блюз', 'blues'],
    ['блюз-рок', 'blues rock'],
    ['босса-нова', 'bossa nova'],
    ['брейкбит', 'breakbeat'],
    ['брит-поп', 'britpop'],
    ['кельтский фолк', 'celtic folk'],
    ['шансон', 'chanson'],
    ['chillwave', 'chillwave'],
    ['чиптюн', 'chiptune'],
    ['колдвейв', 'coldwave'],
    ['кантри', 'country'],
    ['кантри-баллада', 'country ballad'],
    ['краст-панк', 'crust punk'],
    ['дарк-эмбиент', 'dark ambient'],
    ['дарк-кабаре', 'dark cabaret'],
    ['дарк-фолк', 'dark folk'],
    ['мрачный пост-панк', 'dark post-punk'],
    ['дарквейв', 'darkwave'],
    ['дэт-метал', 'death metal'],
    ['дет-рок', 'deathrock'],
    ['диско', 'disco'],
    ['дум-метал', 'doom metal'],
    ['даунтемпо', 'downtempo'],
    ['дрон', 'drone'],
    ['данжен-синт', 'dungeon synth'],
    ['даб', 'dub'],
    ['даб-техно', 'dub techno'],
    ['евродэнс 1997', 'eurodance 1997'],
    ['экспериментальный нойз', 'experimental noise'],
    ['фламенко', 'flamenco'],
    ['фолк-рок с мировой музыкой', 'folk rock, world music shades'],
    ['фьюнерал-дум', 'funeral doom'],
    ['фанк', 'funk'],
    ['гаражный рок', 'garage rock'],
    ['глэм-рок', 'glam rock'],
    ['госпел', 'gospel'],
    ['готик-метал', 'gothic metal'],
    ['готический пост-панк с диким панком', 'gothic post-punk gloom with raw punk energy'],
    ['грайм', 'grime'],
    ['грайндкор', 'grindcore'],
    ['гранж', 'grunge'],
    ['хард-рок', 'hard rock'],
    ['хардкор-панк', 'hardcore punk'],
    ['хэви-метал', 'heavy metal'],
    ['хип-хоп', 'hip-hop'],
    ['хорроркор', 'horrorcore'],
    ['хаус', 'house'],
    ['хайперпоп', 'hyperpop'],
    ['IDM', 'idm'],
    ['инди-фолк', 'indie folk'],
    ['инди-поп', 'indie pop'],
    ['индастриал', 'industrial'],
    ['индастриал-метал', 'industrial metal'],
    ['индастриал-рок', 'industrial rock'],
    ['джаз-фьюжн', 'jazz fusion'],
    ['джаз-нуар', 'jazz noir'],
    ['клезмер', 'klezmer'],
    ['клезмер-панк', 'klezmer punk'],
    ['краут-рок', 'krautrock'],
    ['латино-рок', 'latin rock'],
    ['мэт-рок', 'math rock'],
    ['мелодичный дэт-метал', 'melodic death metal'],
    ['мелодичный хардкор', 'melodic hardcore'],
    ['моутаун', 'motown'],
    ['мистический арт-рок', 'mystic art rock'],
    ['неоклассический дарквейв', 'neoclassical darkwave'],
    ['неофолк', 'neofolk'],
    ['неофолк-баллада, модальный минор', 'neofolk ballad, modal minor'],
    ['нью-романтик', 'new romantic'],
    ['новая волна', 'new wave'],
    ['нойз-рок', 'noise rock'],
    ['ну-диско', 'nu disco'],
    ['ню-метал', 'nu metal'],
    ['пост-боп', 'post-bop'],
    ['пост-гранж', 'post-grunge'],
    ['пост-хардкор', 'post-hardcore'],
    ['пост-метал', 'post-metal'],
    ['пост-панк', 'post-punk'],
    ['пост-рок, атмосферные инструментальные волны', 'post-rock, atmospheric instrumental waves'],
    ['пауэр-баллада', 'power ballad'],
    ['пауэр-метал', 'power metal'],
    ['пауэр-поп', 'power pop'],
    ['прогрессив-метал', 'progressive metal'],
    ['прогрессив-рок', 'progressive rock'],
    ['психодел-фолк', 'psychedelic folk'],
    ['психодел-рок', 'psychedelic rock'],
    ['сайкобилли', 'psychobilly'],
    ['панк', 'punk'],
    ['панк-блюз', 'punk blues'],
    ['ритм-н-блюз, соул', 'r&b soul'],
    ['рэгтайм', 'ragtime'],
    ['рэп-рок', 'rap rock'],
    ['регги', 'reggae'],
    ['реггетон', 'reggaeton'],
    ['ритмичный нойз', 'rhythmic noise'],
    ['рокабилли', 'rockabilly'],
    ['сырой лоу-фай сибирский панк, советский андеграунд 1980-х', 'raw lo-fi Siberian punk, 1980s Soviet underground punk'],
    ['шугейз', 'shoegaze'],
    ['авторская песня', 'singer-songwriter'],
    ['ска', 'ska'],
    ['ска-панк', 'ska punk'],
    ['слакер-рок', 'slacker rock'],
    ['сладж-метал', 'sludge metal'],
    ['слоукор', 'slowcore'],
    ['саузерн-рок', 'southern rock'],
    ['спейс-рок', 'space rock'],
    ['стонер-рок', 'stoner rock'],
    ['сёрф-рок', 'surf rock'],
    ['свинг', 'swing'],
    ['синт-фанк', 'synth-funk'],
    ['синтпоп', 'synthpop'],
    ['синтвейв', 'synthwave'],
    ['новое танго', 'tango nuevo'],
    ['техничный дэт-метал', 'technical death metal'],
    ['театральный хоррор-панк', 'theatrical horror punk'],
    ['трэш-метал', 'thrash metal'],
    ['традиционный фолк', 'traditional folk'],
    ['трип-хоп', 'trip-hop'],
    ['вейпорвейв', 'vaporwave'],
    ['world fusion', 'world fusion'],
    ['яхт-рок', 'yacht rock'],
  ],
  rhythm: [
    ['гулкая чихающая бас-гитара впереди, драм-машина и коробочная перкуссия, быстрая короткая песня', 'booming sputtering bass guitar at the forefront, drum machine and box percussion, fast short song'],
    ['бум-бэп, качающий свинг', 'boom bap beat, head-nodding swing'],
    ['брейкбит, нарезанный фанк', 'breakbeat, chopped funk drums'],
    ['двойная бочка', 'double kick blast drums'],
    ['напористая повторяющаяся бас-линия, ровный энергичный грув', 'driving repetitive bassline, steady energetic groove'],
    ['даб-бас с космическими дилэями', 'dub bassline with spacey delays'],
    ['ди-бит', 'd-beat drumming'],
    ['этническая перкуссия', 'ethnic percussion'],
    ['быстрая панк-энергия', 'fast punk energy'],
    ['четыре-на-пол драм-машина', 'four-on-the-floor drum machine'],
    ['фанковый синкопированный грув', 'funky syncopated groove'],
    ['глитчевые IDM-биты', 'glitchy IDM beats'],
    ['хаф-тайм грув, тяжёлый снейр', 'half-time groove, heavy snare'],
    ['тяжёлый переваливающийся бас, глухие картонные барабаны', 'heavy loping bass pulse, muffled cardboard drums'],
    ['тяжёлый регги-уклон баса, картонные барабаны, средний темп', 'heavy loping reggae-leaning bass pulse, cardboard drums, mid-tempo lilt'],
    ['гипнотический грув', 'hypnotic groove'],
    ['джазовый свинг-шаффл, щётки', 'jazz swing shuffle, brushed drums'],
    ['латинская клаве, конги', 'latin clave rhythm, congas'],
    ['средний темп', 'mid-tempo'],
    ['моторик-бит (краут)', 'motorik krautrock beat'],
    ['one drop грув, тёплый круглый бас', 'one drop groove, warm round bass'],
    ['молотящие барабаны', 'pounding drums'],
    ['программные трэп-хэты, 808-бас', 'programmed trap hi-hats, 808 bass'],
    ['медленно', 'slow'],
    ['медленный дум-шаг', 'slow doom plod'],
    ['мягкие барабаны, нарастают и сходят', 'soft drums building and receding'],
    ['разреженная драм-машина, дешёвый пресет', 'sparse drum machine, cheap preset rhythm'],
    ['свинг-шаффл, ведущий райд', 'swing shuffle, ride cymbal lead'],
    ['синкопированная фанковая бас-линия', 'syncopated funky bassline'],
    ['трип-хоп бит, пыльные брейки', 'trip-hop beat, dusty breaks'],
    ['вальс 3/4', 'waltz 3/4'],
    ['маршевый снейр', 'marching snare pattern'],
  ],
  guitars: [
    ['акустический бой', 'acoustic guitar strum'],
    ['агрессивные заниженные перегруженные риффы', 'aggressive down-tuned distorted guitar riffs'],
    ['блюзовые гитарные фразы', 'bluesy guitar licks'],
    ['звенящая 12-струнная', 'chiming 12-string guitar'],
    ['чистые арпеджио электрогитары', 'clean electric guitar arpeggios'],
    ['чистая щипковая гитара, интимно', 'clean plucked guitar, intimate'],
    ['дешёвая перегруженная дрон-гитара', 'cheap distorted drone guitar'],
    ['дешёвая гудящая гитара', 'cheap droning guitar'],
    ['дешёвая гудящая овердрайв-гитара', 'cheap droning overdriven guitar'],
    ['дешёвая овердрайв-гитара, примитивные аккорды', 'cheap overdriven guitar, primitive chords'],
    ['дешёвая вязкая овердрайв-гитара', 'cheap sludgy overdriven guitar'],
    ['гитары в дилэях', 'delay-drenched guitars'],
    ['дважды записанные гитары по краям', 'double-tracked hard-panned guitars'],
    ['гудящая дешёвая гитара', 'droning cheap guitar'],
    ['пальцевый фолк-подхват', 'fingerpicked folk guitar'],
    ['фламенко-расцветы', 'flamenco flourishes'],
    ['фузз-стена гитар', 'fuzz wall of guitars'],
    ['арфа и акустика', 'harp and acoustic guitar'],
    ['высокий пронзительный слайд', 'high piercing slide guitar'],
    ['джангл-поп гитары', 'jangle pop guitars'],
    ['металлические лязгающие гитары', 'metallic clangorous guitars'],
    ['сёрф-гитара с пружинным ревером', 'surf guitar with spring reverb'],
    ['оффбит-скэнк гитара', 'offbeat skank guitar'],
    ['педал-стил плачет', 'pedal steel guitar weep'],
    ['пауэр-аккорды, глухое чередование', 'power chords, palm-muted chug'],
    ['сырые овердрайв-гитары', 'raw overdriven guitars'],
    ['слайд-гитара', 'slide guitar lines'],
    ['стаккато арпеджио', 'staccato arpeggiated guitars'],
    ['тремоло-гитара', 'tremolo-picked guitar'],
    ['сдвоенные гитарные гармонии', 'twin guitar harmonies'],
    ['стена мерцающих реверб-гитар', 'wall of shimmering reverb guitars'],
  ],
  keys: [
    ['яркий синт-лид-хук', 'bright synth lead hook'],
    ['блёстки челесты', 'celesta sparkle'],
    ['хаотичные скретчи', 'chaotic turntable scratches'],
    ['расстроенное прямое пианино', 'detuned upright piano'],
    ['фолк-аккордеонные акценты', 'folk accordion accents'],
    ['блёстки глокеншпиля', 'glockenspiel twinkle'],
    ['Хэммонд-орган', 'Hammond organ'],
    ['Хэммонд пузырит', 'Hammond organ bubbles'],
    ['клавесин', 'harpsichord'],
    ['мелодика', 'melodica'],
    ['музыкальная шкатулка', 'music box plucks'],
    ['меллотрон-флейты', 'mellotron flutes'],
    ['Родес', 'Rhodes electric piano'],
    ['пэд струнного квартета', 'string quartet pad'],
    ['вывания терменвокса', 'theremin wails'],
    ['винтажные аналоговые синт-пэды', 'vintage analog synth pads'],
    ['винтажное электропианино, аккорды', 'vintage electric piano chords'],
    ['тёплый аналоговый пэд', 'warm analog synth pad'],
    ['деревянная флейта', 'wooden flute'],
    ['вурлитцер-удары', 'wurlitzer stabs'],
  ],
  vocals: [
    ['воздушный эфирный женский вокал', 'airy ethereal female vocals'],
    ['ангельский хор на бэках', 'angelic choir backing'],
    ['андрогинный воздушный вокал', 'androgynous airy vocals'],
    ['цепкий радостный мужской вокал', 'catchy cheerful male vocals'],
    ['детский женский вокал', 'childlike female vocals'],
    ['хор поёт в унисон', 'choir chant in unison'],
    ['депрессивный баритон', 'deadpan baritone male vocals'],
    ['отчаянный хриплый мужской вокал', 'desperate raspy male vocals'],
    ['угрюмый вокал с перегруженным клиппингующим микрофоном', 'dour male vocals with overdriven microphone clipping'],
    ['утопленный мужской вокал на грани срыва, монотонная декламация, гэнг-крики на бэках', 'drowned buried male vocals on the verge of breaking, monotone incantation delivery, gang shout backing vocals'],
    ['дуэт мужской и женский', 'duet male and female vocals'],
    ['мужской фальцет', 'falsetto male vocals'],
    ['плоский низкий полуречитатив, депрессивная подача, утоплен в ленточном шуме', 'flat low half-spoken male vocals, deadpan delivery buried in tape noise'],
    ['плоский монотонный низкий мужской голос, полуречитатив, отстранённая подача, вокал в шуме ленты', 'flat monotonous low male voice, half-spoken deadpan singing, detached emotionless delivery, vocals buried in tape noise'],
    ['полуречитативный харизматичный мужской баритон', 'half-spoken charismatic male baritone'],
    ['гортанный дэт-гроул', 'guttural death growls'],
    ['высокий надтреснутый мужской вокал на грани слёз', 'high strained male vocals, on the verge of tears'],
    ['надтреснутый любительский вокал, без автотюна', 'high cracked amateur male vocals, off-key, no autotune, no vocal runs, unpolished raw singing'],
    ['дикое немелодичное пение, срывы', 'wild tuneless singing, voice cracking, off-pitch, no autotune, no melisma'],
    ['любительский голос без обработки', 'untrained amateur voice, no vocal processing, dry unpolished delivery'],
    ['анти-поп подача', 'no pop vocal runs, no autotune, no modern production, plain blunt singing'],
    ['манера Моррисона', 'deep resonant baritone, swaggering theatrical delivery, spoken-word verses building to shouted incantation'],
    ['манера Кобейна', 'raspy desperate male vocals, cracked voice, screamed chorus, off-key sloppy punk delivery, double-tracked'],
    ['манера Марли', 'laid-back soulful raspy male vocals, reggae phrasing, offbeat accents, warm relaxed delivery'],
    ['манера Летова', 'flat monotonous low male voice, half-spoken deadpan incantation, detached, buried in tape noise'],
    ['расслабленный соуловый мужской вокал', 'laid-back soulful male vocals'],
    ['низкий резонирующий бас-вокал', 'low resonant bass vocals'],
    ['оперное женское сопрано', 'operatic female soprano'],
    ['рэп-флоу, ритмичная подача', 'rap flow, rhythmic delivery'],
    ['крик и гроул', 'screamed and growled male vocals'],
    ['выкрикнутый отчаянный мужской вокал', 'shouted desperate male vocals'],
    ['пронзительный женский вокал', 'shrill female vocals'],
    ['гладкое мужское напевание, крунинг', 'smooth crooning male vocals'],
    ['парящий мощный женский вокал', 'soaring powerful female vocals'],
    ['мужской вокал рассказчика, драматичная подача, подпевающий хор', 'storytelling male vocals with dramatic delivery, singalong gang chorus'],
    ['дерзкий мужской баритон', 'swaggering baritone male vocals'],
    ['горловое пение, обертоны', 'throat singing overtone drones'],
    ['вокодерный робо-вокал', 'vocoder robot vocals'],
    ['тёплый интимный женский вокал', 'warm intimate female vocals'],
    ['тёплый интимный мужской вокал', 'warm intimate male vocals'],
    ['усталый надтреснутый низкий голос с мукой, полуречитатив, в грязи', 'weary strained low male voice cracking with anguish, half-spoken, buried in mud'],
    ['шёпотный спокен-ворд поверх саундскейпа', 'whispered spoken-word male recitation over soundscape'],
    ['шёпотный женский вокал, близкий микрофон', 'whispered female vocals, close-miked'],
  ],
  mood: [
    ['абразивно, срочно, энергично', 'abrasive, urgent, energetic'],
    ['гимново-триумфально', 'anthemic triumphant'],
    ['атмосферно, смурно', 'atmospheric, moody'],
    ['горько-сладкая ностальгия', 'bittersweet nostalgia'],
    ['мрачно, свирепо и срочно', 'bleak fierce and urgent'],
    ['пост-советская безнадёга', 'bleak post-soviet hopelessness'],
    ['кабаре-театр, гипнотическая мрачность', 'cabaret theatre mood, hypnotic brooding'],
    ['празднично, вечеринка', 'celebratory party'],
    ['кинематографично', 'cinematic'],
    ['клаустрофобический ужас', 'claustrophobic dread'],
    ['космическое удивление', 'cosmic wonder'],
    ['циничная ирония', 'cynical irony'],
    ['атмосфера тёмной сказки', 'dark fairy tale atmosphere'],
    ['мечтательная меланхолия', 'dreamy melancholy'],
    ['жутко, тревожно', 'eerie unsettling'],
    ['эпический размах', 'epic grandeur'],
    ['нежная элегия, плач', 'gentle lament, elegiac'],
    ['надежда, рассвет', 'hopeful sunrise'],
    ['гипнотический транс-повтор', 'hypnotic trance-like repetition'],
    ['интенсивная агрессия', 'intense aggression'],
    ['одинокая полночь', 'lonely midnight'],
    ['медитативная духовная баллада', 'meditative spiritual ballad'],
    ['коварная игривость', 'mischievous playful'],
    ['монотонное безысходное отчаяние, без яркой мелодии, серость', 'monotonous hopeless despair, no bright melody, grey atmosphere'],
    ['нервная паранойя', 'nervous paranoia'],
    ['без цепкой мелодии, мрачная безнадёга', 'no catchy melody, bleak hopelessness'],
    ['без крика', 'no shouting'],
    ['параноидальная клаустрофобия', 'paranoid claustrophobia'],
    ['игривый абсурд', 'playful absurdity'],
    ['бунтарская дерзость', 'rebellious defiance'],
    ['романтическая тоска', 'romantic longing'],
    ['саркастичный деп-пан', 'sarcastic deadpan'],
    ['простая счастливая поп-мелодия, вечеринка', 'simple happy pop melody, party'],
    ['солнечное утро', 'sunny morning'],
    ['уверенная расхлябанность', 'swaggering confidence'],
    ['нежная колыбельная', 'tender lullaby'],
    ['триумфальная эпика', 'triumphant epic'],
    ['усталая покорность', 'weary resignation'],
    ['грустное странничество', 'wistful wanderlust'],
  ],
  production: [
    ['домашняя запись 1987, лоу-фай муть', '1987 home tape recording, lo-fi murk'],
    ['сибирский магнитиздат 1988, ленту в хрипе и промышленном хаосе', '1988 Siberian underground home-tape recording, lo-fi tape murk with noisy industrial chaos'],
    ['AM-радио компрессия', 'AM radio compression'],
    ['взорванная кассетная муть, вобл и шипение ленты', 'blown-out cassette murk, tape wobble and hiss'],
    ['яркий современный поп-лоск', 'bright modern pop sheen'],
    ['соборный реверб, огромное пространство', 'cathedral reverb, huge space'],
    ['чистая современная студия', 'clean modern studio'],
    ['сжатый радио-звук', 'compressed radio sound'],
    ['хрустящий телефонный EQ', 'crunchy telephone EQ'],
    ['плотная стена звука', 'dense wall of sound'],
    ['сухая близкая ясность', 'dry close-miked clarity'],
    ['даб-микшение, эхо и выпадения', 'dub mixing, echoes and dropouts'],
    ['гараж-репетиционка', 'garage rehearsal room ambience'],
    ['зернистое аналоговое тепло', 'gritty analog warmth'],
    ['лоу-фай кассетная запись', 'lo-fi cassette recording'],
    ['лоу-фай домашняя муть', 'lo-fi home-tape murk'],
    ['лоу-фай запись с телефона', 'lo-fi phone recording'],
    ['вой громкости, кирпич', 'loudness war brickwall'],
    ['глухо, как под одеялами', 'muffled under blankets'],
    ['узкое моно', 'narrow mono'],
    ['полированный радио-микс', 'polished radio-ready mix'],
    ['просторный широкий стерео', 'spacious wide stereo'],
    ['ленточное насыщение, холодный сырой подвал', 'tape saturation, cold damp basement recording'],
    ['запись 1930-х', 'vintage 1930s recording'],
    ['винтажная аналоговая запись', 'vintage analog recording'],
    ['гараж 1960-х', '1960s garage recording, vintage analog'],
    ['сырой гранж 1991', '1991 raw grunge production'],
    ['аналоговое регги 1977', '1977 analog reggae production'],
    ['тёплый винил', 'warm vinyl character'],
  ],
}
// ru → en словарь по всем полям (для подстановки при отправке)
const slotDict = {}
for (const opts of Object.values(slotOptions)) {
  for (const [ru, en] of opts) slotDict[ru.trim().toLowerCase()] = en
}
// в datalist — по-русски, по алфавиту
const slotHints = {}
for (const [k, opts] of Object.entries(slotOptions)) {
  slotHints[k] = [...opts].sort((a, b) => a[0].localeCompare(b[0], 'ru'))
}

const compiledStyle = computed(() => {
  const base = styleOverride.value.trim()
  if (base) return base
  const parts = [
    slots.value.language, slots.value.genre, slots.value.rhythm,
    slots.value.guitars, slots.value.keys, slots.value.vocals,
    slots.value.mood, slots.value.production,
  ].map(s => (s || '').trim()).filter(Boolean)
  const rack = rackCompile(rackSel.value)
  if (rack) parts.push(rack)
  if (slots.value.bpm) parts.push(slots.value.bpm + ' BPM')
  return parts.join(', ')
})

function applyPreset(p, mode) {
  title.value = p.name
  lyrics.value = p.lyrics
  seed.value = p.seed
  cot.value = 'full'
  slots.value = {
    language: '', genre: '', rhythm: '', guitars: '', keys: '',
    vocals: '', mood: '', production: '', bpm: p.bpm || null,
  }
  if (mode === 'line') {
    styleOverride.value = p.style   // точная строка калибровки, 1:1
  } else {
    styleOverride.value = ''
    for (const k of Object.keys(p.slots)) slots.value[k] = p.slots[k]
  }
}

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

// частое обновление состояния плеера: кнопки ▶→■ должны переключаться сразу
async function refreshPlayer() {
  try { playerState.value = await api.audioState() } catch {}
}

async function stopAll() {
  await api.stopAudio()
  nowPlayingKey.value = ''
  await refreshPlayer()
}

// копайтер стихов (Ollama на 184)
const copOpen = ref(false)
const copTheme = ref('')
const copUseExample = ref(true)
const copBusy = ref(false)
const copText = ref('')
const copErr = ref('')
const copSeconds = ref(null)

async function copGenerate() {
  if (!copTheme.value.trim()) return
  copBusy.value = true
  copErr.value = ''
  copText.value = ''
  try {
    const r = await api.copilot({
      theme: copTheme.value,
      style: compiledStyle.value,
      example: copUseExample.value ? lyrics.value : '',
      lang: slots.value.language || 'Russian',
    })
    copText.value = r.text
    copSeconds.value = r.seconds
  } catch (e) {
    copErr.value = String(e)
  } finally {
    copBusy.value = false
  }
}

function copInsert() {
  if (copText.value.trim()) lyrics.value = copText.value
  copOpen.value = false
}

async function togglePlay(j) {
  await toggleArtifact(j, `m${j.id}`, `#${j.id} ${j.title}`, () => api.playAudio(j.id))
}

// финальная строка стиля: русские значения из списков подставляются словарём,
// самописная кириллица переводится через Ollama; английское проходит как есть
const CYR = /[а-яё]/i
function dictStyle(s) {
  const t = s.trim()
  return slotDict[t.toLowerCase()] || t
}

async function finalStyle() {
  let s
  if (styleOverride.value.trim()) {
    s = styleOverride.value.trim()
  } else {
    const parts = [
      slots.value.language, slots.value.genre, slots.value.rhythm,
      slots.value.guitars, slots.value.keys, slots.value.vocals,
      slots.value.mood, slots.value.production,
    ].map(v => (v || '').trim()).filter(Boolean).map(dictStyle)
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

// стих к отправке: строки-пометки (начинаются с #) вырезаются — это заметки
// для себя (ударения, произношение), модель их не поёт
function cleanLyrics(text) {
  return text.split('\n').filter(l => !l.trim().startsWith('#')).join('\n')
}

function payload(extra = {}) {
  return {
    title: title.value,
    lyrics: cleanLyrics(lyrics.value),
    seed: seed.value ? Number(seed.value) : 0,
    cot: cot.value,
    ...extra,
  }
}

async function submit() {
  if (!compiledStyle.value || !lyrics.value.trim()) return
  submitting.value = true
  try {
    await api.submit({ ...payload(), style: await finalStyle() })
    await refresh()
  } finally { submitting.value = false }
}

async function submitFan(n) {
  if (!compiledStyle.value || !lyrics.value.trim()) return
  submitting.value = true
  try {
    await api.submitFan({ ...payload(), style: await finalStyle() }, n)
    await refresh()
  } finally { submitting.value = false }
}

async function makePlan() {
  if (!compiledStyle.value || !lyrics.value.trim()) return
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
    planAbc.value = r.abc
    planInfo.value = { seed: usedSeed, seconds: r.seconds, truncated: r.truncated }
  } catch (e) {
    planErr.value = String(e)
  } finally { planBusy.value = false }
}

async function renderFromAbc() {
  if (!planAbc.value.trim()) return
  submitting.value = true
  try {
    await api.submit({
      ...payload({
        abc: planAbc.value,
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
    planAbc.value = await r.text()
    planInfo.value = { seed: j.seed, seconds: null, truncated: false, fromJob: j.id }
    planOpen.value = true
  } catch (e) {
    planErr.value = String(e)
    planOpen.value = true
  }
}

function reuseJob(j) {
  title.value = j.title
  styleOverride.value = j.style
  lyrics.value = j.lyrics
  seed.value = j.seed || null
  cot.value = ['full', 'melody', 'off'].includes(j.cot) ? j.cot : 'full'
}

// ---------- Remix: трек → ABC (SheetSage2 на воркере) ----------

async function transcribeFromTrack() {
  planErr.value = ''
  planBusy.value = true
  planOpen.value = true
  try {
    const r = await api.transcribeFile()
    if (!r) { planOpen.value = false; return }   // диалог отменён
    planAbc.value = r.abc
    planInfo.value = { seed: null, seconds: r.seconds, truncated: false, fromTrack: true }
  } catch (e) {
    planErr.value = String(e)
  } finally { planBusy.value = false }
}

// ---------- Пиано-ролл ----------

async function toggleRoll(j) {
  const open = !rollOpen.value[j.id]
  rollOpen.value = { ...rollOpen.value, [j.id]: open }
  if (open && !rollData.value[j.id]) await openRoll(j)
}

async function openRoll(j) {
  rollBusy.value = { ...rollBusy.value, [j.id]: true }
  rollErr.value = { ...rollErr.value, [j.id]: '' }
  try {
    rollData.value = { ...rollData.value, [j.id]: await api.jobScore(j.id) }
  } catch (e) {
    rollErr.value = { ...rollErr.value, [j.id]: String(e) }
  } finally { rollBusy.value = { ...rollBusy.value, [j.id]: false } }
}

function barDensity(bar, voice) {
  const notes = (bar.voices && bar.voices[voice]) || 0
  const rests = (bar.rests && bar.rests[voice]) || 0
  if (!notes) return 0
  const ratio = notes / Math.max(1, notes + rests)
  return ratio > 0.66 ? 3 : ratio > 0.33 ? 2 : 1   // ▓ ▒ ░
}

function barSelStart(j, idx) {
  rollDrag = true
  rollSel.value = { ...rollSel.value, [j.id]: { a: idx, b: idx } }
}

function barSelOver(j, idx) {
  if (!rollDrag) return
  const s = rollSel.value[j.id]
  if (s && s.b !== idx) rollSel.value = { ...rollSel.value, [j.id]: { ...s, b: idx } }
}

function barSelEnd() { rollDrag = false }

function isBarSel(j, idx) {
  const s = rollSel.value[j.id]
  return !!s && idx >= Math.min(s.a, s.b) && idx <= Math.max(s.a, s.b)
}

function selRange(j) {
  const s = rollSel.value[j.id]
  if (!s) return null
  const bars = rollData.value[j.id] && rollData.value[j.id].bars
  if (!bars || !bars.length) return null
  const a = bars[Math.min(s.a, s.b)], b = bars[Math.max(s.a, s.b)]
  const to = Math.min(b.end_sec, j.duration_sec || b.end_sec)   // звук короче расклада ABC
  const from = Math.min(a.start_sec, Math.max(0, to - 1))
  return { from, to }
}

async function makePreview(j) {
  const r = selRange(j)
  if (!r || r.to - r.from < 1) return
  previewBusy.value = { ...previewBusy.value, [j.id]: true }
  try {
    const p = await api.jobPreview(j.id, r.from, r.to)
    await toggleArtifact(j, `p${j.id}:${p.file}`,
      `превью ${r.from.toFixed(0)}–${r.to.toFixed(0)} с · #${j.id}`,
      () => api.playFile(j.id, p.file, p.duration_sec))
  } catch (e) {
    rollErr.value = { ...rollErr.value, [j.id]: String(e) }
  } finally { previewBusy.value = { ...previewBusy.value, [j.id]: false } }
}

// ---------- Овердаб ----------

function toggleOverdub(j) {
  odOpen.value = { ...odOpen.value, [j.id]: !odOpen.value[j.id] }
  if (odStyle.value[j.id] === undefined) {
    odStyle.value = { ...odStyle.value, [j.id]: 'sparse flute melody, airy' }
    odGain.value = { ...odGain.value, [j.id]: 0.5 }
  }
}

async function submitOverdub(j) {
  if (!odStyle.value[j.id] || !odStyle.value[j.id].trim()) return
  odBusy.value = { ...odBusy.value, [j.id]: true }
  try {
    await api.submitOverdub(j.id, odStyle.value[j.id], odGain.value[j.id])
    odOpen.value = { ...odOpen.value, [j.id]: false }
    refresh()
  } catch (e) {
    rollErr.value = { ...rollErr.value, [j.id]: String(e) }
  } finally { odBusy.value = { ...odBusy.value, [j.id]: false } }
}

// ---------- Стемы ----------

async function toggleStems(j) {
  const open = !stemsOpen.value[j.id]
  stemsOpen.value = { ...stemsOpen.value, [j.id]: open }
  if (!open) return
  if (!stemsList.value[j.id]) {
    stemsBusy.value = { ...stemsBusy.value, [j.id]: true }
    try {
      let list = await api.jobStems(j.id)
      if (!list || !list.length) {
        await api.makeStems(j.id)
        list = await api.jobStems(j.id)
      }
      stemsList.value = { ...stemsList.value, [j.id]: list || [] }
    } catch (e) {
      rollErr.value = { ...rollErr.value, [j.id]: String(e) }
    } finally { stemsBusy.value = { ...stemsBusy.value, [j.id]: false } }
  }
}

function playStem(j, s) {
  toggleArtifact(j, `s${j.id}:${s.file}`, `${s.name} · #${j.id}`,
    () => api.playFile(j.id, s.file, j.duration_sec))
}

async function corpusCreate() {
  if (!corpusName.value.trim()) return
  corpusBusy.value = true
  corpusErr.value = ''
  try {
    await api.corpusCreate(corpusName.value.trim())
    corpusName.value = ''
    await loadCorpora()
  } catch (e) { corpusErr.value = String(e) } finally { corpusBusy.value = false }
}

async function corpusAddTracks(c) {
  corpusBusy.value = true
  corpusErr.value = ''
  try {
    await api.corpusAddTracks(c.id)
    delete corpusTracks.value[c.id]
    corpusTracks.value = { ...corpusTracks.value }
    await loadCorpora()
  } catch (e) { corpusErr.value = String(e) } finally { corpusBusy.value = false }
}

async function corpusBuild(c) {
  corpusBusy.value = true
  corpusErr.value = ''
  try {
    const p = await api.corpusBuild(c.id)
    corpusProfile.value = { ...corpusProfile.value, [c.id]: p }
    await loadCorpora()
  } catch (e) { corpusErr.value = String(e) } finally { corpusBusy.value = false }
}

async function corpusShow(c) {
  if (!corpusProfile.value[c.id]) {
    try { corpusProfile.value = { ...corpusProfile.value, [c.id]: await api.corpusGet(c.id) } } catch (e) {
      corpusErr.value = String(e); return
    }
  }
}

function applyProfileStyle(p) {
  if (p && p.style) styleOverride.value = p.style
}

function applyProfileAbc(p) {
  if (!p || !p.abc_template) return
  planAbc.value = p.abc_template
  planInfo.value = { seed: null, seconds: null, truncated: false, fromProfile: true }
  planErr.value = ''
  planOpen.value = true
}

async function cancel(id) {
  await api.cancel(id)
  refresh()
}

function audioSrc(j) {
  const f = j.mp3_file || j.audio_file
  return f ? `/audio/${j.id}/${f}` : ''
}

function openExternal(j) {
  const f = j.mp3_file || j.audio_file
  if (f) api.openExternal(j.id, f)
}

function openListen(j) {
  api.openURL(`${api.getServerURLSync()}/listen/${j.id}`)
}

async function saveAudio(j, file) {
  const f = file || j.audio_file
  if (f) await api.saveAudio(j.id, f)
}

async function saveServerURL() {
  await api.setServerURL(serverURL.value.trim())
  serverURLEdit.value = false
  refresh()
}

async function stopPlaying() {
  await api.stopAudio()
  nowPlayingKey.value = ''
  setTimeout(refresh, 200)
}

function fmtDur(s) {
  if (!s) return ''
  const m = Math.floor(s / 60)
  return `${m}:${String(Math.round(s % 60)).padStart(2, '0')}`
}

const statusLabel = { queued: 'в очереди', running: 'генерируется', done: 'готово', error: 'ошибка', canceled: 'отменено' }

onMounted(async () => {
  serverURL.value = await api.getServerURL()
  refresh()
  loadCorpora()
  timer = setInterval(refresh, 3000)
  window.addEventListener('mouseup', onWindowMouseup)
  setInterval(refreshPlayer, 1000)
})
onUnmounted(() => { clearInterval(timer); window.removeEventListener('mouseup', onWindowMouseup) })
</script>

<template>
  <header>
    <h1>Yue Studio</h1>
    <span class="health" :class="health ? 'up' : 'down'">
      {{ health ? 'сервер: готов' + (health.model_loaded ? ' (модель в памяти)' : '') : 'сервер недоступен' }}
    </span>
    <div v-if="playerState.playing || playerState.job_id" class="playerbar">
      <button class="ghost" @click="api.toggleAudio()">{{ playerState.playing ? '⏸' : '▶' }}</button>
      <span class="now" :title="playerState.error">{{ nowPlaying || ('#' + playerState.job_id) }}</span>
      <span class="pos muted">{{ fmtDur(playerState.position_sec) }}<template v-if="playerState.duration_sec"> / {{ fmtDur(playerState.duration_sec) }}</template></span>
      <button class="ghost" @click="stopPlaying()">■</button>
      <span v-if="playerState.error" class="error" :title="playerState.error">звук: ошибка</span>
    </div>
    <span class="spacer"></span>
    <input v-if="serverURLEdit" v-model="serverURL" class="server-input" @keyup.enter="saveServerURL" />
    <a v-else class="server" @click="serverURLEdit = true" title="Сменить сервер">{{ serverURL }}</a>
  </header>

  <main>
    <div class="left-col">
      <section class="panel lib">
        <h2>Библиотека стилей <span class="muted">({{ visiblePresets.length }})</span></h2>
        <div class="chips">
          <div v-for="p in visiblePresets" :key="p.id" class="chip">
            <button class="chip-main" :title="p.style" @click="applyPreset(p, 'slots')">{{ p.name }}</button>
            <button class="chip-alt" title="Применить точной строкой калибровки (1:1)" @click="applyPreset(p, 'line')">1:1</button>
            <button class="chip-alt del" title="Убрать пресет из библиотеки" @click="hidePreset(p)">✕</button>
          </div>
        </div>
        <p v-if="hiddenPresets.length" class="muted" style="margin:8px 0 0">
          скрыто: {{ hiddenPresets.length }} · <a @click="restorePresets" style="cursor:pointer">восстановить все</a>
        </p>
      </section>

      <section class="panel lib">
        <h2>Профили из корпуса <span class="muted">(3–10 треков исполнителя)</span></h2>
        <div class="corpus-new">
          <input v-model="corpusName" placeholder="Имя профиля (напр. «Летов 89–91»)" @keyup.enter="corpusCreate" />
          <button class="ghost" :disabled="corpusBusy || !corpusName.trim()" @click="corpusCreate">создать</button>
        </div>
        <p v-if="corpusErr" class="error">{{ corpusErr }}</p>
        <div v-for="c in corpora" :key="c.id" class="corpus-item">
          <strong>{{ c.name }}</strong>
          <span class="muted">{{ c.tracks }} трек(ов)</span>
          <span class="spacer"></span>
          <button class="ghost small-btn" :disabled="corpusBusy" @click="corpusAddTracks(c)">＋ треки…</button>
          <button class="ghost small-btn" :disabled="corpusBusy || c.tracks < 3" @click="corpusBuild(c)">
            {{ corpusBusy ? '…' : 'собрать профиль' }}
          </button>
          <button v-if="c.has_profile" class="ghost small-btn" @click="corpusShow(c)">профиль</button>
          <div class="corpus-tracks">
            <div v-for="t in corpusTracks[c.id] || []" :key="t.filename" class="corpus-track">
              <strong>{{ t.filename }}</strong>
              <span class="muted">
                {{ t.tempo_bpm ? t.tempo_bpm.toFixed(0) + ' BPM' : '—' }} ·
                {{ t.key || 'тональность —' }} ·
                {{ (t.top_chords || []).join(' ') || 'аккорды —' }}
              </span>
              <span v-if="t.structure && t.structure.length" class="muted">{{ t.structure.join(' → ') }}</span>
              <p v-if="t.lyrics_head" class="muted track-lyrics">«{{ t.lyrics_head }}…»</p>
              <p v-if="t.abc_error" class="error">транскрипция не удалась: {{ t.abc_error }}</p>
            </div>
            <p v-if="c.tracks < 3" class="muted">Для сбора профиля нужно ещё {{ 3 - c.tracks }} трек(а) этого исполнителя.</p>
          </div>
          <div v-if="corpusProfile[c.id]" class="corpus-profile">
            <p class="muted">
              темп {{ corpusProfile[c.id].tempo_median }} BPM ({{ (corpusProfile[c.id].tempo_range || []).join('–') }}) ·
              тональности {{ Object.keys(corpusProfile[c.id].keys || {}).slice(0, 3).join(', ') }}
            </p>
            <p class="muted style">{{ corpusProfile[c.id].style }}</p>
            <div class="corpus-actions">
              <button class="primary small" @click="applyProfileStyle(corpusProfile[c.id])">стиль → в форму</button>
              <button class="ghost small-btn" @click="applyProfileAbc(corpusProfile[c.id])">ABC → в редактор плана</button>
            </div>
          </div>
        </div>
        <p v-if="!corpora.length" class="muted">Профиль: тональности/прогрессии/темп/структура (SheetSage2), DSP-паспорт, тексты (Whisper), строка стиля (Ollama). Стены характера нет: тембр/голос не переносится.</p>
      </section>

      <section class="panel form">
        <h2>Новая композиция</h2>
        <input v-model="title" placeholder="Название" />

        <div class="slots">
          <label v-for="[key, label, hint] in slotMeta" :key="key">
            <span>{{ label }}</span>
            <input v-model="slots[key]" :placeholder="hint" :list="'dl-' + key" />
            <datalist :id="'dl-' + key">
              <option v-for="[ru] in slotHints[key]" :key="ru" :value="ru" />
            </datalist>
          </label>
          <label>
            <span>BPM</span>
            <input v-model.number="slots.bpm" type="number" min="40" max="250" placeholder="—" />
          </label>
        </div>

        <details>
          <summary>Стиль одной строкой (переопределяет слоты)</summary>
          <textarea v-model="styleOverride" rows="3" placeholder="Russian, 1980s Siberian underground rock, ..."></textarea>
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
                <select v-if="rackSel.some(r => r.id === it.id)"
                        :value="rackSel.find(r => r.id === it.id).effect"
                        @change="rackSetEffect(it.id, $event.target.value)">
                  <option v-for="e in rackEffects" :key="e.id" :value="e.id">{{ e.name }}</option>
                </select>
              </div>
            </div>
          </div>
        </details>

        <div class="lyrics-head">
          <label class="lyrics-label">Стих (с тегами [Verse] / [Chorus] / [Outro]; строки с # — пометки, не поются)</label>
          <button class="ghost small-btn" @click="copOpen = true">✎ копайтер…</button>
        </div>
        <textarea v-model="lyrics" rows="10"></textarea>

        <div class="row">
          <label class="seed">seed <input v-model.number="seed" type="number" placeholder="случайный" /></label>
          <label class="cot">cot
            <select v-model="cot">
              <option value="full">full</option>
              <option value="melody">melody</option>
              <option value="off">off</option>
            </select>
          </label>
          <label class="autotr" title="Слоты можно писать по-русски: перед отправкой строка стиля переводится в английский через Ollama (qwen2.5 на 184). Модель обучена на английских тегах.">
            <input type="checkbox" v-model="autoTranslate" /> рус → eng
          </label>
          <span class="compiled" :title="compiledStyle">→ {{ translateBusy ? 'перевожу…' : (compiledStyle || 'заполни слоты или строку стиля') }}</span>
        </div>

        <div class="actions">
          <button class="primary" :disabled="submitting || !compiledStyle || !lyrics.trim()" @click="submit">
            {{ submitting ? '...' : 'В очередь' }}
          </button>
          <button class="primary alt" :disabled="submitting || !compiledStyle || !lyrics.trim()" @click="submitFan(5)" title="5 джоб с сидами base+0..4 (best-of-N)">
            ×5 сидов
          </button>
          <button class="ghost" :disabled="planBusy || !compiledStyle || !lyrics.trim()" @click="makePlan">
            {{ planBusy ? 'план…' : 'План (ABC)' }}
          </button>
        </div>
      </section>
    </div>

    <section class="panel list">
      <h2>Очередь и результаты</h2>
      <p v-if="!jobs.length" class="muted">Пока пусто.</p>
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
          <button class="ghost" @click="toggleDsp(j)">DSP {{ dspOpen[j.id] ? '▴' : '▾' }}</button>
          <button v-if="j.abc_file" class="ghost" @click="toggleRoll(j)">ролл {{ rollOpen[j.id] ? '▴' : '▾' }}</button>
          <button class="ghost" @click="toggleOverdub(j)">овердаб</button>
          <button class="ghost" @click="toggleStems(j)">стемы {{ stemsOpen[j.id] ? '▴' : '▾' }}</button>
          <button class="ghost" @click="saveAudio(j)">сохранить flac</button>
          <button v-if="j.abc_file" class="ghost" @click="loadJobAbc(j)" title="Открыть партитуру в редакторе плана">ABC →</button>
          <button v-if="j.abc_file" class="ghost icon" title="Сохранить партитуру (score.abc) в файл" @click="saveAudio(j, j.abc_file)">⤓ abc</button>
        </div>

        <div v-if="rollOpen[j.id] && j.status === 'done'" class="roll-block" @mouseup="barSelEnd()" @mouseleave="barSelEnd()">
          <p v-if="rollBusy[j.id]" class="muted">Разбираю партитуру…</p>
          <p v-if="rollErr[j.id]" class="error">{{ rollErr[j.id] }}</p>
          <template v-if="rollData[j.id]">
            <p class="muted roll-meta">
              {{ rollData[j.id].tempo_bpm }} BPM · {{ rollData[j.id].key }} · {{ rollData[j.id].meter }} ·
              {{ rollData[j.id].bars.length }} тактов · ~{{ fmtDur(rollData[j.id].duration_sec) }}
              <template v-if="selRange(j)"> · выделено {{ selRange(j).from.toFixed(0) }}–{{ selRange(j).to.toFixed(0) }} с</template>
            </p>
            <div v-for="(chunk, ci) in rollChunks(j)" :key="ci" class="roll-grid"
                 :style="{ gridTemplateColumns: `70px repeat(${chunk.length}, minmax(16px, 1fr))` }">
              <div></div>
              <div v-for="b in chunk" :key="'s' + b.idx" class="roll-sec" :title="b.section">{{ b.section.slice(0, 3) }}</div>
              <template v-for="v in rollData[j.id].voice_order" :key="v">
                <div class="roll-voice">{{ v }}</div>
                <div v-for="b in chunk" :key="v + b.idx"
                     class="roll-cell" :class="['d' + barDensity(b, v), { sel: isBarSel(j, b.idx), off: b.start_sec >= j.duration_sec }]"
                     :title="`${b.section} · такт ${b.idx + 1} · ${b.start_sec.toFixed(1)}–${b.end_sec.toFixed(1)}s${b.start_sec >= j.duration_sec ? ' · за пределами звука' : ''}`"
                     @mousedown.prevent="barSelStart(j, b.idx)" @mouseover="barSelOver(j, b.idx)"></div>
              </template>
              <div class="roll-voice">аккорды</div>
              <div v-for="b in chunk" :key="'c' + b.idx" class="roll-chord">{{ (b.chords[0] || '') }}</div>
            </div>
            <div class="roll-actions">
              <button class="primary small" :disabled="!selRange(j) || previewBusy[j.id]" @click="makePreview(j)">
                {{ previewBusy[j.id] ? 'декодирую…' : '▶ превью фрагмента' }}
              </button>
              <span class="muted">выдели такты мышью; превью — VAE-decode куска латентов (секунды), без AR-генерации</span>
            </div>
          </template>
        </div>

        <div v-if="odOpen[j.id] && j.status === 'done'" class="od-block">
          <p class="muted">Партия поверх трека: рендер по партитуре джобы с новым стилем, затем микс с оригиналом. Это не настоящий овердаб — YuE2 не даёт стемов.</p>
          <div class="od-row">
            <input v-model="odStyle[j.id]" placeholder="стиль партии: sparse flute melody, airy" class="od-style" />
            <label class="od-gain">гейн <input type="range" min="0.1" max="1" step="0.05" v-model.number="odGain[j.id]" /> {{ odGain[j.id] }}</label>
            <button class="primary small" :disabled="odBusy[j.id] || !odStyle[j.id] || !odStyle[j.id].trim()" @click="submitOverdub(j)">
              {{ odBusy[j.id] ? '…' : 'сгенерировать' }}
            </button>
          </div>
          <p v-if="rollErr[j.id]" class="error">{{ rollErr[j.id] }}</p>
        </div>

        <div v-if="stemsOpen[j.id] && j.status === 'done'" class="stems-block">
          <p v-if="stemsBusy[j.id]" class="muted">Разделяю (demucs)…</p>
          <div v-for="s in stemsList[j.id] || []" :key="s.file" class="stem-row">
            <button class="ghost play-mini" :class="{ stop: isPlaying('s' + j.id + ':' + s.file) }"
                    :disabled="playBusy['s' + j.id + ':' + s.file]" @click="playStem(j, s)">
              {{ playBtn('s' + j.id + ':' + s.file) }}
            </button>
            <button class="ghost play-mini stopbtn" title="Остановить всё" @click="stopAll()">■</button>
            <strong>{{ s.name }}</strong>
            <button class="ghost small-btn" title="Сохранить стем flac" @click="saveAudio(j, s.file)">⤓</button>
          </div>
        </div>

        <div v-if="dspOpen[j.id] && j.status === 'done'" class="dsp-block">
          <p v-if="metricsErr" class="error">{{ metricsErr }}</p>
          <div class="dsp-row">
            <select :value="dspSel[j.id] || ''" @change="selChain(j, $event.target.value)">
              <option value="">цепочка эффектов…</option>
              <option v-for="c in dspChains" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
            <button class="primary small" :disabled="!dspSel[j.id] || dspBusy[j.id]" @click="applyDsp(j)">
              {{ dspBusy[j.id] ? 'гоню…' : 'применить' }}
            </button>
            <button class="ghost" @click="openMetrics(j)">метрики трека</button>
          </div>
          <p v-if="curChain(j)" class="muted dsp-note">{{ curChain(j).note }}</p>
          <div v-if="curChain(j)" class="dsp-params">
            <label v-for="p in curChain(j).params" :key="p.id">
              <span class="dsp-plabel">{{ p.label }}</span>
              <input type="range" :min="p.min" :max="p.max" :step="p.step"
                     v-model.number="dspParams[j.id][p.id]" :disabled="dspBusy[j.id]" />
              <span class="dsp-pval">{{ dspParams[j.id][p.id] }}</span>
            </label>
          </div>
          <div v-for="v in dspVariants[j.id] || []" :key="v.file" class="dsp-variant">
            <button class="ghost play-mini" :class="{ stop: isPlaying('v' + j.id + ':' + v.file) }"
                    :disabled="playBusy['v' + j.id + ':' + v.file]" @click="playVariant(j, v)">
              {{ playBtn('v' + j.id + ':' + v.file) }}
            </button>
            <strong>{{ chainLabel(v.file) }}</strong>
            <span v-if="v.metrics" class="muted deltas">
              Δ крест {{ vDelta(j, v, 'crest_db') }} dB · Δ дин {{ vDelta(j, v, 'dyn_range_db') }} dB ·
              Δ верх {{ vDelta(j, v, 'bands.high') }}% · Δ флэтнес {{ vDelta(j, v, 'flatness_median', 3) }}
            </span>
            <span v-else class="muted">без метрик</span>
            <button class="ghost" @click="openVariantMetrics(j, v)">полная дельта</button>
          </div>
        </div>
      </article>
    </section>
  </main>

  <div v-if="planOpen" class="modal-backdrop" @click.self="planOpen = false">
    <div class="modal">
      <div class="modal-head">
        <h2>План — ABC</h2>
        <span v-if="planInfo" class="muted plan-meta">
          seed {{ planInfo.seed }}<template v-if="planInfo.seconds"> · {{ planInfo.seconds }}s</template><template v-if="planInfo.fromJob"> · из джобы #{{ planInfo.fromJob }}</template><template v-if="planInfo.truncated"> · обрезан лимитом токенов</template>
        </span>
        <span class="spacer"></span>
        <button class="ghost" @click="planOpen = false">✕</button>
      </div>
      <p v-if="planBusy" class="muted">Планирование на GPU… первый запуск грузит модель, может занять пару минут.</p>
      <p v-if="planErr" class="error">{{ planErr }}</p>
      <textarea v-model="planAbc" rows="18" class="abc" spellcheck="false"></textarea>
      <div class="modal-actions">
        <button class="primary" :disabled="submitting || !planAbc.trim()" @click="renderFromAbc">Рендер по этому ABC</button>
        <button class="ghost" :disabled="planBusy" @click="makePlan">Новый план</button>
        <button class="ghost" :disabled="planBusy" @click="transcribeFromTrack" title="Транскрипция вашего трека (SheetSage2) — кавер по чужой или своей мелодии">Из трека…</button>
      </div>
    </div>
  </div>

  <div v-if="copOpen" class="modal-backdrop" @click.self="copOpen = false">
    <div class="modal cop-modal">
      <div class="modal-head">
        <h2>Копайтер стихов</h2>
        <span class="muted plan-meta">qwen2.5 · 184</span>
        <span class="spacer"></span>
        <button class="ghost" @click="copOpen = false">✕</button>
      </div>
      <div class="cop-form">
        <input v-model="copTheme" placeholder="Тема: о чём песня" @keyup.enter="copGenerate" />
        <label class="cop-example">
          <input type="checkbox" v-model="copUseExample" />
          подражать манере текущего стиха
        </label>
        <button class="primary" :disabled="copBusy || !copTheme.trim()" @click="copGenerate">
          {{ copBusy ? 'пишет… (грузит модель, до минуты)' : 'Написать' }}
        </button>
      </div>
      <p v-if="copErr" class="error">{{ copErr }}</p>
      <p v-if="copSeconds" class="muted">написано за {{ copSeconds }}s — можно править перед вставкой</p>
      <textarea v-if="copText" v-model="copText" rows="14" spellcheck="true"></textarea>
      <div class="modal-actions">
        <button class="primary" :disabled="!copText.trim()" @click="copInsert">Вставить в редактор</button>
        <button class="ghost" :disabled="copBusy" @click="copGenerate">Ещё вариант</button>
      </div>
    </div>
  </div>

  <div v-if="confirmOpen" class="modal-backdrop" @click.self="confirmOpen = false">
    <div class="modal confirm-modal">
      <div class="modal-head">
        <h2>{{ confirmTitle }}</h2>
        <span class="spacer"></span>
        <button class="ghost" @click="confirmOpen = false">✕</button>
      </div>
      <p class="muted">{{ confirmBody }}</p>
      <div class="modal-actions">
        <button class="primary danger" @click="doConfirm">Да, точно</button>
        <button class="ghost" @click="confirmOpen = false">Отмена</button>
      </div>
    </div>
  </div>

  <div v-if="metricsOpen" class="modal-backdrop" @click.self="metricsOpen = false">
    <div class="modal metrics-modal">
      <div class="modal-head">
        <h2>DSP-метрики — {{ metricsTitle || ('#' + (metricsJob && metricsJob.id) + ' ' + (metricsJob && metricsJob.title)) }}</h2>
        <span class="spacer"></span>
        <button class="ghost" @click="metricsOpen = false">✕</button>
      </div>
      <p v-if="metricsBusy" class="muted">Замер (librosa)…</p>
      <p v-if="metricsErr" class="error">{{ metricsErr }}</p>
      <template v-if="metrics">
        <div class="cmp-row">
          <span class="muted">дельта до:</span>
          <select v-model="cmpTarget" @change="onCmpChange">
            <option value="">— нет —</option>
            <optgroup label="Референсы">
              <option v-for="r in refs" :key="r.id" :value="'ref:' + r.id">{{ refName(r.id) }}</option>
            </optgroup>
            <optgroup label="Джобы">
              <option v-for="jj in doneJobs" :key="jj.id" :value="'job:' + jj.id">#{{ jj.id }} {{ jj.title }}</option>
            </optgroup>
          </select>
          <button class="ghost" :disabled="addingRef" @click="addReference">＋ файл-референс…</button>
        </div>
        <p v-if="cmpBusy" class="muted">Считаю…</p>
        <div class="metrics-table">
          <div class="mrow head"><span>метрика</span><span>трек</span><span>сравнение</span><span>Δ</span></div>
          <div v-for="[key, label, unit, dec] in metricRows" :key="key" class="mrow">
            <span>{{ label }}</span>
            <span>{{ fmtMetric(metricVal(metrics, key), dec, unit) }}</span>
            <span class="muted">{{ cmpMetrics ? fmtMetric(metricVal(cmpMetrics, key), dec, unit) : '—' }}</span>
            <span class="delta">{{ deltaStr(key, dec) }}</span>
          </div>
          <div class="mrow">
            <span>Тональность (хрома)</span>
            <span>{{ metrics.key || '—' }}</span>
            <span class="muted">{{ (cmpMetrics && cmpMetrics.key) || '—' }}</span>
            <span></span>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
header { display: flex; align-items: baseline; gap: 16px; padding: 18px 24px; border-bottom: 1px solid var(--border); }
h1 { font-size: 20px; margin: 0; letter-spacing: .5px; }
h2 { font-size: 15px; margin: 0 0 12px; color: var(--muted); text-transform: uppercase; letter-spacing: 1px; }
.health { font-size: 12px; color: var(--muted); }
.health.up { color: var(--ok); }
.health.down { color: var(--err); }
.playerbar { display: flex; align-items: center; gap: 10px; font-size: 12px; }
.playerbar .now { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.playerbar .pos { font-variant-numeric: tabular-nums; }
.playerbar .ghost { padding: 2px 8px; }
.spacer { flex: 1; }
.server { font-size: 12px; color: var(--muted); cursor: pointer; }
.server-input { font-size: 12px; width: 260px; }

main { display: grid; grid-template-columns: 440px 1fr; gap: 20px; padding: 20px 24px; max-width: 1400px; margin: 0 auto; }
@media (max-width: 1000px) { main { grid-template-columns: 1fr; } }
.left-col { display: flex; flex-direction: column; gap: 20px; }
.panel { background: var(--panel); border: 1px solid var(--border); border-radius: 10px; padding: 16px; }

.lib .chips { display: flex; flex-wrap: wrap; gap: 6px; }
.chip { display: inline-flex; border: 1px solid var(--border); border-radius: 14px; overflow: hidden; }
.chip-main { border: 0; font-size: 12px; padding: 4px 10px; background: transparent; color: inherit; cursor: pointer; white-space: nowrap; }
.chip-main:hover { background: var(--panel2); }
.chip-alt { border: 0; border-left: 1px solid var(--border); font-size: 10px; padding: 4px 7px; background: transparent; color: var(--muted); cursor: pointer; }
.chip-alt:hover { color: inherit; background: var(--panel2); }
.chip-alt.del:hover { color: var(--err); }
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

.row { display: flex; align-items: center; gap: 12px; margin: 10px 0 14px; font-size: 12px; }
.seed input { width: 110px; }
.cot select { font-size: 12px; }
.compiled { color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; }
.autotr { font-size: 12px; color: var(--muted); display: flex; align-items: center; gap: 4px; cursor: pointer; }

.actions { display: flex; gap: 8px; }
.actions .primary { flex: 0 0 auto; }
.primary { background: var(--ok); padding: 6px 16px; }
.primary.alt { background: var(--panel2); border: 1px solid var(--ok); color: var(--ok); }

.job { border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; margin-bottom: 10px; background: var(--panel2); }
.job-head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.status { font-size: 12px; padding: 2px 8px; border-radius: 10px; background: var(--border); }
.status.done { background: rgba(76,175,125,.2); color: var(--ok); }
.status.running { background: rgba(217,160,61,.2); color: var(--run); }
.status.error { background: rgba(224,93,61,.2); color: var(--err); }
.style { font-size: 12px; margin: 6px 0; }
audio { width: 100%; margin-top: 6px; }
.error { color: var(--err); font-size: 13px; }
.job-actions { display: flex; gap: 8px; margin-top: 6px; align-items: center; }
.job-actions button { font-size: 12px; padding: 4px 10px; }
.muted { color: var(--muted); }
.badge { font-size: 11px; padding: 1px 7px; border-radius: 9px; background: rgba(120,140,255,.15); color: #9aa5ff; }
.play-main { background: var(--ok); padding: 6px 16px; min-width: 44px; }
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

.dsp-block { margin-top: 8px; padding: 10px; border: 1px dashed var(--border); border-radius: 8px; display: flex; flex-direction: column; gap: 8px; }
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
.od-block { margin-top: 8px; padding: 10px; border: 1px dashed var(--border); border-radius: 8px; }
.od-block p { margin: 0 0 8px; font-size: 12px; }
.od-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.od-style { flex: 1; min-width: 240px; }
.od-gain { font-size: 12px; color: var(--muted); display: flex; align-items: center; gap: 6px; }

/* стемы */
.stems-block { margin-top: 8px; padding: 10px; border: 1px dashed var(--border); border-radius: 8px; display: flex; flex-direction: column; gap: 4px; }
.stem-row { display: flex; align-items: center; gap: 10px; font-size: 13px; }
</style>
