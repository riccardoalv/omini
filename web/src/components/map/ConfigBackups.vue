<script setup lang="ts">
import { Download } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/lib/api'
import { type DiffPart, diffLines, diffParts } from '@/lib/diff'
import { formatAgo, formatBytes, formatDateTime } from '@/lib/format'
import type { Backup } from '@/lib/types'

import PanelSection from './PanelSection.vue'

/**
 * The device's configuration over time: each version Omini kept (daily and
 * after its own changes), marked as made by Omini or outside it, the
 * differences between two of them, and a download. Never restored.
 */
const props = defineProps<{ nodeId: string; label: string }>()
const { t, locale } = useI18n()

const versions = ref<Backup[]>([])
const from = ref<number>()
const to = ref<number>()
const contents = ref(new Map<number, string>())
const error = ref('')

watch(
  () => props.nodeId,
  async (id) => {
    versions.value = []
    contents.value = new Map()
    try {
      const list = await api.backups(id)
      if (props.nodeId !== id) return
      versions.value = list
      to.value = list[0]?.id
      from.value = list[1]?.id
    } catch {
      // no section
    }
  },
  { immediate: true },
)

async function content(id: number): Promise<string> {
  const have = contents.value.get(id)
  if (have !== undefined) return have
  const b = await api.backup(id)
  contents.value = new Map(contents.value).set(id, b.content)
  return b.content
}

const parts = ref<DiffPart[]>([])
watch(
  [from, to],
  async ([a, b]) => {
    parts.value = []
    error.value = ''
    if (a === undefined || b === undefined) return
    try {
      const [x, y] = await Promise.all([content(a), content(b)])
      parts.value = diffParts(diffLines(x, y))
    } catch {
      error.value = t('common.error')
    }
  },
  { immediate: true },
)
const changed = computed(() => parts.value.some((p) => 'lines' in p))

const ext: Record<string, string> = { xml: 'xml', json: 'json', text: 'txt' }
async function download(id: number) {
  const v = versions.value.find((x) => x.id === id)
  if (!v) return
  const blob = new Blob([await content(id)], { type: 'text/plain' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `${props.label}-${v.taken_at.slice(0, 19).replace(/[T:]/g, '-')}.${ext[v.format] ?? 'txt'}`
  a.click()
  URL.revokeObjectURL(a.href)
}

const option = (v: Backup) =>
  `${formatDateTime(v.taken_at, locale.value)} · ${v.by_omini ? t('backups.byOmini') : t('backups.outside')}`
const summary = computed(() =>
  versions.value.length
    ? t('backups.summary', {
        n: versions.value.length,
        when: formatAgo(versions.value[0]!.taken_at, locale.value),
      })
    : '',
)
</script>

<template>
  <PanelSection
    v-if="versions.length"
    id="config"
    :title="t('backups.title')"
    :summary="summary"
    data-test="config-backups"
  >
    <div class="backups">
      <p class="muted small">{{ t('backups.hint') }}</p>
      <div class="pick">
        <label>
          <span>{{ t('backups.from') }}</span>
          <select v-model="from" class="select" data-test="backup-from">
            <option v-for="v in versions" :key="v.id" :value="v.id">{{ option(v) }}</option>
          </select>
        </label>
        <label>
          <span>{{ t('backups.to') }}</span>
          <select v-model="to" class="select" data-test="backup-to">
            <option v-for="v in versions" :key="v.id" :value="v.id">{{ option(v) }}</option>
          </select>
        </label>
        <button
          v-if="to !== undefined"
          class="btn small"
          type="button"
          data-test="backup-download"
          @click="download(to)"
        >
          <Download :size="14" />{{ t('backups.download') }}
        </button>
      </div>
      <p v-if="versions.length === 1" class="muted small">
        {{ t('backups.only', { size: formatBytes(versions[0]!.size) }) }}
      </p>
      <p v-else-if="from !== undefined && !changed && !error" class="muted small">
        {{ t('backups.same') }}
      </p>
      <pre v-if="changed" class="diff" data-test="backup-diff"><template
        v-for="(p, k) in parts"
        :key="k"
      ><span v-if="'skipped' in p" class="skip">{{ t('backups.skipped', { n: p.skipped }, p.skipped) }}
</span><template v-else><span
        v-for="(l, j) in p.lines"
        :key="j"
        :class="{ add: l.op === '+', del: l.op === '-' }"
      >{{ l.op }} {{ l.text }}
</span></template></template></pre>
      <p v-if="error" class="error small" role="alert">{{ error }}</p>
    </div>
  </PanelSection>
</template>

<style scoped>
.backups {
  display: grid;
  gap: 8px;
}
.pick {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: end;
}
.pick label {
  display: grid;
  gap: 2px;
  font-size: 12px;
  flex: 1;
  min-width: 10em;
}
.diff {
  max-height: 360px;
  margin: 0;
  padding: 8px;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  font-family: var(--mono);
  font-size: 11.5px;
  line-height: 1.45;
  white-space: pre;
}
.add {
  display: block;
  background: var(--ok-soft);
}
.del {
  display: block;
  background: var(--danger-soft);
}
.skip {
  display: block;
  color: var(--text-muted);
  font-style: italic;
}
.small {
  margin: 0;
  font-size: 12px;
}
.error {
  color: var(--danger);
}
</style>
