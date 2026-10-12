<script setup lang="ts">
import { Pause, Play, Power } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import { pausedOn } from '@/lib/paused'
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

const paused = computed(() => pausedOn(props.node, props.nodes))
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
const pause = () => run(() => api.pause(props.node.id), t('host.pausedNow'))
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
  <button v-if="paused" class="btn small warn" data-test="resume" :disabled="busy" @click="resume">
    <Play :size="14" />{{ t('host.resume') }}
  </button>
  <button
    v-else-if="actions?.can_pause"
    class="btn small"
    data-test="pause"
    :disabled="busy"
    :title="t('host.pauseHint')"
    @click="pause"
  >
    <Pause :size="14" />{{ t('host.pause') }}
  </button>
</template>
