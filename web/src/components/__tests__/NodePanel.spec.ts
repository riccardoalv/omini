import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { TopoNode } from '@/lib/types'

import NodePanel from '../map/NodePanel.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      webServices: vi.fn<typeof mod.api.webServices>(),
      updateInventory: vi.fn<typeof mod.api.updateInventory>(),
      deleteInventory: vi.fn<typeof mod.api.deleteInventory>(),
      setPortLabel: vi.fn<typeof mod.api.setPortLabel>(),
      scanNode: vi.fn<typeof mod.api.scanNode>(),
    },
  }
})

const firewall: TopoNode = {
  id: 'dev:00:e0:4c:68:00:02',
  kind: 'device',
  label: 'opnsense',
  role: 'firewall',
  online: true,
  ip: '192.168.1.1',
}

const mountPanel = (node: TopoNode) =>
  mount(NodePanel, {
    props: { node, nodes: [node], edges: [], expandedParent: false },
    global: { plugins: plugins(), stubs: { RouterLink: true } },
  })

describe('NodePanel web interface', () => {
  beforeEach(() => {
    vi.mocked(api.webServices).mockReset()
  })

  it('offers the web interface as an icon next to the name', async () => {
    vi.mocked(api.webServices).mockResolvedValue([
      { url: 'https://192.168.1.1/', port: 443, title: 'OPNsense' },
    ])
    const w = mountPanel(firewall)
    await flushPromises()

    expect(api.webServices).toHaveBeenCalledWith(firewall.id)
    const link = w.get('header [data-test=open-web]')
    expect(link.attributes()).toMatchObject({
      href: 'https://192.168.1.1/',
      target: '_blank',
      rel: 'noopener noreferrer',
      'aria-label': 'Open web interface',
    })
  })

  it('lists several web interfaces in a small menu', async () => {
    vi.mocked(api.webServices).mockResolvedValue([
      { url: 'https://192.168.1.1/', port: 443, title: 'OPNsense' },
      { url: 'http://192.168.1.1:8080/', port: 8080, title: 'Status page' },
    ])
    const w = mountPanel(firewall)
    await flushPromises()
    expect(w.findAll('[data-test=open-web]')).toHaveLength(0)
    await w.get('[data-test=open-web-menu]').trigger('click')
    const links = w.findAll('[data-test=open-web]')
    expect(links.map((l) => l.attributes('href'))).toEqual([
      'https://192.168.1.1/',
      'http://192.168.1.1:8080/',
    ])
    expect(links[1]!.text()).toContain('Status page')
  })

  it('shows nothing when there is no web interface', async () => {
    vi.mocked(api.webServices).mockResolvedValue([])
    const w = mountPanel(firewall)
    await flushPromises()
    expect(w.find('[data-test=open-web]').exists()).toBe(false)
    expect(w.text()).not.toContain('Looking for a web interface')
  })

  it('does not probe nodes without an IP', async () => {
    mountPanel({ ...firewall, id: 'seg:x', kind: 'segment', ip: undefined })
    await flushPromises()
    expect(api.webServices).not.toHaveBeenCalled()
  })
})

describe('NodePanel identification', () => {
  beforeEach(() => {
    vi.mocked(api.webServices).mockResolvedValue([])
  })

  it('shows what was detected and from what', async () => {
    const w = mountPanel({
      ...firewall,
      type: 'firewall',
      product: 'opnsense',
      os: 'freebsd',
      reasons: ['hostname:OPNsense', 'vendor:FreeBSD Foundation'],
    })
    await flushPromises()
    const text = w.get('.classification').text()
    expect(text).toContain('Firewall')
    expect(text).toContain('OPNsense')
    expect(text).toContain('FreeBSD')
    expect(text).toContain('name: OPNsense')
    expect(text).toContain('MAC vendor: FreeBSD Foundation')
  })

  it('lets the user correct the type and icon', async () => {
    vi.mocked(api.updateInventory).mockResolvedValue({} as never)
    const w = mountPanel({ ...firewall, id: 'mac:aa', kind: 'client', type: 'unknown' })
    await flushPromises()
    await w.get('[data-test=edit-classification]').trigger('click')
    await w.get('[data-test=type-select]').setValue('phone')
    await w.get('[data-test=icon-select]').setValue('android')
    await w.get('.classify-form').trigger('submit')
    await flushPromises()

    expect(api.updateInventory).toHaveBeenCalledWith('mac:aa', {
      device_type: 'phone',
      icon: 'android',
    })
    expect(w.emitted('changed')![0]).toEqual([{ type: 'phone', icon: 'android' }])
  })
})

