import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import { initialValues, missingRequired, toConfig } from '@/lib/forms'
import type { Config, Integration, IntegrationType, TestResult } from '@/lib/types'

/** State and actions of an integration form, for creating or editing one. */
export function useIntegrationForm(
  type: IntegrationType,
  existing?: Integration,
  prefill?: Config,
) {
  const { t } = useI18n()
  const editing = !!existing
  const values = ref<Config>(initialValues(type.fields, { ...prefill, ...existing?.config }))
  const busy = ref(false)
  const testing = ref(false)
  const error = ref('')
  const result = ref<TestResult>()
  const missing = computed(() => missingRequired(type.fields, values.value, editing))

  const message = (e: unknown) => (e instanceof ApiError ? e.message : t('common.error'))

  async function test() {
    testing.value = true
    result.value = undefined
    try {
      result.value = await api.testIntegration({
        id: existing?.id,
        type: type.type,
        config: toConfig(type.fields, values.value, editing),
      })
    } catch (e) {
      result.value = { ok: false, error: message(e) }
    } finally {
      testing.value = false
    }
  }

  /** Saves and returns the integration, or undefined on error (shown in `error`). */
  async function save(): Promise<Integration | undefined> {
    busy.value = true
    error.value = ''
    try {
      const config = toConfig(type.fields, values.value, editing)
      return existing
        ? await api.updateIntegration(existing.id, { config })
        : await api.createIntegration({ type: type.type, config })
    } catch (e) {
      error.value = message(e)
      return undefined
    } finally {
      busy.value = false
    }
  }

  return { editing, values, busy, testing, error, result, missing, test, save }
}
