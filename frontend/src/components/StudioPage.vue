<script setup>
// Студия трека: пиано-ролл партитуры, минус по стемам, овердаб, DSP-цепочки.
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from '../i18n/index.js'
const { t } = useI18n()
import { api } from '../api.js'
import { usePlayer, fmtDur } from '../composables/usePlayer.js'
import { odPartyChips } from '../slotOptions.js'
import VSelect from '../VSelect.vue'

const props = defineProps({ job: { type: Object, required: true }, autoTranslate: Boolean })
const emit = defineEmits(['close', 'open-metrics', 'voices-to-plan'])

const { isPlaying, playBusy, playBtn, toggleArtifact } = usePlayer()

const rollData = ref(null)     // parsed score
const rollBusy = ref(false)
const rollErr = ref('')
const rollSel = ref(null)      // {a: barIdx, b: barIdx}
const rollVoices = ref([])     // [{orig, name, on}]
const stemMute = ref({})       // {drums: true, ...} — что выкинуть из минуса
const stemsList = ref([])
const previewBusy = ref(false)
let rollDrag = false

// овердаб: чипы партий + ручной ввод
const odChips = ref(new Set())
const odStyle = ref('')
const odGain = ref(0.5)
const odBusy = ref(false)

// DSP-цепочки (ffmpeg на ПК)
const dspChains = ref([])
const dspSel = ref('')
const dspParams = ref({})
const dspBusy = ref(false)
const dspVariants = ref([])
const jobMetrics = ref(null)   // метрики исходника (для инлайн-дельт)

const curChain = computed(() => dspChains.value.find((c) => c.id === dspSel.value) || null)

// такты строками по 32 — иначе сетка шириной в сотни тактов
const rollChunks = computed(() => {
  const bars = rollData.value && rollData.value.bars
  if (!bars) return []
  const out = []
  for (let i = 0; i < bars.length; i += 32) out.push(bars.slice(i, i + 32))
  return out
})

const selRange = computed(() => {
  const s = rollSel.value
  const bars = rollData.value && rollData.value.bars
  if (!s || !bars || !bars.length) return null
  const a = bars[Math.min(s.a, s.b)], b = bars[Math.max(s.a, s.b)]
  const to = Math.min(b.end_sec, props.job.duration_sec || b.end_sec)   // звук короче расклада ABC
  const from = Math.min(a.start_sec, Math.max(0, to - 1))
  return { from, to }
})

onMounted(async () => {
  await openRoll()
  try { stemsList.value = (await api.jobStems(props.job.id)) || [] } catch {}
  try { dspChains.value = (await api.dspChains()) || [] } catch {}
  // восстановить промежуточное состояние студии (переживает перезапуск)
  try {
    const st = JSON.parse(localStorage.getItem('yue_studio_state') || '{}')
    const per = st.jobs && st.jobs[props.job.id] || {}
    if (per.odStyle !== undefined) odStyle.value = per.odStyle
    if (per.odGain !== undefined) odGain.value = per.odGain
    if (per.stemMute) stemMute.value = per.stemMute
    if (per.rollVoices) rollVoices.value = per.rollVoices
    if (per.dspSel) dspSel.value = per.dspSel
    if (per.dspParams) dspParams.value = per.dspParams
    if (per.odChips) odChips.value = new Set(per.odChips)
  } catch {}
})

onUnmounted(saveStudioState)

function saveStudioState() {
  try {
    const key = 'yue_studio_state'
    const st = JSON.parse(localStorage.getItem(key) || '{}')
    st.jobs = st.jobs || {}
    st.jobs[props.job.id] = {
      odStyle: odStyle.value, odGain: odGain.value, odChips: [...odChips.value],
      stemMute: stemMute.value, rollVoices: rollVoices.value,
      dspSel: dspSel.value, dspParams: dspParams.value,
    }
    localStorage.setItem(key, JSON.stringify(st))
  } catch {}
}

async function openRoll() {
  rollBusy.value = true
  rollErr.value = ''
  try {
    const d = await api.jobScore(props.job.id)
    rollData.value = d
    if (!rollVoices.value.length) {
      rollVoices.value = (d.voice_order || []).map((v) => ({ orig: v, name: v, on: true }))
    }
  } catch (e) {
    rollErr.value = String(e)
  } finally { rollBusy.value = false }
}

