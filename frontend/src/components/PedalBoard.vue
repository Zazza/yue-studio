<script setup>
// Блок «🎸 Педали»: доска гитарных эффектов по порядку звука, готовые наборы,
// превью места с «было / стало», сравнение до четырёх досок на одном куске,
// применение к дорожке (реестр пересборки) или ко всему треку. Логика доски —
// в pedals.js (vitest), окно превью — в fxPreview.js.
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { usePlayer, fmtDur } from '../composables/usePlayer.js'
import { useInserts } from '../composables/useInserts.js'
import { previewWindow } from '../fxPreview.js'
import {
  addPedal, applyPreset, boardSteps, movePedal, pedalChains, removePedal, setParam, togglePedal,
} from '../pedals.js'
import VSelect from '../VSelect.vue'

const props = defineProps({
  job: { type: Object, required: true },
  chains: { type: Array, default: () => [] },
  sel: { type: Object, default: null },        // выделение на волне/ролле {from, to}
  cursor: { type: Number, default: 0 },        // курсор воспроизведения, с
})
const emit = defineEmits(['applied'])
const { t } = useI18n()
const { nowPlayingKey, playerState, refreshPlayer } = usePlayer()
const inserts = useInserts()

const STEMS = ['guitar', 'other', 'piano', 'vocals', 'bass', '']
const SLOTS = ['A', 'B', 'C', 'D']
const SLOT = 'P'                    // свой слот превью: не путается с превью блока DSP
const NO_STEM = 'не выделилась'     // ошибка Go: дорожки в треке нет (воркер без 6-стемной модели)
const PLAY_KEY = 'pedals'           // «сейчас играет превью педалей» в общем плеере
const STATE_KEY = 'yue_studio_state'

const board = ref([])
const stem = ref('guitar')
const selIdx = ref(-1)
const presets = ref([])
const presetSel = ref('')
const busy = ref(false)
const msg = ref('')
const prev = ref(null)              // {from, to, which: 'wet'|'dry', slot, hasSolo}
const solo = ref(false)             // слушать только дорожку (без микса): в стене звук педали маскируется
const cmpSel = ref([])              // id наборов для сравнения (кроме текущей доски)
const cmp = ref([])                 // [{slot, label}] — готовые куски сравнения

const palette = computed(() => pedalChains(props.chains))
const chainById = (id) => props.chains.find((c) => c.id === id)
const chainName = (id) => chainById(id)?.name || id
const selPedal = computed(() => board.value[selIdx.value] || null)
const selChain = computed(() => (selPedal.value ? chainById(selPedal.value.chain) : null))
const stemOptions = computed(() => STEMS.map((v) => ({ value: v, label: t('studio.dsp.target.' + (v || 'mix')) })))
const hasOn = computed(() => board.value.some((p) => !p.off))

// доска и дорожка — на трек, переживают перезапуск (как остальное в студии)
function load() {
  try {
    const st = JSON.parse(localStorage.getItem(STATE_KEY) || '{}')
    const per = (st.pedals && st.pedals[props.job.id]) || {}
    if (Array.isArray(per.board)) board.value = per.board
    if (typeof per.stem === 'string') stem.value = per.stem
  } catch { /* битое состояние — начинаем с пустой доски */ }
}
function save() {
  try {
    const st = JSON.parse(localStorage.getItem(STATE_KEY) || '{}')
    st.pedals = st.pedals || {}
    st.pedals[props.job.id] = { board: board.value, stem: stem.value }
    localStorage.setItem(STATE_KEY, JSON.stringify(st))
  } catch { /* нет localStorage — доска живёт до закрытия студии */ }
}
watch([board, stem], save, { deep: true })
onMounted(async () => {
  load()
  try { presets.value = await api.dspPresets() } catch (e) { msg.value = String(e) }
})

function defaultsOf(id) {
  const d = {}
  for (const p of chainById(id)?.params || []) d[p.id] = p.default
  return d
}
function add(id) {
  if (!id) return
  board.value = addPedal(board.value, id, defaultsOf(id))
  selIdx.value = board.value.length - 1
}
function choosePreset(id) {
  presetSel.value = id
  const p = presets.value.find((x) => x.id === id)
  if (!p) return
  board.value = applyPreset(p, props.chains)
  selIdx.value = -1
  msg.value = p.note
}
function move(i, dir) {
  board.value = movePedal(board.value, i, dir)
  if (selIdx.value === i) selIdx.value = i + dir
}
function remove(i) {
  board.value = removePedal(board.value, i)
  selIdx.value = -1
}

const win = () => previewWindow(props.sel, props.cursor || null, props.job.duration_sec)
function curPos() {
  const playing = nowPlayingKey.value === PLAY_KEY && playerState.value.job_id === props.job.id
  return playing ? (playerState.value.position_sec || 0) : 0
}
async function play(slot, which, start = 0) {
  const piece = solo.value && prev.value && prev.value.hasSolo ? which + '_solo' : which
  await api.playPreview(props.job.id, slot, piece, start)
  nowPlayingKey.value = PLAY_KEY
  prev.value = { ...prev.value, slot, which }
  refreshPlayer()
}

