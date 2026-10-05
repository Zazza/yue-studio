<script setup>
import { useConfirm } from '../composables/useConfirm.js'
import { useI18n } from '../i18n/index.js'
const { t } = useI18n()
const { open, title, body, doConfirm, cancelConfirm } = useConfirm()
</script>

<template>
  <div v-if="open" class="modal-backdrop" @click.self="cancelConfirm">
    <div class="modal confirm-modal">
      <div class="modal-head">
        <h2>{{ title }}</h2>
        <span class="spacer"></span>
        <button class="ghost" @click="cancelConfirm"><AppIcon name="x" /></button>
      </div>
      <p class="muted">{{ body }}</p>
      <div class="modal-actions">
        <button class="primary danger" @click="doConfirm">{{ t('common.confirm') }}</button>
        <button class="ghost" @click="cancelConfirm">{{ t('common.cancel') }}</button>
      </div>
    </div>
  </div>
</template>
