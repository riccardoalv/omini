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
