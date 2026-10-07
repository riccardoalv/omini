<script setup lang="ts">
import { onMounted } from 'vue'

import AppShell from './components/AppShell.vue'
import AuthView from './components/AuthView.vue'
import { loadSession, session } from './lib/session'

onMounted(loadSession)
</script>

<template>
  <div v-if="!session.ready" class="boot" />
  <AuthView v-else-if="!session.authenticated" />
  <AppShell v-else>
    <RouterView />
  </AppShell>
</template>

<style scoped>
.boot {
  height: 100%;
  background: var(--bg);
}
</style>
