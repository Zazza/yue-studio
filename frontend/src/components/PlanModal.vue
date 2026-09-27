<script setup>
// Редактор плана: ABC-партитура до рендера (план, из джобы, из трека, из профиля).
defineProps({
  open: Boolean,
  busy: Boolean,
  err: String,
  submitting: Boolean,
  info: Object, // {seed, seconds, truncated, fromJob, fromTrack, fromProfile}
})
const emit = defineEmits(['close', 'render', 'new-plan', 'from-track'])
const abc = defineModel('abc', { type: String, default: '' })
</script>

<template>
  <div v-if="open" class="modal-backdrop" @click.self="emit('close')">
    <div class="modal">
      <div class="modal-head">
        <h2>Ноты трека (ABC) <span class="muted">— для продвинутых</span></h2>
        <span v-if="info" class="muted plan-meta">
          seed {{ info.seed }}<template v-if="info.seconds"> · {{ info.seconds }}s</template><template v-if="info.fromJob"> · из джобы #{{ info.fromJob }}</template><template v-if="info.truncated"> · обрезан лимитом токенов</template>
        </span>
        <span class="spacer"></span>
        <button class="ghost" @click="emit('close')">✕</button>
      </div>
      <p v-if="busy" class="muted">Планирование на GPU… первый запуск грузит модель, может занять пару минут.</p>
      <p v-if="err" class="error">{{ err }}</p>
      <details class="abc-help">
        <summary>шпаргалка по ABC</summary>
        <p class="muted">
          [Verse] / [Chorus] — секции песни · | — граница такта · C D E F G A B — ноты (после ноты цифра = длительность: C2 вдвое дольше C) ·
          z — пауза · "Am" — аккорд в кавычках · минуc перед нотой (например _E) — понижение на полтона.<br />
          Это нотная запись: обычный путь — стили и студия, сюда можно не заходить.
        </p>
      </details>
      <textarea v-model="abc" rows="18" class="abc" spellcheck="false"></textarea>
      <div class="modal-actions">
        <button class="primary" :disabled="submitting || !abc.trim()" @click="emit('render', abc)">Рендер по этому ABC</button>
        <button class="ghost" :disabled="busy" @click="emit('new-plan')">Новый план</button>
        <button class="ghost" :disabled="busy" @click="emit('from-track')" title="Транскрипция вашего трека (SheetSage2) — кавер по чужой или своей мелодии">Из трека…</button>
      </div>
    </div>
  </div>
</template>
