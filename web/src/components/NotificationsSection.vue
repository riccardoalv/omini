<script setup lang="ts">
import { ChevronDown, Hash, Mail, Pencil, Plus, Send, Trash2, Webhook } from 'lucide-vue-next'
import { siDiscord, siNtfy, siTelegram } from 'simple-icons'
import { computed, onMounted, ref, watch, type Component } from 'vue'
import { useI18n } from 'vue-i18n'

import IntegrationFields from '@/components/IntegrationFields.vue'
import ToggleSwitch from '@/components/ToggleSwitch.vue'
import { api, ApiError } from '@/lib/api'
import { formatAgo } from '@/lib/format'
import { initialValues, missingRequired, toConfig } from '@/lib/forms'
import type { Config, FormField, Notifier, NotifierType, Severity } from '@/lib/types'

/**
 * Settings → Notifications: one accordion per way of being told — Telegram,
 * e-mail, Slack, Discord, ntfy and a plain webhook (Slack, Discord and ntfy
 * are webhooks in a format of their own) — each with how to set it up, its
 * channels and the form to add one.
 */
const { t, tm, rt, locale } = useI18n()

interface Kind {
  id: 'telegram' | 'email' | 'slack' | 'discord' | 'ntfy' | 'webhook'
  type: NotifierType['type']
  /** Webhook format this kind stands for. */
  format?: 'json' | 'slack' | 'discord' | 'ntfy'
  icon?: Component
  logo?: { path: string; hex: string }
}
const KINDS: Kind[] = [
  { id: 'telegram', type: 'telegram', logo: siTelegram },
  { id: 'email', type: 'email', icon: Mail },
  { id: 'slack', type: 'webhook', format: 'slack', icon: Hash },
  { id: 'discord', type: 'webhook', format: 'discord', logo: siDiscord },
  { id: 'ntfy', type: 'webhook', format: 'ntfy', logo: siNtfy },
  { id: 'webhook', type: 'webhook', format: 'json', icon: Webhook },
]

const types = ref<NotifierType[]>([])
const channels = ref<Notifier[]>([])
const loaded = ref(false)

async function load() {
  try {
    ;[types.value, channels.value] = await Promise.all([api.notifierTypes(), api.notifiers()])
  } catch {
    // shown empty; the next visit retries
  } finally {
    loaded.value = true
  }
}
onMounted(load)

// Omini's address, for the "Open in Omini" links of the messages. Picked up
// from the browser when a channel is added or tested; editable here.
const address = ref('')
const addressSaved = ref('')
const addressError = ref('')
const addressOk = ref(false)
const here = window.location.origin
onMounted(async () => {
  try {
    addressSaved.value = address.value = (await api.notifierSettings()).public_url
  } catch {
    // shown empty
  }
})
async function saveAddress(value = address.value) {
  addressError.value = ''
  addressOk.value = false
  try {
    addressSaved.value = address.value = (
      await api.setNotifierSettings({ public_url: value })
    ).public_url
    addressOk.value = true
  } catch (e) {
    addressError.value = e instanceof ApiError ? e.message : t('common.error')
  }
}

/** Kinds the server offers (a type it does not know is not shown). */
const kinds = computed(() => KINDS.filter((k) => types.value.some((x) => x.type === k.type)))

/** The kind of a channel: a webhook by its format. */
function kindOf(n: Notifier): Kind['id'] {
  if (n.type !== 'webhook') return n.type
  const f = String(n.config.format ?? 'json')
  return f === 'slack' || f === 'discord' || f === 'ntfy' ? f : 'webhook'
}
const channelsOf = (k: Kind) => channels.value.filter((n) => kindOf(n) === k.id)

// Open accordions: the ones with channels start open.
const open = ref<Set<string>>(new Set())
watch(loaded, () => {
  open.value = new Set(kinds.value.filter((x) => channelsOf(x).length).map((x) => x.id))
})
const isOpen = (k: Kind) => open.value.has(k.id)
function toggleOpen(k: Kind) {
  const next = new Set(open.value)
  if (next.has(k.id)) next.delete(k.id)
  else next.add(k.id)
  open.value = next
}

/**
 * The fields of a kind's form: the type's fields, without the format (the
 * kind sets it) and, but for a plain webhook, without the signing secret.
 */
function fieldsOf(k: Kind): FormField[] {
  const all = types.value.find((x) => x.type === k.type)?.fields ?? []
  if (k.type !== 'webhook') return all
  return all.filter((f) => f.key !== 'format' && (k.format === 'json' || f.key !== 'secret'))
}

