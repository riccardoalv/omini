<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import { fieldLabel, initialValues, inputType, missingRequired, toConfig } from '@/lib/forms'
import type { Config, Integration, IntegrationType, TestResult } from '@/lib/types'

import ModalDialog from './ModalDialog.vue'
import SecretInput from './SecretInput.vue'

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

const name = ref(props.existing?.name ?? props.prefillName ?? props.type.name)
const values = ref<Config>(
  initialValues(props.type.fields, { ...props.prefill, ...props.existing?.config }),
)
const busy = ref(false)
const testing = ref(false)
const error = ref('')
const result = ref<TestResult>()

const editing = !!props.existing
const missing = computed(() => missingRequired(props.type.fields, values.value, editing))

async function test() {
  testing.value = true
  result.value = undefined
  try {
    result.value = await api.testIntegration({
      id: props.existing?.id,
      type: props.type.type,
      config: toConfig(props.type.fields, values.value, editing),
    })
  } catch (e) {
    result.value = { ok: false, error: e instanceof ApiError ? e.message : t('common.error') }
  } finally {
    testing.value = false
  }
}

async function save() {
  busy.value = true
  error.value = ''
  try {
    const config = toConfig(props.type.fields, values.value, editing)
    const saved = props.existing
      ? await api.updateIntegration(props.existing.id, { name: name.value, config })
      : await api.createIntegration({ name: name.value, type: props.type.type, config })
    emit('saved', saved)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ModalDialog :title="existing ? existing.name : type.name" @close="emit('close')">
    <form id="integration-form" @submit.prevent="save">
      <p v-if="type.description" class="muted description">{{ type.description }}</p>

      <div class="field">
        <label for="f-name">{{ t('integrations.name') }}</label>
        <input id="f-name" v-model="name" class="input" required />
      </div>

      <div v-for="f in type.fields" :key="f.key" class="field">
        <label v-if="f.type === 'bool'" class="checkbox">
          <input v-model="values[f.key]" type="checkbox" :name="f.key" />
          {{ fieldLabel(f) }}
        </label>
        <template v-else>
          <label :for="`f-${f.key}`">{{ fieldLabel(f) }}<span v-if="f.required"> *</span></label>
          <select
            v-if="f.type === 'select'"
            :id="`f-${f.key}`"
            v-model="values[f.key]"
            class="select"
            :name="f.key"
          >
            <option v-for="o in f.options" :key="o" :value="o">{{ o }}</option>
          </select>
          <SecretInput
            v-else-if="f.type === 'secret'"
            :id="`f-${f.key}`"
            v-model="values[f.key]"
            :name="f.key"
            :required="f.required && !editing"
            :placeholder="editing ? t('integrations.secretSaved') : undefined"
          />
          <input
            v-else
            :id="`f-${f.key}`"
            v-model="values[f.key]"
            class="input"
            :name="f.key"
            :type="inputType(f)"
            :required="f.required"
            autocomplete="off"
          />
        </template>
        <span v-if="f.help" class="help">{{ f.help }}</span>
      </div>

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
.alert {
  margin: 4px 0 0;
}
</style>
