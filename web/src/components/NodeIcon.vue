<script setup lang="ts">
import {
  AirVent,
  Box,
  Cast,
  Cctv,
  CircleQuestionMark,
  Container,
  EthernetPort,
  Gamepad2,
  HardDrive,
  House,
  Layers,
  Monitor,
  Network,
  Printer,
  Router,
  Server,
  Shield,
  Smartphone,
  Speaker,
  Tablet,
  Tv,
  Users,
  Watch,
  Wifi,
} from 'lucide-vue-next'
import { computed, type Component } from 'vue'

const props = withDefaults(defineProps<{ name: string; size?: number }>(), { size: 18 })

const icons: Record<string, Component> = {
  'air-vent': AirVent,
  box: Box,
  cast: Cast,
  cctv: Cctv,
  'circle-question-mark': CircleQuestionMark,
  container: Container,
  'ethernet-port': EthernetPort,
  'gamepad-2': Gamepad2,
  'hard-drive': HardDrive,
  house: House,
  layers: Layers,
  monitor: Monitor,
  network: Network,
  printer: Printer,
  router: Router,
  server: Server,
  shield: Shield,
  smartphone: Smartphone,
  speaker: Speaker,
  tablet: Tablet,
  tv: Tv,
  users: Users,
  watch: Watch,
  wifi: Wifi,
}

const icon = computed(() => icons[props.name] ?? Box)
</script>

<template>
  <component :is="icon" :size="size" :stroke-width="1.75" />
</template>
