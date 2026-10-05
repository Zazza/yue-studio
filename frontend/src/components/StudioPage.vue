<script setup>
// Студия трека: пиано-ролл партитуры, минус по стемам, овердаб, DSP-цепочки.
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from '../i18n/index.js'
const { t, locale } = useI18n()
import { api } from '../api.js'
import { usePlayer, fmtDur } from '../composables/usePlayer.js'
import { useConfirm } from '../composables/useConfirm.js'
import { useInserts } from '../composables/useInserts.js'
import { odPartyChips } from '../slotOptions.js'
import { applyTrick, beatSecAt, continuationPlan, pickTargets, planTimeline, vocalCeiling, sectionRequest, sectionWindows, sliceAbc, sliceLeadSec, TRICK_INSTRUMENTS, TRICK_MUTES, trickStyleSuffix } from '../abcEdit.js'
import { useRevoice } from '../composables/useRevoice.js'
import { revoiceSpecKinds, vocalEndsQuiet, voiceSource } from '../vocalParts.js'
import { INSERT_DEFAULT_DB, INSERT_MAX_DB, INSERT_MIN_DB } from '../insertMix.js'
import { insertTitle, insertWindow, mixChildId, mixLabel } from '../insertLabels.js'
import { applyFoundTones } from '../dspTones.js'
import { isFlat, bumpRange } from '../envelope.js'
import { ONE_CLICK_LEVELS, oneClickParams } from '../oneClick.js'
import { chainDefaults, hasGrid, needsStem, voiceTarget } from '../dspVoice.js'
import { previewWindow } from '../fxPreview.js'
import { sectionLabel } from '../sectionNames.js'
import { cursorSec as cursorInterp, gridMarks, posEdges, secToPosRange } from '../waveLogic.js'
import VSelect from '../VSelect.vue'
import WaveView from './WaveView.vue'
import PedalBoard from './PedalBoard.vue'

// стиль импортированного трека — должен совпадать с IMPORT_STYLE в worker/yue_worker.py
const IMPORT_STYLE = '(импорт внешнего трека)'
const props = defineProps({ job: { type: Object, required: true }, autoTranslate: Boolean })
const emit = defineEmits(['close', 'open-metrics'])

const { isPlaying, playBusy, playBtn, toggleArtifact, playerState, nowPlayingKey, refreshPlayer, setPlayRange, rangeUntil } = usePlayer()
const { askConfirm } = useConfirm()
const inserts = useInserts()
const revoice = useRevoice()

const rollData = ref(null)     // parsed score
const rollBusy = ref(false)
const rollErr = ref('')
const rollSel = ref(null)      // {a: barIdx, b: barIdx}
const stemMute = ref({})       // {drums: true, ...} — что выкинуть из минуса
const stemsList = ref([])
const previewBusy = ref(false)
let rollDrag = false

// овердаб: чипы партий + ручной ввод
const odChips = ref(new Set())
const odStyle = ref('')
const odLyrics = ref('')
const odGain = ref(0.5)
const odBusy = ref(false)

// DSP-цепочки (ffmpeg на ПК)
const dspChains = ref([])
const dspSel = ref('')
const dspParams = ref({})
const dspBusy = ref(false)
const dspVariants = ref([])
const allJobs = ref([])
const jobMetrics = ref(null)   // метрики исходника (для инлайн-дельт)

const curChain = computed(() => dspChains.value.find((c) => c.id === dspSel.value) || null)

// волна громкости («как в плеере»): выбор места правки по звуку
const waveFile = ref('')            // '' = основной трек версии
const wavePeaks = ref(null)         // {duration_sec, peaks: [[min,max],...]}
const waveBusy = ref(false)
const waveErr = ref('')
const waveMode = ref('amp')         // 'amp' | 'spectrum'
const waveSnap = ref(true)          // прилипание границ выделения к тактам
const waveSelPrecise = ref(null)    // {from, to} — точные секунды выделения по волне
const waveCursor = ref(0)           // позиция воспроизведения по артефакту волны
const minusReady = ref(false)       // минус.flac уже собран — доступен в селекторе волны
const spectrumUrl = ref('')
const spectrumCache = new Map()     // file → data:URL
let waveSeekBusy = false            // перемотка = ffmpeg-перекодировка на ПК
let waveLastClickMs = 0
let waveRafId = 0
let waveLastPoll = { pos: 0, ts: 0 }

// Позиционная сетка: колонка = музыкальный такт (позиция внутри голоса),
// голоса в одной колонке звучат одновременно. Плоский поток из score.json
// (голоса последовательными блоками) рисовал «тот же момент» в разных
// местах сетки — выделение и метки врали местоположением.
const voiceBarList = computed(() => {
  const res = {}
  for (const b of (rollData.value && rollData.value.bars) || []) {
    const v = voiceOfBar(b)
    ;(res[v] = res[v] || []).push(b)
  }
  return res
})
const posCount = computed(() => Math.max(0, ...Object.values(voiceBarList.value).map((a) => a.length)))

const barAt = (v, pos) => (voiceBarList.value[v] || [])[pos] || null

// границы музыкального момента: min start / max end по голосам в колонке
function posTime(pos) {
  let from = Infinity, to = -Infinity
  for (const v of (rollData.value && rollData.value.voice_order) || []) {
    const b = barAt(v, pos)
    if (!b) continue
    from = Math.min(from, b.start_sec)
    to = Math.max(to, b.end_sec)
  }
  return { from, to }
}

// волна: колонки ролла как [{sec, section}], границы тактов и сетка,
// обрезанная по длине аудио (трек бывает короче плана)
const posTimes = computed(() => Array.from({ length: posCount.value }, (_, i) => posTime(i)))
const waveDuration = computed(() => (wavePeaks.value && wavePeaks.value.duration_sec) || props.job.duration_sec || 0)
const waveEdges = computed(() => posEdges(posTimes.value))
const waveColumns = computed(() => posTimes.value.map((tm, i) => ({ sec: tm.from, section: sectionLabel(posSection(i), locale.value) })))
const waveMarks = computed(() => gridMarks(waveColumns.value, waveDuration.value))

// селектор файла волны: все артефакты джобы (трек, эффекты, вклейки, стемы, минус)
const waveFiles = computed(() => {
  const out = [{ value: '', label: t('studio.wave.file.main') }]
  const seen = new Set([''])
  const add = (file, label) => {
    if (file && !seen.has(file)) { seen.add(file); out.push({ value: file, label: label || file }) }
  }
  for (const v of dspVariants.value) add(v.file, variantLabel(v))
  for (const s of stemsList.value) add(s.file, t('studio.wave.stem', { name: stemLabel(s.name) || s.file }))
  if (minusReady.value) add('minus.flac', t('studio.wave.minus'))
  return out
})

const waveKey = computed(() => `w${props.job.id}:${waveFile.value || 'main'}`)

// цвет волны (амплитуда, выделение, курсор): янтарь/коралл — наши, плюс классика.
// Выбор живёт между запусками; '' = акцент темы не входит в список — янтарь по умолчанию
const waveColors = [
  { id: 'amber', hex: '#ffbe3d' },
  { id: 'coral', hex: '#e05d3d' },
  { id: 'blue', hex: '#4a9ede' },
  { id: 'red', hex: '#e03131' },
  { id: 'green', hex: '#4caf7d' },
]
const waveColor = ref(localStorage.getItem('yue_wave_color') || '#ffbe3d')
watch(waveColor, (v) => localStorage.setItem('yue_wave_color', v))

// ширина такта ролла (масштаб): ролл — одна прокручиваемая строка
const rollCellW = ref(16)
function rollZoom(delta) { rollCellW.value = Math.min(48, Math.max(8, rollCellW.value + delta)) }
const rollPositions = computed(() => Array.from({ length: posCount.value }, (_, i) => i))

// колесо над роллом крутит сам ролл (горизонталь), а не всю страницу;
// на краях прокрутки — отдаем событие форме, чтобы страница листалась
function onRollWheel(e) {
  const el = e.currentTarget
  const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY
  if (!delta) return
  const before = el.scrollLeft
  el.scrollLeft = before + delta
  if (el.scrollLeft !== before) e.preventDefault()
}

// выделение — единый источник для всех кнопок студии: точные секунды волны
// переопределяют тактовую сетку (приёмы остаются по тактам через rollSel),
// протяжка по роллу — наоборот, отбрасывает точность волны
const selRange = computed(() => {
  if (waveSelPrecise.value) {
    const cappedTo = Math.min(waveSelPrecise.value.to, waveDuration.value || waveSelPrecise.value.to)
    return { from: Math.min(waveSelPrecise.value.from, Math.max(0, cappedTo - 1)), to: cappedTo }
  }
  const s = rollSel.value
  if (!s || !posCount.value) return null
  const lo = Math.max(0, Math.min(s.a, s.b)), hi = Math.min(posCount.value - 1, Math.max(s.a, s.b))
  let from = Infinity, to = -Infinity
  for (let p = lo; p <= hi; p++) {
    const t = posTime(p)
    if (!isFinite(t.from)) continue
    from = Math.min(from, t.from)
    to = Math.max(to, t.to)
  }
  if (!isFinite(from)) return null
  const cappedTo = Math.min(to, props.job.duration_sec || to)   // звук короче расклада ABC
  return { from: Math.min(from, Math.max(0, cappedTo - 1)), to: cappedTo }
})

onMounted(async () => {
  waveRafId = requestAnimationFrame(waveCursorLoop)
  loadWave()
  await openRoll()
  try { stemsList.value = (await api.jobStems(props.job.id)) || [] } catch {}
  try { dspChains.value = (await api.dspChains()) || [] } catch {}
  // для подписей вклеек из старых файлов (рендер куска → инструмент)
  try { allJobs.value = (await api.jobs()) || [] } catch { /* без списка — подпись по номеру */ }
  reloadVariants()
  api.workerConfig().then((c) => {
    seedvcOk.value = !!c?.seedvc_available
    whisperOk.value = c?.whisper_available !== false
  }).catch(() => {})
  // восстановить промежуточное состояние студии (переживает перезапуск)
  try {
    const st = JSON.parse(localStorage.getItem('yue_studio_state') || '{}')
    const per = st.jobs && st.jobs[props.job.id] || {}
    if (per.odStyle !== undefined) odStyle.value = per.odStyle
    if (per.odLyrics !== undefined) odLyrics.value = per.odLyrics
    if (per.odGain !== undefined) odGain.value = per.odGain
    if (per.stemMute) stemMute.value = per.stemMute
    if (per.dspSel) dspSel.value = per.dspSel
    if (per.dspParams) dspParams.value = per.dspParams
    if (per.odChips) odChips.value = new Set(per.odChips)
    // trickV=2 — времена меток по per-voice таймлайну; всё, что сохранено
    // до него (раздутая вдвое шкала), молча выбрасываем как неверное
    if (per.trickV === 2) {
      if (per.trickMarks) sentMarks.value = per.trickMarks      // история версий
      if (per.pendingSpecs) pendingSpecs.value = per.pendingSpecs
    }
  } catch {}
})

onUnmounted(() => { cancelAnimationFrame(waveRafId); setPlayRange(null); saveStudioState(); stopFragPoll() })

function saveStudioState() {
  try {
    const key = 'yue_studio_state'
    const st = JSON.parse(localStorage.getItem(key) || '{}')
    st.jobs = st.jobs || {}
    st.jobs[props.job.id] = {
      odStyle: odStyle.value, odLyrics: odLyrics.value, odGain: odGain.value, odChips: [...odChips.value],
      stemMute: stemMute.value,
      dspSel: dspSel.value, dspParams: dspParams.value,
      trickV: 2,
      trickMarks: sentMarks.value,
      pendingSpecs: pendingSpecs.value,
    }
    localStorage.setItem(key, JSON.stringify(st))
  } catch {}
}

