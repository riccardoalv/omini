<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'

/** The longest note, in characters (the server's NoteMax). */
const NOTE_MAX = 1000

const props = defineProps<{ nodeId: string; note?: string }>()
const emit = defineEmits<{ saved: [note: string]; error: [message: string] }>()
const { t } = useI18n()

const text = ref(props.note ?? '')
const saving = ref(false)
// Another device opened in the panel, or the note changed elsewhere.
watch(
  () => [props.nodeId, props.note] as const,
  () => (text.value = props.note ?? ''),
)
const left = computed(() => NOTE_MAX - [...text.value].length)

async function save() {
  const note = text.value.trim()
  if (note === (props.note ?? '').trim() || saving.value) return
  saving.value = true
  try {
    await api.updateInventory(props.nodeId, { note })
    emit('saved', note)
  } catch (e) {
    emit('error', e instanceof ApiError ? e.message : t('common.error'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="note">
    <textarea
      v-model="text"
      class="input"
      rows="2"
      data-test="note"
      :maxlength="NOTE_MAX"
      :placeholder="t('panel.notePlaceholder')"
      :aria-label="t('panel.note')"
      @blur="save"
    />
    <span v-if="left < 100" class="left muted" data-test="note-left">
      {{ t('panel.noteLeft', left) }}
    </span>
  </div>
</template>

<style scoped>
.note {
  margin-bottom: 12px;
}
textarea {
  /* .input is sized for one line (36px, no vertical padding): a note has room for two. */
  width: 100%;
  height: auto;
  min-height: calc(2 * 1.45em + 18px);
  padding: 8px 10px;
  resize: vertical;
  font: inherit;
  font-size: 13px;
  line-height: 1.45;
}
.left {
  display: block;
  margin-top: 2px;
  font-size: 11px;
  text-align: right;
}
</style>
