<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import PanelSection from '@/components/map/PanelSection.vue'
import type { Container } from '@/lib/types'

/**
 * The container or Swarm service that serves an app (or that a device is):
 * its name, image, state, replicas, stack and the ports it publishes.
 */
const props = defineProps<{ container: Container; fixed?: boolean }>()
const { t } = useI18n()

const state = computed(() => {
  const c = props.container
  const replicas =
    c.replicas !== undefined
      ? t('panel.containerReplicas', { running: c.running ?? 0, replicas: c.replicas })
      : ''
  return [c.state, replicas].filter(Boolean).join(' · ')
})
const ports = computed(() =>
  (props.container.ports ?? []).map((p) => {
    const proto = p.protocol ?? 'tcp'
    return p.target_port && p.target_port !== p.port
      ? `${p.port}/${proto} → ${p.target_port}`
      : `${p.port}/${proto}`
  }),
)
/** One line, e.g. "homelab_jellyfin · lscr.io/linuxserver/jellyfin · running 1/1 · stack homelab". */
const summary = computed(() => {
  const c = props.container
  return [
    c.name,
    c.image,
    state.value,
    c.stack ? t('panel.containerStackOf', { stack: c.stack }) : '',
  ]
    .filter(Boolean)
    .join(' · ')
})
</script>

<template>
  <PanelSection
    id="container"
    :title="t('panel.container')"
    :summary="summary"
    :fixed="fixed"
    data-test="container"
  >
    <dl class="facts">
      <template v-if="container.name">
        <dt>{{ t('panel.containerName') }}</dt>
        <dd class="mono" data-test="container-name">{{ container.name }}</dd>
      </template>
      <template v-if="container.image">
        <dt>{{ t('panel.containerImage') }}</dt>
        <dd class="mono" data-test="container-image">{{ container.image }}</dd>
      </template>
      <template v-if="state">
        <dt>{{ t('panel.containerState') }}</dt>
        <dd data-test="container-state">{{ state }}</dd>
      </template>
      <template v-if="container.stack">
        <dt>{{ t('panel.containerStack') }}</dt>
        <dd data-test="container-stack">{{ container.stack }}</dd>
      </template>
      <template v-if="ports.length">
        <dt>{{ t('panel.containerPorts') }}</dt>
        <dd class="mono" data-test="container-ports">{{ ports.join(', ') }}</dd>
      </template>
    </dl>
  </PanelSection>
</template>

<style scoped>
.facts {
  display: grid;
  grid-template-columns: minmax(90px, auto) 1fr;
  gap: 7px 14px;
  margin: 0;
  font-size: 13px;
}
dt {
  color: var(--text-muted);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