// партитура трека: своя копия, чтобы «создать партитуру» включала ролл сразу,
// не дожидаясь обновления списка треков
const abcFile = ref(props.job.abc_file || '')
watch(() => props.job.abc_file, (v) => { if (v) abcFile.value = v })
const transcribeBusy = ref(false)

async function openRoll() {
  // импорт без транскрипции / DSP-вариант: плана нет, /score ответит 404 —
  // это не ошибка, а другой тип трека (подсказка и кнопка в шаблоне)
  if (!abcFile.value) { rollData.value = null; return }
  rollBusy.value = true
  rollErr.value = ''
  try {
    const d = await api.jobScore(props.job.id)
    rollData.value = d
    nextTick(watchSteps)   // разделы шагов появились в DOM — следить за прокруткой
  } catch (e) {
    rollErr.value = String(e)
  } finally { rollBusy.value = false }
}

// повторить создание партитуры (SheetSage2 на воркере, ждёт очередь к GPU)
async function retryTranscribe() {
  transcribeBusy.value = true
  rollErr.value = ''
  try {
    const r = await api.transcribeJob(props.job.id)
    abcFile.value = r?.abc_file || 'score.abc'
    await openRoll()
  } catch (e) {
    rollErr.value = String(e)
  } finally { transcribeBusy.value = false }
}

// ---------- волна громкости ----------

async function loadWave() {
  waveBusy.value = true
  waveErr.value = ''
  try {
    wavePeaks.value = await api.jobPeaks(props.job.id, waveFile.value, 0)
  } catch (e) {
    wavePeaks.value = null
    waveErr.value = String(e)
  } finally { waveBusy.value = false }
}

async function loadSpectrum() {
  const key = waveFile.value || ''
  if (spectrumCache.has(key)) { spectrumUrl.value = spectrumCache.get(key); return }
  waveBusy.value = true
  try {
    const b64 = await api.jobSpectrumPNG(props.job.id, waveFile.value)
    spectrumUrl.value = `data:image/png;base64,${b64}`
    spectrumCache.set(key, spectrumUrl.value)
  } catch (e) {
    spectrumUrl.value = ''
    waveErr.value = String(e)   // чаще всего «ffmpeg not available on worker»
  } finally { waveBusy.value = false }
}

watch(waveFile, () => {
  spectrumUrl.value = spectrumCache.get(waveFile.value || '') || ''
  waveSelPrecise.value = null
  loadWave()
})
watch(waveMode, (m) => { if (m === 'spectrum' && !spectrumUrl.value) loadSpectrum() })

// выделение по волне — единый источник для кнопок: точные секунды в приоритете,
// колонки ролла синхронизируются (приёмы плана остаются тактовыми)
function onWaveSelect(sel) {
  waveSelPrecise.value = sel
  const p = secToPosRange(sel.from, sel.to, posTimes.value)
  if (p) rollSel.value = { a: p.lo, b: p.hi }
}

// клик по волне — слушать с этого места. Перемотка = ffmpeg-перекодировка
// хвоста на ПК: не чаще раза в 300 мс и не параллельно самой себе
async function onWaveSeek(sec) {
  const now = Date.now()
  if (waveSeekBusy || now - waveLastClickMs < 300) return
  waveLastClickMs = now
  waveSeekBusy = true
  try {
    if (nowPlayingKey.value === waveKey.value && playerState.value.job_id === props.job.id) {
      await api.seekAudio(sec)
    } else {
      await api.playFile(props.job.id, waveFile.value || props.job.audio_file || 'audio.flac', waveDuration.value)
      nowPlayingKey.value = waveKey.value
      await api.seekAudio(sec)
    }
    refreshPlayer()
  } catch (e) {
    rollErr.value = String(e)
  } finally { setTimeout(() => { waveSeekBusy = false }, 200) }
}

// курсор: опрос позиции раз в 1 с, между опросами идём вперёд плавно (rAF);
// позиция относительна артефакту — рисуем только файл волны. Прогон
// выделенного куска (верхний ▶) останавливается в его конце
watch(() => playerState.value.position_sec, (p) => { waveLastPoll = { pos: p || 0, ts: Date.now() } })
watch(selRange, (r) => {
  setPlayRange(r && wavePeaks.value ? {
    key: waveKey.value, jobId: props.job.id,
    file: waveFile.value || props.job.audio_file || 'audio.flac',
    dur: waveDuration.value, from: r.from, to: r.to,
  } : null)
})
function waveCursorLoop() {
  waveRafId = requestAnimationFrame(waveCursorLoop)
  if (nowPlayingKey.value === waveKey.value && playerState.value.job_id === props.job.id) {
    waveCursor.value = cursorInterp(waveLastPoll.pos, waveLastPoll.ts,
      playerState.value.playing && !waveSeekBusy, Date.now(), waveDuration.value)
    const ru = rangeUntil.value
    if (ru && ru.key === waveKey.value && playerState.value.playing && waveCursor.value >= ru.to - 0.05) {
      rangeUntil.value = null
      api.stopAudio().then(refreshPlayer).catch(() => {})
    }
  }
}

function barDensity(bar, voice) {
  const notes = (bar.voices && bar.voices[voice]) || 0
  const rests = (bar.rests && bar.rests[voice]) || 0
  if (!notes) return 0
  const ratio = notes / Math.max(1, notes + rests)
  return ratio > 0.66 ? 3 : ratio > 0.33 ? 2 : 1   // ▓ ▒ ░
}

// ячейка позиционной сетки: голос v, такт-позиция pos
const cellDensity = (v, pos) => {
  const b = barAt(v, pos)
  return b ? barDensity(b, v) : 0
}
const cellOff = (v, pos) => {
  const b = barAt(v, pos)
  return !!b && b.start_sec >= props.job.duration_sec
}
const posSection = (pos) => {
  for (const v of (rollData.value && rollData.value.voice_order) || []) {
    const b = barAt(v, pos)
    if (b) return b.section
  }
  return ''
}
// подпись секции в плане: по-русски и только в её начале (раньше «ver ver ver…» в каждом такте)
const secLabel = (pos) => sectionLabel(posSection(pos), locale.value)
const isSecStart = (pos) => pos === 0 || posSection(pos) !== posSection(pos - 1)
// голоса партитуры (Vocal/Ins) — по-русски; неизвестный — как есть
// дорожки demucs по-русски (барабаны, бас, гитары/синты, голос, гитара, клавиши); неизвестная — как есть
const stemLabel = (n) => { if (!n) return ''; const k = 'studio.dsp.target.' + n; const s = t(k); return s === k ? n : s }
// шаги студии: ссылки на разделы, активный — по прокрутке
const STUDIO_STEPS = ['listen', 'edit', 'sound', 'done']
const activeStep = ref('listen')
const fxBox = ref(null)
function goStep(st) {
  const el = document.getElementById('st-' + st)
  if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  activeStep.value = st
}
// активный шаг — последний раздел, чей заголовок поднялся выше 40% экрана; в самом низу
// страницы — последний шаг (до верха он не доедет: страница кончается раньше)
let stepScroller = null
function updateStep() {
  const c = stepScroller
  if (!c) return
  if (c.scrollTop + c.clientHeight >= c.scrollHeight - 4) { activeStep.value = STUDIO_STEPS[STUDIO_STEPS.length - 1]; return }
  let cur = STUDIO_STEPS[0]
  for (const st of STUDIO_STEPS) {
    const el = document.getElementById('st-' + st)
    if (el && el.getBoundingClientRect().top < window.innerHeight * 0.4) cur = st
  }
  activeStep.value = cur
}
function watchSteps() {
  if (stepScroller) return
  const first = document.getElementById('st-' + STUDIO_STEPS[0])
  stepScroller = first && first.closest('.app-body')
  if (stepScroller) stepScroller.addEventListener('scroll', updateStep, { passive: true })
}
onUnmounted(() => { if (stepScroller) stepScroller.removeEventListener('scroll', updateStep) })
// «Готово» → цепочка в «Эффектах звука» (напр. «Громкость альбома»): раскрыть и показать
function goChain(id) {
  selChain(id)
  if (fxBox.value) { fxBox.value.open = true; fxBox.value.scrollIntoView({ behavior: 'smooth', block: 'start' }) }
}
// подсказка правок — по состоянию, одной строкой
const trickHint = computed(() => {
  if (planDraft.value) return t('studio.trick.hint.build')
  if (hasSel.value) return t('studio.trick.hint.pick')
  return t('studio.trick.hint.select')
})
const voiceLabel = (v) => { const k = 'studio.roll.voice.' + v; const s = t(k); return s === k ? v : s }
const posChord = (pos) => {
  for (const v of (rollData.value && rollData.value.voice_order) || []) {
    const b = barAt(v, pos)
    if (b && b.chords && b.chords[0]) return b.chords[0]
  }
  return ''
}
function cellTitle(v, pos) {
  const b = barAt(v, pos)
  if (!b) return `${voiceLabel(v)}: в этом такте у голоса нет своей партии`
  const tail = b.start_sec >= props.job.duration_sec ? ' · за пределами звука' : ''
  const mark = isTrickCell(v, pos) ? ' · ✋ ' + trickTitle(v, pos) : ''
  return `${sectionLabel(b.section, locale.value)} · такт ${pos + 1} · ${b.start_sec.toFixed(1)}–${b.end_sec.toFixed(1)}с${tail}${mark}`
}

function barSelStart(idx) {
  rollDrag = true
  waveSelPrecise.value = null   // выделение теперь по тактам, точность волны не нужна
  rollSel.value = { a: idx, b: idx }
}

function barSelOver(idx) {
  if (!rollDrag) return
  const s = rollSel.value
  if (s && s.b !== idx) rollSel.value = { ...s, b: idx }
}

function barSelEnd() { rollDrag = false }

function isBarSel(idx) {
  const s = rollSel.value
  return !!s && idx >= Math.min(s.a, s.b) && idx <= Math.max(s.a, s.b)
}

function onWindowMouseup() { rollDrag = false }

async function makePreview() {
  const r = selRange.value
  if (!r || r.to - r.from < 1) return
  previewBusy.value = true
  try {
    const p = await api.jobPreview(props.job.id, r.from, r.to)
    await toggleArtifact(`p${props.job.id}:${p.file}`,
      `превью ${r.from.toFixed(0)}–${r.to.toFixed(0)} с · #${props.job.id}`,
      () => api.playFile(props.job.id, p.file, p.duration_sec))
  } catch (e) {
    rollErr.value = String(e)
  } finally { previewBusy.value = false }
}

// убрать голоса из текста ABC: строки V: <имя> и тела этих голосов выкидываются
function dropVoices(abc, voiceNames) {
  const removed = new Set(voiceNames)
  const out = []
  let curVoice = null
  for (const line of abc.split('\n')) {
    const t = line.trim()
    const vm = t.match(/^V:\s*(\S+)/)
    if (vm) {
      curVoice = vm[1]
      if (removed.has(curVoice)) continue
      out.push(line)
      continue
    }
    if (!t || /^[A-Za-z]:/.test(t)) { out.push(line); continue }   // заголовок/пустая
    if (curVoice && removed.has(curVoice)) continue                // тело убранного голоса
    out.push(line)
  }
  return out.join('\n')
}

// инструментал: та же музыка (план, в т.ч. с накопленными приёмами), но без
// вокальной партии; стиль получает подсказку, лирика — [Instrumental]
const hasVocals = computed(() =>
  ((rollData.value && rollData.value.voice_order) || []).some((v) => /vocal/i.test(v)))

