// Общая модалка подтверждения (один инстанс на приложение).
import { ref } from 'vue'

const open = ref(false)
const title = ref('')
const body = ref('')
let confirmFn = null

function askConfirm(t, b, fn) {
  title.value = t
  body.value = b
  confirmFn = fn
  open.value = true
}

function doConfirm() {
  open.value = false
  if (confirmFn) { const f = confirmFn; confirmFn = null; f() }
}

function cancelConfirm() { open.value = false; confirmFn = null }

export function useConfirm() {
  return { open, title, body, askConfirm, doConfirm, cancelConfirm }
}
