<script setup>
// Студия, шаг «Звук»: синт-партия по аккордам трека — поверх трека, ничего не вычитая (разделение не нужно).
// «Разобрать аккорды» — аккорды и секции плана по тактам звука (chord_grid); готовый синт (старые машины +
// эффекты), стиль партии, октава, секции, громкость; «▶ стало / ▶ было» — кусок с партией и без; «в трек» —
// правка в «Правках трека» (запись движка с add на «весь трек как вход»). Ноты — synthPart.js.
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { useInserts } from '../composables/useInserts.js'
import { usePlayer } from '../composables/usePlayer.js'
import { useWindowPlay } from '../composables/useWindowPlay.js'
import VSelect from '../VSelect.vue'
import ChainEditor from './ChainEditor.vue'
import BLOCKS from '../fxBlocks.json'
import { fxPresets } from '../fxPresets.js'
import { fromWorkerChain, toWorkerChain } from '../fxChain.js'
import { applyEngine } from '../engineRun.js'
import { partNotes } from '../synthPart.js'
import { previewWindow, applyWindow } from '../trackDesk.js'

const props = defineProps({
  job: { type: Object, required: true },
  sel: { type: Object, default: null },
  cursor: { type: Number, default: 0 },
})
const emit = defineEmits(['applied'])
const { t, locale } = useI18n()
const inserts = useInserts()
const { toggleArtifact, playBtn } = usePlayer()
const before = useWindowPlay('synth-before', () => t('instr.before'))

const SYNTHS = fxPresets.filter((p) => (p.stems || []).includes('synth'))
const STYLES = ['pad', 'arp', 'pulse', 'drone']
const grid = ref(null)            // {bpm, bars}
const sections = ref([])          // выбранные секции
const presetId = ref(SYNTHS[0]?.id || '')
const style = ref(SYNTHS[0]?.style || 'pad')
const octave = ref(SYNTHS[0]?.octave || 0)
const chain = ref(SYNTHS[0] ? fromWorkerChain(SYNTHS[0].chain, BLOCKS) : [])
const busy = ref('')
const msg = ref('')
const err = ref('')

const tr = (lbl) => (lbl && (lbl[locale.value] || lbl.ru)) || ''
const preset = computed(() => SYNTHS.find((p) => p.id === presetId.value))
const presetOptions = SYNTHS.map((p) => ({ value: p.id, label: tr(p.name) }))
const styleOptions = computed(() => STYLES.map((s) => ({ value: s, label: t('synth.style.' + s) })))
// секции плана по порядку появления, без повторов
const allSections = computed(() => [...new Set(((grid.value && grid.value.bars) || []).map((b) => b.section).filter(Boolean))])
const notes = computed(() => (grid.value ? partNotes(grid.value.bars, { style: style.value, octave: octave.value, sections: sections.value }) : []))
// громкость партии относительно трека — это rel_db блока synth: верхний ползунок и крутилка в редакторе цепочки —
// один параметр, а не два (кросс-ревью: второй ничего не делал)
const db = computed({
  get: () => { const b = chain.value.find((x) => x.type === 'synth'); return b ? b.params.rel_db : -10 },
  set: (v) => { chain.value = chain.value.map((b) => (b.type === 'synth' ? { ...b, params: { ...b.params, rel_db: Number(v) } } : b)) },
})
const listenWin = computed(() => previewWindow(props.sel, props.cursor, props.job.duration_sec))
const target = computed(() => applyWindow(props.sel))
const ready = computed(() => !busy.value && notes.value.length > 0 && chain.value.some((b) => b.type === 'synth' && b.on))

watch(presetId, () => {
  const p = preset.value
  if (!p) return
  chain.value = fromWorkerChain(p.chain, BLOCKS)
  style.value = p.style || 'pad'
  octave.value = p.octave || 0
})
watch(() => props.job.id, () => { grid.value = null; sections.value = []; msg.value = '' })
onMounted(() => { msg.value = '' })

async function loadGrid() {
  busy.value = 'grid'
  err.value = ''
  try {
    grid.value = await api.chordGrid(props.job.id)
    sections.value = [...allSections.value]
    msg.value = t('synth.grid.done', { bars: grid.value.bars.length, bpm: grid.value.bpm })
  } catch (e) { err.value = String(e) } finally { busy.value = '' }
}

