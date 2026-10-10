<script setup>
// Страница «Пресеты звука» (меню ⋮): библиотека пресетов — что внутри, свои — переименовать и удалить.
// Применяются пресеты в форме нового трека и в студии («Готово»), здесь — только просмотр и правка своих.
import AppIcon from './AppIcon.vue'
import PresetLibrary from './PresetLibrary.vue'
import { useI18n } from '../i18n/index.js'

const { t } = useI18n()
const emit = defineEmits(['close'])
</script>

<template>
  <div class="modal-backdrop page-backdrop" @click.self="emit('close')">
    <section class="panel page-modal">
      <div class="page-modal-head">
        <h2>{{ t('preset.title') }}</h2>
        <button class="ghost icon" :title="t('common.close')" @click="emit('close')"><AppIcon name="x" /></button>
      </div>
      <div class="page-modal-body">
        <p class="muted">{{ t('preset.lib.desc') }}</p>
        <PresetLibrary />
      </div>
    </section>
  </div>
</template>
