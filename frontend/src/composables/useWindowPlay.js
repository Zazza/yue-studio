// «▶ было»: кусок [from, to) файла трека встроенным плеером со стопом в конце куска — плеер сам
// конца куска не знает, поэтому за позицией следит этот помощник. Звать из setup компонента:
// при его закрытии идущий кусок останавливается (следить за концом больше некому).
import { ref, watch, onBeforeUnmount } from 'vue'
import { api } from '../api.js'
import { usePlayer } from './usePlayer.js'

export function useWindowPlay(key, label) {
  const { toggleArtifact, playerState, nowPlayingKey } = usePlayer()
  const until = ref(null)   // {from, to} идущего куска
  let armed = false

  async function play({ jobId, file, dur, from, to }) {
    await toggleArtifact(key, label(), async () => {
      await api.playFile(jobId, file, dur)
      await api.seekAudio(from)
      armed = false
      until.value = { from, to }
    })
  }

  watch(() => playerState.value.position_sec, async (pos) => {
    if (until.value == null || nowPlayingKey.value !== key) return
    const { from, to } = until.value
    if (!armed) {               // первая позиция может остаться от прошлого проигрывания
      if (pos >= from && pos < to) armed = true
      return
    }
    if (pos >= to) {
      until.value = null
      await toggleArtifact(key, label(), async () => {})
    }
  })

  onBeforeUnmount(() => {
    if (until.value && nowPlayingKey.value === key) {
      until.value = null
      nowPlayingKey.value = ''
      api.stopAudio().catch(() => { /* плеер уже остановлен */ })
    }
  })

  return { play }
}
