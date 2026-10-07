<script setup lang="ts">
import { Network, Puzzle, Radar } from 'lucide-vue-next'
import { computed } from 'vue'

import DeviceIcon from '@/components/DeviceIcon.vue'
import { deviceTypes } from '@/lib/icons'

/**
 * The logo of an integration: built-in ones have their own icon, plugins the
 * logo from the store catalog (a brand logo, or a device type's icon when the
 * brand has none), anything else a puzzle piece.
 */
const props = withDefaults(defineProps<{ type: string; icon?: string; size?: number }>(), {
  icon: undefined,
  size: 26,
})

const builtin = computed(() => ({ network: Network, nmap: Radar })[props.type])
const device = computed(() =>
  props.icon && deviceTypes.includes(props.icon)
    ? { type: props.icon }
    : { product: props.icon, type: 'app' },
)
</script>

<template>
  <span class="integration-logo" data-test="integration-logo">
    <component :is="builtin" v-if="builtin" :size="size" class="builtin" />
    <DeviceIcon v-else-if="icon" :device="device" :size="size + 4" />
    <Puzzle v-else :size="size" />
  </span>
</template>

<style scoped>
.integration-logo {
  display: inline-grid;
  place-items: center;
}
.builtin {
  color: var(--accent);
}
</style>
