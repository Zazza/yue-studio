<script setup>
// Единый селект в стиле приложения: кнопка + свой выпадающий список.
// Нативные <select> в WebKitGTK/WebView2 рисуются по-разному и глючат.
import { ref, onMounted, onUnmounted } from 'vue'

const props = defineProps({
  modelValue: { type: String, default: '' },
  options: { type: Array, default: () => [] },        // [{value, label, disabled}]
  placeholder: { type: String, default: '— выберите —' },
  disabled: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])
const open = ref(false)
const root = ref(null)

const current = () => {
  const o = props.options.find((x) => String(x.value) === String(props.modelValue))
  return o ? o.label : (props.modelValue || props.placeholder)
}
const pick = (o) => {
  if (o.disabled) return
  open.value = false
  emit('update:modelValue', o.value)
}
const onDocClick = (e) => { if (root.value && !root.value.contains(e.target)) open.value = false }
onMounted(() => document.addEventListener('click', onDocClick))
onUnmounted(() => document.removeEventListener('click', onDocClick))
</script>

<template>
  <div ref="root" class="vselect" :class="{ disabled }">
    <button type="button" class="vselect-btn" :disabled="disabled" @click.stop="disabled || (open = !open)">
      <span class="vselect-label">{{ current() }}</span>
      <span class="vselect-arrow" :class="{ open }">▾</span>
    </button>
    <ul v-if="open && options.length" class="vselect-drop">
      <li v-for="o in options" :key="o.value" :class="{ sel: String(o.value) === String(modelValue), off: o.disabled }"
          @mousedown.prevent="pick(o)">{{ o.label }}</li>
    </ul>
  </div>
</template>

<style scoped>
.vselect { position: relative; }
.vselect-btn {
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
  width: 100%; text-align: left; background: var(--panel2); border: 1px solid var(--border);
  border-radius: 6px; color: var(--text); font-weight: 400; font-size: 13px; padding: 6px 10px;
}
.vselect.disabled { opacity: .55; }
.vselect-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.vselect-arrow { flex: none; color: var(--muted); font-size: 10px; transition: transform .15s; }
.vselect-arrow.open { transform: rotate(180deg); }
.vselect-drop {
  position: absolute; top: 100%; left: 0; right: 0; z-index: 50; margin: 2px 0 0; padding: 4px 0;
  list-style: none; background: var(--panel); border: 1px solid var(--border); border-radius: 6px;
  max-height: 220px; overflow-y: auto; box-shadow: 0 8px 24px rgba(0,0,0,.4);
}
.vselect-drop li { padding: 5px 10px; font-size: 13px; cursor: pointer; }
.vselect-drop li:hover { background: var(--panel2); }
.vselect-drop li.sel { color: var(--accent); font-weight: 600; }
.vselect-drop li.off { opacity: .5; cursor: default; }
</style>