function barDensity(bar, voice) {
  const notes = (bar.voices && bar.voices[voice]) || 0
  const rests = (bar.rests && bar.rests[voice]) || 0
  if (!notes) return 0
  const ratio = notes / Math.max(1, notes + rests)
  return ratio > 0.66 ? 3 : ratio > 0.33 ? 2 : 1   // ▓ ▒ ░
}

function barSelStart(idx) {
  rollDrag = true
  rollSel.value = { a: idx, b: idx }
}

function barSelOver(idx) {
  if (!rollDrag) return
  const s = rollSel.value
  if (s && s.b !== idx) rollSel.value = { ...s, b: idx }
}

function barSelEnd() { rollDrag = false }

function isBarSel(idx) {
  const s = rollSel.value
  return !!s && idx >= Math.min(s.a, s.b) && idx <= Math.max(s.a, s.b)
}

function onWindowMouseup() { rollDrag = false }

async function makePreview() {
  const r = selRange.value
  if (!r || r.to - r.from < 1) return
  previewBusy.value = true
  try {
    const p = await api.jobPreview(props.job.id, r.from, r.to)
    await toggleArtifact(`p${props.job.id}:${p.file}`,
      `превью ${r.from.toFixed(0)}–${r.to.toFixed(0)} с · #${props.job.id}`,
      () => api.playFile(props.job.id, p.file, p.duration_sec))
  } catch (e) {
    rollErr.value = String(e)
  } finally { previewBusy.value = false }
}

// применить правки голосов к тексту ABC: выключенные голоса убираются,
// переименованные получают новое имя (V: Old → V: New), новые дописываются
function applyVoiceEdits(abc, voices, barsCount) {
  const rename = {}
  const removed = new Set()
  const added = []
  for (const v of voices) {
    if (v.orig && v.name.trim() && v.name.trim() !== v.orig) rename[v.orig] = v.name.trim()
    if (v.orig && !v.on) removed.add(v.orig)
    if (!v.orig && v.name.trim() && v.on) added.push(v.name.trim())
  }
  const out = []
  let curVoice = null
  for (const line of abc.split('\n')) {
    const t = line.trim()
    const vm = t.match(/^V:\s*(.+)$/)
    if (vm) {
      curVoice = vm[1].split(/\s+/)[0]
      if (removed.has(curVoice)) continue
      if (rename[curVoice]) {
        out.push(line.replace(new RegExp('^(\\s*)V:\\s*' + curVoice), '$1V: ' + rename[curVoice]))
        continue
      }
      out.push(line)
      continue
    }
    if (!t || /^[A-Za-z]:/.test(t)) { out.push(line); continue }   // заголовок/пустая
    // тело: строка текущего голоса
    if (curVoice && removed.has(curVoice)) continue
    out.push(line)
  }
  for (const name of added) {
    out.push(`V: ${name}`)
    out.push('z4 | ' + 'z4 | '.repeat(Math.max(0, (barsCount || 8) - 1)).trim())
  }
  return out.join('\n')
}

// голоса → редактор плана (партитура с правками), оттуда — рендер по своему ABC
async function voicesToPlan() {
  try {
    const r = await fetch(`/audio/${props.job.id}/${props.job.abc_file || 'score.abc'}`)
    const abc = await r.text()
    const bars = (rollData.value && rollData.value.bars || []).length
    emit('voices-to-plan', applyVoiceEdits(abc, rollVoices.value, bars))
  } catch (e) {
    rollErr.value = String(e)
  }
}

function addRollVoice() {
  rollVoices.value = [...rollVoices.value, { orig: null, name: '', on: true }]
}

async function makeMinus() {
  const exclude = Object.keys(stemMute.value).filter((k) => stemMute.value[k])
  if (!exclude.length) return
  rollBusy.value = true
  try {
    const r = await api.makeMinus(props.job.id, exclude)
    if (r && r.file) {
      toggleArtifact(`m${props.job.id}:minus`, `минус (−${exclude.join(', ')}) · #${props.job.id}`,
        () => api.playFile(props.job.id, r.file, props.job.duration_sec))
    }
  } catch (e) {
    rollErr.value = String(e)
  } finally { rollBusy.value = false }
}

