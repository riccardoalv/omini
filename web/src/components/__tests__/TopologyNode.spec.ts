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

describe('TopologyNode Wi-Fi network', () => {
  it('is a small picture with the network name, colored by band', () => {
    const w = mount(TopologyNode, {
      props: {
        data: {
          node: {
            id: 'wifi:dev:ap:IOT · 2.4 GHz',
            kind: 'ssid',
            label: 'IOT · 2.4 GHz',
            online: true,
            band: '2.4ghz',
          },
        },
      },
      global: { plugins: plugins(), stubs: { Handle: true } },
    })
    expect(w.get('[data-test=ssid]').text()).toBe('IOT · 2.4 GHz')
    expect(w.get('.ssid-icon').classes()).toContain('band-2.4ghz')
    expect(w.get('.topo-node').classes()).toContain('ssid')
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
