<script setup>
// Волна громкости артефакта («как в плеере»): клик — слушать с места,
// протяжка — выделение с прилипанием к тактам. Первая канва в проекте:
// сетка тактов/секций полупрозрачно поверх, курсор воспроизведения,
// режим спектра — готовой картинкой воркера под той же канвой
// (ось X у обоих линейна 0..длительность).
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { pxToSec, secToPx, snapSec } from '../waveLogic.js'

const props = defineProps({
  peaks: { type: Object, required: true },  // {duration_sec, peaks: [[min,max],...]}
  duration: { type: Number, default: 0 },
  marks: { type: Array, default: () => [] },        // gridMarks: [{sec, section}]
  edges: { type: Array, default: () => [] },        // границы тактов для прилипания
  cursorSec: { type: Number, default: 0 },
  selection: { type: Object, default: null },       // {from, to}
  snap: { type: Boolean, default: true },
  mode: { type: String, default: 'amp' },           // 'amp' | 'spectrum'
  spectrumUrl: { type: String, default: '' },
})
const emit = defineEmits(['seek', 'select'])

const wrapRef = ref(null)
const canvasRef = ref(null)
const drag = ref(null)   // {x0, x1, w} — протяжка выделения

const dur = computed(() => props.duration || props.peaks.duration_sec || 0)

// выделение во время протяжки — по пикселям, прилипание только на отпускании
const activeSel = computed(() => {
  if (drag.value) {
    const a = pxToSec(Math.min(drag.value.x0, drag.value.x1), dur.value, drag.value.w)
    const b = pxToSec(Math.max(drag.value.x0, drag.value.x1), dur.value, drag.value.w)
    return { from: a, to: b }
  }
  return props.selection
})

function cssVar(name, fallback) {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

function draw() {
  const cv = canvasRef.value
  if (!cv || !props.peaks) return
  const w = cv.clientWidth, h = cv.clientHeight
  if (!w || !h) return
  const dpr = window.devicePixelRatio || 1
  cv.width = Math.round(w * dpr)
  cv.height = Math.round(h * dpr)
  const ctx = cv.getContext('2d')
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
  ctx.clearRect(0, 0, w, h)
  const accent = cssVar('--accent', '#e05d3d')
  const muted = cssVar('--muted', '#8a8f98')
  const mid = h / 2

  // сетка: такты тонко, границы секций заметнее + подпись
  ctx.font = '10px sans-serif'
  for (const m of props.marks || []) {
    const x = Math.round(secToPx(m.sec, dur.value, w)) + 0.5
    const strong = !!m.section
    ctx.strokeStyle = strong ? 'rgba(224,93,61,.45)' : 'rgba(128,128,128,.18)'
    ctx.beginPath()
    ctx.moveTo(x, 0)
    ctx.lineTo(x, h)
    ctx.lineWidth = strong ? 1.5 : 1
    ctx.stroke()
    if (strong && m.section) {
      ctx.fillStyle = muted
      ctx.fillText(m.section, Math.min(x + 3, w - 40), 10)
    }
  }

  // амплитуда: на пиксель — агрегат накрытых им окон
  if (props.mode !== 'spectrum') {
    const peaks = props.peaks.peaks || []
    if (peaks.length) {
      const bpp = peaks.length / w
      ctx.strokeStyle = accent
      ctx.lineWidth = 1
      ctx.beginPath()
      for (let x = 0; x < w; x++) {
        const i0 = Math.floor(x * bpp)
        const i1 = Math.max(i0 + 1, Math.floor((x + 1) * bpp))
        let lo = 1, hi = -1
        for (let i = i0; i < i1 && i < peaks.length; i++) {
          lo = Math.min(lo, peaks[i][0])
          hi = Math.max(hi, peaks[i][1])
        }
        if (hi < lo) continue
        const y0 = mid - Math.max(hi, 0) * mid
        const y1 = mid - Math.min(lo, 0) * mid
        ctx.moveTo(x + 0.5, Math.max(y0, 1))
        ctx.lineTo(x + 0.5, Math.min(y1, h - 1))
      }
      ctx.stroke()
      // центральная линия
      ctx.strokeStyle = 'rgba(128,128,128,.25)'
      ctx.beginPath()
      ctx.moveTo(0, mid + 0.5)
      ctx.lineTo(w, mid + 0.5)
      ctx.stroke()
    }
  }

  // выделение
  const sel = activeSel.value
  if (sel && sel.to > sel.from) {
    const x1 = secToPx(sel.from, dur.value, w)
    const x2 = secToPx(sel.to, dur.value, w)
    ctx.fillStyle = 'rgba(224,93,61,.18)'
    ctx.fillRect(x1, 0, x2 - x1, h)
    ctx.strokeStyle = 'rgba(224,93,61,.6)'
    ctx.lineWidth = 1
    ctx.strokeRect(Math.round(x1) + 0.5, 0.5, Math.max(x2 - x1 - 1, 1), h - 1)
  }

  // курсор воспроизведения
  if (props.cursorSec > 0) {
    const x = Math.round(secToPx(props.cursorSec, dur.value, w)) + 0.5
    ctx.strokeStyle = accent
    ctx.lineWidth = 2
    ctx.beginPath()
    ctx.moveTo(x, 0)
    ctx.lineTo(x, h)
    ctx.stroke()
  }
}

function redraw() { requestAnimationFrame(draw) }

function onDown(e) {
  const r = e.currentTarget.getBoundingClientRect()
  drag.value = { x0: e.clientX - r.left, x1: e.clientX - r.left, w: r.width }
}

function onMove(e) {
  if (!drag.value) return
  drag.value = { ...drag.value, x1: e.clientX - e.currentTarget.getBoundingClientRect().left }
}

function onUp() {
  const d = drag.value
  drag.value = null
  if (!d) return
  // короткое движение без протяжки — клик: слушать с этого места
  if (Math.abs(d.x1 - d.x0) < 3) {
    emit('seek', pxToSec(d.x0, dur.value, d.w))
    return
  }
  const a = pxToSec(Math.min(d.x0, d.x1), dur.value, d.w)
  const b = pxToSec(Math.max(d.x0, d.x1), dur.value, d.w)
  emit('select', { from: snapSec(a, props.edges, props.snap), to: snapSec(b, props.edges, props.snap) })
}

let ro = null
onMounted(() => {
  ro = new ResizeObserver(redraw)
  if (wrapRef.value) ro.observe(wrapRef.value)
  draw()
})
onUnmounted(() => { if (ro) ro.disconnect() })

watch(() => [props.peaks, props.marks, props.cursorSec, props.selection, props.mode, drag.value, props.spectrumUrl], redraw, { deep: false })
</script>

<template>
  <div ref="wrapRef" class="wave-wrap">
    <img v-if="mode === 'spectrum' && spectrumUrl" class="wave-spectrum" :src="spectrumUrl" alt="spectrogram">
    <div v-else-if="mode === 'spectrum'" class="wave-empty"></div>
    <canvas ref="canvasRef" class="wave-canvas"
            @pointerdown.prevent="onDown" @pointermove="onMove"
            @pointerup="onUp" @pointercancel="onUp"></canvas>
  </div>
</template>
