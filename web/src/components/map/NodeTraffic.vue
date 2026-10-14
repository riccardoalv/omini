<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { displayName } from '@/lib/names'
import { serviceName } from '@/lib/services'
import { formatRate } from '@/lib/traffic'
import type { Conversation, Destination, InternetUsage, TopoNode } from '@/lib/types'

import PanelSection from './PanelSection.vue'

/**
 * A device's traffic from the flow exports: where it goes on the internet (by
 * name) and whom it talks to on the network, which the map can highlight. On
 * the internet's node: who uses the internet now.
 */
const props = defineProps<{ node: TopoNode; nodes: TopoNode[] }>()
const emit = defineEmits<{ select: [id: string]; peers: [ids: string[] | undefined] }>()
const { t } = useI18n()

const byId = computed(() => new Map(props.nodes.map((n) => [n.id, n])))
const label = (id: string | undefined, fallback: string) => {
  const n = id ? byId.value.get(id) : undefined
  return n ? displayName(n, t) : fallback
}

const dests = ref<Destination[]>([])
const talks = ref<Conversation[]>([])
const users = ref<InternetUsage[]>([])
const usersSeconds = ref(900)
const highlighted = ref(false)

/** A private IPv4 or link-local address: on the network even when not on the map. */
const privateAddr = (ip: string) =>
  /^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|169\.254\.|fe80:|f[cd][0-9a-f]{2}:)/i.test(ip)

async function load(n: TopoNode) {
  dests.value = []
  talks.value = []
  users.value = []
  highlighted.value = false
  emit('peers', undefined)
  const id = n.id
  try {
    if (n.kind === 'wan') {
      const res = await api.internet('15m')
      if (props.node.id === id) {
        users.value = res.devices.slice(0, 5)
        usersSeconds.value = res.seconds
      }
      return
    }
    if (n.kind === 'ssid' || n.kind === 'app') return
    const [d, f] = await Promise.all([
      api.destinations(id, 24),
      api.flows({ node: id, minutes: 60, limit: 30 }),
    ])
    if (props.node.id !== id) return
    dests.value = d.destinations.slice(0, 8)
    talks.value = f.conversations.filter((c) => {
      const mine = c.a_node === id
      return (mine ? c.b_node : c.a_node) || privateAddr(mine ? c.b : c.a)
    })
  } catch {
    // flows are optional
  }
}
watch(
  () => props.node.id,
  () => load(props.node),
  { immediate: true },
)

const peerOf = (c: Conversation) => {
  const mine = c.a_node === props.node.id
  const id = mine ? c.b_node : c.a_node
  return { id, label: label(id, mine ? c.b : c.a) }
}
const peerIds = computed(() => [
  ...new Set(talks.value.map((c) => peerOf(c).id).filter((id): id is string => !!id)),
])
function togglePeers() {
  highlighted.value = !highlighted.value
  emit('peers', highlighted.value ? [props.node.id, ...peerIds.value] : undefined)
}
const services = (ports: { proto: number; port: number }[]) =>
  ports
    .slice(0, 2)
    .map((p) => serviceName(p.proto, p.port))
    .join(', ')
</script>

<template>
  <PanelSection
    v-if="users.length"
    id="internet-users"
    :title="t('flows.usersNow')"
    data-test="internet-users"
  >
    <ul class="talks">
      <li v-for="u in users" :key="u.node">
        <button
          class="link grow"
          type="button"
          :disabled="!u.on_map"
          @click="u.on_map && emit('select', u.node)"
        >
          {{ label(u.node, u.label) }}
        </button>
        <span class="mono small">↓ {{ formatRate((u.rx * 8) / usersSeconds) }}</span>
        <span class="mono small muted">↑ {{ formatRate((u.tx * 8) / usersSeconds) }}</span>
      </li>
    </ul>
    <RouterLink class="link small" to="/internet">{{ t('flows.allUsage') }}</RouterLink>
  </PanelSection>

  <PanelSection
    v-if="dests.length"
    id="goes-to"
    :title="t('flows.goesTo')"
    data-test="node-destinations"
  >
    <p class="muted small hint">{{ t('flows.goesToHint') }}</p>
    <ul class="talks">
      <li v-for="d in dests" :key="d.name">
        <span class="grow" :title="d.name">{{ d.name }}</span>
        <span class="muted small">{{ services(d.ports) }}</span>
        <span class="mono small">↓ {{ formatBytes(d.rx) }}</span>
        <span class="mono small muted">↑ {{ formatBytes(d.tx) }}</span>
      </li>
    </ul>
    <RouterLink class="link small" to="/internet">{{ t('flows.allUsage') }}</RouterLink>
  </PanelSection>

  <PanelSection v-if="talks.length" id="talks" :title="t('flows.local')" data-test="node-talks">
    <ul class="talks">
      <li v-for="c in talks.slice(0, 8)" :key="c.a + c.b">
        <button
          class="link grow"
          type="button"
          :disabled="!peerOf(c).id"
          @click="peerOf(c).id && emit('select', peerOf(c).id!)"
        >
          {{ peerOf(c).label }}
        </button>
        <span class="muted small">{{ services(c.ports) }}</span>
        <span class="mono small">{{ formatBytes(c.bytes_ab + c.bytes_ba) }}</span>
      </li>
    </ul>
    <button
      v-if="peerIds.length"
      class="btn small"
      type="button"
      :aria-pressed="highlighted"
      data-test="show-peers"
      @click="togglePeers"
    >
      {{ highlighted ? t('flows.hidePeers') : t('flows.showPeers') }}
    </button>
  </PanelSection>
</template>

<style scoped>
.talks {
  display: grid;
  gap: 4px;
  margin: 0 0 6px;
  padding: 0;
  list-style: none;
  font-size: 13px;
}
.talks li {
  display: flex;
  align-items: center;
  gap: 8px;
}
.grow {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.small {
  font-size: 12px;
  white-space: nowrap;
}
.hint {
  margin: 0 0 6px;
  white-space: normal;
}
</style>
