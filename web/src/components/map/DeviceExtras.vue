<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { formatAgo, formatBytes } from '@/lib/format'
import type { Device } from '@/lib/types'

/**
 * What some devices report besides ports and health: VLANs, services, VPN
 * peers, DHCP pool usage and the firewall's state table.
 */
const props = defineProps<{ device: Device }>()
const { t, locale } = useI18n()

const d = computed(() => props.device)
const services = computed(() =>
  [...(d.value.services ?? [])].sort(
    (a, b) => Number(a.running ?? true) - Number(b.running ?? true) || a.name.localeCompare(b.name),
  ),
)
const stopped = computed(() => services.value.filter((s) => s.running === false).length)
const statesPct = computed(() => {
  const f = d.value.firewall_states
  return f?.current !== undefined && f.limit ? Math.round((f.current / f.limit) * 100) : undefined
})
const pct = (used?: number, total?: number) =>
  used !== undefined && total ? Math.round((used / total) * 100) : undefined
const tone = (p?: number) => (p === undefined ? '' : p >= 90 ? 'bad' : p >= 75 ? 'warn' : 'ok')
</script>

<template>
  <section v-if="d.firewall_states || d.dhcp_pools?.length" class="block" data-test="usage">
    <h3>{{ t('panel.usage') }}</h3>
    <div v-if="d.firewall_states" class="bar-row" data-test="fw-states">
      <span class="grow">{{ t('panel.firewallStates') }}</span>
      <span class="mono">
        {{ d.firewall_states.current?.toLocaleString(locale) ?? '?' }}
        <template v-if="d.firewall_states.limit">
          / {{ d.firewall_states.limit.toLocaleString(locale) }}</template
        >
      </span>
      <span v-if="statesPct !== undefined" class="meter" :class="tone(statesPct)">
        <i :style="{ width: `${statesPct}%` }" />
      </span>
    </div>
    <div v-for="p in d.dhcp_pools" :key="p.network" class="bar-row" data-test="dhcp-pool">
      <span class="grow">{{ t('panel.dhcpPool', { network: p.network }) }}</span>
      <span class="mono">{{ p.used ?? '?' }} / {{ p.total ?? '?' }}</span>
      <span
        v-if="pct(p.used, p.total) !== undefined"
        class="meter"
        :class="tone(pct(p.used, p.total))"
      >
        <i :style="{ width: `${pct(p.used, p.total)}%` }" />
      </span>
    </div>
  </section>

  <section v-if="d.vlans?.length" class="block" data-test="vlans">
    <h3>VLANs ({{ d.vlans.length }})</h3>
    <ul class="rows">
      <li v-for="v in d.vlans" :key="v.id">
        <span class="tag mono">{{ v.id }}</span>
        <span class="grow">{{ v.name }}</span>
        <span class="muted mono">{{ [v.interface, v.subnet].filter(Boolean).join(' · ') }}</span>
      </li>
    </ul>
  </section>

  <section v-if="d.vpn_peers?.length" class="block" data-test="vpn">
    <h3>{{ t('panel.vpn') }} ({{ d.vpn_peers.length }})</h3>
    <ul class="rows">
      <li v-for="p in d.vpn_peers" :key="`${p.protocol}|${p.name}`">
        <span class="dot" :class="{ online: p.connected }" />
        <span class="grow">
          <strong>{{ p.name }}</strong>
          <span class="muted"> · {{ t(`panel.vpnProtocol.${p.protocol}`) }}</span>
          <span v-if="p.endpoint" class="muted mono"> · {{ p.endpoint }}</span>
        </span>
        <span class="muted small">
          <template v-if="p.last_handshake">{{ formatAgo(p.last_handshake, locale) }}</template>
          <template v-if="p.rx_bytes !== undefined">
            · ↓ {{ formatBytes(p.rx_bytes) }} ↑ {{ formatBytes(p.tx_bytes) }}</template
          >
        </span>
      </li>
    </ul>
  </section>

  <section v-if="services.length" class="block" data-test="services">
    <h3>
      {{ t('panel.services') }} ({{ services.length }})
      <span v-if="stopped" class="stopped">{{ t('panel.servicesStopped', { n: stopped }) }}</span>
    </h3>
    <ul class="services">
      <li
        v-for="s in services"
        :key="s.name"
        :class="{ off: s.running === false }"
        :title="s.description || s.name"
      >
        <span class="dot" :class="{ online: s.running !== false }" />
        {{ s.description || s.name }}
      </li>
    </ul>
  </section>
</template>

<style scoped>
.rows {
  display: grid;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: 13px;
}
.rows li {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.grow {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tag {
  min-width: 34px;
  padding: 0 6px;
  border-radius: var(--radius-sm);
  background: var(--accent-soft);
  color: var(--accent);
  font-size: 12px;
  text-align: center;
}
.small {
  font-size: 12px;
  white-space: nowrap;
}
.bar-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
  font-size: 13px;
}
.meter {
  flex: none;
  width: 80px;
  height: 6px;
  overflow: hidden;
  border-radius: 3px;
  background: var(--border);
}
.meter i {
  display: block;
  height: 100%;
  background: var(--ok);
}
.meter.warn i {
  background: var(--warn);
}
.meter.bad i {
  background: var(--danger);
}
.services {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 4px 12px;
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: 12.5px;
}
.services li {
  display: flex;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.services li.off {
  color: var(--danger);
}
.dot {
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--danger);
}
.dot.online {
  background: var(--ok);
}
.stopped {
  margin-left: 6px;
  color: var(--danger);
  font-size: 11.5px;
  font-weight: 500;
  text-transform: none;
  letter-spacing: 0;
}
</style>
