<script setup lang="ts">
import { Bell, Mail, Pencil, Send, Trash2, Webhook } from 'lucide-vue-next'
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import IntegrationFields from '@/components/IntegrationFields.vue'
import ToggleSwitch from '@/components/ToggleSwitch.vue'
import { api, ApiError } from '@/lib/api'
import { formatAgo } from '@/lib/format'
import { initialValues, missingRequired, toConfig } from '@/lib/forms'
import type { Config, Notifier, NotifierType, Severity } from '@/lib/types'

/** Settings → Notifications: channels the alerts are sent to. */
const { t, locale } = useI18n()

const types = ref<NotifierType[]>([])
const channels = ref<Notifier[]>([])
const icons = { webhook: Webhook, telegram: Send, email: Mail }

async function load() {
  try {
    ;[types.value, channels.value] = await Promise.all([api.notifierTypes(), api.notifiers()])
  } catch {
    // shown empty; the next visit retries
  }
}
onMounted(load)

// The form: a new channel (type picked) or an existing one.
const editing = ref<{ id?: number; type: NotifierType['type'] }>()
const values = ref<Config>({})
const minSeverity = ref<Severity>('warning')
const notifyResolved = ref(true)
const error = ref('')
const tested = ref('')
const busy = ref(false)

const fieldsOf = (type: string) => types.value.find((k) => k.type === type)?.fields ?? []

function startNew(type: NotifierType['type']) {
  editing.value = { type }
  values.value = initialValues(fieldsOf(type))
  minSeverity.value = 'warning'
  notifyResolved.value = true
  error.value = tested.value = ''
}
function startEdit(n: Notifier) {
  editing.value = { id: n.id, type: n.type }
  values.value = initialValues(fieldsOf(n.type), n.config)
  minSeverity.value = n.min_severity
  notifyResolved.value = n.notify_resolved
  error.value = tested.value = ''
}

function config(): Config | undefined {
  const fields = fieldsOf(editing.value!.type)
  const keep = editing.value!.id !== undefined
  const missing = missingRequired(fields, values.value, keep)
  if (missing.length) {
    error.value = t('notifications.missing', { fields: missing.join(', ') })
    return undefined
  }
  return toConfig(fields, values.value, keep)
}

async function save() {
  const cfg = config()
  if (!cfg || !editing.value) return
  busy.value = true
  error.value = ''
  try {
    const input = {
      config: cfg,
      min_severity: minSeverity.value,
      notify_resolved: notifyResolved.value,
    }
    if (editing.value.id === undefined)
      await api.createNotifier({ type: editing.value.type, ...input })
    else await api.updateNotifier(editing.value.id, input)
    editing.value = undefined
    await load()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    busy.value = false
  }
}

async function test() {
  const cfg = config()
  if (!cfg || !editing.value) return
  busy.value = true
  error.value = tested.value = ''
  try {
    const res = await api.testNotifier({
      id: editing.value.id,
      type: editing.value.type,
      config: cfg,
    })
    if (res.ok) tested.value = t('notifications.testOk')
    else error.value = res.error ?? t('common.error')
  } finally {
    busy.value = false
  }
}

async function toggle(n: Notifier, enabled: boolean) {
  const updated = await api.updateNotifier(n.id, { enabled })
  channels.value = channels.value.map((c) => (c.id === n.id ? updated : c))
}

async function remove(n: Notifier) {
  if (!window.confirm(t('notifications.confirmDelete'))) return
  await api.deleteNotifier(n.id)
  channels.value = channels.value.filter((c) => c.id !== n.id)
}

/** What the channel sends to, without secrets: the address, chat or recipients. */
function target(n: Notifier): string {
  const c = n.config
  if (n.type === 'webhook') return String(c.url ?? '')
  if (n.type === 'telegram') return `chat ${c.chat_id ?? ''}`
  return String(c.to ?? '')
}
</script>

