import { mount } from '@vue/test-utils'
import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { alertsState } from '@/lib/alerts'
import { api } from '@/lib/api'
import { prefs } from '@/lib/prefs'
import { features, noteIntegrations } from '@/lib/features'
import type { Alert, Integration } from '@/lib/types'

import AppShell from '../AppShell.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      alerts: vi.fn<typeof mod.api.alerts>(),
      integrations: vi.fn<typeof mod.api.integrations>(),
    },
  }
})

const alert = (id: number, severity: Alert['severity'], dismissed = false): Alert => ({
  id,
  key: `k${id}`,
  rule: 'high_cpu',
  severity,
  params: {},
  opened_at: '2026-10-07T10:00:00Z',
  updated_at: '2026-10-07T10:00:00Z',
  dismissed,
})

const flows = (enabled: boolean): Integration => ({
  id: 7,
  name: 'Traffic flows',
  type: 'flows',
  config: {},
  enabled,
  created_at: '2026-10-07T10:00:00Z',
  updated_at: '2026-10-07T10:00:00Z',
})

const router = createRouter({
  history: createMemoryHistory(),
  routes: ['/', '/alerts', '/flows', '/integrations', '/settings'].map((path) => ({
    path,
    component: { template: '<div />' },
  })),
})

describe('AppShell', () => {
  beforeEach(() => {
    prefs.sidebarExpanded = false
    alertsState.list = []
    vi.mocked(api.alerts).mockResolvedValue([])
    vi.mocked(api.integrations).mockResolvedValue([])
    features.flows = false
  })

  it('expands and collapses the sidebar, remembering the choice', async () => {
    const w = mount(AppShell, { global: { plugins: [...plugins(), router] } })
    const nav = w.get('nav')
    const toggle = w.get('[data-test=sidebar-toggle]')
    expect(nav.classes()).not.toContain('expanded')
    expect(toggle.attributes('aria-expanded')).toBe('false')

    await toggle.trigger('click')
    expect(nav.classes()).toContain('expanded')
    expect(prefs.sidebarExpanded).toBe(true)
    expect(toggle.text()).toBe('Collapse menu')

    await toggle.trigger('click')
    expect(nav.classes()).not.toContain('expanded')
  })

  it('links to every screen; Flows only while the flows integration is on', async () => {
    const links = () => w.findAll('a').map((a) => a.attributes('href'))
    let w = mount(AppShell, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    expect(links()).toEqual(['/', '/alerts', '/integrations', '/settings'])
    w.unmount()

    vi.mocked(api.integrations).mockResolvedValue([flows(true)])
    w = mount(AppShell, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    expect(links()).toEqual(['/', '/alerts', '/flows', '/integrations', '/settings'])

    // Turned off: the entry goes away.
    noteIntegrations([flows(false)])
    await flushPromises()
    expect(links()).not.toContain('/flows')
    w.unmount()
  })

  it('counts the alerts that ask for attention on the Alerts entry', async () => {
    vi.mocked(api.alerts).mockResolvedValue([
      alert(1, 'critical'),
      alert(2, 'warning'),
      alert(3, 'warning', true), // dismissed
      alert(4, 'info'), // informative only
    ])
    const w = mount(AppShell, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    expect(w.get('[data-test=alerts-badge]').text()).toBe('2')
    w.unmount()
  })
})