async function renderInstrumental() {
  const vocalVoices = ((rollData.value && rollData.value.voice_order) || []).filter((v) => /vocal/i.test(v))
  if (!vocalVoices.length) return
  trickBusy.value = true
  rollErr.value = ''
  trickMsg.value = ''
  buildJob.value = { id: null, status: 'starting' }
  try {
    await ensureBaseAbc()
    const abc = dropVoices(planDraft.value || baseAbc.value, vocalVoices)
    const id = await api.submit({
      title: (props.job.title || 'трек') + ' · инструментал',
      style: trickStyle(props.job.style) + ', instrumental, no vocals',
      lyrics: '[Instrumental]',
      seed: props.job.seed || 0,   // 0 — воркер возьмёт сид родителя
      cot: props.job.cot === 'off' ? 'melody' : props.job.cot,
      abc,
      parent_id: props.job.id, role: 'rebuild',
    })
    trickMsg.value = t('studio.trick.done', { id })
    buildJob.value = { id, status: 'queued' }
    startJobPoll()
    pollBuild()
  } catch (e) {
    buildJob.value = null
    rollErr.value = String(e)
  } finally {
    trickBusy.value = false
  }
}

// ---------- Приёмы: точечные «поломки» плана по выделенным тактам ----------

const trickBusy = ref(false)
const trickMsg = ref('')
// Приёмы хранятся СПИСКОМ, а не текстом: план = исходник + повтор всех
// приёмов (адреса тактов у приёмов — по исходному плану, ролл показывает
// его же, поэтому повтор стабилен). Так «убрать приём» — просто вычеркнуть
// из списка. sentMarks — метки, уже уехавшие в отрендеренные версии (история).
const baseAbc = ref(null)     // исходный план джобы (подкачивается раз)

// подкачка исходного плана — через Go-биндинг: fetch('/audio/...') из окна
// Wails не долетает до воркера (нет его origin)
async function ensureBaseAbc() {
  if (!baseAbc.value) {
    baseAbc.value = await api.jobAbcText(props.job.id, abcFile.value || 'score.abc')
  }
}
const pendingSpecs = ref([])  // неподтверждённые: [{kind, label, from, to, dir, targets}]
const sentMarks = ref([])     // [{kind, label, from, to}] — история версий
const trickMarks = computed(() => [...sentMarks.value, ...pendingSpecs.value])
const planDraft = computed(() =>
  pendingSpecs.value.length && baseAbc.value
    ? pendingSpecs.value.reduce((abc, s) => applyTrick(abc, s), baseAbc.value)
    : null)

const marksOfBar = (b) =>
  trickMarks.value.filter((m) => b.end_sec > m.from && b.start_sec < m.to)
// точка — на такте его голоса в своей колонке (позиционная сетка)
const isTrickCell = (v, pos) => {
  const b = barAt(v, pos)
  return !!b && marksOfBar(b).length > 0
}
const trickTitle = (v, pos) => {
  const b = barAt(v, pos)
  return b ? marksOfBar(b).map((m) => m.label).join(', ') : ''
}

function trickLabel(kind, extra) {
  if (kind === 'chord') return t('studio.trick.chord') + ' ' + t('studio.trick.chord.' + (extra.flavor || 'dark'))
  if (kind === 'octave') return t('studio.trick.oct.' + (extra.dir === 'down' ? 'down' : 'up'))
  if (kind === 'tempo') return t('studio.trick.tempo.' + (extra.dir === 'down' ? 'down' : 'up'))
  if (kind === 'instrument') return '+ ' + t('studio.trick.inst.' + (extra.inst || 'flute')) + ' (' + (extra.section || '—') + ')'
  return t('studio.trick.' + kind)
}

function voiceOfBar(b) {
  return Object.keys(b.voices || {})[0] || Object.keys(b.rests || {})[0] || ''
}

// выделение позиций: колонки ролла (музыкальные такты, голоса параллельны)
const selPos = computed(() => {
  const s = rollSel.value
  if (!s || !posCount.value) return null
  const lo = Math.max(0, Math.min(s.a, s.b))
  const hi = Math.min(posCount.value - 1, Math.max(s.a, s.b))
  return lo <= hi ? { lo, hi } : null
})
const hasSel = computed(() => !!selPos.value)
const hasVocalSel = computed(() => {
  const p = selPos.value
  if (!p) return false
  return (rollData.value.voice_order || []).some((v) =>
    /vocal/i.test(v) && (voiceBarList.value[v] || []).slice(p.lo, p.hi + 1).some((b) => (b.voices || {})[v]))
})

// приём по выделению: адресация — чистая функция (тесты в abcEdit.test),
// позиция = такт, все голоса в выбранных колонках (октава — только вокал).
// «+ инструмент» план не трогает — рендер куска группой и замена дорожек (addSection).
const instSel = ref(TRICK_INSTRUMENTS[0].id)
const instOptions = computed(() => [...TRICK_INSTRUMENTS, ...TRICK_MUTES].map((i) => ({ value: i.id, label: t('studio.trick.inst.' + i.id) })))

async function runTrick(kind, extra = {}) {
  const p = selPos.value
  if (!p) return
  if (kind === 'instrument') return addSection(extra.inst)
  const r = pickTargets(voiceBarList.value, p.lo, p.hi, kind)
  if (!r || !r.targets.length) return
  const spec = { kind, label: trickLabel(kind, extra), from: r.from, to: r.to,
    dir: extra.dir, flavor: extra.flavor, targets: r.targets }
  trickBusy.value = true
  rollErr.value = ''
  trickMsg.value = ''
  try {
    await ensureBaseAbc()
    // потолок голоса фиксируется по исходному плану версии — не уползает от повторов
    if (kind === 'vocalUp' || kind === 'vocalVary') spec.ceiling = vocalCeiling(baseAbc.value)
    pendingSpecs.value = [...pendingSpecs.value, spec]
    trickMsg.value = t('studio.trick.staged', { n: pendingSpecs.value.length })
  } catch (e) {
    rollErr.value = String(e)
  } finally {
    trickBusy.value = false
  }
}

// «+ инструмент» / приём группой: выделенный кусок перерендеривается моделью
// целой группой (план окна со всеми голосами, стиль трека + приписка, сид
// трека), затем сервис useInserts заменяет в треке только дорожки inst.stems
// (голос не трогается). Замеры: замена целого куска давала слышный шов,
// наложение одиночной партии — чужой грув и второй голос; замена стема
// с подгонкой по бочке держит ритм и не шьёт весь микс.
async function addSection(instId) {
  const p = selPos.value
  const r = selTimeRange()
  const inst = [...TRICK_INSTRUMENTS, ...TRICK_MUTES].find((i) => i.id === instId)
  if (!p || !r || !inst) return
  if (inst.mute) {
    // без рендера: заглушить дорожки в окне и пересобрать трек
    trickBusy.value = true
    rollErr.value = ''
    try {
      const res = await inserts.addMute(props.job.id, { instId: inst.id, from: r.from, to: r.to, stems: inst.mute, db: inst.db })
      if (res && res.variant) await api.playFile(props.job.id, res.variant.file, props.job.duration_sec)
    } catch (e) {
      rollErr.value = String(e)
    } finally {
      trickBusy.value = false
    }
    return
  }
  trickBusy.value = true
  rollErr.value = ''
  trickMsg.value = ''
  instJob.value = { id: null, status: 'starting' }
  try {
    await ensureBaseAbc()
    const req = sectionRequest(props.job, baseAbc.value, inst, r.from, r.to)
    if (!req.abc.includes('|')) {
      rollErr.value = t('studio.trick.fragment.empty')
      instJob.value = null
      return
    }
    req.title = (props.job.title || 'трек') + ' · ' + t('studio.trick.inst.' + inst.id)
    const childId = await api.submit(req)
    sentMarks.value = [...sentMarks.value,
      { kind: 'instrument', label: '+ ' + t('studio.trick.inst.' + inst.id), from: r.from, to: r.to }]
    // рендер начинается с такта контекста (sliceAbc pad=1): по плану его
    // начало стоит на from − lead
    const lead = sliceLeadSec(baseAbc.value, r.from, 1)
    // доля — по темпу в месте вклейки (приём «темп» мог его поменять)
    const beat = beatSecAt(baseAbc.value, r.from)
    const spec = { childId, instId: inst.id, from: r.from, to: r.to, lead, beat,
      db: inst.db ?? INSERT_DEFAULT_DB, stems: inst.stems,
      // мелодическим — плавный вход за такт до выделения, сбивке — точно по доле
      fadeIn: inst.fadeIn ?? beat * 4, fadeOut: beat, keepHighHz: inst.keepHighHz || 0 }
    saveStudioState()
    // микс — на вечном сервисе: студию можно закрыть сразу
    inserts.register([{ ...spec, parent: props.job.id, srcJob: props.job.id }])
    instJob.value = { id: childId, status: 'queued' }
    startJobPoll()
    pollInst()
  } catch (e) {
    instJob.value = null
    rollErr.value = String(e)
  } finally {
    trickBusy.value = false
  }
}

// время выделенного диапазона: делегирует в selRange — тот уже знает про
// точные секунды волны и кап по длине звука
function selTimeRange() {
  return selRange.value
}

const pickableCount = computed(() => {
  const r = selTimeRange()
  if (!r) return 0
  const hit = (m) => m.to > r.from && m.from < r.to
  return pendingSpecs.value.filter(hit).length + sentMarks.value.filter(hit).length
})

function unpickSelection() {
  const r = selTimeRange()
  if (!r) return
  const hit = (m) => m.to > r.from && m.from < r.to
  pendingSpecs.value = pendingSpecs.value.filter((m) => !hit(m))
  sentMarks.value = sentMarks.value.filter((m) => !hit(m))
  trickMsg.value = pendingSpecs.value.length
    ? t('studio.trick.staged', { n: pendingSpecs.value.length })
    : ''
}

// модель следует плану приблизительно: регистр голоса и инструменты план
// держит слабо — такие приёмы дублируются припиской в стиль (trickStyleSuffix,
// тесты там же); тот же двойной ход, что у драматургии «взрыв»
function trickStyle(base) {
  const suffix = trickStyleSuffix(pendingSpecs.value)
  if (!suffix) return base
  const head = (base || '').trim().replace(/,$/, '')
  return head ? head + ', ' + suffix : suffix
}

// пересборка: одна кнопка на все накопленные приёмы; тот же стиль/лирика/seed,
// план с поломками (req_abc). Черновик — быстрая проба начала, список не сбрасывает.
async function rebuild(draft = false) {
  trickBusy.value = true
  rollErr.value = ''
  trickMsg.value = ''
  if (!draft) buildJob.value = { id: null, status: 'starting' }   // отклик сразу
  try {
    await ensureBaseAbc()
    // без план-правок — исходный план (тот же seed → практически тот же трек).
    // План режем по фактической длительности аудио: аудио могло обрезаться
    // лимитом токенов, а полный план длиннее — пересборка по нему раздувалась
    // до десятков минут («2-минутный трек → 10:40»).
    const full = planDraft.value || baseAbc.value
    const dur = Number(props.job.duration_sec) || 0
    const abc = dur > 0 ? sliceAbc(full, 0, dur + 1, 0) : full
    const id = await api.submit({
      title: (props.job.title || 'трек') + (draft ? ' · ✦' : ' · приёмы'),
      style: trickStyle(props.job.style),
      lyrics: props.job.lyrics,
      seed: props.job.seed || 0,   // тот же seed = тот же голос; 0 — воркер возьмёт сид родителя
      cot: props.job.cot === 'off' ? 'melody' : props.job.cot,   // abc требует full|melody
      abc,
      draft,
      parent_id: props.job.id, role: draft ? 'fragment' : 'rebuild',
    })
    if (draft) {
      trickMsg.value = t('studio.trick.drafted', { id })
    } else {
      inheritMarks(id)
      sentMarks.value = trickMarks.value.slice()   // всё отправленное — история
      pendingSpecs.value = []
      trickMsg.value = t('studio.trick.done', { id })
      buildJob.value = { id, status: 'queued' }
      startJobPoll()
      pollBuild()
    }
  } catch (e) {
    buildJob.value = null
    rollErr.value = String(e)
  } finally {
    trickBusy.value = false
  }
}

// откат: план — исходный, метки с ролла убираются все (отображение;
// готовые версии уже отрендерены и не меняются)
function resetDraft() {
  pendingSpecs.value = []
  sentMarks.value = []
  trickMsg.value = ''
}

