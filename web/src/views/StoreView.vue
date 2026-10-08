<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import PluginStore from '@/components/PluginStore.vue'

/** The plugin store as a screen of its own (also a modal in Integrations and Settings). */
const { t } = useI18n()
const router = useRouter()
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div>
        <h1>{{ t('store.screenTitle') }}</h1>
        <p class="muted">{{ t('store.subtitle') }}</p>
      </div>
    </header>
    <PluginStore
      page
      @add="(type) => router.push({ path: '/integrations', query: { add: type } })"
    />
  </div>
</template>
