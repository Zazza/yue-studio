<script setup>
// Страница «Голоса» (примерочная): ручки характера → дескриптор вокала,
// короткое draft-прослушивание на нейтральном стенде, карточки голосов.
// Карточка = ручки + seed прослушивания; аудио копируется на воркере,
// так что карточка переживает удаление исходной джобы из очереди.
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from '../i18n/index.js'
const { t } = useI18n()
import { api } from '../api.js'
import { usePlayer } from '../composables/usePlayer.js'
import { useConfirm } from '../composables/useConfirm.js'
import VSelect from '../VSelect.vue'
import {
  PARAMS_VERSION, VOICE_REGISTERS, VOICE_KNOBS, VOICE_PRESETS, VOICE_ARCHETYPES,
  defaultVoiceParams, normalizeVoiceParams, voiceDescriptor,
  auditionStyle, auditionJob, needsTranslate,
} from '../voiceLab.js'

const emit = defineEmits(['close', 'apply-voice'])
const { askConfirm } = useConfirm()
const { toggleArtifact, playBtn } = usePlayer()

const params = ref(defaultVoiceParams())
const seedInput = ref('')          // пусто → новый случайный seed на каждое прослушивание
const usedSeed = ref(0)
const auditionId = ref(null)
const audition = ref(null)
const busy = ref(false)
const err = ref('')
const name = ref('')
const voices = ref([])
const autoplayedId = ref(0)      // какая проба уже включалась сама (не повторять на ре-полле)

let pollTimer = null
const descriptor = computed(() => voiceDescriptor(params.value))
const registerOptions = computed(() => VOICE_REGISTERS.map(r => ({ value: r, label: t('voicelab.register.' + r) })))

async function loadVoices() {
  try { voices.value = (await api.voices()) || [] } catch {}
}

onMounted(loadVoices)
onBeforeUnmount(stopPoll)

function stopPoll() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
}

async function poll() {
  if (!auditionId.value) return
  try {
    const jobs = await api.jobs()
    const j = (jobs || []).find(x => x.id === auditionId.value)
    if (!j) return
    audition.value = j
    if (j.status === 'done') {
      stopPoll()
      // проба готова — включаем сами: «прослушать» и должно звучать само
      if (autoplayedId.value !== auditionId.value) {
        autoplayedId.value = auditionId.value
        await toggleArtifact('audition', t('voicelab.audition.jobTitle'),
          () => api.playAudio(auditionId.value))
      }
    } else if (['error', 'canceled'].includes(j.status)) stopPoll()
  } catch {}
}

// Прослушивание: seed всегда конкретный (воркер не возвращает использованный
// случайный seed — иначе голос не воспроизвести повторно).
async function runAudition() {
  busy.value = true
  err.value = ''
  try {
    let seed = Number.parseInt(seedInput.value, 10)
    if (!Number.isFinite(seed) || seed <= 0) seed = Math.floor(Math.random() * 2 ** 31)
    usedSeed.value = seed
    seedInput.value = String(seed)
    let desc = descriptor.value
    if (needsTranslate(desc)) {
      try { desc = (await api.translate(desc)).text || desc } catch {}
    }
    const id = await api.submit({ ...auditionJob(params.value, seed), style: auditionStyle(desc) })
    auditionId.value = id
    audition.value = null
    stopPoll()
    pollTimer = setInterval(poll, 3000)
    poll()
  } catch (e) { err.value = String(e) } finally { busy.value = false }
}

const canSave = computed(() =>
  name.value.trim() && audition.value && audition.value.status === 'done')

async function save() {
  if (!canSave.value) return
  busy.value = true
  err.value = ''
  try {
    await api.voiceCreate(name.value.trim(), auditionId.value,
      JSON.stringify({ v: PARAMS_VERSION, ...normalizeVoiceParams(params.value) }), usedSeed.value)
    name.value = ''
    await loadVoices()
  } catch (e) { err.value = String(e) } finally { busy.value = false }
}

// пресет задаёт стартовые ручки — дальше пользователь докручивает руками
function applyPreset(p) {
  params.value = normalizeVoiceParams(p.params)
}

function cardParams(v) {
  try { return normalizeVoiceParams(JSON.parse(v.params || '{}')) } catch { return defaultVoiceParams() }
}
const cardDescriptor = (v) => voiceDescriptor(cardParams(v))
const cardPlay = (v) => toggleArtifact('v' + v.id, v.name, () => api.playAudio(v.job_id))

// «Переспросить»: ручки и seed из карточки — тот же голос заново.
function reaudition(v) {
  params.value = cardParams(v)
  seedInput.value = String(v.seed || '')
  runAudition()
}

function toForm(v) {
  emit('apply-voice', { vocals: cardDescriptor(v), seed: v.seed })
}

function del(v) {
  askConfirm(t('voicelab.delete.title', { name: v.name }), t('voicelab.delete.body'),
    async () => {
      try { await api.voiceDelete(v.id) } catch (e) { alert(String(e)) }
      loadVoices()
    })
}

// разовая уборка: черновики прослушивания засоряют список треков, руками удалять лень.
// Карточки не трогаем — их аудио-копии хранятся отдельно от джоб.
const cleaning = ref(false)

