<script setup lang="ts">
import { ChevronRight, Pause, Pencil, Play, Plus, Trash2 } from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import DeviceIcon from '@/components/DeviceIcon.vue'
import { api, ApiError } from '@/lib/api'
import { displayName } from '@/lib/names'
import type { InventoryEntry, Person, PersonDeviceResult } from '@/lib/types'

/**
 * The people of the network and their devices (the map menu's People tab):
 * add, rename and delete people; a person opens to their devices (a click
 * shows one on the map), and pauses or resumes the internet of all of them
 * at once, after one confirmation, with what happened to each device.
 */
const props = defineProps<{
  /** Ids of the devices whose internet a router pauses. */
  pausedIds?: string[]
}>()
const emit = defineEmits<{ select: [id: string]; changed: [] }>()
const { t } = useI18n()

const people = ref<Person[]>([])
const inventory = ref(new Map<string, InventoryEntry>())
const loading = ref(true)
const error = ref('')

async function load() {
  try {
    const [list, inv] = await Promise.all([api.people(), api.inventory()])
    people.value = list
    inventory.value = new Map(inv.map((e) => [e.id, e]))
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    loading.value = false
  }
}
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  timer = setInterval(load, 60_000)
})
onBeforeUnmount(() => clearInterval(timer))

const deviceName = (id: string) => {
  const e = inventory.value.get(id)
  return e ? displayName({ ...e, label: e.alias || e.label }, t) : id
}
const paused = computed(() => new Set(props.pausedIds ?? []))

/** Runs a request; its error shows above the list. */
async function attempt(fn: () => Promise<unknown>): Promise<boolean> {
  error.value = ''
  try {
    await fn()
    return true
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
    return false
  }
}

// Adding a person.
const newName = ref('')
async function add() {
  const name = newName.value.trim()
  if (!name) return
  if (await attempt(() => api.createPerson({ name }))) {
    newName.value = ''
    await load()
  }
}

// Renaming one, in place.
const renaming = ref<number>()
const renameTo = ref('')
function startRename(p: Person) {
  renaming.value = p.id
  renameTo.value = p.name
}
async function rename(p: Person) {
  const name = renameTo.value.trim()
  if (name && name !== p.name && !(await attempt(() => api.updatePerson(p.id, { name })))) return
  renaming.value = undefined
  await load()
}

// Deleting one: a short confirmation.
const deleting = ref<number>()
async function remove(p: Person) {
  if (await attempt(() => api.deletePerson(p.id))) {
    deleting.value = undefined
    if (open.value === p.id) open.value = undefined
    await load()
    emit('changed')
  }
}

// A person opens to their devices.
const open = ref<number>()
function toggle(p: Person) {
  open.value = open.value === p.id ? undefined : p.id
  asking.value = false
  results.value = undefined
}

// Pausing a person: one confirmation, then what happened to each device.
const asking = ref(false)
const busy = ref(false)
const results = ref<PersonDeviceResult[]>()
async function pauseOrResume(p: Person, pause: boolean) {
  busy.value = true
  results.value = undefined
  const ok = await attempt(async () => {
    results.value = await (pause ? api.pausePerson(p.id) : api.resumePerson(p.id))
  })
  busy.value = false
  asking.value = false
  if (ok) emit('changed')
}
const anyPaused = (p: Person) => p.devices.some((id) => paused.value.has(id))
const anyNotPaused = (p: Person) => p.devices.some((id) => !paused.value.has(id))
</script>

