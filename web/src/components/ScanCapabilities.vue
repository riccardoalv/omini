<script setup lang="ts">
import { CircleCheck, CircleAlert } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/lib/api'
import type { Capabilities } from '@/lib/types'

/**
 * What the network scan (or nmap) can do on this server: host network,
 * multicast, ping, raw sockets — each working or limited, with how to fix it.
 */
const props = defineProps<{ type: 'network' | 'nmap' }>()
const { t } = useI18n()
const caps = ref<Capabilities>()

onMounted(async () => {
  try {
    caps.value = (await api.capabilities()).capabilities
  } catch {
    // not shown
  }
})

const rows = computed(() => {
  const c = caps.value
  if (!c) return []
  const all = [
    { key: 'host_network', ok: c.host_network },
    { key: 'multicast', ok: c.multicast },
    { key: 'ping', ok: c.unprivileged_ping || c.raw_sockets },
    { key: 'raw_sockets', ok: c.raw_sockets },
  ]
  // The network scan needs no raw sockets; nmap needs no multicast.
  return all.filter((r) =>
    props.type === 'network' ? r.key !== 'raw_sockets' : r.key !== 'multicast',
  )
})
</script>

<template>
  <section v-if="rows.length" class="caps" data-test="capabilities">
    <h4>{{ t('discovery.title') }}</h4>
    <ul>
      <li v-for="r in rows" :key="r.key" :class="{ ok: r.ok }" :data-test="`cap-${r.key}`">
        <component :is="r.ok ? CircleCheck : CircleAlert" :size="15" class="icon" />
        <span>
          <strong>{{ t(`discovery.limits.${r.key}`) }}</strong>
          <span class="muted">{{
            r.ok ? t(`discovery.ok.${r.key}`) : t(`discovery.fix.${r.key}`)
          }}</span>
        </span>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.caps {
  margin: 8px 0 4px;
}
.caps h4 {
  margin: 0 0 6px;
  font-size: 13px;
}
ul {
  display: grid;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
}
li {
  display: flex;
  gap: 8px;
  font-size: 12.5px;
  line-height: 1.4;
}
li span {
  display: grid;
}
.icon {
  flex: none;
  margin-top: 1px;
  color: var(--warn);
}
li.ok .icon {
  color: var(--ok);
}
</style>
