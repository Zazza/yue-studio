<script setup>
// Единый селект в стиле приложения: кнопка + свой выпадающий список.
// Нативные <select> в WebKitGTK/WebView2 рисуются по-разному и глючат.
// Список телепортируется в body с position:fixed: absolute внутри
// скроллящейся панели (студия трека) не накрывает контент, а удлиняет
// скролл — «открыл селект, а прокручивать надо панель».
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { matchOptions } from './optionFilter.js'

const props = defineProps({
  modelValue: { type: String, default: '' },
  options: { type: Array, default: () => [] },        // [{value, label, disabled, icon?}] — icon: имя из icons.js
  placeholder: { type: String, default: '— выберите —' },
  disabled: { type: Boolean, default: false },
  // длинные списки (песни): поле поиска сверху, пункты фильтруются по словам
  searchable: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])
const open = ref(false)
const root = ref(null)
const drop = ref(null)
const dropStyle = ref({})   // fixed-координаты по кнопке
const query = ref('')
const search = ref(null)
const shown = computed(() => (props.searchable ? matchOptions(props.options, query.value) : props.options))

function place() {
  if (!root.value) return
  const r = root.value.getBoundingClientRect()
  const below = window.innerHeight - r.bottom
  const above = r.top
  const up = below < 260 && above > below
  const maxH = Math.max(120, Math.min(220, (up ? above : below) - 12))
  // ширина — по самому длинному пункту, но не уже кнопки и не шире экрана:
  // список, равный кнопке, ломал длинные варианты переносом строк
  dropStyle.value = {
    position: 'fixed',
    left: r.left + 'px',
    minWidth: r.width + 'px',
    width: 'max-content',
    maxWidth: Math.max(r.width, window.innerWidth - r.left - 12) + 'px',
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
    query.value = ''
    await nextTick()
    place()
    if (search.value) search.value.focus()
  } else {
    open.value = false
  }
}

const currentOpt = () => props.options.find((x) => String(x.value) === String(props.modelValue))
const current = () => {
  const o = currentOpt()
  return o ? o.label : (props.modelValue || props.placeholder)
}
const pick = (o) => {
  if (o.disabled) return
  open.value = false
  emit('update:modelValue', o.value)
}
// Enter — первый подходящий пункт, Esc — закрыть
const onSearchKey = (e) => {
  if (e.key === 'Enter') {
    const o = shown.value.find((x) => !x.disabled)
    if (o) pick(o)
  } else if (e.key === 'Escape') {
    open.value = false
  }
}
// скролл/ресайз уводят fixed-список от кнопки — закрываем, это честнее сдвига.
const onDocClick = (e) => {
  if (root.value && !root.value.contains(e.target) && drop.value && !drop.value.contains(e.target)) {
    open.value = false
  }
}

// Но прокрутка САМОГО списка (длинные группы стилей) — не закрывает
const onReflow = (e) => {
  if (!open.value) return
  if (drop.value && e && e.target && drop.value.contains(e.target)) return
  open.value = false
}
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
      <span class="vselect-label"><template v-if="currentOpt()?.icon"><AppIcon :name="currentOpt().icon" /> </template>{{ current() }}</span>
      <span class="vselect-arrow" :class="{ open }">▾</span>
    </button>
    <Teleport to="body">
      <ul v-if="open && options.length" ref="drop" class="vselect-drop" :style="dropStyle">
        <li v-if="searchable" class="vselect-search">
          <AppIcon name="search" class="vselect-search-ico" />
          <input ref="search" v-model="query" @keydown="onSearchKey" />
        </li>
        <li v-if="searchable && !shown.length" class="off">—</li>
        <li v-for="o in shown" :key="o.value" :class="{ sel: String(o.value) === String(modelValue), off: o.disabled }"
            @mousedown.prevent="pick(o)"><template v-if="o.icon"><AppIcon :name="o.icon" /> </template>{{ o.label }}</li>
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
.vselect-drop li { padding: 5px 10px; font-size: 13px; cursor: pointer; white-space: nowrap; }
.vselect-drop li:hover { background: var(--panel2); }
.vselect-drop li.sel { color: var(--accent); font-weight: 600; }
.vselect-drop li.off { opacity: .5; cursor: default; }
.vselect-drop li.vselect-search { position: sticky; top: -4px; padding: 4px 6px; background: var(--panel); cursor: default;
  display: flex; align-items: center; gap: 6px; }
.vselect-search-ico { color: var(--muted); }
.vselect-drop li.vselect-search:hover { background: var(--panel); }
.vselect-search input { width: 100%; box-sizing: border-box; font-size: 13px; padding: 4px 8px; }
</style>
