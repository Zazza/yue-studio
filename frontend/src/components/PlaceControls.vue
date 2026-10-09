<script setup>
// Место звука в стерео: панорама (Л…центр…П) и ширина (0 — моно, 1 — как есть, 2 — шире), «в центр».
// Значение отдаётся по отпусканию ползунка (change): каждое изменение у пульта — пересборка трека.
import { computed, ref, watch } from 'vue'
import { useI18n } from '../i18n/index.js'
import { CENTER, isCenter, normPlace, placeLabel, PAN_MIN, PAN_MAX, WIDTH_MIN, WIDTH_MAX } from '../mixDesk.js'

const props = defineProps({
  modelValue: { type: Object, default: null },
  disabled: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()

// черновик под рукой — ползунок двигается без пересборки на каждом шаге
const draft = ref(normPlace(props.modelValue))
watch(() => props.modelValue, (v) => { draft.value = normPlace(v) })
const words = computed(() => ({ center: t('mix.w.center'), right: t('mix.w.right'), left: t('mix.w.left'), width: t('mix.width') }))
const caption = computed(() => placeLabel(draft.value, words.value))

function commit(key, v) {
  draft.value = { ...draft.value, [key]: Number(v) }
  emit('update:modelValue', { ...draft.value })
}
function center() {
  draft.value = { ...CENTER }
  emit('update:modelValue', { ...CENTER })
}
</script>

<template>
  <span class="place-ctl">
    <label class="muted" :title="t('mix.pan.tip')">{{ t('mix.pan') }}
      <input type="range" :min="PAN_MIN" :max="PAN_MAX" step="0.05" :value="draft.pan" :disabled="disabled"
             @input="draft = { ...draft, pan: Number($event.target.value) }" @change="commit('pan', $event.target.value)" /></label>
    <label class="muted" :title="t('mix.width.tip')">{{ t('mix.width') }}
      <input type="range" :min="WIDTH_MIN" :max="WIDTH_MAX" step="0.05" :value="draft.width" :disabled="disabled"
             @input="draft = { ...draft, width: Number($event.target.value) }" @change="commit('width', $event.target.value)" /></label>
    <span class="muted place-cap">{{ caption }}</span>
    <button v-if="!isCenter(draft)" class="ghost small-btn" :disabled="disabled" :title="t('mix.center.tip')" @click="center">
      {{ t('mix.center') }}</button>
  </span>
</template>

<style scoped>
.place-ctl { display: inline-flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.place-ctl input[type='range'] { width: 90px; vertical-align: middle; }
.place-cap { min-width: 9em; }
</style>
