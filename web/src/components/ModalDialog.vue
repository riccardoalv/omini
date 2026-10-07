<script setup lang="ts">
import { X } from 'lucide-vue-next'
import { onBeforeUnmount, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'

defineProps<{ title: string; wide?: boolean; xl?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="backdrop" @mousedown.self="emit('close')">
    <section
      class="card modal"
      :class="{ wide, xl }"
      role="dialog"
      aria-modal="true"
      :aria-label="title"
    >
      <header>
        <h2>{{ title }}</h2>
        <button
          class="btn ghost icon"
          type="button"
          :aria-label="t('common.close')"
          @click="emit('close')"
        >
          <X :size="18" />
        </button>
      </header>
      <div class="body">
        <slot />
      </div>
      <footer v-if="$slots.footer">
        <slot name="footer" />
      </footer>
    </section>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: grid;
  place-items: center;
  padding: 16px;
  background: rgb(0 0 0 / 0.5);
}
.modal {
  width: 100%;
  max-width: 480px;
  max-height: calc(100vh - 32px);
  display: flex;
  flex-direction: column;
  box-shadow: var(--shadow);
}
.modal.wide {
  max-width: 680px;
}
.modal.xl {
  max-width: 960px;
  height: min(720px, calc(100vh - 32px));
}
header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 10px 20px;
}
h2 {
  font-size: 17px;
}
.body {
  padding: 6px 20px 16px;
  overflow: auto;
}
footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 20px;
  border-top: 1px solid var(--border);
}
</style>
