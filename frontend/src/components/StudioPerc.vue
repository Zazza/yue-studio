<script setup>
// Студия, шаг «Звук»: перкуссия по сетке трека — поверх трека, ничего не вычитая (как «Синт по аккордам»).
// «Разобрать сетку» — такты и секции из плана (chord_grid), нет плана — такты по темпу и сильной доле (/grid);
// готовая перкуссия (голос драм-машины или сэмплы набора + эффекты), рисунок, свинг, акцент, громкость от трека;
// «▶ стало / ▶ было», «в трек» — правка-добавление в «Правках трека». Удары — percPart.js.
import { computed, ref, watch } from 'vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { useInserts } from '../composables/useInserts.js'
import { usePlayer } from '../composables/usePlayer.js'
import { useWindowPlay } from '../composables/useWindowPlay.js'
import VSelect from '../VSelect.vue'
import ChainEditor from './ChainEditor.vue'
import PlaceControls from './PlaceControls.vue'
import BLOCKS from '../fxBlocks.json'
import { fxPresets } from '../fxPresets.js'
import { fromWorkerChain, toWorkerChain } from '../fxChain.js'
import { applyEngine, ensureKits } from '../engineRun.js'
import { percHits, barsFromBeat, PERC_PATTERNS } from '../percPart.js'
import { previewWindow, applyWindow } from '../trackDesk.js'
import { normPlace } from '../mixDesk.js'

const props = defineProps({
  job: { type: Object, required: true },
  sel: { type: Object, default: null },
  cursor: { type: Number, default: 0 },
})
const emit = defineEmits(['applied'])
const { t, locale } = useI18n()
const inserts = useInserts()
const { toggleArtifact, playBtn } = usePlayer()
const before = useWindowPlay('perc-before', () => t('instr.before'))

const PERCS = fxPresets.filter((p) => (p.stems || []).includes('perc'))
const bars = ref(null)            // такты [{start, end, section}]
const beat = ref(null)            // сетка без плана {bpm, offset}: такты — barsFromBeat со сдвигом shift
const shift = ref(0)              // с какой доли (0…3 от offset) начинается такт — /grid даёт фазу доли, не такта
const sections = ref([])
const presetId = ref(PERCS[0]?.id || '')
const pattern = ref(PERCS[0]?.pattern || 'eighths')
const swing = ref(PERCS[0]?.swing || 0)
const accent = ref(1)
const chain = ref(PERCS[0] ? fromWorkerChain(PERCS[0].chain, BLOCKS) : [])
// место партии в стерео (у готовых — своё: пэды шире, хэт/шейкер в стороне); ложится в пересборке
const place = ref(normPlace(PERCS[0]?.place))
const busy = ref('')
const msg = ref('')
const err = ref('')

const tr = (lbl) => (lbl && (lbl[locale.value] || lbl.ru)) || ''
const preset = computed(() => PERCS.find((p) => p.id === presetId.value))
const presetOptions = PERCS.map((p) => ({ value: p.id, label: tr(p.name) }))
const patternOptions = computed(() => PERC_PATTERNS.map((s) => ({ value: s, label: t('perc.pattern.' + s) })))
const allSections = computed(() => [...new Set((bars.value || []).map((b) => b.section).filter(Boolean))])
const hits = computed(() => (bars.value
  ? percHits(bars.value, { pattern: pattern.value, sections: sections.value, swing: swing.value, accent: accent.value }) : []))
// громкость от трека — rel_db блока perc: ползунок и крутилка в редакторе цепочки — один параметр
const db = computed({
  get: () => { const b = chain.value.find((x) => x.type === 'perc'); return b ? b.params.rel_db : -14 },
  set: (v) => { chain.value = chain.value.map((b) => (b.type === 'perc' ? { ...b, params: { ...b.params, rel_db: Number(v) } } : b)) },
})
const listenWin = computed(() => previewWindow(props.sel, props.cursor, props.job.duration_sec))
const target = computed(() => applyWindow(props.sel))
const ready = computed(() => !busy.value && hits.value.length > 0 && chain.value.some((b) => b.type === 'perc' && b.on))

watch(presetId, () => {
  const p = preset.value
  if (!p) return
  place.value = normPlace(p.place)
  chain.value = fromWorkerChain(p.chain, BLOCKS)
  pattern.value = p.pattern || 'eighths'
  swing.value = p.swing || 0
})
watch(() => props.job.id, () => { bars.value = null; beat.value = null; shift.value = 0; sections.value = []; msg.value = '' })
watch(shift, () => { if (beat.value) bars.value = barsFromBeat(beat.value, props.job.duration_sec, shift.value) })
const shiftOptions = computed(() => [0, 1, 2, 3].map((k) => ({ value: k, label: t('perc.shift.n', { n: k + 1 }) })))

async function loadGrid() {
  busy.value = 'grid'
  err.value = ''
  try {
    try {
      const g = await api.chordGrid(props.job.id)
      beat.value = null
      bars.value = g.bars
      msg.value = t('perc.grid.done', { bars: g.bars.length, bpm: g.bpm })
    } catch {
      // нет плана (или такты по нему не нашлись) — сетка по темпу и сильной доле всего трека, без частей песни
      const g = await api.jobGrid(props.job.id, 0, 0)
      beat.value = g
      bars.value = barsFromBeat(g, props.job.duration_sec, shift.value)
      msg.value = t('perc.grid.beat', { bars: bars.value.length, bpm: g.bpm })
    }
    sections.value = [...allSections.value]
  } catch (e) { err.value = String(e) } finally { busy.value = '' }
}

function toggleSection(s) {
  sections.value = sections.value.includes(s) ? sections.value.filter((x) => x !== s) : [...sections.value, s]
}

