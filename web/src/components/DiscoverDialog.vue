<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import type { DiscoveredHost } from '@/lib/types'

import ModalDialog from './ModalDialog.vue'
import SecretInput from './SecretInput.vue'

const emit = defineEmits<{ close: []; add: [host: DiscoveredHost, community: string] }>()
const { t } = useI18n()

const cidr = ref('192.168.1.0/24')
const community = ref('public')
const scanning = ref(false)
const results = ref<DiscoveredHost[]>()
const error = ref('')

async function scan() {
  scanning.value = true
  error.value = ''
  results.value = undefined
  try {
    results.value = await api.scan(cidr.value, community.value)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    scanning.value = false
  }
}
</script>

<template>
  <ModalDialog :title="t('discover.title')" wide @close="emit('close')">
    <p class="muted hint">{{ t('discover.hint') }}</p>
    <form class="row" @submit.prevent="scan">
      <div class="field grow">
        <label for="d-cidr">{{ t('discover.subnet') }}</label>
        <input id="d-cidr" v-model="cidr" class="input mono" required />
      </div>
      <div class="field grow">
        <label for="d-community">{{ t('discover.community') }}</label>
        <SecretInput id="d-community" v-model="community" required />
      </div>
      <button class="btn primary scan" type="submit" :disabled="scanning">
        {{ t('discover.scan') }}
      </button>
    </form>

    <p v-if="scanning" class="muted">{{ t('discover.scanning') }}</p>
    <p v-if="error" class="alert error">{{ error }}</p>
    <template v-if="results">
      <p v-if="!results.length" class="muted">{{ t('discover.none') }}</p>
      <template v-else>
        <p class="muted">{{ t('discover.found', results.length) }}</p>
        <table class="table">
          <tbody>
            <tr v-for="h in results" :key="h.ip">
              <td class="mono">{{ h.ip }}</td>
              <td>
                <strong>{{ h.name }}</strong>
                <div class="muted small">
                  {{ [h.vendor, h.description].filter(Boolean).join(' · ') }}
                </div>
              </td>
              <td class="end">
                <span v-if="h.integrated" class="badge">{{ t('discover.integrated') }}</span>
                <button v-else class="btn small" @click="emit('add', h, community)">
                  {{ t('common.add') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </template>
    </template>
  </ModalDialog>
</template>

<style scoped>
.hint {
  margin: 0 0 14px;
}
.row {
  display: flex;
  gap: 10px;
  align-items: flex-end;
  flex-wrap: wrap;
}
.grow {
  flex: 1;
  min-width: 160px;
}
.scan {
  margin-bottom: 14px;
  height: 36px;
}
.small {
  font-size: 12.5px;
}
.end {
  text-align: right;
}
</style>
