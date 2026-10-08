import { reactive } from 'vue'

import type { Integration } from './types'

/**
 * Screens that depend on an integration being on: the menu shows them only
 * then. Kept up to date by every list of integrations the UI fetches (see
 * `api.integrations`).
 */
export const features = reactive({ flows: false })

export function noteIntegrations(list: Integration[]) {
  features.flows = list.some((i) => i.type === 'flows' && i.enabled)
}
