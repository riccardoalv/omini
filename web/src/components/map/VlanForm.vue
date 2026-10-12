<script setup lang="ts">
import { Check, Globe, Lock, Network as NetworkIcon, Pencil, X } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import ToggleSwitch from '@/components/ToggleSwitch.vue'
import { api, ApiError } from '@/lib/api'
import { inNetwork, subnetOf, suggestGateway, suggestRange } from '@/lib/networks'
import type { Network, NetworkAccess, NetworkVlan } from '@/lib/types'
import { VLAN_COLORS, vlanHex } from '@/lib/vlans'

/**
 * Creating or changing a VLAN: its id, name and color and, with a router that
 * can serve it, its network (gateway, DHCP, access), which Omini applies to
 * the router in one go.
 */
const props = defineProps<{
  /** The VLAN to change; none: a new one. */
  vlan?: NetworkVlan
  /** Every VLAN, for a free id and a free network. */
  vlans: NetworkVlan[]
  /** A router Omini can configure serves the network's VLANs. */
  router: boolean
  /** Destructive changes are allowed (Settings). */
  allowDestructive: boolean
  /** Access points Omini can configure: the VLAN can have a Wi-Fi network. */
  wifi?: boolean
}>()
const emit = defineEmits<{ saved: []; cancel: [] }>()
const { t } = useI18n()

const ACCESS: Exclude<NetworkAccess, 'custom'>[] = ['internet', 'all', 'local']
const accessIcon = { internet: Globe, all: NetworkIcon, local: Lock } as const

const usedNetworks = computed(() =>
  props.vlans.flatMap((v) => [v.network?.gateway, v.subnet].filter((x): x is string => !!x)),
)
function freeId() {
  const used = new Set(props.vlans.map((v) => v.id))
  let id = 10
  while (used.has(id)) id += 10
  return id
}

const isNew = !props.vlan
const id = ref(props.vlan?.id ?? freeId())
const name = ref(props.vlan?.name ?? '')
const color = ref(
  props.vlan?.color || VLAN_COLORS[props.vlans.length % VLAN_COLORS.length] || VLAN_COLORS[0],
)
const subnet = ref(props.vlan?.subnet ?? '')
/** The VLAN gets (or has) a network on the router. */
const withNetwork = ref(!!props.vlan?.network || (isNew && props.router))
const current = props.vlan?.network
const gateway = ref(current?.gateway ?? suggestGateway(id.value, usedNetworks.value))
const dhcpOn = ref(current ? !!current.dhcp?.enabled : true)
const range0 = current?.dhcp?.enabled ? current.dhcp : suggestRange(gateway.value)
const from = ref(range0?.from ?? '')
const to = ref(range0?.to ?? '')
const access = ref<NetworkAccess>(current?.access ?? 'internet')
const mdns = ref(!!current?.mdns)
/** The router reports no mDNS repeater (null) for its networks: no toggle. */
const canMdns = computed(
  () => current?.mdns !== null && !props.vlans.some((v) => v.network && v.network.mdns === null),
)

// A new VLAN follows its id and gateway until the user types their own.
let gatewayTouched = !isNew
let rangeTouched = !!current?.dhcp?.enabled
const touchGateway = () => (gatewayTouched = true)
const touchRange = () => (rangeTouched = true)
watch(id, (v) => {
  if (!gatewayTouched) gateway.value = suggestGateway(Number(v), usedNetworks.value)
})
// The range follows the gateway, unless the user typed one that still fits.
watch(gateway, (g) => {
  if (rangeTouched && inNetwork(from.value, g) && inNetwork(to.value, g)) return
  const r = suggestRange(g)
  if (r) [from.value, to.value] = [r.from, r.to]
})

