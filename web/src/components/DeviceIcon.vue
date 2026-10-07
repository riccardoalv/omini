<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { iconChoice, logoColor, logos, type Classified } from '@/lib/icons'

import NodeIcon from './NodeIcon.vue'

const props = withDefaults(defineProps<{ device: Classified; size?: number }>(), { size: 18 })
const { t, te } = useI18n()

// Follow the effective theme to keep brand colors readable.
const dark = ref(true)
let mq: MediaQueryList | undefined
function updateTheme() {
  const forced = document.documentElement.dataset.theme
  dark.value = forced ? forced === 'dark' : !(mq?.matches ?? false)
}
let observer: MutationObserver | undefined
onMounted(() => {
  mq = window.matchMedia?.('(prefers-color-scheme: light)')
  mq?.addEventListener?.('change', updateTheme)
  observer = new MutationObserver(updateTheme)
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
  updateTheme()
})
onBeforeUnmount(() => {
  mq?.removeEventListener?.('change', updateTheme)
  observer?.disconnect()
})

const choice = computed(() => iconChoice(props.device))
const logo = computed(() => (choice.value.logo ? logos[choice.value.logo] : undefined))
const color = computed(() => (logo.value ? logoColor(logo.value.hex, dark.value) : undefined))
const typeLabel = computed(() => {
  const key = `types.${props.device.type ?? 'unknown'}`
  return te(key) ? t(key) : ''
})
const title = computed(() => [typeLabel.value, logo.value?.title].filter(Boolean).join(' · '))
</script>

<template>
  <span class="device-icon" :title="title" :style="{ width: `${size}px`, height: `${size}px` }">
    <!-- Product logo alone -->
    <svg
      v-if="logo && !choice.badge"
      class="logo"
      viewBox="0 0 24 24"
      :width="size"
      :height="size"
      role="img"
      :aria-label="title"
      :style="{ color }"
    >
      <path :d="logo.path" fill="currentColor" />
    </svg>
    <template v-else>
      <NodeIcon :name="choice.type ?? 'circle-question-mark'" :size="size" />
      <span
        v-if="logo"
        class="badge"
        :style="{ width: `${size * 0.7}px`, height: `${size * 0.7}px` }"
      >
        <svg viewBox="0 0 24 24" role="img" :aria-label="logo.title" :style="{ color }">
          <path :d="logo.path" fill="currentColor" />
        </svg>
      </span>
    </template>
  </span>
</template>

<style scoped>
.device-icon {
  position: relative;
  display: inline-grid;
  place-items: center;
  flex: none;
}
.logo {
  display: block;
}
.badge {
  position: absolute;
  right: -35%;
  bottom: -30%;
  display: grid;
  place-items: center;
  padding: 2px;
  border-radius: 50%;
  background: var(--bg-elevated);
  box-shadow: 0 0 0 1.5px var(--border);
}
.badge svg {
  width: 100%;
  height: 100%;
}
</style>
