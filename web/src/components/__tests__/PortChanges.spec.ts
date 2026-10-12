import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '@/lib/api'
import type { Interface, TopoNode } from '@/lib/types'

import NodePanel from '../map/NodePanel.vue'
import PortPanel from '../map/PortPanel.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      applyChange: vi.fn<typeof mod.api.applyChange>(),
      configChanges: vi.fn<typeof mod.api.configChanges>(),
      changeSettings: vi.fn<typeof mod.api.changeSettings>(),
      webServices: vi.fn<typeof mod.api.webServices>(),
      presence: vi.fn<typeof mod.api.presence>(),
      flows: vi.fn<typeof mod.api.flows>(),
    },
  }
})

const phone: TopoNode = { id: 'mac:92', kind: 'client', label: 'NixOS-Laptop', online: true }
const ports: Interface[] = [
  { name: 'Port 5', up: true, speed_mbps: 1000, enabled: true },
  { name: 'Port 9', up: true, speed_mbps: 10000, enabled: true },
  { name: 'Port 2', up: false, enabled: false },
]

const mountPanel = (props: Partial<InstanceType<typeof PortPanel>['$props']> = {}) =>
  mount(PortPanel, {
    props: {
      ports,
      links: { 'Port 5': phone },
      nodeId: 'dev:sw',
      changes: ['port.enabled'],
      guards: { 'Port 9': "It is this device's link to the rest of the network (OPNsense)." },
      ...props,
    },
    global: { plugins: plugins() },
  })

beforeEach(() => {
  vi.mocked(api.applyChange).mockReset()
  vi.mocked(api.applyChange).mockResolvedValue({
    id: 1,
    node_id: 'dev:sw',
    kind: 'port.enabled',
    after: false,
    at: '2026-10-10T12:00:00Z',
  })
})

describe('Enabling and disabling a port', () => {
  it('asks once, then applies and tells the map to reload', async () => {
    const w = mountPanel()
    await w.get('[data-port="Port 5"]').trigger('click')
    await w.get('[data-test=toggle-port]').trigger('click')
    expect(w.get('.confirm').text()).toContain('Disable Port 5 (NixOS-Laptop)?')
    expect(api.applyChange).not.toHaveBeenCalled()
    await w.get('[data-test=confirm-toggle]').trigger('click')
    await flushPromises()
    expect(api.applyChange).toHaveBeenCalledWith('dev:sw', {
      kind: 'port.enabled',
      port: 'Port 5',
      value: false,
      confirm: false,
    })
    expect(w.emitted('applied')).toHaveLength(1)
    expect(w.find('.confirm').exists()).toBe(false)
  })

  it('shows a disabled port struck through, and offers to enable it', async () => {
    const w = mountPanel()
    expect(w.get('[data-port="Port 2"]').classes()).toContain('off')
    expect(w.find('[data-port="Port 2"] .strike').exists()).toBe(true)
    await w.get('[data-port="Port 2"]').trigger('click')
    expect(w.get('[data-test=port-card]').text()).toContain('Disabled')
    expect(w.get('[data-test=toggle-port]').text()).toBe('Enable port')
    await w.get('[data-test=toggle-port]').trigger('click')
    await w.get('[data-test=confirm-toggle]').trigger('click')
    await flushPromises()
    expect(vi.mocked(api.applyChange).mock.calls[0]![1]).toMatchObject({ value: true })
  })

  it('blocks a destructive change, with the reason', async () => {
    const w = mountPanel()
    await w.get('[data-port="Port 9"]').trigger('click')
    expect(w.get('[data-test=toggle-port]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-test=blocked]').text()).toContain('rest of the network (OPNsense)')
  })

  it('asks a destructive change twice when allowed', async () => {
    const w = mountPanel({ allowDestructive: true })
    await w.get('[data-port="Port 9"]').trigger('click')
    expect(w.find('[data-test=blocked]').exists()).toBe(false)
    await w.get('[data-test=toggle-port]').trigger('click')
    await w.get('[data-test=confirm-toggle]').trigger('click')
    expect(api.applyChange).not.toHaveBeenCalled()
    expect(w.get('.confirm.danger').text()).toContain('destructive change')
    await w.get('[data-test=confirm-toggle]').trigger('click')
    await flushPromises()
    expect(vi.mocked(api.applyChange).mock.calls[0]![1]).toMatchObject({
      port: 'Port 9',
      confirm: true,
    })
  })

  it('shows why the device refused', async () => {
    vi.mocked(api.applyChange).mockRejectedValue(new ApiError(502, 'the switch did not answer'))
    const w = mountPanel()
    await w.get('[data-port="Port 5"]').trigger('click')
    await w.get('[data-test=toggle-port]').trigger('click')
    await w.get('[data-test=confirm-toggle]').trigger('click')
    await flushPromises()
    expect(w.get('[role=alert]').text()).toBe('the switch did not answer')
    expect(w.emitted('applied')).toBeUndefined()
  })

  it('has no button on a port that does not report its state', async () => {
    const w = mountPanel({ ports: [...ports, { name: 'Port 8', up: true }] })
    await w.get('[data-port="Port 8"]').trigger('click')
    expect(w.find('[data-test=toggle-port]').exists()).toBe(false)
  })

  it('has no button when the device cannot change', async () => {
    const w = mountPanel({ changes: undefined })
    await w.get('[data-port="Port 5"]').trigger('click')
    expect(w.find('[data-test=toggle-port]').exists()).toBe(false)
  })
})

describe('Changes in the device panel', () => {
  it('lists the changes made from Omini, failed ones included', async () => {
    vi.mocked(api.webServices).mockResolvedValue([])
    vi.mocked(api.presence).mockResolvedValue([])
    vi.mocked(api.changeSettings).mockResolvedValue({ allow_destructive: false, dns_domain: '' })
    vi.mocked(api.configChanges).mockResolvedValue([
      {
        id: 3,
        node_id: 'dev:sw',
        kind: 'dns.set',
        after: { name: 'nas.lan', ip: '192.168.1.20' },
        at: '2026-10-10T12:10:00Z',
      },
      {
        id: 2,
        node_id: 'dev:sw',
        kind: 'port.enabled',
        port: 'Port 5',
        after: true,
        error: 'the switch did not answer',
        at: '2026-10-10T12:05:00Z',
      },
      {
        id: 1,
        node_id: 'dev:sw',
        kind: 'port.enabled',
        port: 'Port 5',
        before: true,
        after: false,
        at: '2026-10-10T12:00:00Z',
      },
    ])
    const sw: TopoNode = {
      id: 'dev:sw',
      kind: 'device',
      label: 'sw',
      online: true,
      changes: ['port.enabled'],
      device: { key: 'sw', name: 'sw', interfaces: ports },
    }
    const w = mount(NodePanel, {
      props: { node: sw, nodes: [sw], edges: [] },
      global: { plugins: plugins(), stubs: { RouterLink: true } },
    })
    await flushPromises()
    const items = w.findAll('[data-test=node-changes] li')
    expect(items).toHaveLength(3)
    expect(items[0]!.text()).toContain('DNS name nas.lan → 192.168.1.20')
    expect(items[1]!.text()).toContain('Port 5: enabled: not applied: the switch did not answer')
    expect(items[1]!.classes()).toContain('failed')
    expect(items[2]!.text()).toContain('Port 5: disabled')
  })
})
