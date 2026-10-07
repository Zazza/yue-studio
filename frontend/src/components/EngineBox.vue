<script setup>
// Блок «Звуковой движок» студии: готовая цепочка движка воркера (педали → усилитель NAM →
// кабинет → пространство) на дорожку в выделении. Запись копится в реестре пересборки
// вместе со вклейками и эффектами ffmpeg (useInserts.addStemEngine) — трек собирается
// с чистого оригинала, звук не сдвигается. Свою цепочку собирают на странице «Инструменты»
// («→ в студию»).
import { computed, onMounted, ref } from 'vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { useInserts } from '../composables/useInserts.js'
import VSelect from '../VSelect.vue'
import { fxPresets } from '../fxPresets.js'
import { missingKits } from '../fxChain.js'

const props = defineProps({
  job: { type: Object, required: true },
  sel: { type: Object, default: null },        // выделение на волне/ролле {from, to}
})
const emit = defineEmits(['applied'])
const { t, locale } = useI18n()
const inserts = useInserts()

const STEMS = ['guitar', 'other', 'piano', 'vocals', 'bass', 'drums', 'kick', 'snare', 'toms', 'hh', 'ride', 'crash']
const PREVIEW_SEC = 10                         // проверка цепочки перед записью — короткий кусок

const presetId = ref(fxPresets[0].id)
const stem = ref('guitar')
const amp = ref('')
const amps = ref([])
const busy = ref(false)
const msg = ref('')

const tr = (lbl) => (lbl && (lbl[locale.value] || lbl.ru)) || ''
const preset = computed(() => fxPresets.find((p) => p.id === presetId.value))
const presetOptions = computed(() => fxPresets.map((p) => ({ value: p.id, label: tr(p.name) })))
const stemOptions = computed(() => STEMS.map((v) => ({ value: v, label: t('studio.dsp.target.' + v) })))
const needsAmp = computed(() => (preset.value?.chain || []).some((b) => b.type === 'amp'))
const ampOptions = computed(() => amps.value.map((a) => ({ value: a.name, label: a.name })))

onMounted(async () => {
  try {
    amps.value = ((await api.fxAssets()) || {}).amps || []
    if (amps.value.length) amp.value = amps.value[0].name
  } catch { amps.value = [] }     // старый воркер — ошибка покажется при применении
})

// цепочка пресета с выбранным захватом в усилителе (пустой model — «выберите захват»)
function chain() {
  return (preset.value?.chain || []).map((b) => (b.type === 'amp' && !b.model ? { ...b, model: amp.value } : { ...b }))
}

async function apply() {
  if (needsAmp.value && !amp.value) { msg.value = t('engine.noAmp'); return }
  const s = props.sel
  const from = s ? s.from : 0
  const to = s ? s.to : 0
  const c = chain()
  // всё, что уйдёт в превью и в запись, — до первого await: пока качается набор, дорожку,
  // пресет и трек могут сменить, а превью и запись должны быть про одно и то же
  const jobId = props.job.id
  const dur = props.job.duration_sec
  const src = stem.value
  const label = t('engine.label', { name: tr(preset.value?.name) })
  busy.value = true
  msg.value = ''
  try {
    // старый воркер превью не знает: запрос ниже создал бы лишний вариант — сначала спросить
    const cfg = await api.workerConfig()
    if (!cfg || !cfg.fx_preview) throw new Error(t('instr.engine.old'))
    // набор сэмплов (замена ударов), которого нет на воркере, — скачать по требованию
    const need = missingKits(c, ((await api.fxAssets()) || {}).kits)
    for (const k of need) { msg.value = t('instr.kit.busy'); await api.installFxKit(k) }
    // запись с цепочкой, которую воркер не примет (движок выключен, нет захвата, нет дорожки),
    // роняла бы каждую следующую пересборку — сначала короткое превью того же
    const pTo = to > from ? Math.min(to, from + PREVIEW_SEC) : Math.min(from + PREVIEW_SEC, dur || from + PREVIEW_SEC)
    const r = await api.applyFx(jobId, { source: src, chain: c, from, to: pTo, output: 'solo', preview: true })
    // старый воркер превью не знает и делает вариант — такую запись пересборка отвергнет
    if (!r || !String(r.file || '').startsWith('preview-fx-')) throw new Error(t('instr.engine.old'))
    await inserts.addStemEngine(jobId, { stem: src, chain: c, from, to, label })
    emit('applied')
    msg.value = t('engine.applied')
  } catch (e) { msg.value = String(e) } finally { busy.value = false }
}
</script>

<template>
  <div class="dsp-row">
    <VSelect v-model="presetId" :options="presetOptions" :disabled="busy" style="max-width: 230px" />
    <VSelect v-model="stem" :options="stemOptions" :disabled="busy" :title="t('engine.stem.tip')" style="max-width: 150px" />
    <VSelect v-if="needsAmp" v-model="amp" :disabled="busy" :options="ampOptions" :placeholder="t('instr.asset.none')" style="max-width: 230px" />
    <button class="primary small" :disabled="busy" :title="t('engine.apply.tip')" @click="apply">
      {{ busy ? t('instr.busy') : t('engine.apply') }}
    </button>
  </div>
  <p v-if="preset" class="muted studio-box-hint">{{ tr(preset.note) }}</p>
  <p class="muted studio-box-hint">{{ sel ? t('engine.window', { from: sel.from.toFixed(1), to: sel.to.toFixed(1) }) : t('engine.whole') }}</p>
  <p v-if="msg" class="muted">{{ msg }}</p>
</template>
