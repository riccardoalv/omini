<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/lib/api'
import { chartScale, chartSeries } from '@/lib/chart'
import { formatRate } from '@/lib/traffic'
import type { TrafficPoint } from '@/lib/types'

/**
 * Traffic of one interface (or a Wi-Fi client's own traffic, iface "") over
 * time: per minute for the last day, per hour (average, with the peak) for
 * longer ranges.
 */
const props = withDefaults(
  defineProps<{
    node: string
    iface: string
    /** The interface is at the far end of the link: its sent bytes are the download. */
    swap?: boolean
    hours?: number
  }>(),
  { swap: false, hours: 24 },
)
const { t, locale } = useI18n()

const RANGES = [1, 24, 168, 720, 8760] as const
const hours = ref<number>(props.hours)
let picked = false // the user (or the first load) chose the range
const points = ref<TrafficPoint[]>([])
const loading = ref(false)
const failed = ref(false)

async function load() {
  loading.value = true
  failed.value = false
  const want = { node: props.node, iface: props.iface, hours: hours.value }
  try {
    const got = await api.history(want.node, want.iface, want.hours)
    if (want.node !== props.node || want.iface !== props.iface || want.hours !== hours.value) return
    // A day asked but only minutes recorded (Omini just started): the last
    // hour shows them readably.
    const first = got[0] && Date.parse(got[0].at)
    if (!picked && want.hours === 24 && first && Date.now() - first < 3600_000) {
      picked = true
      hours.value = 1
      return
    }
    points.value = got
  } catch {
    failed.value = true
    points.value = []
  } finally {
    loading.value = false
  }
}
watch(() => [props.node, props.iface, hours.value], load, { immediate: true })

const W = 600
const H = 180
const PAD = { left: 58, right: 8, top: 10, bottom: 22 }

const series = computed(() => chartSeries(points.value, props.swap))
const scale = computed(() =>
  chartScale(series.value, Date.now() - hours.value * 3600_000, Date.now(), {
    width: W,
    height: H,
    ...PAD,
  }),
)

const timeFormat = computed(
  () =>
    new Intl.DateTimeFormat(
      locale.value,
      hours.value <= 24
        ? { hour: '2-digit', minute: '2-digit' }
        : hours.value <= 720
          ? { day: '2-digit', month: '2-digit' }
          : { month: 'short' },
    ),
)
const fullTime = computed(
  () => new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }),
)

const hover = ref<number | null>(null)
const svg = ref<SVGSVGElement>()
function onMove(ev: PointerEvent) {
  const s = series.value
  if (!svg.value || !s.length) return
  const r = svg.value.getBoundingClientRect()
  const x = ((ev.clientX - r.left) / r.width) * W
  let best = 0
  for (let i = 1; i < s.length; i++)
    if (Math.abs(scale.value.x(s[i]!.at) - x) < Math.abs(scale.value.x(s[best]!.at) - x)) best = i
  hover.value = best
}
const hovered = computed(() => (hover.value === null ? undefined : series.value[hover.value]))

const stats = computed(() => {
  const s = series.value
  if (!s.length) return undefined
  const avg = (k: 'down' | 'up') => s.reduce((sum, p) => sum + p[k], 0) / s.length
  const peak = (k: 'downMax' | 'upMax') => Math.max(...s.map((p) => p[k]))
  return { down: avg('down'), up: avg('up'), downPeak: peak('downMax'), upPeak: peak('upMax') }
})
</script>

