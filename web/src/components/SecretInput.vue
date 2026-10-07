<script setup lang="ts">
import { Eye, EyeOff } from 'lucide-vue-next'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

defineOptions({ inheritAttrs: false })
const model = defineModel<string | number | boolean>()
const { t } = useI18n()
const visible = ref(false)
</script>

<template>
  <div class="secret">
    <input
      v-bind="$attrs"
      v-model="model"
      class="input"
      :type="visible ? 'text' : 'password'"
      autocomplete="new-password"
      spellcheck="false"
    />
    <button
      type="button"
      class="reveal"
      :aria-label="visible ? t('common.hide') : t('common.show')"
      :title="visible ? t('common.hide') : t('common.show')"
      :aria-pressed="visible"
      @click="visible = !visible"
    >
      <component :is="visible ? EyeOff : Eye" :size="16" />
    </button>
  </div>
</template>

<style scoped>
.secret {
  position: relative;
  display: flex;
  align-items: center;
}
.secret .input {
  padding-right: 40px;
}
.reveal {
  position: absolute;
  right: 4px;
  display: grid;
  place-items: center;
  width: 30px;
  height: 28px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}
.reveal:hover {
  background: var(--surface-hover);
  color: var(--text);
}
.reveal:focus-visible {
  outline: 2px solid var(--accent);
}
</style>