<template>
  <section class="card section" data-test="notifications">
    <h2><Bell :size="16" /> {{ t('notifications.title') }}</h2>
    <p class="muted">{{ t('notifications.hint') }}</p>

    <ul v-if="channels.length" class="channels">
      <li v-for="n in channels" :key="n.id" class="channel" data-test="channel">
        <component :is="icons[n.type]" :size="18" class="kind" />
        <div class="grow">
          <strong>{{ t(`notifications.types.${n.type}`) }}</strong>
          <span class="muted target">{{ target(n) }}</span>
          <span class="muted small">
            {{ t(`notifications.severity.${n.min_severity}`) }} ·
            {{
              n.last_sent_at
                ? t('notifications.lastSent', { ago: formatAgo(n.last_sent_at, locale) })
                : t('notifications.never')
            }}
          </span>
          <span v-if="n.last_error" class="err small" data-test="channel-error">{{
            n.last_error
          }}</span>
        </div>
        <ToggleSwitch
          :model-value="n.enabled"
          :label="t('notifications.enabled')"
          @update:model-value="(v: boolean) => toggle(n, v)"
        />
        <button class="btn ghost icon small" :aria-label="t('common.edit')" @click="startEdit(n)">
          <Pencil :size="14" />
        </button>
        <button class="btn ghost icon small" :aria-label="t('common.delete')" @click="remove(n)">
          <Trash2 :size="14" />
        </button>
      </li>
    </ul>
    <p v-else-if="!editing" class="muted">{{ t('notifications.none') }}</p>

    <div v-if="!editing" class="add">
      <button
        v-for="k in types"
        :key="k.type"
        class="btn small"
        :data-test="`add-${k.type}`"
        @click="startNew(k.type)"
      >
        <component :is="icons[k.type]" :size="14" />{{ t(`notifications.types.${k.type}`) }}
      </button>
    </div>

    <form v-else class="form" data-test="channel-form" @submit.prevent="save">
      <h3>{{ t(`notifications.types.${editing.type}`) }}</h3>
      <IntegrationFields
        v-model="values"
        :type="`notify_${editing.type}`"
        :fields="fieldsOf(editing.type)"
        :editing="editing.id !== undefined"
        id-prefix="n"
      />
      <div class="field">
        <label for="n-severity">{{ t('notifications.minSeverity') }}</label>
        <select id="n-severity" v-model="minSeverity" class="select" data-test="min-severity">
          <option v-for="s in ['critical', 'warning', 'info']" :key="s" :value="s">
            {{ t(`notifications.severity.${s}`) }}
          </option>
        </select>
      </div>
      <div class="bool">
        <ToggleSwitch v-model="notifyResolved" :label="t('notifications.notifyResolved')" />
        <span>{{ t('notifications.notifyResolved') }}</span>
      </div>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <p v-if="tested" class="alert ok" role="status">{{ tested }}</p>
      <div class="buttons">
        <button class="btn" type="button" @click="editing = undefined">
          {{ t('common.cancel') }}
        </button>
        <button class="btn" type="button" data-test="test-channel" :disabled="busy" @click="test">
          <Send :size="14" />{{ t('notifications.test') }}
        </button>
        <button class="btn primary" type="submit" data-test="save-channel" :disabled="busy">
          {{ t('common.save') }}
        </button>
      </div>
    </form>
  </section>
</template>

<style scoped>
h2 {
  display: flex;
  align-items: center;
  gap: 8px;
}
.channels {
  display: grid;
  gap: 8px;
  margin: 12px 0;
  padding: 0;
  list-style: none;
}
.channel {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}
.grow {
  display: grid;
  flex: 1;
  min-width: 0;
}
.target {
  overflow: hidden;
  font-family: var(--mono);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.small {
  font-size: 12px;
}
.err {
  color: var(--danger);
  overflow-wrap: anywhere;
}
.kind {
  flex: none;
  color: var(--accent);
}
.add {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 8px;
}
.form {
  margin-top: 14px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}
.form h3 {
  margin: 0 0 10px;
  font-size: 14px;
}
.bool {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 10px 0;
}
.buttons {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