<template>
  <div class="people" data-test="people-tab">
    <form class="add" @submit.prevent="add">
      <input
        v-model="newName"
        class="input"
        data-test="person-name"
        maxlength="60"
        :placeholder="t('people.namePlaceholder')"
        :aria-label="t('people.namePlaceholder')"
      />
      <button
        class="btn small primary"
        type="submit"
        data-test="add-person"
        :disabled="!newName.trim()"
      >
        <Plus :size="14" />{{ t('people.add') }}
      </button>
    </form>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="!loading && !people.length" class="muted small" data-test="people-empty">
      {{ t('people.empty') }}
    </p>

    <ul class="list">
      <li v-for="p in people" :key="p.id" class="person" :data-person="p.id">
        <div class="row">
          <template v-if="renaming === p.id">
            <form class="rename" @submit.prevent="rename(p)">
              <input
                v-model="renameTo"
                class="input"
                data-test="rename-input"
                maxlength="60"
                :aria-label="t('people.rename')"
              />
              <button class="btn small primary" type="submit" data-test="rename-save">
                {{ t('people.save') }}
              </button>
              <button class="btn small ghost" type="button" @click="renaming = undefined">
                {{ t('changes.cancel') }}
              </button>
            </form>
          </template>
          <button
            v-else
            class="open"
            type="button"
            data-test="person-open"
            :aria-expanded="open === p.id"
            @click="toggle(p)"
          >
            <ChevronRight :size="14" class="chev" :class="{ down: open === p.id }" />
            <span
              class="dot"
              :class="{ online: p.home }"
              data-test="home-dot"
              :title="p.home ? t('people.home') : t('people.away')"
            />
            <strong class="name">{{ p.name }}</strong>
            <span class="muted small">{{
              t('people.devices', { n: p.devices.length }, p.devices.length)
            }}</span>
          </button>
          <span v-if="deleting === p.id" class="confirm" data-test="delete-confirm">
            <span>{{ t('people.confirmDelete', { name: p.name }) }}</span>
            <button class="btn small ghost" type="button" @click="deleting = undefined">
              {{ t('changes.cancel') }}
            </button>
            <button
              class="btn small danger"
              type="button"
              data-test="confirm-delete"
              @click="remove(p)"
            >
              {{ t('people.delete') }}
            </button>
          </span>
          <span v-else-if="renaming !== p.id" class="tools">
            <button
              class="btn ghost icon small"
              type="button"
              data-test="rename"
              :aria-label="t('people.rename')"
              :title="t('people.rename')"
              @click="startRename(p)"
            >
              <Pencil :size="14" />
            </button>
            <button
              class="btn ghost icon small"
              type="button"
              data-test="delete"
              :aria-label="t('people.delete')"
              :title="t('people.delete')"
              @click="deleting = p.id"
            >
              <Trash2 :size="14" />
            </button>
          </span>
        </div>

        <div v-if="open === p.id" class="detail" data-test="person-detail">
          <p v-if="!p.devices.length" class="muted small">
            {{ t('people.noDevices', { name: p.name }) }}
          </p>
          <ul v-else class="devices">
            <li v-for="id in p.devices" :key="id">
              <button
                class="link device"
                type="button"
                data-test="person-device"
                @click="emit('select', id)"
              >
                <span class="dot" :class="{ online: inventory.get(id)?.online }" />
                <DeviceIcon v-if="inventory.get(id)" :device="inventory.get(id)!" :size="14" />
                {{ deviceName(id) }}
              </button>
              <Pause
                v-if="paused.has(id)"
                :size="12"
                class="muted"
                :aria-label="t('host.paused')"
              />
            </li>
          </ul>
          <div v-if="p.devices.length" class="actions">
            <span v-if="asking" class="confirm" data-test="pause-person-confirm">
              <span>{{ t('people.confirmPause', { name: p.name }) }}</span>
              <button
                class="btn small ghost"
                type="button"
                :disabled="busy"
                @click="asking = false"
              >
                {{ t('changes.cancel') }}
              </button>
              <button
                class="btn small primary"
                type="button"
                data-test="confirm-pause-person"
                :disabled="busy"
                @click="pauseOrResume(p, true)"
              >
                {{ busy ? t('changes.applying') : t('people.pause') }}
              </button>
            </span>
            <button
              v-else-if="anyNotPaused(p)"
              class="btn small"
              type="button"
              data-test="pause-person"
              :disabled="busy"
              :title="t('people.pauseHint')"
              @click="asking = true"
            >
              <Pause :size="14" />{{ t('people.pause') }}
            </button>
            <button
              v-if="anyPaused(p) && !asking"
              class="btn small warn"
              type="button"
              data-test="resume-person"
              :disabled="busy"
              @click="pauseOrResume(p, false)"
            >
              <Play :size="14" />{{ t('people.resume') }}
            </button>
          </div>
          <ul v-if="results" class="results" data-test="person-results" role="status">
            <li v-for="r in results" :key="r.node_id" :class="r.status">
              {{ deviceName(r.node_id) }}: {{ t(`people.status.${r.status}`) }}
              <small v-if="r.error && r.status === 'failed'" class="muted">({{ r.error }})</small>
            </li>
          </ul>
        </div>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.add,
.rename {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
.rename {
  flex: 1;
  margin: 0;
}
.add .input,
.rename .input {
  flex: 1;
  max-width: 320px;
}
.list,
.devices,
.results {
  margin: 0;
  padding: 0;
  list-style: none;
}
.person {
  border-bottom: 1px solid var(--border);
}
.row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 40px;
}
.open {
  display: flex;
  flex: 1;
  align-items: center;
  gap: 8px;
  padding: 8px 0;
  border: 0;
  background: none;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.chev {
  color: var(--text-muted);
  transition: transform 0.15s;
}
.chev.down {
  transform: rotate(90deg);
}
.small {
  font-size: 12.5px;
}
.tools {
  display: inline-flex;
  gap: 2px;
}
.detail {
  padding: 0 0 12px 22px;
}
.devices li {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 3px 0;
}
.device {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
}
.confirm {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-sm);
  background: var(--surface);
  font-size: 12.5px;
}
.results {
  margin-top: 8px;
  font-size: 12.5px;
}
.results .done {
  color: var(--ok);
}
.results .failed {
  color: var(--danger);
}
.results .skipped {
  color: var(--text-muted);
}
</style>
