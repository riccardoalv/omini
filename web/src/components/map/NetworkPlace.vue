<script setup lang="ts">
import { Lock, MoveRight, Unlock } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import { inNetwork } from '@/lib/networks'
import type { TopoNode } from '@/lib/types'
import { describeVlans } from '@/lib/vlans'

import DnsNameRow from './DnsNameRow.vue'
import PanelSection from './PanelSection.vue'
import PortVlansEditor from './PortVlansEditor.vue'

/**
 * Where a device is on the network, and what Omini can change about it: a
 * fixed address on the router (a DHCP reservation), and the VLAN of the
 * switch port it is on.
 */
const props = defineProps<{
  node: TopoNode
  nodes: TopoNode[]
  nameOf: (id: string) => string
  allowDestructive: boolean
}>()
const emit = defineEmits<{ applied: [] }>()
const { t } = useI18n()

const mac = computed(() => props.node.mac?.toLowerCase())

/** The router that serves the device's address and can fix it. */
const router = computed(() =>
  props.nodes.find(
    (r) =>
      r.changes?.includes('reservation.set') &&
      !!props.node.ip &&
      (r.device?.networks ?? []).some((nw) => nw.gateway && inNetwork(props.node.ip!, nw.gateway)),
  ),
)
/** Its fixed address, on whichever router has it. */
const reservation = computed(() => {
  for (const r of props.nodes)
    for (const x of r.device?.dhcp_reservations ?? [])
      if (x.mac.toLowerCase() === mac.value) return { router: r, ...x }
  return undefined
})

/** An address outside the DHCP range, when the router refuses one inside. */
const proposed = ref('')
const ip = ref('')
watch(
  () => [props.node.id, reservation.value?.ip, props.node.ip, !!router.value],
  async () => {
    proposed.value = ''
    ip.value = reservation.value?.ip ?? props.node.ip ?? ''
    if (reservation.value || !router.value) return
    const id = props.node.id
    const list = await api.fixCandidates().catch(() => [])
    const c = list.find((x) => x.node_id === id)
    if (c?.proposed && props.node.id === id && !reservation.value) {
      proposed.value = c.proposed
      ip.value = c.proposed
    }
  },
  { immediate: true },
)
const busy = ref(false)
const error = ref('')
async function fix() {
  busy.value = true
  error.value = ''
  try {
    await api.reserve(props.node.id, ip.value.trim())
    emit('applied')
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    busy.value = false
  }
}
async function free() {
  busy.value = true
  error.value = ''
  try {
    await api.unreserve(props.node.id)
    emit('applied')
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    busy.value = false
  }
}

/**
 * The port whose VLANs put the device in a VLAN: its own (a VM's virtual
 * NIC, changed on the hypervisor), else the switch port it is on.
 */
const place = computed(() => {
  const own = props.node.changes?.includes('port.vlans')
    ? (props.node.device?.interfaces ?? []).filter((i) => i.vlans)
    : []
  if (own.length === 1) return { sw: props.node, port: own[0]!, sharing: 0 }
  const sw = props.nodes.find((x) => x.id === props.node.parent_id)
  const port = sw?.device?.interfaces?.find((i) => i.name === props.node.port)
  if (!sw || !port?.vlans || !sw.changes?.includes('port.vlans')) return undefined
  const sharing = props.nodes.filter(
    (x) => x.parent_id === sw.id && x.port === port.name && x.id !== props.node.id,
  ).length
  return { sw, port, sharing }
})
const moving = ref(false)

const summary = computed(() =>
  [
    place.value ? describeVlans(place.value.port.vlans!, t) : '',
    reservation.value ? t('place.fixed', { ip: reservation.value.ip }) : '',
  ]
    .filter(Boolean)
    .join(' · '),
)
const shown = computed(() => !!place.value || !!router.value || !!reservation.value)
</script>

<template>
  <PanelSection
    v-if="shown"
    id="place"
    :title="t('place.title')"
    :summary="summary"
    data-test="network-place"
  >
    <div class="place">
      <div v-if="router || reservation" class="row" data-test="fixed-ip">
        <template v-if="reservation">
          <Lock :size="15" class="icon" />
          <span class="grow">
            {{ t('place.fixedOn', { ip: reservation.ip, router: nameOf(reservation.router.id) }) }}
          </span>
          <button
            class="btn small"
            type="button"
            :disabled="busy"
            data-test="free-ip"
            @click="free"
          >
            <Unlock :size="14" />{{ t('place.free') }}
          </button>
        </template>
        <template v-else>
          <Unlock :size="15" class="icon" />
          <input v-model="ip" class="input mono small-input" :aria-label="t('place.ip')" />
          <button
            class="btn small primary"
            type="button"
            :disabled="busy || !ip"
            data-test="fix-ip"
            @click="fix"
          >
            <Lock :size="14" />{{ t('place.fix') }}
          </button>
        </template>
      </div>
      <p v-if="router && !reservation" class="muted small">
        {{ t('place.fixHint', { router: nameOf(router.id) }) }}
        <template v-if="proposed && ip === proposed">{{ t('fixIp.proposed') }}</template>
      </p>
      <DnsNameRow
        v-if="router || reservation"
        :key="node.id + (reservation?.ip ?? '') + node.label"
        :node-id="node.id"
        :name-of="nameOf"
      />

      <template v-if="place">
        <div class="row" data-test="port-vlan">
          <MoveRight :size="15" class="icon" />
          <span class="grow">
            {{
              t('place.onPort', {
                port: place.port.name,
                device: nameOf(place.sw.id),
                vlans: describeVlans(place.port.vlans!, t),
              })
            }}
          </span>
          <button
            v-if="!moving"
            class="btn small"
            type="button"
            data-test="move-vlan"
            @click="moving = true"
          >
            {{ t('place.move') }}
          </button>
        </div>
        <p v-if="place.sharing" class="warn small">
          {{ t('place.sharing', { n: place.sharing }, place.sharing) }}
        </p>
        <p v-if="moving" class="muted small">{{ t('place.renew') }}</p>
        <PortVlansEditor
          v-if="moving"
          :node-id="place.sw.id"
          :port="place.port"
          :name-of="nameOf"
          :allow-destructive="allowDestructive"
          @close="moving = false"
          @applied="emit('applied')"
        />
      </template>
      <p v-if="error" class="error small" role="alert">{{ error }}</p>
    </div>
  </PanelSection>
</template>

<style scoped>
.place {
  display: grid;
  gap: 8px;
}
.row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.grow {
  flex: 1;
}
.icon {
  color: var(--text-muted);
}
.small-input {
  flex: 1;
  height: 30px;
}
.small {
  margin: 0;
  font-size: 12px;
}
.warn {
  color: var(--warn);
}
.error {
  color: var(--danger);
}
.mono {
  font-family: var(--mono);
}
</style>
