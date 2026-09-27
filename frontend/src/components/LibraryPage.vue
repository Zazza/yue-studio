<script setup>
// Страница «Библиотека»: просмотр встроенных групп + управление своими.
import { ref } from 'vue'
import { useI18n } from '../i18n/index.js'
const { t } = useI18n()
import { groups as builtinGroups, loadCustomGroups, saveCustomGroups } from '../groups.js'
import VSelect from '../VSelect.vue'

const emit = defineEmits(['close'])

const customGroups = ref(loadCustomGroups())
const newGroupName = ref('')
const newStyleGroupId = ref('')
const newStyleName = ref('')
const newStyleStr = ref('')

function persist() {
  saveCustomGroups(customGroups.value)
}

function addCustomGroup() {
  const name = newGroupName.value.trim()
  if (!name) return
  const id = 'c-' + Date.now().toString(36)
  customGroups.value = [...customGroups.value, { id, name, items: [] }]
  newGroupName.value = ''
  newStyleGroupId.value = id
  persist()
}

function delCustomGroup(gid) {
  customGroups.value = customGroups.value.filter((g) => g.id !== gid)
  if (newStyleGroupId.value === gid) newStyleGroupId.value = ''
  persist()
}

function addCustomStyle() {
  const g = customGroups.value.find((x) => x.id === newStyleGroupId.value)
  const name = newStyleName.value.trim()
  const style = newStyleStr.value.trim()
  if (!g || !name || !style) return
  const item = { id: 'c-' + Date.now().toString(36) + '-' + g.items.length, name, style }
  g.items = [...(g.items || []), item]
  newStyleName.value = ''
  newStyleStr.value = ''
  persist()
}

function renameCustomGroup(gid, ev) {
  const g = customGroups.value.find((x) => x.id === gid)
  const name = ev.target.value.trim()
  if (g && name) { g.name = name; persist() }
  else if (ev.target) ev.target.value = g ? g.name : ''
}

function renameCustomStyle(gid, sid, ev) {
  const g = customGroups.value.find((x) => x.id === gid)
  const i = g && (g.items || []).find((x) => x.id === sid)
  const name = ev.target.value.trim()
  if (i && name) { i.name = name; persist() }
  else if (ev.target && i) ev.target.value = i.name
}

function moveCustomStyle(fromGid, sid, toGid) {
  const from = customGroups.value.find((x) => x.id === fromGid)
  const to = customGroups.value.find((x) => x.id === toGid)
  if (!from || !to || from === to) return
  const item = (from.items || []).find((x) => x.id === sid)
  if (!item) return
  from.items = from.items.filter((x) => x.id !== sid)
  to.items = [...(to.items || []), item]
  persist()
}

function delCustomStyle(gid, sid) {
  const g = customGroups.value.find((x) => x.id === gid)
  if (!g) return
  g.items = g.items.filter((i) => i.id !== sid)
  persist()
}
</script>

<template>
  <main class="settings-page">
    <section class="panel">
      <h2>{{ t('library.title') }}</h2>

      <h3 class="set-h">{{ t('library.builtin') }} <span class="muted">{{ t('library.builtin.readonly') }}</span></h3>
      <div v-for="g in builtinGroups" :key="g.id" class="lib-group">
        <strong>{{ g.name }}</strong> <span class="muted">({{ (g.items || []).length }})</span>
        <div class="muted lib-group-items">{{ (g.items || []).map(i => i.name).join(' · ') }}</div>
      </div>

      <h3 class="set-h">{{ t('library.custom') }}</h3>
      <div v-for="g in customGroups" :key="g.id" class="lib-group">
        <div class="lib-group-head">
          <input class="lib-name-input" :value="g.name" @change="renameCustomGroup(g.id, $event)" :title="t('library.rename.group')" />
          <button class="ghost" @click="delCustomGroup(g.id)">{{ t('library.group.del') }}</button>
        </div>
        <div v-for="i in g.items || []" :key="i.id" class="lib-style-row">
          <input class="lib-name-input" :value="i.name" @change="renameCustomStyle(g.id, i.id, $event)" :title="t('library.rename.style')" />
          <span class="muted lib-style-text" :title="i.style">{{ (i.style || '').slice(0, 60) }}…</span>
          <VSelect v-if="customGroups.length > 1" :model-value="g.id" :options="customGroups.map((tg) => ({ value: tg.id, label: '→ ' + tg.name, disabled: tg.id === g.id }))"
                   style="max-width: 200px" :title="t('library.move')"
                   @update:model-value="(v) => moveCustomStyle(g.id, i.id, v)" />
          <button class="ghost" @click="delCustomStyle(g.id, i.id)">✕</button>
        </div>
        <p v-if="!(g.items || []).length" class="muted">{{ t('library.group.empty') }}</p>
      </div>
      <p v-if="!customGroups.length" class="muted">{{ t('library.custom.empty') }}</p>

      <div class="set-row" style="margin-top:10px">
        <input v-model="newGroupName" :placeholder="t('library.group.new')" @keyup.enter="addCustomGroup" />
        <button class="ghost" :disabled="!newGroupName.trim()" @click="addCustomGroup">{{ t('library.group.add') }}</button>
      </div>

      <h3 class="set-h">{{ t('library.style.add') }}</h3>
      <div class="set-row">
        <VSelect v-model="newStyleGroupId" :options="customGroups.map((g) => ({ value: g.id, label: g.name }))" placeholder="— группа —" />
      </div>
      <div class="set-row" style="margin-top:8px">
        <input v-model="newStyleName" :placeholder="t('library.style.name')" />
      </div>
      <div class="set-row" style="margin-top:8px">
        <input v-model="newStyleStr" :placeholder="t('library.style.line')" @keyup.enter="addCustomStyle" />
        <button class="ghost" :disabled="!newStyleGroupId || !newStyleName.trim() || !newStyleStr.trim()" @click="addCustomStyle">{{ t('common.add') }}</button>
      </div>

      <div class="set-actions">
        <button class="ghost" @click="emit('close')">{{ t('common.back') }}</button>
      </div>
    </section>
  </main>
</template>
