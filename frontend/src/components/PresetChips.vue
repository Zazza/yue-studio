<script setup>
// Выбор пресетов звука в форме нового трека: чипы, до трёх. Пресеты применит приложение, когда
// трек будет готов (версии «· пресет» появятся под треком). У черновика не показывается.
import { computed, onMounted, ref } from 'vue'
import VSelect from '../VSelect.vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { togglePreset, presetOptions } from '../soundPresets.js'

const ids = defineModel({ type: Array, default: () => [] })
const { t } = useI18n()
const presets = ref([])
// пресетов около сотни: выбор из списка с поиском по семьям и течениям, выбранные — чипами (до трёх)
const options = computed(() => presetOptions(presets.value).map((o) => (typeof o.value === 'number'
  ? { ...o, disabled: !ids.value.includes(o.value) && ids.value.length >= 3 } : o)))
const picked = computed(() => ids.value.map((id) => presets.value.find((p) => p.id === id)).filter(Boolean))
const pick = ref(null)
function add(id) {
  if (typeof id === 'number' && !ids.value.includes(id)) ids.value = togglePreset(ids.value, id)
  pick.value = null
}

onMounted(async () => {
  try { presets.value = (await api.soundPresets()) || [] } catch { presets.value = []; return }   // старый воркер/сбой — выбор не трогаем
  // выбранный раньше пресет могли удалить — не держать невидимый id (отправка упала бы 422)
  const have = new Set(presets.value.map((p) => p.id))
  if (ids.value.some((id) => !have.has(id))) ids.value = ids.value.filter((id) => have.has(id))
})
</script>

<template>
  <div v-if="presets.length" class="preset-chips">
    <span class="muted" :title="t('preset.form.tip')">{{ t('preset.form') }}</span>
    <VSelect :model-value="pick" :options="options" searchable style="max-width: 320px" @update:model-value="add" />
    <button v-for="p in picked" :key="p.id" class="toggle on" :title="p.note"
            @click="ids = togglePreset(ids, p.id)">{{ p.name }} ×</button>
  </div>
</template>

<style scoped>
.preset-chips { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin: 6px 0; }
</style>