describe('NodePanel app node', () => {
  it('links to the app without probing', async () => {
    vi.mocked(api.webServices).mockReset()
    const host: TopoNode = {
      id: 'mac:aa',
      kind: 'client',
      label: 'Ubuntu',
      online: true,
      ip: '192.168.1.42',
    }
    const app: TopoNode = {
      id: 'app:mac:aa:8080',
      kind: 'app',
      label: 'qBittorrent',
      online: true,
      parent_id: 'mac:aa',
      port: '8080',
      product: 'qbittorrent',
      ip: '192.168.1.42',
      web: [
        {
          port: 8080,
          url: 'http://192.168.1.42:8080/',
          title: 'qBittorrent WebUI',
          app: 'qbittorrent',
        },
      ],
    }
    const w = mount(NodePanel, {
      props: { node: app, nodes: [host, app], edges: [], expandedParent: false },
      global: { plugins: plugins(), stubs: { RouterLink: true } },
    })
    await flushPromises()
    expect(api.webServices).not.toHaveBeenCalled()
    expect(w.get('[data-test=open-web]').attributes('href')).toBe('http://192.168.1.42:8080/')
    expect(w.find('.classification').exists()).toBe(false)
    expect(w.text()).toContain('Ubuntu')
  })
})

describe('NodePanel hide and delete', () => {
  beforeEach(() => {
    vi.mocked(api.webServices).mockResolvedValue([])
    vi.mocked(api.updateInventory)
      .mockReset()
      .mockResolvedValue({} as never)
    vi.mocked(api.deleteInventory).mockReset().mockResolvedValue()
  })

  it('hides a device from the map and shows it again', async () => {
    const w = mountPanel(firewall)
    await w.get('[data-test=hide]').trigger('click')
    await flushPromises()
    expect(api.updateInventory).toHaveBeenCalledWith(firewall.id, { hidden: true })
    expect(w.emitted('changed')![0]).toEqual([{ hidden: true }])

    const hidden = mountPanel({ ...firewall, hidden: true })
    expect(hidden.get('[data-test=hide]').text()).toBe('Show on map')
    await hidden.get('[data-test=hide]').trigger('click')
    await flushPromises()
    expect(api.updateInventory).toHaveBeenLastCalledWith(firewall.id, { hidden: false })
  })

  it('deletes only after a second click', async () => {
    const w = mountPanel(firewall)
    const del = w.get('[data-test=delete]')
    await del.trigger('click')
    expect(api.deleteInventory).not.toHaveBeenCalled()
    expect(del.text()).toBe('Click again to delete')

    await del.trigger('click')
    await flushPromises()
    expect(api.deleteInventory).toHaveBeenCalledWith([firewall.id])
    expect(w.emitted('deleted')![0]).toEqual([firewall.id])
  })

  it('has no hide or delete for app nodes', () => {
    const w = mountPanel({ ...firewall, id: 'app:x:8096', kind: 'app' })
    expect(w.find('[data-test=hide]').exists()).toBe(false)
    expect(w.find('[data-test=delete]').exists()).toBe(false)
  })
})

