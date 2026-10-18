<script setup lang="ts">
import { CircleAlert, CircleCheck, CircleDashed, LoaderCircle } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import ModalDialog from '@/components/ModalDialog.vue'
import { api, ApiError } from '@/lib/api'
import type { ChannelPlan, ChannelResult, ChannelStep } from '@/lib/types'

/**
 * Resolving a Wi-Fi channel conflict: the plan for every access point
 * involved (how many clients each has, whether this browser or Omini's machine
 * is on it), the radios left alone and why, then each step's outcome. The
 * plan goes one access point at a time and stops at the first that fails.
 */
const emit = defineEmits<{ close: []; applied: [] }>()
const { t } = useI18n()

const plan = ref<ChannelPlan>()
const allowDestructive = ref(false)
const loading = ref(true)
const busy = ref(false)
const error = ref('')
const again = ref(false)
const results = ref<ChannelResult[]>()

onMounted(async () => {
  try {
    const [p, settings] = await Promise.all([
      api.wifiChannelPlan(),
      api.changeSettings().catch(() => undefined),
    ])
    plan.value = p
    allowDestructive.value = settings?.allow_destructive ?? false
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    loading.value = false
  }
})

const destructive = computed(() => plan.value?.steps.some((s) => s.protected) ?? false)
const blocked = computed(() => destructive.value && !allowDestructive.value)
/** Each step as planned, or as it went once applied. */
const rows = computed<(ChannelStep & { status?: ChannelResult['status']; error?: string })[]>(
  () => results.value ?? plan.value?.steps ?? [],
)
const failed = computed(() => results.value?.find((r) => r.status === 'failed'))

async function apply() {
  if (destructive.value && !again.value) {
    again.value = true
    return
  }
  busy.value = true
  error.value = ''
  try {
    results.value = (await api.applyWifiChannels(destructive.value)).results
    emit('applied')
  } catch (e) {
    const data = e instanceof ApiError ? (e.data as { results?: ChannelResult[] }) : undefined
    if (data?.results) {
      results.value = data.results
      emit('applied')
    } else {
      error.value = e instanceof ApiError ? e.message : t('common.error')
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ModalDialog :title="t('wifiChannels.title')" wide @close="emit('close')">
    <p class="muted intro">{{ t('wifiChannels.intro') }}</p>
    <p v-if="loading" class="muted" data-test="wc-loading">{{ t('wifiChannels.loading') }}</p>
    <template v-else-if="plan">
      <p v-if="!plan.steps.length" class="muted" data-test="wc-none">
        {{ t('wifiChannels.none') }}
      </p>
      <ol v-else class="steps" data-test="wc-steps">
        <li
          v-for="s in rows"
          :key="s.node_id + s.radio"
          :class="{ danger: s.protected }"
          data-test="wc-step"
        >
          <span class="status" :data-status="s.status ?? (busy ? 'running' : 'planned')">
            <LoaderCircle v-if="busy" :size="16" class="spin" />
            <CircleCheck v-else-if="s.status === 'done'" :size="16" class="ok" />
            <CircleAlert v-else-if="s.status === 'failed'" :size="16" class="bad" />
            <CircleDashed v-else-if="s.status === 'not_run'" :size="16" class="muted" />
          </span>
          <div class="grow">
            <strong>{{
              t('wifiChannels.step', { ap: s.label, radio: s.radio, from: s.from, to: s.to })
            }}</strong>
            <small class="muted">{{
              t('wifiChannels.clients', { n: s.clients }, s.clients)
            }}</small>
            <small v-if="s.browser" class="warn" data-test="wc-browser">{{
              t('wifiChannels.browser')
            }}</small>
            <small v-if="s.protected" class="bad" data-test="wc-omini">{{
              t('wifiChannels.omini')
            }}</small>
            <small v-if="s.status" class="muted">
              {{ t(`wifiChannels.status.${s.status}`)
              }}<template v-if="s.error">: {{ s.error }}</template>
            </small>
          </div>
        </li>
      </ol>
      <template v-if="plan.skipped.length">
        <h4>{{ t('wifiChannels.skipped') }}</h4>
        <ul class="skipped" data-test="wc-skipped">
          <li v-for="r in plan.skipped" :key="r.node_id + r.radio">
            {{ t(`wifiChannels.skip.${r.skip}`, { ap: r.label, channel: r.channel }) }}
          </li>
        </ul>
      </template>
      <p v-if="results && !failed" class="ok" role="status" data-test="wc-done">
        {{ t('wifiChannels.allDone') }}
      </p>
      <p v-else-if="failed" class="bad" role="alert" data-test="wc-stopped">
        {{ t('wifiChannels.stopped', { ap: failed.label }) }}
      </p>
      <p v-else-if="blocked" class="bad" data-test="wc-blocked">{{ t('wifiChannels.blocked') }}</p>
      <p v-else-if="again" class="warn" data-test="wc-again">{{ t('wifiChannels.askAgain') }}</p>
    </template>
    <p v-if="error" class="bad" role="alert">{{ error }}</p>

    <template #footer>
      <button class="btn ghost" type="button" @click="emit('close')">
        {{ results ? t('common.close') : t('common.cancel') }}
      </button>
      <button
        v-if="plan?.steps.length && !results"
        class="btn"
        :class="destructive ? 'danger' : 'primary'"
        type="button"
        data-test="wc-apply"
        :disabled="busy || blocked"
        @click="apply"
      >
        {{
          busy
            ? t('wifiChannels.applying')
            : again
              ? t('wifiChannels.applyAnyway')
              : t('wifiChannels.apply', { n: plan.steps.length }, plan.steps.length)
        }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.intro {
  margin-top: 0;
}
.steps,
.skipped {
  display: grid;
  gap: 6px;
  margin: 12px 0;
  padding: 0;
  list-style: none;
}
.steps li {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.steps li.danger {
  border-color: var(--danger);
}
.steps .grow {
  display: grid;
  gap: 2px;
  flex: 1;
}
.status {
  display: inline-flex;
  width: 16px;
  padding-top: 2px;
}
.skipped {
  padding-left: 18px;
  list-style: disc;
  font-size: 12.5px;
  color: var(--text-muted);
}
h4 {
  margin: 12px 0 0;
  font-size: 13px;
}
.ok {
  color: var(--ok);
}
.bad {
  color: var(--danger);
}
.warn {
  color: var(--warn);
}
.spin {
  animation: spin 1s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
