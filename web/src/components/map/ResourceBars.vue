<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { formatBytes } from '@/lib/format'
import type { Storage } from '@/lib/types'

const props = defineProps<{
  cpu?: number
  memory?: number
  /** Logical CPUs (vCPUs of a VM), shown next to the CPU bar. */
  cpuCount?: number
  /** Memory in use and installed, shown next to the memory bar. */
  memUsed?: number
  memTotal?: number
  swap?: number
  /** File systems: one bar each, with used of total. */
  storage?: Storage[]
}>()
const { t } = useI18n()

// Disks fill up on purpose (caches, logs): they warn later than CPU and memory.
const level = (pct: number, warn = 60, high = 85) =>
  pct >= high ? 'high' : pct >= warn ? 'medium' : 'low'

interface Bar {
  key: string
  label: string
  pct: number
  level: string
  detail?: string
}

const bars = computed<Bar[]>(() => {
  const out: Bar[] = []
  const add = (key: string, label: string, value?: number, detail?: string, disk = false) => {
    if (value === undefined) return
    const pct = Math.min(100, Math.max(0, value))
    out.push({ key, label, pct, level: disk ? level(pct, 80, 90) : level(pct), detail })
  }
  add(
    'cpu',
    t('panel.cpu'),
    props.cpu,
    props.cpuCount ? t('panel.cpus', { n: props.cpuCount }, props.cpuCount) : undefined,
  )
  // Memory: the percentage, or worked out from used and total when only they are known.
  const memory =
    props.memory ??
    (props.memTotal && props.memUsed !== undefined
      ? (props.memUsed / props.memTotal) * 100
      : undefined)
  add(
    'memory',
    t('panel.memory'),
    memory,
    props.memTotal
      ? props.memUsed !== undefined
        ? t('panel.usedOf', {
            used: formatBytes(props.memUsed),
            total: formatBytes(props.memTotal),
          })
        : formatBytes(props.memTotal)
      : undefined,
  )
  add('swap', t('panel.swap'), props.swap)
  for (const s of props.storage ?? []) {
    if (!s.total_bytes) continue
    const used = s.used_bytes ?? 0
    add(
      `disk:${s.mount}`,
      t('panel.disk', { mount: s.mount }),
      (used / s.total_bytes) * 100,
      t('panel.usedOf', { used: formatBytes(used), total: formatBytes(s.total_bytes) }),
      true,
    )
  }
  return out
})
</script>

<template>
  <div v-if="bars.length" class="bars">
    <div v-for="b in bars" :key="b.key" class="bar" :data-test="`bar-${b.key}`">
      <div class="head">
        <span class="label">{{ b.label }}</span>
        <span class="right">
          <span v-if="b.detail" class="detail">{{ b.detail }}</span>
          <strong class="value" :class="b.level">{{ Math.round(b.pct) }}%</strong>
        </span>
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
.label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.right {
  display: inline-flex;
  align-items: baseline;
  gap: 8px;
  flex: none;
}
.detail {
  font-size: 12px;
  font-variant-numeric: tabular-nums;
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