const network = computed(() => subnetOf(gateway.value)?.cidr)
const problem = computed(() => {
  if (!withNetwork.value) return ''
  if (!network.value) return t('vlans.net.badGateway')
  if (
    dhcpOn.value &&
    (!inNetwork(from.value, gateway.value) || !inNetwork(to.value, gateway.value))
  )
    return t('vlans.net.badRange', { network: network.value })
  return ''
})
/** Choosing a preset over rules set by hand replaces them. */
const replacesRules = computed(() => current?.access === 'custom' && access.value !== 'custom')

// The VLAN's Wi-Fi: its first network (name, password, on or off). The
// password is sent to the access points and never shown back.
const currentWifi = props.vlan?.wifi?.[0]
const withWifi = ref(!!currentWifi)
const ssid = ref(currentWifi?.ssid ?? '')
const password = ref('')
const wifiOn = ref(currentWifi ? currentWifi.enabled : true)
watch(name, (n) => {
  if (!currentWifi && !ssidTouched) ssid.value = n
})
let ssidTouched = false
const touchSsid = () => (ssidTouched = true)
const wifiProblem = computed(() => {
  if (!props.wifi || !withWifi.value) return ''
  if (!ssid.value.trim()) return t('vlans.wifi.needName')
  if (!currentWifi && password.value.length < 8) return t('vlans.wifi.needPassword')
  if (password.value && (password.value.length < 8 || password.value.length > 63))
    return t('vlans.wifi.badPassword')
  return ''
})
/** The Wi-Fi change to make after the VLAN is saved, if any. */
async function saveWifi() {
  if (!props.wifi) return
  if (!withWifi.value) {
    if (currentWifi) await api.deleteWifi(currentWifi.ssid)
    return
  }
  const changed =
    !currentWifi ||
    ssid.value.trim() !== currentWifi.ssid ||
    !!password.value ||
    wifiOn.value !== currentWifi.enabled
  if (!changed) return
  await api.setWifi({
    ssid: ssid.value.trim(),
    previous: currentWifi && currentWifi.ssid !== ssid.value.trim() ? currentWifi.ssid : undefined,
    vlan: Number(id.value),
    password: password.value || undefined,
    enabled: wifiOn.value,
  })
}

const busy = ref(false)
const error = ref('')
/** The router change is destructive (the network Omini is in): asked again. */
const again = ref<'ask' | 'blocked'>()

function value(): Network | undefined {
  if (!withNetwork.value) return undefined
  return {
    vlan: Number(id.value),
    gateway: gateway.value.trim(),
    dhcp: dhcpOn.value
      ? { enabled: true, from: from.value.trim(), to: to.value.trim() }
      : { enabled: false },
    access: access.value,
    mdns: canMdns.value ? mdns.value : undefined,
  }
}

