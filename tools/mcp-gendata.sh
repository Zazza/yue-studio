#!/usr/bin/env bash
# Генерация данных библиотеки стилей для MCP (internal/mcp/*.json)
# из фронтендовых источников. Запуск из корня репо: make mcp-data
set -euo pipefail
cd "$(dirname "$0")/.."

node --input-type=module <<'EOF'
import { writeFileSync } from 'node:fs'
import { slotOptions, durOptions } from './frontend/src/slotOptions.js'
import { groups as builtinGroups } from './frontend/src/groups.js'
import { presets } from './frontend/src/presets.js'
import { readFileSync } from 'node:fs'
import { fxPresets } from './frontend/src/fxPresets.js'

// слоты: {[2]string}
writeFileSync('internal/mcp/slot_options.json', JSON.stringify(slotOptions, null, 1))

// группы: встроенные + пресеты одной группой «Пресеты» (как в UI-библиотеке)
const presetGroup = {
  id: 'presets',
  name: 'Пресеты',
  items: presets.map((p, i) => ({
    id: `preset-${i}`,
    name: p.name || `пресет ${i + 1}`,
    style: p.style || Object.values(p.slots || {}).filter(Boolean).join(', '),
  })),
}
const groups = [...builtinGroups.map((g) => ({
  id: g.id, name: g.name,
  items: (g.items || []).map((i) => ({ id: i.id, name: i.name, style: i.style })),
})), presetGroup]
writeFileSync('internal/mcp/style_groups.json', JSON.stringify(groups, null, 1))
// звуковой движок: описание блоков — один источник worker/fx_blocks.json, копии побайтно
const fxBlocks = readFileSync('worker/fx_blocks.json')
writeFileSync('frontend/src/fxBlocks.json', fxBlocks)
writeFileSync('internal/mcp/fx_blocks.json', fxBlocks)
writeFileSync('internal/mcp/fx_presets.json', JSON.stringify(fxPresets, null, 1))
console.log('mcp data ok:', slotOptions ? Object.keys(slotOptions).length : 0, 'slots,', groups.length, 'groups')
EOF
