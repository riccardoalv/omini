<script setup lang="ts">
import { ArrowRight, ListOrdered, RefreshCw } from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import { formatAgo } from '@/lib/format'
import type { CollectionInfo, Integration } from '@/lib/types'

/**
 * How Omini collects: in rounds, one integration at a time, from the edge of
 * the network (access points, servers) to its center (switches, then the
 * router), the map built once at the end. Shows the interval (editable), the
 * order of the last round and when it ran.
 */
const props = defineProps<{ integrations: Integration[] }>()
const emit = defineEmits<{ ran: [] }>()
const { t, locale } = useI18n()

const info = ref<CollectionInfo>()
const error = ref('')
const INTERVALS = [15, 30, 60, 120, 300, 600, 900, 1800, 3600, 21600, 86400]

async function load() {
  try {
    info.value = await api.collection()
  } catch {
    // shown without the round; the next refresh retries
  }
}
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  timer = setInterval(load, 15_000)
})
onBeforeUnmount(() => {
  clearInterval(timer)
  clearTimeout(watching)
})

// "Run a round now": the whole round, edge to center; followed until the
// server reports a round other than the one shown when it was asked.
const running = ref(false)
let watching: ReturnType<typeof setTimeout> | undefined
const WATCH_EVERY_MS = 2000
const WATCH_FOR_MS = 15 * 60_000
async function runRound() {
  error.value = ''
  running.value = true
  const before = info.value?.round?.started_at
  const until = Date.now() + WATCH_FOR_MS
  try {
    await api.runRound()
  } catch (e) {
    running.value = false
    error.value = e instanceof ApiError ? e.message : t('common.error')
    return
  }
  const check = async () => {
    await load()
    if (info.value?.round?.started_at !== before || Date.now() > until) {
      running.value = false
      emit('ran')
      return
    }
    watching = setTimeout(check, WATCH_EVERY_MS)
  }
  watching = setTimeout(check, WATCH_EVERY_MS)
}

async function setInterval_(s: number) {
  error.value = ''
  try {
    info.value = await api.setCollection(s === info.value?.default_s ? 0 : s)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  }
}

function label(s: number) {
  if (s < 60) return t('integrations.everySeconds', { n: s })
  if (s < 3600) return t('integrations.everyMinutes', { n: s / 60 }, s / 60)
  return t('integrations.everyHours', { n: s / 3600 }, s / 3600)
}
const options = computed(() => {
  const list = new Set(INTERVALS)
  if (info.value) list.add(info.value.interval_s).add(info.value.default_s)
  return [...list].sort((a, b) => a - b)
})

const names = computed(() => new Map(props.integrations.map((i) => [i.id, i.name])))
const order = computed(() =>
  (info.value?.round?.order ?? []).map((id) => names.value.get(id)).filter((n): n is string => !!n),
)
</script>

<template>
  <section class="card round" data-test="collection-round">
    <div class="head">
      <ListOrdered :size="18" class="icon" />
      <div class="text">
        <strong>{{ t('round.title') }}</strong>
        <span class="muted">{{ t('round.hint') }}</span>
      </div>
      <label class="every">
        <span class="muted">{{ t('integrations.interval') }}</span>
        <select
          class="select small"
          data-test="round-interval"
          :value="info?.interval_s"
          :disabled="!info"
          @change="setInterval_(Number(($event.target as HTMLSelectElement).value))"
        >
          <option v-for="s in options" :key="s" :value="s">
            {{ label(s)
            }}<template v-if="s === info?.default_s"> · {{ t('round.default') }}</template>
          </option>
        </select>
      </label>
      <button
        class="btn primary"
        type="button"
        data-test="run-round"
        :disabled="running || !info"
        @click="runRound"
      >
        <RefreshCw :size="15" :class="{ spin: running }" />
        {{ running ? t('round.running') : t('round.run') }}
      </button>
    </div>
    <div v-if="order.length" class="order" data-test="round-order">
      <span class="muted edge">{{ t('round.edge') }}</span>
      <template v-for="(n, i) in order" :key="i">
        <ArrowRight v-if="i > 0" :size="14" class="arrow" />
        <span class="step"
          ><b>{{ i + 1 }}</b> {{ n }}</span
        >
      </template>
      <span class="muted edge">{{ t('round.center') }}</span>
    </div>
    <p v-if="info?.round" class="muted small last">
      {{
        t('round.last', {
          ago: formatAgo(info.round.started_at, locale),
          s: (info.round.duration_ms / 1000).toFixed(1),
        })
      }}
    </p>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
  </section>
</template>

<style scoped>
.round {
  display: grid;
  gap: 10px;
  margin-bottom: 16px;
  padding: 14px 18px;
}
.head {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.icon {
  flex: none;
  color: var(--accent);
}
.text {
  display: grid;
  flex: 1;
  gap: 2px;
  min-width: 220px;
}
.text .muted {
  font-size: 13px;
}
.every {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.order {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 13px;
}
.step {
  padding: 3px 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--bg);
}
.step b {
  margin-right: 4px;
  color: var(--accent);
}
.arrow {
  color: var(--text-muted);
}
.edge {
  font-size: 12px;
}
.small {
  font-size: 12px;
}
.last {
  margin: 0;
}
.spin {
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
