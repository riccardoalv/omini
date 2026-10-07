<script setup lang="ts">
import {
  Box,
  EthernetPort,
  Monitor,
  Network,
  Router,
  Server,
  Shield,
  Smartphone,
  Users,
  Wifi,
} from 'lucide-vue-next'
import { computed, type Component } from 'vue'

const props = withDefaults(defineProps<{ name: string; size?: number }>(), { size: 18 })

const icons: Record<string, Component> = {
  box: Box,
  'ethernet-port': EthernetPort,
  monitor: Monitor,
  network: Network,
  router: Router,
  server: Server,
  shield: Shield,
  smartphone: Smartphone,
  users: Users,
  wifi: Wifi,
}

const icon = computed(() => icons[props.name] ?? Box)
</script>

<template>
  <component :is="icon" :size="size" :stroke-width="1.75" />
</template>
