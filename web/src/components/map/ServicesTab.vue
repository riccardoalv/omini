<script setup lang="ts">
import { CircleAlert, CircleCheck, CircleHelp, Lock, Search } from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/lib/api'
import { formatAgo, formatUptime } from '@/lib/format'
import { displayName } from '@/lib/names'
import type { DhcpOffer, DnsQuery, FixCandidate, ServicesView, TopoNode } from '@/lib/types'
import { DNS_SLOW_MS } from '@/lib/services'

import FixIpDialog from './FixIpDialog.vue'

/**
 * DNS and DHCP: what the network scan's checks found every round (who answers
 * DHCP, how each DNS server resolves), with the pools and leases the routers
 * report. A tab each.
 */
const emit = defineEmits<{ select: [id: string] }>()
const { t, locale } = useI18n()

const data = ref<ServicesView>()
const nodes = ref(new Map<string, TopoNode>())
const loading = ref(true)
const query = ref('')

const tab = ref<'dhcp' | 'dns'>('dhcp')
/** Devices on DHCP without a reservation; the infrastructure ones get a button. */
const candidates = ref<FixCandidate[]>([])
const infraCount = computed(() => candidates.value.filter((c) => c.infra).length)
const fixing = ref(false)
function go(t: 'dhcp' | 'dns') {
  tab.value = t
}

async function load() {
  try {
    const [view, topo, fix] = await Promise.all([
      api.services(),
      api.topology(),
      api.fixCandidates().catch(() => []),
    ])
    data.value = view
    candidates.value = fix
    nodes.value = new Map(topo.topology.nodes.map((n) => [n.id, n]))
  } finally {
    loading.value = false
  }
}
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  timer = setInterval(load, 60_000)
})
onBeforeUnmount(() => clearInterval(timer))

const name = (id?: string) => {
  const n = id ? nodes.value.get(id) : undefined
  return n ? displayName(n, t) : ''
}
/** A device's name opens it on the map. */
function open(id?: string) {
  if (id) emit('select', id)
}

// DHCP: one line per interface checked, and every server that answered.
type Tone = 'ok' | 'bad' | 'unknown'
const checks = computed(() =>
  (data.value?.dhcp.checks ?? []).map((c) => {
    const unknown = c.offers.length > 1 ? c.offers.filter((o) => !o.known) : []
    let tone: Tone = 'ok'
    let text = t('services.dhcp.answered', { n: c.offers.length }, c.offers.length)
    if (c.error) {
      tone = 'unknown'
      text = t('services.dhcp.notChecked', { error: c.error })
    } else if (!c.offers.length) {
      tone = 'bad'
      text = t('services.dhcp.noAnswer')
    } else if (unknown.length) {
      tone = 'bad'
      text = t('services.dhcp.rogue', { ips: unknown.map((o) => o.server_ip).join(', ') })
    }
    return { iface: c.interface, tone, text }
  }),
)
const servers = computed(() => {
  const out: (DhcpOffer & { rogue: boolean })[] = []
  for (const c of data.value?.dhcp.checks ?? [])
    for (const o of c.offers)
      if (!out.some((x) => x.server_ip === o.server_ip))
        out.push({ ...o, rogue: c.offers.length > 1 && !o.known })
  return out
})
const pools = computed(() => data.value?.dhcp.pools ?? [])
const pct = (used?: number, total?: number) =>
  used !== undefined && total ? Math.round((used / total) * 100) : undefined
const leases = computed(() => {
  const q = query.value.trim().toLowerCase()
  return (data.value?.dhcp.leases ?? []).filter(
    (l) =>
      !q ||
      [l.ip, l.mac, l.hostname ?? '', name(l.client), name(l.node_id)]
        .join(' ')
        .toLowerCase()
        .includes(q),
  )
})

// DNS: a line per server, with its external and local queries.
const dnsServers = computed(() =>
  (data.value?.dns.servers ?? []).map((s) => {
    let tone: Tone = 'ok'
    let status = t('services.dns.ok')
    if (!s.external?.ok) {
      tone = 'bad'
      status = t('services.dns.down')
    } else if ((s.external.rtt_ms ?? 0) > DNS_SLOW_MS) {
      tone = 'bad'
      status = t('services.dns.slow')
    } else if (s.local && !s.local.ok) {
      tone = 'bad'
      status = t('services.dns.localFails')
    }
    return { ...s, tone, status }
  }),
)
function queryText(q?: DnsQuery) {
  if (!q) return '—'
  if (!q.ok) return t('services.dns.failed', { error: errorText(q.error) })
  return `${q.answers?.[0] ?? ''} · ${Math.round(q.rtt_ms ?? 0)} ms`
}
function errorText(e?: string) {
  if (e === 'timeout' || e === 'refused' || e === 'not found')
    return t(`services.dns.errors.${e.replace(' ', '')}`)
  return e ?? ''
}
</script>

