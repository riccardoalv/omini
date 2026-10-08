<script setup lang="ts">
import {
  ArrowDownFromLine,
  ArrowRightFromLine,
  Braces,
  Image,
  Moon,
  PenTool,
  Workflow,
  Sun,
} from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import ModalDialog from '@/components/ModalDialog.vue'
import type { ExportFormat } from '@/lib/export'

export interface ExportOptions {
  format: ExportFormat
  theme: 'dark' | 'light'
  direction: 'RIGHT' | 'DOWN'
}

/** Options for exporting the map: format, theme and orientation of the image. */
const props = defineProps<{
  /** Starting values: the screen's theme and orientation. */
  theme: 'dark' | 'light'
  direction: 'RIGHT' | 'DOWN'
  busy?: boolean
  error?: string
}>()
const emit = defineEmits<{ close: []; export: [options: ExportOptions] }>()
const { t } = useI18n()

const format = ref<ExportFormat>('png')
const theme = ref(props.theme)
const direction = ref(props.direction)
const image = computed(() => format.value === 'png' || format.value === 'svg')
/** draw.io files are laid out too (orientation), but have their own colors. */
const laidOut = computed(() => format.value !== 'json')

const formats = [
  { value: 'png', icon: Image },
  { value: 'svg', icon: PenTool },
  { value: 'drawio', icon: Workflow },
  { value: 'json', icon: Braces },
] as const
const themes = [
  { value: 'dark', icon: Moon },
  { value: 'light', icon: Sun },
] as const
const directions = [
  { value: 'RIGHT', icon: ArrowRightFromLine },
  { value: 'DOWN', icon: ArrowDownFromLine },
] as const

function submit() {
  emit('export', { format: format.value, theme: theme.value, direction: direction.value })
}
</script>

<template>
  <ModalDialog :title="t('map.exportDialog.title')" @close="emit('close')">
    <form id="export-form" class="options" @submit.prevent="submit">
      <fieldset>
        <legend>{{ t('map.exportDialog.format') }}</legend>
        <div class="choices">
          <label
            v-for="f in formats"
            :key="f.value"
            class="choice"
            :class="{ on: format === f.value }"
          >
            <input
              v-model="format"
              type="radio"
              name="format"
              :value="f.value"
              :data-test="`format-${f.value}`"
            />
            <component :is="f.icon" :size="18" />
            <span>{{ t(`map.exportAs.${f.value}`) }}</span>
          </label>
        </div>
      </fieldset>

      <fieldset :disabled="!image">
        <legend>{{ t('map.exportDialog.theme') }}</legend>
        <div class="choices">
          <label
            v-for="th in themes"
            :key="th.value"
            class="choice"
            :class="{ on: theme === th.value }"
          >
            <input
              v-model="theme"
              type="radio"
              name="theme"
              :value="th.value"
              :data-test="`theme-${th.value}`"
            />
            <component :is="th.icon" :size="18" />
            <span>{{ t(`map.exportDialog.${th.value}`) }}</span>
          </label>
        </div>
      </fieldset>

      <fieldset :disabled="!laidOut">
        <legend>{{ t('map.exportDialog.orientation') }}</legend>
        <div class="choices">
          <label
            v-for="d in directions"
            :key="d.value"
            class="choice"
            :class="{ on: direction === d.value }"
          >
            <input
              v-model="direction"
              type="radio"
              name="direction"
              :value="d.value"
              :data-test="`direction-${d.value}`"
            />
            <component :is="d.icon" :size="18" />
            <span>{{ d.value === 'RIGHT' ? t('map.leftToRight') : t('map.topDown') }}</span>
          </label>
        </div>
      </fieldset>
      <p v-if="image" class="hint">{{ t('map.exportDialog.hint') }}</p>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    </form>
    <template #footer>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button
        class="btn primary"
        type="submit"
        form="export-form"
        data-test="export-confirm"
        :disabled="busy"
      >
        {{ busy ? t('map.exportDialog.exporting') : t('map.export') }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.options {
  display: grid;
  gap: 16px;
}
fieldset {
  margin: 0;
  padding: 0;
  border: 0;
}
fieldset:disabled {
  opacity: 0.45;
}
legend {
  margin-bottom: 8px;
  color: var(--text-muted);
  font-size: 12.5px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.choices {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
  gap: 8px;
}
.choice {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  cursor: pointer;
  font-size: 13.5px;
}
.choice.on {
  border-color: var(--accent);
  background: var(--accent-soft);
  color: var(--accent);
}
.choice input {
  position: absolute;
  opacity: 0;
  pointer-events: none;
}
.choice:has(input:focus-visible) {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.hint {
  margin: 0;
  color: var(--text-muted);
  font-size: 12.5px;
}
</style>