function cleanProbes() {
  askConfirm(t('voicelab.clean.title'), t('voicelab.clean.body'), async () => {
    cleaning.value = true
    try {
      const jobs = await api.jobs()
      const probes = (jobs || []).filter((x) => x.draft && x.title === 'voice audition')
      for (const p of probes) {
        try { await api.deleteJob(p.id) } catch { /* уже нет — идём дальше */ }
      }
      if (probes.some((p) => p.id === auditionId.value)) {
        auditionId.value = null
        audition.value = null
        stopPoll()
      }
    } catch (e) { err.value = String(e) } finally { cleaning.value = false }
  })
}
</script>

<template>
  <main class="settings-page">
    <section class="panel">
      <h2>{{ t('voicelab.title') }}</h2>
      <p class="muted">{{ t('voicelab.desc') }}</p>

      <div class="voice-presets">
        <span class="muted">{{ t('voicelab.preset.label') }}</span>
        <button v-for="p in VOICE_PRESETS" :key="p.id" class="ghost small-btn"
                :title="voiceDescriptor(p.params)" @click="applyPreset(p)">
          {{ t('voicelab.preset.' + p.id) }}
        </button>
      </div>
      <div class="voice-presets">
        <span class="muted">{{ t('voicelab.archetype.label') }}</span>
        <button v-for="a in VOICE_ARCHETYPES" :key="a.id" class="ghost small-btn"
                :title="voiceDescriptor(a.params)" @click="applyPreset(a)">
          {{ t('voicelab.archetype.' + a.id) }}
        </button>
      </div>

      <div class="dsp-params">
        <label>
          <span>{{ t('voicelab.knob.register') }}</span>
          <VSelect v-model="params.register" :options="registerOptions" />
          <span></span>
        </label>
        <label v-for="k in VOICE_KNOBS" :key="k.key">
          <span>{{ t('voicelab.knob.' + k.key) }}</span>
          <input v-model.number="params[k.key]" type="range" min="0" :max="k.max" step="1" />
          <span class="dsp-pval">{{ t('voicelab.' + k.key + '.' + params[k.key]) }}</span>
        </label>
        <label>
          <span :title="t('voicelab.extra.tip')">{{ t('voicelab.extra') }}</span>
          <input v-model="params.extra" :placeholder="t('voicelab.extra.ph')" />
          <span></span>
        </label>
        <label>
          <span :title="t('voicelab.seed.tip')">{{ t('voicelab.seed') }}</span>
          <input v-model="seedInput" :placeholder="t('voicelab.seed.tip')" inputmode="numeric" />
          <span></span>
        </label>
      </div>
      <p class="muted voice-hint">{{ t('voicelab.extra.hint') }}</p>
      <p class="muted voice-preview" :title="t('voicelab.preview')">{{ descriptor }}</p>

      <div class="corpus-actions">
        <button class="primary" :disabled="busy" :title="t('voicelab.audition.tip')" @click="runAudition">
          {{ busy ? t('voicelab.audition.busy') : t('voicelab.audition') }}
        </button>
        <template v-if="audition">
          <span v-if="['queued', 'running'].includes(audition.status)" class="muted">
            {{ t('queue.status.' + audition.status) }} · {{ t('voicelab.audition.wait') }}<template v-if="audition.stage"> · {{ audition.stage }}</template><template v-if="audition.elapsed_s"> {{ Math.round(audition.elapsed_s) }} с</template>
          </span>
          <template v-if="audition.status === 'done'">
            <button class="ghost small-btn" :title="t('voicelab.audition.replay')"
                    @click="toggleArtifact('audition', t('voicelab.audition.jobTitle'), () => api.playAudio(auditionId))">
              {{ playBtn('audition') }}
            </button>
            <span class="muted">{{ t('voicelab.audition.ready') }}</span>
          </template>
          <span v-if="audition.status === 'error'" class="error">{{ audition.error }}</span>
        </template>
      </div>

      <div v-if="audition && audition.status === 'done'" class="corpus-new">
        <input v-model="name" :placeholder="t('voicelab.save.name')" @keyup.enter="save" />
        <button class="ghost" :disabled="busy || !canSave" @click="save">{{ t('voicelab.save') }}</button>
      </div>
      <p v-if="err" class="error">{{ err }}</p>

      <h2 style="margin-top:18px">{{ t('voicelab.cards') }}
        <button class="ghost small-btn" style="margin-left:10px" :disabled="cleaning"
                :title="t('voicelab.clean.tip')" @click="cleanProbes">{{ t('voicelab.clean') }}</button>
      </h2>
      <div v-for="v in voices" :key="v.id" class="corpus-item voice-card">
        <strong>{{ v.name }}</strong>
        <span class="muted voice-card-desc">{{ cardDescriptor(v) }}</span>
        <span class="muted">seed {{ v.seed || '—' }}</span>
        <span class="spacer"></span>
        <button v-if="v.job_alive" class="ghost small-btn" @click="cardPlay(v)">{{ playBtn('v' + v.id) }}</button>
        <span v-else class="muted">{{ t('voicelab.card.sourceGone') }}</span>
        <button class="ghost small-btn" @click="reaudition(v)">{{ t('voicelab.card.reaudition') }}</button>
        <button class="primary small" @click="toForm(v)">{{ t('voicelab.card.toForm') }}</button>
        <button class="ghost small-btn" @click="del(v)">✕</button>
      </div>
      <p v-if="!voices.length" class="muted">{{ t('voicelab.empty') }}</p>

      <div class="set-actions">
        <button class="ghost" @click="emit('close')">{{ t('common.back') }}</button>
      </div>
    </section>
  </main>
</template>
