<script setup>
// Единый селект в стиле приложения: кнопка + свой выпадающий список.
// Нативные <select> в WebKitGTK/WebView2 рисуются по-разному и глючат.
// Список телепортируется в body с position:fixed: absolute внутри
// скроллящейся панели (студия трека) не накрывает контент, а удлиняет
// скролл — «открыл селект, а прокручивать надо панель».
import { ref, onMounted, onUnmounted, nextTick } from 'vue'

const props = defineProps({
  modelValue: { type: String, default: '' },
  options: { type: Array, default: () => [] },        // [{value, label, disabled}]
  placeholder: { type: String, default: '— выберите —' },
  disabled: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])
const open = ref(false)
const root = ref(null)
const drop = ref(null)
const dropStyle = ref({})   // fixed-координаты по кнопке

function place() {
  if (!root.value) return
  const r = root.value.getBoundingClientRect()
  const below = window.innerHeight - r.bottom
  const above = r.top
  const up = below < 260 && above > below
  const maxH = Math.max(120, Math.min(220, (up ? above : below) - 12))
  dropStyle.value = {
    position: 'fixed',
    left: r.left + 'px',
    width: r.width + 'px',
    ...(up
      ? { bottom: (window.innerHeight - r.top + 2) + 'px', boxShadow: '0 -8px 24px rgba(0,0,0,.4)' }
      : { top: (r.bottom + 2) + 'px', boxShadow: '0 8px 24px rgba(0,0,0,.4)' }),
    maxHeight: maxH + 'px',
  }
}

const toggle = async () => {
  if (props.disabled) return
  if (!open.value) {
    open.value = true
    await nextTick()
    place()
  } else {
    open.value = false
  }
}

const current = () => {
  const o = props.options.find((x) => String(x.value) === String(props.modelValue))
  return o ? o.label : (props.modelValue || props.placeholder)
}
const pick = (o) => {
  if (o.disabled) return
  open.value = false
  emit('update:modelValue', o.value)
}
const onDocClick = (e) => {
  if (root.value && !root.value.contains(e.target) && drop.value && !drop.value.contains(e.target)) {
    open.value = false
  }
}
// скролл/ресайз уводят fixed-список от кнопки — закрываем, это честнее сдвига
const onReflow = () => { if (open.value) open.value = false }
onMounted(() => {
  document.addEventListener('click', onDocClick)
  window.addEventListener('scroll', onReflow, true)
  window.addEventListener('resize', onReflow)
})
onUnmounted(() => {
  document.removeEventListener('click', onDocClick)
  window.removeEventListener('scroll', onReflow, true)
  window.removeEventListener('resize', onReflow)
})
</script>

<template>
  <div ref="root" class="vselect" :class="{ disabled }">
    <button type="button" class="vselect-btn" :disabled="disabled" @click.stop="toggle">
      <span class="vselect-label">{{ current() }}</span>
      <span class="vselect-arrow" :class="{ open }">▾</span>
    </button>
    <Teleport to="body">
      <ul v-if="open && options.length" ref="drop" class="vselect-drop" :style="dropStyle">
        <li v-for="o in options" :key="o.value" :class="{ sel: String(o.value) === String(modelValue), off: o.disabled }"
            @mousedown.prevent="pick(o)">{{ o.label }}</li>
      </ul>
    </Teleport>
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
/* список телепортирован в body: fixed-координаты приходят инлайн-стилем */
.vselect-drop {
  z-index: 2000; margin: 0; padding: 4px 0;
  list-style: none; background: var(--panel); border: 1px solid var(--border); border-radius: 6px;
  overflow-y: auto;
}
.vselect-drop li { padding: 5px 10px; font-size: 13px; cursor: pointer; }
.vselect-drop li:hover { background: var(--panel2); }
.vselect-drop li.sel { color: var(--accent); font-weight: 600; }
.vselect-drop li.off { opacity: .5; cursor: default; }
</style>
