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

  it('puts odd ports on top and even ports below when there are more than 8', () => {
    const w = mountPanel(Array.from({ length: 10 }, (_, i) => port(`p${i + 1}`)))
    const rows = w.findAll('.row')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.findAll('.port').map((p) => p.attributes('data-port'))).toEqual([
      'p1',
      'p3',
      'p5',
      'p7',
      'p9',
    ])
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
    await p.trigger('click')
    expect(w.emitted('select')![0]).toEqual(['mac:aa'])
    await p.trigger('mouseleave')
    expect(w.find('[data-test=port-tip]').exists()).toBe(false)
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

  it('shows nothing without data', () => {
    const w = mount(ResourceBars, { props: {}, global: { plugins: plugins() } })
    expect(w.find('.bars').exists()).toBe(false)
  })
})