function playStem(s) {
  toggleArtifact(`s${props.job.id}:${s.file}`, `${s.name} · #${props.job.id}`,
    () => api.playFile(props.job.id, s.file, props.job.duration_sec))
}

// ---------- Овердаб ----------

function odToggleChip(idx) {
  const cur = new Set(odChips.value)
  cur.has(idx) ? cur.delete(idx) : cur.add(idx)
  odChips.value = cur
}

// строка партии: чипы + ручной ввод; русский ввод переводится в английский (как стили)
async function odFinalStyle() {
  const chips = [...odChips.value].map((i) => odPartyChips[i].en)
  const manual = odStyle.value.trim()
  let s = [...chips, manual].filter(Boolean).join(', ')
  if (s && props.autoTranslate && /[а-яё]/i.test(s)) {
    try { s = (await api.translate(s)).text } catch {}
  }
  return s
}

async function submitOverdub() {
  const style = await odFinalStyle()
  if (!style) return
  odBusy.value = true
  try {
    await api.submitOverdub(props.job.id, style, odGain.value)
    emit('close')
  } catch (e) {
    rollErr.value = String(e)
  } finally { odBusy.value = false }
}

// ---------- DSP ----------

function chainLabel(file) {
  const isPrev = file.startsWith('dsp-preview-')
  const id = String(file).replace(/^dsp-preview-/, '').replace(/^dsp-/, '').replace(/\.flac$/, '')
  const c = dspChains.value.find((x) => x.id === id)
  return (c ? c.name : id) + (isPrev ? ' (превью 15с)' : '')
}

function selChain(chainId) {
  dspSel.value = chainId
  const c = dspChains.value.find((x) => x.id === chainId)
  const p = {}
  if (c) for (const prm of c.params) p[prm.id] = prm.default
  dspParams.value = p
}

async function ensureJobMetrics() {
  if (!jobMetrics.value) {
    try { jobMetrics.value = await api.analyze(props.job.id) } catch {}
  }
}

async function reloadVariants() {
  try { dspVariants.value = (await api.dspVariants(props.job.id)) || [] } catch {}
}

async function previewDsp() {
  const c = curChain.value
  if (!c) return
  dspBusy.value = true
  try {
    const v = await api.dspPreview(props.job.id, c.id, dspParams.value || {})
    await reloadVariants()
    if (v && v.file) playVariant(v)   // сразу слушаем кусок
  } catch (e) {
    rollErr.value = String(e)
  } finally { dspBusy.value = false }
}

async function applyDsp() {
  const c = curChain.value
  if (!c) return
  dspBusy.value = true
  try {
    await ensureJobMetrics()
    await api.applyDsp(props.job.id, c.id, dspParams.value || {})
    await reloadVariants()
  } catch (e) {
    rollErr.value = String(e)
  } finally { dspBusy.value = false }
}

function vDelta(v, key, dec = 1) {
  const a = jobMetrics.value
  if (!a || !v.metrics) return ''
  let av = a[key], bv = v.metrics[key]
  if (key.startsWith('bands.')) {
    const k = key.slice(6)
    av = a.bands && a.bands[k]
    bv = v.metrics.bands && v.metrics.bands[k]
  }
  if (av == null || bv == null) return ''
  const d = Number(bv) - Number(av)   // вариант минус исходник
  return (d > 0 ? '+' : '') + d.toFixed(dec)
}

function playVariant(v) {
  toggleArtifact(`v${props.job.id}:${v.file}`, `${chainLabel(v.file)} · #${props.job.id}`,
    () => api.playFile(props.job.id, v.file, props.job.duration_sec))
}

function openVariantMetrics(v) {
  emit('open-metrics', props.job, { metrics: v.metrics, title: `${chainLabel(v.file)} · #${props.job.id}` })
}

window.addEventListener('mouseup', onWindowMouseup)
onUnmounted(() => window.removeEventListener('mouseup', onWindowMouseup))
</script>

