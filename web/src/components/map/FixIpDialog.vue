<script setup lang="ts">
import { CircleAlert, CircleCheck, Lock } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import ModalDialog from '@/components/ModalDialog.vue'
import { api, ApiError } from '@/lib/api'
import type { FixCandidate, FixResult } from '@/lib/types'

/**
 * Fixing many addresses at once: every device still on DHCP, the
 * infrastructure checked, each keeping its address (or the one proposed
 * outside the DHCP range when its router refuses one inside).
 */
const props = defineProps<{
  candidates: FixCandidate[]
  /** A node's name, for the router column. */
  nameOf: (id: string) => string
}>()
const emit = defineEmits<{ close: []; applied: [] }>()
const { t } = useI18n()

// Those whose DHCP server cannot reserve addresses are only listed, with why.
const fixable = computed(() => props.candidates.filter((c) => !c.server))
const elsewhere = computed(() => props.candidates.filter((c) => c.server))
const picked = ref(
  new Set(props.candidates.filter((c) => c.infra && !c.server).map((c) => c.node_id)),
)
const showOthers = ref(false)
const infra = computed(() => fixable.value.filter((c) => c.infra))
const others = computed(() => fixable.value.filter((c) => !c.infra))

function toggle(id: string) {
  const s = new Set(picked.value)
  if (!s.delete(id)) s.add(id)
  picked.value = s
}

const busy = ref(false)
const error = ref('')
const results = ref(new Map<string, FixResult>())
const done = computed(() => results.value.size > 0)

async function apply() {
  busy.value = true
  error.value = ''
  try {
    const items = fixable.value
      .filter((c) => picked.value.has(c.node_id))
      .map((c) => ({ node_id: c.node_id, ip: c.proposed ?? c.ip }))
    const res = await api.fixMany(items)
    results.value = new Map(res.map((r) => [r.node_id, r]))
    emit('applied')
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ModalDialog :title="t('fixIp.title')" wide @close="emit('close')">
    <p class="muted">{{ t('fixIp.intro') }}</p>
    <p v-if="!fixable.length" class="muted" data-test="fix-none">{{ t('fixIp.none') }}</p>

    <template v-for="group in [infra, showOthers ? others : []]" :key="group === infra ? 'i' : 'o'">
      <ul v-if="group.length" class="list" data-test="fix-list">
        <li v-for="c in group" :key="c.node_id" :data-node="c.node_id">
          <label>
            <input
              type="checkbox"
              :checked="picked.has(c.node_id)"
              :disabled="busy || done"
              @change="toggle(c.node_id)"
            />
            <strong>{{ c.label }}</strong>
            <span class="muted small">{{ t('fixIp.on', { router: nameOf(c.router) }) }}</span>
          </label>
          <span class="mono addr">
            <template v-if="c.proposed">
              <s class="muted">{{ c.ip }}</s> → {{ c.proposed }}
            </template>
            <template v-else>{{ c.ip }}</template>
          </span>
          <span v-if="results.get(c.node_id)" class="result">
            <CircleAlert v-if="results.get(c.node_id)!.error" :size="16" class="bad" />
            <CircleCheck v-else :size="16" class="ok" />
            <small v-if="results.get(c.node_id)!.error" class="bad">
              {{ results.get(c.node_id)!.error }}
            </small>
          </span>
          <small v-else-if="c.proposed" class="muted note">{{ t('fixIp.proposed') }}</small>
        </li>
      </ul>
    </template>
    <button
      v-if="others.length && !showOthers"
      class="link"
      type="button"
      data-test="fix-others"
      @click="showOthers = true"
    >
      {{ t('fixIp.others', { n: others.length }, others.length) }}
    </button>
    <ul v-if="elsewhere.length" class="list elsewhere" data-test="fix-elsewhere">
      <li v-for="c in elsewhere" :key="c.node_id" :data-node="c.node_id">
        <strong>{{ c.label }}</strong>
        <span class="mono addr">{{ c.ip }}</span>
        <small class="muted note">{{ t('fixIp.elsewhere', { server: nameOf(c.server!) }) }}</small>
      </li>
    </ul>
    <p v-if="error" class="bad" role="alert">{{ error }}</p>

    <template #footer>
      <button class="btn ghost" type="button" @click="emit('close')">
        {{ done ? t('common.close') : t('common.cancel') }}
      </button>
      <button
        v-if="!done"
        class="btn primary"
        type="button"
        data-test="fix-apply"
        :disabled="busy || !picked.size"
        @click="apply"
      >
        <Lock :size="16" />
        {{ t('fixIp.apply', { n: picked.size }, picked.size) }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.list {
  list-style: none;
  margin: 12px 0;
  padding: 0;
  display: grid;
  gap: 6px;
}
.list li {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 2px 12px;
  align-items: center;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.list label {
  display: flex;
  gap: 8px;
  align-items: baseline;
  flex-wrap: wrap;
  min-width: 0;
}
.addr {
  white-space: nowrap;
}
.note,
.result {
  grid-column: 1 / -1;
}
.result {
  display: flex;
  gap: 6px;
  align-items: center;
}
.ok {
  color: var(--ok);
}
.bad {
  color: var(--danger);
}
</style>
