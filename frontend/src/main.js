import { createApp } from 'vue'
import App from './App.vue'
import AppIcon from './components/AppIcon.vue'
import './style.css'

// <AppIcon> — во всех экранах (значки вместо эмодзи), поэтому глобально
createApp(App).component('AppIcon', AppIcon).mount('#app')