describe('NodePanel firewall data', () => {
  beforeEach(() => {
    vi.mocked(api.webServices).mockResolvedValue([])
  })

  it('shows port speed, half duplex and gateways', () => {
    const w = mountPanel({
      ...firewall,
      device: {
        key: 'fw',
        name: 'fw',
        interfaces: [
          {
            name: 'igb0',
            description: 'WAN',
            up: true,
            speed_mbps: 2500,
            media: '2500Base-T <full-duplex>',
          },
          { name: 'igb1', description: 'LAN', up: true, speed_mbps: 100, duplex: 'half' },
        ],
        gateways: [
          { name: 'WAN_DHCP', status: 'up', rtt_ms: 1.2, loss_pct: 0 },
          { name: 'WAN2', status: 'down', loss_pct: 100 },
        ],
      },
    })
    const rows = w.findAll('.ports tr')
    expect(rows[0]!.text()).toContain('2.5G')
    expect(rows[0]!.find('[title]').attributes('title')).toBe('2500Base-T <full-duplex>')
    expect(rows[1]!.text()).toContain('half duplex')
    const gws = w.get('[data-test=gateways]').text()
    expect(gws).toContain('WAN_DHCP')
    expect(gws).toContain('1.2 ms')
    expect(gws).toContain('Offline')
    expect(gws).toContain('100% loss')
  })
})

describe('NodePanel port descriptions', () => {
  beforeEach(() => {
    vi.mocked(api.webServices).mockResolvedValue([])
    vi.mocked(api.setPortLabel).mockReset().mockResolvedValue()
  })

  const sw = (labels?: Record<string, string>): TopoNode => ({
    ...firewall,
    port_labels: labels,
    device: {
      key: 'sw',
      name: 'sw',
      interfaces: [{ name: 'ge1', description: 'port 1', up: true, speed_mbps: 1000 }],
    },
  })

  it("shows the user's description instead of the device's", () => {
    const w = mountPanel(sw({ ge1: 'Uplink to rack' }))
    expect(w.get('.ports').text()).toContain('Uplink to rack')
    expect(w.get('.ports').text()).not.toContain('port 1')
  })

  it('edits a description inline', async () => {
    const w = mountPanel(sw())
    await w.get('[data-test=edit-port]').trigger('click')
    const input = w.get('[data-test=port-label-input]')
    expect((input.element as HTMLInputElement).value).toBe('port 1')
    await input.setValue(' TV room ')
    await w.get('.port-edit').trigger('submit')
    await flushPromises()
    expect(api.setPortLabel).toHaveBeenCalledWith(firewall.id, 'ge1', 'TV room')
    const changed = w.emitted('changed')!
    expect(changed[changed.length - 1]).toEqual([{ port_labels: { ge1: 'TV room' } }])
  })
})

describe('NodePanel scan', () => {
  beforeEach(() => {
    vi.mocked(api.webServices).mockResolvedValue([])
    vi.mocked(api.scanNode).mockReset()
  })

  const mountWith = (canScan: boolean, node: TopoNode = firewall) =>
    mount(NodePanel, {
      props: { node, nodes: [node], edges: [], expandedParent: false, canScan },
      global: { plugins: plugins(), stubs: { RouterLink: true } },
    })

  it('offers a scan only when the nmap integration is added', () => {
    expect(mountWith(false).find('[data-test=scan]').exists()).toBe(false)
    expect(mountWith(true).find('[data-test=scan]').exists()).toBe(true)
    expect(
      mountWith(true, { ...firewall, kind: 'app' })
        .find('[data-test=scan]')
        .exists(),
    ).toBe(false)
  })

  it('scans the device and shows what nmap found', async () => {
    type Host = Awaited<ReturnType<typeof api.scanNode>>
    let finish: (h: Host) => void = () => {}
    vi.mocked(api.scanNode).mockReturnValue(new Promise<Host>((r) => (finish = r)))
    const w = mountWith(true)
    await w.get('[data-test=scan]').trigger('click')
    expect(w.get('[data-test=scan]').text()).toContain('Scanning')
    finish({ ip: '192.168.1.1', open_ports: [22, 443], os: 'freebsd' })
    await flushPromises()
    expect(api.scanNode).toHaveBeenCalledWith(firewall.id)
    expect(w.get('[data-test=scan-result]').text()).toBe('Scanned: 2 open ports · FreeBSD.')
    expect(w.emitted('scanned')).toHaveLength(1)
  })
})
