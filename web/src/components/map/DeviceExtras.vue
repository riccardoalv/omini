<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import PanelSection from '@/components/map/PanelSection.vue'
import { formatAgo, formatBytes } from '@/lib/format'
import type { Device } from '@/lib/types'

/**
 * What some devices report besides ports and health: Wi-Fi radios, VLANs, services, VPN
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
const bands: Record<string, string> = { '2.4ghz': '2.4 GHz', '5ghz': '5 GHz', '6ghz': '6 GHz' }
/** The radios' channels at a glance: "2.4 GHz 6 · 5 GHz 36". */
const radiosSummary = computed(() =>
  (d.value.radios ?? [])
    .filter((r) => r.enabled !== false)
    .map((r) => `${bands[r.band] ?? r.band} ${r.channel ?? '?'}`)
    .join(' · '),
)
const tone = (p?: number) => (p === undefined ? '' : p >= 90 ? 'bad' : p >= 75 ? 'warn' : 'ok')
</script>

<template>
  <PanelSection
    v-if="d.radios?.length"
    id="radios"
    :title="t('panel.radios')"
    :summary="radiosSummary"
    data-test="radios"
  >
    <p v-if="d.wireless_uplink" class="muted small mesh">{{ t('panel.meshUnit') }}</p>
    <ul class="rows">
      <li
        v-for="r in d.radios"
        :key="r.name"
        :class="{ off: r.enabled === false }"
        data-test="radio"
      >
        <span class="tag band">{{ bands[r.band] ?? r.band }}</span>
        <span class="grow">
          <strong v-if="r.channel !== undefined">{{ t('panel.channel', { n: r.channel }) }}</strong>
          <span v-if="r.width_mhz" class="muted"> · {{ r.width_mhz }} MHz</span>
          <span v-if="r.auto" class="badge">{{ t('panel.radioAuto') }}</span>
          <span v-if="r.enabled === false" class="badge off">{{ t('panel.radioOff') }}</span>
        </span>
        <span class="muted small mono">
          <template v-if="r.tx_power_dbm !== undefined">{{ r.tx_power_dbm }} dBm · </template
          >{{ r.name }}
        </span>
      </li>
    </ul>
  </PanelSection>

  <PanelSection
    v-if="d.firewall_states || d.dhcp_pools?.length"
    id="usage"
    :title="t('panel.usage')"
    :summary="statesPct !== undefined ? `${statesPct}%` : ''"
    data-test="usage"
  >
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
  </PanelSection>

  <PanelSection
    v-if="d.vlans?.length"
    id="vlans"
    title="VLANs"
    :summary="String(d.vlans.length)"
    data-test="vlans"
  >
    <ul class="rows">
      <li v-for="v in d.vlans" :key="v.id">
        <span class="tag mono">{{ v.id }}</span>
        <span class="grow">{{ v.name }}</span>
        <span class="muted mono">{{ [v.interface, v.subnet].filter(Boolean).join(' · ') }}</span>
      </li>
    </ul>
  </PanelSection>

  <PanelSection
    v-if="d.vpn_peers?.length"
    id="vpn"
    :title="t('panel.vpn')"
    :summary="String(d.vpn_peers.length)"
    data-test="vpn"
  >
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
  </PanelSection>

  <PanelSection
    v-if="services.length"
    id="services"
    :title="t('panel.services')"
    :summary="stopped ? t('panel.servicesStopped', { n: stopped }) : String(services.length)"
    data-test="services"
  >
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
  </PanelSection>
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
.band {
  min-width: 56px;
}
.badge {
  margin-left: 6px;
  padding: 0 5px;
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  font-size: 11px;
}
.badge.off,
.rows li.off {
  color: var(--danger);
}
.mesh {
  margin: 0 0 6px;
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
