<script setup>
// Студия, шаг «Готово»: пресеты звука. «Применить к треку» — пресет на этот трек сейчас (новая
// версия «<трек> · <пресет>»); «сохранить правки как пресет» — правки «весь трек» из «Правок трека»
// (движок, эффекты, педали на дорожки) + финал другого пресета → свой пресет для любых треков.
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { useInserts } from '../composables/useInserts.js'
import VSelect from '../VSelect.vue'
import { presetFromEdits, withLevels } from '../soundPresets.js'
import { placeLabel } from '../mixDesk.js'

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
const levels = ref({})         // громкость записей выбранного пресета на это применение {индекс: дБ}

const options = computed(() => presets.value.map((p) => ({ value: p.id, label: p.name })))
const finalOptions = computed(() => [{ value: 0, label: t('preset.final.none') },
  ...presets.value.filter((p) => (p.final || []).length || p.target_lufs != null).map((p) => ({ value: p.id, label: t('preset.final.of', { name: p.name }) }))])
const fromEdits = computed(() => presetFromEdits(inserts.appliedFor(props.job.id)))
const finalSteps = computed(() => (presets.value.find((p) => p.id === finalFrom.value) || {}).final || [])
const current = computed(() => presets.value.find((p) => p.id === presetId.value) || null)
const changed = computed(() => Object.keys(levels.value).length > 0)
const stemName = (s) => t('studio.dsp.target.' + s)
// подпись записи пресета: дорожки и что с ними (для ползунка громкости)
const specLabel = (sp) => (sp.stems || []).map(stemName).join(', ') +
  (sp.engine ? ' · ' + sp.engine.map((b) => b.type).join(' → ') : sp.chain ? ' · ' + sp.chain : sp.steps ? ' · ' + t('pedals')
    : sp.place ? ' · ' + t('mix.place') + ' ' + placeLabel(sp.place) : '')
const levelOf = (i) => (i in levels.value ? levels.value[i] : (current.value.specs[i].db || 0))
function setLevel(i, v) {
  const base = current.value.specs[i].db || 0
  const next = { ...levels.value }
  if (Number(v) === base) delete next[i]
  else next[i] = Number(v)
  levels.value = next
}
watch(presetId, () => { levels.value = {} })
const finalPreset = computed(() => presets.value.find((p) => p.id === finalFrom.value) || {})
// мастер пресета: свой из «Правок трека», иначе — как у пресета, чей финал взят
const masterOut = computed(() => (fromEdits.value.master.length ? fromEdits.value.master : finalPreset.value.master || []))
const canSave = computed(() => !busy.value && name.value.trim() && (fromEdits.value.specs.length || finalSteps.value.length ||
  masterOut.value.length || finalPreset.value.target_lufs != null))

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
    // громкость поправлена — применяется копия пресета, сам пресет не меняется
    const child = changed.value ? await api.applySoundPresetWith(jobId, withLevels(p, levels.value))
      : await api.applySoundPreset(jobId, p.id)
    msg.value = t('preset.applied', { name: p.name, id: child })
    emit('applied')
  } catch (e) { msg.value = ''; err.value = String(e) } finally { busy.value = '' }
}

// «сохранить как мой»: копия выбранного пресета с поправленной громкостью — новый свой пресет
async function saveLevels() {
  const p = current.value
  if (!p) return
  busy.value = 'save'
  err.value = ''
  try {
    const c = withLevels(p, levels.value)    // свой пресет: без id/slug/builtin встроенного
    const saved = await api.soundPresetCreate({ name: (p.name + ' · ' + t('preset.mine')).slice(0, 80), note: c.note || '',
      specs: c.specs, final: c.final, master: c.master || [], parts: c.parts || [], target_lufs: c.target_lufs ?? null,
      reference_job_id: c.reference_job_id || 0 })
    msg.value = t('preset.saved', { name: saved.name })
    await load()
    presetId.value = saved.id
  } catch (e) { err.value = String(e) } finally { busy.value = '' }
}

async function save() {
  busy.value = 'save'
  err.value = ''
  try {
    const p = await api.soundPresetCreate({ name: name.value.trim(), note: note.value.trim(),
      specs: fromEdits.value.specs, final: JSON.parse(JSON.stringify(finalSteps.value)),
      master: JSON.parse(JSON.stringify(masterOut.value)),
      // «финал как у X» — с его целевой громкостью: у встроенных громкость вынесена из финала в цель
      target_lufs: finalPreset.value.target_lufs ?? null })
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
  <p v-if="current" class="muted studio-box-hint">{{ current.note }}</p>
  <div v-if="current && (current.specs || []).length" class="dsp-params preset-levels">
    <label v-for="(sp, i) in current.specs" :key="i" :title="t('preset.level.tip')">
      <span class="dsp-plabel">{{ specLabel(sp) }}</span>
      <input type="range" min="-24" max="24" step="0.5" :value="levelOf(i)" :disabled="!!busy"
             @change="(e) => setLevel(i, e.target.value)" />
      <span class="dsp-pval">{{ levelOf(i) > 0 ? '+' : '' }}{{ levelOf(i) }} {{ t('studio.inserts.dbUnit') }}</span>
    </label>
  </div>
  <div v-if="changed" class="dsp-row">
    <span class="muted">{{ t('preset.level.changed') }}</span>
    <button class="ghost small-btn" :disabled="!!busy" @click="levels = {}">{{ t('preset.level.reset') }}</button>
    <button class="ghost small-btn" :disabled="!!busy" @click="saveLevels">{{ t('preset.level.save') }}</button>
  </div>
  <div v-if="saveOpen" class="preset-save">
    <p class="muted">{{ t('preset.save.count', { n: fromEdits.specs.length, m: fromEdits.skipped }) }}
      <span v-if="fromEdits.master.length"> · {{ t('mix.master.inPreset') }}</span></p>
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
