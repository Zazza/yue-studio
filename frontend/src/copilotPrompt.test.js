import { describe, it, expect } from 'vitest'
import { buildCopilotInstruction } from './copilotPrompt.js'

describe('buildCopilotInstruction — инструкция копайтеру из формы', () => {
  it('базовая инструкция содержит язык и разметку секций', () => {
    const s = buildCopilotInstruction({ language: 'Russian' })
    expect(s).toContain('Russian')
    expect(s).toContain('[Verse]')
    expect(s).toContain('[Chorus]')
  })

  it('пустые слоты дают дефолт', () => {
    expect(buildCopilotInstruction()).toContain('Russian')
  })

  it('женский вокал → от лица женщины', () => {
    const s = buildCopilotInstruction({ vocals: 'женский, нежный' })
    expect(s).toContain('от лица женщины')
    expect(s).not.toContain('от лица мужчины')
  })

  it('мужской вокал → от лица мужчины', () => {
    const s = buildCopilotInstruction({ vocals: 'мужской, хриплый' })
    expect(s).toContain('от лица мужчины')
  })

  it('пост-панк → рваные строки и лозунговый припев', () => {
    const s = buildCopilotInstruction({ genre: 'пост-панк, cold motorik' })
    expect(s).toContain('лозунговый припев')
  })

  it('поп → припев-хук', () => {
    const s = buildCopilotInstruction({ genre: 'поп' })
    expect(s).toContain('припев-хук')
  })

  it('блюз → строфа AAB', () => {
    const s = buildCopilotInstruction({ genre: 'блюз' })
    expect(s).toContain('AAB')
  })

  it('правило жанра берётся первое совпавшее, не все сразу', () => {
    const s = buildCopilotInstruction({ genre: 'поп' })
    expect(s).not.toContain('AAB')
    expect(s).not.toContain('рваные')
  })
})