// удары партии — в блок perc (секунды трека; воркер сдвигает их на окно превью)
function workerChain() {
  return toWorkerChain(chain.value).map((b) => (b.type === 'perc' ? { ...b, notes: hits.value } : b))
}

// «стало» — трек с партией; «партия отдельно» — она одна (output solo): услышать, что искать в миксе
async function playAfter(solo = false) {
  err.value = ''
  const jobId = props.job.id
  const req = { source: 'mix', chain: workerChain(), output: solo ? 'solo' : 'mix', preview: true, add: true, ...listenWin.value }
  const key = solo ? 'perc-solo' : 'perc-after'
  await toggleArtifact(key, solo ? t('synth.solo') : t('instr.after'), async () => {
    busy.value = 'preview'
    try {
      // бочка/райд из набора: набора на воркере может не быть — докачать, как у пульта дорожек
      await ensureKits(api, req.chain, ((await api.fxAssets()) || {}).kits, (k) => { msg.value = t('perc.kit', { kit: k }) })
      const r = await api.applyFx(jobId, req)
      await api.playFile(jobId, r.file, r.duration_sec || 15)
    } catch (e) { err.value = String(e); throw e } finally { busy.value = '' }
  })
}

function playBefore() {
  const w = listenWin.value
  before.play({ jobId: props.job.id, file: props.job.audio_file, dur: props.job.duration_sec, from: w.from, to: w.to })
}

async function apply() {
  err.value = ''
  msg.value = ''
  const jobId = props.job.id
  const label = t('perc.label', { name: tr(preset.value?.name) || t('perc.own'), pattern: t('perc.pattern.' + pattern.value) })
  const placeNow = { ...place.value }
  const snap = { jobId, dur: props.job.duration_sec, src: 'mix', ...target.value, chain: workerChain(), label }
  busy.value = 'apply'
  try {
    const rec = await applyEngine(api, snap, { oldMsg: t('instr.engine.old'), onKit: (k) => { msg.value = t('perc.kit', { kit: k }) } })
    await inserts.addStemEngine(jobId, { ...rec, add: true, place: placeNow })
    msg.value = t('perc.applied')
    emit('applied')
  } catch (e) { err.value = String(e) } finally { busy.value = '' }
}
</script>

<template>
  <div class="dsp-row">
    <button class="ghost small-btn" :disabled="!!busy" :title="t('perc.grid.tip')" @click="loadGrid">
      {{ busy === 'grid' ? t('instr.busy') : bars ? t('synth.grid.again') : t('perc.grid') }}</button>
    <span v-if="msg" class="muted">{{ msg }}</span>
    <label v-if="beat" class="muted" :title="t('perc.shift.tip')">{{ t('perc.shift') }}
      <VSelect v-model="shift" :options="shiftOptions" style="max-width: 110px" /></label>
  </div>
  <template v-if="bars">
    <div v-if="allSections.length" class="voice-presets">
      <span class="muted">{{ t('synth.sections') }}</span>
      <button v-for="s in allSections" :key="s" class="toggle" :class="{ on: sections.includes(s) }" @click="toggleSection(s)">{{ s }}</button>
    </div>
    <div class="dsp-row">
      <VSelect v-model="presetId" :options="presetOptions" style="max-width: 260px" />
      <VSelect v-model="pattern" :options="patternOptions" style="max-width: 190px" />
      <label class="muted" :title="t('perc.swing.tip')">{{ t('perc.swing') }}
        <input v-model.number="swing" type="range" min="0" max="0.5" step="0.05" /> {{ Math.round(swing * 100) }} %</label>
      <label class="muted" :title="t('perc.accent.tip')">{{ t('perc.accent') }}
        <input v-model.number="accent" type="range" min="0" max="1" step="0.1" /> {{ Math.round(accent * 100) }} %</label>
      <label class="muted">{{ t('synth.level') }}
        <input v-model.number="db" type="range" min="-30" max="6" step="1" :title="t('synth.level.tip')" /> {{ db }} {{ t('studio.inserts.dbUnit') }}</label>
    </div>
    <div class="dsp-row"><span class="muted">{{ t('mix.place') }}</span> <PlaceControls v-model="place" :disabled="!!busy" /></div>
    <p v-if="preset" class="muted studio-box-hint">{{ tr(preset.note) }}</p>
    <details>
      <summary class="muted">{{ t('perc.edit') }}</summary>
      <ChainEditor v-model="chain" :busy="busy" />
    </details>
    <div class="dsp-row">
      <button class="ghost small-btn" :disabled="!ready" :title="t('instr.after.tip')" @click="playAfter()">
        {{ busy === 'preview' ? t('instr.busy') : playBtn('perc-after') + ' ' + t('instr.after') }}</button>
      <button class="ghost small-btn" :disabled="!ready" :title="t('synth.solo.tip')" @click="playAfter(true)">
        {{ playBtn('perc-solo') }} {{ t('synth.solo') }}</button>
      <button class="ghost small-btn" @click="playBefore">{{ playBtn('perc-before') }} {{ t('instr.before') }}</button>
      <span class="muted">{{ t('desk.listen', { from: listenWin.from.toFixed(1), to: listenWin.to.toFixed(1) }) }} · {{ t('perc.hits', { n: hits.length }) }}</span>
      <span class="spacer"></span>
      <button class="primary small" :disabled="!ready" :title="t('perc.apply.tip')" @click="apply">
        {{ busy === 'apply' ? t('instr.busy') : t('desk.apply') }}</button>
    </div>
  </template>
  <p v-if="err" class="err">{{ err }}</p>
</template>

<style scoped>
.spacer { flex: 1; }
</style>
