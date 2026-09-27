<script setup>
import { useConfirm } from '../composables/useConfirm.js'
const { open, title, body, doConfirm, cancelConfirm } = useConfirm()
</script>

<template>
  <div v-if="open" class="modal-backdrop" @click.self="cancelConfirm">
    <div class="modal confirm-modal">
      <div class="modal-head">
        <h2>{{ title }}</h2>
        <span class="spacer"></span>
        <button class="ghost" @click="cancelConfirm">✕</button>
      </div>
      <p class="muted">{{ body }}</p>
      <div class="modal-actions">
        <button class="primary danger" @click="doConfirm">Да, точно</button>
        <button class="ghost" @click="cancelConfirm">Отмена</button>
      </div>
    </div>
  </div>
</template>
