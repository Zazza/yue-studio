<script setup>
// Страница «Свои треки»: импорт трека в студию + профили исполнителей из корпусов.
import { ref, onMounted, onUnmounted } from 'vue'
import { useI18n } from '../i18n/index.js'
const { t } = useI18n()
import { api } from '../api.js'

const emit = defineEmits(['close', 'imported', 'apply-style', 'apply-abc', 'style-to-library'])

// модалка: Esc закрывает
const onKey = (e) => { if (e.key === 'Escape') emit('close') }
onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))

const importBusy = ref(false)
const corpora = ref([])
const corpusName = ref('')
const corpusBusy = ref(false)
const corpusErr = ref('')
const corpusProfile = ref({}) // id -> profile
const corpusTracks = ref({}) // id -> разбор треков

async function loadCorpora() {
  try {
    corpora.value = (await api.corpusList()) || []
    for (const c of corpora.value) {
      if (c.tracks > 0 && !corpusTracks.value[c.id]) {
        try { corpusTracks.value = { ...corpusTracks.value, [c.id]: await api.corpusTracks(c.id) } } catch {}
      }
    }
  } catch {}
}

onMounted(loadCorpora)

async function importTrack() {
  importBusy.value = true
  try {
    const r = await api.importTrack()
    if (r && r.id) emit('imported', r)
  } finally { importBusy.value = false }
}

async function create() {
  if (!corpusName.value.trim()) return
  corpusBusy.value = true
  corpusErr.value = ''
  try {
    await api.corpusCreate(corpusName.value.trim())
    corpusName.value = ''
    await loadCorpora()
  } catch (e) { corpusErr.value = String(e) } finally { corpusBusy.value = false }
}

async function addTracks(c) {
  corpusBusy.value = true
  corpusErr.value = ''
  try {
    await api.corpusAddTracks(c.id)
    delete corpusTracks.value[c.id]
    corpusTracks.value = { ...corpusTracks.value }
    await loadCorpora()
  } catch (e) { corpusErr.value = String(e) } finally { corpusBusy.value = false }
}

async function build(c) {
  corpusBusy.value = true
  corpusErr.value = ''
  try {
    const p = await api.corpusBuild(c.id)
    corpusProfile.value = { ...corpusProfile.value, [c.id]: p }
    await loadCorpora()
  } catch (e) { corpusErr.value = String(e) } finally { corpusBusy.value = false }
}

async function show(c) {
  if (!corpusProfile.value[c.id]) {
    try { corpusProfile.value = { ...corpusProfile.value, [c.id]: await api.corpusGet(c.id) } } catch (e) {
      corpusErr.value = String(e)
    }
  }
}

function applyStyle(p) {
  if (p && p.style) emit('apply-style', p.style)
}

function applyAbc(p) {
  if (p && p.abc_template) emit('apply-abc', p.abc_template)
}
</script>

<template>
  <div class="modal-backdrop page-backdrop" @click.self="emit('close')">
    <section class="panel lib page-modal">
      <div class="page-modal-head">
        <h2>{{ t('corpus.title') }}</h2>
        <button class="ghost icon" :title="t('common.close')" @click="emit('close')">✕</button>
      </div>
      <div class="page-modal-body">
      <p class="muted">
        {{ t('corpus.import.desc') }}
      </p>
      <div class="corpus-actions">
        <button class="primary" :disabled="importBusy" @click="importTrack">
          {{ importBusy ? t('corpus.import.busy') : t('corpus.import.btn') }}
        </button>
      </div>

      <h2 style="margin-top:18px">{{ t('corpus.profiles') }} <span class="muted">{{ t('corpus.profiles.sub') }}</span></h2>
      <div class="corpus-new">
        <input v-model="corpusName" :placeholder="t('corpus.profile.name')" @keyup.enter="create" />
        <button class="ghost" :disabled="corpusBusy || !corpusName.trim()" @click="create">{{ t('corpus.profile.create') }}</button>
      </div>
      <p v-if="corpusErr" class="error">{{ corpusErr }}</p>
      <div v-for="c in corpora" :key="c.id" class="corpus-item">
        <strong>{{ c.name }}</strong>
        <span class="muted">{{ c.tracks }} трек(ов)</span>
        <span class="spacer"></span>
        <button class="ghost small-btn" :disabled="corpusBusy" @click="addTracks(c)">{{ t('corpus.tracks.add') }}</button>
        <button class="ghost small-btn" :disabled="corpusBusy || !c.tracks" @click="build(c)">
          {{ corpusBusy ? '…' : t('corpus.profile.build') }}
        </button>
        <button v-if="c.has_profile" class="ghost small-btn" @click="show(c)">{{ t('corpus.profile.show') }}</button>
        <div class="corpus-tracks">
          <div v-for="t in corpusTracks[c.id] || []" :key="t.filename" class="corpus-track">
            <strong>{{ t.filename }}</strong>
            <span class="muted">
              {{ t.tempo_bpm ? t.tempo_bpm.toFixed(0) + ' BPM' : '—' }} ·
              {{ t.key || 'тональность —' }} ·
              {{ (t.top_chords || []).join(' ') || 'аккорды —' }}
            </span>
            <span v-if="t.structure && t.structure.length" class="muted">{{ t.structure.join(' → ') }}</span>
            <p v-if="t.lyrics_head" class="muted track-lyrics">«{{ t.lyrics_head }}…»</p>
            <p v-if="t.abc_error" class="error">транскрипция не удалась: {{ t.abc_error }}</p>
          </div>
        </div>
        <div v-if="corpusProfile[c.id]" class="corpus-profile">
          <p class="muted">
            темп {{ corpusProfile[c.id].tempo_median }} BPM ({{ (corpusProfile[c.id].tempo_range || []).join('–') }}) ·
            тональности {{ Object.keys(corpusProfile[c.id].keys || {}).slice(0, 3).join(', ') }}
          </p>
          <p class="muted style">{{ corpusProfile[c.id].style }}</p>
          <div class="corpus-actions">
            <button class="primary small" @click="applyStyle(corpusProfile[c.id])">{{ t('corpus.profile.style') }}</button>
            <button class="ghost small-btn" @click="emit('style-to-library', { corpus: c, profile: corpusProfile[c.id] })">{{ t('corpus.style.toLib') }}</button>
            <button class="ghost small-btn" title="Нотный шаблон профиля — в редактор плана (для продвинутых)" @click="applyAbc(corpusProfile[c.id])">ноты → в план</button>
          </div>
        </div>
      </div>
      <p v-if="!corpora.length" class="muted">{{ t('corpus.desc') }}</p>
      </div>
    </section>
  </div>
</template>