<template>
  <main class="settings-page studio-page">
    <section class="panel">
      <h2>{{ t('studio.title') }} <span class="muted">#{{ job.id }} {{ job.title }}</span></h2>
      <div class="roll-block" @mouseup="barSelEnd" @mouseleave="barSelEnd">
        <p v-if="rollBusy" class="muted">{{ t('studio.parsing') }}</p>
        <p v-if="rollErr" class="error">{{ rollErr }}</p>
        <template v-if="rollData">
          <p class="muted roll-meta">
            {{ rollData.tempo_bpm }} BPM · {{ rollData.key }} · {{ rollData.meter }} ·
            {{ rollData.bars.length }} тактов · ~{{ fmtDur(rollData.duration_sec) }}
            <template v-if="selRange"> · выделено {{ selRange.from.toFixed(0) }}–{{ selRange.to.toFixed(0) }} с</template>
          </p>
          <div class="roll-voices">
            <span class="muted">{{ t('studio.voices') }}</span>
            <div v-for="(v, vi) in rollVoices" :key="vi" class="roll-voice-edit">
              <button class="toggle" :class="{ on: v.on }" :title="v.on ? t('studio.voice.on.tip') : t('studio.voice.off.tip')" @click="v.on = !v.on; rollVoices = [...rollVoices]">{{ v.on ? t('studio.voice.on') : t('studio.voice.off') }}</button>
              <input v-model="v.name" :placeholder="v.orig ? v.orig : t('studio.voice.ph')" :disabled="!v.on" class="lib-name-input" :title="t('studio.voice.name.tip')" />
              <button class="ghost small-btn" v-if="v.orig" :title="t('studio.voice.del')" @click="rollVoices = rollVoices.filter((_, i) => i !== vi)">✕</button>
            </div>
            <button class="ghost small-btn" @click="addRollVoice">{{ t('studio.voice.add') }}</button>
            <button class="primary small" title="Партитура с правками голосов → редактор нот → рендер по ним. Модель следует плану приблизительно" @click="voicesToPlan">в план (правки голосов)</button>
          </div>

          <div v-for="(chunk, ci) in rollChunks" :key="ci" class="roll-grid"
               :style="{ gridTemplateColumns: `70px repeat(${chunk.length}, minmax(16px, 1fr))` }">
            <div></div>
            <div v-for="b in chunk" :key="'s' + b.idx" class="roll-sec" :title="b.section">{{ b.section.slice(0, 3) }}</div>
            <template v-for="v in rollData.voice_order" :key="v">
              <div class="roll-voice">{{ v }}</div>
              <div v-for="b in chunk" :key="v + b.idx"
                   class="roll-cell" :class="['d' + barDensity(b, v), { sel: isBarSel(b.idx), off: b.start_sec >= job.duration_sec }]"
                   :title="`${b.section} · такт ${b.idx + 1} · ${b.start_sec.toFixed(1)}–${b.end_sec.toFixed(1)}s${b.start_sec >= job.duration_sec ? ' · за пределами звука' : ''}`"
                   @mousedown.prevent="barSelStart(b.idx)" @mouseover="barSelOver(b.idx)"></div>
            </template>
            <div class="roll-voice">{{ t('studio.chords') }}</div>
            <div v-for="b in chunk" :key="'c' + b.idx" class="roll-chord">{{ (b.chords[0] || '') }}</div>
          </div>
          <div class="roll-actions">
            <button class="primary small" :disabled="!selRange || previewBusy" @click="makePreview">
              {{ previewBusy ? t('studio.preview.busy') : t('studio.preview') }}
            </button>
            <span class="muted">{{ t('studio.preview.hint') }}</span>
          </div>

          <div class="roll-stems">
            <div class="stems-inline">
              <span class="muted">{{ t('studio.stems') }}</span>
              <label v-for="nm in ['drums', 'bass', 'other', 'vocals']" :key="nm" class="stem-toggle">
                <button class="toggle" :class="{ on: !stemMute[nm] }"
                       :title="stemMute[nm] ? 'Выключено из минуса' : 'Присутствует в минусе'"
                       @click="stemMute = { ...stemMute, [nm]: !stemMute[nm] }">
                  {{ nm }}
                </button>
              </label>
              <button class="primary small" :disabled="rollBusy || !Object.values(stemMute).some(Boolean)"
                      :title="t('studio.minus.tip')" @click="makeMinus">
                {{ rollBusy ? '…' : t('studio.minus') }}
              </button>
              <span class="muted">{{ t('studio.minus.hint') }}</span>
            </div>
            <div v-for="st in stemsList" :key="st.file" class="stem-row">
              <button class="ghost play-mini" :class="{ stop: isPlaying('s' + job.id + ':' + st.file) }"
                      :disabled="playBusy['s' + job.id + ':' + st.file]" @click="playStem(st)">
                {{ playBtn('s' + job.id + ':' + st.file) }}
              </button>
              <strong>{{ st.name }}</strong>
            </div>
          </div>

          <details class="studio-sec">
            <summary>{{ t('studio.overdub') }} <span class="muted">{{ t('studio.overdub.sub') }}</span></summary>
            <p class="muted">{{ t('studio.overdub.desc') }}</p>
            <div class="od-chips">
              <button v-for="(c, ci) in odPartyChips" :key="ci" class="toggle"
                      :class="{ on: odChips.has(ci) }" @click="odToggleChip(ci)">{{ c.ru }}</button>
            </div>
            <div class="od-row">
              <input v-model="odStyle" :placeholder="t('studio.overdub.style.ph')" class="od-style" />
              <label class="od-gain">{{ t('studio.overdub.gain') }} <input type="range" min="0.1" max="1" step="0.05" v-model.number="odGain" /> {{ odGain }}</label>
              <button class="primary small" :disabled="odBusy || !odStyle || !odStyle.trim()" @click="submitOverdub">
                {{ odBusy ? '…' : t('studio.overdub.generate') }}
              </button>
            </div>
          </details>

          <details class="studio-sec">
            <summary>{{ t('studio.dsp') }} <span class="muted">{{ t('studio.dsp.sub') }}</span></summary>
            <div class="dsp-row">
              <VSelect :model-value="dspSel" :options="dspChains.map((c) => ({ value: c.id, label: c.name }))"
                       :placeholder="t('studio.dsp.chain')" style="max-width: 220px"
                       @update:model-value="(v) => selChain(v)" />
              <button class="primary small" :disabled="!dspSel || dspBusy" @click="applyDsp">
                {{ dspBusy ? t('studio.dsp.applying') : t('studio.dsp.apply') }}
              </button>
              <button class="ghost small-btn" :disabled="!dspSel || dspBusy"
                      :title="t('studio.dsp.preview.tip')" @click="previewDsp">
                {{ dspBusy ? '…' : t('studio.dsp.preview') }}
              </button>
              <button class="ghost" @click="emit('open-metrics', job, null)">{{ t('studio.dsp.metrics') }}</button>
            </div>
            <p v-if="curChain" class="muted dsp-note">{{ curChain.note }}</p>
            <div v-if="curChain" class="dsp-params">
              <label v-for="p in curChain.params" :key="p.id">
                <span class="dsp-plabel">{{ p.label }}</span>
                <input type="range" :min="p.min" :max="p.max" :step="p.step"
                       v-model.number="dspParams[p.id]" :disabled="dspBusy" />
                <span class="dsp-pval">{{ dspParams[p.id] }}</span>
              </label>
            </div>
            <div v-for="v in dspVariants" :key="v.file" class="dsp-variant">
              <button class="ghost play-mini" :class="{ stop: isPlaying('v' + job.id + ':' + v.file) }"
                      :disabled="playBusy['v' + job.id + ':' + v.file]" @click="playVariant(v)">
                {{ playBtn('v' + job.id + ':' + v.file) }}
              </button>
              <strong>{{ chainLabel(v.file) }}</strong>
              <span v-if="v.metrics" class="muted deltas">
                Δ крест {{ vDelta(v, 'crest_db') }} dB · Δ дин {{ vDelta(v, 'dyn_range_db') }} dB ·
                Δ верх {{ vDelta(v, 'bands.high') }}% · Δ флэтнес {{ vDelta(v, 'flatness_median', 3) }}
              </span>
              <span v-else class="muted">без метрик</span>
              <button class="ghost" @click="openVariantMetrics(v)">полная дельта</button>
            </div>
          </details>
        </template>
      </div>
      <div class="set-actions">
        <button class="ghost" @click="emit('close')">{{ t('common.back') }}</button>
      </div>
    </section>
  </main>
</template>
