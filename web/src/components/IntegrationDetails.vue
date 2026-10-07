<script setup lang="ts">
import { RefreshCw } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useIntegrationForm } from '@/composables/useIntegrationForm'
import { api, ApiError } from '@/lib/api'
import { formatAgo, formatDateTime } from '@/lib/format'
import type { CollectionStatus, Integration, IntegrationType } from '@/lib/types'

import IntegrationFields from './IntegrationFields.vue'

const props = defineProps<{ integration: Integration; type: IntegrationType }>()
const emit = defineEmits<{ saved: [integration: Integration]; ran: [status: CollectionStatus] }>()
const { t, locale } = useI18n()

const { editing, values, busy, testing, error, result, missing, test, save } = useIntegrationForm(
  props.type,
  props.integration,
)
const saved = ref(false)

async function submit() {
  saved.value = false
  const updated = await save()
  if (updated) {
    saved.value = true
    emit('saved', updated)
  }
}

// "Run now": collect this integration immediately (caches skipped).
const running = ref(false)
const runError = ref('')
const lastRun = ref<CollectionStatus>()

async function runNow() {
  running.value = true
  runError.value = ''
  lastRun.value = undefined
  try {
    const st = await api.runIntegration(props.integration.id)
    lastRun.value = st
    emit('ran', st)
    await loadMethods()
  } catch (e) {
    runError.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    running.value = false
  }
}

// What the last collection found, per discovery method (network scan).
const methods = ref<Record<string, number>>({})
const hosts = ref(0)
onMounted(loadMethods)
async function loadMethods() {
  if (props.integration.type !== 'network') return
  try {
    const topo = await api.topology()
    const counts: Record<string, number> = {}
    let total = 0
    for (const n of topo.topology.nodes) {
      if (n.integration_id !== props.integration.id || !n.device?.hosts) continue
      for (const h of n.device.hosts) {
        total++
        for (const src of h.sources ?? []) counts[src] = (counts[src] ?? 0) + 1
      }
    }
    methods.value = counts
    hosts.value = total
  } catch {
    // statistics are informative only
  }
}
const methodList = computed(() =>
  Object.entries(methods.value)
    .filter(([m]) => m !== 'self')
    .sort((a, b) => b[1] - a[1]),
)
const status = computed(() => lastRun.value ?? props.integration.status)
</script>

<template>
  <div class="details">
    <section class="status">
      <dl>
        <template v-if="status">
          <dt>{{ t('integrations.lastCollection') }}</dt>
          <dd :title="formatDateTime(status.collected_at, locale)">
            {{ formatAgo(status.collected_at, locale) }}
          </dd>
          <dt>{{ t('integrations.duration') }}</dt>
          <dd>{{ (status.duration_ms / 1000).toFixed(1) }} s</dd>
          <dt>{{ t('integrations.devicesFound') }}</dt>
          <dd>{{ integration.type === 'network' ? hosts : status.devices }}</dd>
        </template>
        <template v-else>
          <dt>{{ t('integrations.lastCollection') }}</dt>
          <dd>{{ t('integrations.neverCollected') }}</dd>
        </template>
      </dl>
      <p v-if="status?.error" class="alert error">{{ status.error }}</p>
      <button
        class="btn primary run"
        type="button"
        data-test="run-now"
        :disabled="running || !integration.enabled"
        @click="runNow"
      >
        <RefreshCw :size="15" :class="{ spin: running }" />
        {{ running ? t('integrations.running') : t('integrations.runNow') }}
      </button>
      <p v-if="lastRun?.ok" class="alert ok" role="status">
        {{
          t('integrations.ranOk', {
            n: lastRun.devices,
            s: (lastRun.duration_ms / 1000).toFixed(1),
          })
        }}
      </p>
      <p v-if="runError" class="alert error" role="alert">{{ runError }}</p>
      <div v-if="methodList.length" class="methods">
        <span class="muted">{{ t('integrations.foundBy') }}:</span>
        <span v-for="[m, n] in methodList" :key="m" class="badge" :data-method="m">
          {{ t(`integrations.method.${m}`) }} · {{ n }}
        </span>
      </div>
    </section>

    <form class="settings" @submit.prevent="submit">
      <h3>{{ t('integrations.settings') }}</h3>
      <IntegrationFields
        v-model="values"
        :type="integration.type"
        :fields="type.fields"
        :editing="editing"
        :id-prefix="`d${integration.id}`"
      />
      <p v-if="result" class="alert" :class="result.ok ? 'ok' : 'error'" role="status">
        {{ result.ok ? result.message : result.error }}
      </p>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <p v-if="saved" class="alert ok" role="status">{{ t('integrations.saved') }}</p>
      <div class="actions">
        <button class="btn" type="button" :disabled="testing || missing.length > 0" @click="test">
          {{ testing ? t('integrations.testing') : t('integrations.test') }}
        </button>
        <button class="btn primary" type="submit" :disabled="busy || missing.length > 0">
          {{ t('common.save') }}
        </button>
      </div>
    </form>
  </div>
</template>

<style scoped>
.details {
  display: grid;
  grid-template-columns: minmax(200px, 1fr) minmax(260px, 2fr);
  gap: 24px;
  padding: 16px 18px 18px;
  border-top: 1px solid var(--border);
}
dl {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 6px 14px;
  margin: 0 0 12px;
}
dt {
  color: var(--text-muted);
}
dd {
  margin: 0;
}
.run {
  margin: 4px 0 12px;
}
.spin {
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
.methods {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
  font-size: 12.5px;
}
h3 {
  margin: 0 0 12px;
  font-size: 13px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.alert {
  margin: 0 0 10px;
}
@media (max-width: 760px) {
  .details {
    grid-template-columns: 1fr;
  }
}
</style>
