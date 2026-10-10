<script setup>
// Страница «Инструменты»: короткая фраза (гитара записью, бас и барабаны нотами) играет по кругу через
// цепочку звукового движка воркера (педали → усилитель NAM → кабинет → пространство); любая правка — пересчёт
// круга через PHRASE_DEBOUNCE_MS и продолжение с того же места. Трек здесь не нужен: трек — это студия.
// Описание блоков — fxBlocks.json (копия worker/fx_blocks.json), логика цепочки — fxChain.js, готовые цепочки —
// fxPresets.js, семья фразы и место в круге — phraseLoop.js.
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import AppIcon from './AppIcon.vue'
import { useI18n } from '../i18n/index.js'
import { api } from '../api.js'
import { ensureKits as ensureKitsOn } from '../engineRun.js'
import { usePlayer } from '../composables/usePlayer.js'
import { useConfirm } from '../composables/useConfirm.js'
import { useInstruments } from '../composables/useInstruments.js'
import { instrumentFromPreset } from '../myInstruments.js'
import { installKit as installKitOn } from '../kitProgress.js'
import KitProgress from './KitProgress.vue'
import VSelect from '../VSelect.vue'
import BLOCKS from '../fxBlocks.json'
import { fxPresets } from '../fxPresets.js'
import ChainEditor from './ChainEditor.vue'
import { toWorkerChain, fromWorkerChain, missingRequired, fillAmp } from '../fxChain.js'
import { groupPresets, groupOptions, itemOptions, groupOf } from '../presetGroups.js'
import { familyOf, phraseStems, pagePresets, phaseAfter, phraseFamily, phraseChain } from '../phraseLoop.js'

const { t, locale } = useI18n()
const emit = defineEmits(['close'])
const { nowPlaying, nowPlayingKey, refreshPlayer } = usePlayer()
const { askConfirm } = useConfirm()
const { all: instruments, reload: reloadMine } = useInstruments()

const PHRASE_DEBOUNCE_MS = 250   // пересчёт — после последней правки, а не на каждый шаг крутилки
const LOOP_ID = 2 ** 40          // «трек» круга в плеере (Go: phraseLoopID)
const LOOP_SEC = 180             // длина разложенного круга (Go: loopSeconds); у конца — запуск заново
const LOOP_KEY = 'instr-loop'
const TEMPO_MIN = 0.5
const TEMPO_MAX = 1.5

// готовые и свои; перкуссия без группы — своя группа «Перкуссия по сетке» (в списке «Разное» её не найти)
const withGroup = (p) => (p.group || familyOf(p) !== 'perc' ? p : { ...p, group: 'perc' })
const presets = computed(() => pagePresets(instruments.value).map(withGroup))
const first = pagePresets(fxPresets)[0]
const chain = ref(fromWorkerChain(first.chain, BLOCKS))
const presetId = ref(first.id)
const phrases = ref([])
const phraseId = ref('')
const tempo = ref(1)
const bypass = ref(false)
const assets = ref({ amps: [], irs: [] })
const engineOn = ref(true)
const phrasesKnown = ref(true)   // воркер умеет фразы (в /config есть fx_phrases); до ответа — не пугать
const playing = ref(false)
const busy = ref('')
const err = ref('')
const note = ref('')

const tr = (lbl) => (lbl && (lbl[locale.value] || lbl.ru)) || ''
const preset = computed(() => presets.value.find((p) => p.id === presetId.value))
const family = computed(() => familyOf(preset.value))
const presetGroups = computed(() => groupPresets(presets.value, locale.value))
// два списка: группа и инструмент в ней; поиск — только внутри группы
const groupId = ref('')
const groupOpts = computed(() => groupOptions(presetGroups.value, t('instr.group.other')))
const itemOpts = computed(() => itemOptions(presetGroups.value.find((g) => g.group === groupId.value), locale.value))
watch([presetGroups, presetId], () => {
  if (!presetGroups.value.some((g) => g.group === groupId.value && g.items.some((p) => p.id === presetId.value))) {
    groupId.value = groupOf(presetGroups.value, presetId.value)
  }
}, { immediate: true })
// выбрали другую группу — инструмент из неё (иначе второй список показывал бы чужой пресет голым id)
watch(groupId, (g) => {
  const grp = presetGroups.value.find((x) => x.group === g)
  if (grp && grp.items.length && !grp.items.some((p) => p.id === presetId.value)) applyPreset(grp.items[0].id)
})
const familyPhrases = computed(() => phrases.value.filter((p) => p.family === phraseFamily(family.value)))
const phraseOpts = computed(() => familyPhrases.value.map((p) => ({ value: p.id, label: tr(p.name) })))
const phrase = computed(() => phrases.value.find((p) => p.id === phraseId.value) || null)
// семья сменилась (гитара → барабаны) — фраза этой семьи; у синта — фраза его стиля (пэд, арпеджио…)
watch([familyPhrases, presetId], ([list]) => {
  const own = preset.value?.style && list.find((p) => p.style === preset.value.style)
  if (own && family.value === 'synth') phraseId.value = own.id
  else if (!list.some((p) => p.id === phraseId.value)) phraseId.value = list[0]?.id || ''
})
const missing = computed(() => missingRequired(chain.value, BLOCKS))
const ready = computed(() => engineOn.value && phrasesKnown.value && !!phraseId.value && !missing.value.length)

