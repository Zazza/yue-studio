<script setup>
// Страница «Инструменты»: кусок дорожки трека через цепочку звукового движка воркера
// (педали → усилитель NAM → кабинет → пространство) — послушать «было/стало», понравилось —
// та же цепочка на весь трек вариантом. Описание блоков — fxBlocks.json (копия
// worker/fx_blocks.json), логика цепочки — fxChain.js, готовые цепочки — fxPresets.js.
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { ensureKits as ensureKitsOn } from '../engineRun.js'
import { useInserts } from '../composables/useInserts.js'
import { usePlayer } from '../composables/usePlayer.js'
import { useWindowPlay } from '../composables/useWindowPlay.js'
import VSelect from '../VSelect.vue'
import BLOCKS from '../fxBlocks.json'
import { fxPresets } from '../fxPresets.js'
import ChainEditor from './ChainEditor.vue'
import PresetLibrary from './PresetLibrary.vue'
import { toWorkerChain, fromWorkerChain, missingRequired, fillAmp } from '../fxChain.js'
import { groupPresets } from '../presetGroups.js'

const { t, locale } = useI18n()
const emit = defineEmits(['close'])
const { toggleArtifact, playBtn } = usePlayer()
const before = useWindowPlay('instr-before', () => t('instr.before'))
const inserts = useInserts()

const SOURCES = ['mix', 'vocals', 'drums', 'bass', 'other', 'guitar', 'piano',
  'kick', 'snare', 'toms', 'hh', 'ride', 'crash']
const LEN_MIN = 3
// дорожки, которые пересборка студии меняет движком (Go: studio.engineStems): и части барабанов
const STUDIO_STEMS = ['vocals', 'drums', 'bass', 'other', 'guitar', 'piano', 'kick', 'snare', 'toms', 'hh', 'ride', 'crash']
const LEN_MAX = 60

const jobs = ref([])
const jobId = ref('')
const stems = ref([])          // имена дорожек трека (stem-<имя>.flac)
const source = ref('other')
const start = ref(20)
const len = ref(15)
const solo = ref(false)
const chain = ref(fromWorkerChain(fxPresets[0].chain, BLOCKS))
const presetId = ref(fxPresets[0].id)
const assets = ref({ amps: [], irs: [] })
const engineOn = ref(true)
const engineKnown = ref(false) // воркер умеет превью движка (в /config есть fx_preview); до ответа — нет
const busy = ref('')
const err = ref('')
const note = ref('')
const lastPreview = ref(null)  // {file, duration_sec, clipped, key}

const tr = (lbl) => (lbl && (lbl[locale.value] || lbl.ru)) || ''
const job = computed(() => jobs.value.find((j) => String(j.id) === jobId.value) || null)
const jobOptions = computed(() => jobs.value.map((j) => ({ value: String(j.id), label: `#${j.id} · ${j.title || ''}` })))
const sourceOptions = computed(() => SOURCES
  .filter((s) => s === 'mix' || stems.value.includes(s))
  .map((s) => ({ value: s, label: t('studio.dsp.target.' + s) })))
const preset = computed(() => fxPresets.find((p) => p.id === presetId.value))
// синты играют ноты партии по аккордам — их место в студии («Синт по аккордам»), не на дорожке трека
const stemPresets = fxPresets.filter((p) => !(p.stems || []).includes('synth'))
const presetGroups = computed(() => groupPresets(stemPresets, locale.value))
const missing = computed(() => missingRequired(chain.value, BLOCKS))
const end = computed(() => Math.min(Number(start.value) + Number(len.value), job.value?.duration_sec || Infinity))
const ready = computed(() => engineOn.value && engineKnown.value && job.value && !missing.value.length &&
  toWorkerChain(chain.value).length > 0 && (source.value === 'mix' || stems.value.includes(source.value)))

async function loadJobs() {
  try {
    jobs.value = ((await api.jobs()) || []).filter((j) => j.status === 'done' && j.audio_file)
    if (!jobId.value && jobs.value.length) jobId.value = String(jobs.value[0].id)
  } catch (e) { err.value = String(e) }
}

let stemsReq = 0
async function loadStems() {
  const req = ++stemsReq            // ответ для уже сменившегося трека — отбросить
  stems.value = []
  const j = job.value
  if (!j) return
  let names = []
  try {
    names = ((await api.jobStems(j.id)) || []).map((s) => s.name)
  } catch { /* старый воркер или нет дорожек — останется «весь трек» */ }
  if (req !== stemsReq) return
  stems.value = names
  if (!sourceOptions.value.some((o) => o.value === source.value)) source.value = 'mix'
}

async function loadAssets() {
  try { assets.value = (await api.fxAssets()) || { amps: [], irs: [] } } catch { assets.value = { amps: [], irs: [] } }
}

async function loadConfig() {
  try {
    const c = await api.workerConfig()
    engineKnown.value = !!(c && c.fx_preview)   // воркер этапа 2 превью не знает — делал бы варианты
    engineOn.value = !!(c && c.fx_engine)
  } catch { engineKnown.value = false }
}

