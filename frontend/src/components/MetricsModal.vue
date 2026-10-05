<script setup>
// Метрики трека с дельтой до референса/другой джобы.
import { ref, computed } from 'vue'
import { useI18n } from '../i18n/index.js'
const { t } = useI18n()
import { api } from '../api.js'
import VSelect from '../VSelect.vue'

const props = defineProps({ jobs: { type: Array, default: () => [] } })

const open = ref(false)
const job = ref(null)
const title = ref('')     // подзаголовок, если показываем не исходник (DSP-вариант)
const metrics = ref(null)
const busy = ref(false)
const err = ref('')
const refs = ref([])
const cmpTarget = ref('') // '' | 'ref:<id>' | 'job:<id>'
const cmpMetrics = ref(null)
const cmpBusy = ref(false)
const addingRef = ref(false)

const rows = [
  ['tempo_bpm', 'Темп', 'BPM', 1],
  // громкость по стандарту стримингов (EBU R128): стриминги выравнивают к ~−14 LUFS
  ['lufs', 'Громкость (LUFS)', 'LUFS', 1],
  ['lra', 'Диапазон громкости (LRA)', 'LU', 1],
  ['true_peak_db', 'True peak', 'dBTP', 1],
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

const doneJobs = computed(() =>
  props.jobs.filter((j) => j.status === 'done' && j.id !== (job.value && job.value.id)))

function metricVal(m, key) {
  if (!m) return null
  if (key.startsWith('bands.')) return m.bands ? m.bands[key.slice(6)] : null
  return m[key]
}

function fmtMetric(v, dec, unit) {
  if (v === null || v === undefined) return '—'
  return Number(v).toFixed(dec) + (unit ? ' ' + unit : '')
}

function deltaStr(key, dec) {
  const a = metricVal(metrics.value, key)
  const b = metricVal(cmpMetrics.value, key)
  if (a == null || b == null) return ''
  const d = Number(a) - Number(b)
  return (d > 0 ? '+' : '') + d.toFixed(dec)
}

function refName(id) {
  return String(id).replace(/^\d+-/, '')
}

async function loadRefs() {
  try { refs.value = (await api.references()) || [] } catch {}
}

async function openFor(j, preset) {
  job.value = j
  title.value = ''
  open.value = true
  err.value = ''
  metrics.value = null
  cmpTarget.value = ''
  cmpMetrics.value = null
  if (preset) {
    // вариант DSP: метрики уже известны, сразу сравнение с исходником
    metrics.value = preset.metrics
    title.value = preset.title
    cmpTarget.value = 'job:' + j.id
    await onCmpChange()
    return
  }
  busy.value = true
  try {
    loadRefs()
    metrics.value = await api.analyze(j.id)
  } catch (e) {
    err.value = String(e)
  } finally {
    busy.value = false
  }
}

async function onCmpChange() {
  cmpMetrics.value = null
  if (!cmpTarget.value) return
  cmpBusy.value = true
  try {
    const [kind, id] = cmpTarget.value.split(':')
    if (kind === 'ref') {
      const r = refs.value.find((x) => x.id === id)
      cmpMetrics.value = r ? r.metrics : null
    } else if (kind === 'job') {
      cmpMetrics.value = await api.analyze(Number(id))
    }
  } catch (e) {
    err.value = String(e)
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

defineExpose({ openFor })
</script>

<template>
  <div v-if="open" class="modal-backdrop" @click.self="open = false">
    <div class="modal metrics-modal">
      <div class="modal-head">
        <h2>Метрики эффектов — {{ title || ('#' + (job && job.id) + ' ' + (job && job.title)) }}</h2>
        <span class="spacer"></span>
        <button class="ghost" @click="open = false"><AppIcon name="x" /></button>
      </div>
      <p v-if="busy" class="muted">{{ t('metrics.librosa') }}</p>
      <p v-if="err" class="error">{{ err }}</p>
      <template v-if="metrics">
        <div class="cmp-row">
          <span class="muted">дельта до:</span>
          <VSelect v-model="cmpTarget" style="max-width: 260px"
                   :options="[{ value: '', label: '— нет —' },
                              ...refs.map((r) => ({ value: 'ref:' + r.id, label: 'реф: ' + refName(r.id) })),
                              ...doneJobs.map((jj) => ({ value: 'job:' + jj.id, label: '#' + jj.id + ' ' + jj.title }))]"
                   @update:model-value="onCmpChange()" />
          <button class="ghost" :disabled="addingRef" @click="addReference"><AppIcon name="plus" /> файл-референс…</button>
        </div>
        <p v-if="cmpBusy" class="muted">{{ t('metrics.busy') }}</p>
        <div class="metrics-table">
          <div class="mrow head"><span>метрика</span><span>трек</span><span>сравнение</span><span>Δ</span></div>
          <div v-for="[key, label, unit, dec] in rows" :key="key" class="mrow">
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
