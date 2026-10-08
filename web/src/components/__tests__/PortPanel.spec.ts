import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import type { Interface, TopoNode } from '@/lib/types'

import PortPanel from '../map/PortPanel.vue'
import ResourceBars from '../map/ResourceBars.vue'
import { plugins } from './helpers'

const port = (name: string, extra: Partial<Interface> = {}): Interface => ({ name, ...extra })
const modem: TopoNode = { id: 'mac:aa', kind: 'client', label: 'ISP modem', online: true }

const mountPanel = (ports: Interface[], links: Record<string, TopoNode | undefined> = {}) =>
  mount(PortPanel, {
    props: { ports, links },
    global: { plugins: plugins() },
    attachTo: document.body,
  })

describe('PortPanel', () => {
  it("shows a port's VLANs and its module's optics, flagging weak light", async () => {
    const w = mountPanel([
      port('sfp1', {
        up: true,
        speed_mbps: 10000,
        connector: 'sfp',
        vlans: { untagged: 1, tagged: [20, 30] },
        transceiver: {
          vendor: 'FS',
          part: 'SFP-10GSR-85',
          rx_power_dbm: -22.4,
          tx_power_dbm: -2.1,
        },
      }),
    ])
    await w.get('.port').trigger('click')
    const card = w.get('[data-test=port-card]').text()
    expect(card).toContain('untagged 1 · tagged 20, 30')
    expect(card).toContain('FS SFP-10GSR-85')
    expect(card).toContain('Rx -22.4 dBm · Tx -2.1 dBm')
    expect(w.find('[data-test=port-card] dd.err').text()).toContain('Rx -22.4')
    w.unmount()
  })

  it('draws one port per interface, colored by speed, empty when down', () => {
    const w = mountPanel([
      port('igb0', { up: true, speed_mbps: 2500 }),
      port('igb1', { up: true, speed_mbps: 1000 }),
      port('igb2', { up: false }),
    ])
    const ports = w.findAll('.port')
    expect(ports).toHaveLength(3)
    expect(ports[0]!.attributes('style')).toContain('--c: var(--speed-2_5g)')
    expect(ports[1]!.attributes('style')).toContain('--c: var(--speed-1g)')
    expect(ports[2]!.classes()).not.toContain('up')
    expect(ports[2]!.attributes('aria-label')).toBe('igb2: down')
    // Legend: only the speeds in use, plus "down".
    expect(w.get('.legend').text()).toBe('2.5 Gbps 1 Gbps down')
  })

  it('keeps every port in one row, in order', () => {
    const w = mountPanel(Array.from({ length: 10 }, (_, i) => port(`p${i + 1}`)))
    const rows = w.findAll('.row')
    expect(rows).toHaveLength(1)
    expect(rows[0]!.findAll('.port').map((p) => p.attributes('data-port'))).toEqual(
      Array.from({ length: 10 }, (_, i) => `p${i + 1}`),
    )
  })

  it('shows the details of a port on hover and opens what is connected on click', async () => {
    const w = mountPanel(
      [
        port('igb0', {
          description: 'WAN',
          up: true,
          speed_mbps: 2500,
          duplex: 'full',
          media: '2500Base-T <full-duplex>',
          ips: ['203.0.113.10/24'],
          rx_bytes: 1536,
          tx_bytes: 2048,
          rx_errors: 2,
        }),
      ],
      { igb0: modem },
    )
    const p = w.get('[data-port=igb0]')
    await p.trigger('mouseenter')
    const tip = w.get('[data-test=port-tip]').text()
    for (const s of [
      'igb0',
      'WAN',
      '2.5G · full duplex',
      '2500Base-T',
      'ISP modem',
      '203.0.113.10/24',
      '1.5 KiB',
      '2.0 KiB',
    ]) {
      expect(tip).toContain(s)
    }
    expect(w.get('[data-test=port-tip] .err').text()).toBe('2')
    await p.trigger('mouseleave')
    expect(w.find('[data-test=port-tip]').exists()).toBe(false)
  })

  it('opens a card with the port options on click', async () => {
    const w = mountPanel([port('igb0', { up: true, speed_mbps: 1000 })], { igb0: modem })
    await w.get('[data-port=igb0]').trigger('click')
    const card = w.get('[data-test=port-card]')
    expect(card.text()).toContain('ISP modem')
    // Open what is connected.
    await card.get('[data-test=open-linked]').trigger('click')
    expect(w.emitted('select')![0]).toEqual(['mac:aa'])
    // Name the port.
    await card.get('[data-test=edit-port]').trigger('click')
    await w.get('[data-test=port-label-input]').setValue(' Uplink ')
    await w.get('.name-form').trigger('submit')
    expect(w.emitted('label')![0]).toEqual(['igb0', 'Uplink'])
    // A second click closes it.
    await w.get('[data-port=igb0]').trigger('click')
    expect(w.find('[data-test=port-card]').exists()).toBe(false)
  })

  it("shows the name the user gave a port, with the device's own name next to it", async () => {
    const w = mount(PortPanel, {
      props: {
        ports: [port('mlxen0', { up: true, speed_mbps: 10000, description: 'LAN_PHYSICAL' })],
        links: {},
        labels: { mlxen0: 'Porta LAN' },
      },
      global: { plugins: plugins() },
    })
    expect(w.get('[data-port=mlxen0] .name').text()).toBe('Porta LAN')
    await w.get('[data-port=mlxen0]').trigger('click')
    const card = w.get('[data-test=port-card]').text()
    expect(card).toContain('Porta LAN')
    expect(card).toContain('mlxen0')
    expect(card).toContain('LAN_PHYSICAL')
  })
  it('draws SFP cages and names the generation by speed', async () => {
    const w = mountPanel([
      port('mlxen0', { up: true, speed_mbps: 10000, connector: 'sfp' }),
      port('re0', { up: true, speed_mbps: 2500, connector: 'rj45' }),
      port('igb0', { up: true, speed_mbps: 1000 }), // unknown: RJ45
    ])
    expect(w.get('[data-port=mlxen0] svg').classes()).toContain('cage')
    expect(w.get('[data-port=re0] svg').classes()).not.toContain('cage')
    expect(w.get('[data-port=igb0] svg').classes()).not.toContain('cage')
    await w.get('[data-port=mlxen0]').trigger('mouseenter')
    expect(w.get('[data-test=port-tip]').text()).toContain('SFP+')
  })
})

