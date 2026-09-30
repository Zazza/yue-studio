<script setup>
// Студия трека: пиано-ролл партитуры, минус по стемам, овердаб, DSP-цепочки.
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from '../i18n/index.js'
const { t } = useI18n()
import { api } from '../api.js'
import { usePlayer, fmtDur } from '../composables/usePlayer.js'
import { useConfirm } from '../composables/useConfirm.js'
import { useInserts } from '../composables/useInserts.js'
import { odPartyChips } from '../slotOptions.js'
import { applyTrick, beatSecAt, continuationPlan, pickTargets, planTimeline, sectionRequest, sectionWindows, sliceAbc, sliceLeadSec, TRICK_INSTRUMENTS, TRICK_MUTES, trickStyleSuffix } from '../abcEdit.js'
import { useRevoice } from '../composables/useRevoice.js'
import { revoiceSpecKinds, vocalEndsQuiet, voiceSource } from '../vocalParts.js'
import { INSERT_DEFAULT_DB, INSERT_MAX_DB, INSERT_MIN_DB } from '../insertMix.js'
import { mixLabel } from '../insertLabels.js'
import VSelect from '../VSelect.vue'

const props = defineProps({ job: { type: Object, required: true }, autoTranslate: Boolean })
const emit = defineEmits(['close', 'open-metrics'])

const { isPlaying, playBusy, playBtn, toggleArtifact } = usePlayer()
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

// такты строками по 32 — иначе сетка шириной в сотни тактов
const rollChunks = computed(() => {
  const out = []
  for (let i = 0; i < posCount.value; i += 32) {
    out.push(Array.from({ length: Math.min(32, posCount.value - i) }, (_, j) => i + j))
  }
  return out
})

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

