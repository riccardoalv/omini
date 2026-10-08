<script setup lang="ts">
import 'vanilla-colorful/hex-color-picker.js'

import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

/**
 * A color picker: saturation square and hue bar (vanilla-colorful, MIT),
 * the color as hex and as R, G, B. `update:modelValue` follows every move;
 * `apply` confirms it.
 */
const props = defineProps<{ modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [color: string]; apply: [color: string] }>()
const { t } = useI18n()

const color = ref(props.modelValue.toLowerCase())
watch(
  () => props.modelValue,
  (v) => {
    if (v.toLowerCase() !== color.value) color.value = v.toLowerCase()
  },
)

function set(v: string) {
  color.value = v.toLowerCase()
  emit('update:modelValue', color.value)
}

const rgb = computed(() => [1, 3, 5].map((i) => parseInt(color.value.slice(i, i + 2), 16) || 0))

const hexDraft = ref(color.value.slice(1))
watch(color, (c) => (hexDraft.value = c.slice(1)))
function onHex(e: Event) {
  const v = (e.target as HTMLInputElement).value.trim().replace(/^#/, '').toLowerCase()
  hexDraft.value = v
  if (/^[0-9a-f]{6}$/.test(v)) set(`#${v}`)
  else if (/^[0-9a-f]{3}$/.test(v)) set(`#${[...v].map((c) => c + c).join('')}`)
}

function onChannel(i: number, e: Event) {
  const n = Math.max(
    0,
    Math.min(255, Math.round(Number((e.target as HTMLInputElement).value) || 0)),
  )
  const next = [...rgb.value]
  next[i] = n
  set(`#${next.map((v) => v.toString(16).padStart(2, '0')).join('')}`)
}
</script>

<template>
  <div class="color-picker">
    <hex-color-picker
      :color="color"
      data-test="picker"
      @color-changed="(e: CustomEvent<{ value: string }>) => set(e.detail.value)"
    />
    <div class="fields">
      <span class="preview" :style="{ background: color }" />
      <label class="hex">
        <span>#</span>
        <input
          :value="hexDraft"
          maxlength="7"
          spellcheck="false"
          data-test="hex"
          :aria-label="t('color.hex')"
          @input="onHex"
        />
      </label>
    </div>
    <div class="fields rgb">
      <label v-for="(c, i) in ['R', 'G', 'B']" :key="c">
        <span>{{ c }}</span>
        <input
          type="number"
          min="0"
          max="255"
          :value="rgb[i]"
          :data-test="`rgb-${c.toLowerCase()}`"
          @change="onChannel(i, $event)"
        />
      </label>
    </div>
    <button class="btn small primary apply" data-test="apply" @click="emit('apply', color)">
      {{ t('color.apply') }}
    </button>
  </div>
</template>

<style scoped>
.color-picker {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 200px;
}
hex-color-picker {
  width: 100%;
  height: 150px;
}
hex-color-picker::part(saturation) {
  border-radius: var(--radius-sm) var(--radius-sm) 0 0;
  bottom: 18px;
}
hex-color-picker::part(hue) {
  height: 12px;
  border-radius: 6px;
}
hex-color-picker::part(saturation-pointer),
hex-color-picker::part(hue-pointer) {
  width: 16px;
  height: 16px;
  border-width: 2px;
}
.fields {
  display: flex;
  align-items: center;
  gap: 6px;
}
.preview {
  flex: none;
  width: 26px;
  height: 26px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}
label {
  display: flex;
  flex: 1;
  align-items: center;
  gap: 4px;
  min-width: 0;
  padding: 0 6px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg);
  color: var(--text-muted);
  font-size: 12px;
}
label:focus-within {
  border-color: var(--accent);
}
input {
  width: 100%;
  min-width: 0;
  padding: 5px 0;
  border: 0;
  outline: none;
  background: transparent;
  color: var(--text);
  font: inherit;
  font-family: var(--font-mono, ui-monospace, monospace);
}
input[type='number'] {
  appearance: textfield;
  -moz-appearance: textfield;
}
input[type='number']::-webkit-inner-spin-button,
input[type='number']::-webkit-outer-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.apply {
  justify-content: center;
}
</style>
