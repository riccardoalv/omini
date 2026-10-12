<script setup lang="ts">
import { Globe, Pencil } from 'lucide-vue-next'
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import type { DnsName } from '@/lib/types'

/**
 * A device's local DNS name: <name>.<domain> on the DNS server of its network,
 * kept by Omini while its address is fixed. It can be renamed or turned off.
 */
const props = defineProps<{ nodeId: string; nameOf: (id: string) => string }>()
const { t } = useI18n()

const dns = ref<DnsName>()
const editing = ref(false)
const label = ref('')
const busy = ref(false)
const error = ref('')

async function load() {
  try {
    dns.value = await api.dnsName(props.nodeId)
  } catch {
    dns.value = undefined
  }
}
onMounted(load)

async function set(name: string) {
  busy.value = true
  error.value = ''
  try {
    dns.value = await api.setDnsName(props.nodeId, name)
    editing.value = false
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    busy.value = false
  }
}
function edit() {
  label.value = dns.value?.label ?? ''
  editing.value = true
}
</script>

<template>
  <div v-if="dns" class="dns" data-test="dns-name">
    <form v-if="editing" class="row" @submit.prevent="set(label.trim())">
      <Globe :size="15" class="icon" />
      <input
        v-model="label"
        class="input mono small-input"
        :aria-label="t('dnsName.label')"
        data-test="dns-label"
      />
      <button class="btn small primary" type="submit" :disabled="busy">
        {{ t('common.save') }}
      </button>
      <button class="btn small ghost" type="button" @click="editing = false">
        {{ t('common.cancel') }}
      </button>
    </form>
    <div v-else class="row">
      <Globe :size="15" class="icon" />
      <span v-if="dns.name" class="grow">
        <span class="mono">{{ dns.name }}</span>
        <small v-if="dns.server" class="muted">
          · {{ t('dnsName.on', { server: nameOf(dns.server) }) }}</small
        >
      </span>
      <span v-else class="grow muted">
        {{ dns.off ? t('dnsName.off') : t(`dnsName.missing.${dns.missing ?? 'no_name'}`) }}
      </span>
      <template v-if="dns.missing !== 'no_fixed_ip' && dns.missing !== 'no_server'">
        <button
          v-if="!dns.off"
          class="btn small ghost icon-btn"
          type="button"
          :aria-label="t('dnsName.edit')"
          :disabled="busy"
          data-test="dns-edit"
          @click="edit"
        >
          <Pencil :size="14" />
        </button>
        <button
          v-if="dns.name"
          class="btn small"
          type="button"
          :disabled="busy"
          data-test="dns-off"
          @click="set('-')"
        >
          {{ t('dnsName.turnOff') }}
        </button>
        <button
          v-else-if="dns.off"
          class="btn small"
          type="button"
          :disabled="busy"
          data-test="dns-on"
          @click="set('')"
        >
          {{ t('dnsName.turnOn') }}
        </button>
      </template>
    </div>
    <p
      v-for="o in (dns.others ?? []).filter((x) => x.name !== dns!.name)"
      :key="o.name"
      class="muted small other"
    >
      {{ t('dnsName.other', { name: o.name, ip: o.ip }) }}
    </p>
    <p v-if="error" class="error small" role="alert">{{ error }}</p>
  </div>
</template>

<style scoped>
.dns {
  display: grid;
  gap: 4px;
}
.row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.grow {
  flex: 1;
  min-width: 0;
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
.other {
  padding-left: 23px;
}
.mono {
  font-family: var(--mono);
}
.error {
  color: var(--danger);
}
</style>
