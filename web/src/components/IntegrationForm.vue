<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import { useIntegrationForm } from '@/composables/useIntegrationForm'
import type { Config, Integration, IntegrationType } from '@/lib/types'

import IntegrationFields from './IntegrationFields.vue'
import ModalDialog from './ModalDialog.vue'

const props = defineProps<{
  type: IntegrationType
  /** Set when editing an existing integration. */
  existing?: Integration
  /** Prefilled values (e.g. from discovery). */
  prefill?: Config
  prefillName?: string
}>()
const emit = defineEmits<{ close: []; saved: [integration: Integration] }>()
const { t } = useI18n()

const { editing, name, values, busy, testing, error, result, missing, test, save } =
  useIntegrationForm(props.type, props.existing, props.prefill, props.prefillName)

async function submit() {
  const saved = await save()
  if (saved) emit('saved', saved)
}
</script>

<template>
  <ModalDialog :title="existing ? existing.name : type.name" @close="emit('close')">
    <form id="integration-form" @submit.prevent="submit">
      <p v-if="type.description" class="muted description">{{ type.description }}</p>

      <div class="field">
        <label for="f-name">{{ t('integrations.name') }}</label>
        <input id="f-name" v-model="name" class="input" required />
      </div>

      <IntegrationFields
        v-model="values"
        :type="type.type"
        :fields="type.fields"
        :editing="editing"
      />

      <p v-if="result" class="alert" :class="result.ok ? 'ok' : 'error'" role="status">
        {{ result.ok ? result.message : result.error }}
      </p>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    </form>

    <template #footer>
      <button class="btn" type="button" :disabled="testing || missing.length > 0" @click="test">
        {{ testing ? t('integrations.testing') : t('integrations.test') }}
      </button>
      <span class="spacer" />
      <button class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button
        class="btn primary"
        type="submit"
        form="integration-form"
        :disabled="busy || missing.length > 0"
      >
        {{ t('common.save') }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.description {
  margin: 0 0 16px;
}
.spacer {
  flex: 1;
}
.bool-field {
  display: flex;
  align-items: center;
  gap: 10px;
  font-weight: 500;
}
.alert {
  margin: 4px 0 0;
}
</style>
