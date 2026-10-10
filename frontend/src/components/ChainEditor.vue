<script setup>
// Редактор цепочки звукового движка: блоки по порядку (вкл/↑/↓/✕), крутилки, полосы эквалайзера,
// выбор захвата/IR/набора, «+ блок». Общий для страницы «Инструменты» и пульта дорожек студии.
// Цепочка — в форме fxChain.js ({type, on, params}); описание блоков — fxBlocks.json.
import { computed, ref } from 'vue'
import { useI18n } from '../i18n/index.js'
import VSelect from '../VSelect.vue'
import BLOCKS from '../fxBlocks.json'
import { addBlock, removeBlock, moveBlock, toggleBlock, setParam, addBand, removeBand, setBand } from '../fxChain.js'

const chain = defineModel({ type: Array, required: true })
const props = defineProps({
  assets: { type: Object, default: () => ({ amps: [], irs: [], kits: [] }) },
  busy: { type: String, default: '' },   // «kit» — идёт установка набора
})
// upload(kind) — загрузить свой .nam/.wav; installKit(имя) — скачать набор сэмплов на воркер
const emit = defineEmits(['upload', 'install-kit'])
const { t, locale } = useI18n()

const tr = (lbl) => (lbl && (lbl[locale.value] || lbl.ru)) || ''
const blockOptions = computed(() => Object.keys(BLOCKS).map((k) => ({ value: k, label: tr(BLOCKS[k].label) })))
const newType = ref('reverb')

function assetOptions(kind, def) {
  const list = ({ amp: props.assets.amps, ir: props.assets.irs, kit: props.assets.kits })[kind] || []
  const opts = list.map((a) => ({ value: a.name, label: a.name }))
  return def === '' ? [{ value: '', label: t('instr.builtin') }, ...opts] : opts
}
// набор, который качать для этого поля: часть до «/» умолчания блока (osdk/kick → osdk)
const kitOf = (s) => String(s.default || 'osdk').split('/')[0] || 'osdk'

const set = (i, key, v) => { chain.value = setParam(chain.value, i, key, v, BLOCKS) }
// новый усилитель — сразу с первым загруженным захватом: без него цепочка не считается
function add() {
  const next = addBlock(chain.value, newType.value, BLOCKS)
  const first = (props.assets.amps || [])[0]
  const i = next.length - 1
  chain.value = newType.value === 'amp' && first && !next[i].params.model ? setParam(next, i, 'model', first.name, BLOCKS) : next
}
</script>

<template>
  <div v-for="(b, i) in chain" :key="i" class="instr-block" :class="{ off: !b.on }">
    <div class="instr-block-head">
      <label class="instr-on"><input type="checkbox" :checked="b.on" @change="chain = toggleBlock(chain, i)" /> {{ tr(BLOCKS[b.type].label) }}</label>
      <span class="spacer"></span>
      <button class="ghost icon" :title="t('instr.up')" :disabled="i === 0" @click="chain = moveBlock(chain, i, -1)">↑</button>
      <button class="ghost icon" :title="t('instr.down')" :disabled="i === chain.length - 1" @click="chain = moveBlock(chain, i, 1)">↓</button>
      <button class="ghost icon" :title="t('instr.remove')" @click="chain = removeBlock(chain, i)"><AppIcon name="x" /></button>
    </div>
    <div v-if="b.on" class="dsp-params">
      <label v-for="s in BLOCKS[b.type].strings || []" :key="s.id">
        <span>{{ tr(s.label) }}</span>
        <VSelect :model-value="b.params[s.id]" :options="assetOptions(s.asset, s.default)"
                 :placeholder="t('instr.asset.none')" @update:model-value="(v) => set(i, s.id, v)" />
        <span>
          <button v-if="s.asset !== 'kit'" class="ghost small-btn" :title="t('instr.upload.tip')" @click.prevent="emit('upload', s.asset)">{{ t('instr.upload') }}</button>
          <button v-else class="ghost small-btn" :disabled="!!busy" :title="t('instr.kit.tip')" @click.prevent="emit('install-kit', kitOf(s))">{{ busy === 'kit' ? t('instr.kit.busy') : t('instr.kit') }}</button>
        </span>
      </label>
      <label v-for="p in BLOCKS[b.type].params" :key="p.id">
        <span>{{ tr(p.label) }}</span>
        <input type="range" :min="p.zero_off && p.max > 0 ? 0 : p.min" :max="p.zero_off && p.max < 0 ? 0 : p.max" :step="p.step" :value="b.params[p.id]"
               @input="(e) => set(i, p.id, Number(e.target.value))" />
        <span class="dsp-pval">{{ b.params[p.id] }}</span>
      </label>
      <template v-if="BLOCKS[b.type].bands">
        <div v-for="(band, k) in b.params.bands" :key="'band' + k" class="instr-band">
          <label v-for="(bs, key) in BLOCKS[b.type].bands" :key="key">
            <span>{{ t('instr.band', { n: k + 1 }) }} · {{ tr(bs.label) }}</span>
            <input type="range" :min="bs.min" :max="bs.max" :step="bs.step" :value="band[key]"
                   @input="(e) => (chain = setBand(chain, i, k, key, Number(e.target.value), BLOCKS))" />
            <span class="dsp-pval">{{ band[key] }}</span>
          </label>
          <button class="ghost small-btn" @click="chain = removeBand(chain, i, k, BLOCKS)">{{ t('instr.band.remove') }}</button>
        </div>
        <button class="ghost small-btn" @click="chain = addBand(chain, i, BLOCKS)">{{ t('instr.band.add') }}</button>
      </template>
    </div>
  </div>
  <div class="corpus-actions">
    <VSelect v-model="newType" :options="blockOptions" style="width: 200px" />
    <button class="ghost" @click="add">{{ t('instr.add') }}</button>
  </div>
</template>

<style scoped>
.instr-block { border: 1px solid var(--line, #2a2a35); border-radius: 8px; padding: 6px 10px; margin: 6px 0; }
.instr-block.off { opacity: 0.55; }
.instr-block-head { display: flex; align-items: center; gap: 6px; }
.instr-on { display: flex; align-items: center; gap: 6px; font-size: 13px; }
.instr-band { border-left: 2px solid var(--line, #2a2a35); padding-left: 8px; margin: 4px 0; }
.spacer { flex: 1; }
/* средняя колонка сжимается (длинное имя захвата NAM — с многоточием), правая — по кнопке «загрузить…»:
   с колонкой 52px из общего .dsp-params кнопка вылезала за край — у страницы появлялась горизонтальная прокрутка */
.dsp-params label { grid-template-columns: 190px minmax(0, 1fr) minmax(52px, auto); }
.dsp-params :deep(.vselect) { min-width: 0; }
</style>