onMounted(async () => {
  await Promise.all([loadJobs(), loadAssets(), loadConfig()])
  chain.value = withAmp(chain.value)   // пресет по умолчанию с усилителем — захват по подсказке пресета
  fitStart()
  await loadStems()
})
function fitStart() {
  const dur = job.value?.duration_sec
  if (dur && Number(start.value) > dur - LEN_MIN) start.value = Math.max(0, Math.floor(dur - LEN_MIN))
}
watch(jobId, () => { lastPreview.value = null; fitStart(); loadStems() })

const onKey = (e) => { if (e.key === 'Escape') emit('close') }
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

// захват не выбран — по подсказке пресета (amp_hint), иначе первый загруженный: пресет с amp без захвата молчит
const withAmp = (c) => fillAmp(c, assets.value.amps, preset.value?.amp_hint)

function applyPreset(id) {
  presetId.value = id
  const p = fxPresets.find((x) => x.id === id)
  if (p) chain.value = withAmp(fromWorkerChain(p.chain, BLOCKS))
}


function window_() {
  const from = Math.max(0, Number(start.value) || 0)
  const l = Math.min(LEN_MAX, Math.max(LEN_MIN, Number(len.value) || 15))
  const dur = job.value?.duration_sec || from + l
  return { from, to: Math.min(from + l, dur) }
}

// наборы сэмплов, которых нет на воркере, — скачать перед расчётом (по требованию)
// workerChain — уже собранная до первого await цепочка: пока качается набор, форму могут поменять;
// after — какое «занято» вернуть после скачивания (на кнопках снова «считаю»)
async function ensureKits(workerChain, after) {
  const got = await ensureKitsOn(api, workerChain, assets.value.kits, () => { busy.value = 'kit' })
  if (!got.length) return
  await loadAssets()
  busy.value = after
}

async function installKit(name) {
  err.value = ''
  busy.value = 'kit'
  try { await api.installFxKit(name); await loadAssets() } catch (e) { err.value = String(e) } finally { busy.value = '' }
}

function request(preview) {
  const req = { source: source.value, chain: toWorkerChain(chain.value), output: solo.value ? 'solo' : 'mix' }
  if (preview) Object.assign(req, window_(), { preview: true })
  return req
}

async function makeStems() {
  busy.value = 'stems'
  err.value = ''
  try { await api.makeStems(job.value.id); await loadStems() } catch (e) { err.value = String(e) } finally { busy.value = '' }
}

// «стало»: превью воркера (повтор тех же настроек — тот же файл, без пересчёта)
async function playAfter() {
  err.value = ''
  note.value = ''
  await toggleArtifact('instr-after', t('instr.after'), async () => {
    const j = job.value             // трек сменят во время расчёта — играть файл того, для кого считали
    const req = request(true)       // весь запрос — до первого await (скачивание набора)
    try {
      await ensureKits(req.chain, 'preview')
      busy.value = 'preview'
      const r = await api.applyFx(j.id, req)
      lastPreview.value = r
      if (r && r.clipped) note.value = t('instr.clipped')
      await api.playFile(j.id, r.file, r.duration_sec || len.value)
    } catch (e) { err.value = String(e); throw e } finally { busy.value = '' }
  })
}

// «было»: тот же кусок без обработки — с той же секунды, стоп в конце куска
async function playBefore() {
  err.value = ''
  const { from, to } = window_()
  const file = source.value === 'mix' || !solo.value ? job.value.audio_file : `stem-${source.value}.flac`
  await before.play({ jobId: job.value.id, file, dur: job.value.duration_sec, from, to })
}

// понравилось — та же цепочка на весь трек вариантом и сразу отдельным треком
async function toTrack() {
  busy.value = 'apply'
  err.value = ''
  const j = job.value               // трек/пресет сменят во время расчёта — вариант и имя остаются свои
  const title = `${j.title || '#' + j.id} · ${tr(preset.value?.name) || t('instr.title')}`
  const req = request(false)        // весь запрос — до первого await (скачивание набора)
  try {
    await ensureKits(req.chain, 'apply')
    const v = await api.applyFx(j.id, req)
    await api.variantToTrack(j.id, v.file, title)
    note.value = t('instr.toTrack.done')
  } catch (e) { err.value = String(e) } finally { busy.value = '' }
}

// та же цепочка на дорожку трека в окне куска — в реестр пересборки студии (копится со
// вклейками, звучит в треке после пересборки); весь трек — только эффектами студии
async function toStudio() {
  busy.value = 'studio'
  err.value = ''
  const j = job.value
  const { from, to } = window_()
  const label = t('engine.label', { name: tr(preset.value?.name) || t('instr.title') })
  const stem = source.value          // всё — до первого await (скачивание набора)
  const workerChain = toWorkerChain(chain.value)
  try {
    await ensureKits(workerChain, 'studio')
    await inserts.addStemEngine(j.id, { stem, chain: workerChain, from, to, label })
    note.value = t('instr.toStudio.done')
  } catch (e) { err.value = String(e) } finally { busy.value = '' }
}

