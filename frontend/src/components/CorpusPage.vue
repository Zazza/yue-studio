<script setup>
// Страница «Свои треки»: импорт трека в студию + профили исполнителей из корпусов.
import { ref, onMounted } from 'vue'
import { api } from '../api.js'

const emit = defineEmits(['close', 'imported', 'apply-style', 'apply-abc', 'style-to-library'])

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
  <main class="settings-page">
    <section class="panel lib">
      <h2>Свой трек → студия</h2>
      <p class="muted">
        Загрузите готовый трек (flac/mp3/wav/ogg/m4a) — в его студии будут работать стемы и минус-трек,
        эффекты звука, ролл по транскрипции и овердаб. Изменение инструментов — минусом по стемам или кавером, не правкой оригинала.
      </p>
      <div class="corpus-actions">
        <button class="primary" :disabled="importBusy" @click="importTrack">
          {{ importBusy ? 'импортирую…' : '＋ импортировать трек в студию' }}
        </button>
      </div>

      <h2 style="margin-top:18px">Профили из корпуса <span class="muted">(треки исполнителя: один — разбор, несколько — усреднение)</span></h2>
      <div class="corpus-new">
        <input v-model="corpusName" placeholder="Имя профиля (напр. «блюз 60-х»)" @keyup.enter="create" />
        <button class="ghost" :disabled="corpusBusy || !corpusName.trim()" @click="create">создать</button>
      </div>
      <p v-if="corpusErr" class="error">{{ corpusErr }}</p>
      <div v-for="c in corpora" :key="c.id" class="corpus-item">
        <strong>{{ c.name }}</strong>
        <span class="muted">{{ c.tracks }} трек(ов)</span>
        <span class="spacer"></span>
        <button class="ghost small-btn" :disabled="corpusBusy" @click="addTracks(c)">＋ треки…</button>
        <button class="ghost small-btn" :disabled="corpusBusy || !c.tracks" @click="build(c)">
          {{ corpusBusy ? '…' : 'собрать профиль' }}
        </button>
        <button v-if="c.has_profile" class="ghost small-btn" @click="show(c)">профиль</button>
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
            <button class="primary small" @click="applyStyle(corpusProfile[c.id])">стиль → в форму</button>
            <button class="ghost small-btn" @click="emit('style-to-library', { corpus: c, profile: corpusProfile[c.id] })">стиль → в библиотеку</button>
            <button class="ghost small-btn" title="Нотный шаблон профиля — в редактор плана (для продвинутых)" @click="applyAbc(corpusProfile[c.id])">ноты → в план</button>
          </div>
        </div>
      </div>
      <p v-if="!corpora.length" class="muted">Профиль: тональности/прогрессии/темп/структура (SheetSage2), DSP-паспорт, тексты (Whisper), строка стиля (Ollama). Стены характера нет: тембр/голос не переносится.</p>
      <div class="set-actions">
        <button class="ghost" @click="emit('close')">Вернуться</button>
      </div>
    </section>
  </main>
</template>
