<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import type { PluginTrust } from '@/lib/types'

/** Publisher and trust level of a plugin, as small badges. */
defineProps<{ publisher: 'official' | 'community'; trust: PluginTrust }>()
const { t } = useI18n()
</script>

<template>
  <span class="trust">
    <span class="badge" :class="publisher">{{ t(`plugins.${publisher}`) }}</span>
    <span class="badge" :class="trust" :title="t(`plugins.trustHint.${trust}`)">{{
      t(`plugins.trust.${trust}`)
    }}</span>
  </span>
</template>

<style scoped>
.trust {
  display: inline-flex;
  gap: 4px;
  flex-wrap: wrap;
}
.badge.official {
  background: var(--accent-soft);
  color: var(--accent);
}
.badge.plug-and-play,
.badge.stable {
  background: var(--ok-soft);
  color: var(--ok);
}
.badge.experimental {
  background: var(--warn-soft);
  color: var(--warn);
}
.badge.unverified {
  background: var(--danger-soft);
  color: var(--danger);
}
</style>
