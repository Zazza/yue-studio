<script setup>
// Пресеты звука у трека в карточке: ждёт очереди / применяется / → версия #N / ошибка с повтором.
import { computed } from 'vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { presetLine } from '../soundPresets.js'

const props = defineProps({
  job: { type: Object, required: true },
  names: { type: Object, default: () => ({}) },   // id пресета → название
})
const emit = defineEmits(['changed'])
const { t } = useI18n()

const lines = computed(() => (props.job.sound_presets || []).map((st) => ({
  id: st.id, ...presetLine(st, props.names[st.id] || '#' + st.id, t),
})))

async function retry(id) {
  try { await api.soundPresetRetry(props.job.id, id) } catch { /* статус обновится со списком треков */ }
  emit('changed')
}
</script>

<template>
  <div v-if="lines.length" class="job-presets">
    <span v-for="l in lines" :key="l.id" class="muted job-preset">
      {{ l.icon }} {{ l.text }}
      <button v-if="l.retry" class="ghost small-btn" :title="t('preset.retry.tip')" @click="retry(l.id)">↻</button>
    </span>
  </div>
</template>

<style scoped>
.job-presets { display: flex; flex-direction: column; gap: 2px; margin: 4px 0; font-size: 12px; }
</style>
