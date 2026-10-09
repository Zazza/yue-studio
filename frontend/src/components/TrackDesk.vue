<script setup>
// Пульт дорожек студии: строка на каждую дорожку трека — послушать её, видеть её правки и
// собрать ей звук прямо здесь: готовая цепочка или своя (общий редактор с «Инструментами»),
// «▶ стало / ▶ было» на выделении (нет выделения — 15 с от курсора), «в трек» — запись в реестр
// пересборки (окно — выделение, иначе весь трек). Запись движка из «Правок трека» открывается
// здесь же на правку («заменить»). Раскладка строк и окна — чистый модуль trackDesk.js.
import { computed, onMounted, ref } from 'vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { useInserts } from '../composables/useInserts.js'
import { usePlayer } from '../composables/usePlayer.js'
import { useWindowPlay } from '../composables/useWindowPlay.js'
import ChainEditor from './ChainEditor.vue'
import PlaceControls from './PlaceControls.vue'
import BLOCKS from '../fxBlocks.json'
import { fxPresets } from '../fxPresets.js'
import { fromWorkerChain, toWorkerChain, missingRequired, fillAmp } from '../fxChain.js'
import { groupPresets } from '../presetGroups.js'
import { applyEngine, ensureKits } from '../engineRun.js'
import { insertTitle, insertWindow } from '../insertLabels.js'
import { deskRows, previewWindow, applyWindow, presetsFor, rhythmSection, stemLevels, stemAudibility } from '../trackDesk.js'
import { stemPlace } from '../mixDesk.js'

const props = defineProps({
  job: { type: Object, required: true },
  stems: { type: Array, default: () => [] },     // дорожки трека [{name, file}]
  sel: { type: Object, default: null },          // выделение на волне {from, to}
  cursor: { type: Number, default: 0 },          // курсор волны, с
  names: { type: Object, required: true },       // подписи записей реестра (как в «Правках трека»)
  win: { type: Object, required: true },         // подписи окон записей
})
// applied — трек пересобран; stems — дорожки сделаны (студии перечитать список)
const emit = defineEmits(['applied', 'stems'])
const { t, locale } = useI18n()
const inserts = useInserts()
const { toggleArtifact, playBtn, isPlaying, playBusy } = usePlayer()
const before = useWindowPlay('desk-before', () => t('instr.before'))

const open = ref('')             // раскрытая дорожка
const editing = ref(null)        // запись движка на правке: {childId, from, to} | null
const chain = ref([])
const presetId = ref('')
const solo = ref(false)
const assets = ref({ amps: [], irs: [], kits: [] })
const busy = ref('')
const msg = ref('')
const err = ref('')

const tr = (lbl) => (lbl && (lbl[locale.value] || lbl.ru)) || ''
const rows = computed(() => deskRows(props.stems.map((s) => s.name), inserts.appliedFor(props.job.id)))
// разделено без частей барабанов (Demucs / старое разделение) — предложить разделить подробнее
const noParts = computed(() => props.stems.length > 0 && !props.stems.some((s) => s.name === 'kick'))
const fileOf = (stem) => (props.stems.find((s) => s.name === stem) || {}).file || `stem-${stem}.flac`
const stemName = (s) => t('studio.dsp.target.' + s)
const missing = computed(() => missingRequired(chain.value, BLOCKS))
const ready = computed(() => !busy.value && !missing.value.length && toWorkerChain(chain.value).length > 0)
// окно правки: у записи на правке — её окно; иначе выделение (нет — весь трек)
const target = computed(() => (editing.value ? { from: editing.value.from, to: editing.value.to } : applyWindow(props.sel)))
const targetCaption = computed(() => (target.value.to > 0 || target.value.from > 0
  ? t('engine.window', { from: target.value.from.toFixed(1), to: target.value.to > 0 ? target.value.to.toFixed(1) : t('studio.inserts.toEnd') })
  : t('engine.whole')))
const listenWin = computed(() => (editing.value && editing.value.to > editing.value.from
  ? { from: editing.value.from, to: Math.min(editing.value.to, editing.value.from + 15) }
  : previewWindow(props.sel, props.cursor, props.job.duration_sec)))

onMounted(loadAssets)
async function loadAssets() {
  try { assets.value = (await api.fxAssets()) || { amps: [], irs: [], kits: [] } } catch { /* старый воркер — ошибка покажется при «стало» */ }
}

// захват не выбран — по подсказке пресета (amp_hint), иначе первый загруженный: пресет с усилителем без захвата не считается
const withAmp = (c, hints) => fillAmp(c, assets.value.amps, hints)

