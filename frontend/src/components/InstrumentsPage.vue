<script setup>
// Страница «Инструменты»: кусок дорожки трека через цепочку звукового движка воркера
// (педали → усилитель NAM → кабинет → пространство) — послушать «было/стало», понравилось —
// та же цепочка на весь трек вариантом. Описание блоков — fxBlocks.json (копия
// worker/fx_blocks.json), логика цепочки — fxChain.js, готовые цепочки — fxPresets.js.
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { useInserts } from '../composables/useInserts.js'
import { usePlayer } from '../composables/usePlayer.js'
import VSelect from '../VSelect.vue'
import BLOCKS from '../fxBlocks.json'
import { fxPresets } from '../fxPresets.js'
import {
  addBlock, removeBlock, moveBlock, toggleBlock, setParam, addBand, removeBand, setBand,
  toWorkerChain, fromWorkerChain, missingRequired,
} from '../fxChain.js'

const { t, locale } = useI18n()
const emit = defineEmits(['close'])
const { toggleArtifact, playBtn, playerState, nowPlayingKey } = usePlayer()
const BEFORE_KEY = 'instr-before'
const inserts = useInserts()

const SOURCES = ['mix', 'vocals', 'drums', 'bass', 'other', 'guitar', 'piano',
  'kick', 'snare', 'toms', 'hh', 'ride', 'crash']
const LEN_MIN = 3
// дорожки, которые пересборка студии меняет (Go: studio.mutable); части барабанов — нет
const STUDIO_STEMS = ['vocals', 'drums', 'bass', 'other', 'guitar', 'piano']
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
const blockOptions = computed(() => Object.keys(BLOCKS).map((k) => ({ value: k, label: tr(BLOCKS[k].label) })))
const preset = computed(() => fxPresets.find((p) => p.id === presetId.value))
const missing = computed(() => missingRequired(chain.value, BLOCKS))
const end = computed(() => Math.min(Number(start.value) + Number(len.value), job.value?.duration_sec || Infinity))
const ready = computed(() => engineOn.value && engineKnown.value && job.value && !missing.value.length &&
  toWorkerChain(chain.value).length > 0 && (source.value === 'mix' || stems.value.includes(source.value)))

function assetOptions(kind, def) {
  const list = (kind === 'amp' ? assets.value.amps : assets.value.irs) || []
  const opts = list.map((a) => ({ value: a.name, label: a.name }))
  return def === '' ? [{ value: '', label: t('instr.builtin') }, ...opts] : opts
}

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
  chain.value = fillAmp(chain.value)   // пресет по умолчанию с усилителем — первый загруженный захват
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
// закрыли страницу во время «было» — остановить: следить за концом куска больше некому
onBeforeUnmount(() => {
  if (beforeUntil.value && nowPlayingKey.value === BEFORE_KEY) {
    beforeUntil.value = null
    nowPlayingKey.value = ''
    api.stopAudio().catch(() => { /* плеер уже остановлен */ })
  }
})

// захват не выбран, а он один/первый есть — подставить: пресет с amp без захватов иначе молчит
function fillAmp(c) {
  const first = (assets.value.amps || [])[0]
  return first ? c.map((b) => (b.type === 'amp' && !b.params.model ? { ...b, params: { ...b.params, model: first.name } } : b)) : c
}

function applyPreset(id) {
  presetId.value = id
  const p = fxPresets.find((x) => x.id === id)
  if (p) chain.value = fillAmp(fromWorkerChain(p.chain, BLOCKS))
}

const newType = ref('reverb')
function add() { chain.value = fillAmp(addBlock(chain.value, newType.value, BLOCKS)) }
const set = (i, key, v) => { chain.value = setParam(chain.value, i, key, v, BLOCKS) }

function window_() {
  const from = Math.max(0, Number(start.value) || 0)
  const l = Math.min(LEN_MAX, Math.max(LEN_MIN, Number(len.value) || 15))
  const dur = job.value?.duration_sec || from + l
  return { from, to: Math.min(from + l, dur) }
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
    busy.value = 'preview'
    const j = job.value             // трек сменят во время расчёта — играть файл того, для кого считали
    try {
      const r = await api.applyFx(j.id, request(true))
      lastPreview.value = r
      if (r && r.clipped) note.value = t('instr.clipped')
      await api.playFile(j.id, r.file, r.duration_sec || len.value)
    } catch (e) { err.value = String(e); throw e } finally { busy.value = '' }
  })
}

// «было»: тот же кусок без обработки — с той же секунды
async function playBefore() {
  err.value = ''
  const { from, to } = window_()
  const file = source.value === 'mix' || !solo.value ? job.value.audio_file : `stem-${source.value}.flac`
  await toggleArtifact(BEFORE_KEY, t('instr.before'), async () => {
    await api.playFile(job.value.id, file, job.value.duration_sec)
    await api.seekAudio(from)
    beforeArmed = false
    beforeUntil.value = { from, to }
  })
}