async function save(confirm = false) {
  if (problem.value || wifiProblem.value) return
  busy.value = true
  error.value = ''
  try {
    const body = {
      name: name.value,
      color: color.value,
      network: value(),
      confirm,
      ...(withNetwork.value ? {} : { subnet: subnet.value }),
    }
    if (isNew) await api.createVlan({ id: Number(id.value), ...body })
    else await api.updateVlan(Number(id.value), body)
    await saveWifi()
    emit('saved')
  } catch (e) {
    if (e instanceof ApiError && e.status === 409 && e.message === 'destructive') {
      again.value = props.allowDestructive ? 'ask' : 'blocked'
    } else {
      error.value = e instanceof ApiError ? e.message : t('common.error')
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="card vlan-form" data-test="vlan-form" @submit.prevent="save()">
    <div class="row">
      <label class="field id">
        <span>{{ t('vlans.id') }}</span>
        <input
          v-model.number="id"
          class="input mono"
          type="number"
          min="1"
          max="4094"
          required
          :disabled="!isNew"
          data-test="vlan-id"
        />
      </label>
      <label class="field grow">
        <span>{{ t('vlans.name') }}</span>
        <input
          v-model="name"
          class="input"
          maxlength="32"
          :placeholder="t('vlans.namePlaceholder')"
          data-test="vlan-name"
        />
      </label>
    </div>
    <div class="field">
      <span>{{ t('vlans.color') }}</span>
      <div class="colors">
        <button
          v-for="c in VLAN_COLORS"
          :key="c"
          type="button"
          class="swatch"
          :class="{ on: color === c }"
          :style="{ background: vlanHex(c) }"
          :aria-label="c"
          @click="color = c"
        />
      </div>
    </div>

    <section v-if="router || vlan?.network" class="network" data-test="vlan-network">
      <div class="section-head">
        <div>
          <h4>{{ t('vlans.net.title') }}</h4>
          <p class="muted small">{{ t('vlans.net.hint') }}</p>
        </div>
        <ToggleSwitch
          v-if="!vlan?.network"
          v-model="withNetwork"
          :label="t('vlans.net.title')"
          data-test="with-network"
        />
      </div>
      <template v-if="withNetwork">
        <div class="row">
          <label class="field grow">
            <span>{{ t('vlans.net.gateway') }}</span>
            <input
              v-model="gateway"
              class="input mono"
              placeholder="192.168.30.1/24"
              data-test="gateway"
              @input="touchGateway"
            />
            <small v-if="network" class="muted">{{ t('vlans.net.network', { network }) }}</small>
          </label>
        </div>
        <div class="dhcp">
          <label class="inline">
            <ToggleSwitch v-model="dhcpOn" :label="t('vlans.net.dhcp')" data-test="dhcp" />
            <span>{{ t('vlans.net.dhcp') }}</span>
          </label>
          <div v-if="dhcpOn" class="range">
            <input
              v-model="from"
              class="input mono"
              :aria-label="t('vlans.net.from')"
              data-test="dhcp-from"
              @input="touchRange"
            />
            <span class="muted">{{ t('vlans.net.to') }}</span>
            <input
              v-model="to"
              class="input mono"
              :aria-label="t('vlans.net.until')"
              data-test="dhcp-to"
              @input="touchRange"
            />
          </div>
        </div>
        <div class="field">
          <span>{{ t('vlans.net.access') }}</span>
          <div class="access" role="radiogroup">
            <button
              v-for="a in ACCESS"
              :key="a"
              type="button"
              role="radio"
              class="choice"
              :class="{ on: access === a }"
              :aria-checked="access === a"
              :data-access="a"
              @click="access = a"
            >
              <component :is="accessIcon[a]" :size="16" />
              <strong>{{ t(`vlans.access.${a}`) }}</strong>
              <small class="muted">{{ t(`vlans.access.${a}Hint`) }}</small>
            </button>
            <button
              v-if="current?.access === 'custom'"
              type="button"
              role="radio"
              class="choice"
              :class="{ on: access === 'custom' }"
              :aria-checked="access === 'custom'"
              data-access="custom"
              @click="access = 'custom'"
            >
              <Pencil :size="16" />
              <strong>{{ t('vlans.access.custom') }}</strong>
              <small class="muted">{{ t('vlans.access.customHint') }}</small>
            </button>
          </div>
          <p v-if="replacesRules" class="warn small" data-test="replaces-rules">
            {{ t('vlans.net.replacesRules') }}
          </p>
        </div>
        <label v-if="canMdns" class="inline mdns">
          <ToggleSwitch v-model="mdns" :label="t('vlans.net.mdns')" data-test="mdns" />
          <span>
            {{ t('vlans.net.mdns') }}
            <small class="muted">{{ t('vlans.net.mdnsHint') }}</small>
          </span>
        </label>
      </template>
    </section>
    <label v-else class="field">
      <span>{{ t('vlans.subnet') }}</span>
      <input v-model="subnet" class="input mono" placeholder="192.168.20.0/24" />
    </label>

    <section v-if="wifi" class="network" data-test="vlan-wifi">
      <div class="section-head">
        <div>
          <h4>{{ t('vlans.wifi.title') }}</h4>
          <p class="muted small">{{ t('vlans.wifi.hint') }}</p>
        </div>
        <ToggleSwitch v-model="withWifi" :label="t('vlans.wifi.title')" data-test="with-wifi" />
      </div>
      <template v-if="withWifi">
        <div class="row">
          <label class="field grow">
            <span>{{ t('vlans.wifi.ssid') }}</span>
            <input
              v-model="ssid"
              class="input"
              maxlength="32"
              data-test="ssid"
              @input="touchSsid"
            />
          </label>
          <label class="field grow">
            <span>{{ t('vlans.wifi.password') }}</span>
            <input
              v-model="password"
              class="input"
              type="password"
              autocomplete="new-password"
              maxlength="63"
              :placeholder="currentWifi ? t('vlans.wifi.keep') : ''"
              data-test="wifi-password"
            />
          </label>
        </div>
        <label class="inline">
          <ToggleSwitch v-model="wifiOn" :label="t('vlans.wifi.on')" data-test="wifi-on" />
          <span>{{ t('vlans.wifi.on') }}</span>
        </label>
      </template>
      <p v-else-if="currentWifi" class="warn small">
        {{ t('vlans.wifi.removes', { ssid: currentWifi.ssid }) }}
      </p>
    </section>

    <p v-if="problem" class="error small" role="alert">{{ problem }}</p>
    <p v-if="wifiProblem" class="error small" role="alert">{{ wifiProblem }}</p>
    <p v-if="error" class="error small" role="alert">{{ error }}</p>
    <p v-if="again === 'blocked'" class="error small" data-test="blocked">
      {{ t('vlans.net.blocked') }}
    </p>
    <p v-else-if="again === 'ask'" class="warn small" data-test="ask-again">
      {{ t('vlans.net.askAgain') }}
    </p>
    <div class="actions">
      <button class="btn ghost" type="button" :disabled="busy" @click="emit('cancel')">
        <X :size="15" />{{ t('common.cancel') }}
      </button>
      <button
        v-if="again === 'ask'"
        class="btn danger"
        type="button"
        :disabled="busy"
        data-test="save-anyway"
        @click="save(true)"
      >
        {{ t('vlans.net.applyAnyway') }}
      </button>
      <button
        v-else
        class="btn primary"
        type="submit"
        :disabled="busy || !!problem || !!wifiProblem || again === 'blocked'"
        data-test="vlan-save"
      >
        <Check :size="15" />{{
          busy
            ? t('changes.applying')
            : withNetwork && router
              ? t('vlans.net.apply')
              : t('common.save')
        }}
      </button>
    </div>
  </form>
</template>

<style scoped>
.vlan-form {
  display: grid;
  gap: 14px;
  margin-bottom: 16px;
  padding: 14px;
}
.row {
  display: flex;
  gap: 12px;
}
.field {
  display: grid;
  gap: 4px;
  font-size: 12.5px;
  color: var(--text-muted);
}
.field.id {
  width: 110px;
}
.grow {
  flex: 1;
}
.colors {
  display: flex;
  gap: 6px;
}
.swatch {
  width: 22px;
  height: 22px;
  border: 2px solid transparent;
  border-radius: 50%;
  cursor: pointer;
}
.swatch.on {
  border-color: var(--text);
}
.network {
  display: grid;
  gap: 12px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
}
.section-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
h4 {
  margin: 0;
  font-size: 13.5px;
}
.small {
  margin: 2px 0 0;
  font-size: 12px;
}
.dhcp {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
}
.inline {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  cursor: pointer;
}
.mdns span {
  display: grid;
}
.range {
  display: flex;
  align-items: center;
  gap: 8px;
}
.range .input {
  width: 140px;
}
.access {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 8px;
}
.choice {
  display: grid;
  gap: 2px;
  justify-items: start;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-elevated);
  color: var(--text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.choice:hover {
  border-color: var(--border-strong);
}
.choice.on {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.choice small {
  font-size: 11.5px;
}
.actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
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
@media (max-width: 560px) {
  .row {
    flex-direction: column;
  }
  .field.id {
    width: auto;
  }
}
</style>