async function upload(kind) {
  err.value = ''
  try {
    const r = await api.uploadFxAsset(kind)
    if (r) { await loadAssets(); chain.value = withAmp(chain.value) }
  } catch (e) { err.value = String(e) }
}
</script>

<template>
  <div class="modal-backdrop page-backdrop" @click.self="emit('close')">
    <section class="panel page-modal">
      <div class="page-modal-head">
        <h2>{{ t('instr.title') }}</h2>
        <button class="ghost icon" :title="t('common.close')" @click="emit('close')"><AppIcon name="x" /></button>
      </div>
      <div class="page-modal-body">
        <p class="muted">{{ t('instr.desc') }}</p>
        <p v-if="!engineKnown" class="err">{{ t('instr.engine.old') }}</p>
        <p v-else-if="!engineOn" class="err">{{ t('instr.engine.off') }}</p>

        <div class="dsp-params">
          <label>
            <span>{{ t('instr.track') }}</span>
            <VSelect v-model="jobId" :options="jobOptions" searchable />
            <span></span>
          </label>
          <label>
            <span :title="t('instr.source.tip')">{{ t('instr.source') }}</span>
            <VSelect v-model="source" :options="sourceOptions" />
            <span>
              <button v-if="job && !stems.length" class="ghost small-btn" :disabled="!!busy" @click.prevent="makeStems">
                {{ busy === 'stems' ? t('instr.stems.busy') : t('instr.stems.make') }}
              </button>
            </span>
          </label>
          <label>
            <span>{{ t('instr.start') }}</span>
            <input v-model.number="start" type="range" min="0" :max="Math.max(0, (job?.duration_sec || 60) - LEN_MIN)" step="0.5" />
            <span class="dsp-pval">{{ Number(start).toFixed(1) }} {{ t('instr.sec') }}</span>
          </label>
          <label>
            <span>{{ t('instr.len') }}</span>
            <input v-model.number="len" type="range" :min="LEN_MIN" :max="LEN_MAX" step="1" />
            <span class="dsp-pval">{{ len }} {{ t('instr.sec') }}</span>
          </label>
        </div>

        <div v-for="(g, gi) in presetGroups" :key="g.group" class="voice-presets">
          <span class="muted">{{ g.label || (gi === 0 ? t('instr.presets') : t('instr.presets.more')) }}</span>
          <button v-for="p in g.items" :key="p.id" class="ghost small-btn" :class="{ on: p.id === presetId }"
                  :title="tr(p.note)" @click="applyPreset(p.id)">{{ tr(p.name) }}</button>
        </div>
        <p v-if="preset" class="muted voice-hint">{{ tr(preset.note) }}</p>

        <ChainEditor v-model="chain" :assets="assets" :busy="busy" @upload="upload" @install-kit="installKit" />
        <p v-if="missing.length" class="err">{{ t('instr.needAmp') }}</p>

        <div class="corpus-actions">
          <label class="instr-on" :title="t('instr.solo.tip')"><input v-model="solo" type="checkbox" :disabled="source === 'mix'" /> {{ t('instr.solo') }}</label>
          <button class="primary" :disabled="!ready || !!busy" :title="t('instr.after.tip')" @click="playAfter">
            <template v-if="busy === 'preview'">{{ t('instr.busy') }}</template>
            <template v-else>{{ playBtn('instr-after') }} {{ t('instr.after') }}</template>
          </button>
          <button class="ghost" :disabled="!job" :title="t('instr.before.tip')" @click="playBefore">{{ playBtn('instr-before') }} {{ t('instr.before') }}</button>
          <span class="spacer"></span>
          <button class="ghost" :disabled="!ready || !!busy || !STUDIO_STEMS.includes(source)" :title="t('instr.toStudio.tip')" @click="toStudio">
            {{ busy === 'studio' ? t('instr.busy') : t('instr.toStudio') }}
          </button>
          <button class="ghost" :disabled="!ready || !!busy" :title="t('instr.toTrack.tip')" @click="toTrack">
            {{ busy === 'apply' ? t('instr.busy') : t('instr.toTrack') }}
          </button>
        </div>
        <p v-if="note" class="muted">{{ note }}</p>
        <p v-if="err" class="err">{{ err }}</p>
        <p class="muted voice-hint">{{ t('instr.window', { from: window_().from.toFixed(1), to: end.toFixed(1) }) }}</p>

        <h3>{{ t('preset.title') }}</h3>
        <p class="muted">{{ t('preset.lib.desc') }}</p>
        <PresetLibrary />
      </div>
    </section>
  </div>
</template>

<style scoped>
.instr-on { display: flex; align-items: center; gap: 6px; font-size: 13px; }
.spacer { flex: 1; }
.small-btn.on { border-color: var(--accent, #7aa2f7); }
</style>
