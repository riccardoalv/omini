<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import NotificationsSection from '@/components/NotificationsSection.vue'
import PluginsSection from '@/components/PluginsSection.vue'
import { locales } from '@/i18n'
import { api } from '@/lib/api'
import { prefs } from '@/lib/prefs'
import { session, setLocale, signOut } from '@/lib/session'

const { t } = useI18n()
const version = ref('')

onMounted(async () => {
  try {
    version.value = (await api.health()).version
  } catch {
    // version is informative only
  }
})
</script>

<template>
  <div class="page narrow">
    <header class="page-header">
      <h1>{{ t('settings.title') }}</h1>
    </header>

    <section class="card section">
      <h2>{{ t('settings.appearance') }}</h2>
      <div class="field">
        <label for="s-theme">{{ t('settings.theme') }}</label>
        <select id="s-theme" v-model="prefs.theme" class="select">
          <option value="system">{{ t('settings.themeSystem') }}</option>
          <option value="dark">{{ t('settings.themeDark') }}</option>
          <option value="light">{{ t('settings.themeLight') }}</option>
        </select>
      </div>
      <div class="field">
        <label for="s-locale">{{ t('settings.language') }}</label>
        <select
          id="s-locale"
          :value="prefs.locale"
          class="select"
          @change="setLocale(($event.target as HTMLSelectElement).value)"
        >
          <option v-for="l in locales" :key="l.code" :value="l.code">{{ l.name }}</option>
        </select>
      </div>
    </section>

    <section class="card section">
      <h2>{{ t('settings.map') }}</h2>
      <div class="field">
        <label for="s-threshold">{{ t('settings.collapseThreshold') }}</label>
        <input
          id="s-threshold"
          v-model.number="prefs.collapseThreshold"
          class="input short"
          type="number"
          min="2"
          max="100"
        />
        <span class="help">{{ t('settings.collapseHint') }}</span>
      </div>
    </section>

    <NotificationsSection />

    <PluginsSection />

    <section class="card section">
      <h2>{{ t('settings.account') }}</h2>
      <p class="muted">{{ t('settings.signedInAs', { name: session.username }) }}</p>
      <button class="btn" @click="signOut">{{ t('auth.signOut') }}</button>
    </section>

    <section class="card section">
      <h2>{{ t('settings.about') }}</h2>
      <p class="muted">
        Omini · {{ t('settings.version', { v: version || '…' }) }} ·
        <a href="https://github.com/riccardoalv/omini" target="_blank" rel="noopener">GitHub</a>
      </p>
    </section>
  </div>
</template>

<style scoped>
.narrow {
  max-width: 640px;
}
.section {
  padding: 18px 20px 6px;
  margin-bottom: 14px;
}
.section h2 {
  font-size: 15px;
  margin-bottom: 14px;
}
.section > p {
  margin: 0 0 14px;
}
.section > .btn {
  margin-bottom: 14px;
}
.short {
  max-width: 120px;
}
</style>