// withStem — вызов на выбранной дорожке; гитары в треке нет (старые стемы без
// 6-стемной модели, воркер её не выделил) — педали переходят на «гитары/синты»
// и повторяют вызов, а не падают ошибкой
async function withStem(fn) {
  try {
    return await fn(stem.value)
  } catch (e) {
    if (stem.value !== 'guitar' || !String(e).includes(NO_STEM)) throw e
    stem.value = 'other'
    const r = await fn(stem.value)
    msg.value = t('pedals.noguitar')
    return r
  }
}

async function preview() {
  const w = win()
  busy.value = true
  msg.value = ''
  try {
    const r = await withStem((st) => api.fxPreview(props.job.id, st, boardSteps(board.value), w.from, w.to, SLOT))
    prev.value = { from: r.from, to: r.to, which: 'wet', slot: SLOT, hasSolo: !!r.wet_solo }
    cmp.value = []
    await play(SLOT, 'wet')
  } catch (e) { msg.value = String(e) } finally { busy.value = false }
}
async function toggleAB() {
  if (!prev.value) return
  try {
    await play(prev.value.slot, prev.value.which === 'wet' ? 'dry' : 'wet', curPos())
  } catch (e) { msg.value = String(e) }
}

// сравнение: A — текущая доска, B–D — выбранные наборы, все на одном куске
async function compare() {
  const w = win()
  const boards = [{ label: t('pedals.cmp.current'), steps: boardSteps(board.value) }]
  for (const id of cmpSel.value.slice(0, SLOTS.length - 1)) {
    const p = presets.value.find((x) => x.id === id)
    if (p) boards.push({ label: p.name, steps: boardSteps(applyPreset(p, props.chains)) })
  }
  busy.value = true
  msg.value = ''
  try {
    const done = []
    for (let i = 0; i < boards.length; i++) {
      if (!boards[i].steps.some((s) => !s.off)) continue
      const r = await withStem((st) => api.fxPreview(props.job.id, st, boards[i].steps, w.from, w.to, SLOTS[i]))
      done.push({ slot: SLOTS[i], label: boards[i].label })
      prev.value = { from: r.from, to: r.to, which: 'wet', slot: SLOTS[i], hasSolo: !!r.wet_solo }
    }
    cmp.value = done
    if (done.length) await play(done[0].slot, 'wet')
  } catch (e) { msg.value = String(e) } finally { busy.value = false }
}
async function playSlot(slot) {
  try { await play(slot, 'wet', curPos()) } catch (e) { msg.value = String(e) }
}

async function apply() {
  const steps = boardSteps(board.value)
  const label = presetSel.value && presets.value.find((p) => p.id === presetSel.value)?.name || ''
  busy.value = true
  msg.value = ''
  try {
    if (stem.value) {
      const s = props.sel
      await withStem(async (st) => {
        // запись реестра с дорожкой, которой нет, пересборку роняет каждый раз — сначала
        // проверяем дорожку быстрым превью (то же скачивание/разделение, тот же кэш)
        const w = win()
        await api.fxPreview(props.job.id, st, steps, w.from, w.to, SLOT)
        await inserts.addStemPedals(props.job.id, { stem: st, steps, from: s ? s.from : 0, to: s ? s.to : 0, label })
      })
    } else {
      await api.applySteps(props.job.id, steps, label)
    }
    emit('applied')
    msg.value = (msg.value ? msg.value + ' ' : '') + t('pedals.applied')
  } catch (e) { msg.value = String(e) } finally { busy.value = false }
}

async function findGrid() {
  const i = selIdx.value
  if (i < 0) return
  try {
    const s = props.sel
    const g = await api.jobGrid(props.job.id, s ? s.from : 0, s ? s.to : 0)
    board.value = setParam(board.value, i, 'bpm', g.bpm)
    if (selChain.value?.params.some((p) => p.id === 'offset')) board.value = setParam(board.value, i, 'offset', g.offset)
    msg.value = t(`studio.dsp.grid.found.${g.source}`, { bpm: g.bpm.toFixed(1), at: fmtDur(g.offset) })
  } catch (e) { msg.value = String(e) }
}
</script>

