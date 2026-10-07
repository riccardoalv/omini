import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import TopologyNode from '../map/TopologyNode.vue'
import { plugins } from './helpers'

const mountNode = () =>
  mount(TopologyNode, {
    props: { data: { node: { id: 'x', kind: 'client', label: 'Ubuntu', online: true } } },
    global: { plugins: plugins(), stubs: { Handle: true } },
  })

describe('TopologyNode', () => {
  it('asks to toggle its children on middle click only', async () => {
    const w = mountNode()
    await w.get('.topo-node').trigger('auxclick', { button: 1 })
    await w.get('.topo-node').trigger('auxclick', { button: 2 })
    expect(w.emitted('toggle')).toHaveLength(1)
  })
})

describe('TopologyNode Wi-Fi client', () => {
  it('shows the network and band it uses', () => {
    const w = mount(TopologyNode, {
      props: {
        data: {
          node: {
            id: 'mac:02',
            kind: 'client',
            label: 'A36',
            online: true,
            ip: '192.168.1.41',
            ssid: 'IOT',
            band: '2.4ghz',
          },
        },
      },
      global: { plugins: plugins(), stubs: { Handle: true } },
    })
    const tag = w.get('[data-test=wifi-tag]')
    expect(tag.text()).toBe('IOT · 2.4G')
    expect(tag.classes()).toContain('band-2.4ghz')
    expect(w.text()).toContain('192.168.1.41')
  })
})

describe('TopologyNode traffic badge', () => {
  it('goes beside a client, above other devices', () => {
    const at = (kind: 'client' | 'device') =>
      mount(TopologyNode, {
        props: {
          data: {
            node: { id: 'x', kind, label: 'x', online: true },
            flow: { down: 1e6, up: 1e5 },
          },
        },
        global: { plugins: plugins(), stubs: { Handle: true } },
      })
        .get('[data-test=node-flow]')
        .classes()
    expect(at('client')).toContain('side')
    expect(at('device')).not.toContain('side')
  })
})