async function loadAssets() {
  try { assets.value = (await api.fxAssets()) || { amps: [], irs: [] } } catch { assets.value = { amps: [], irs: [] } }
}

async function loadConfig() {
  try {
    const c = await api.workerConfig()
    phrasesKnown.value = !!(c && c.fx_phrases)
    engineOn.value = !!(c && c.fx_engine)
  } catch { phrasesKnown.value = false }
}

async function loadPhrases() {
  try {
    phrases.value = (await api.fxPhrases()) || []
  } catch (e) {
    phrases.value = []
    err.value = String(e)
  }
}

onMounted(async () => {
  reloadMine()                        // свои инструменты: первая загрузка могла упасть (старый воркер, сеть)
  await Promise.all([loadAssets(), loadConfig()])
  await loadPhrases()   // и у «старого» по /config воркера: не знает — ошибка текстом, а не пустой список
  chain.value = withAmp(chain.value)   // пресет по умолчанию с усилителем — захват по подсказке пресета
})

const onKey = (e) => { if (e.key === 'Escape') emit('close') }
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  if (playing.value) stopLoop()      // закрыли страницу — круг не играет ещё три минуты сам по себе
})

// захват не выбран — по подсказке пресета (amp_hint), иначе первый загруженный: пресет с amp без захвата молчит
const withAmp = (c) => fillAmp(c, assets.value.amps, preset.value?.amp_hint)

function applyPreset(id) {
  presetId.value = id
  const p = presets.value.find((x) => x.id === id)
  if (p) chain.value = withAmp(fromWorkerChain(p.chain, BLOCKS))
}

// ---------- свои инструменты ----------

const nameMode = ref('')         // '' | 'new' — «сохранить как мой», 'rename' — переименовать свой
const myName = ref('')

function askName(mode) {
  nameMode.value = mode
  myName.value = mode === 'rename' ? preset.value?.title || '' : ''
}

async function saveName() {
  const name = myName.value.trim()
  if (!name) return
  err.value = ''
  try {
    if (nameMode.value === 'rename') {
      const body = instrumentFromPreset(preset.value, preset.value.chain, name)
      await api.fxInstrumentUpdate(preset.value.wid, body)
      await reloadMine()
    } else {
      const r = await api.fxInstrumentCreate(instrumentFromPreset(preset.value, toWorkerChain(chain.value), name))
      await reloadMine()
      presetId.value = `my-${r.id}`      // новый свой сразу выбран; цепочка в редакторе та же
    }
    nameMode.value = ''
  } catch (e) { err.value = String(e) }
}

// свой: заменить цепочку текущей из редактора
async function saveMine() {
  err.value = ''
  try {
    await api.fxInstrumentUpdate(preset.value.wid, instrumentFromPreset(preset.value, toWorkerChain(chain.value), preset.value.title))
    await reloadMine()
    note.value = t('instr.my.saved')
  } catch (e) { err.value = String(e) }
}

function deleteMine() {
  const p = preset.value
  askConfirm(t('instr.my.del.title'), p.title, async () => {
    err.value = ''
    try {
      await api.fxInstrumentDelete(p.wid)
      await reloadMine()
      // после удаления — первый инструмент той же группы, иначе первый вообще
      const grp = presetGroups.value.find((g) => g.group === p.group)
      applyPreset((grp && grp.items[0]?.id) || presets.value[0].id)
    } catch (e) { err.value = String(e) }
  })
}

// наборы сэмплов, которых нет на воркере, — скачать перед расчётом (по требованию)
async function ensureKits(workerChain) {
  const got = await ensureKitsOn(api, workerChain, assets.value.kits, () => { busy.value = 'kit' })
  if (got.length) await loadAssets()
}

