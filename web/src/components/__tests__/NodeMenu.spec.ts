import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import NodeMenu from '../map/NodeMenu.vue'
import { plugins } from './helpers'

const mountMenu = (props: { canCollapse: boolean; canExpand: boolean }) =>
  mount(NodeMenu, { props: { x: 10, y: 20, ...props }, global: { plugins: plugins() } })

describe('NodeMenu', () => {
  it('offers collapsing when the node has visible children', async () => {
    const w = mountMenu({ canCollapse: true, canExpand: false })
    expect(w.find('[data-test=expand]').exists()).toBe(false)
    await w.get('[data-test=collapse]').trigger('click')
    expect(w.emitted('collapse')).toHaveLength(1)
    expect(w.get('.menu').attributes('style')).toContain('left: 10px')
  })

  it('offers expanding a collapsed group', async () => {
    const w = mountMenu({ canCollapse: false, canExpand: true })
    expect(w.find('[data-test=collapse]').exists()).toBe(false)
    await w.get('[data-test=expand]').trigger('click')
    expect(w.emitted('expand')).toHaveLength(1)
  })

  it('closes with Escape', async () => {
    const w = mountMenu({ canCollapse: true, canExpand: false })
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(w.emitted('close')).toHaveLength(1)
  })
})
