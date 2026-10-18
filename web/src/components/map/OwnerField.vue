<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import type { Person } from '@/lib/types'

/**
 * Who owns the device: one of the people, nobody, or a new person named here
 * (created and picked at once). Saved as soon as it is picked.
 */
const props = defineProps<{ nodeId: string; ownerId?: number }>()
const emit = defineEmits<{ saved: [ownerId: number | undefined]; error: [message: string] }>()
const { t } = useI18n()

const NEW = 'new'
const people = ref<Person[]>([])
const picked = ref(String(props.ownerId ?? ''))
const naming = ref(false)
const newName = ref('')
const saving = ref(false)

onMounted(async () => {
  try {
    people.value = await api.people()
  } catch {
    // the select still offers "Nobody" and "New person…"
  }
})
watch(
  () => [props.nodeId, props.ownerId] as const,
  () => {
    picked.value = String(props.ownerId ?? '')
    naming.value = false
  },
)

async function save(ownerId: number | undefined) {
  saving.value = true
  try {
    await api.updateInventory(props.nodeId, { owner_id: ownerId ?? null })
    emit('saved', ownerId)
  } catch (e) {
    picked.value = String(props.ownerId ?? '')
    emit('error', e instanceof ApiError ? e.message : t('common.error'))
  } finally {
    saving.value = false
  }
}

function onPick() {
  if (picked.value === NEW) {
    naming.value = true
    newName.value = ''
    return
  }
  naming.value = false
  const id = picked.value ? Number(picked.value) : undefined
  if (id !== props.ownerId) void save(id)
}

async function create() {
  const name = newName.value.trim()
  if (!name) return
  saving.value = true
  try {
    const p = await api.createPerson({ name })
    people.value = [...people.value, p].sort((a, b) => a.name.localeCompare(b.name))
    picked.value = String(p.id)
    naming.value = false
  } catch (e) {
    emit('error', e instanceof ApiError ? e.message : t('common.error'))
    return
  } finally {
    saving.value = false
  }
  await save(Number(picked.value))
}
function cancel() {
  naming.value = false
  picked.value = String(props.ownerId ?? '')
}
</script>

<template>
  <div class="owner">
    <label class="muted" :for="`owner-${nodeId}`">{{ t('people.owner') }}</label>
    <select
      :id="`owner-${nodeId}`"
      v-model="picked"
      class="input"
      data-test="owner"
      :disabled="saving"
      @change="onPick"
    >
      <option value="">{{ t('people.nobody') }}</option>
      <option v-for="p in people" :key="p.id" :value="String(p.id)">{{ p.name }}</option>
      <option :value="NEW">{{ t('people.newPerson') }}</option>
    </select>
    <form v-if="naming" class="new" @submit.prevent="create">
      <input
        v-model="newName"
        class="input"
        data-test="owner-new-name"
        maxlength="60"
        :placeholder="t('people.namePlaceholder')"
        :aria-label="t('people.namePlaceholder')"
      />
      <button
        class="btn small primary"
        type="submit"
        data-test="owner-create"
        :disabled="saving || !newName.trim()"
      >
        {{ t('people.save') }}
      </button>
      <button class="btn small ghost" type="button" @click="cancel">
        {{ t('changes.cancel') }}
      </button>
    </form>
  </div>
</template>

<style scoped>
.owner {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  font-size: 13px;
}
.owner select {
  min-width: 160px;
}
.new {
  display: flex;
  gap: 6px;
}
</style>
