// Готовые цепочки + свои инструменты воркера одним списком (страница «Инструменты», пульт «Дорожки», «Синт по
// аккордам», «Перкуссия по сетке»). Синглтон: свои грузятся один раз, после правки — reload.
import { computed, ref } from 'vue'
import { api } from '../api.js'
import { fxPresets } from '../fxPresets.js'
import { mergeInstruments } from '../myInstruments.js'

const mine = ref([])
const loadErr = ref('')
let loaded = null

async function reload() {
  try {
    mine.value = (await api.fxInstruments()) || []
    loadErr.value = ''
  } catch (e) {
    loadErr.value = String(e)   // старый воркер без /fx/instruments — только готовые
  }
}

const all = computed(() => mergeInstruments(fxPresets, mine.value))

export function useInstruments() {
  if (!loaded) loaded = reload()
  return { all, mine, loadErr, reload, ready: () => loaded }
}
