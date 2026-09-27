// Тесты библиотеки стилей: встроенные группы + сохранение своих в localStorage.
import { describe, it, expect, beforeEach } from 'vitest'
import { groups, loadCustomGroups, saveCustomGroups } from './groups.js'

// node-окружение vitest без DOM: минимальный localStorage
class MemStorage {
  constructor() { this.m = new Map() }
  getItem(k) { return this.m.has(k) ? this.m.get(k) : null }
  setItem(k, v) { this.m.set(k, String(v)) }
  removeItem(k) { this.m.delete(k) }
  clear() { this.m.clear() }
}
globalThis.localStorage = new MemStorage()

describe('встроенные группы', () => {
  it('группы с уникальными id и непустыми названиями/стилями', () => {
    const ids = groups.map(g => g.id)
    expect(new Set(ids).size).toBe(groups.length)
    for (const g of groups) {
      expect(g.name.trim(), g.id).not.toBe('')
      expect((g.items || []).length, g.id).toBeGreaterThan(0)
      for (const i of g.items || []) {
        expect(i.name.trim(), `${g.id}/${i.id}`).not.toBe('')
        expect(i.style.trim(), `${g.id}/${i.id}`).not.toBe('')
      }
    }
  })
})

describe('свои группы (localStorage)', () => {
  beforeEach(() => localStorage.clear())

  it('пустое хранилище → пустой список, не падает', () => {
    expect(loadCustomGroups()).toEqual([])
  })

  it('сохранение и загрузка — туда-обратно', () => {
    const gs = [{ id: 'c-x', name: 'Мои регги', items: [{ id: 'c-x-0', name: 'даб', style: 'dub, slow' }] }]
    saveCustomGroups(gs)
    expect(loadCustomGroups()).toEqual(gs)
  })

  it('битый JSON в хранилище деградирует к пустому списку', () => {
    localStorage.setItem('yue_custom_groups', '{broken')
    expect(loadCustomGroups()).toEqual([])
  })
})