// метки приёмов переезжают в новую версию (формат — состояние студии)
function inheritMarks(newJobId) {
  if (!newJobId) return
  try {
    const key = 'yue_studio_state'
    const st = JSON.parse(localStorage.getItem(key) || '{}')
    st.jobs = st.jobs || {}
    st.jobs[newJobId] = { ...(st.jobs[newJobId] || {}), trickMarks: trickMarks.value }
    localStorage.setItem(key, JSON.stringify(st))
  } catch {}
}

// проверка приёма по месту: мини-партитура из текущего плана (окно выделения
// или последнего приёма, ± такт контекста) драфт-рендером. Кусок звучит
// ПРЯМО ЗДЕСЬ: статус рядом с кнопкой, по готовности включается сам.
// Тот же механизм следит и за полной пересборкой (buildJob).
const fragJob = ref(null)
const buildJob = ref(null)
let jobsTimer = null

function stopFragPoll() {
  if (jobsTimer) { clearInterval(jobsTimer); jobsTimer = null }
}

function watchJob(refObj, key, label, playFn = null, onDone = null) {
  return async () => {
    if (!refObj.value || !refObj.value.id) return
    try {
      const jobs = await api.jobs()
      const j = (jobs || []).find((x) => x.id === refObj.value.id)
      if (!j) return
      const was = refObj.value.status
      refObj.value = j
      if (j.status === 'done' && was !== 'done') {
        if (onDone) await onDone(j)
        const play = playFn || (() => api.playAudio(j.id))
        await toggleArtifact(key + j.id, `${label} · #${j.id}`, play)
      }
      maybeStopPoll()
    } catch (e) {
      rollErr.value = String(e)   // раньше молчаливый catch хоронил ошибки микса
      maybeStopPoll()
    }
  }
}

// общий таймер живёт, пока хоть одна из слежений активна: раньше завершение
// любой джобы убивало опрос для остальных (партия инструмента не доезжала)
function maybeStopPoll() {
  const active = [fragJob, buildJob, instJob].some((r) =>
    r.value && r.value.id && ['queued', 'running'].includes(r.value.status))
  if (!active) stopFragPoll()
}
const pollFragment = watchJob(fragJob, 'f', 'кусок')
// новая версия трека: по готовности переносим все вклейки (с текущей
// громкостью) вечному сервису useInserts — он микширует, даже если студия закрыта
const pollBuild = watchJob(buildJob, 'b', 'новая версия', null, async (j) => {
  inserts.carryTo(props.job.id, j.id, props.job.id)
})
const instJob = ref(null)

// партия инструмента: по готовности дитя сервис useInserts пересобирает трек
// со всеми вклейками (ffmpeg на ПК, в ритм по бочке) — играем смешанный файл
// вклейки этого трека с громкостью и отметкой «в сетке / по плану»
const appliedInserts = computed(() => inserts.appliedFor(props.job.id))
// подписи строк реестра: эффект на дорожку — «эффект · дорожка», окно «до конца»
const insertNames = {
  chainName: (id) => (dspChains.value.find((c) => c.id === id) || { name: id }).name,
  stemName: (s) => t('studio.dsp.target.' + s),
  instName: (id) => t('studio.trick.inst.' + id),
}
const insertWin = computed(() => ({ fmt: fmtDur, toEnd: t('studio.inserts.toEnd'), whole: t('studio.inserts.whole') }))
const dbBusy = ref(false)
async function onInsertDb(it, value) {
  dbBusy.value = true
  rollErr.value = ''
  try {
    const r = await inserts.setDb(props.job.id, it.childId, Number(value))
    if (r && r.variant) await api.playFile(props.job.id, r.variant.file, props.job.duration_sec)
  } catch (e) {
    rollErr.value = String(e)
  } finally {
    dbBusy.value = false
  }
}

// «куплеты реже»: во всех куплетах плана гитары −6 дБ, барабаны −3 дБ —
// куплет «воздушнее», припев сильнее открывается (замер на #212: куплет
// был тише припева на 0.6 LU, стал на 1.9)
async function sparseVerses() {
  const voice = ((rollData.value && rollData.value.voice_order) || [])[0]
  const wins = sectionWindows((rollData.value && rollData.value.bars) || [], voice, 'verse')
    .filter((w) => w.from < (Number(props.job.duration_sec) || Infinity))
  if (!wins.length) { rollErr.value = t('studio.sparse.none'); return }
  trickBusy.value = true
  rollErr.value = ''
  try {
    const res = await inserts.addMutes(props.job.id, wins.flatMap((w) => [
      { instId: 'otherdown', from: w.from, to: w.to, stems: ['other'], db: -6 },
      { instId: 'drumsdown', from: w.from, to: w.to, stems: ['drums'], db: -3 },
    ]))
    if (res && res.variant) await api.playFile(props.job.id, res.variant.file, props.job.duration_sec)
  } catch (e) {
    rollErr.value = String(e)
  } finally {
    trickBusy.value = false
  }
}

// «ещё вариант»: тот же кусок, другой сид — модель играет его по-разному
// (сбивка на одном сиде удалась, на другом звучала провалом). Новый рендер
// станет текущим, прежние остаются в alts — переключатель 1/2/3.
async function moreVariant(it) {
  const inst = TRICK_INSTRUMENTS.find((i) => i.id === it.instId)
  if (!inst) return
  trickBusy.value = true
  rollErr.value = ''
  try {
    await ensureBaseAbc()
    const req = sectionRequest(props.job, baseAbc.value, inst, it.from, it.to,
      Math.floor(Math.random() * 1e9))
    req.title = (props.job.title || 'трек') + ' · ' + t('studio.trick.inst.' + inst.id) + ' · вариант'
    const childId = await api.submit(req)
    const spec = { instId: it.instId, from: it.from, to: it.to, lead: it.lead, beat: it.beat, db: it.db,
      stems: it.stems, fadeIn: it.fadeIn, fadeOut: it.fadeOut, keepHighHz: it.keepHighHz }
    inserts.register([{ ...spec, childId, replaces: it.childId, parent: props.job.id, srcJob: props.job.id }])
    instJob.value = { id: childId, status: 'queued' }
    startJobPoll()
    pollInst()
  } catch (e) {
    rollErr.value = String(e)
  } finally {
    trickBusy.value = false
  }
}
async function pickAlt(it, alt) {
  if (alt === it.childId) return
  dbBusy.value = true
  rollErr.value = ''
  try {
    const r = await inserts.selectAlt(props.job.id, it.childId, alt)
    if (r && r.variant) await api.playFile(props.job.id, r.variant.file, props.job.duration_sec)
  } catch (e) {
    rollErr.value = String(e)
  } finally {
    dbBusy.value = false
  }
}

// ЭКСПЕРИМЕНТ «продолжение с места»: трек до начала выделения остаётся тем же
// дублем, дальше модель играет заново (другой сид) — новый трек-вложение.
// Приёмы, собранные на ролле, уходят изменённым планом.
// что изменить в звучании с места: приписка к стилю трека (инструменты и
// характер в план не записать — «electric guitar enters and builds»)
const contStyle = ref('')
async function continueFromSel() {
  const r = selTimeRange()
  if (!r) return
  trickBusy.value = true
  rollErr.value = ''
  try {
    let abc = ''
    if (pendingSpecs.value.length) {
      await ensureBaseAbc()
      abc = continuationPlan(baseAbc.value, pendingSpecs.value)   // приёмы — ровно по разу
    }
    const id = await api.continueJob(props.job.id, r.from, 0, abc, contStyle.value.trim())
    buildJob.value = { id, status: 'queued' }
    trickMsg.value = t('studio.cont.done', { id })
    startJobPoll()
    pollBuild()
  } catch (e) {
    rollErr.value = String(e)
  } finally {
    trickBusy.value = false
  }
}

// «перепеть с места» по частям: голос выделенной части меняется приёмами
// голоса (выше / вариации / октава), остальной голос и музыка версии — как были.
// Продолжение берётся от ИСТОЧНИКА голоса (у версии-микса своих шагов модели
// нет), 2 дубля; сервис useRevoice по готовности подставляет голос дубля
// только в окно части → новая версия под «📎» (замер 2026-09-30, #214/#210).
const REVOICE_TAKES = 2
async function revoiceFromSel() {
  const r = selTimeRange()
  if (!r) return
  trickBusy.value = true
  rollErr.value = ''
  trickMsg.value = ''
  try {
    const jobs = await api.jobs()
    const byId = Object.fromEntries((jobs || []).map((j) => [j.id, j]))
    const srcId = voiceSource(byId[props.job.id] || props.job, byId)
    const src = srcId != null && byId[srcId]
    if (!src) { rollErr.value = t('studio.revoice.nosrc'); return }
    await ensureBaseAbc()
    const srcAbc = srcId === props.job.id ? baseAbc.value : await api.jobAbcText(srcId, src.abc_file || 'score.abc')
    // адреса тактов приёмов — по плану этой версии; у источника план должен совпадать
    const n = (abc) => Object.entries(planTimeline(abc)).filter(([v]) => /vocal/i.test(v)).map(([, l]) => l.length).join()
    if (n(srcAbc) !== n(baseAbc.value)) { rollErr.value = t('studio.revoice.plan', { id: srcId }); return }
    const specs = pendingSpecs.value.filter((s) => revoiceSpecKinds.has(s.kind))
    const abc = continuationPlan(srcAbc, specs)
    const beat = beatSecAt(baseAbc.value, r.from)
    const what = specs.map((s) => s.label).join(', ') || t('studio.revoice.take')
    const ids = []
    for (let k = 0; k < REVOICE_TAKES; k++) {
      const id = await api.continueJob(srcId, r.from, 0, abc, '')
      ids.push(id)
      // источник голоса новой версии — этот дубль (его голос подставлен)
      revoice.register([{ parent: props.job.id, child: id, from: r.from, to: r.to, beat, voiceSrc: id,
        title: `${props.job.title || 'трек'} · голос: ${what} (дубль #${id})` }])
    }
    const quiet = vocalEndsQuiet((rollData.value && rollData.value.bars) || [], r.to)
    trickMsg.value = t('studio.revoice.done', { ids: ids.map((i) => '#' + i).join(', ') }) +
      (quiet ? '' : ' ' + t('studio.revoice.seam'))
  } catch (e) {
    rollErr.value = String(e)
  } finally {
    trickBusy.value = false
  }
}

// воспроизведение партии: если файл микса ещё не на сервере (автовклейка не
// успела/не дошла) — пересобрать прямо сейчас; кнопка ▶ тем же путём самолечится
async function playInstrument(j) {
  // студия могла заметить готовность партии раньше сервиса: сначала сервис
  // вклеивает готовые партии, потом играем свежий микс (имя — по реестру)
  j = { ...j, mixing: true }
  instJob.value = j
  try {
    await inserts.flush()
    const file = inserts.latestFile(props.job.id)
    try {
      if (!file) throw new Error('нет микса')
      await api.playFile(props.job.id, file, props.job.duration_sec)
    } catch {
      // файла на воркере нет (пересборка не дошла) — пересобрать сейчас
      const r = await inserts.rebuild(props.job.id)
      if (!r || !r.variant) throw new Error(t('studio.trick.inst.notready'))
      await api.playFile(props.job.id, r.variant.file, props.job.duration_sec)
    }
  } finally {
    // пока шла пересборка, могли запустить новую партию — её статус не трогаем
    if (instJob.value && instJob.value.id === j.id) instJob.value = { ...j, mixing: false }
  }
}
const pollInst = watchJob(instJob, 'i', 'инструмент', playInstrument)

function startJobPoll() {
  stopFragPoll()
  jobsTimer = setInterval(async () => {
    await pollFragment()
    await pollBuild()
    await pollInst()
  }, 3000)
}

