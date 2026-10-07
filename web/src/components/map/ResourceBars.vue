<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ cpu?: number; memory?: number }>()
const { t } = useI18n()

const level = (pct: number) => (pct >= 85 ? 'high' : pct >= 60 ? 'medium' : 'low')

const bars = computed(() =>
  [
    { key: 'cpu', label: t('panel.cpu'), value: props.cpu },
    { key: 'memory', label: t('panel.memory'), value: props.memory },
  ]
    .filter((b): b is { key: string; label: string; value: number } => b.value !== undefined)
    .map((b) => {
      const pct = Math.min(100, Math.max(0, b.value))
      return { ...b, pct, level: level(pct) }
    }),
)
</script>

<template>
  <div v-if="bars.length" class="bars">
    <div v-for="b in bars" :key="b.key" class="bar" :data-test="`bar-${b.key}`">
      <div class="head">
        <span>{{ b.label }}</span>
        <strong class="value" :class="b.level">{{ Math.round(b.pct) }}%</strong>
      </div>
      <div
        class="track"
        role="meter"
        :aria-label="b.label"
        aria-valuemin="0"
        aria-valuemax="100"
        :aria-valuenow="Math.round(b.pct)"
      >
        <div class="fill" :class="b.level" :style="{ width: `${b.pct}%` }" />
        <span v-for="mark in [25, 50, 75]" :key="mark" class="mark" :style="{ left: `${mark}%` }" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.bars {
  display: grid;
  gap: 12px;
  margin: 4px 0 16px;
}
.head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 6px;
  font-size: 13px;
  color: var(--text-muted);
}
.value {
  font-size: 15px;
  font-variant-numeric: tabular-nums;
  color: var(--text);
}
.value.medium {
  color: var(--warn);
}
.value.high {
  color: var(--danger);
}
.track {
  position: relative;
  height: 10px;
  border-radius: 999px;
  background: var(--bg-sunken);
  box-shadow: inset 0 0 0 1px var(--border);
  overflow: hidden;
}
.fill {
  --c: var(--ok);
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, color-mix(in srgb, var(--c) 65%, transparent), var(--c));
  box-shadow: 0 0 10px color-mix(in srgb, var(--c) 45%, transparent);
  transition: width 0.6s ease;
}
.fill.medium {
  --c: var(--warn);
}
.fill.high {
  --c: var(--danger);
}
.mark {
  position: absolute;
  top: 2px;
  bottom: 2px;
  width: 1px;
  background: color-mix(in srgb, var(--border-strong) 60%, transparent);
}
</style>