const selRange = computed(() => {
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
  await openRoll()
  try { stemsList.value = (await api.jobStems(props.job.id)) || [] } catch {}
  try { dspChains.value = (await api.dspChains()) || [] } catch {}
  // для подписей вклеек из старых файлов (рендер куска → инструмент)
  try { allJobs.value = (await api.jobs()) || [] } catch { /* без списка — подпись по номеру */ }
  reloadVariants()
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

onUnmounted(() => { saveStudioState(); stopFragPoll() })

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

async function openRoll() {
  rollBusy.value = true
  rollErr.value = ''
  try {
    const d = await api.jobScore(props.job.id)
    rollData.value = d
  } catch (e) {
    rollErr.value = String(e)
  } finally { rollBusy.value = false }
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
const posChord = (pos) => {
  for (const v of (rollData.value && rollData.value.voice_order) || []) {
    const b = barAt(v, pos)
    if (b && b.chords && b.chords[0]) return b.chords[0]
  }
  return ''
}
function cellTitle(v, pos) {
  const b = barAt(v, pos)
  if (!b) return `${v}: в этом такте у голоса нет своей партии`
  const tail = b.start_sec >= props.job.duration_sec ? ' · за пределами звука' : ''
  const mark = isTrickCell(v, pos) ? ' · ✋ ' + trickTitle(v, pos) : ''
  return `${b.section} · такт ${pos + 1} · ${b.start_sec.toFixed(1)}–${b.end_sec.toFixed(1)}с${tail}${mark}`
}

function barSelStart(idx) {
  rollDrag = true
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
      seed: props.job.seed || Math.floor(Math.random() * 1e9),
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
    baseAbc.value = await api.jobAbcText(props.job.id, props.job.abc_file || 'score.abc')
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

// время выделенного диапазона позиций (для меток, мини-рендера и превью)
function selTimeRange() {
  const p = selPos.value
  if (!p) return null
  let from = Infinity, to = -Infinity
  for (let pos = p.lo; pos <= p.hi; pos++) {
    const t = posTime(pos)
    if (!isFinite(t.from)) continue
    from = Math.min(from, t.from)
    to = Math.max(to, t.to)
  }
  return isFinite(from) ? { from, to } : null
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
      seed: props.job.seed || Math.floor(Math.random() * 1e9),   // тот же seed = тот же голос
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
      revoice.register([{ parent: props.job.id, child: id, from: r.from, to: r.to, beat, voiceSrc: srcId,
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
      seed: props.job.seed || Math.floor(Math.random() * 1e9),
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

// ---------- DSP ----------

function chainLabel(file) {
  if (file.startsWith('overdub-inst-')) {
    const label = mixLabel(file, { applied: inserts.appliedFor(props.job.id), jobs: allJobs.value,
      labelOf: (id) => t('studio.trick.inst.' + id), fmt: fmtDur })
    return label ? t('studio.trick.inst.mix', { what: label })
      : t('studio.trick.inst.variant', { id: file.replace(/^overdub-inst-/, '').replace(/\.flac$/, '') })
  }
  const isPrev = file.startsWith('dsp-preview-')
  const id = String(file).replace(/^dsp-preview-/, '').replace(/^dsp-/, '').replace(/\.flac$/, '')
  const c = dspChains.value.find((x) => x.id === id)
  return (c ? c.name : id) + (isPrev ? ' (превью 15с)' : '')
}

function selChain(chainId) {
  dspSel.value = chainId
  const c = dspChains.value.find((x) => x.id === chainId)
  const p = {}
  if (c) for (const prm of c.params) p[prm.id] = prm.default
  dspParams.value = p
}

async function ensureJobMetrics() {
  if (!jobMetrics.value) {
    try { jobMetrics.value = await api.analyze(props.job.id) } catch {}
  }
}

async function reloadVariants() {
  try { dspVariants.value = (await api.dspVariants(props.job.id)) || [] } catch {}
}

async function previewDsp() {
  const c = curChain.value
  if (!c) return
  dspBusy.value = true
  try {
    const v = await api.dspPreview(props.job.id, c.id, dspParams.value || {})
    await reloadVariants()
    if (v && v.file) playVariant(v)   // сразу слушаем кусок
  } catch (e) {
    rollErr.value = String(e)
  } finally { dspBusy.value = false }
}

async function applyDsp() {
  const c = curChain.value
  if (!c) return
  dspBusy.value = true
  try {
    await ensureJobMetrics()
    await api.applyDsp(props.job.id, c.id, dspParams.value || {})
    await reloadVariants()
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
  toggleArtifact(`v${props.job.id}:${v.file}`, `${chainLabel(v.file)} · #${props.job.id}`,
    () => api.playFile(props.job.id, v.file, props.job.duration_sec))
}

// удалить вариант (файл + метрики) — с подтверждением, как остальные удаления
function delVariant(v) {
  askConfirm(t('studio.dsp.del.title', { name: chainLabel(v.file) }), t('studio.dsp.del.body'),
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
      (props.job.title || 'трек') + ' · ' + chainLabel(v.file))
    rollErr.value = ''
    trickMsg.value = t('studio.dsp.totrack.done', { name: chainLabel(v.file) })
  } catch (e) {
    rollErr.value = String(e)
  } finally { dspBusy.value = false }
}

function openVariantMetrics(v) {
  emit('open-metrics', props.job, { metrics: v.metrics, title: `${chainLabel(v.file)} · #${props.job.id}` })
}

window.addEventListener('mouseup', onWindowMouseup)
onUnmounted(() => window.removeEventListener('mouseup', onWindowMouseup))
</script>

<template>
  <main class="settings-page studio-page">
    <section class="panel">
      <h2>{{ t('studio.title') }} <span class="muted">#{{ job.id }} {{ job.title }}</span></h2>
      <div class="roll-block" @mouseup="barSelEnd" @mouseleave="barSelEnd">
        <p v-if="rollBusy" class="muted">{{ t('studio.parsing') }}</p>
        <p v-if="rollErr" class="error">{{ rollErr }}</p>
        <template v-if="rollData">
          <p class="muted roll-meta">
            {{ rollData.tempo_bpm }} BPM · {{ rollData.key }} · {{ rollData.meter }} ·
            {{ posCount }} тактов · ~{{ fmtDur(rollData.duration_sec) }}
            <template v-if="selRange"> · выделено {{ selRange.from.toFixed(0) }}–{{ selRange.to.toFixed(0) }} с</template>
          </p>
          <div class="trick-row">
            <span class="muted">{{ t('studio.novocal.label') }}</span>
            <button class="primary small" :disabled="!hasVocals || trickBusy"
                    :title="t('studio.novocal.tip')" @click="renderInstrumental">{{ t('studio.novocal') }}</button>
          </div>

          <div v-for="(chunk, ci) in rollChunks" :key="ci" class="roll-grid"
               :style="{ gridTemplateColumns: `70px repeat(${chunk.length}, minmax(16px, 1fr))` }">
            <div></div>
            <div v-for="pos in chunk" :key="'s' + pos" class="roll-sec" :title="posSection(pos)">{{ (posSection(pos) || '').slice(0, 3) }}</div>
            <template v-for="v in rollData.voice_order" :key="v">
              <div class="roll-voice">{{ v }}</div>
              <div v-for="pos in chunk" :key="v + pos"
                   class="roll-cell" :class="['d' + cellDensity(v, pos), { sel: isBarSel(pos), off: cellOff(v, pos), trick: isTrickCell(v, pos), empty: !barAt(v, pos) }]"
                   :title="cellTitle(v, pos)"
                   @mousedown.prevent="barSelStart(pos)" @mouseover="barSelOver(pos)"></div>
            </template>
            <div class="roll-voice">{{ t('studio.chords') }}</div>
            <div v-for="pos in chunk" :key="'c' + pos" class="roll-chord">{{ posChord(pos) }}</div>
          </div>
          <div class="roll-actions">
            <button class="primary small" :disabled="!selRange || previewBusy" @click="makePreview">
              {{ previewBusy ? t('studio.preview.busy') : t('studio.preview') }}
            </button>
            <span class="muted">{{ t('studio.preview.hint') }}</span>
          </div>

          <div class="trick-row">
            <span class="muted">{{ t('studio.trick.label') }}</span>
            <span class="muted">{{ t('studio.trick.chord') }}</span>
            <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                    :title="t('studio.trick.chord.dark.tip')" @click="runTrick('chord', { flavor: 'dark' })">{{ t('studio.trick.chord.dark') }}</button>
            <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                    :title="t('studio.trick.chord.lift.tip')" @click="runTrick('chord', { flavor: 'lift' })">{{ t('studio.trick.chord.lift') }}</button>
            <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                    :title="t('studio.trick.chord.tense.tip')" @click="runTrick('chord', { flavor: 'tense' })">{{ t('studio.trick.chord.tense') }}</button>
            <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                    :title="t('studio.trick.rest.tip')" @click="runTrick('rest')">{{ t('studio.trick.rest') }}</button>
            <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                    :title="t('studio.trick.cut.tip')" @click="runTrick('cut')">{{ t('studio.trick.cut') }}</button>
            <template v-if="hasVocalSel">
              <button class="ghost small-btn" :disabled="trickBusy"
                      :title="t('studio.trick.oct.up.tip')" @click="runTrick('octave', { dir: 'up' })">{{ t('studio.trick.oct.up') }}</button>
              <button class="ghost small-btn" :disabled="trickBusy"
                      :title="t('studio.trick.oct.down.tip')" @click="runTrick('octave', { dir: 'down' })">{{ t('studio.trick.oct.down') }}</button>
              <button class="ghost small-btn" :disabled="trickBusy"
                      :title="t('studio.trick.vocalUp.tip')" @click="runTrick('vocalUp')">{{ t('studio.trick.vocalUp') }}</button>
              <button class="ghost small-btn" :disabled="trickBusy"
                      :title="t('studio.trick.vocalVary.tip')" @click="runTrick('vocalVary')">{{ t('studio.trick.vocalVary') }}</button>
              <button class="ghost small-btn" :disabled="trickBusy"
                      :title="t('studio.revoice.tip')" @click="revoiceFromSel">{{ t('studio.revoice') }}</button>
            </template>
            <span class="muted" style="margin-left:8px">{{ t('studio.trick.inst.label') }}</span>
            <VSelect v-model="instSel" :options="instOptions" :title="t('studio.trick.inst.tip')" style="width:150px" />
            <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                    :title="t('studio.trick.inst.tip')" @click="runTrick('instrument', { inst: instSel })">{{ t('studio.trick.inst.add') }}</button>
            <input v-model="contStyle" class="cont-style" :placeholder="t('studio.cont.style.ph')" :title="t('studio.cont.style.tip')" />
            <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                    :title="t('studio.cont.tip')" @click="continueFromSel">{{ t('studio.cont') }}</button>
            <button class="ghost small-btn" :disabled="trickBusy || !rollData"
                    :title="t('studio.sparse.tip')" @click="sparseVerses">{{ t('studio.sparse') }}</button>
            <span class="muted">{{ t('studio.trick.tempo.label') }}</span>
            <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                    :title="t('studio.trick.tempo.up.tip')" @click="runTrick('tempo', { dir: 'up' })">{{ t('studio.trick.tempo.up') }}</button>
            <button class="ghost small-btn" :disabled="!hasSel || trickBusy"
                    :title="t('studio.trick.tempo.down.tip')" @click="runTrick('tempo', { dir: 'down' })">{{ t('studio.trick.tempo.down') }}</button>
            <span class="spacer"></span>
            <button class="ghost small-btn" :disabled="trickBusy || !(selTimeRange() || pendingSpecs.length)"
                    :title="t('studio.trick.fragment.tip')" @click="renderFragment">
              {{ trickBusy ? '…' : t('studio.trick.fragment') }}</button>
            <button class="ghost small-btn" :disabled="trickBusy"
                    :title="t('studio.trick.redraft.tip')" @click="rebuild(true)">{{ t('studio.trick.redraft') }}</button>
            <button class="primary small" :disabled="trickBusy"
                    :title="t('studio.trick.rebuild.tip')" @click="rebuild(false)">{{ t('studio.trick.rebuild') }}</button>
            <button class="ghost small-btn" :disabled="!pickableCount || trickBusy"
                    :title="t('studio.trick.unpick.tip')" @click="unpickSelection">{{ t('studio.trick.unpick') }}</button>
            <button class="ghost small-btn" :disabled="!planDraft || trickBusy"
                    :title="t('studio.trick.reset.tip')" @click="resetDraft">{{ t('studio.trick.reset') }}</button>
          </div>
          <p class="muted trick-hint">{{ t('studio.trick.hint') }}</p>
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
          <div v-if="appliedInserts.length" class="insert-list">
            <span class="muted">{{ t('studio.inserts.title') }}</span>
            <div v-for="it in appliedInserts" :key="it.instId + ':' + it.from" class="insert-row">
              <strong>{{ t('studio.trick.inst.' + it.instId) }}</strong>
              <span class="muted">{{ fmtDur(it.from) }}–{{ fmtDur(it.to) }}</span>
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
              <button class="ghost small-btn" :disabled="dbBusy || trickBusy" :title="t('studio.inserts.more.tip')"
                      @click="moreVariant(it)">↻ {{ t('studio.inserts.more') }}</button>
            </div>
          </div>

          <div class="roll-stems">
            <div class="stems-inline">
              <span class="muted">{{ t('studio.stems') }}</span>
              <label v-for="nm in ['drums', 'bass', 'other', 'vocals']" :key="nm" class="stem-toggle">
                <button class="toggle" :class="{ on: !stemMute[nm] }"
                       :title="stemMute[nm] ? 'Выключено из минуса' : 'Присутствует в минусе'"
                       @click="stemMute = { ...stemMute, [nm]: !stemMute[nm] }">
                  {{ nm }}
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
              <strong>{{ st.name }}</strong>
            </div>
          </div>

          <details class="studio-sec">
            <summary>{{ t('studio.overdub') }} <span class="muted">{{ t('studio.overdub.sub') }}</span></summary>
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
              <button class="ghost small-btn" :disabled="!!odLyrBusy" :title="t('lyrics.job.tip')" @click="odRecognizeLyrics">
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
          </details>

          <details class="studio-sec">
            <summary>{{ t('studio.dsp') }} <span class="muted">{{ t('studio.dsp.sub') }}</span></summary>
            <div class="dsp-row">
              <VSelect :model-value="dspSel" :options="dspChains.map((c) => ({ value: c.id, label: c.name }))"
                       :placeholder="t('studio.dsp.chain')" style="max-width: 220px"
                       @update:model-value="(v) => selChain(v)" />
              <button class="primary small" :disabled="!dspSel || dspBusy" @click="applyDsp">
                {{ dspBusy ? t('studio.dsp.applying') : t('studio.dsp.apply') }}
              </button>
              <button class="ghost small-btn" :disabled="!dspSel || dspBusy"
                      :title="t('studio.dsp.preview.tip')" @click="previewDsp">
                {{ dspBusy ? '…' : t('studio.dsp.preview') }}
              </button>
              <button class="ghost" @click="emit('open-metrics', job, null)">{{ t('studio.dsp.metrics') }}</button>
            </div>
            <p v-if="curChain" class="muted dsp-note">{{ curChain.note }}</p>
            <div v-if="curChain" class="dsp-params">
              <label v-for="p in curChain.params" :key="p.id">
                <span class="dsp-plabel">{{ p.label }}</span>
                <input type="range" :min="p.min" :max="p.max" :step="p.step"
                       v-model.number="dspParams[p.id]" :disabled="dspBusy" />
                <span class="dsp-pval">{{ dspParams[p.id] }}</span>
              </label>
            </div>
            <div v-for="v in dspVariants" :key="v.file" class="dsp-variant">
              <button class="ghost play-mini" :class="{ stop: isPlaying('v' + job.id + ':' + v.file) }"
                      :disabled="playBusy['v' + job.id + ':' + v.file]" @click="playVariant(v)">
                {{ playBtn('v' + job.id + ':' + v.file) }}
              </button>
              <strong>{{ chainLabel(v.file) }}</strong>
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
          </details>
        </template>
      </div>
      <div class="set-actions">
        <button class="ghost" @click="emit('close')">{{ t('common.back') }}</button>
      </div>
    </section>
  </main>
</template>
