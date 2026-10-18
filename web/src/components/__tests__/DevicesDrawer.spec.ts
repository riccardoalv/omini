import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import { prefs } from '@/lib/prefs'

import DevicesDrawer from '../map/DevicesDrawer.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      inventory: vi.fn<typeof mod.api.inventory>().mockResolvedValue([]),
      vlans: vi.fn<typeof mod.api.vlans>().mockResolvedValue([]),
      people: vi
        .fn<typeof mod.api.people>()
        .mockResolvedValue([{ id: 1, name: 'Ana', created_at: '', devices: [], home: true }]),
      topology: vi.fn<typeof mod.api.topology>().mockResolvedValue({
        topology: { nodes: [], edges: [] },
        statuses: [],
        generated_at: '',
        layout: {},
      }),
    },
  }
})

const props = {
  summary: { devices: 3, clients: 7 },
  networkGroups: [
    {
      key: 'subnet:192.168.1.0/24',
      name: 'LAN · 192.168.1.0/24',
      label: 'LAN',
      cidr: '192.168.1.0/24',
      members: new Set(['a']),
    },
  ],
  colors: new Map<string, string>(),
  offlineCount: 2,
  hiddenCount: 1,
  showHidden: false,
}
const mountDrawer = (extra = {}) =>
  mount(DevicesDrawer, { props: { ...props, ...extra }, global: { plugins: plugins() } })

describe('DevicesDrawer', () => {
  it('has the devices, people, VLANs and DNS & DHCP tabs, and opens on the devices', () => {
    const w = mountDrawer()
    expect(w.findAll('[role=tab]').map((t) => t.text())).toEqual([
      'Devices',
      'People',
      'VLANs',
      'DNS & DHCP',
    ])
    expect(w.text()).toContain('3 devices')
    expect(w.text()).toContain('7 clients online')
  })

  it('switches offline and hidden devices from the devices tab', async () => {
    prefs.hideOffline = false
    const w = mountDrawer()
    await w.get('[data-test=toggle-offline]').trigger('click')
    expect(prefs.hideOffline).toBe(true)
    await w.get('[data-test=toggle-hidden]').trigger('click')
    expect(w.emitted('update:showHidden')).toEqual([[true]])
    prefs.hideOffline = false
    const none = mountDrawer({ hiddenCount: 0 })
    expect(none.find('[data-test=toggle-hidden]').exists()).toBe(false)
  })

  it('picks a network from the VLANs tab and steps aside', async () => {
    const w = mountDrawer({ tab: 'vlans' })
    await flushPromises()
    await w.get('[data-net="subnet:192.168.1.0/24"]').trigger('click')
    expect(w.emitted('update:netFilter')).toEqual([['subnet:192.168.1.0/24']])
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('lists the people in the people tab', async () => {
    const w = mountDrawer({ tab: 'people' })
    await flushPromises()
    expect(w.get('[data-test=people-tab]').text()).toContain('Ana')
  })
})
