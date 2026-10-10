<script setup>
// Окно при запуске: «Что это» (смысл приложения в пяти пунктах) и справа
// «Что нового» (новости версии). Когда показывать — решает welcome.js.
import { ref, watch } from 'vue'
import { useI18n } from '../i18n/index.js'
import { RELEASE_NOTES } from '../releaseNotes.js'

const props = defineProps({ tab: { type: String, default: 'about' } })
const emit = defineEmits(['close'])   // close(dontShow: boolean)
const { t, locale } = useI18n()

const cur = ref(props.tab)
watch(() => props.tab, (v) => { cur.value = v })
const dontShow = ref(false)
const POINTS = ['plan', 'edit', 'versions', 'sound', 'agent']
const notes = RELEASE_NOTES[0]
const close = () => emit('close', dontShow.value)
</script>

<template>
  <div class="modal-backdrop" @keydown.esc="close">
    <div class="modal welcome-modal" role="dialog" aria-modal="true">
      <div class="modal-head welcome-head">
        <img class="welcome-logo" src="../assets/logo.png" alt="" width="32" height="32">
        <div class="welcome-tabs">
          <button class="ghost small-btn" :class="{ on: cur === 'about' }" @click="cur = 'about'">{{ t('welcome.tab.about') }}</button>
          <span class="spacer"></span>
          <button v-if="notes" class="ghost small-btn" :class="{ on: cur === 'news' }" @click="cur = 'news'">
            {{ t('welcome.tab.news') }} · {{ notes.version }}</button>
        </div>
        <button class="ghost icon" :title="t('common.close')" @click="close"><AppIcon name="x" /></button>
      </div>

      <div v-if="cur === 'about'" class="welcome-body">
        <h2 class="welcome-title">{{ t('welcome.title') }}</h2>
        <p class="welcome-lead">{{ t('welcome.lead') }}</p>
        <dl class="welcome-points">
          <template v-for="p in POINTS" :key="p">
            <dt>{{ t('welcome.' + p + '.title') }}</dt>
            <dd>{{ t('welcome.' + p + '.body') }}</dd>
          </template>
        </dl>
        <p class="muted welcome-start">{{ t('welcome.start') }}</p>
      </div>

      <div v-else class="welcome-body">
        <h2 class="welcome-title">{{ t('welcome.news.title', { v: notes.version }) }}</h2>
        <ul class="welcome-news">
          <li v-for="(item, i) in notes[locale] || notes.ru" :key="i">{{ item }}</li>
        </ul>
      </div>

      <div class="modal-actions welcome-actions">
        <label class="muted welcome-dont"><input v-model="dontShow" type="checkbox"> {{ t('welcome.dontShow') }}</label>
        <span class="spacer"></span>
        <button class="primary" @click="close">{{ t('welcome.ok') }}</button>
      </div>
    </div>
  </div>
</template>

<style>
.welcome-modal { width: min(620px, 92vw); gap: 14px; }
/* .modal-head из App.vue подключается позже и ставит baseline — картинка вставала
   нижним краем на линию текста и торчала вверх; двойной класс сильнее */
.modal-head.welcome-head { align-items: center; }
.welcome-logo { width: 32px; height: 32px; flex: none; filter: drop-shadow(0 0 6px var(--lcd-glow)); }
.welcome-tabs { display: flex; align-items: center; gap: 8px; flex: 1; min-width: 0; }
.welcome-tabs .on { color: var(--text); border-color: var(--accent); }
.welcome-body { overflow-y: auto; min-height: 0; }
.welcome-modal h2.welcome-title { margin: 0 0 6px; padding: 0; background: none; border: none; text-shadow: none;
  font-size: 18px; text-transform: none; letter-spacing: 0; color: var(--lcd-text); }
.welcome-lead { margin: 0 0 14px; line-height: 1.5; max-width: 62ch; }
.welcome-points { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 8px 16px; margin: 0; }
.welcome-points dt { font-weight: 600; white-space: nowrap; }
.welcome-points dd { margin: 0; color: var(--muted); line-height: 1.45; }
.welcome-start { margin: 14px 0 0; }
.welcome-news { margin: 0; padding-left: 20px; display: flex; flex-direction: column; gap: 6px; line-height: 1.45; }
.welcome-actions { align-items: center; }
.welcome-dont { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
@media (max-width: 560px) {
  .welcome-points { grid-template-columns: 1fr; gap: 2px; }
  .welcome-points dd { margin-bottom: 8px; }
}
</style>
