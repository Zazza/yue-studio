<script setup>
// Студия, шаг «Готово»: мастер — цепочка движка на весь микс с правками, считается на воркере (склейка
// шины, ограничитель по истинному пику, громкость к цели LUFS). «▶ стало / ▶ было» — на окне микса без
// мастера; «в трек» — запись мастера в «Правках трека» (одна, новая заменяет), пересборка, строка громкости.
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { useInserts } from '../composables/useInserts.js'
import { usePlayer } from '../composables/usePlayer.js'
import { useWindowPlay } from '../composables/useWindowPlay.js'
import ChainEditor from './ChainEditor.vue'
import BLOCKS from '../fxBlocks.json'
import { fxPresets } from '../fxPresets.js'
import { fromWorkerChain, toWorkerChain } from '../fxChain.js'
import { previewWindow } from '../trackDesk.js'
import { isMasterRecord, masterChain, masterLine, premasterFile } from '../mixDesk.js'

const props = defineProps({
  job: { type: Object, required: true },
  sel: { type: Object, default: null },
  cursor: { type: Number, default: 0 },
})
const emit = defineEmits(['applied'])
const { t, locale } = useI18n()
const inserts = useInserts()
const { toggleArtifact, playBtn } = usePlayer()
const before = useWindowPlay('master-before', () => t('instr.before'))

const MASTERS = fxPresets.filter((p) => (p.stems || []).includes('master'))
const tr = (lbl) => (lbl && (lbl[locale.value] || lbl.ru)) || ''
const presetId = ref('')
const chain = ref([])
const target = ref(-14)          // цель LUFS; 0 — громкость не менять
const ceiling = ref(-1)          // потолок, dBTP
const limiter = ref(true)        // ограничитель пиков в конце (без цели — только потолок)
const busy = ref('')
const msg = ref('')
const err = ref('')

const current = computed(() => inserts.appliedFor(props.job.id).find((it) => isMasterRecord(it) && !it.off) || null)
// вход мастера: микс с правками без мастера (мастер уже в треке — его копия до мастера), правок нет — оригинал
const inputFile = computed(() => {
  const mix = inserts.mixFile(props.job.id)
  if (!mix) return ''
  return current.value ? premasterFile(mix) : mix
})
// громкость после мастера — по метрикам последней пересборки (любая правка с мастером в треке её обновляет);
// после перезапуска студии — из списка вариантов трека
const line = computed(() => (current.value ? masterLine(inserts.mixMetrics(props.job.id)) : ''))
async function loadLine() {
  const mix = inserts.mixFile(props.job.id)
  if (!current.value || !mix || inserts.mixMetrics(props.job.id)) return
  try {
    const v = ((await api.dspVariants(props.job.id)) || []).find((x) => x.file === mix)
    if (v) inserts.setMixMetrics(props.job.id, v.metrics)
  } catch { /* список вариантов не пришёл — строки просто нет */ }
}
onMounted(loadLine)
watch(() => props.job.id, loadLine)
const listenWin = computed(() => previewWindow(props.sel, props.cursor, props.job.duration_sec))
const workerChain = () => masterChain(toWorkerChain(chain.value), { target: target.value, ceiling: ceiling.value, limiter: limiter.value })
const ready = computed(() => !busy.value && workerChain().length > 0 && !inserts.isBuilding(props.job.id))

function fromChain(c) {
  // ограничитель остаётся в цепочке на своём месте и со всеми параметрами; цель и потолок — ползунки над ней
  chain.value = fromWorkerChain(c, BLOCKS)
  const lim = c.find((b) => b.type === 'limiter')
  limiter.value = !!lim
  target.value = lim ? Number(lim.target_lufs) || 0 : 0
  ceiling.value = lim && lim.ceiling_db != null ? Number(lim.ceiling_db) : -1
}
function pick(p) {
  presetId.value = p.id
  fromChain(p.chain)
}
if (MASTERS[0]) pick(MASTERS[0])

// «изменить» из «Правок трека»: цепочка записи мастера в редактор
function editRecord(rec) {
  presetId.value = ''
  msg.value = ''
  err.value = ''
  fromChain(rec.engine || [])
}
defineExpose({ editRecord })

