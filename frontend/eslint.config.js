import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'

export default [
  js.configs.recommended,
  ...pluginVue.configs['flat/essential'],
  {
    ignores: ['dist/**', 'wailsjs/**', 'node_modules/**'],
  },
  {
    languageOptions: {
      globals: {
        localStorage: 'readonly',
        window: 'readonly',
        document: 'readonly',
        console: 'readonly',
        alert: 'readonly',
        fetch: 'readonly',
        setTimeout: 'readonly',
        clearTimeout: 'readonly',
        setInterval: 'readonly',
        requestAnimationFrame: 'readonly',
        cancelAnimationFrame: 'readonly',
        getComputedStyle: 'readonly',
        ResizeObserver: 'readonly',
        URL: 'readonly',
        CustomEvent: 'readonly',
        clearInterval: 'readonly',
      },
    },
    rules: {
      'no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      // пустые catch — осознанный best-effort (состояние плеера, localStorage)
      'no-empty': ['error', { allowEmptyCatch: true }],
    },
  },
]
