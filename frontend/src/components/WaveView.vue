<script setup>
// Волна громкости артефакта («как в плеере»): клик — слушать с места,
// протяжка — выделение с прилипанием к тактам. Первая канва в проекте:
// сетка тактов/секций полупрозрачно поверх, курсор воспроизведения,
// режим спектра — готовой картинкой воркера под той же канвой
// (ось X у обоих линейна 0..длительность). Масштаб — окном просмотра:
// колесо — зум в точке курсора, shift+колесо/горизонтальное — прокрутка,
// «⟲» — вернуть весь трек в окно.
// Линия громкости (prop envelope — массив точек, null — режим выключен):
// клик по пустому месту ставит точку на линию, протяжка точки гнёт линию,
// протяжка по пустому месту — выделение участка (select), двойной или правый
// клик по точке удаляет; изменение — событие envelope (новый массив).
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { zoomAt, panWindow, clampWindow, viewSecToPx, viewPxToSec, snapSec } from '../waveLogic.js'
import { useI18n } from '../i18n/index.js'
import { addPoint, movePoint, removePoint, dbAt, dbToY, yToDb, hitPoint } from '../envelope.js'

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
  envelope: { type: Array, default: null },         // [{t, db}] — линия громкости; null — выкл
  color: { type: String, default: '' },             // цвет волны/выделения/курсора; '' — акцент темы
})
const emit = defineEmits(['seek', 'select', 'envelope'])
const { t } = useI18n()

const wrapRef = ref(null)
const canvasRef = ref(null)
const drag = ref(null)   // {x0, x1, w} — протяжка выделения
// линия громкости: локальная копия на время протяжки точки (событие — на отпускании)
const envPts = ref(null)
const envDrag = ref(null)  // {i, moved}
watch(() => props.envelope, (v) => { if (!envDrag.value) envPts.value = v ? v.map((p) => ({ ...p })) : null },
  { immediate: true })
const ENV_HIT_PX = 7

const dur = computed(() => props.duration || props.peaks.duration_sec || 0)

// окно просмотра: {t0, span} в мировых секундах; сбрасывается при смене файла
const win = ref({ t0: 0, span: 1 })
watch(() => props.peaks, () => { win.value = { t0: 0, span: dur.value || 1 } })
const zoomX = computed(() => (win.value.span > 0 ? dur.value / win.value.span : 1))