function toggleSection(s) {
  sections.value = sections.value.includes(s) ? sections.value.filter((x) => x !== s) : [...sections.value, s]
}

// цепочка воркеру: ноты партии — в блок synth (секунды трека; воркер сдвигает их на окно превью); громкость —
// относительно трека там, где играет синт (rel_db): по пику пэд тонул в плотном припеве (прослушивание #681)
function workerChain() {
  return toWorkerChain(chain.value).map((b) => (b.type === 'synth' ? { ...b, notes: notes.value } : b))
}

async function playAfter() {
  err.value = ''
  const jobId = props.job.id
  const req = { source: 'mix', chain: workerChain(), output: 'mix', preview: true, add: true, ...listenWin.value }
  await toggleArtifact('synth-after', t('instr.after'), async () => {
    busy.value = 'preview'
    try {
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
  const label = t('synth.label', { name: tr(preset.value?.name) || t('synth.own'), style: t('synth.style.' + style.value) })
  const snap = { jobId, dur: props.job.duration_sec, src: 'mix', ...target.value, chain: workerChain(), label }
  busy.value = 'apply'
  try {
    const rec = await applyEngine(api, snap, { oldMsg: t('instr.engine.old') })
    await inserts.addStemEngine(jobId, { ...rec, add: true })
    msg.value = t('synth.applied')
    emit('applied')
  } catch (e) { err.value = String(e) } finally { busy.value = '' }
}
</script>

<template>
  <div class="dsp-row">
    <button class="ghost small-btn" :disabled="!!busy" :title="t('synth.grid.tip')" @click="loadGrid">
      {{ busy === 'grid' ? t('instr.busy') : grid ? t('synth.grid.again') : t('synth.grid') }}</button>
    <span v-if="msg" class="muted">{{ msg }}</span>
  </div>
  <template v-if="grid">
    <div class="voice-presets">
      <span class="muted">{{ t('synth.sections') }}</span>
      <button v-for="s in allSections" :key="s" class="toggle" :class="{ on: sections.includes(s) }" @click="toggleSection(s)">{{ s }}</button>
    </div>
    <div class="dsp-row">
      <VSelect v-model="presetId" :options="presetOptions" style="max-width: 260px" />
      <VSelect v-model="style" :options="styleOptions" style="max-width: 170px" />
      <label class="muted">{{ t('synth.octave') }}
        <input v-model.number="octave" type="number" min="-2" max="2" step="1" style="width: 3.5em" /></label>
      <label class="muted">{{ t('synth.level') }}
        <input v-model.number="db" type="range" min="-30" max="6" step="1" :title="t('synth.level.tip')" /> {{ db }} {{ t('studio.inserts.dbUnit') }}</label>
    </div>
    <p v-if="preset" class="muted studio-box-hint">{{ tr(preset.note) }}</p>
    <details>
      <summary class="muted">{{ t('synth.edit') }}</summary>
      <ChainEditor v-model="chain" :busy="busy" />
    </details>
    <div class="dsp-row">
      <button class="ghost small-btn" :disabled="!ready" :title="t('instr.after.tip')" @click="playAfter">
        {{ busy === 'preview' ? t('instr.busy') : playBtn('synth-after') + ' ' + t('instr.after') }}</button>
      <button class="ghost small-btn" @click="playBefore">{{ playBtn('synth-before') }} {{ t('instr.before') }}</button>
      <span class="muted">{{ t('desk.listen', { from: listenWin.from.toFixed(1), to: listenWin.to.toFixed(1) }) }} · {{ t('synth.notes', { n: notes.length }) }}</span>
      <span class="spacer"></span>
      <button class="primary small" :disabled="!ready" :title="t('synth.apply.tip')" @click="apply">
        {{ busy === 'apply' ? t('instr.busy') : t('desk.apply') }}</button>
    </div>
  </template>
  <p v-if="err" class="err">{{ err }}</p>
</template>

<style scoped>
.spacer { flex: 1; }
</style>
