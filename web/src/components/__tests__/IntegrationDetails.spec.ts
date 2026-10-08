import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import type { Integration, IntegrationType } from '@/lib/types'

import IntegrationDetails from '../IntegrationDetails.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      updateIntegration: vi.fn<typeof mod.api.updateIntegration>(),
      topology: vi.fn<typeof mod.api.topology>(),
    },
  }
})

const type: IntegrationType = { type: 'demo', name: 'Demo network', kind: 'core', fields: [] }
const demo: Integration = {
  id: 4,
  name: 'Demo network',
  type: 'demo',
  config: {},
  enabled: true,
  created_at: '2026-10-06T00:00:00Z',
  updated_at: '2026-10-06T00:00:00Z',
}

const mountDetails = (integration = demo) =>
  mount(IntegrationDetails, { props: { integration, type }, global: { plugins: plugins() } })

describe('IntegrationDetails run now', () => {
  it('has no button of its own: a whole round runs from the Integrations screen', () => {
    expect(mountDetails().find('[data-test=run-now]').exists()).toBe(false)
  })
})

describe('IntegrationDetails interval', () => {
  it('has no interval of its own: collection runs in rounds', () => {
    expect(mountDetails().find('[data-test=interval]').exists()).toBe(false)
  })
})
