<script setup>
// Редактор плана: ABC-партитура до рендера (план, из джобы, из трека, из профиля).
import { useI18n } from '../i18n/index.js'

const { t } = useI18n()

defineProps({
  open: Boolean,
  busy: Boolean,
  err: String,
  info: Object, // {seed, seconds, truncated, fromJob, fromTrack, fromProfile}
})
const emit = defineEmits(['close', 'use', 'new-plan', 'from-track'])
const abc = defineModel('abc', { type: String, default: '' })
</script>

<template>
  <div v-if="open" class="modal-backdrop" @click.self="emit('close')">
    <div class="modal">
      <div class="modal-head">
        <h2>{{ t('plan.title') }} <span class="muted">{{ t('plan.advanced') }}</span></h2>
        <span v-if="info" class="muted plan-meta">
          seed {{ info.seed }}<template v-if="info.seconds"> · {{ info.seconds }}s</template><template v-if="info.fromJob"> · из джобы #{{ info.fromJob }}</template><template v-if="info.truncated"> · обрезан лимитом токенов</template>
        </span>
        <span class="spacer"></span>
        <button class="ghost" @click="emit('close')">✕</button>
      </div>
      <p v-if="busy" class="muted">{{ t('plan.busy') }}</p>
      <p v-if="err" class="error">{{ err }}</p>
      <p v-if="info && info.marks && info.marks.length" class="plan-marks">
        {{ t('plan.marks') }}: {{ info.marks.map((m) => m.label).join(' · ') }}
        <span class="muted">{{ t('plan.marks.hint') }}</span>
      </p>
      <details class="abc-help">
        <summary>{{ t('plan.help') }}</summary>
        <p class="muted">
          [Verse] / [Chorus] — секции песни · | — граница такта · C D E F G A B — ноты (после ноты цифра = длительность: C2 вдвое дольше C) ·
          z — пауза · "Am" — аккорд в кавычках · минуc перед нотой (например _E) — понижение на полтона.<br />
          Это нотная запись: обычный путь — стили и студия, сюда можно не заходить.
        </p>
      </details>
      <textarea v-model="abc" rows="18" class="abc" spellcheck="false"></textarea>
      <div class="modal-actions">
        <button class="primary" :disabled="!abc.trim()" :title="t('plan.use.tip')"
                @click="emit('use', abc)">{{ t('plan.use') }}</button>
        <button class="ghost" :disabled="busy" @click="emit('new-plan')">{{ t('plan.new') }}</button>
        <button class="ghost" :disabled="busy" @click="emit('from-track')" :title="t('plan.fromTrack.tip')">{{ t('plan.fromTrack') }}</button>
      </div>
    </div>
  </div>
</template>
