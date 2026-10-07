<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { iconChoice, iconURL, logoColor, logos, type Classified } from '@/lib/icons'

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
const badgeSize = computed(() => Math.round(props.size * 0.8))
// Catalog icons that fail to load (unknown name, offline) are simply not shown.
const remoteFailed = ref(false)
watch(
  () => choice.value.remote,
  () => (remoteFailed.value = false),
)
</script>

<template>
  <span class="device-icon" :title="title" :style="{ width: `${size}px`, height: `${size}px` }">
    <!-- Catalog icon alone (apps, products without a Simple Icons logo) -->
    <img
      v-if="choice.remote && !choice.badge && !remoteFailed"
      class="logo"
      :src="iconURL(choice.remote)"
      :width="size"
      :height="size"
      :alt="title"
      @error="remoteFailed = true"
    />
    <NodeIcon
      v-else-if="choice.remote && !choice.badge"
      :name="choice.type ?? 'app-window'"
      :size="size"
    />
    <!-- Product logo alone -->
    <svg
      v-else-if="logo && !choice.badge"
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
        v-if="choice.remote && !remoteFailed"
        class="badge"
        :style="{ width: `${badgeSize}px`, height: `${badgeSize}px` }"
      >
        <img :src="iconURL(choice.remote)" alt="" @error="remoteFailed = true" />
      </span>
      <span
        v-else-if="logo"
        class="badge"
        :style="{ width: `${badgeSize}px`, height: `${badgeSize}px` }"
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
/* Catalog icons are not all square: keep their proportions. */
img.logo,
.badge img {
  object-fit: contain;
}
.badge {
  position: absolute;
  right: -45%;
  bottom: -38%;
  display: grid;
  place-items: center;
  padding: 2px;
  border-radius: 50%;
  background: var(--bg-elevated);
  box-shadow: 0 0 0 1.5px var(--border);
}
.badge img,
.badge svg {
  width: 100%;
  height: 100%;
}
</style>
