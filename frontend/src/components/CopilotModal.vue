<script setup>
// Копайтер стихов (Ollama на воркере).
import { ref } from 'vue'
import { api } from '../api.js'

const props = defineProps({
  open: Boolean,
  style: String,    // строка стиля для тональности запроса
  example: String,  // текущий стих — образец манеры
  lang: String,     // язык песни
})
const emit = defineEmits(['close', 'insert'])

const theme = ref('')
const useExample = ref(true)
const busy = ref(false)
const text = ref('')
const err = ref('')
const seconds = ref(null)

async function generate() {
  if (!theme.value.trim()) return
  busy.value = true
  err.value = ''
  text.value = ''
  try {
    const r = await api.copilot({
      theme: theme.value,
      style: props.style,
      example: useExample.value ? props.example : '',
      lang: props.lang || 'Russian',
    })
    text.value = r.text
    seconds.value = r.seconds
  } catch (e) {
    err.value = String(e)
  } finally {
    busy.value = false
  }
}

function insert() {
  if (text.value.trim()) emit('insert', text.value)
  emit('close')
}
</script>

<template>
  <div v-if="open" class="modal-backdrop" @click.self="emit('close')">
    <div class="modal cop-modal">
      <div class="modal-head">
        <h2>Копайтер стихов</h2>
        <span class="muted plan-meta">qwen2.5</span>
        <span class="spacer"></span>
        <button class="ghost" @click="emit('close')">✕</button>
      </div>
      <div class="cop-form">
        <input v-model="theme" placeholder="Тема: о чём песня" @keyup.enter="generate" />
        <label class="cop-example">
          <input type="checkbox" v-model="useExample" />
          подражать манере текущего стиха
        </label>
        <button class="primary" :disabled="busy || !theme.trim()" @click="generate">
          {{ busy ? 'пишет… (грузит модель, до минуты)' : 'Написать' }}
        </button>
      </div>
      <p v-if="err" class="error">{{ err }}</p>
      <p v-if="seconds" class="muted">написано за {{ seconds }}s — можно править перед вставкой</p>
      <textarea v-if="text" v-model="text" rows="14" spellcheck="true"></textarea>
      <div class="modal-actions">
        <button class="primary" :disabled="!text.trim()" @click="insert">Вставить в редактор</button>
        <button class="ghost" :disabled="busy" @click="generate">Ещё вариант</button>
      </div>
    </div>
  </div>
</template>