function pickPreset(p) {
  presetId.value = p.id
  chain.value = withAmp(fromWorkerChain(p.chain, BLOCKS), p.amp_hint)
}

function toggleRow(stem) {
  msg.value = ''
  err.value = ''
  editing.value = null
  if (open.value === stem) { open.value = ''; return }
  open.value = stem
  const first = presetsFor(stem, fxPresets)[0]
  if (first) pickPreset(first)
  else { presetId.value = ''; chain.value = [] }
}

// «изменить» из «Правок трека»: редактор дорожки записи с её цепочкой, кнопка — «заменить»
function editRecord(rec) {
  const stem = (rec.stems || [])[0]
  if (!stem || !rec.engine) return
  // дорожки записи у трека нет (трек разделён на 4 дорожки) — сказать, а не молчать
  if (!rows.value.some((r) => r.stem === stem)) { err.value = t('desk.noRow', { stem: stemName(stem) }); return }
  open.value = stem
  presetId.value = ''
  msg.value = ''
  err.value = ''
  editing.value = { childId: rec.childId, from: rec.from || 0, to: rec.to || 0, label: rec.label || '' }
  chain.value = withAmp(fromWorkerChain(rec.engine, BLOCKS))
}
defineExpose({ editRecord })

function playStem(row) {
  const file = fileOf(row.stem)
  toggleArtifact(`s${props.job.id}:${file}`, `${stemName(row.stem)} · #${props.job.id}`,
    () => api.playFile(props.job.id, file, props.job.duration_sec))
}
const stemKey = (row) => `s${props.job.id}:${fileOf(row.stem)}`

async function makeStems() {
  busy.value = 'stems'
  err.value = ''
  try { await api.makeStems(props.job.id); emit('stems') } catch (e) { err.value = String(e) } finally { busy.value = '' }
}

// «стало»: превью воркера на окне прослушивания (повтор тех же настроек — тот же файл)
async function playAfter() {
  err.value = ''
  msg.value = ''
  const jobId = props.job.id     // всё — до первого await: дорожку и цепочку могут сменить
  const req = { source: open.value, chain: toWorkerChain(chain.value), output: solo.value ? 'solo' : 'mix', preview: true, ...listenWin.value }
  await toggleArtifact('desk-after', t('instr.after'), async () => {
    try {
      await ensureKits(api, req.chain, assets.value.kits, () => { busy.value = 'kit' })
      busy.value = 'preview'
      const r = await api.applyFx(jobId, req)
      if (r && r.clipped) msg.value = t('instr.clipped')
      await api.playFile(jobId, r.file, r.duration_sec || 15)
    } catch (e) { err.value = String(e); throw e } finally { busy.value = ''; loadAssets() }
  })
}

// «было»: тот же кусок без обработки — дорожка соло или весь микс
function playBeforeWin() {
  const w = listenWin.value
  before.play({ jobId: props.job.id, file: solo.value ? fileOf(open.value) : props.job.audio_file,
    dur: props.job.duration_sec, from: w.from, to: w.to })
}

// «в трек» / «заменить»: проверка коротким превью → запись реестра → пересборка
async function apply() {
  err.value = ''
  msg.value = ''
  const p = fxPresets.find((x) => x.id === presetId.value)
  const ed = editing.value
  const snap = {
    jobId: props.job.id, dur: props.job.duration_sec, src: open.value, ...target.value,
    chain: toWorkerChain(chain.value),
    label: p ? t('engine.label', { name: tr(p.name) }) : (ed && ed.label) || t('engine.label', { name: t('desk.own') }),
  }
  busy.value = 'apply'
  try {
    const rec = await applyEngine(api, snap, { oldMsg: t('instr.engine.old'), onKit: () => { busy.value = 'kit' } })
    if (ed) await inserts.replaceEngine(snap.jobId, ed.childId, { chain: rec.chain, label: rec.label })
    else await inserts.addStemEngine(snap.jobId, rec)
    // пока шла запись, могли открыть на правку другую запись — её не сбрасывать
    if (editing.value === ed) editing.value = null
    msg.value = t('engine.applied')
    emit('applied')
  } catch (e) { err.value = String(e) } finally { busy.value = ''; loadAssets() }
}

