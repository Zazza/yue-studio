<script setup>
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'
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

// метки минут-секунд под полосой прокрутки: «куда встану, если кликну».
// Шаг — «круглый» (15/30/60 с…), чтобы меток было не больше ~8
const seekTicks = computed(() => {
  const d = playerState.value.duration_sec || 0
  if (!d) return []
  const step = [15, 30, 60, 120, 300, 600].find((s) => d / s <= 8) || Math.ceil(d / 8 / 600) * 600
  const out = []
  for (let s = step; s < d - step / 2; s += step) out.push({ sec: s, pct: (100 * s) / d })
  return out
})

// название трека — фиксированной ширины, длинное бежит строкой, как в Winamp:
// ширина надписи не меняется, и полоса прокрутки рядом не «плавает»
const nowLabel = computed(() => nowPlaying.value || (playerState.value.job_id ? '#' + playerState.value.job_id : ''))
const nowBox = ref(null)
const nowText = ref(null)
const marquee = ref(0) // 0 — влезает; иначе длительность круга, с
const MARQUEE_PX_PER_SEC = 30
async function measureNow() {
  await nextTick()
  if (!nowBox.value || !nowText.value) return
  const w = nowText.value.scrollWidth
  marquee.value = w > nowBox.value.clientWidth ? Math.max(4, w / MARQUEE_PX_PER_SEC) : 0
}
watch(nowLabel, measureNow)
let ro = null
onMounted(() => {
  measureNow()
  if (window.ResizeObserver && nowBox.value) {
    ro = new ResizeObserver(measureNow)
    ro.observe(nowBox.value)
  }
})
onUnmounted(() => ro && ro.disconnect())

// наведение на полосу: LCD показывает, куда прыгнет клик («→ 1:23»)
const hoverSec = ref(null)
function onSeekHover(e) {
  const d = playerState.value.duration_sec
  if (!d) return
  const r = e.currentTarget.getBoundingClientRect()
  const frac = Math.min(1, Math.max(0, (e.clientX - r.left) / r.width))
  hoverSec.value = frac * d
}
</script>

<template>
  <div class="playerbar">
    <button class="ghost" :disabled="!playerState.job_id" :title="t('player.prev')" style="letter-spacing:-2px" @click="playNeighbor(jobs, -1)"><svg width="12" height="10" viewBox="0 0 12 10"><path d="M2 0h1.6v10H2zM11 0v10L4.4 5z" fill="currentColor"/></svg></button>
    <button class="ghost" :disabled="!playerState.job_id && !playRange" @click="togglePlay" :title="t('player.pause')"><svg v-if="playerState.playing" width="10" height="10" viewBox="0 0 10 10"><path d="M1 0h2.8v10H1zM6.2 0H9v10H6.2z" fill="currentColor"/></svg><svg v-else width="10" height="10" viewBox="0 0 10 10"><path d="M1 0l8 5-8 5z" fill="currentColor"/></svg></button>
    <button class="ghost" :disabled="!playerState.job_id" @click="stopPlaying" :title="t('player.stop')"><svg width="9" height="9" viewBox="0 0 9 9"><rect width="9" height="9" fill="currentColor"/></svg></button>
    <button class="ghost" :disabled="!playerState.job_id" :title="t('player.next')" style="letter-spacing:-2px" @click="playNeighbor(jobs, 1)"><svg width="12" height="10" viewBox="0 0 12 10"><path d="M1 0v10l6.6-5zM8.4 0H10v10H8.4z" fill="currentColor"/></svg></button>
    <!-- название — только когда что-то играет: пустое занимало 220 px между кнопками и полосой -->
    <span v-show="nowLabel" ref="nowBox" class="now" :title="playerState.error || nowLabel">
      <span class="now-track" :class="{ scroll: marquee }" :style="marquee ? { animationDuration: marquee + 's' } : null">
        <span ref="nowText" class="now-text">{{ nowLabel }}</span><span v-if="marquee" class="now-text" aria-hidden="true">{{ nowLabel }}</span>
      </span>
    </span>
    <div class="seek-wrap" @mousemove="onSeekHover" @mouseleave="hoverSec = null">
      <input class="seek" type="range" min="0" :max="Math.max(1, playerState.duration_sec || 1)"
             step="1" v-model.number="seekPos" :disabled="!playerState.duration_sec"
             @input="onSeekInput" @change="onSeekChange" :title="t('player.seek')" />
      <div v-if="seekTicks.length" class="seek-ticks">
        <span v-for="tk in seekTicks" :key="tk.sec" class="seek-tick" :style="{ left: tk.pct + '%' }">{{ fmtDur(tk.sec) }}</span>
      </div>
    </div>
    <span class="pos muted" :title="t('player.seek')">{{ playerState.duration_sec
      ? (hoverSec != null ? '→ ' + fmtDur(hoverSec) : fmtDur(playerState.position_sec) + ' / ' + fmtDur(playerState.duration_sec))
      : '-:-- / -:--' }}</span>
    <label class="vol" :title="t('player.volume')">🔊<input type="range" min="0" max="1" step="0.05" v-model.number="volume" @input="onVolume" /></label>
    <span v-if="playerState.error" class="error" :title="playerState.error">звук: ошибка</span>
  </div>
</template>
