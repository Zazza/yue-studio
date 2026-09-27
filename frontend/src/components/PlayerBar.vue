<script setup>
import { api } from '../api.js'
import { usePlayer, fmtDur } from '../composables/usePlayer.js'

defineProps({ jobs: { type: Array, default: () => [] } })
const emit = defineEmits(['play-job', 'refresh'])
const { playerState, nowPlaying, volume, seekPos, onVolume, onSeekInput, onSeekChange } = usePlayer()

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
  <div v-if="playerState.playing || playerState.job_id" class="playerbar">
    <button class="ghost" title="Предыдущий готовый трек" @click="playNeighbor(jobs, -1)">⏮</button>
    <button class="ghost" @click="api.toggleAudio()" title="Пауза/продолжить">{{ playerState.playing ? '⏸' : '▶' }}</button>
    <button class="ghost" @click="stopPlaying" title="Стоп">■</button>
    <button class="ghost" title="Следующий готовый трек" @click="playNeighbor(jobs, 1)">⏭</button>
    <span class="now" :title="playerState.error">{{ nowPlaying || ('#' + playerState.job_id) }}</span>
    <input class="seek" type="range" min="0" :max="Math.max(1, playerState.duration_sec || 1)"
           step="1" v-model.number="seekPos" :disabled="!playerState.duration_sec"
           @input="onSeekInput" @change="onSeekChange" title="Перемотка" />
    <span class="pos muted">{{ fmtDur(playerState.position_sec) }}<template v-if="playerState.duration_sec"> / {{ fmtDur(playerState.duration_sec) }}</template></span>
    <label class="vol" title="Громкость">🔊<input type="range" min="0" max="1" step="0.05" v-model.number="volume" @input="onVolume" /></label>
    <span v-if="playerState.error" class="error" :title="playerState.error">звук: ошибка</span>
  </div>
</template>