// The form: a new channel of a kind, or an existing one.
const editing = ref<{ id?: number; kind: Kind }>()
const values = ref<Config>({})
const minSeverity = ref<Severity>('warning')
const notifyResolved = ref(true)
const error = ref('')
const tested = ref('')
const busy = ref(false)

function startNew(k: Kind) {
  editing.value = { kind: k }
  values.value = initialValues(fieldsOf(k))
  minSeverity.value = 'warning'
  notifyResolved.value = true
  error.value = tested.value = ''
}
function startEdit(k: Kind, n: Notifier) {
  editing.value = { id: n.id, kind: k }
  values.value = initialValues(fieldsOf(k), n.config)
  minSeverity.value = n.min_severity
  notifyResolved.value = n.notify_resolved
  error.value = tested.value = ''
}

function config(): Config | undefined {
  const k = editing.value!.kind
  const fields = fieldsOf(k)
  const keep = editing.value!.id !== undefined
  const missing = missingRequired(fields, values.value, keep)
  if (missing.length) {
    error.value = t('notifications.missing', { fields: missing.join(', ') })
    return undefined
  }
  const cfg = toConfig(fields, values.value, keep)
  return k.format ? { ...cfg, format: k.format } : cfg
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
      await api.createNotifier({ type: editing.value.kind.type, ...input })
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
      type: editing.value.kind.type,
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

/** The setup steps of a kind, from the messages (a list per language). */
function steps(k: Kind): string[] {
  const list = tm(`notifications.kinds.${k.id}.steps`) as unknown
  return Array.isArray(list) ? list.map((m) => rt(m)) : []
}

function status(k: Kind): string {
  const list = channelsOf(k)
  if (!list.length) return t('notifications.notSet')
  const on = list.filter((n) => n.enabled).length
  return t('notifications.channelsOn', { n: list.length, on }, list.length)
}
</script>

<template>
  <div class="notifications" data-test="notifications">
    <p class="muted intro">{{ t('notifications.hint') }}</p>

    <form class="address card" data-test="public-url" @submit.prevent="saveAddress()">
      <div class="address-text">
        <label for="n-address">{{ t('notifications.address') }}</label>
        <small class="muted">{{ t('notifications.addressHint') }}</small>
      </div>
      <div class="address-input">
        <input
          id="n-address"
          v-model="address"
          class="input"
          type="url"
          :placeholder="here"
          @input="addressOk = false"
        />
        <button
          v-if="!address && !addressSaved"
          class="btn"
          type="button"
          data-test="use-here"
          @click="saveAddress(here)"
        >
          {{ t('notifications.useHere') }}
        </button>
        <button v-else class="btn" type="submit" :disabled="address === addressSaved">
          {{ t('common.save') }}
        </button>
      </div>
      <small v-if="addressOk" class="ok">{{ t('notifications.addressSaved') }}</small>
      <small v-if="addressError" class="error" role="alert">{{ addressError }}</small>
    </form>

    <div class="kinds">
      <section
        v-for="k in kinds"
        :key="k.id"
        class="kind card"
        :class="{ open: isOpen(k), active: channelsOf(k).some((n) => n.enabled) }"
        :data-test="`kind-${k.id}`"
      >
        <button
          class="kind-head"
          type="button"
          :aria-expanded="isOpen(k)"
          :data-test="`toggle-${k.id}`"
          @click="toggleOpen(k)"
        >
          <span class="kind-icon" :style="k.logo ? { color: `#${k.logo.hex}` } : undefined">
            <svg v-if="k.logo" viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
              <path :d="k.logo.path" fill="currentColor" />
            </svg>
            <component :is="k.icon" v-else :size="22" />
          </span>
          <span class="kind-text">
            <strong>{{ t(`notifications.kinds.${k.id}.name`) }}</strong>
            <span class="muted">{{ t(`notifications.kinds.${k.id}.what`) }}</span>
          </span>
          <span class="status" :class="{ set: channelsOf(k).length }">{{ status(k) }}</span>
          <ChevronDown :size="18" class="chevron" />
        </button>

        <div v-if="isOpen(k)" class="kind-body">
          <div class="guide">
            <h4>{{ t('notifications.howTo') }}</h4>
            <ol>
              <li v-for="(s, i) in steps(k)" :key="i">{{ s }}</li>
            </ol>
          </div>

          <div class="side">
            <ul v-if="channelsOf(k).length" class="channels">
              <li v-for="n in channelsOf(k)" :key="n.id" class="channel" data-test="channel">
                <div class="grow">
                  <span class="target">{{ target(n) }}</span>
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
                <button
                  class="btn ghost icon small"
                  :aria-label="t('common.edit')"
                  @click="startEdit(k, n)"
                >
                  <Pencil :size="14" />
                </button>
                <button
                  class="btn ghost icon small"
                  :aria-label="t('common.delete')"
                  @click="remove(n)"
                >
                  <Trash2 :size="14" />
                </button>
              </li>
            </ul>

            <form
              v-if="editing?.kind.id === k.id"
              class="form"
              data-test="channel-form"
              @submit.prevent="save"
            >
              <h4>
                {{
                  editing.id === undefined
                    ? t('notifications.newChannel')
                    : t('notifications.editChannel')
                }}
              </h4>
              <IntegrationFields
                v-model="values"
                :type="`notify_${k.type}`"
                :fields="fieldsOf(k)"
                :editing="editing.id !== undefined"
                :id-prefix="`n-${k.id}`"
              />
              <div class="field">
                <label :for="`n-${k.id}-severity`">{{ t('notifications.minSeverity') }}</label>
                <select
                  :id="`n-${k.id}-severity`"
                  v-model="minSeverity"
                  class="select"
                  data-test="min-severity"
                >
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
                <button
                  class="btn"
                  type="button"
                  data-test="test-channel"
                  :disabled="busy"
                  @click="test"
                >
                  <Send :size="14" />{{ t('notifications.test') }}
                </button>
                <button class="btn primary" type="submit" data-test="save-channel" :disabled="busy">
                  {{ t('common.save') }}
                </button>
              </div>
            </form>
            <button
              v-else
              class="btn add"
              type="button"
              :data-test="`add-${k.id}`"
              @click="startNew(k)"
            >
              <Plus :size="15" />{{
                t('notifications.addKind', { kind: t(`notifications.kinds.${k.id}.name`) })
              }}
            </button>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.intro {
  margin: 0 0 16px;
}
.address {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 16px;
  margin-bottom: 16px;
  padding: 14px 18px;
}
.address-text {
  display: grid;
  flex: 1;
  gap: 2px;
  min-width: 220px;
}
.address-text small {
  font-size: 13px;
}
.address-input {
  display: flex;
  gap: 8px;
  flex: 1;
  min-width: 260px;
}
.address-input .input {
  flex: 1;
}
.address .ok {
  flex-basis: 100%;
  color: var(--ok);
}
.address .error {
  flex-basis: 100%;
  color: var(--danger);
}
.kinds {
  display: grid;
  gap: 10px;
}
.kind {
  padding: 0;
  overflow: hidden;
}
.kind.active {
  border-color: color-mix(in srgb, var(--ok) 45%, var(--border));
}
.kind-head {
  display: flex;
  align-items: center;
  gap: 14px;
  width: 100%;
  padding: 14px 18px;
  border: 0;
  background: transparent;
  color: var(--text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.kind-head:hover {
  background: var(--surface-hover);
}
.kind-icon {
  display: grid;
  flex: none;
  place-items: center;
  width: 40px;
  height: 40px;
  border-radius: var(--radius);
  background: var(--bg);
  color: var(--accent);
}
.kind-text {
  display: grid;
  flex: 1;
  gap: 2px;
  min-width: 0;
}
.kind-text strong {
  font-size: 15px;
}
.kind-text .muted {
  font-size: 13px;
}
.status {
  flex: none;
  padding: 3px 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  color: var(--text-muted);
  font-size: 12px;
  white-space: nowrap;
}
.status.set {
  border-color: color-mix(in srgb, var(--ok) 50%, transparent);
  color: var(--ok);
}
.chevron {
  flex: none;
  color: var(--text-muted);
  transition: transform 0.15s;
}
.open .chevron {
  transform: rotate(180deg);
}
.kind-body {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr);
  gap: 24px;
  padding: 4px 18px 18px 72px;
}
@media (max-width: 900px) {
  .kind-body {
    grid-template-columns: 1fr;
    padding-left: 18px;
  }
}
.guide h4,
.form h4 {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 600;
}
.guide ol {
  display: grid;
  gap: 6px;
  margin: 0;
  padding-left: 20px;
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.5;
}
.side {
  display: grid;
  align-content: start;
  gap: 12px;
}
.channels {
  display: grid;
  gap: 8px;
  margin: 0;
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
  background: var(--bg);
}
.grow {
  display: grid;
  flex: 1;
  min-width: 0;
}
.target {
  overflow: hidden;
  font-family: var(--mono);
  font-size: 12.5px;
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
.add {
  justify-self: start;
}
.form {
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg);
}
.bool {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 10px 0;
}
.buttons {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}
</style>
