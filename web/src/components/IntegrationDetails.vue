<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useIntegrationForm } from '@/composables/useIntegrationForm'
import { api } from '@/lib/api'
import { formatAgo, formatDateTime } from '@/lib/format'
import type { Integration, IntegrationType } from '@/lib/types'

import IntegrationFields from './IntegrationFields.vue'

const props = defineProps<{ integration: Integration; type: IntegrationType }>()
const emit = defineEmits<{ saved: [integration: Integration] }>()
const { t, locale } = useI18n()

const { editing, name, values, busy, testing, error, result, missing, test, save } =
  useIntegrationForm(props.type, props.integration)
const saved = ref(false)

async function submit() {
  saved.value = false
  const updated = await save()
  if (updated) {
    saved.value = true
    emit('saved', updated)
  }
}

// What the last collection found, per discovery method (network scan).
const methods = ref<Record<string, number>>({})
const hosts = ref(0)
onMounted(async () => {
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
})
const methodList = computed(() =>
  Object.entries(methods.value)
    .filter(([m]) => m !== 'self')
    .sort((a, b) => b[1] - a[1]),
)
const status = computed(() => props.integration.status)
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
      <div v-if="methodList.length" class="methods">
        <span class="muted">{{ t('integrations.foundBy') }}:</span>
        <span v-for="[m, n] in methodList" :key="m" class="badge" :data-method="m">
          {{ t(`integrations.method.${m}`) }} · {{ n }}
        </span>
      </div>
    </section>

    <form class="settings" @submit.prevent="submit">
      <h3>{{ t('integrations.settings') }}</h3>
      <div class="field">
        <label :for="`d${integration.id}-name`">{{ t('integrations.name') }}</label>
        <input :id="`d${integration.id}-name`" v-model="name" class="input" required />
      </div>
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
