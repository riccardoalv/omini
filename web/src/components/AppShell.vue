<script setup lang="ts">
import {
  Cable,
  LayoutGrid,
  Network,
  PanelLeftClose,
  PanelLeftOpen,
  Settings,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { prefs } from '@/lib/prefs'

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
    <nav class="sidebar" :class="{ expanded: prefs.sidebarExpanded }" :aria-label="t('app.name')">
      <div class="brand">
        <Logo class="logo" />
        <span class="brand-name">{{ t('app.name') }}</span>
      </div>
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
      <button
        class="nav-item toggle"
        type="button"
        data-test="sidebar-toggle"
        :aria-expanded="prefs.sidebarExpanded"
        :title="prefs.sidebarExpanded ? t('nav.collapse') : t('nav.expand')"
        @click="prefs.sidebarExpanded = !prefs.sidebarExpanded"
      >
        <component
          :is="prefs.sidebarExpanded ? PanelLeftClose : PanelLeftOpen"
          :size="20"
          :stroke-width="1.75"
        />
        <span class="nav-label">{{
          prefs.sidebarExpanded ? t('nav.collapse') : t('nav.expand')
        }}</span>
      </button>
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
  padding: 14px 8px;
  background: var(--bg-elevated);
  border-right: 1px solid var(--border);
  z-index: 10;
  transition: width 0.18s ease;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
}
.logo {
  width: 30px;
  height: 30px;
  flex: none;
}
.brand-name {
  display: none;
  font-size: 17px;
  font-weight: 700;
}
.nav-item {
  width: 52px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
  padding: 8px 0 6px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  font: inherit;
  text-decoration: none;
  cursor: pointer;
}
.nav-item svg {
  flex: none;
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
.toggle {
  margin-top: auto;
}

/* Expanded: labels next to the icons. */
.sidebar.expanded {
  width: 210px;
  align-items: stretch;
  padding: 14px 12px;
}
.sidebar.expanded .brand {
  padding: 0 8px;
}
.sidebar.expanded .brand-name {
  display: inline;
}
.sidebar.expanded .nav-item {
  width: auto;
  flex-direction: row;
  gap: 12px;
  padding: 9px 12px;
}
.sidebar.expanded .nav-label {
  max-width: none;
  font-size: 14px;
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
  .sidebar,
  .sidebar.expanded {
    width: 100%;
    flex-direction: row;
    justify-content: space-around;
    padding: 4px 8px;
    border-right: 0;
    border-top: 1px solid var(--border);
  }
  .sidebar.expanded .nav-item {
    flex-direction: column;
    gap: 3px;
    padding: 8px 0 6px;
  }
  .sidebar.expanded .nav-label {
    font-size: 10.5px;
  }
  .brand,
  .toggle {
    display: none;
  }
}
</style>
