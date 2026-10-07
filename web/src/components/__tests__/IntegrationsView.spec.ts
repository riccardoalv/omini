import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { api } from '@/lib/api'
import type { Integration } from '@/lib/types'

import IntegrationsView from '../../views/IntegrationsView.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      integrations: vi.fn<typeof mod.api.integrations>(),
      integrationTypes: vi.fn<typeof mod.api.integrationTypes>(),
      updateIntegration: vi.fn<typeof mod.api.updateIntegration>(),
      pluginCatalog: vi.fn<typeof mod.api.pluginCatalog>(),
      installPlugin: vi.fn<typeof mod.api.installPlugin>(),
      plugins: vi.fn<typeof mod.api.plugins>(),
    },
  }
})

const demo: Integration = {
  id: 1,
  name: 'Demo network',
  type: 'demo',
  config: {},
  enabled: true,
  created_at: '2026-10-06T00:00:00Z',
  updated_at: '2026-10-06T00:00:00Z',
  status: {
    integration_id: 1,
    ok: true,
    collected_at: '2026-10-06T00:00:00Z',
    duration_ms: 3,
    devices: 3,
  },
}

describe('IntegrationsView', () => {
  beforeEach(() => {
    vi.mocked(api.integrations).mockResolvedValue([demo])
    vi.mocked(api.integrationTypes).mockResolvedValue([])
    vi.mocked(api.pluginCatalog).mockResolvedValue([])
    vi.mocked(api.plugins).mockResolvedValue([])
  })

  it('enables and disables integrations with a switch', async () => {
    vi.mocked(api.updateIntegration).mockResolvedValue({
      ...demo,
      enabled: false,
      status: undefined,
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: IntegrationsView }],
    })
    const w = mount(IntegrationsView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()

    expect(w.find('input[type=checkbox]').exists()).toBe(false)
    const sw = w.get('[role=switch]')
    expect(sw.attributes('aria-checked')).toBe('true')

    await sw.trigger('click')
    await flushPromises()
    expect(api.updateIntegration).toHaveBeenCalledWith(1, { enabled: false })
    expect(w.get('[role=switch]').attributes('aria-checked')).toBe('false')
    expect(w.text()).toContain('Disabled')
  })

  it('"Add integration" opens the store, and picking an integration opens its form', async () => {
    const opnsense = {
      type: 'opnsense',
      name: 'OPNsense',
      kind: 'plugin' as const,
      fields: [{ key: 'url', type: 'url' as const, label: 'Address', required: true }],
    }
    vi.mocked(api.integrationTypes).mockResolvedValue([opnsense])
    vi.mocked(api.pluginCatalog).mockResolvedValue([
      {
        id: 'opnsense',
        name: 'OPNsense',
        description: 'OPNsense firewall',
        url: 'https://github.com/riccardoalv/omini-plugin-opnsense',
        publisher: 'official',
        trust: 'experimental',
        installed: true,
      },
    ])
    vi.mocked(api.plugins).mockResolvedValue([
      {
        manifest: { id: 'opnsense', name: 'OPNsense', version: '0.1.0' },
        dev: false,
        publisher: 'official',
        trust: 'experimental',
      },
    ])
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: IntegrationsView }],
    })
    const w = mount(IntegrationsView, {
      global: { plugins: [...plugins(), router] },
      attachTo: document.body,
    })
    await flushPromises()

    // "Add integration" opens the store.
    await w.get('[data-test=add]').trigger('click')
    await flushPromises()
    await w.get('[data-test=store-use]').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Address')
    w.unmount()
  })
})