// «было» играет файл трека целиком — остановить на конце куска (плеер сам этого не знает)
const beforeUntil = ref(null)    // {from, to} куска «было»
let beforeArmed = false
watch(() => playerState.value.position_sec, async (pos) => {
  if (beforeUntil.value == null || nowPlayingKey.value !== BEFORE_KEY) return
  const { from, to } = beforeUntil.value
  if (!beforeArmed) {               // первая позиция может остаться от прошлого проигрывания
    if (pos >= from && pos < to) beforeArmed = true
    return
  }
  if (pos >= to) {
    beforeUntil.value = null
    await toggleArtifact(BEFORE_KEY, t('instr.before'), async () => {})
  }
})

// понравилось — та же цепочка на весь трек вариантом и сразу отдельным треком
async function toTrack() {
  busy.value = 'apply'
  err.value = ''
  const j = job.value               // трек/пресет сменят во время расчёта — вариант и имя остаются свои
  const title = `${j.title || '#' + j.id} · ${tr(preset.value?.name) || t('instr.title')}`
  try {
    const v = await api.applyFx(j.id, request(false))
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
  try {
    await inserts.addStemEngine(j.id, { stem: source.value, chain: toWorkerChain(chain.value), from, to, label })
    note.value = t('instr.toStudio.done')
  } catch (e) { err.value = String(e) } finally { busy.value = '' }
}

async function upload(kind) {
  err.value = ''
  try {
    const r = await api.uploadFxAsset(kind)
    if (r) { await loadAssets(); chain.value = fillAmp(chain.value) }
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

        <div class="voice-presets">
          <span class="muted">{{ t('instr.presets') }}</span>
          <button v-for="p in fxPresets" :key="p.id" class="ghost small-btn" :class="{ on: p.id === presetId }"
                  :title="tr(p.note)" @click="applyPreset(p.id)">{{ tr(p.name) }}</button>
        </div>
        <p v-if="preset" class="muted voice-hint">{{ tr(preset.note) }}</p>

        <div v-for="(b, i) in chain" :key="i" class="instr-block" :class="{ off: !b.on }">
          <div class="instr-block-head">
            <label class="instr-on"><input type="checkbox" :checked="b.on" @change="chain = toggleBlock(chain, i)" /> {{ tr(BLOCKS[b.type].label) }}</label>
            <span class="spacer"></span>
            <button class="ghost icon" :title="t('instr.up')" :disabled="i === 0" @click="chain = moveBlock(chain, i, -1)">↑</button>
            <button class="ghost icon" :title="t('instr.down')" :disabled="i === chain.length - 1" @click="chain = moveBlock(chain, i, 1)">↓</button>
            <button class="ghost icon" :title="t('instr.remove')" @click="chain = removeBlock(chain, i)"><AppIcon name="x" /></button>
          </div>
          <div v-if="b.on" class="dsp-params">
            <label v-for="s in BLOCKS[b.type].strings || []" :key="s.id">
              <span>{{ tr(s.label) }}</span>
              <VSelect :model-value="b.params[s.id]" :options="assetOptions(s.asset, s.default)"
                       :placeholder="t('instr.asset.none')" @update:model-value="(v) => set(i, s.id, v)" />
              <span>
                <button class="ghost small-btn" :title="t('instr.upload.tip')" @click.prevent="upload(s.asset)">{{ t('instr.upload') }}</button>
              </span>
            </label>
            <label v-for="p in BLOCKS[b.type].params" :key="p.id">
              <span>{{ tr(p.label) }}</span>
              <input type="range" :min="p.zero_off ? 0 : p.min" :max="p.max" :step="p.step" :value="b.params[p.id]"
                     @input="(e) => set(i, p.id, Number(e.target.value))" />
              <span class="dsp-pval">{{ b.params[p.id] }}</span>
            </label>
            <template v-if="BLOCKS[b.type].bands">
              <div v-for="(band, k) in b.params.bands" :key="'band' + k" class="instr-band">
                <label v-for="(bs, key) in BLOCKS[b.type].bands" :key="key">
                  <span>{{ t('instr.band', { n: k + 1 }) }} · {{ tr(bs.label) }}</span>
                  <input type="range" :min="bs.min" :max="bs.max" :step="bs.step" :value="band[key]"
                         @input="(e) => (chain = setBand(chain, i, k, key, Number(e.target.value), BLOCKS))" />
                  <span class="dsp-pval">{{ band[key] }}</span>
                </label>
                <button class="ghost small-btn" @click="chain = removeBand(chain, i, k, BLOCKS)">{{ t('instr.band.remove') }}</button>
              </div>
              <button class="ghost small-btn" @click="chain = addBand(chain, i, BLOCKS)">{{ t('instr.band.add') }}</button>
            </template>
          </div>
        </div>

        <div class="corpus-actions">
          <VSelect v-model="newType" :options="blockOptions" style="width: 200px" />
          <button class="ghost" @click="add">{{ t('instr.add') }}</button>
        </div>
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
      </div>
    </section>
  </div>
</template>

<style scoped>
.instr-block { border: 1px solid var(--line, #2a2a35); border-radius: 8px; padding: 6px 10px; margin: 6px 0; }
.instr-block.off { opacity: 0.55; }
.instr-block-head { display: flex; align-items: center; gap: 6px; }
.instr-on { display: flex; align-items: center; gap: 6px; font-size: 13px; }
.instr-band { border-left: 2px solid var(--line, #2a2a35); padding-left: 8px; margin: 4px 0; }
.spacer { flex: 1; }
.small-btn.on { border-color: var(--accent, #7aa2f7); }
</style>