function cssVar(name, fallback) {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

// цвет волны — выбор пользователя (янтарь/коралл/…); '' = акцент темы.
// hex → rgba с альфой для заливок выделения и линий сетки
function withAlpha(color, a) {
  const m = /^#([0-9a-f]{6})$/i.exec(String(color).trim())
  if (!m) return color
  const n = parseInt(m[1], 16)
  return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${a})`
}

// выделение во время протяжки — по пикселям, прилипание только на отпускании
const activeSel = computed(() => {
  if (drag.value) {
    const a = viewPxToSec(Math.min(drag.value.x0, drag.value.x1), win.value, drag.value.w)
    const b = viewPxToSec(Math.max(drag.value.x0, drag.value.x1), win.value, drag.value.w)
    return { from: a, to: b }
  }
  return props.selection
})

// спектрограмма растягивается той же осью: ширина/сдвиг — долями контейнера
const spectrumStyle = computed(() => {
  const w = win.value
  if (!(w.span > 0)) return {}
  return {
    width: (dur.value / w.span * 100) + '%',
    left: (-w.t0 / w.span * 100) + '%',
  }
})

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
  const wave = props.color || accent
  const wv = win.value
  const x = (sec) => viewSecToPx(sec, wv, w)
  const mid = h / 2

  // сетка: такты тонко, границы секций заметнее + подпись (только видимые)
  ctx.font = '10px sans-serif'
  const t0 = wv.t0, t1 = wv.t0 + wv.span
  for (const m of props.marks || []) {
    if (m.sec < t0 - 1 || m.sec > t1) continue
    const mx = Math.round(x(m.sec)) + 0.5
    const strong = !!m.section
    ctx.strokeStyle = strong ? withAlpha(wave, .45) : 'rgba(128,128,128,.18)'
    ctx.lineWidth = strong ? 1.5 : 1
    ctx.beginPath()
    ctx.moveTo(mx, 0)
    ctx.lineTo(mx, h)
    ctx.stroke()
    if (strong && m.section && mx > -40) {
      ctx.fillStyle = muted
      ctx.fillText(m.section, Math.min(mx + 3, w - 40), 10)
    }
  }

  // амплитуда: на пиксель — агрегат накрытых им окон (в координатах окна)
  if (props.mode !== 'spectrum') {
    const peaks = props.peaks.peaks || []
    if (peaks.length && dur.value > 0) {
      const secPerPx = wv.span / w
      const binsPerSec = peaks.length / dur.value
      ctx.strokeStyle = wave
      ctx.lineWidth = 1
      ctx.beginPath()
      for (let px = 0; px < w; px++) {
        const s0 = wv.t0 + px * secPerPx
        const s1 = s0 + secPerPx
        let i0 = Math.floor(s0 * binsPerSec)
        let i1 = Math.max(i0 + 1, Math.ceil(s1 * binsPerSec))
        let lo = 1, hi = -1
        for (let i = Math.max(0, i0); i < Math.min(i1, peaks.length); i++) {
          lo = Math.min(lo, peaks[i][0])
          hi = Math.max(hi, peaks[i][1])
        }
        if (hi < lo) continue
        const y0 = mid - Math.max(hi, 0) * mid
        const y1 = mid - Math.min(lo, 0) * mid
        ctx.moveTo(px + 0.5, Math.max(y0, 1))
        ctx.lineTo(px + 0.5, Math.min(y1, h - 1))
      }
      ctx.stroke()
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
    const x1 = x(sel.from), x2 = x(sel.to)
    if (x2 > 0 && x1 < w) {
      ctx.fillStyle = withAlpha(wave, .18)
      ctx.fillRect(x1, 0, x2 - x1, h)
      ctx.strokeStyle = withAlpha(wave, .6)
      ctx.lineWidth = 1
      ctx.strokeRect(Math.round(x1) + 0.5, 0.5, Math.max(x2 - x1 - 1, 1), h - 1)
    }
  }

  // линия громкости: 0 дБ пунктиром, линия по видимому окну, точки кружками
  if (envPts.value) {
    const pts = envPts.value
    const y = (db) => dbToY(db, h)
    ctx.strokeStyle = 'rgba(128,128,128,.5)'
    ctx.setLineDash([4, 4])
    ctx.beginPath()
    ctx.moveTo(0, Math.round(y(0)) + 0.5)
    ctx.lineTo(w, Math.round(y(0)) + 0.5)
    ctx.stroke()
    ctx.setLineDash([])
    const env = cssVar('--env', '#3d9be0')
    ctx.strokeStyle = env
    ctx.lineWidth = 2
    ctx.beginPath()
    ctx.moveTo(0, y(dbAt(pts, t0)))
    for (const p of pts) if (p.t > t0 && p.t < t1) ctx.lineTo(x(p.t), y(p.db))
    ctx.lineTo(w, y(dbAt(pts, t1)))
    ctx.stroke()
    ctx.fillStyle = env
    ctx.font = '10px sans-serif'
    for (const p of pts) {
      if (p.t < t0 || p.t > t1) continue
      ctx.beginPath()
      ctx.arc(x(p.t), y(p.db), 4, 0, 2 * Math.PI)
      ctx.fill()
    }
    const d = envDrag.value && pts[envDrag.value.i]
    if (d) ctx.fillText(t('studio.wave.env.db', { db: (d.db > 0 ? '+' : '') + d.db.toFixed(1) }), Math.min(x(d.t) + 6, w - 50), Math.max(y(d.db) - 6, 10))
  }

  // курсор воспроизведения
  if (props.cursorSec > 0) {
    const cx = Math.round(x(props.cursorSec)) + 0.5
    if (cx >= 0 && cx <= w) {
      ctx.strokeStyle = wave
      ctx.lineWidth = 2
      ctx.beginPath()
      ctx.moveTo(cx, 0)
      ctx.lineTo(cx, h)
      ctx.stroke()
    }
  }
}

function redraw() { requestAnimationFrame(draw) }

// координаты события в канве: секунда трека и дБ линии
function envAt(e) {
  const r = e.currentTarget.getBoundingClientRect()
  const px = e.clientX - r.left, py = e.clientY - r.top
  return { px, py, w: r.width, h: r.height, t: Math.max(0, Math.min(dur.value, viewPxToSec(px, win.value, r.width))) }
}
function envHit(e) {
  const a = envAt(e)
  const i = hitPoint(envPts.value, a.px, a.py, (t) => viewSecToPx(t, win.value, a.w), (db) => dbToY(db, a.h), ENV_HIT_PX)
  return { ...a, i }
}

function onDown(e) {
  if (envPts.value) {
    if (e.button !== 0) return
    const a = envHit(e)
    if (a.i >= 0) {
      envDrag.value = { i: a.i, moved: false }
      e.currentTarget.setPointerCapture(e.pointerId)
      return
    }
    // мимо точек: клик — точка на линии, протяжка — выделение участка
  }
  const r = e.currentTarget.getBoundingClientRect()
  drag.value = { x0: e.clientX - r.left, x1: e.clientX - r.left, w: r.width }
}

function onMove(e) {
  if (envDrag.value) {
    const a = envAt(e)
    envPts.value = movePoint(envPts.value, envDrag.value.i, a.t, yToDb(a.py, a.h))
    envDrag.value = { ...envDrag.value, moved: true }
    return
  }
  if (!drag.value) return
  drag.value = { ...drag.value, x1: e.clientX - e.currentTarget.getBoundingClientRect().left }
}

function onUp() {
  if (envDrag.value) {
    const moved = envDrag.value.moved
    envDrag.value = null
    if (moved) emit('envelope', envPts.value.map((p) => ({ ...p })))
    return
  }
  const d = drag.value
  drag.value = null
  if (!d) return
  // короткое движение без протяжки — клик: слушать с этого места; в режиме
  // линии — точка на самой линии (громкость не прыгает, линия гнётся протяжкой)
  if (Math.abs(d.x1 - d.x0) < 3) {
    const t = Math.max(0, Math.min(dur.value, viewPxToSec(d.x0, win.value, d.w)))
    if (envPts.value) {
      envPts.value = addPoint(envPts.value, t, dbAt(envPts.value, t))
      emit('envelope', envPts.value.map((p) => ({ ...p })))
      return
    }
    emit('seek', t)
    return
  }
  const a = viewPxToSec(Math.min(d.x0, d.x1), win.value, d.w)
  const b = viewPxToSec(Math.max(d.x0, d.x1), win.value, d.w)
  emit('select', { from: snapSec(a, props.edges, props.snap), to: snapSec(b, props.edges, props.snap) })
}

// двойной/правый клик по точке линии — удалить её
function onEnvRemove(e) {
  if (!envPts.value) return
  const { i } = envHit(e)
  if (i < 0) return
  envPts.value = removePoint(envPts.value, i)
  emit('envelope', envPts.value.map((p) => ({ ...p })))
}

// колесо — зум в точке курсора; shift/горизонтальное — прокрутка окна
function onWheel(e) {
  const r = e.currentTarget.getBoundingClientRect()
  if (e.shiftKey || Math.abs(e.deltaX) > Math.abs(e.deltaY)) {
    const d = (e.shiftKey ? e.deltaY : e.deltaX) * win.value.span / r.width
    win.value = panWindow(win.value, dur.value, d)
  } else {
    const anchor = viewPxToSec(e.clientX - r.left, win.value, r.width)
    win.value = zoomAt(win.value, dur.value, anchor, e.deltaY < 0 ? 1.3 : 1 / 1.3)
  }
}

// полоса прокрутки-миникарта под волной: клик мимо ползунка — прыжок центром
// окна, перетаскивание ползунка — панорама
const thumbStyle = computed(() => {
  if (!(dur.value > 0)) return { display: 'none' }
  const w = win.value
  const full = Math.max(w.span / dur.value, 0.02)   // ползунок не тоньше 2%
  const left = Math.min(Math.max(w.t0 / dur.value, 0), 1 - full)
  return { left: (left * 100) + '%', width: (full * 100) + '%' }
})
const scrollDrag = ref(null)   // {x, t0, w}

function onScrollDown(e) {
  const r = e.currentTarget.getBoundingClientRect()
  const frac = (e.clientX - r.left) / r.width
  const wv = win.value
  const thumbL = wv.t0 / dur.value
  const thumbW = Math.max(wv.span / dur.value, 0.02)
  if (frac < thumbL || frac > thumbL + thumbW) {
    win.value = clampWindow({ t0: frac * dur.value - wv.span / 2, span: wv.span }, dur.value)
  }
  scrollDrag.value = { x: e.clientX, t0: win.value.t0, w: r.width }
  e.currentTarget.setPointerCapture(e.pointerId)
}

function onScrollMove(e) {
  if (!scrollDrag.value) return
  const d = ((e.clientX - scrollDrag.value.x) / scrollDrag.value.w) * dur.value
  win.value = clampWindow({ t0: scrollDrag.value.t0 + d, span: win.value.span }, dur.value)
}

function onScrollUp() { scrollDrag.value = null }

function fitWindow() {
  win.value = clampWindow({ t0: 0, span: dur.value || 1 }, dur.value)
}

let ro = null
onMounted(() => {
  fitWindow()
  ro = new ResizeObserver(redraw)
  if (wrapRef.value) ro.observe(wrapRef.value)
  draw()
})
onUnmounted(() => { if (ro) ro.disconnect() })

watch(() => [props.peaks, props.marks, props.cursorSec, props.selection, props.mode, props.color, drag.value, props.spectrumUrl, win.value, envPts.value, envDrag.value], redraw, { deep: false })
</script>

<template>
  <div ref="wrapRef" class="wave-wrap">
    <img v-if="mode === 'spectrum' && spectrumUrl" class="wave-spectrum" :src="spectrumUrl"
         :style="spectrumStyle" alt="spectrogram">
    <div v-else-if="mode === 'spectrum'" class="wave-empty"></div>
    <canvas ref="canvasRef" class="wave-canvas"
            @pointerdown.prevent="onDown" @pointermove="onMove"
            @pointerup="onUp" @pointercancel="onUp"
            @dblclick="onEnvRemove" @contextmenu="envelope && ($event.preventDefault(), onEnvRemove($event))"
            @wheel.prevent="onWheel"></canvas>
    <div class="wave-zoom">
      <span v-if="zoomX > 1.01" class="muted">×{{ zoomX.toFixed(zoomX < 10 ? 1 : 0) }}</span>
      <button v-if="zoomX > 1.01" class="ghost small-btn" title="весь трек в окно" @click="fitWindow">⟲</button>
    </div>
  </div>
  <div class="wave-scroll" @pointerdown.prevent="onScrollDown" @pointermove="onScrollMove"
       @pointerup="onScrollUp" @pointercancel="onScrollUp">
    <div class="wave-thumb" :style="thumbStyle"></div>
  </div>
</template>