// ритм-секция набором: все части барабанов и бас, что есть у трека, — одной пересборкой
const room = ref(true)
const rhythm = computed(() => rhythmSection(props.stems.map((s) => s.name), fxPresets, room.value, locale.value))
// окно ритм-секции — всегда выделение (нет — весь трек), не окно открытой на правку записи
const rhythmWin = computed(() => applyWindow(props.sel))
const rhythmCaption = computed(() => (rhythmWin.value.to > 0
  ? t('engine.window', { from: rhythmWin.value.from.toFixed(1), to: rhythmWin.value.to.toFixed(1) })
  : t('engine.whole')))
async function applyRhythm() {
  err.value = ''
  msg.value = ''
  const jobId = props.job.id
  const dur = props.job.duration_sec
  const items = rhythm.value.map((r) => ({ ...r, ...rhythmWin.value }))   // всё — до первого await
  busy.value = 'rhythm'
  try {
    // каждая цепочка — проверка коротким превью, как у «в трек»: запись, которую воркер не примет
    // (движок выключен, нет набора, старый воркер), роняла бы каждую следующую пересборку трека
    const recs = []
    for (const it of items) {
      recs.push(await applyEngine(api, { jobId, dur, src: it.stem, from: it.from, to: it.to, chain: it.chain, label: it.label },
        { oldMsg: t('instr.engine.old'), onKit: () => { busy.value = 'kit' } }))
      busy.value = 'rhythm'
    }
    await inserts.addStemEngines(jobId, recs)
    msg.value = t('desk.rhythm.done', { n: items.length })
    emit('applied')
  } catch (e) { err.value = String(e) } finally { busy.value = ''; loadAssets() }
}

// громкость дорожки к треку (замер воркера): почти неслышную двигать и обрабатывать бесполезно (#689: райд −35 дБ)
const levels = computed(() => stemLevels(props.stems))
const levelText = (stem) => (stem in levels.value ? `${levels.value[stem] > 0 ? '+' : ''}${Math.round(levels.value[stem])} ${t('studio.inserts.dbUnit')}` : '')
const levelMark = (stem) => (stem in levels.value ? stemAudibility(levels.value[stem]) : '')

// место дорожки в стерео: отпустил ползунок — запись «место» заменяется, трек пересобирается
const placeOf = (stem) => stemPlace(inserts.appliedFor(props.job.id), stem)
async function setPlace(stem, place) {
  err.value = ''
  busy.value = 'place'
  try { await inserts.setStemPlace(props.job.id, stem, place); emit('applied') } catch (e) { err.value = String(e) } finally { busy.value = '' }
}

async function installKit(name) {
  err.value = ''
  busy.value = 'kit'
  try { await api.installFxKit(name); await loadAssets() } catch (e) { err.value = String(e) } finally { busy.value = '' }
}
async function upload(kind) {
  err.value = ''
  try {
    if (await api.uploadFxAsset(kind)) { await loadAssets(); chain.value = withAmp(chain.value, fxPresets.find((x) => x.id === presetId.value)?.amp_hint) }
  } catch (e) { err.value = String(e) }
}
</script>

