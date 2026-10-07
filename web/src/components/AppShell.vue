<script setup lang="ts">
import { Cable, LayoutGrid, Network, Settings } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import Logo from './LogoMark.vue'

const { t } = useI18n()

const items = [
  { to: '/', icon: Network, label: 'nav.map' },
  { to: '/devices', icon: LayoutGrid, label: 'nav.devices' },
  { to: '/integrations', icon: Cable, label: 'nav.integrations' },
  { to: '/settings', icon: Settings, label: 'nav.settings' },
]
</script>

<template>
  <div class="shell">
    <nav class="sidebar" :aria-label="t('app.name')">
      <Logo class="logo" />
      <RouterLink
        v-for="item in items"
        :key="item.to"
        :to="item.to"
        class="nav-item"
        :title="t(item.label)"
      >
        <component :is="item.icon" :size="20" :stroke-width="1.75" />
        <span class="nav-label">{{ t(item.label) }}</span>
      </RouterLink>
    </nav>
    <main class="content">
      <slot />
    </main>
  </div>
</template>

<style scoped>
.shell {
  display: flex;
  height: 100%;
}
.sidebar {
  width: 68px;
  flex: none;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 14px 0;
  background: var(--bg-elevated);
  border-right: 1px solid var(--border);
  z-index: 10;
}
.logo {
  width: 30px;
  height: 30px;
  margin-bottom: 14px;
}
.nav-item {
  width: 52px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
  padding: 8px 0 6px;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  text-decoration: none;
}
.nav-item:hover {
  background: var(--surface-hover);
  color: var(--text);
}
.nav-item.router-link-exact-active {
  background: var(--accent-soft);
  color: var(--accent);
}
.nav-label {
  font-size: 10.5px;
  font-weight: 500;
  max-width: 50px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.content {
  flex: 1;
  min-width: 0;
  overflow: auto;
  position: relative;
}

@media (max-width: 640px) {
  .shell {
    flex-direction: column-reverse;
  }
  .sidebar {
    width: 100%;
    flex-direction: row;
    justify-content: space-around;
    padding: 4px 8px;
    border-right: 0;
    border-top: 1px solid var(--border);
  }
  .logo {
    display: none;
  }
}
</style>
