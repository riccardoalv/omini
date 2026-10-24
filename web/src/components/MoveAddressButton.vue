<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { moved, refreshAlerts } from '@/lib/alerts'
import { api, ApiError } from '@/lib/api'
import type { Alert } from '@/lib/types'

/**
 * "Use 192.168.1.110": an offline device whose integration cannot reach it
 * answers at another address now (DHCP gave it another one). One short
 * confirmation, then the integration is pointed there and collected again.
 */
const props = defineProps<{ alert: Alert }>()
const emit = defineEmits<{ moved: [] }>()
const { t } = useI18n()

const to = computed(() => moved(props.alert))
const busy = ref(false)
const result = ref<{ ok: boolean; text: string }>()

async function use() {
  const m = to.value
  if (!m) return
  const name = String(props.alert.params.node ?? props.alert.params.integration ?? '')
  if (!window.confirm(t('moved.confirm', { name, from: m.from, to: m.to }))) return
  busy.value = true
  result.value = undefined
  try {
    const out = await api.moveIntegration(m.integration, m.to)
    result.value =
      !out.status || out.status.ok
        ? { ok: true, text: t('moved.done', { to: m.to }) }
        : { ok: false, text: t('moved.stillFailing', { error: out.status.error ?? '' }) }
    emit('moved')
    void refreshAlerts()
  } catch (e) {
    result.value = { ok: false, text: e instanceof ApiError ? e.message : t('common.error') }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <span v-if="to || result" class="move" @click.stop>
    <button
      v-if="to && !result?.ok"
      class="btn small primary"
      type="button"
      :disabled="busy"
      data-test="move-address"
      @click="use"
    >
      {{ t('moved.use', { to: to.to }) }}
    </button>
    <small v-if="result" :class="result.ok ? 'ok' : 'error-text'" data-test="move-result">{{
      result.text
    }}</small>
  </span>
</template>

<style scoped>
.move {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.ok {
  color: var(--ok);
}
.error-text {
  color: var(--danger);
}
</style>
