import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { Integration, IntegrationType } from '@/lib/types'

import IntegrationDetails from '../IntegrationDetails.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      runIntegration: vi.fn<typeof mod.api.runIntegration>(),
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
  beforeEach(() => {
    vi.mocked(api.runIntegration).mockReset()
  })

  it('runs the integration and shows the result', async () => {
    const status = {
      integration_id: 4,
      ok: true,
      collected_at: '2026-10-06T00:00:00Z',
      duration_ms: 6800,
      devices: 18,
    }
    vi.mocked(api.runIntegration).mockResolvedValue(status)
    const w = mountDetails()
    await w.get('[data-test=run-now]').trigger('click')
    await flushPromises()

    expect(api.runIntegration).toHaveBeenCalledWith(4)
    expect(w.get('[role=status]').text()).toBe('Done: 18 devices in 6.8 s.')
    expect(w.emitted('ran')![0]).toEqual([status])
  })

  it('shows the error when the run fails', async () => {
    const { ApiError } = await import('@/lib/api')
    vi.mocked(api.runIntegration).mockRejectedValue(new ApiError(409, 'integration is disabled'))
    const w = mountDetails()
    await w.get('[data-test=run-now]').trigger('click')
    await flushPromises()
    expect(w.get('[role=alert]').text()).toBe('integration is disabled')
  })

  it('cannot run a disabled integration', () => {
    const w = mountDetails({ ...demo, enabled: false })
    expect(w.get('[data-test=run-now]').attributes('disabled')).toBeDefined()
  })
})

describe('IntegrationDetails interval', () => {
  it('saves the collection interval as soon as it changes', async () => {
    vi.mocked(api.updateIntegration).mockResolvedValue({ ...demo, interval_s: 300 })
    const w = mountDetails()
    const select = w.get('[data-test=interval]')
    expect((select.element as HTMLSelectElement).value).toBe('0')
    expect(select.text()).toContain('Default (1 minute)')
    expect(select.text()).toContain('5 minutes')
    await select.setValue('300')
    await flushPromises()
    expect(api.updateIntegration).toHaveBeenCalledWith(4, { interval_s: 300 })
    expect(w.emitted('saved')![0]).toEqual([{ ...demo, interval_s: 300 }])
  })
})
