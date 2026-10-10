<script setup>
// Строка «качаю набор …: часть, файлы, МБ» — пока ставится набор сэмплов (kitInstalling), опрос воркера раз в секунду.
import { onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../api.js'
import { useI18n } from '../i18n/index.js'
import { kitInstalling, kitProgressText } from '../kitProgress.js'

const { t } = useI18n()
const text = ref('')
let timer = null

async function poll() {
  try {
    const p = await api.fxKitProgress()
    text.value = kitProgressText(p, t) || t('kit.progress.start', { name: kitInstalling.value })
  } catch {
    text.value = t('kit.progress.start', { name: kitInstalling.value })   // старый воркер без прогресса — хотя бы имя
  }
}

watch(kitInstalling, (name) => {
  clearInterval(timer)
  text.value = ''
  if (name) { poll(); timer = setInterval(poll, 1000) }
}, { immediate: true })
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <p v-if="kitInstalling" class="muted kit-progress">{{ text }}</p>
</template>