<template>
  <div class="dsp-row">
    <VSelect v-model="stem" :options="stemOptions" :title="t('pedals.stem.tip')" style="max-width: 150px" />
    <VSelect :model-value="presetSel" :options="presets.map((p) => ({ value: p.id, label: p.name }))"
             :placeholder="t('pedals.preset')" style="max-width: 200px" @update:model-value="choosePreset" />
    <VSelect :model-value="''" :options="palette.map((c) => ({ value: c.id, label: c.name }))"
             :placeholder="t('pedals.add')" style="max-width: 220px" @update:model-value="add" />
  </div>

  <div class="pedal-row">
    <span v-if="!board.length" class="muted">{{ t('pedals.empty') }}</span>
    <template v-for="(p, i) in board" :key="i">
      <span v-if="i > 0" class="muted">→</span>
      <div class="pedal" :class="{ off: p.off, sel: i === selIdx }" @click="selIdx = i === selIdx ? -1 : i">
        <div class="pedal-name">{{ chainName(p.chain) }}</div>
        <div class="pedal-ctl" @click.stop>
          <button class="ghost small-btn" :title="t('pedals.toggle.tip')" @click="board = togglePedal(board, i)">
            {{ p.off ? '○' : '●' }}</button>
          <button class="ghost small-btn" :disabled="i === 0" @click="move(i, -1)">←</button>
          <button class="ghost small-btn" :disabled="i === board.length - 1" @click="move(i, 1)">→</button>
          <button class="ghost small-btn" :title="t('pedals.remove')" @click="remove(i)">✕</button>
        </div>
      </div>
    </template>
  </div>

  <div v-if="selChain" class="dsp-params">
    <p class="muted dsp-note">{{ selChain.note }}</p>
    <div v-if="selChain.params.some((p) => p.id === 'bpm')" class="dsp-row">
      <button class="ghost small-btn" :title="t('studio.dsp.grid.tip')" @click="findGrid">{{ t('studio.dsp.grid') }}</button>
    </div>
    <label v-for="prm in selChain.params" :key="prm.id">
      <span class="dsp-plabel">{{ prm.label }}</span>
      <input type="range" :min="prm.min" :max="prm.max" :step="prm.step" :disabled="busy"
             :value="selPedal.params[prm.id] ?? prm.default"
             @input="(e) => (board = setParam(board, selIdx, prm.id, Number(e.target.value)))" />
      <span class="dsp-pval">{{ selPedal.params[prm.id] ?? prm.default }}</span>
    </label>
  </div>

  <div class="dsp-row">
    <button class="ghost small-btn" :disabled="busy || !hasOn" :title="t('studio.dsp.preview.tip')" @click="preview">
      {{ busy ? '…' : t('studio.dsp.preview') }}</button>
    <button v-if="prev" class="ghost small-btn" :title="t('studio.dsp.ab.tip')" @click="toggleAB">
      {{ prev.which === 'wet' ? t('studio.dsp.ab.wet') : t('studio.dsp.ab.dry') }}</button>
    <label v-if="prev && prev.hasSolo" class="muted" :title="t('studio.dsp.solo.tip')">
      <input v-model="solo" type="checkbox" @change="play(prev.slot, prev.which, curPos())" /> {{ t('studio.dsp.solo') }}
    </label>
    <span v-if="prev" class="muted">{{ fmtDur(prev.from) }}–{{ fmtDur(prev.to) }}</span>
    <button class="primary small" :disabled="busy || !hasOn" :title="t('pedals.apply.tip')" @click="apply">
      {{ t('pedals.apply') }}</button>
  </div>

  <details class="pedal-cmp">
    <summary class="muted">{{ t('pedals.cmp') }}</summary>
    <div class="dsp-row">
      <label v-for="p in presets" :key="p.id" class="muted">
        <input v-model="cmpSel" type="checkbox" :value="p.id"
               :disabled="!cmpSel.includes(p.id) && cmpSel.length >= SLOTS.length - 1" /> {{ p.name }}
      </label>
    </div>
    <div class="dsp-row">
      <button class="ghost small-btn" :disabled="busy" :title="t('pedals.cmp.tip')" @click="compare">
        {{ busy ? '…' : t('pedals.cmp.run') }}</button>
      <button v-for="c in cmp" :key="c.slot" class="ghost small-btn"
              :class="{ stop: prev && prev.slot === c.slot && prev.which === 'wet' }" @click="playSlot(c.slot)">
        {{ c.slot }} · {{ c.label }}</button>
      <button v-if="cmp.length" class="ghost small-btn" @click="play(prev.slot, 'dry', curPos())">
        {{ t('pedals.cmp.dry') }}</button>
    </div>
  </details>
  <p v-if="msg" class="muted">{{ msg }}</p>
</template>

<style scoped>
.pedal-row { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin: 6px 0; }
.pedal {
  border: 1px solid var(--border, #444); border-radius: 6px; padding: 4px 6px; cursor: pointer;
  min-width: 110px; background: var(--panel, transparent);
}
.pedal.sel { border-color: var(--accent, #c90); }
.pedal.off { opacity: 0.45; }
.pedal-name { font-size: 0.85em; margin-bottom: 2px; }
.pedal-ctl { display: flex; gap: 2px; }
.pedal-cmp { margin: 4px 0; }
</style>
