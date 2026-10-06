<script setup>
// Страница настроек: воркер + Ollama + пути воркера.
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from '../i18n/index.js'
const { t } = useI18n()
import { api } from '../api.js'
import VSelect from '../VSelect.vue'

const serverURL = defineModel('serverURL')
const emit = defineEmits(['close', 'saved'])

// модалка: Esc закрывает
const onKey = (e) => { if (e.key === 'Escape') emit('close') }
onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))

const ollamaURL = ref('')
const ollamaModel = ref('')
const info = ref(null)      // data_dir, whisper_py — только показать
const models = ref([])      // модели, установленные в Ollama
const err = ref('')
const saved = ref(false)
const workerProbe = ref('') // статус проверки воркера
const ollamaProbe = ref('') // статус проверки Ollama
// у треков «без голоса» сразу делать дорожки — метка, если в инструментал пролез голос
const autoStems = ref(true)
const stemsModel = ref('htdemucs')      // разделение на дорожки: htdemucs | roformer
const roformerOk = ref(false)           // окружение RoFormer установлено на воркере

const stemsOptions = computed(() => [
  { value: 'htdemucs', label: t('settings.stemsModel.fast') },
  { value: 'roformer', label: t('settings.stemsModel.clean'), disabled: !roformerOk.value },
])
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
    autoStems.value = c.auto_stems_instrumental !== false
    stemsModel.value = c.stems_pref || 'htdemucs'
    roformerOk.value = !!c.roformer_available
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
        auto_stems_instrumental: autoStems.value,
        stems_model: stemsModel.value,
      })
      const c = await api.workerConfig()
      ollamaURL.value = c.ollama_url || ''
      ollamaModel.value = c.ollama_model || ''
      // сервер мог смениться — доступность RoFormer и выбор берём из его ответа
      stemsModel.value = c.stems_pref || 'htdemucs'
      roformerOk.value = !!c.roformer_available
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
  <div class="modal-backdrop page-backdrop" @click.self="emit('close')">
    <section class="panel page-modal">
      <div class="page-modal-head">
        <h2>{{ t('settings.title') }}</h2>
        <button class="ghost icon" :title="t('common.close')" @click="emit('close')"><AppIcon name="x" /></button>
      </div>
      <div class="page-modal-body">

      <h3 class="set-h">{{ t('settings.worker') }}</h3>
      <div class="set-row">
        <span class="set-label">{{ t('settings.worker.url') }}</span>
        <input v-model="serverURL" placeholder="http://localhost:8091" @keyup.enter="save" />
      </div>
      <div class="set-hint muted">{{ workerProbe }}</div>

      <h3 class="set-h">{{ t('settings.ollama') }} <span class="muted">{{ t('settings.ollama.sub') }}</span></h3>
      <div class="set-row">
        <span class="set-label">{{ t('settings.server') }}</span>
        <input v-model="ollamaURL" placeholder="http://127.0.0.1:11434/api/chat" @change="checkOllama" @keyup.enter="save" />
        <button class="ghost" @click="checkOllama">{{ t('common.check') }}</button>
      </div>
      <div class="set-row">
        <span class="set-label">{{ t('settings.model') }}</span>
        <VSelect v-model="ollamaModel" :options="modelOptions" :placeholder="t('settings.model.ph')" />
        <button class="ghost" @click="checkOllama" :title="t('common.refresh')">{{ t('common.refresh') }}</button>
      </div>
      <div class="set-hint muted">{{ ollamaProbe }}</div>

      <h3 class="set-h">{{ t('settings.checks') }}</h3>
      <label class="set-row set-check">
        <input type="checkbox" v-model="autoStems" />
        <span>{{ t('settings.autoStems') }}</span>
      </label>
      <div class="set-hint muted">{{ t('settings.autoStems.hint') }}</div>

      <div class="set-row">
        <span>{{ t('settings.stemsModel') }}</span>
        <VSelect v-model="stemsModel" :options="stemsOptions" />
      </div>
      <div class="set-hint muted">{{ roformerOk ? t('settings.stemsModel.hint') : t('settings.stemsModel.missing') }}</div>

      <h3 class="set-h">{{ t('settings.info') }} <span class="muted">{{ t('settings.info.readonly') }}</span></h3>
      <div v-if="info" class="set-info muted">
        папка данных: {{ info.data_dir }}<br />
        whisper: {{ info.whisper_available ? info.whisper_py : 'не найден (тексты треков недоступны)' }}<br />
        <template v-if="info.stems_model">{{ t('settings.stemsModel') }}: {{ info.stems_model === 'bs-roformer-sw' ? 'BS-Roformer-SW' : t('settings.stemsModel.demucs') }}</template>
      </div>
      <div v-else class="set-info muted">воркер недоступен — Ollama и пути не показать</div>

      <div v-if="err" class="error">{{ err }}</div>
      <div v-if="saved" class="ok">сохранено</div>
      <div class="set-actions">
        <button @click="save"><AppIcon name="save" /> {{ t('common.save') }}</button>
      </div>
      </div>
    </section>
  </div>
</template>
