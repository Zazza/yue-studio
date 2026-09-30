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

// «играть выделенное» студии: верхний ▶ при активном выделении гоняет кусок
// [from, to] артефакта волны — один прогон, стоп в конце (rangeUntil).
const playRange = ref(null)   // {key, jobId, file, dur, from, to} | null
const rangeUntil = ref(null)  // {key, to} — идущий прогон, стоп по нему

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

function setPlayRange(r) {
  playRange.value = r
  if (!r) rangeUntil.value = null
}

// один прогон выделенного куска: ползунок и тайминг шапки — сразу с начала
async function playRangeOnce(r) {
  if (playBusy.value[r.key]) return
  playBusy.value = { ...playBusy.value, [r.key]: true }
  nowPlaying.value = `кусок ${fmtDur(r.from)}–${fmtDur(r.to)}`
  try {
    await api.playFile(r.jobId, r.file, r.dur)
    nowPlayingKey.value = r.key
    seekPos.value = r.from
    await api.seekAudio(r.from)
    rangeUntil.value = { key: r.key, to: r.to }
  } catch (e) {
    nowPlaying.value = `звук: ${String(e)}`
  } finally {
    playBusy.value = { ...playBusy.value, [r.key]: false }
    setTimeout(() => refreshCb && refreshCb(), 300)
  }
}

// ▶ шапки: есть выделение в студии — гоняет кусок (пауза внутри куска —
// продолжение с места паузы, стоп/конец — новый прогон с начала); иначе —
// обычная пауза/продолжение загруженного
async function togglePlay() {
  const r = playRange.value
  if (!r) return api.toggleAudio()
  const st = playerState.value
  if (nowPlayingKey.value === r.key && st.job_id === r.jobId && st.job_id > 0
      && (st.playing || (st.position_sec > r.from && st.position_sec < r.to))) {
    return api.toggleAudio()
  }
  return playRangeOnce(r)
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
  // округляем ДО деления: floor+round(остатка) давал «1:60» вместо «2:00»
  const total = Math.round(s)
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`
}

export function usePlayer() {
  return {
    playerState, nowPlaying, playBusy, nowPlayingKey, volume, seekPos,
    refreshPlayer, isPlaying, toggleArtifact, playBtn, stopAll,
    onVolume, onSeekInput, onSeekChange, onRefresh,
    playRange, rangeUntil, setPlayRange, togglePlay,
  }
}

// инициализация один раз при первом импорте
watch(() => playerState.value.position_sec, (p) => { if (!seeking) seekPos.value = p || 0 })
setInterval(refreshPlayer, 1000)
