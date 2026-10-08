import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { alertsState } from '@/lib/alerts'
import { api } from '@/lib/api'
import type { Alert, PresenceEvent } from '@/lib/types'

import { plugins } from '../../components/__tests__/helpers'
import AlertsView from '../AlertsView.vue'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      alerts: vi.fn<typeof mod.api.alerts>(),
      dismissAlert: vi.fn<typeof mod.api.dismissAlert>(),
      presence: vi.fn<typeof mod.api.presence>(),
      inventory: vi.fn<typeof mod.api.inventory>(),
    },
  }
})

const alert = (over: Partial<Alert>): Alert => ({
  id: 1,
  key: 'k',
  rule: 'high_cpu',
  severity: 'warning',
  node_id: 'dev:fw',
  params: { node: 'OPNsense', pct: 91 },
  opened_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
  dismissed: false,
  ...over,
})

const event = (over: Partial<PresenceEvent>): PresenceEvent => ({
  id: 1,
  node_id: 'mac:aa',
  kind: 'join',
  at: new Date().toISOString(),
  label: 'Phone',
  ...over,
})

function setup() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'map', component: { template: '<div />' } },
      { path: '/alerts', name: 'alerts', component: AlertsView },
    ],
  })
  return router
}

describe('AlertsView', () => {
  beforeEach(() => {
    alertsState.list = []
    alertsState.loaded = false
    vi.mocked(api.alerts).mockResolvedValue([
      alert({ id: 1 }),
      alert({
        id: 2,
        rule: 'disk_full',
        severity: 'critical',
        params: { node: 'NAS', mount: '/', pct: 95 },
      }),
      alert({
        id: 3,
        rule: 'unmanaged_switch',
        severity: 'info',
        node_id: 'seg:x',
        dismissed: true,
        params: { parent: 'sw', port: 'Port 5', macs: 4 },
      }),
    ])
    vi.mocked(api.presence).mockResolvedValue([
      event({ id: 3, kind: 'leave' }),
      event({ id: 2, kind: 'join', first: true, label: 'TV', node_id: 'mac:bb' }),
    ])
    vi.mocked(api.dismissAlert).mockResolvedValue()
    vi.mocked(api.inventory).mockResolvedValue([
      {
        id: 'mac:aa',
        kind: 'client',
        label: '192.168.1.10',
        alias: 'Kitchen tablet',
        pinned: false,
        hidden: false,
        first_seen: '2026-10-01T00:00:00Z',
        last_seen: '2026-10-07T00:00:00Z',
        online: false,
      },
    ])
  })

  it('lists open alerts, most severe first, hiding dismissed ones', async () => {
    const router = setup()
    await router.push('/alerts')
    const w = mount(AlertsView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    const rows = w.findAll('[data-test=alert]')
    expect(rows.map((r) => r.text())).toEqual([
      expect.stringContaining('Disk almost full on NAS'),
      expect.stringContaining('High CPU on OPNsense'),
    ])
    await w.get('[data-test=sev-critical]').trigger('click')
    expect(w.findAll('[data-test=alert]')).toHaveLength(1)
    w.unmount()
  })

  it('dismisses an alert and opens a node on the map', async () => {
    const router = setup()
    await router.push('/alerts')
    const w = mount(AlertsView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    await w.findAll('[data-test=alert-dismiss]')[1]!.trigger('click')
    expect(api.dismissAlert).toHaveBeenCalledWith(1, true)
    await flushPromises()
    expect(w.findAll('[data-test=alert]')).toHaveLength(1)

    await w.get('[data-test=alert-map]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('map')
    expect(router.currentRoute.value.query.node).toBe('dev:fw')
    w.unmount()
  })

  it('shows the timeline by day, and only new devices when asked', async () => {
    const router = setup()
    await router.push('/alerts?tab=timeline')
    const w = mount(AlertsView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    const rows = w.findAll('[data-test=event]')
    expect(rows.map((r) => r.text())).toEqual([
      expect.stringContaining('Kitchen tablet left the network'),
      expect.stringContaining('TV seen for the first time'),
    ])
    expect(w.text()).toContain('Today')
    await w.get('[data-test=new-only]').setValue(true)
    await flushPromises()
    expect(api.presence).toHaveBeenLastCalledWith({ before: undefined, limit: 100, first: true })
    w.unmount()
  })
})