async function renderFragment() {
  let from, to
  const r = selTimeRange()
  if (r) {
    from = r.from; to = r.to
  } else if (pendingSpecs.value.length) {
    const m = pendingSpecs.value[pendingSpecs.value.length - 1]
    from = m.from; to = m.to
  } else return
  trickBusy.value = true
  rollErr.value = ''
  trickMsg.value = ''
  fragJob.value = { id: null, status: 'starting' }   // отклик на клик — сразу
  try {
    await ensureBaseAbc()
    const mini = sliceAbc(planDraft.value || baseAbc.value, from, to)
    if (!mini.includes('|')) {
      rollErr.value = t('studio.trick.fragment.empty')
      return
    }
    const id = await api.submit({
      title: (props.job.title || 'трек') + ' · кусок',
      style: trickStyle(props.job.style),
      lyrics: props.job.lyrics,
      seed: props.job.seed || 0,   // 0 — воркер возьмёт сид родителя
      cot: props.job.cot === 'off' ? 'melody' : props.job.cot,
      abc: mini,
      draft: true,
      parent_id: props.job.id, role: 'fragment',
    })
    fragJob.value = { id, status: 'queued' }
    startJobPoll()
    pollFragment()
  } catch (e) {
    fragJob.value = null
    rollErr.value = String(e)
  } finally {
    trickBusy.value = false
  }
}

async function makeMinus() {
  const exclude = Object.keys(stemMute.value).filter((k) => stemMute.value[k])
  if (!exclude.length) return
  rollBusy.value = true
  try {
    const r = await api.makeMinus(props.job.id, exclude)
    if (r && r.file) {
      minusReady.value = true
      toggleArtifact(`m${props.job.id}:minus`, `минус (−${exclude.join(', ')}) · #${props.job.id}`,
        () => api.playFile(props.job.id, r.file, props.job.duration_sec))
    }
  } catch (e) {
    rollErr.value = String(e)
  } finally { rollBusy.value = false }
}

function playStem(s) {
  toggleArtifact(`s${props.job.id}:${s.file}`, `${s.name} · #${props.job.id}`,
    () => api.playFile(props.job.id, s.file, props.job.duration_sec))
}

// ---------- Овердаб ----------

function odToggleChip(idx) {
  const cur = new Set(odChips.value)
  cur.has(idx) ? cur.delete(idx) : cur.add(idx)
  odChips.value = cur
}

// строка партии: чипы + ручной ввод; русский ввод переводится в английский (как стили)
async function odFinalStyle() {
  const chips = [...odChips.value].map((i) => odPartyChips[i].en)
  const manual = odStyle.value.trim()
  let s = [...chips, manual].filter(Boolean).join(', ')
  if (s && props.autoTranslate && /[а-яё]/i.test(s)) {
    try { s = (await api.translate(s)).text } catch {}
  }
  return s
}

// лирика овердаба: распознавание из трека и адаптация-перевод
const odLyrBusy = ref('')
const odLyrErr = ref('')
async function odRecognizeLyrics() {
  odLyrErr.value = ''
  odLyrBusy.value = 'rec'
  try {
    // текст из аудио самой джобы — без повторной загрузки файла
    const r = await api.jobLyrics(props.job.id)
    if (r && r.text) odLyrics.value = r.text
  } catch (e) {
    odLyrErr.value = String(e)
  } finally {
    odLyrBusy.value = ''
  }
}
async function odAdaptLyrics() {
  if (!odLyrics.value.trim()) return
  odLyrErr.value = ''
  odLyrBusy.value = 'adapt'
  try {
    const r = await api.adaptLyrics(odLyrics.value)
    if (r && r.text) odLyrics.value = r.text
  } catch (e) {
    odLyrErr.value = String(e)
  } finally {
    odLyrBusy.value = ''
  }
}

async function submitOverdub() {
  const style = await odFinalStyle()
  if (!style) return
  odBusy.value = true
  try {
    await api.submitOverdub(props.job.id, style, odLyrics.value.trim(), odGain.value, '')
    emit('close')
  } catch (e) {
    rollErr.value = String(e)
  } finally { odBusy.value = false }
}

// импорт внешнего трека: у него нет токенов модели — продолжить и перепеть нельзя
const isImport = computed(() => props.job.style === IMPORT_STYLE)
// подсказка кнопки, которая у импортированного трека недоступна
const importTip = (key) => (isImport.value ? t('studio.import.noTokens') : t(key))

// ---------- ЭКСПЕРИМЕНТ «голос альбома» (Seed-VC на воркере) ----------
// голос этого трека поётся тембром голоса другой песни; кусок образца воркер
// подбирает сам. Без Seed-VC на воркере блок виден с подписью «недоступно».
const seedvcOk = ref(false)
// whisper (распознавание текста) установлен на воркере; пока не знаем — считаем, что да
const whisperOk = ref(true)
const vcRef = ref('')
const vcBusy = ref(false)
const vcRefOptions = computed(() => allJobs.value
  .filter((j) => !j.parent_id && j.id !== props.job.id && j.status === 'done' && j.audio_file)
  .map((j) => ({ value: String(j.id), label: `#${j.id} ${j.title || ''}` })))
async function submitVoice() {
  if (!Number(vcRef.value)) return
  vcBusy.value = true
  try {
    await api.voiceConvert(props.job.id, { ref_job_id: Number(vcRef.value) })
    emit('close')
  } catch (e) {
    rollErr.value = String(e)
  } finally { vcBusy.value = false }
}

// ---------- DSP ----------

// подпись варианта: эффект целиком — по цепочке, микс — по реестру вклеек,
// иначе подпись, сохранённая воркером (микс из MCP или с другого ПК)
function variantLabel(v) {
  const file = v.file
  if (file.startsWith('overdub-inst-')) {
    const label = mixLabel(file, { applied: inserts.appliedFor(props.job.id), jobs: allJobs.value,
      labelOf: (id) => t('studio.trick.inst.' + id), fmt: fmtDur })
    if (label) return t('studio.trick.inst.mix', { what: label })
    return v.label || t('studio.trick.inst.variant', { id: mixChildId(file) })
  }
  const isPrev = file.startsWith('dsp-preview-')
  const id = String(file).replace(/^dsp-preview-/, '').replace(/^dsp-/, '').replace(/\.flac$/, '')
  const c = dspChains.value.find((x) => x.id === id)
  return (c ? c.name : id) + (isPrev ? ' (превью 15с)' : '')
}

function selChain(chainId) {
  dspSel.value = chainId
  const c = dspChains.value.find((c) => c.id === chainId)
  dspParams.value = chainDefaults(c)
  // голосовая цепочка (мегафон, телефон…) без выбранной дорожки — сама на голос
  dspTarget.value = voiceTarget(c, dspTarget.value)
}

async function ensureJobMetrics() {
  if (!jobMetrics.value) {
    try { jobMetrics.value = await api.analyze(props.job.id) } catch {}
  }
}

async function reloadVariants() {
  try { dspVariants.value = (await api.dspVariants(props.job.id)) || [] } catch {}
}

// Быстрое превью: кусок трека (выделение, иначе 15 с от курсора) с эффектом на
// весь трек или на выбранную дорожку; «было» и «стало» — куски на ПК, в список
// вариантов не попадают. Переключение было↔стало — с той же позиции.
const fxPrev = ref(null)          // {from, to, dur_sec, wet_solo?, which: 'wet'|'dry', label}
const fxSolo = ref(false)         // только дорожка, без остального микса
const FX_PREV_KEY = 'fxprev'      // ключ «сейчас играет превью» в общем плеере
function fxPrevPos() {
  const playing = nowPlayingKey.value === FX_PREV_KEY && playerState.value.job_id === props.job.id
  return playing ? (playerState.value.position_sec || 0) : 0
}
async function playFxPrev(which, startSec = 0) {
  const piece = fxSolo.value && fxPrev.value && fxPrev.value.wet_solo ? which + '_solo' : which
  await api.playPreview(props.job.id, '', piece, startSec)
  nowPlayingKey.value = FX_PREV_KEY
  fxPrev.value = { ...fxPrev.value, which }
  refreshPlayer()
}
async function previewDsp() {
  const c = curChain.value
  if (!c) return
  const win = previewWindow(selTimeRange(), waveCursor.value || null, props.job.duration_sec)
  dspBusy.value = true
  try {
    const res = await api.fxPreview(props.job.id, dspTarget.value, [{ chain: c.id, params: { ...(dspParams.value || {}) } }],
      win.from, win.to)
    fxPrev.value = { ...res, which: 'wet', label: c.name + (dspTarget.value ? ' · ' + t('studio.dsp.target.' + dspTarget.value) : '') }
    await playFxPrev('wet')
    rollErr.value = ''
  } catch (e) {
    rollErr.value = String(e)
  } finally { dspBusy.value = false }
}
async function toggleFxPrev() {
  if (!fxPrev.value) return
  try {
    await playFxPrev(fxPrev.value.which === 'wet' ? 'dry' : 'wet', fxPrevPos())
  } catch (e) { rollErr.value = String(e) }
}

// на что эффект: '' — весь трек, иначе дорожка (стем) — через пересборку
// дорожек: остальное не меняется (звон голоса #254 — обработка микса глушила гитары)
// guitar/piano — подробные дорожки 6-стемной модели (внутри «прочего»)
const DSP_TARGETS = ['', 'vocals', 'drums', 'bass', 'other', 'guitar', 'piano']

// линия громкости по волне: весь трек — отдельный вариант; дорожка — запись
// реестра пересборки (копится со вклейками/эффектами, повторная заменяет)
const envOn = ref(false)
const envTarget = ref('')
const envPts = ref([])
const envBusy = ref(false)
function envLoad() {
  const rec = envTarget.value && inserts.appliedFor(props.job.id)
    .find((x) => x.envelope && (x.stems || [])[0] === envTarget.value)
  envPts.value = rec ? rec.envelope.map((p) => ({ ...p })) : []
}
watch(envTarget, envLoad)
watch(envOn, (on) => { if (on) envLoad() })
// поднять/опустить выделенный участок линии шагом ENV_BUMP_DB, края — плавные
const ENV_BUMP_DB = 2
function bumpSel(delta) {
  const r = selRange.value
  if (r) envPts.value = bumpRange(envPts.value, r.from, r.to, delta)
}
const envCanApply = computed(() => !envBusy.value && (envTarget.value ? true : !isFlat(envPts.value)))
async function applyEnvelope() {
  envBusy.value = true
  try {
    const pts = envPts.value.map(({ t, db }) => ({ t, db }))
    let v
    if (envTarget.value) {
      const res = await inserts.addStemEnvelope(props.job.id, { stem: envTarget.value, envelope: pts })
      v = res && res.variant
      // пустая линия убрала единственную запись реестра — пересобирать нечего
      if (!v) waveErr.value = t('studio.wave.env.nothing')
    } else {
      v = await api.volumeEnvelope(props.job.id, '', pts)
    }
    await reloadVariants()
    if (v) await api.playFile(props.job.id, v.file, props.job.duration_sec)
  } catch (e) {
    waveErr.value = String(e)
  } finally { envBusy.value = false }
}
const dspTarget = ref('')
const dspTargetOptions = computed(() => DSP_TARGETS.map((v) => ({ value: v, label: t('studio.dsp.target.' + (v || 'mix')) })))
// цели эффекта: цепочке с ключом (ducking) весь микс недоступен
const fxTargetOptions = computed(() => dspTargetOptions.value.filter((o) => o.value || !needsStem(curChain.value)))