describe('ResourceBars', () => {
  it('shows CPU and memory as bars colored by load', () => {
    const w = mount(ResourceBars, {
      props: { cpu: 12.4, memory: 91 },
      global: { plugins: plugins() },
    })
    const cpu = w.get('[data-test=bar-cpu]')
    expect(cpu.text()).toContain('12%')
    expect(cpu.get('.fill').attributes('style')).toContain('width: 12.4%')
    expect(cpu.get('.fill').classes()).toContain('low')
    expect(w.get('[data-test=bar-memory] .fill').classes()).toContain('high')
    expect(w.get('[data-test=bar-memory] [role=meter]').attributes('aria-valuenow')).toBe('91')
  })

  it('adds swap and one bar per disk, which warn later', () => {
    const GiB = 1024 ** 3
    const w = mount(ResourceBars, {
      props: {
        swap: 0,
        storage: [
          { mount: '/', total_bytes: 10 * GiB, used_bytes: 8.2 * GiB },
          { mount: '/boot/efi', total_bytes: 256 * 1024 ** 2, used_bytes: 0 },
          { mount: '/dev', total_bytes: 0 },
        ],
      },
      global: { plugins: plugins() },
    })
    expect(w.get('[data-test=bar-swap]').text()).toContain('0%')
    const root = w.get('[data-test="bar-disk:/"]')
    expect(root.text()).toContain('Disk /')
    expect(root.text()).toContain('8.2 GiB of 10.0 GiB')
    // 82% is yellow for a disk (it would be red for memory).
    expect(root.get('.fill').classes()).toContain('medium')
    expect(w.find('[data-test="bar-disk:/dev"]').exists()).toBe(false)
  })

  it('shows nothing without data', () => {
    const w = mount(ResourceBars, { props: {}, global: { plugins: plugins() } })
    expect(w.find('.bars').exists()).toBe(false)
  })
})