<template>
  <div class="services">
    <header class="page-header">
      <div>
        <p class="muted">{{ t('services.subtitle') }}</p>
      </div>
      <p v-if="data?.generated_at" class="muted small">
        {{ t('services.checked', { when: formatAgo(data.generated_at, locale) }) }}
      </p>
    </header>

    <div class="tabs" role="tablist">
      <button
        v-for="tb in ['dhcp', 'dns'] as const"
        :key="tb"
        type="button"
        role="tab"
        class="tab"
        :class="{ active: tab === tb }"
        :aria-selected="tab === tb"
        :data-test="`tab-${tb}`"
        @click="go(tb)"
      >
        {{ t(`services.tabs.${tb}`) }}
      </button>
    </div>

    <p v-if="loading" class="muted">{{ t('common.loading') }}</p>

    <template v-else-if="tab === 'dhcp'">
      <div v-if="candidates.length" class="card fix" data-test="fix-ip">
        <Lock :size="18" />
        <span>
          {{
            infraCount
              ? t('fixIp.banner', { n: infraCount }, infraCount)
              : t('fixIp.bannerOthers', { n: candidates.length }, candidates.length)
          }}
        </span>
        <button class="btn" type="button" data-test="fix-open" @click="fixing = true">
          {{ t('fixIp.open') }}
        </button>
      </div>
      <FixIpDialog
        v-if="fixing"
        :candidates="candidates"
        :name-of="name"
        @close="fixing = false"
        @applied="load"
      />
      <div v-if="!checks.length && !pools.length" class="card empty" data-test="dhcp-empty">
        <p>{{ t('services.dhcp.none') }}</p>
        <p class="muted small">{{ t('services.dhcp.noneHint') }}</p>
      </div>
      <template v-else>
        <ul v-if="checks.length" class="status" data-test="dhcp-checks">
          <li v-for="c in checks" :key="c.iface" class="card" :class="c.tone">
            <component
              :is="c.tone === 'ok' ? CircleCheck : c.tone === 'bad' ? CircleAlert : CircleHelp"
              :size="18"
            />
            <span class="mono iface">{{ c.iface }}</span>
            <span>{{ c.text }}</span>
          </li>
        </ul>

        <section v-if="servers.length" class="block">
          <h2>{{ t('services.dhcp.servers') }}</h2>
          <div class="card table-wrap">
            <table class="table" data-test="dhcp-servers">
              <thead>
                <tr>
                  <th>{{ t('services.dhcp.server') }}</th>
                  <th class="hide-sm">{{ t('services.dhcp.gives') }}</th>
                  <th class="num">{{ t('services.dhcp.answer') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="s in servers" :key="s.server_ip" :class="{ rogue: s.rogue }">
                  <td>
                    <button v-if="s.node_id" class="link" type="button" @click="open(s.node_id)">
                      {{ name(s.node_id) }}
                    </button>
                    <span class="mono"> {{ s.server_ip }}</span>
                    <span v-if="s.server_mac" class="muted mono small"> · {{ s.server_mac }}</span>
                    <span v-if="s.rogue" class="badge bad">{{ t('services.dhcp.unknown') }}</span>
                  </td>
                  <td class="hide-sm small">
                    <span v-if="s.routers?.length">
                      {{ t('services.dhcp.gateway') }}
                      <span class="mono">{{ s.routers.join(', ') }}</span>
                    </span>
                    <span v-if="s.dns?.length">
                      · DNS <span class="mono">{{ s.dns.join(', ') }}</span>
                    </span>
                    <span v-if="s.domain"> · {{ s.domain }}</span>
                    <span v-if="s.lease_s">
                      · {{ t('services.dhcp.lease', { time: formatUptime(s.lease_s) }) }}
                    </span>
                  </td>
                  <td class="num mono">
                    {{ s.rtt_ms !== undefined ? `${Math.round(s.rtt_ms)} ms` : '' }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section v-if="pools.length" class="block">
          <h2>{{ t('services.dhcp.pools') }}</h2>
          <div class="card table-wrap">
            <table class="table" data-test="dhcp-pools">
              <tbody>
                <tr v-for="p in pools" :key="p.node_id + p.network">
                  <td>
                    <span class="mono">{{ p.network }}</span>
                    <span class="muted small"> · {{ name(p.node_id) }}</span>
                  </td>
                  <td class="usage">
                    <div v-if="pct(p.used, p.total) !== undefined" class="bar">
                      <i
                        :class="{
                          warn: pct(p.used, p.total)! >= 75,
                          bad: pct(p.used, p.total)! >= 90,
                        }"
                        :style="{ width: `${pct(p.used, p.total)}%` }"
                      />
                    </div>
                  </td>
                  <td class="num mono">
                    <template v-if="p.total">{{ p.used ?? 0 }} / {{ p.total }}</template>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section v-if="data?.dhcp.leases.length" class="block">
          <div class="block-head">
            <h2>{{ t('services.dhcp.leases', { n: data.dhcp.leases.length }) }}</h2>
            <label class="search">
              <Search :size="16" />
              <input
                v-model="query"
                class="input"
                type="search"
                :placeholder="t('services.dhcp.search')"
              />
            </label>
          </div>
          <div class="card table-wrap">
            <table class="table" data-test="dhcp-leases">
              <thead>
                <tr>
                  <th>IP</th>
                  <th>{{ t('services.dhcp.device') }}</th>
                  <th class="hide-sm">MAC</th>
                  <th class="hide-sm">{{ t('services.dhcp.expires') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="l in leases" :key="l.node_id + l.mac + l.ip">
                  <td class="mono">{{ l.ip }}</td>
                  <td>
                    <button v-if="l.client" class="link" type="button" @click="open(l.client)">
                      {{ name(l.client) }}
                    </button>
                    <span v-else>{{ l.hostname ?? '' }}</span>
                  </td>
                  <td class="hide-sm mono small">{{ l.mac }}</td>
                  <td class="hide-sm small muted">
                    {{ l.expires_at ? formatAgo(l.expires_at, locale) : '' }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </template>
    </template>

    <template v-else>
      <div v-if="!dnsServers.length" class="card empty" data-test="dns-empty">
        <p>{{ t('services.dns.none') }}</p>
        <p class="muted small">{{ t('services.dns.noneHint') }}</p>
      </div>
      <div v-else class="card table-wrap">
        <table class="table" data-test="dns-servers">
          <thead>
            <tr>
              <th>{{ t('services.dns.server') }}</th>
              <th>{{ t('services.dns.status') }}</th>
              <th class="hide-sm">{{ t('services.dns.external') }}</th>
              <th class="hide-sm">{{ t('services.dns.local') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in dnsServers" :key="s.server">
              <td>
                <button v-if="s.node_id" class="link" type="button" @click="open(s.node_id)">
                  {{ name(s.node_id) }}
                </button>
                <span class="mono"> {{ s.server }}</span>
              </td>
              <td>
                <span class="badge" :class="s.tone">{{ s.status }}</span>
              </td>
              <td class="hide-sm small">
                <span class="muted">{{ s.external?.name }}</span>
                {{ queryText(s.external) }}
              </td>
              <td class="hide-sm small">
                <template v-if="s.local">
                  <span class="muted">{{ s.local.name }}</span>
                  {{ queryText(s.local) }}
                </template>
                <span v-else class="muted">—</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>

<style scoped>
.tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 16px;
  border-bottom: 1px solid var(--border);
}
.tab {
  padding: 8px 14px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: none;
  color: var(--text-muted);
  font: inherit;
  font-weight: 600;
  cursor: pointer;
}
.tab.active {
  border-bottom-color: var(--accent);
  color: var(--text);
}
.status {
  display: grid;
  gap: 8px;
  margin: 0 0 16px;
  padding: 0;
  list-style: none;
}
.status li {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
}
.status .ok svg {
  color: var(--ok);
}
.fix {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-bottom: 12px;
}
.fix span {
  flex: 1;
}
.status .bad {
  border-color: var(--danger);
}
.status .bad svg {
  color: var(--danger);
}
.status .unknown svg {
  color: var(--text-muted);
}
.iface {
  min-width: 64px;
  color: var(--text-muted);
}
.block {
  margin-top: 20px;
}
.block h2 {
  margin: 0 0 8px;
  font-size: 13px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.block-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}
.block-head h2 {
  margin: 0;
}
.search {
  position: relative;
  display: flex;
  align-items: center;
  max-width: 280px;
}
.search svg {
  position: absolute;
  left: 10px;
  color: var(--text-muted);
}
.search .input {
  padding-left: 34px;
}
.table-wrap {
  overflow-x: auto;
}
.num {
  text-align: right;
  white-space: nowrap;
}
.usage {
  width: 40%;
}
.bar {
  height: 6px;
  border-radius: 3px;
  background: var(--border);
}
.bar i {
  display: block;
  height: 100%;
  border-radius: 3px;
  background: var(--ok);
}
.bar i.warn {
  background: var(--warn);
}
.bar i.bad {
  background: var(--danger);
}
.badge {
  display: inline-block;
  margin-left: 6px;
  padding: 1px 8px;
  border-radius: 999px;
  font-size: 11.5px;
  font-weight: 600;
}
.badge.ok {
  background: color-mix(in srgb, var(--ok) 18%, transparent);
  color: var(--ok);
}
.badge.bad {
  background: color-mix(in srgb, var(--danger) 18%, transparent);
  color: var(--danger);
}
tr.rogue td {
  background: color-mix(in srgb, var(--danger) 8%, transparent);
}
.empty {
  display: grid;
  justify-items: center;
  gap: 6px;
  padding: 32px;
  text-align: center;
}
.empty p {
  margin: 0;
}
.small {
  font-size: 12.5px;
}
@media (max-width: 760px) {
  .hide-sm {
    display: none;
  }
}
</style>
