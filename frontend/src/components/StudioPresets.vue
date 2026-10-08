<script setup>
// Студия, шаг «Готово»: пресеты звука. «Применить к треку» — пресет на этот трек сейчас (новая
// версия «<трек> · <пресет>»); «сохранить правки как пресет» — правки «весь трек» из «Правок трека»
// (движок, эффекты, педали на дорожки) + финал другого пресета → свой пресет для любых треков.
import { computed, onMounted, ref } from 'vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { useInserts } from '../composables/useInserts.js'
import VSelect from '../VSelect.vue'
import { presetFromEdits } from '../soundPresets.js'

const props = defineProps({ job: { type: Object, required: true } })
const emit = defineEmits(['applied'])
const { t } = useI18n()
const inserts = useInserts()

const presets = ref([])
const presetId = ref(null)
const busy = ref('')
const msg = ref('')
const err = ref('')
const saveOpen = ref(false)
const name = ref('')
const note = ref('')
const finalFrom = ref(0)        // 0 — без финала, иначе id пресета, чей финал копируем

const options = computed(() => presets.value.map((p) => ({ value: p.id, label: p.name })))
const finalOptions = computed(() => [{ value: 0, label: t('preset.final.none') },
  ...presets.value.filter((p) => (p.final || []).length).map((p) => ({ value: p.id, label: t('preset.final.of', { name: p.name }) }))])
const fromEdits = computed(() => presetFromEdits(inserts.appliedFor(props.job.id)))
const finalSteps = computed(() => (presets.value.find((p) => p.id === finalFrom.value) || {}).final || [])
const canSave = computed(() => !busy.value && name.value.trim() && (fromEdits.value.specs.length || finalSteps.value.length))

onMounted(load)
async function load() {
  try { presets.value = (await api.soundPresets()) || [] } catch (e) { err.value = String(e) }
  if (presetId.value == null && presets.value.length) presetId.value = presets.value[0].id
}

async function apply() {
  const jobId = props.job.id
  const p = presets.value.find((x) => x.id === presetId.value)
  if (!p) return
  busy.value = 'apply'
  err.value = ''
  msg.value = t('preset.applying', { name: p.name })
  try {
    const child = await api.applySoundPreset(jobId, p.id)
    msg.value = t('preset.applied', { name: p.name, id: child })
    emit('applied')
  } catch (e) { msg.value = ''; err.value = String(e) } finally { busy.value = '' }
}

async function save() {
  busy.value = 'save'
  err.value = ''
  try {
    const p = await api.soundPresetCreate({ name: name.value.trim(), note: note.value.trim(),
      specs: fromEdits.value.specs, final: JSON.parse(JSON.stringify(finalSteps.value)) })
    msg.value = t('preset.saved', { name: p.name })
    saveOpen.value = false
    name.value = ''
    note.value = ''
    await load()
    presetId.value = p.id
  } catch (e) { err.value = String(e) } finally { busy.value = '' }
}
</script>

<template>
  <div class="dsp-row">
    <VSelect v-model="presetId" :options="options" :disabled="!!busy" style="max-width: 280px" />
    <button class="primary small" :disabled="!!busy || presetId == null" :title="t('preset.apply.tip')" @click="apply">
      {{ busy === 'apply' ? t('instr.busy') : t('preset.apply') }}</button>
    <button class="ghost small-btn" :class="{ on: saveOpen }" :disabled="!!busy" @click="saveOpen = !saveOpen">{{ t('preset.save') }}</button>
  </div>
  <p v-if="presetId != null" class="muted studio-box-hint">{{ (presets.find((p) => p.id === presetId) || {}).note }}</p>
  <div v-if="saveOpen" class="preset-save">
    <p class="muted">{{ t('preset.save.count', { n: fromEdits.specs.length, m: fromEdits.skipped }) }}</p>
    <div class="dsp-row">
      <input v-model="name" maxlength="80" :placeholder="t('preset.save.name')" />
      <input v-model="note" maxlength="500" :placeholder="t('preset.save.note')" class="preset-note" />
    </div>
    <div class="dsp-row">
      <span class="muted">{{ t('preset.save.final') }}</span>
      <VSelect v-model="finalFrom" :options="finalOptions" style="max-width: 280px" />
      <button class="primary small" :disabled="!canSave" @click="save">{{ busy === 'save' ? '…' : t('common.save') }}</button>
    </div>
  </div>
  <p v-if="msg" class="muted">{{ msg }}</p>
  <p v-if="err" class="err">{{ err }}</p>
</template>

<style scoped>
.preset-save { border-left: 2px solid var(--line, #2a2a35); padding-left: 10px; margin: 6px 0; }
.preset-note { flex: 1; min-width: 200px; }
.small-btn.on { border-color: var(--accent, #7aa2f7); }
</style>
