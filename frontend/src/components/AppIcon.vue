<script setup>
// Значок из набора icons.js; размер — от шрифта (1.1em), цвет — currentColor.
// Перед текстом ставится через пробел в шаблоне: <AppIcon name="play" /> {{ t('queue.play') }}
import { ICONS } from '../icons.js'

defineProps({ name: { type: String, required: true } })
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html — разметка из своего статичного набора, не пользовательская -->
  <svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
       stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false" v-html="ICONS[name]" />
</template>