async function installKit(name) {
  err.value = ''
  busy.value = 'kit'
  try { await installKitOn(api, name); await loadAssets() } catch (e) { err.value = String(e) } finally { busy.value = '' }
}

async function upload(kind) {
  err.value = ''
  try {
    const r = await api.uploadFxAsset(kind)
    if (r) { await loadAssets(); chain.value = withAmp(chain.value) }
  } catch (e) { err.value = String(e) }
}

// ---------- круг ----------

let loop = null          // {phase, cycle}: с какого места круга запущен файл и длина круга
let playQueue = Promise.resolve()   // загрузки круга в плеер — строго по одной

function enqueuePlay(fn) {
  const next = playQueue.then(fn, fn)
  playQueue = next.catch(() => false)
  return next
}
let seq = 0              // ответ устаревшего расчёта (успели покрутить ещё) — отбросить
let timer = null
let poll = null

function request() {
  const tmp = Number(tempo.value)
  // ноты синта и удары перкуссии — по тактам круга этой фразы и темпа
  return { phrase: phraseId.value, tempo: tmp, chain: phraseChain(toWorkerChain(chain.value), preset.value, phrase.value, tmp),
    stems: phraseStems(preset.value), bypass: bypass.value }
}

// посчитать круг и запустить его с места, где играл прежний (доля круга та же)
async function render() {
  const my = ++seq
  const req = request()              // весь запрос — до первого await (скачивание набора)
  err.value = ''
  try {
    // без обработки воркер оставляет только блоки, которые сами играют ноты (synth, perc), — качать их наборы, но не
    // наборы эффектов: сухая фраза не должна ждать (или падать без сети) из-за набора, который не прозвучит
    await ensureKits(req.bypass ? req.chain.filter((b) => b.type === 'synth' || b.type === 'perc') : req.chain)
    if (my !== seq || !playing.value) return   // остановили или покрутили, пока качался набор
    busy.value = 'loop'
    const r = await api.fxPhrase(req)
    if (my !== seq || !playing.value) return
    let phase = 0
    if (loop) {
      const st = await api.audioState()
      if (st && st.job_id === LOOP_ID) phase = phaseAfter(loop.phase, st.position_sec || 0, loop.cycle, r.cycle_sec)
    }
    // загрузки в плеер — по очереди и только актуальная: старый запуск, дождавшийся ответа позже нового (стоп →
    // другие настройки → ▶), не подменяет звук и состояние круга
    const played = await enqueuePlay(async () => {
      if (my !== seq || !playing.value) return false
      await api.playLoop(r.file, phase)
      if (my !== seq || !playing.value) {   // остановили или перезапустили, пока плеер грузил круг
        if (!playing.value) await api.stopAudio().catch(() => { /* плеер уже стоит */ })
        return false
      }
      return true
    })
    if (!played) return
    loop = { phase, cycle: r.cycle_sec }
    note.value = r.clipped ? t('instr.clipped') : ''
    nowPlaying.value = `${t('instr.title')}: ${phraseOpts.value.find((o) => o.value === req.phrase)?.label || ''}`
    nowPlayingKey.value = LOOP_KEY
  } catch (e) {
    if (my === seq) err.value = String(e)
  } finally {
    if (my === seq || !playing.value) busy.value = ''   // остановлен — занятость снимается (иначе редактор выключен)
  }
}

function schedule() {
  if (!playing.value) return
  clearTimeout(timer)
  timer = setTimeout(render, PHRASE_DEBOUNCE_MS)
}

// плеер занят другим (▶ трека) — круг остановлен; у конца разложенного файла — тот же круг заново
async function watchPlayer() {
  if (!playing.value || !loop || busy.value) return
  let st
  try { st = await api.audioState() } catch { return }
  if (!st || st.job_id !== LOOP_ID) { playing.value = false; loop = null; clearInterval(poll); return }
  if (st.position_sec > LOOP_SEC - 5) schedule()
}

async function startLoop() {
  if (!ready.value) return
  playing.value = true
  loop = null
  clearInterval(poll)
  poll = setInterval(watchPlayer, 1000)
  await render()
  if (err.value) stopLoop()
}

async function stopLoop() {
  playing.value = false
  seq++
  loop = null
  clearTimeout(timer)
  clearInterval(poll)
  busy.value = ''
  try { await api.stopAudio() } catch { /* плеер уже стоит */ }
  if (nowPlayingKey.value === LOOP_KEY) nowPlayingKey.value = ''
  refreshPlayer()
}