// «найти свист»: узкие тона (воркер) в выбранной дорожке или миксе — окно =
// выделение на ролле/волне или крутилки start/end; до трёх тонов → вырезы
const toneMsg = ref('')
const toneBusy = ref(false)
async function findWhistle() {
  const sel = selTimeRange()
  const p = dspParams.value || {}
  const from = sel ? sel.from : Number(p.start) || 0
  const to = sel ? sel.to : Number(p.end) || 0
  toneBusy.value = true
  toneMsg.value = ''
  try {
    const tones = (await api.jobTones(props.job.id, from, to, dspTarget.value)) || []
    if (!tones.length) { toneMsg.value = t('studio.dsp.tones.none'); return }
    dspParams.value = applyFoundTones(p, tones, sel)
    toneMsg.value = t('studio.dsp.tones.found', {
      list: tones.map((x) => t('studio.dsp.tones.item', { hz: Math.round(x.hz), db: Math.round(x.prominence_db) })).join(', ') })
  } catch (e) {
    toneMsg.value = String(e)
  } finally {
    toneBusy.value = false
  }
}

// «Ритм-гейт»: сетка долей (темп и сильная доля) — по выделению или всему треку
const gridBusy = ref(false)
const gridMsg = ref('')
async function findGrid() {
  const sel = selTimeRange()
  gridBusy.value = true
  gridMsg.value = ''
  try {
    const g = await api.jobGrid(props.job.id, sel?.from ?? 0, sel?.to ?? 0)
    dspParams.value = { ...dspParams.value, bpm: g.bpm, offset: g.offset }
    // source: drums | mix
    gridMsg.value = t(`studio.dsp.grid.found.${g.source}`, { bpm: g.bpm.toFixed(1), at: fmtDur(g.offset) })
  } catch (e) {
    gridMsg.value = String(e)
  } finally {
    gridBusy.value = false
  }
}

async function applyDsp() {
  const c = curChain.value
  if (!c) return
  dspBusy.value = true
  try {
    if (dspTarget.value) {
      // эффект на дорожку: в реестр пересборки (копится со вклейками), окно — выделение
      const sel = selTimeRange()
      const res = await inserts.addStemFx(props.job.id, { stem: dspTarget.value, chain: c.id,
        params: { ...(dspParams.value || {}) }, from: sel ? sel.from : 0, to: sel ? sel.to : 0 })
      await reloadVariants()
      if (res && res.variant) await api.playFile(props.job.id, res.variant.file, props.job.duration_sec)
      return
    }
    await ensureJobMetrics()
    await api.applyDsp(props.job.id, c.id, dspParams.value || {})
    await reloadVariants()
  } catch (e) {
    rollErr.value = String(e)
  } finally { dspBusy.value = false }
}

// Эффекты «одним кликом» (мастеринг, дыхание): цепочка на весь микс с крутилками
// выбранного уровня, результат сразу отдельным треком-версией (повторный клик —
// ещё одна версия, лишние удалить)
const oneClickLevel = ref('medium')
async function applyOneClick(chainId) {
  const c = dspChains.value.find((x) => x.id === chainId)
  if (!c) { rollErr.value = t('studio.dsp.oneclick.none', { name: chainId }); return }
  dspBusy.value = true
  try {
    selChain(chainId)
    dspTarget.value = ''
    await ensureJobMetrics()
    const params = oneClickParams(chainId, oneClickLevel.value)
    const v = await api.applyDsp(props.job.id, chainId, params)
    await reloadVariants()
    const name = variantLabel(v) + ' (' + t('studio.dsp.level.' + oneClickLevel.value) + ')'
    await api.variantToTrack(props.job.id, v.file, (props.job.title || 'трек') + ' · ' + name)
    rollErr.value = ''
    trickMsg.value = t('studio.dsp.totrack.done', { name })
  } catch (e) {
    rollErr.value = String(e)
  } finally { dspBusy.value = false }
}

function vDelta(v, key, dec = 1) {
  const a = jobMetrics.value
  if (!a || !v.metrics) return ''
  let av = a[key], bv = v.metrics[key]
  if (key.startsWith('bands.')) {
    const k = key.slice(6)
    av = a.bands && a.bands[k]
    bv = v.metrics.bands && v.metrics.bands[k]
  }
  if (av == null || bv == null) return ''
  const d = Number(bv) - Number(av)   // вариант минус исходник
  return (d > 0 ? '+' : '') + d.toFixed(dec)
}

function playVariant(v) {
  toggleArtifact(`v${props.job.id}:${v.file}`, `${variantLabel(v)} · #${props.job.id}`,
    () => api.playFile(props.job.id, v.file, props.job.duration_sec))
}

// удалить вариант (файл + метрики) — с подтверждением, как остальные удаления
function delVariant(v) {
  askConfirm(t('studio.dsp.del.title', { name: variantLabel(v) }), t('studio.dsp.del.body'),
    async () => {
      try { await api.dspVariantDelete(props.job.id, v.file) } catch (e) { alert(String(e)) }
      reloadVariants()
    })
}

// вариант эффекта → отдельный трек с подписью («трек · Кассета»);
// стемы/минус/эффекты в его студии работают (включая эффекты поверх эффекта)
async function variantToTrack(v) {
  dspBusy.value = true
  try {
    await api.variantToTrack(props.job.id, v.file,
      (props.job.title || 'трек') + ' · ' + variantLabel(v))
    rollErr.value = ''
    trickMsg.value = t('studio.dsp.totrack.done', { name: variantLabel(v) })
  } catch (e) {
    rollErr.value = String(e)
  } finally { dspBusy.value = false }
}

function openVariantMetrics(v) {
  emit('open-metrics', props.job, { metrics: v.metrics, title: `${variantLabel(v)} · #${props.job.id}` })
}

window.addEventListener('mouseup', onWindowMouseup)
onUnmounted(() => window.removeEventListener('mouseup', onWindowMouseup))
</script>

