import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { nextTick } from 'vue'

import DrawLayer from '@/components/map/DrawLayer.vue'

import { plugins } from './helpers'

// jsdom has no PointerEvent: a mouse event with the same name does.
async function pointer(el: Element, type: string, clientX = 0, clientY = 0) {
  el.dispatchEvent(new MouseEvent(type, { clientX, clientY, bubbles: true, cancelable: true }))
  await nextTick()
}

describe('DrawLayer', () => {
  it('shows the rectangle being dragged and tells it when released', async () => {
    const w = mount(DrawLayer, { global: { plugins: plugins() } })
    expect(w.find('.draft').exists()).toBe(false)
    const layer = w.get('[data-test=draw-layer]').element
    await pointer(layer, 'pointerdown', 50, 40)
    await pointer(layer, 'pointermove', 10, 90)
    expect(w.get('.draft').attributes('style')).toContain('width: 40px')
    await pointer(layer, 'pointerup')
    expect(w.emitted('draw')).toEqual([
      [
        { x: 50, y: 40 },
        { x: 10, y: 90 },
      ],
    ])
    expect(w.find('.draft').exists()).toBe(false)
  })
})
