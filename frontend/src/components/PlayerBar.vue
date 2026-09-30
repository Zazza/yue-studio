<script setup>
import { api } from '../api.js'
import { useI18n } from '../i18n/index.js'
const { t } = useI18n()
import { usePlayer, fmtDur } from '../composables/usePlayer.js'

defineProps({ jobs: { type: Array, default: () => [] } })
const emit = defineEmits(['play-job', 'refresh'])
const { playerState, nowPlaying, volume, seekPos, onVolume, onSeekInput, onSeekChange, playRange, togglePlay } = usePlayer()

async function stopPlaying() {
  await api.stopAudio()
  emit('refresh')
}

// соседний готовый трек относительно текущего
function playNeighbor(list, delta) {
  const done = list.filter((x) => x.status === 'done' && x.audio_file)
  if (!done.length) return
  const cur = playerState.value.job_id
  let i = done.findIndex((x) => x.id === cur)
  i = i < 0 ? 0 : Math.min(done.length - 1, Math.max(0, i + delta))
  const j = done[i]
  if (j && j.id !== cur) emit('play-job', j)
}
</script>

<template>
  <div class="playerbar">
    <button class="ghost" :disabled="!playerState.job_id" :title="t('player.prev')" style="letter-spacing:-2px" @click="playNeighbor(jobs, -1)"><svg width="12" height="10" viewBox="0 0 12 10"><path d="M2 0h1.6v10H2zM11 0v10L4.4 5z" fill="currentColor"/></svg></button>
    <button class="ghost" :disabled="!playerState.job_id && !playRange" @click="togglePlay" :title="t('player.pause')"><svg v-if="playerState.playing" width="10" height="10" viewBox="0 0 10 10"><path d="M1 0h2.8v10H1zM6.2 0H9v10H6.2z" fill="currentColor"/></svg><svg v-else width="10" height="10" viewBox="0 0 10 10"><path d="M1 0l8 5-8 5z" fill="currentColor"/></svg></button>
    <button class="ghost" :disabled="!playerState.job_id" @click="stopPlaying" :title="t('player.stop')"><svg width="9" height="9" viewBox="0 0 9 9"><rect width="9" height="9" fill="currentColor"/></svg></button>
    <button class="ghost" :disabled="!playerState.job_id" :title="t('player.next')" style="letter-spacing:-2px" @click="playNeighbor(jobs, 1)"><svg width="12" height="10" viewBox="0 0 12 10"><path d="M1 0v10l6.6-5zM8.4 0H10v10H8.4z" fill="currentColor"/></svg></button>
    <span class="now" :title="playerState.error">{{ nowPlaying || (playerState.job_id ? '#' + playerState.job_id : '') }}</span>
    <input class="seek" type="range" min="0" :max="Math.max(1, playerState.duration_sec || 1)"
           step="1" v-model.number="seekPos" :disabled="!playerState.duration_sec"
           @input="onSeekInput" @change="onSeekChange" :title="t('player.seek')" />
    <span class="pos muted">{{ playerState.duration_sec
      ? fmtDur(playerState.position_sec) + ' / ' + fmtDur(playerState.duration_sec)
      : '-:-- / -:--' }}</span>
    <label class="vol" :title="t('player.volume')">🔊<input type="range" min="0" max="1" step="0.05" v-model.number="volume" @input="onVolume" /></label>
    <span v-if="playerState.error" class="error" :title="playerState.error">звук: ошибка</span>
  </div>
</template>