<template>
  <div class="traffic-chart" data-test="traffic-chart">
    <div class="bar">
      <div class="legend">
        <span class="down"><i />{{ t('traffic.down') }}</span>
        <span class="up"><i />{{ t('traffic.up') }}</span>
      </div>
      <div class="ranges" role="group" :aria-label="t('traffic.title')">
        <button
          v-for="r in RANGES"
          :key="r"
          type="button"
          class="range"
          :class="{ on: hours === r }"
          :data-test="`range-${r}`"
          @click="((picked = true), (hours = r))"
        >
          {{ t(`traffic.ranges.${r}`) }}
        </button>
      </div>
    </div>
    <div class="plot">
      <svg
        ref="svg"
        :viewBox="`0 0 ${W} ${H}`"
        preserveAspectRatio="none"
        role="img"
        :aria-label="t('traffic.title')"
        @pointermove="onMove"
        @pointerleave="hover = null"
      >
        <g class="grid">
          <template v-for="tick in scale.yTicks" :key="tick.value">
            <line :x1="PAD.left" :x2="W - PAD.right" :y1="tick.y" :y2="tick.y" />
          </template>
        </g>
        <path v-if="scale.downArea" class="area down" :d="scale.downArea" />
        <path v-if="scale.upArea" class="area up" :d="scale.upArea" />
        <path v-if="scale.downLine" class="line down" :d="scale.downLine" />
        <path v-if="scale.upLine" class="line up" :d="scale.upLine" />
        <line
          v-if="hovered"
          class="cursor"
          :x1="scale.x(hovered.at)"
          :x2="scale.x(hovered.at)"
          :y1="PAD.top"
          :y2="H - PAD.bottom"
        />
      </svg>
      <div class="y-labels">
        <span
          v-for="tick in scale.yTicks"
          :key="tick.value"
          :style="{ top: `${(tick.y / H) * 100}%` }"
        >
          {{ formatRate(tick.value) }}
        </span>
      </div>
      <div class="x-labels">
        <span
          v-for="tick in scale.xTicks"
          :key="tick"
          :style="{ left: `${(scale.x(tick) / W) * 100}%` }"
        >
          {{ timeFormat.format(tick) }}
        </span>
      </div>
      <div v-if="hovered" class="tip" data-test="chart-tip">
        <strong>{{ fullTime.format(hovered.at) }}</strong>
        <span class="down">↓ {{ formatRate(hovered.down) }}</span>
        <span class="up">↑ {{ formatRate(hovered.up) }}</span>
        <span v-if="hovered.downMax > hovered.down" class="muted">
          {{ t('traffic.peak') }} ↓ {{ formatRate(hovered.downMax) }}
        </span>
      </div>
      <p v-if="!loading && !series.length" class="empty muted" data-test="chart-empty">
        {{ failed ? t('common.error') : t('traffic.noData') }}
      </p>
    </div>
    <dl v-if="stats" class="stats" data-test="chart-stats">
      <div>
        <dt>{{ t('traffic.average') }}</dt>
        <dd>
          <span class="down">↓ {{ formatRate(stats.down) }}</span>
          <span class="up">↑ {{ formatRate(stats.up) }}</span>
        </dd>
      </div>
      <div>
        <dt>{{ t('traffic.peak') }}</dt>
        <dd>
          <span class="down">↓ {{ formatRate(stats.downPeak) }}</span>
          <span class="up">↑ {{ formatRate(stats.upPeak) }}</span>
        </dd>
      </div>
    </dl>
  </div>
</template>

<style scoped>
.traffic-chart {
  --down: #38bdf8;
  --up: #a78bfa;
  display: grid;
  gap: 8px;
}
.bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
}
.legend {
  display: flex;
  gap: 12px;
  font-size: 12px;
  color: var(--text-muted);
}
.legend i {
  display: inline-block;
  width: 10px;
  height: 3px;
  margin-right: 5px;
  vertical-align: middle;
  border-radius: 2px;
  background: currentColor;
}
.legend .down i,
.down {
  color: var(--down);
}
.legend .up i,
.up {
  color: var(--up);
}
.ranges {
  display: flex;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  overflow: hidden;
}
.range {
  padding: 3px 8px;
  border: 0;
  background: transparent;
  color: var(--text-muted);
  font-size: 11.5px;
  cursor: pointer;
}
.range + .range {
  border-left: 1px solid var(--border);
}
.range.on {
  background: var(--accent-soft);
  color: var(--accent);
}
.plot {
  position: relative;
  height: 180px;
}
svg {
  width: 100%;
  height: 100%;
  overflow: visible;
  touch-action: none;
}
.grid line {
  stroke: var(--border);
  stroke-width: 1;
  vector-effect: non-scaling-stroke;
}
.line {
  fill: none;
  stroke-width: 1.5;
  vector-effect: non-scaling-stroke;
}
.line.down {
  stroke: var(--down);
}
.line.up {
  stroke: var(--up);
}
.area.down {
  fill: color-mix(in srgb, var(--down) 18%, transparent);
}
.area.up {
  fill: color-mix(in srgb, var(--up) 14%, transparent);
}
.cursor {
  stroke: var(--text-muted);
  stroke-dasharray: 3 3;
  vector-effect: non-scaling-stroke;
}
.y-labels span,
.x-labels span {
  position: absolute;
  color: var(--text-muted);
  font-family: var(--mono);
  font-size: 10px;
  white-space: nowrap;
  pointer-events: none;
}
.y-labels span {
  left: 0;
  transform: translateY(-50%);
}
.x-labels span {
  bottom: 0;
  transform: translateX(-50%);
}
.tip {
  position: absolute;
  top: 6px;
  right: 10px;
  display: grid;
  gap: 1px;
  padding: 6px 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-elevated);
  font-size: 11.5px;
  pointer-events: none;
}
.empty {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  margin: 0;
  font-size: 12.5px;
}
.stats {
  display: flex;
  gap: 24px;
  margin: 0;
  font-size: 12px;
}
.stats dt {
  color: var(--text-muted);
}
.stats dd {
  display: flex;
  gap: 10px;
  margin: 0;
  font-family: var(--mono);
}
</style>
