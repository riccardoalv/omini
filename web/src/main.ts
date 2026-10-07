import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import './styles/main.css'

import { createApp, watch } from 'vue'

import App from './App.vue'
import { createAppI18n } from './i18n'
import { loadCatalog } from './lib/catalog'
import { applyTheme, prefs } from './lib/prefs'
import { router } from './router'

const i18n = createAppI18n(prefs.locale)
watch(
  () => prefs.locale,
  (locale) => {
    i18n.global.locale.value = locale as 'en' | 'pt-BR'
    document.documentElement.lang = locale
  },
  { immediate: true },
)
watch(() => prefs.theme, applyTheme, { immediate: true })

loadCatalog()
createApp(App).use(i18n).use(router).mount('#app')
