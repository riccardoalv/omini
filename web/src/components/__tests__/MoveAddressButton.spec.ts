import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { alertsState } from '@/lib/alerts'
import { api } from '@/lib/api'
import type { Alert, Integration } from '@/lib/types'

import AlertToasts from '../AlertToasts.vue'
import MoveAddressButton from '../MoveAddressButton.vue'
import IntegrationsView from '../../views/IntegrationsView.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      alerts: vi.fn<typeof mod.api.alerts>(),
      moveIntegration: vi.fn<typeof mod.api.moveIntegration>(),
      integrations: vi.fn<typeof mod.api.integrations>(),
      integrationTypes: vi.fn<typeof mod.api.integrationTypes>(),
      pluginCatalog: vi.fn<typeof mod.api.pluginCatalog>(),
      plugins: vi.fn<typeof mod.api.plugins>(),
      pluginUpdates: vi.fn<typeof mod.api.pluginUpdates>(),
    },
  }
})

const offline: Alert = {
  id: 7,
  key: 'device_offline|dev:sw',
  rule: 'device_offline',
  severity: 'critical',
  node_id: 'dev:sw',
  params: {
    node: 'Horaco switch',
    error: 'cannot connect to http://192.168.1.96: No route to host',
    moved_from: '192.168.1.96',
    moved_to: '192.168.1.110',
    integration_id: 3,
  },
  opened_at: '2026-10-24T03:00:00Z',
  updated_at: '2026-10-24T03:00:00Z',
  dismissed: false,
}

const horaco: Integration = {
  id: 3,
  name: 'Horaco',
  type: 'horaco',
  config: { host: '192.168.1.96' },
  enabled: true,
  created_at: '2026-10-06T00:00:00Z',
  updated_at: '2026-10-06T00:00:00Z',
  status: {
    integration_id: 3,
    ok: false,
    error: 'cannot connect to http://192.168.1.96: No route to host',
    collected_at: '2026-10-24T03:00:00Z',
    duration_ms: 3,
    devices: 0,
  },
}

describe('MoveAddressButton', () => {
  beforeEach(() => {
    vi.mocked(api.alerts).mockResolvedValue([])
    vi.mocked(api.moveIntegration).mockReset()
  })
  afterEach(() => {
    vi.restoreAllMocks()
    alertsState.list = []
  })

  it('asks once, then points the integration at the new address', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const w = mount(MoveAddressButton, {
      props: { alert: offline },
      global: { plugins: plugins() },
    })
    const button = w.get('[data-test=move-address]')
    expect(button.text()).toBe('Use 192.168.1.110')

    await button.trigger('click')
    expect(confirm).toHaveBeenCalledWith(
      "Change Horaco switch's address from 192.168.1.96 to 192.168.1.110?",
    )
    expect(api.moveIntegration).not.toHaveBeenCalled()

    confirm.mockReturnValue(true)
    vi.mocked(api.moveIntegration).mockResolvedValue({
      integration: { ...horaco, config: { host: '192.168.1.110' } },
      status: { ...horaco.status!, ok: true, error: undefined, devices: 1 },
    })
    await w.get('[data-test=move-address]').trigger('click')
    await flushPromises()
    expect(api.moveIntegration).toHaveBeenCalledWith(3, '192.168.1.110')
    expect(w.get('[data-test=move-result]').text()).toBe('Done: it answers at 192.168.1.110.')
    expect(w.emitted('moved')).toHaveLength(1)
    expect(w.find('[data-test=move-address]').exists()).toBe(false)
  })

  it('says when the integration still fails at the new address', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    vi.mocked(api.moveIntegration).mockResolvedValue({
      integration: horaco,
      status: { ...horaco.status!, error: 'login refused' },
    })
    const w = mount(MoveAddressButton, {
      props: { alert: offline },
      global: { plugins: plugins() },
    })
    await w.get('[data-test=move-address]').trigger('click')
    await flushPromises()
    expect(w.get('[data-test=move-result]').text()).toBe('Saved, but it still fails: login refused')
    // It can be tried again.
    expect(w.find('[data-test=move-address]').exists()).toBe(true)
  })

  it('shows nothing for an alert of a device that did not move', () => {
    const plain = { ...offline, params: { node: 'NAS', error: 'timeout' } }
    const w = mount(MoveAddressButton, { props: { alert: plain }, global: { plugins: plugins() } })
    expect(w.find('[data-test=move-address]').exists()).toBe(false)
  })

  it('is on the popup, with the tip to update the integration', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    })
    await router.push('/')
    localStorage.clear()
    alertsState.list = [offline]
    const w = mount(AlertToasts, { global: { plugins: [...plugins(), router] } })
    const toast = w.get('[data-test=toast]')
    expect(toast.text()).toContain('it answers at 192.168.1.110 now')
    expect(toast.text()).toContain('press "Use 192.168.1.110"')
    expect(toast.find('[data-test=move-address]').exists()).toBe(true)
  })

  it('is on the failing integration of the Integrations screen', async () => {
    vi.mocked(api.integrations).mockResolvedValue([horaco])
    vi.mocked(api.integrationTypes).mockResolvedValue([])
    vi.mocked(api.pluginCatalog).mockResolvedValue([])
    vi.mocked(api.plugins).mockResolvedValue([])
    vi.mocked(api.pluginUpdates).mockResolvedValue([])
    alertsState.list = [offline]
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: IntegrationsView }],
    })
    const w = mount(IntegrationsView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    const row = w.get('[data-test=integration-moved]')
    expect(row.text()).toContain('Answers at 192.168.1.110 now')
    expect(row.get('[data-test=move-address]').text()).toBe('Use 192.168.1.110')
    // Pressing it does not open the row.
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    await row.get('[data-test=move-address]').trigger('click')
    expect(w.find('[role=button]').attributes('aria-expanded')).toBe('false')
  })
})
