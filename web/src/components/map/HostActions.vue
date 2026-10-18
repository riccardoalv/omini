<script setup lang="ts">
import { Pause, Play, Power } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import { displayName } from '@/lib/names'
import { pauseOf } from '@/lib/paused'
import type { HostActions, TopoNode } from '@/lib/types'

/**
 * The device's own buttons: wake it (Wake-on-LAN, from Omini or its router)
 * and pause or resume its internet on its router.
 */
const props = defineProps<{ node: TopoNode; nodes: TopoNode[] }>()
const emit = defineEmits<{ applied: []; error: [message: string]; done: [message: string] }>()
const { t } = useI18n()

const actions = ref<HostActions>()
watch(
  () => props.node.id,
  async (id) => {
    actions.value = undefined
    if (!props.node.mac || props.node.kind === 'device') return
    try {
      const a = await api.hostActions(id)
      if (props.node.id === id) actions.value = a
    } catch {
      // no buttons
    }
  },
  { immediate: true },
)

const paused = computed(() => pauseOf(props.node, props.nodes))
const busy = ref(false)

async function run(fn: () => Promise<unknown>, done: string) {
  busy.value = true
  try {
    await fn()
    emit('done', done)
    emit('applied')
  } catch (e) {
    emit('error', e instanceof ApiError ? e.message : t('common.error'))
  } finally {
    busy.value = false
  }
}
const wake = () => run(() => api.wake(props.node.id), t('host.woken'))
// Pausing is an ordinary change: one short confirmation.
const asking = ref(false)
watch(
  () => props.node.id,
  () => (asking.value = false),
)
const pause = async () => {
  await run(() => api.pause(props.node.id), t('host.pausedNow'))
  asking.value = false
}
const resume = () => run(() => api.resume(props.node.id), t('host.resumed'))
</script>

<template>
  <button
    v-if="actions?.wake?.via && !node.online"
    class="btn small"
    data-test="wake"
    :disabled="busy"
    :title="t(`host.wakeHint.${actions.wake.via}`)"
    @click="wake"
  >
    <Power :size="14" />{{ t('host.wake') }}
  </button>
  <button
    v-if="paused"
    class="btn small warn"
    data-test="resume"
    :disabled="busy"
    :title="paused.rule ? t('host.resumeRule', { rule: paused.rule }) : undefined"
    @click="resume"
  >
    <Play :size="14" />{{ t('host.resume') }}
  </button>
  <span v-else-if="actions?.can_pause && asking" class="confirm" data-test="pause-confirm">
    <span>{{ t('host.confirmPause', { name: displayName(node, t) }) }}</span>
    <button class="btn small ghost" type="button" :disabled="busy" @click="asking = false">
      {{ t('changes.cancel') }}
    </button>
    <button
      class="btn small primary"
      type="button"
      data-test="confirm-pause"
      :disabled="busy"
      @click="pause"
    >
      {{ busy ? t('changes.applying') : t('host.pause') }}
    </button>
  </span>
  <button
    v-else-if="actions?.can_pause"
    class="btn small"
    data-test="pause"
    :disabled="busy"
    :title="t('host.pauseHint')"
    @click="asking = true"
  >
    <Pause :size="14" />{{ t('host.pause') }}
  </button>
</template>

<style scoped>
.confirm {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-sm);
  background: var(--surface);
  font-size: 12.5px;
}
</style>
