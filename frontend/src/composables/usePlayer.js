// Единое состояние встроенного плеера: один инстанс на приложение
// (state в module scope — компоненты зовут usePlayer() где угодно).
import { ref, watch } from 'vue'
import { api } from '../api.js'

const playerState = ref({ playing: false, position_sec: 0, duration_sec: 0, job_id: 0 })
const nowPlaying = ref('')     // подпись встроенного плеера
const playBusy = ref({})
// key — уникальный идентификатор артефакта: 'm15' — трек, 's15:stem-bass' — стем
const nowPlayingKey = ref('')
const volume = ref(parseFloat(localStorage.getItem('yue_volume') ?? '0.8'))

// рельса перемотки: тянем локально, на отпускании — команда в плеер
const seekPos = ref(0)
let seeking = false

let refreshCb = null
// App регистрирует колбэк общего обновления (джобы + плеер)
function onRefresh(fn) { refreshCb = fn }

async function refreshPlayer() {
  try { playerState.value = await api.audioState() } catch {}
}

function isPlaying(key) {
  return playerState.value.job_id > 0 && nowPlayingKey.value === key
}

async function toggleArtifact(key, label, play) {
  if (playBusy.value[key]) return
  if (isPlaying(key)) {
    await api.stopAudio()
    nowPlayingKey.value = ''
    setTimeout(() => refreshCb && refreshCb(), 200)
    return
  }
  playBusy.value = { ...playBusy.value, [key]: true }
  nowPlaying.value = label
  try {
    await play()
    nowPlayingKey.value = key
  } catch (e) {
    nowPlaying.value = `звук: ${String(e)}`
  } finally {
    playBusy.value = { ...playBusy.value, [key]: false }
    setTimeout(() => refreshCb && refreshCb(), 300)
  }
}

function playBtn(key) {
  if (playBusy.value[key]) return '…'
  return isPlaying(key) ? '■' : '▶'
}

async function stopAll() {
  await api.stopAudio()
  nowPlayingKey.value = ''
  await refreshPlayer()
}

function onVolume() {
  localStorage.setItem('yue_volume', String(volume.value))
  api.setVolume(volume.value)
}

function onSeekInput() { seeking = true }
async function onSeekChange() {
  seeking = false
  try { await api.seekAudio(seekPos.value) } catch {}
  setTimeout(refreshPlayer, 300)
}

export function fmtDur(s) {
  if (!s) return ''
  const m = Math.floor(s / 60)
  return `${m}:${String(Math.round(s % 60)).padStart(2, '0')}`
}

export function usePlayer() {
  return {
    playerState, nowPlaying, playBusy, nowPlayingKey, volume, seekPos,
    refreshPlayer, isPlaying, toggleArtifact, playBtn, stopAll,
    onVolume, onSeekInput, onSeekChange, onRefresh,
  }
}

// инициализация один раз при первом импорте
watch(() => playerState.value.position_sec, (p) => { if (!seeking) seekPos.value = p || 0 })
setInterval(refreshPlayer, 1000)