<template>
  <div v-if="!stems.length" class="dsp-row">
    <span class="muted">{{ t('desk.noStems') }}</span>
    <button class="primary small" :disabled="!!busy" @click="makeStems">
      {{ busy === 'stems' ? t('instr.stems.busy') : t('instr.stems.make') }}</button>
  </div>
  <div v-if="noParts" class="dsp-row">
    <span class="muted">{{ t('desk.noParts') }}</span>
    <button class="ghost small-btn" :disabled="!!busy" :title="t('desk.resplit.tip')" @click="makeStems">
      {{ busy === 'stems' ? t('instr.stems.busy') : t('desk.resplit') }}</button>
  </div>
  <div v-if="stems.length" class="dsp-row">
    <button class="primary small" :disabled="!!busy || !rhythm.length" :title="t('desk.rhythm.tip')" @click="applyRhythm">
      {{ busy === 'rhythm' ? t('instr.busy') : busy === 'kit' ? t('instr.kit.busy') : t('desk.rhythm') }}</button>
    <label class="muted" :title="t('desk.room.tip')"><input v-model="room" type="checkbox" /> {{ t('desk.room') }}</label>
    <span v-if="!rhythm.length" class="muted">{{ t('desk.rhythm.none') }}</span>
    <span v-else class="muted">{{ rhythm.map((r) => stemName(r.stem)).join(', ') }} · {{ rhythmCaption }}</span>
  </div>
  <p v-if="msg && !open" class="muted">{{ msg }}</p>
  <div v-for="row in rows" :key="row.stem" class="desk-row" :class="{ open: open === row.stem }">
    <div class="desk-head">
      <button class="ghost play-mini" :class="{ stop: isPlaying(stemKey(row)) }" :disabled="playBusy[stemKey(row)]"
              :title="t('desk.solo.tip')" @click="playStem(row)">{{ playBtn(stemKey(row)) }}</button>
      <strong class="desk-name" :title="t('desk.what.' + row.stem)">{{ stemName(row.stem) }}</strong>
      <span v-if="levelText(row.stem)" class="muted desk-level" :class="levelMark(row.stem)" :title="t('desk.level.tip')">
        {{ levelText(row.stem) }}<template v-if="levelMark(row.stem)"> · {{ t('desk.level.' + levelMark(row.stem)) }}</template></span>
      <span v-for="e in row.edits" :key="e.childId" class="desk-chip" :class="{ off: e.off }"
            :title="e.off ? t('desk.chip.off') : ''">{{ insertTitle(e, names) }} · {{ insertWindow(e, win) }}</span>
      <span class="spacer"></span>
      <PlaceControls :model-value="placeOf(row.stem)" :disabled="!!busy" @update:model-value="(p) => setPlace(row.stem, p)" />
      <button class="ghost small-btn" :class="{ on: open === row.stem }" @click="toggleRow(row.stem)">
        {{ open === row.stem ? t('desk.close') : t('desk.sound') }}</button>
    </div>
    <div v-if="open === row.stem" class="desk-editor">
      <p v-if="editing" class="muted">{{ t('desk.editing') }}</p>
      <div v-for="(g, gi) in groupPresets(presetsFor(row.stem, fxPresets), locale)" :key="g.group" class="voice-presets">
        <span class="muted">{{ g.label || (gi === 0 ? t('instr.presets') : t('instr.presets.more')) }}</span>
        <button v-for="p in g.items" :key="p.id" class="ghost small-btn" :class="{ on: p.id === presetId }"
                :title="tr(p.note)" @click="pickPreset(p)">{{ tr(p.name) }}</button>
      </div>
      <ChainEditor v-model="chain" :assets="assets" :busy="busy" @upload="upload" @install-kit="installKit" />
      <p v-if="missing.length" class="err">{{ t('instr.needAmp') }}</p>
      <div class="dsp-row">
        <label class="muted" :title="t('instr.solo.tip')"><input v-model="solo" type="checkbox" /> {{ t('instr.solo') }}</label>
        <button class="ghost small-btn" :disabled="!ready" :title="t('instr.after.tip')" @click="playAfter">
          <template v-if="busy === 'preview' || busy === 'kit'">{{ busy === 'kit' ? t('instr.kit.busy') : t('instr.busy') }}</template>
          <template v-else>{{ playBtn('desk-after') }} {{ t('instr.after') }}</template></button>
        <button class="ghost small-btn" :title="t('instr.before.tip')" @click="playBeforeWin">{{ playBtn('desk-before') }} {{ t('instr.before') }}</button>
        <span class="muted">{{ t('desk.listen', { from: listenWin.from.toFixed(1), to: listenWin.to.toFixed(1) }) }}</span>
        <span class="spacer"></span>
        <span class="muted">{{ targetCaption }}</span>
        <button class="primary small" :disabled="!ready" :title="t('engine.apply.tip')" @click="apply">
          {{ busy === 'apply' ? t('instr.busy') : editing ? t('desk.replace') : t('desk.apply') }}</button>
        <button v-if="editing" class="ghost small-btn" @click="editing = null">{{ t('common.cancel') }}</button>
      </div>
      <p v-if="msg" class="muted">{{ msg }}</p>
    </div>
  </div>
  <p v-if="err" class="err">{{ err }}</p>
</template>

<style scoped>
.desk-row { border-bottom: 1px solid var(--line, #2a2a35); padding: 4px 0; }
.desk-row.open { background: var(--panel-2, rgba(255, 255, 255, 0.02)); }
.desk-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.desk-name { min-width: 90px; cursor: help; }
.desk-level { font-size: 12px; }
.desk-level.silent { opacity: 0.6; font-style: italic; }
.desk-chip { font-size: 12px; border: 1px solid var(--line, #2a2a35); border-radius: 10px; padding: 1px 8px; }
.desk-chip.off { opacity: 0.45; text-decoration: line-through; }
.desk-editor { padding: 6px 0 6px 34px; }
.spacer { flex: 1; }
.small-btn.on { border-color: var(--accent, #7aa2f7); }
</style>
