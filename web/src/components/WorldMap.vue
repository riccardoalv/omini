<script setup lang="ts">
import { geoCentroid, geoNaturalEarth1, geoPath } from 'd3-geo'
import type { Feature, FeatureCollection, Geometry } from 'geojson'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { feature } from 'topojson-client'
import type { GeometryCollection, Topology } from 'topojson-specification'

import world from '@/assets/world-110m.json'
import { formatBytes } from '@/lib/format'

/**
 * Where the network's internet traffic goes, on a world map: each country
 * colored by its traffic, and lines from home to the busiest ones. Clicking a
 * country picks it (again: all).
 */
const props = defineProps<{
  countries: { country: string; rx: number; tx: number }[]
  home?: string
  selected?: string
}>()
const emit = defineEmits<{ select: [country: string | undefined] }>()
const { t, locale } = useI18n()

const W = 960
const H = 470
const topo = world as unknown as Topology<{ countries: GeometryCollection }>
const shapes = (feature(topo, topo.objects.countries) as FeatureCollection<Geometry>).features
const projection = geoNaturalEarth1().fitExtent(
  [
    [4, 4],
    [W - 4, H - 4],
  ],
  { type: 'FeatureCollection', features: shapes },
)
const path = geoPath(projection)
const byId = new Map(shapes.map((f) => [String(f.id), f]))

const traffic = computed(() => new Map(props.countries.map((c) => [c.country, c.rx + c.tx])))
const max = computed(() => Math.max(1, ...traffic.value.values()))
/** A country's shade: by the logarithm of its traffic, so small ones still show. */
function shade(id: string) {
  const v = traffic.value.get(id)
  if (!v) return undefined
  return 0.18 + 0.72 * (Math.log1p(v) / Math.log1p(max.value))
}
const countryShapes = computed(() =>
  shapes.map((f) => ({ id: String(f.id), d: path(f) ?? '', shade: shade(String(f.id)) })),
)

const centroid = (f: Feature<Geometry>) => geoCentroid(f)
/** Lines from home to the ten busiest countries (home itself is a dot). */
const arcs = computed(() => {
  const home = props.home ? byId.get(props.home) : undefined
  if (!home) return []
  const from = centroid(home)
  return props.countries
    .filter((c) => c.country !== props.home && byId.has(c.country))
    .slice(0, 10)
    .map((c) => {
      const to = centroid(byId.get(c.country)!)
      const share = (c.rx + c.tx) / max.value
      return {
        id: c.country,
        d: path({ type: 'LineString', coordinates: [from, to] }) ?? '',
        width: 1 + 3 * Math.sqrt(share),
        end: projection(to),
      }
    })
})
const homePoint = computed(() => {
  const home = props.home ? byId.get(props.home) : undefined
  return home ? projection(centroid(home)) : null
})

const names = computed(() => new Intl.DisplayNames([locale.value], { type: 'region' }))
const nameOf = (id: string) => {
  try {
    return names.value.of(id) ?? id
  } catch {
    return id
  }
}
const hover = ref<string>()
const hovered = computed(() => {
  if (!hover.value) return undefined
  const c = props.countries.find((x) => x.country === hover.value)
  return { name: nameOf(hover.value), rx: c?.rx ?? 0, tx: c?.tx ?? 0 }
})
function pick(id: string) {
  if (!traffic.value.has(id)) return
  emit('select', props.selected === id ? undefined : id)
}
</script>

<template>
  <div class="world" data-test="world-map">
    <svg :viewBox="`0 0 ${W} ${H}`" role="img" :aria-label="t('internet.mapLabel')">
      <path
        v-for="c in countryShapes"
        :key="c.id"
        class="country"
        :class="{
          on: c.shade !== undefined,
          home: c.id === home,
          picked: c.id === selected,
        }"
        :d="c.d"
        :style="c.shade !== undefined ? { fillOpacity: c.shade } : undefined"
        :data-country="c.id"
        @pointerenter="hover = c.id"
        @pointerleave="hover = undefined"
        @click="pick(c.id)"
      />
      <path
        v-for="a in arcs"
        :key="a.id"
        class="arc"
        :class="{ dim: selected && selected !== a.id }"
        :d="a.d"
        :style="{ strokeWidth: a.width }"
      />
      <template v-for="a in arcs" :key="`end-${a.id}`">
        <circle v-if="a.end" class="end" :cx="a.end[0]" :cy="a.end[1]" r="3" />
      </template>
      <circle v-if="homePoint" class="home-dot" :cx="homePoint[0]" :cy="homePoint[1]" r="5" />
    </svg>
    <div v-if="hovered" class="tip" data-test="world-tip">
      <strong>{{ hovered.name }}</strong>
      <span v-if="hovered.rx || hovered.tx">
        ↓ {{ formatBytes(hovered.rx) }} · ↑ {{ formatBytes(hovered.tx) }}
      </span>
      <span v-else class="muted">{{ t('internet.noTraffic') }}</span>
    </div>
  </div>
</template>

<style scoped>
.world {
  position: relative;
}
svg {
  display: block;
  width: 100%;
  height: auto;
}
.country {
  fill: var(--border);
  stroke: var(--bg-elevated, var(--bg));
  stroke-width: 0.5;
}
.country.on {
  fill: var(--accent);
  cursor: pointer;
}
.country.on:hover,
.country.picked {
  stroke: var(--text);
  stroke-width: 1;
}
.country.home {
  stroke: var(--accent);
  stroke-width: 1.2;
}
.arc {
  fill: none;
  stroke: #f59e0b;
  stroke-linecap: round;
  stroke-dasharray: 6 5;
  opacity: 0.85;
  animation: flow 1.6s linear infinite;
  pointer-events: none;
}
.arc.dim {
  opacity: 0.2;
}
.end {
  fill: #f59e0b;
  pointer-events: none;
}
.home-dot {
  fill: var(--accent);
  stroke: var(--bg);
  stroke-width: 2;
  pointer-events: none;
}
@keyframes flow {
  to {
    stroke-dashoffset: -22;
  }
}
@media (prefers-reduced-motion: reduce) {
  .arc {
    animation: none;
  }
}
.tip {
  position: absolute;
  top: 8px;
  left: 8px;
  display: grid;
  gap: 2px;
  padding: 6px 9px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-elevated, var(--bg));
  font-size: 12px;
  pointer-events: none;
}
</style>
