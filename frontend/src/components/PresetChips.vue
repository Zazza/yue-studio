<script setup>
// Выбор пресетов звука в форме нового трека: чипы, до трёх. Пресеты применит приложение, когда
// трек будет готов (версии «· пресет» появятся под треком). У черновика не показывается.
import { onMounted, ref } from 'vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { togglePreset } from '../soundPresets.js'

const ids = defineModel({ type: Array, default: () => [] })
const { t } = useI18n()
const presets = ref([])

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
    <button v-for="p in presets" :key="p.id" class="toggle" :class="{ on: ids.includes(p.id) }"
            :disabled="!ids.includes(p.id) && ids.length >= 3" :title="p.note"
            @click="ids = togglePreset(ids, p.id)">{{ p.name }}</button>
  </div>
</template>

<style scoped>
.preset-chips { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin: 6px 0; }
</style>