async function playAfter() {
  err.value = ''
  const jobId = props.job.id
  const req = { source: 'mix', chain: workerChain(), output: 'mix', preview: true, ...listenWin.value,
    ...(inputFile.value ? { file: inputFile.value } : {}) }
  await toggleArtifact('master-after', t('instr.after'), async () => {
    busy.value = 'preview'
    try {
      const r = await api.applyFx(jobId, req)
      await api.playFile(jobId, r.file, r.duration_sec || 15)
    } catch (e) { err.value = String(e); throw e } finally { busy.value = '' }
  })
}

function playBefore() {
  const w = listenWin.value
  before.play({ jobId: props.job.id, file: inputFile.value || props.job.audio_file, dur: props.job.duration_sec, from: w.from, to: w.to })
}

async function apply() {
  err.value = ''
  msg.value = ''
  const jobId = props.job.id
  const p = MASTERS.find((x) => x.id === presetId.value)
  const label = t('mix.master.label', { name: p ? tr(p.name) : t('desk.own') })
  const c = workerChain()
  busy.value = 'apply'
  try {
    await inserts.setMaster(jobId, { chain: c, label })
    msg.value = t('mix.master.applied')
    emit('applied')
  } catch (e) { err.value = String(e) } finally { busy.value = '' }
}
</script>

<template>
  <div class="voice-presets">
    <span class="muted">{{ t('instr.presets') }}</span>
    <button v-for="p in MASTERS" :key="p.id" class="ghost small-btn" :class="{ on: p.id === presetId }"
            :title="tr(p.note)" @click="pick(p)">{{ tr(p.name) }}</button>
  </div>
  <div class="dsp-row">
    <label class="muted" :title="t('mix.target.tip')">{{ t('mix.target') }}
      <input type="range" min="-24" max="-6" step="0.5" :value="target || -14" :disabled="!target || !!busy"
             @change="target = Number($event.target.value)" />
      {{ target ? target + ' LUFS' : t('mix.target.off') }}</label>
    <label class="muted"><input type="checkbox" :checked="!!target" :disabled="!!busy"
             @change="target = $event.target.checked ? -14 : 0; if (target) limiter = true" /> {{ t('mix.target.on') }}</label>
    <label class="muted" :title="t('mix.limiter.tip')"><input v-model="limiter" type="checkbox" :disabled="!!busy || !!target" /> {{ t('mix.limiter') }}</label>
    <label class="muted" :title="t('mix.ceiling.tip')">{{ t('mix.ceiling') }}
      <input type="range" min="-3" max="-0.1" step="0.1" :value="ceiling" :disabled="!!busy || !limiter"
             @change="ceiling = Number($event.target.value)" /> {{ ceiling }} dBTP</label>
  </div>
  <details>
    <summary class="muted">{{ t('mix.master.edit') }}</summary>
    <ChainEditor v-model="chain" :busy="busy" />
  </details>
  <div class="dsp-row">
    <button class="ghost small-btn" :disabled="!ready" :title="t('mix.master.after.tip')" @click="playAfter">
      {{ busy === 'preview' ? t('instr.busy') : playBtn('master-after') + ' ' + t('instr.after') }}</button>
    <button class="ghost small-btn" @click="playBefore">{{ playBtn('master-before') }} {{ t('instr.before') }}</button>
    <span class="muted">{{ t('desk.listen', { from: listenWin.from.toFixed(1), to: listenWin.to.toFixed(1) }) }}</span>
    <span class="spacer"></span>
    <span v-if="current" class="muted">{{ t('mix.master.has', { name: current.label }) }}</span>
    <button class="primary small" :disabled="!ready" :title="t('mix.master.apply.tip')" @click="apply">
      {{ busy === 'apply' ? t('instr.busy') : current ? t('desk.replace') : t('desk.apply') }}</button>
  </div>
  <p v-if="line" class="muted">{{ t('mix.master.line', { line }) }}</p>
  <p v-if="msg" class="muted">{{ msg }}</p>
  <p v-if="err" class="err">{{ err }}</p>
</template>

<style scoped>
.spacer { flex: 1; }
</style>
