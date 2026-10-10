<script setup>
// Копайтер стихов (Ollama на воркере): тема + редактируемая инструкция-заготовка,
// собранная из жанра/голоса формы (buildCopilotInstruction).
import { ref, watch } from 'vue'
import { useI18n } from '../i18n/index.js'
const { t } = useI18n()
import { api } from '../api.js'
import { buildCopilotInstruction } from '../copilotPrompt.js'

const props = defineProps({
  open: Boolean,
  style: String,    // строка стиля для тональности запроса
  example: String,  // текущий стих — образец манеры
  lang: String,     // язык песни
  slots: Object,    // слоты формы: genre/vocals/language → заготовка инструкции
})
const emit = defineEmits(['close', 'insert'])

const theme = ref('')
const useExample = ref(true)
const instruction = ref('')
const instructionEdited = ref(false)

// заготовка пересобирается при каждом открытии, пока пользователь не правил руками
watch(() => props.open, (o) => {
  if (o && !instructionEdited.value) instruction.value = buildCopilotInstruction(props.slots || {})
})

function onInstructionInput() { instructionEdited.value = true }

function resetInstruction() {
  instructionEdited.value = false
  instruction.value = buildCopilotInstruction(props.slots || {})
}
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
      instruction: instructionEdited.value ? instruction.value : '',
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
  <div v-if="open" class="modal-backdrop">
    <div class="modal cop-modal">
      <div class="modal-head">
        <h2>{{ t('copilot.title') }}</h2>
        <span class="muted plan-meta">qwen2.5</span>
        <span class="spacer"></span>
        <button class="ghost" @click="emit('close')"><AppIcon name="x" /></button>
      </div>
      <div class="cop-form">
        <textarea v-model="theme" rows="3" :placeholder="t('copilot.theme.ph')"
                  @keyup.ctrl.enter="generate"></textarea>
      </div>
      <details class="cop-instruction">
        <summary>{{ t('copilot.instruction') }} <span class="muted">{{ t('copilot.instruction.sub') }}</span>
          <button v-if="instructionEdited" class="ghost small-btn" :title="t('copilot.instruction.rebuild.tip')" @click.stop="resetInstruction">{{ t('copilot.instruction.rebuild') }}</button>
        </summary>
        <textarea v-model="instruction" rows="5" @input="onInstructionInput"></textarea>
      </details>
      <div class="cop-form">
        <label class="cop-example">
          <input type="checkbox" v-model="useExample" />
          {{ t('copilot.example') }}
        </label>
        <button class="primary" :disabled="busy || !theme.trim()" @click="generate">
          {{ busy ? t('copilot.writing') : t('copilot.write') }}
        </button>
      </div>
      <p v-if="err" class="error">{{ err }}</p>
      <p v-if="seconds" class="muted">{{ t('copilot.done', { sec: seconds }) }}</p>
      <textarea v-if="text" v-model="text" rows="14" spellcheck="true"></textarea>
      <div class="modal-actions">
        <button class="primary" :disabled="!text.trim()" @click="insert">{{ t('copilot.insert') }}</button>
        <button class="ghost" :disabled="busy" @click="generate">{{ t('copilot.more') }}</button>
      </div>
    </div>
  </div>
</template>
