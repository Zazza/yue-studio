<script setup>
// Страница настроек: воркер + Ollama + пути воркера.
import { ref, computed, onMounted } from 'vue'
import { api } from '../api.js'
import VSelect from '../VSelect.vue'

const serverURL = defineModel('serverURL')
const emit = defineEmits(['close', 'saved'])

const ollamaURL = ref('')
const ollamaModel = ref('')
const info = ref(null)      // data_dir, whisper_py — только показать
const models = ref([])      // модели, установленные в Ollama
const err = ref('')
const saved = ref(false)
const workerProbe = ref('') // статус проверки воркера
const ollamaProbe = ref('') // статус проверки Ollama

const modelOptions = computed(() => {
  const opts = models.value.map((m) => ({ value: m, label: m }))
  if (ollamaModel.value && !models.value.includes(ollamaModel.value)) {
    opts.unshift({ value: ollamaModel.value, label: ollamaModel.value + ' (не в списке)' })
  }
  return opts
})

onMounted(async () => {
  try {
    const h = await api.status()
    workerProbe.value = h ? 'воркер: доступен' + (h.model_loaded ? ' (модель в памяти)' : '') : 'воркер: недоступен'
  } catch {
    workerProbe.value = 'воркер: недоступен'
  }
  try {
    const c = await api.workerConfig()
    ollamaURL.value = c.ollama_url || ''
    ollamaModel.value = c.ollama_model || ''
    info.value = c
    await checkOllama()
  } catch {
    info.value = null
  }
})

async function checkOllama() {
  ollamaProbe.value = ''
  try {
    const m = await api.ollamaModels(ollamaURL.value.trim())
    models.value = m.models || []
    ollamaProbe.value = `ollama: доступен (${models.value.length} модел.)`
  } catch (e) {
    models.value = []
    ollamaProbe.value = 'ollama: недоступен — ' + e
  }
}

async function save() {
  err.value = ''
  saved.value = false
  try {
    const url = serverURL.value.trim()
    await api.setServerURL(url)
    localStorage.setItem('yue_server', url)
    try {
      await api.setWorkerConfig({
        ollama_url: ollamaURL.value.trim(),
        ollama_model: ollamaModel.value.trim(),
      })
      const c = await api.workerConfig()
      ollamaURL.value = c.ollama_url || ''
      ollamaModel.value = c.ollama_model || ''
      info.value = c
    } catch (e) {
      err.value = 'Ollama-настройки не применены (воркер старой версии или недоступен): ' + e
    }
    saved.value = true
    emit('saved')
  } catch (e) {
    err.value = String(e)
  }
}
</script>

<template>
  <main class="settings-page">
    <section class="panel">
      <h2>Настройки</h2>

      <h3 class="set-h">Подключение к воркеру</h3>
      <div class="set-row">
        <span class="set-label">Адрес воркера</span>
        <input v-model="serverURL" placeholder="http://localhost:8091" @keyup.enter="save" />
      </div>
      <div class="set-hint muted">{{ workerProbe }}</div>

      <h3 class="set-h">Ollama <span class="muted">(копайтер стихов, перевод стиля)</span></h3>
      <div class="set-row">
        <span class="set-label">Сервер</span>
        <input v-model="ollamaURL" placeholder="http://127.0.0.1:11434/api/chat" @change="checkOllama" @keyup.enter="save" />
        <button class="ghost" @click="checkOllama">Проверить</button>
      </div>
      <div class="set-row">
        <span class="set-label">Модель</span>
        <VSelect v-model="ollamaModel" :options="modelOptions" placeholder="— список недоступен —" />
        <button class="ghost" @click="checkOllama" title="Перечитать список моделей из Ollama">Обновить</button>
      </div>
      <div class="set-hint muted">{{ ollamaProbe }}</div>

      <h3 class="set-h">Воркер <span class="muted">(только для информации)</span></h3>
      <div v-if="info" class="set-info muted">
        папка данных: {{ info.data_dir }}<br />
        whisper: {{ info.whisper_available ? info.whisper_py : 'не найден (тексты треков недоступны)' }}
      </div>
      <div v-else class="set-info muted">воркер недоступен — Ollama и пути не показать</div>

      <div v-if="err" class="error">{{ err }}</div>
      <div v-if="saved" class="ok">сохранено</div>
      <div class="set-actions">
        <button @click="save">Сохранить</button>
        <button class="ghost" @click="emit('close')">Вернуться</button>
      </div>
    </section>
  </main>
</template>