watch([chain, phraseId, tempo, bypass], schedule, { deep: true })
</script>

<template>
  <div class="modal-backdrop page-backdrop">
    <section class="panel page-modal">
      <div class="page-modal-head">
        <h2>{{ t('instr.title') }}</h2>
        <button class="ghost icon" :title="t('common.close')" @click="emit('close')"><AppIcon name="x" /></button>
      </div>
      <div class="page-modal-body">
        <p class="muted">{{ t('instr.desc') }}</p>
        <p v-if="!phrasesKnown" class="err">{{ t('instr.phrases.old') }}</p>
        <p v-else-if="!engineOn" class="err">{{ t('instr.engine.off') }}</p>
        <p v-if="err" class="err">{{ err }}</p>
        <p v-if="note" class="muted">{{ note }}</p>
        <KitProgress />

        <div class="instr-pick">
          <label>
            <span class="muted">{{ t('instr.group') }}</span>
            <VSelect v-model="groupId" :options="groupOpts" />
          </label>
          <label>
            <span class="muted">{{ t('instr.item') }}</span>
            <VSelect :model-value="presetId" :options="itemOpts" searchable @update:model-value="applyPreset" />
          </label>
          <label>
            <span class="muted" :title="t('instr.phrase.tip')">{{ t('instr.phrase') }}</span>
            <VSelect v-model="phraseId" :options="phraseOpts" />
          </label>
        </div>
        <p v-if="preset" class="muted voice-hint">{{ tr(preset.note) }}</p>
        <div class="corpus-actions">
          <template v-if="!nameMode">
            <button class="ghost small-btn" :title="t('instr.my.saveAs.tip')" @click="askName('new')">{{ t('instr.my.saveAs') }}</button>
            <template v-if="preset?.mine">
              <button class="ghost small-btn" :title="t('instr.my.save.tip')" @click="saveMine">{{ t('instr.my.save') }}</button>
              <button class="ghost small-btn" @click="askName('rename')">{{ t('instr.my.rename') }}</button>
              <button class="ghost small-btn" @click="deleteMine">{{ t('instr.my.del') }}</button>
            </template>
          </template>
          <template v-else>
            <input v-model="myName" class="instr-name" maxlength="80" :placeholder="t('instr.my.name')" @keydown.enter="saveName" />
            <button class="primary small-btn" :disabled="!myName.trim()" @click="saveName">{{ t('instr.my.ok') }}</button>
            <button class="ghost small-btn" @click="nameMode = ''">{{ t('common.cancel') }}</button>
          </template>
        </div>
        <p v-if="phrasesKnown && phrases.length && !familyPhrases.length" class="muted">{{ t('instr.phrases.none') }}</p>

        <div class="corpus-actions">
          <button class="primary" :disabled="!playing && !ready" :title="t('instr.loop.tip')"
                  @click="playing ? stopLoop() : startLoop()">
            <AppIcon :name="playing ? 'stop' : 'play'" /> {{ playing ? t('instr.loop.stop') : t('instr.loop.play') }}
          </button>
          <label class="instr-on" :title="t('instr.fx.tip')"><input type="checkbox" :checked="!bypass" @change="bypass = !$event.target.checked" /> {{ t('instr.fx') }}</label>
          <label class="instr-tempo" :title="t('instr.tempo.tip')">
            <span>{{ t('instr.tempo') }}</span>
            <input v-model.number="tempo" type="range" :min="TEMPO_MIN" :max="TEMPO_MAX" step="0.05" />
            <span class="dsp-pval">×{{ Number(tempo).toFixed(2) }}</span>
          </label>
          <span v-if="busy === 'loop'" class="muted">{{ t('instr.loop.busy') }}</span>
          <span v-else-if="busy === 'kit'" class="muted">{{ t('instr.kit.busy') }}</span>
        </div>

        <ChainEditor v-model="chain" :assets="assets" :busy="busy" @upload="upload" @install-kit="installKit" />
        <p v-if="missing.length" class="err">{{ t('instr.needAmp') }}</p>
      </div>
    </section>
  </div>
</template>

<style scoped>
.instr-on { display: flex; align-items: center; gap: 6px; font-size: 13px; }
.instr-tempo { display: flex; align-items: center; gap: 8px; font-size: 13px; }
.instr-pick { display: flex; flex-wrap: wrap; gap: 12px; margin: 8px 0; }
.instr-name { min-width: 240px; }
.instr-pick label { display: flex; flex-direction: column; gap: 4px; min-width: 220px; }
</style>