<template>
  <main class="settings-page studio-page">
    <section class="studio-sheet">
      <!-- «назад» — сверху, у названия: страница длинная, нижняя кнопка не на виду -->
      <div class="studio-top">
        <button class="ghost" @click="emit('close')">{{ t('common.back') }}</button>
        <h2>{{ t('studio.title') }} <span class="muted">#{{ job.id }} {{ job.title }}</span></h2>
      </div>
      <!-- шаги работы с треком: ссылки на разделы одной страницы (волна нужна всем шагам — не вкладки) -->
      <nav v-if="rollData" class="studio-steps">
        <button v-for="(st, i) in STUDIO_STEPS" :key="st" class="ghost small-btn" :class="{ on: activeStep === st }"
                @click="goStep(st)">{{ i + 1 }} · {{ t('studio.step.' + st) }}</button>
      </nav>
      <div class="roll-block" @mouseup="barSelEnd" @mouseleave="barSelEnd">
        <p v-if="rollBusy" class="muted">{{ t('studio.parsing') }}</p>
        <p v-if="!abcFile" class="muted">
          {{ t('studio.noScore') }}
          <button class="ghost small-btn" :disabled="transcribeBusy" @click="retryTranscribe">
            {{ transcribeBusy ? t('studio.noScore.busy') : t('studio.noScore.retry') }}</button>
        </p>
        <p v-if="rollErr" class="error">{{ rollErr }}</p>
        <template v-if="rollData">
          <p class="muted roll-meta">
            {{ rollData.tempo_bpm }} BPM · {{ rollData.key }} · {{ rollData.meter }} ·
            {{ posCount }} тактов · ~{{ fmtDur(rollData.duration_sec) }}
            <template v-if="selRange"> · выделено {{ selRange.from.toFixed(0) }}–{{ selRange.to.toFixed(0) }} с</template>
          </p>
          <h3 id="st-listen" class="studio-step">1 · {{ t('studio.step.listen') }} <span class="muted">{{ t('studio.step.listen.sub') }}</span></h3>
          <div class="studio-box wave-panel">
            <div class="studio-box-head">
              <span>{{ t('studio.wave.caption') }}</span>
              <span class="muted wave-hint">{{ t('studio.wave.hint') }}</span>
            </div>
            <div class="studio-box-body">
            <div class="wave-toolbar">
              <VSelect v-model="waveFile" :options="waveFiles" style="width:220px" />
              <button class="ghost small-btn" :class="{ on: waveMode === 'amp' }"
                      :title="t('studio.wave.amp.tip')" @click="waveMode = 'amp'">{{ t('studio.wave.amp') }}</button>
              <button class="ghost small-btn" :class="{ on: waveMode === 'spectrum' }"
                      :title="t('studio.wave.spectrum.tip')" @click="waveMode = 'spectrum'">{{ t('studio.wave.spectrum') }}</button>
              <label class="wave-snap" :title="t('studio.wave.snap.tip')">
                <input type="checkbox" v-model="waveSnap">{{ t('studio.wave.snap') }}
              </label>
              <button class="ghost small-btn" :class="{ on: envOn }" :title="t('studio.wave.env.tip')"
                      @click="envOn = !envOn">{{ t('studio.wave.env') }}</button>
              <template v-if="envOn">
                <VSelect v-model="envTarget" :options="dspTargetOptions" style="max-width: 150px" />
                <button class="ghost small-btn" :disabled="envBusy || !envPts.length" @click="envPts = []">{{ t('studio.wave.env.reset') }}</button>
                <template v-if="selRange">
                  <button class="ghost small-btn" :disabled="envBusy" :title="t('studio.wave.env.bump.tip')"
                          @click="bumpSel(ENV_BUMP_DB)">{{ t('studio.wave.env.up') }}</button>
                  <button class="ghost small-btn" :disabled="envBusy" :title="t('studio.wave.env.bump.tip')"
                          @click="bumpSel(-ENV_BUMP_DB)">{{ t('studio.wave.env.down') }}</button>
                </template>
                <button class="primary small" :disabled="!envCanApply" :title="t('studio.wave.env.apply.tip')" @click="applyEnvelope">
                  {{ envBusy ? '…' : t('studio.wave.env.apply') }}</button>
              </template>
              <span v-if="waveBusy" class="muted">{{ t('studio.wave.loading') }}</span>
              <span v-if="waveErr" class="error">{{ waveErr }}</span>
            </div>
            <span class="wave-colors" :title="t('studio.wave.color.tip')">
              <span class="muted wave-colors-cap">{{ t('studio.wave.color') }}</span>
              <button v-for="c in waveColors" :key="c.id" class="wave-swatch" :class="{ on: waveColor === c.hex }"
                      :style="{ background: c.hex }" :aria-label="c.id"
                      @click="waveColor = c.hex"></button>
            </span>
            <WaveView v-if="wavePeaks" :peaks="wavePeaks" :duration="waveDuration"
                      :marks="waveMarks" :edges="waveEdges" :snap="waveSnap"
                      :mode="waveMode" :spectrum-url="spectrumUrl" :color="waveColor"
                      :cursor-sec="waveCursor" :selection="selRange"
                      :envelope="envOn ? envPts : null"
                      @seek="onWaveSeek" @select="onWaveSelect" @envelope="(v) => (envPts = v)" />
            <p v-if="envOn" class="muted wave-hint">{{ t('studio.wave.env.hint') }}</p>
            </div>
          </div>
          <div class="studio-box">
            <div class="studio-box-head">
              <span>{{ t('studio.roll.caption') }}</span>
              <span class="roll-zoom">
                <button class="ghost small-btn" :title="t('studio.roll.zoom.out')" @click="rollZoom(-4)">−</button>
                <button class="ghost small-btn" :title="t('studio.roll.zoom.in')" @click="rollZoom(4)">+</button>
              </span>
            </div>
            <div class="studio-box-body">
          <div class="roll-scroll" @wheel="onRollWheel">
            <div class="roll-grid" :style="{ gridTemplateColumns: `70px repeat(${posCount}, minmax(${rollCellW}px, 1fr))` }">
              <div></div>
              <div v-for="pos in rollPositions" :key="'s' + pos" class="roll-sec" :class="{ start: isSecStart(pos) }" :title="secLabel(pos)">{{ isSecStart(pos) ? secLabel(pos) : '' }}</div>
              <template v-for="v in rollData.voice_order" :key="v">
                <div class="roll-voice">{{ voiceLabel(v) }}</div>
                <div v-for="pos in rollPositions" :key="v + pos"
                     class="roll-cell" :class="['d' + cellDensity(v, pos), { sel: isBarSel(pos), off: cellOff(v, pos), trick: isTrickCell(v, pos), empty: !barAt(v, pos) }]"
                     :title="cellTitle(v, pos)"
                     @mousedown.prevent="barSelStart(pos)" @mouseover="barSelOver(pos)"></div>
              </template>
              <div class="roll-voice">{{ t('studio.chords') }}</div>
              <div v-for="pos in rollPositions" :key="'c' + pos" class="roll-chord">{{ posChord(pos) }}</div>
            </div>
            </div>
            </div>
          </div>
          <h3 id="st-edit" class="studio-step">2 · {{ t('studio.step.edit') }} <span class="muted">{{ t('studio.step.edit.sub') }}</span></h3>
          <!-- правки: секция с шапкой, приёмы — по смысловым группам -->
          <div class="studio-box">
            <div class="studio-box-head"><span>{{ t('studio.trick.caption') }}</span></div>
            <div class="studio-box-body">
          <div class="trick-bar">
            <div class="trick-group">
              <span class="trick-cap">{{ t('studio.group.listen') }}</span>
              <div class="trick-btns">
                <button class="primary small" :disabled="!selRange || previewBusy" @click="makePreview">
                  {{ previewBusy ? t('studio.preview.busy') : t('studio.preview') }}
                </button>
              </div>
            </div>
            <div class="trick-group">
              <span class="trick-cap">{{ t('studio.group.harmony') }}</span>
              <div class="trick-btns">
                <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                        :title="t('studio.trick.chord.dark.tip')" @click="runTrick('chord', { flavor: 'dark' })">{{ t('studio.trick.chord.dark') }}</button>
                <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                        :title="t('studio.trick.chord.lift.tip')" @click="runTrick('chord', { flavor: 'lift' })">{{ t('studio.trick.chord.lift') }}</button>
                <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                        :title="t('studio.trick.chord.tense.tip')" @click="runTrick('chord', { flavor: 'tense' })">{{ t('studio.trick.chord.tense') }}</button>
              </div>
            </div>
            <div class="trick-group">
              <span class="trick-cap">{{ t('studio.group.arrange') }}</span>
              <div class="trick-btns">
                <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                        :title="t('studio.trick.rest.tip')" @click="runTrick('rest')">{{ t('studio.trick.rest') }}</button>
                <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                        :title="t('studio.trick.cut.tip')" @click="runTrick('cut')">{{ t('studio.trick.cut') }}</button>
                <button class="ghost small-btn" :disabled="trickBusy || !rollData"
                        :title="t('studio.sparse.tip')" @click="sparseVerses">{{ t('studio.sparse') }}</button>
              </div>
            </div>
            <div class="trick-group">
              <span class="trick-cap">{{ t('studio.group.tempo') }}</span>
              <div class="trick-btns">
                <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                        :title="t('studio.trick.tempo.up.tip')" @click="runTrick('tempo', { dir: 'up' })">{{ t('studio.trick.tempo.up') }}</button>
                <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                        :title="t('studio.trick.tempo.down.tip')" @click="runTrick('tempo', { dir: 'down' })">{{ t('studio.trick.tempo.down') }}</button>
              </div>
            </div>
            <div v-if="hasVocalSel" class="trick-group">
              <span class="trick-cap">{{ t('studio.group.voice') }}</span>
              <div class="trick-btns">
                <button class="ghost small-btn" :disabled="trickBusy"
                        :title="t('studio.trick.oct.up.tip')" @click="runTrick('octave', { dir: 'up' })">{{ t('studio.trick.oct.up') }}</button>
                <button class="ghost small-btn" :disabled="trickBusy"
                        :title="t('studio.trick.oct.down.tip')" @click="runTrick('octave', { dir: 'down' })">{{ t('studio.trick.oct.down') }}</button>
                <button class="ghost small-btn" :disabled="trickBusy"
                        :title="t('studio.trick.vocalUp.tip')" @click="runTrick('vocalUp')">{{ t('studio.trick.vocalUp') }}</button>
                <button class="ghost small-btn" :disabled="trickBusy"
                        :title="t('studio.trick.vocalVary.tip')" @click="runTrick('vocalVary')">{{ t('studio.trick.vocalVary') }}</button>
                <button class="ghost small-btn" :disabled="trickBusy || isImport"
                        :title="importTip('studio.revoice.tip')" @click="revoiceFromSel">{{ t('studio.revoice') }}</button>
              </div>
            </div>
            <div class="trick-group">
              <span class="trick-cap">{{ t('studio.group.instrument') }}</span>
              <div class="trick-btns">
                <VSelect v-model="instSel" :options="instOptions" :title="t('studio.trick.inst.tip')" style="width:150px" />
                <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                        :title="t('studio.trick.inst.tip')" @click="runTrick('instrument', { inst: instSel })">{{ t('studio.trick.inst.add') }}</button>
              </div>
            </div>
            <div class="trick-group">
              <span class="trick-cap">{{ t('studio.group.continue') }}</span>
              <div class="trick-btns">
                <input v-model="contStyle" class="cont-style" :placeholder="t('studio.cont.style.ph')" :title="t('studio.cont.style.tip')" />
                <button class="ghost small-btn" :disabled="!hasSel || trickBusy || isImport"
                        :title="importTip('studio.cont.tip')" @click="continueFromSel">{{ t('studio.cont') }}</button>
              </div>
            </div>
            <!-- сборка — на всю ширину: финальные действия над треком -->
            <div class="trick-group assemble">
              <span class="trick-cap">{{ t('studio.group.assemble') }}</span>
              <div class="trick-btns">
                <button class="ghost small-btn" :disabled="trickBusy || !(selTimeRange() || pendingSpecs.length)"
                        :title="t('studio.trick.fragment.tip')" @click="renderFragment">
                  {{ trickBusy ? '…' : t('studio.trick.fragment') }}</button>
                <button class="primary small" :disabled="trickBusy"
                        :title="t('studio.trick.rebuild.tip')" @click="rebuild(false)">{{ t('studio.trick.rebuild') }}</button>
                <button class="ghost small-btn" :disabled="trickBusy"
                        :title="t('studio.trick.redraft.tip')" @click="rebuild(true)">{{ t('studio.trick.redraft') }}</button>
                <button class="ghost small-btn" :disabled="!hasVocals || trickBusy"
                        :title="t('studio.novocal.tip')" @click="renderInstrumental">{{ t('studio.novocal') }}</button>
                <span class="spacer"></span>
                <button class="ghost small-btn" :disabled="!pickableCount || trickBusy"
                        :title="t('studio.trick.unpick.tip')" @click="unpickSelection">{{ t('studio.trick.unpick') }}</button>
                <button class="ghost small-btn" :disabled="!planDraft || trickBusy"
                        :title="t('studio.trick.reset.tip')" @click="resetDraft">{{ t('studio.trick.reset') }}</button>
              </div>
            </div>
          </div>
          <p class="muted trick-hint" :title="t('studio.trick.hint')">{{ trickHint }}</p>
          <p v-if="trickMsg" class="ok trick-hint">{{ trickMsg }}</p>
          <p v-if="fragJob" class="trick-hint" :class="fragJob.status === 'error' ? 'error' : 'muted'">
            <template v-if="fragJob.status === 'starting'"><span class="pulse">♪</span> {{ t('studio.trick.fragment.starting') }}</template>
            <template v-else-if="['queued', 'running'].includes(fragJob.status)">
              <span class="pulse">♪</span> {{ t('queue.status.' + fragJob.status) }} · {{ t('studio.trick.fragment.wait') }}<template v-if="fragJob.elapsed_s"> {{ Math.round(fragJob.elapsed_s) }} с</template>
            </template>
            <template v-else-if="fragJob.status === 'done'">
              ♪ {{ t('studio.trick.fragment.play') }}
              <button class="ghost small-btn" :title="t('studio.trick.fragment.replay')"
                      @click="toggleArtifact('f' + fragJob.id, 'кусок · #' + fragJob.id, () => api.playAudio(fragJob.id))">
                {{ playBtn('f' + fragJob.id) }}
              </button>
            </template>
            <template v-else-if="fragJob.status === 'error'">{{ fragJob.error }}</template>
          </p>
          <p v-if="buildJob" class="trick-hint" :class="buildJob.status === 'error' ? 'error' : 'muted'">
            <template v-if="buildJob.status === 'starting'"><span class="pulse">⟳</span> {{ t('studio.trick.build.starting') }}</template>
            <template v-else-if="['queued', 'running'].includes(buildJob.status)">
              <span class="pulse">⟳</span> {{ t('studio.trick.build.label') }} #{{ buildJob.id }} · {{ t('queue.status.' + buildJob.status) }} · {{ t('studio.trick.fragment.wait') }}<template v-if="buildJob.elapsed_s"> {{ Math.round(buildJob.elapsed_s) }} с</template><template v-if="buildJob.progress_pct != null"> · {{ buildJob.progress_pct }}%</template>
            </template>
            <template v-else-if="buildJob.status === 'done'">
              ✓ {{ t('studio.trick.build.label') }} #{{ buildJob.id }} — {{ t('studio.trick.build.play') }}
              <button class="ghost small-btn" :title="t('studio.trick.fragment.replay')"
                      @click="toggleArtifact('b' + buildJob.id, 'новая версия · #' + buildJob.id, () => api.playAudio(buildJob.id))">
                {{ playBtn('b' + buildJob.id) }}
              </button>
            </template>
            <template v-else-if="buildJob.status === 'error'">{{ buildJob.error }}</template>
          </p>
          <p v-if="instJob" class="trick-hint" :class="instJob.status === 'error' ? 'error' : 'muted'">
            <template v-if="instJob.status === 'starting'"><span class="pulse">♪</span> {{ t('studio.trick.inst.starting') }}</template>
            <template v-else-if="['queued', 'running'].includes(instJob.status)">
              <span class="pulse">♪</span> {{ t('studio.trick.inst.wait') }}<template v-if="instJob.elapsed_s"> {{ Math.round(instJob.elapsed_s) }} с</template>
            </template>
            <template v-else-if="instJob.mixing"><span class="pulse">♪</span> {{ t('studio.trick.inst.mixing') }}</template>
            <template v-else-if="instJob.status === 'done'">
              ✓ {{ t('studio.trick.inst.play') }}
              <button class="ghost small-btn" :title="t('studio.trick.fragment.replay')"
                      @click="toggleArtifact('i' + instJob.id, 'инструмент · #' + instJob.id, () => playInstrument(instJob))">
                {{ instJob.mixing ? '…' : playBtn('i' + instJob.id) }}
              </button>
            </template>
            <template v-else-if="instJob.status === 'error'">{{ instJob.error }}</template>
          </p>
            </div>
          </div>
          <div v-if="appliedInserts.length" class="studio-box">
            <div class="studio-box-head"><span>{{ t('studio.inserts.title') }}</span></div>
            <div class="studio-box-body insert-list">
            <div v-for="it in appliedInserts" :key="it.instId + ':' + it.from" class="insert-row">
              <strong>{{ insertTitle(it, insertNames) }}</strong>
              <span class="muted">{{ insertWindow(it, insertWin) }}</span>
              <label v-if="it.db > -60" class="od-gain">{{ t('studio.inserts.db') }}
                <input type="range" :min="INSERT_MIN_DB" :max="INSERT_MAX_DB" step="1" :value="it.db"
                       :disabled="dbBusy" @change="onInsertDb(it, $event.target.value)" />
                {{ it.db > 0 ? '+' : '' }}{{ it.db }} {{ t('studio.inserts.dbUnit') }}
              </label>
              <span v-if="it.aligned === true" class="muted" :title="t('studio.inserts.aligned.tip')">✓ {{ t('studio.inserts.aligned') }}</span>
              <span v-else-if="it.aligned === false" class="error" :title="t('studio.inserts.plan.tip')">⚠ {{ t('studio.inserts.plan') }}</span>
              <template v-if="(it.alts || []).length > 1">
                <button v-for="(alt, n) in it.alts" :key="alt" class="ghost small-btn" :class="{ on: alt === it.childId }"
                        :disabled="dbBusy" :title="t('studio.inserts.alt.tip')" @click="pickAlt(it, alt)">{{ n + 1 }}</button>
              </template>
              <button v-if="!it.chain" class="ghost small-btn" :disabled="dbBusy || trickBusy" :title="t('studio.inserts.more.tip')"
                      @click="moreVariant(it)">↻ {{ t('studio.inserts.more') }}</button>
            </div>
            </div>
          </div>

          <details class="studio-box">
            <summary class="studio-box-head"><span>{{ t('studio.overdub') }}</span> <span class="muted studio-box-hint">{{ t('studio.overdub.sub') }}</span></summary>
            <div class="studio-box-body">
            <p class="muted">{{ t('studio.overdub.desc') }}</p>
            <div class="od-chips">
              <button v-for="(c, ci) in odPartyChips" :key="ci" class="toggle"
                      :class="{ on: odChips.has(ci) }" @click="odToggleChip(ci)">{{ c.ru }}</button>
            </div>
            <div class="od-row">
              <input v-model="odStyle" :placeholder="t('studio.overdub.style.ph')" class="od-style" />
              <label class="od-gain">{{ t('studio.overdub.gain') }} <input type="range" min="0.1" max="1" step="0.05" v-model.number="odGain" /> {{ odGain }}</label>
            </div>
            <div class="od-row">
              <textarea v-model="odLyrics" rows="4" class="od-lyrics"
                        :placeholder="t('studio.overdub.lyrics.ph')"></textarea>
            </div>
            <div class="od-row">
              <button class="ghost small-btn" :disabled="!!odLyrBusy || !whisperOk"
                      :title="!whisperOk ? t('lyrics.job.noWhisper') : t('lyrics.job.tip')" @click="odRecognizeLyrics">
                {{ odLyrBusy === 'rec' ? '…' : t('lyrics.job') }}</button>
              <button class="ghost small-btn" :disabled="!!odLyrBusy || !odLyrics.trim()" :title="t('lyrics.adapt.tip')" @click="odAdaptLyrics">
                {{ odLyrBusy === 'adapt' ? '…' : t('lyrics.adapt') }}</button>
              <span v-if="odLyrErr" class="error">{{ odLyrErr }}</span>
            </div>
            <div class="od-row">
              <button class="primary small" :disabled="odBusy || (!odStyle.trim() && odChips.size === 0)" @click="submitOverdub">
                {{ odBusy ? '…' : t('studio.overdub.generate') }}
              </button>
            </div>
            </div>
          </details>

          <details class="studio-box">
            <summary class="studio-box-head"><span>{{ t('studio.vc') }}</span> <span class="badge exp">{{ t('studio.vc.exp') }}</span> <span class="muted studio-box-hint">{{ t('studio.vc.sub') }}</span></summary>
            <div class="studio-box-body">
            <p class="muted">{{ t('studio.vc.desc') }}</p>
            <p v-if="!seedvcOk" class="muted">{{ t('studio.vc.unavailable') }}</p>
            <div v-else class="od-row">
              <VSelect v-model="vcRef" :options="vcRefOptions" searchable :placeholder="t('studio.vc.ref')" style="width:320px" />
              <button class="primary small" :disabled="vcBusy || !vcRef" @click="submitVoice">
                {{ vcBusy ? '…' : t('studio.vc.go') }}
              </button>
            </div>
            </div>
          </details>

          <h3 id="st-sound" class="studio-step">3 · {{ t('studio.step.sound') }} <span class="muted">{{ t('studio.step.sound.sub') }}</span></h3>
          <details ref="fxBox" class="studio-box">
            <summary class="studio-box-head"><span>{{ t('studio.dsp') }}</span> <span class="muted studio-box-hint">{{ t('studio.dsp.sub') }}</span></summary>
            <div class="studio-box-body">
            <div class="dsp-row">
              <VSelect :model-value="dspSel" :options="dspChains.map((c) => ({ value: c.id, label: c.name }))"
                       :placeholder="t('studio.dsp.chain')" style="max-width: 220px"
                       @update:model-value="(v) => selChain(v)" />
              <VSelect v-model="dspTarget" :options="fxTargetOptions" :title="t('studio.dsp.target.tip')" style="max-width: 150px" />
              <button class="primary small" :disabled="!dspSel || dspBusy || (needsStem(curChain) && !dspTarget)" @click="applyDsp">
                {{ dspBusy ? t('studio.dsp.applying') : t('studio.dsp.apply') }}
              </button>
              <button class="ghost small-btn" :disabled="!dspSel || dspBusy || needsStem(curChain)"
                      :title="t('studio.dsp.preview.tip')" @click="previewDsp">
                {{ dspBusy ? '…' : t('studio.dsp.preview') }}
              </button>
              <button v-if="fxPrev" class="ghost small-btn" :title="t('studio.dsp.ab.tip')" @click="toggleFxPrev">
                {{ fxPrev.which === 'wet' ? t('studio.dsp.ab.wet') : t('studio.dsp.ab.dry') }}
              </button>
              <label v-if="fxPrev && fxPrev.wet_solo" class="muted" :title="t('studio.dsp.solo.tip')">
                <input v-model="fxSolo" type="checkbox" @change="playFxPrev(fxPrev.which, fxPrevPos())" /> {{ t('studio.dsp.solo') }}
              </label>
              <span v-if="fxPrev" class="muted">{{ fxPrev.label }} · {{ fmtDur(fxPrev.from) }}–{{ fmtDur(fxPrev.to) }}</span>
              <button class="ghost" @click="emit('open-metrics', job, null)">{{ t('studio.dsp.metrics') }}</button>
            </div>
            <p v-if="curChain" class="muted dsp-note">{{ curChain.note }}</p>
            <div v-if="dspSel === 'dewhistle'" class="dsp-row">
              <button class="ghost small-btn" :disabled="toneBusy || dspBusy" :title="t('studio.dsp.tones.tip')"
                      @click="findWhistle">{{ toneBusy ? '…' : t('studio.dsp.tones') }}</button>
              <span v-if="toneMsg" class="muted">{{ toneMsg }}</span>
            </div>
            <div v-if="hasGrid(curChain)" class="dsp-row">
              <button class="ghost small-btn" :disabled="gridBusy || dspBusy" :title="t('studio.dsp.grid.tip')"
                      @click="findGrid">{{ gridBusy ? '…' : t('studio.dsp.grid') }}</button>
              <span v-if="gridMsg" class="muted">{{ gridMsg }}</span>
            </div>
            <div v-if="curChain" class="dsp-params">
              <label v-for="p in curChain.params" :key="p.id">
                <span class="dsp-plabel">{{ p.label }}</span>
                <input type="range" :min="p.min" :max="p.max" :step="p.step"
                       v-model.number="dspParams[p.id]" :disabled="dspBusy" />
                <span class="dsp-pval">{{ dspParams[p.id] }}</span>
              </label>
            </div>
            </div>
          </details>

          <details class="studio-box">
            <summary class="studio-box-head"><span>{{ t('pedals') }}</span> <span class="muted studio-box-hint">{{ t('pedals.sub') }}</span></summary>
            <div class="studio-box-body">
              <PedalBoard :job="job" :chains="dspChains" :sel="selRange" :cursor="waveCursor" @applied="reloadVariants" />
            </div>
          </details>

          <div class="studio-box">
            <div class="studio-box-head"><span>{{ t('studio.stems') }}</span></div>
            <div class="studio-box-body">
            <div class="stems-inline">
              <span class="muted">{{ t('studio.stems.minus') }}</span>
              <label v-for="nm in ['drums', 'bass', 'other', 'vocals']" :key="nm" class="stem-toggle">
                <button class="toggle" :class="{ on: !stemMute[nm] }"
                       :title="stemMute[nm] ? t('studio.stem.off') : t('studio.stem.on')"
                       @click="stemMute = { ...stemMute, [nm]: !stemMute[nm] }">
                  {{ stemLabel(nm) }}
                </button>
              </label>
              <button class="primary small" :disabled="rollBusy || !Object.values(stemMute).some(Boolean)"
                      :title="t('studio.minus.tip')" @click="makeMinus">
                {{ rollBusy ? '…' : t('studio.minus') }}
              </button>
              <span class="muted">{{ t('studio.minus.hint') }}</span>
            </div>
            <div v-for="st in stemsList" :key="st.file" class="stem-row">
              <button class="ghost play-mini" :class="{ stop: isPlaying('s' + job.id + ':' + st.file) }"
                      :disabled="playBusy['s' + job.id + ':' + st.file]" @click="playStem(st)">
                {{ playBtn('s' + job.id + ':' + st.file) }}
              </button>
              <strong>{{ stemLabel(st.name) }}</strong>
            </div>
            </div>
          </div>

          <h3 id="st-done" class="studio-step">4 · {{ t('studio.step.done') }} <span class="muted">{{ t('studio.step.done.sub') }}</span></h3>
          <!-- готово: мастеринг одним кликом, громкость альбома и все результаты (варианты) с ▶ / ⤓ / → в треки -->
          <div class="studio-box">
            <div class="studio-box-head"><span>{{ t('studio.done') }}</span></div>
            <div class="studio-box-body">
            <div class="dsp-row">
              <button class="primary" :disabled="dspBusy" :title="t('studio.dsp.master.tip')" @click="applyOneClick('master')">
                {{ dspBusy ? '…' : t('studio.dsp.master') }}
              </button>
              <button class="primary" :disabled="dspBusy" :title="t('studio.dsp.breathe.tip')" @click="applyOneClick('breathe')">
                {{ dspBusy ? '…' : t('studio.dsp.breathe') }}
              </button>
              <VSelect v-model="oneClickLevel" :title="t('studio.dsp.level.tip')" style="max-width: 130px"
                       :options="ONE_CLICK_LEVELS.map((l) => ({ value: l, label: t('studio.dsp.level.' + l) }))" />
              <button class="ghost" :title="t('studio.done.level.tip')" @click="goChain('level')">{{ t('studio.done.level') }}</button>
            </div>
            <p v-if="!dspVariants.length" class="muted">{{ t('studio.done.empty') }}</p>
            <div v-for="v in dspVariants" :key="v.file" class="dsp-variant">
              <button class="ghost play-mini" :class="{ stop: isPlaying('v' + job.id + ':' + v.file) }"
                      :disabled="playBusy['v' + job.id + ':' + v.file]" @click="playVariant(v)">
                {{ playBtn('v' + job.id + ':' + v.file) }}
              </button>
              <strong>{{ variantLabel(v) }}</strong>
              <span v-if="v.metrics" class="muted deltas">
                Δ крест {{ vDelta(v, 'crest_db') }} dB · Δ дин {{ vDelta(v, 'dyn_range_db') }} dB ·
                Δ верх {{ vDelta(v, 'bands.high') }}% · Δ флэтнес {{ vDelta(v, 'flatness_median', 3) }}
              </span>
              <span v-else class="muted">без метрик</span>
              <button class="ghost" @click="openVariantMetrics(v)">полная дельта</button>
              <button class="ghost" :title="t('studio.dsp.savefile.tip')"
                      @click="api.saveAudio(job.id, v.file)">⤓</button>
              <button class="primary small" :disabled="dspBusy" :title="t('studio.dsp.totrack.tip')"
                      @click="variantToTrack(v)">→ в треки</button>
              <button class="ghost small-btn" :title="t('studio.dsp.del.tip')" :disabled="dspBusy"
                      @click="delVariant(v)">✕</button>
            </div>
            </div>
          </div>
        </template>
      </div>
      <div class="set-actions">
        <button class="ghost" @click="emit('close')">{{ t('common.back') }}</button>
      </div>
    </section>
  </main>
</template>
