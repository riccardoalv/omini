import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import type { MapArea } from '@/lib/types'

import AreaMenu from '../map/AreaMenu.vue'
import AreaNode from '../map/AreaNode.vue'
import { plugins } from './helpers'

const area: MapArea = {
  id: 3,
  name: 'Rack',
  color: 'green',
  direction: 'RIGHT',
  x: 0,
  y: 0,
  width: 300,
  height: 200,
  members: [],
}

const mountArea = (editing = false, zoom = 1) =>
  mount(AreaNode, {
    props: { area, editing, zoom },
    global: { plugins: plugins() },
    attachTo: document.body,
  })

describe('AreaNode', () => {
  it('shows the name and color', () => {
    const w = mountArea()
    expect(w.text()).toContain('Rack')
    expect(w.get('.area').attributes('data-color')).toBe('green')
  })

  it('asks to rename on double click', async () => {
    const w = mountArea()
    await w.get('.area-title').trigger('dblclick')
    expect(w.emitted('startRename')).toHaveLength(1)
  })

  it('renames with Enter and cancels with Escape', async () => {
    const w = mountArea(true)
    const input = w.get('input')
    expect((input.element as HTMLInputElement).value).toBe('Rack')
    await input.setValue('  Server rack ')
    await input.trigger('keydown', { key: 'Enter' })
    expect(w.emitted('rename')![0]).toEqual(['Server rack'])

    const w2 = mountArea(true)
    await w2.get('input').trigger('keydown', { key: 'Escape' })
    expect(w2.emitted('cancelRename')).toHaveLength(1)
    expect(w2.emitted('rename')).toBeUndefined()
  })

  it('does not rename to an empty or unchanged name', async () => {
    const w = mountArea(true)
    await w.get('input').setValue('   ')
    await w.get('input').trigger('keydown', { key: 'Enter' })
    expect(w.emitted('rename')).toBeUndefined()
    expect(w.emitted('cancelRename')).toHaveLength(1)
  })

  it('resizes from the corner, in map units and never below the minimum', async () => {
    const w = mountArea(false, 2)
    const handle = w.get('[data-test=resize]').element
    // jsdom has no PointerEvent: a MouseEvent with the pointer event type is enough.
    const fire = async (type: string, clientX = 0, clientY = 0) => {
      handle.dispatchEvent(new MouseEvent(type, { clientX, clientY, bubbles: true }))
      await w.vm.$nextTick()
    }
    await fire('pointerdown', 100, 100)
    await fire('pointermove', 200, 140)
    expect(w.emitted('resize')![0]).toEqual([{ width: 350, height: 220 }]) // zoom 2: half the pixels
    await fire('pointermove', -1000, -1000)
    expect(w.emitted('resize')![1]).toEqual([{ width: 60, height: 60 }])
    await fire('pointerup')
    expect(w.emitted('resizeEnd')).toHaveLength(1)
  })
})

describe('AreaMenu', () => {
  const mountMenu = () =>
    mount(AreaMenu, { props: { x: 5, y: 6, color: 'green' }, global: { plugins: plugins() } })

  it('renames, recolors and deletes', async () => {
    const w = mountMenu()
    await w.get('[data-test=rename]').trigger('click')
    expect(w.emitted('rename')).toHaveLength(1)

    expect(w.get('.swatch.current').attributes('data-color')).toBe('green')
    await w.get('.swatch[data-color=purple]').trigger('click')
    expect(w.emitted('color')![0]).toEqual(['purple'])

    await w.get('[data-test=delete]').trigger('click')
    expect(w.emitted('delete')).toHaveLength(1)
  })

  it('closes with Escape', () => {
    const w = mountMenu()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(w.emitted('close')).toHaveLength(1)
  })
})
