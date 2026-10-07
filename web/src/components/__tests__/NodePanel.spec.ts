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

  it('shows gateways with status, latency and loss', () => {
    const w = mountPanel({
      ...firewall,
      device: {
        key: 'fw',
        name: 'fw',
        gateways: [
          { name: 'WAN_DHCP', status: 'up', rtt_ms: 1.2, loss_pct: 0 },
          { name: 'WAN2', status: 'down', loss_pct: 100 },
        ],
      },
    })
    const gws = w.get('[data-test=gateways]').text()
    expect(gws).toContain('WAN_DHCP')
    expect(gws).toContain('1.2 ms')
    expect(gws).toContain('Offline')
    expect(gws).toContain('100% loss')
  })
})

describe('NodePanel port names', () => {
  beforeEach(() => {
    vi.mocked(api.webServices).mockResolvedValue([])
    vi.mocked(api.setPortLabel).mockReset().mockResolvedValue()
  })

  const sw: TopoNode = {
    ...firewall,
    device: {
      key: 'sw',
      name: 'sw',
      interfaces: [
        { name: 'ge1', description: 'port 1', up: true, speed_mbps: 1000 },
        { name: 'vlan10', type: 'vlan', up: true },
      ],
    },
  }

  it('shows only the front view of the ports, no list', () => {
    const w = mountPanel(sw)
    expect(w.findAll('[data-port]')).toHaveLength(1) // the VLAN has no jack
    expect(w.find('.ports').exists()).toBe(false)
    expect(w.text()).not.toContain('All ports')
  })

  it('names a port from its card', async () => {
    const w = mountPanel(sw)
    await w.get('[data-port=ge1]').trigger('click')
    await w.get('[data-test=edit-port]').trigger('click')
    await w.get('[data-test=port-label-input]').setValue('TV room')
    await w.get('.name-form').trigger('submit')
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

describe('NodePanel layout', () => {
  beforeEach(() => {
    vi.mocked(api.webServices).mockResolvedValue([])
    localStorage.clear()
  })

  const opnsense: TopoNode = {
    ...firewall,
    traffic: { igb0: { rx_bps: 12_000_000, tx_bps: 3_000_000 } },
    device: {
      key: 'fw',
      name: 'fw',
      os_version: 'OPNsense 26.1',
      uptime_s: 3600 * 50,
      cpu_pct: 12,
      mem_pct: 40,
      dhcp_leases: [
        { ip: '192.168.1.10', mac: 'aa:aa:aa:aa:aa:01' },
        { ip: '192.168.1.11', mac: 'aa:aa:aa:aa:aa:02' },
      ],
      interfaces: [
        { name: 'igb0', type: 'ethernet', up: true, wan: true, speed_mbps: 1000 },
        { name: 'ix0', type: 'ethernet', up: true, speed_mbps: 10000, connector: 'sfp' },
        { name: 'igb1', type: 'ethernet', up: false },
      ],
    },
  }

  it('sums up the device at the top, internet traffic first', () => {
    const w = mountPanel(opnsense)
    expect(w.get('[data-test=flow]').text()).toContain('12.0 Mbps')
    expect(w.get('[data-test=flow]').text()).toContain('3.0 Mbps')
    expect(w.get('[data-test=tile-version]').text()).toContain('OPNsense 26.1')
    expect(w.get('[data-test=tile-ports]').text()).toContain('2 of 3')
    expect(w.get('[data-test=tile-leases]').text()).toContain('2')
    expect(w.find('[data-test=tile-uptime]').exists()).toBe(true)
  })

  it('draws the ports right below CPU and memory', () => {
    const w = mountPanel(opnsense)
    const html = w.html()
    const bars = html.indexOf('data-test="bar-memory"')
    const ports = html.indexOf('data-test="ports-section"')
    const details = html.indexOf('class="block classification"')
    expect(bars).toBeGreaterThan(0)
    expect(ports).toBeGreaterThan(bars)
    expect(details).toBeGreaterThan(ports)
  })

  it('is resized by dragging its left edge, and remembers the width', async () => {
    Object.defineProperty(window, 'innerWidth', { value: 1600, configurable: true })
    const w = mountPanel(firewall)
    const panel = w.get('aside').element as HTMLElement
    const before = parseInt(panel.style.getPropertyValue('--panel-width'))
    w.get('[data-test=resize]').element.dispatchEvent(
      new MouseEvent('pointerdown', { clientX: 1000, bubbles: true }),
    )
    window.dispatchEvent(new MouseEvent('pointermove', { clientX: 800 }))
    window.dispatchEvent(new MouseEvent('pointerup'))
    await flushPromises()
    expect(parseInt(panel.style.getPropertyValue('--panel-width'))).toBe(before + 200)
    expect(localStorage.getItem('omini.panelWidth')).toBe(String(before + 200))

    // Never narrower than the minimum; the keyboard works too.
    await w.get('[data-test=resize]').trigger('keydown', { key: 'ArrowRight' })
    expect(parseInt(panel.style.getPropertyValue('--panel-width'))).toBe(before + 160)
    const again = mountPanel(firewall)
    expect(
      parseInt((again.get('aside').element as HTMLElement).style.getPropertyValue('--panel-width')),
    ).toBe(before + 160)
  })
})

describe('NodePanel clients', () => {
  it('lists the first clients and the rest on demand', async () => {
    vi.mocked(api.webServices).mockResolvedValue([])
    const clients: TopoNode[] = Array.from({ length: 12 }, (_, i) => ({
      id: `mac:${i}`,
      kind: 'client',
      label: `phone-${i}`,
      online: true,
      parent_id: firewall.id,
    }))
    const edges = clients.map((c) => ({
      id: `e:${c.id}`,
      source: firewall.id,
      target: c.id,
      kind: 'inferred' as const,
    }))
    const w = mount(NodePanel, {
      props: { node: firewall, nodes: [firewall, ...clients], edges, expandedParent: false },
      global: { plugins: plugins(), stubs: { RouterLink: true } },
    })
    expect(w.findAll('.client-list li')).toHaveLength(8)
    await w.get('[data-test=more-clients]').trigger('click')
    expect(w.findAll('.client-list li')).toHaveLength(12)
    expect(w.get('[data-test=tile-clients]').text()).toContain('12')
  })
})

describe('NodePanel system health', () => {
  beforeEach(() => {
    vi.mocked(api.webServices).mockResolvedValue([])
  })

  const withHealth = (device: Partial<NonNullable<TopoNode['device']>>): TopoNode => ({
    ...firewall,
    device: { key: 'fw', name: 'fw', ...device },
  })

  it('shows pending updates, the hottest CPU and the load', () => {
    const w = mountPanel(
      withHealth({
        firmware: {
          current: '26.7.4',
          latest: '26.7.5',
          update_available: true,
          updates: 3,
          needs_reboot: true,
          checked_at: new Date().toISOString(),
        },
        temperatures: [
          { sensor: 'CPU 0', kind: 'cpu', celsius: 47 },
          { sensor: 'CPU 1', kind: 'cpu', celsius: 72.4 },
          { sensor: 'Zone', kind: 'board', celsius: 90 },
        ],
        load_avg: [0.34, 0.35, 0.33],
      }),
    )
    const updates = w.get('[data-test=tile-updates]')
    expect(updates.text()).toContain('3 updates → 26.7.5')
    expect(updates.text()).toContain('needs a reboot')
    expect(updates.classes()).toContain('warn')
    const temp = w.get('[data-test=tile-temperature]')
    expect(temp.text()).toContain('CPU temperature')
    expect(temp.text()).toContain('72 °C')
    expect(temp.classes()).toContain('warn')
    expect(temp.attributes('title')).toContain('Zone: 90 °C')
    expect(w.get('[data-test=tile-load]').text()).toContain('0.34 · 0.35 · 0.33')
  })

  it('says when the device is up to date or never checked', () => {
    const ok = mountPanel(withHealth({ firmware: { current: '26.7.5', update_available: false } }))
    expect(ok.get('[data-test=tile-updates]').text()).toContain('Up to date')
    const never = mountPanel(withHealth({ firmware: { current: '26.7.5' } }))
    expect(never.get('[data-test=tile-updates]').text()).toContain('Not checked')
    expect(never.get('[data-test=tile-updates]').attributes('title')).toContain('Check for updates')
  })
})

describe('NodePanel Wi-Fi client', () => {
  it('shows the band, link rate and current traffic', () => {
    vi.mocked(api.webServices).mockResolvedValue([])
    const w = mountPanel({
      id: 'mac:02',
      kind: 'client',
      label: 'moto-g86-5G',
      online: true,
      ip: '192.168.1.185',
      band: '5ghz',
      link_mbps: 866.7,
      flow: { rx_bps: 12_000_000, tx_bps: 300_000 },
    })
    expect(w.get('[data-test=band]').text()).toBe('5 GHz · 867 Mbps link')
    const flow = w.get('[data-test=flow]').text()
    expect(flow).toContain('Traffic now')
    expect(flow).toContain('12.0 Mbps')
  })
})
