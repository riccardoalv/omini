import { beforeEach, describe, expect, it } from 'vitest'
import { ref, shallowRef } from 'vue'

import { prefs } from '@/lib/prefs'
import type { TopologyResponse } from '@/lib/types'

import { useFolding } from '../useFolding'
import { useMapGraph } from '../useMapGraph'
import { star } from './helpers'

beforeEach(() => {
  prefs.expanded = []
  prefs.collapsed = []
  prefs.collapsedAreas = []
  prefs.collapseThreshold = 8
})

function setup(clients: number) {
  const g = useMapGraph(shallowRef<TopologyResponse>(star('dev:ap', clients)), ref([]))
  const selectedId = ref<string>()
  return { g, selectedId, f: useFolding(g.view, g.mapGraph, selectedId) }
}

describe('useFolding', () => {
  it('folds and unfolds a node, keeping it still in the next layout', () => {
    const { g, f } = setup(3)
    expect(g.view.value.groups).toHaveLength(0)
    f.toggleChildren('dev:ap')
    expect(prefs.collapsed).toEqual(['dev:ap'])
    expect(g.view.value.groups).toHaveLength(1)
    expect(f.takeAnchor()).toBe('dev:ap')
    expect(f.takeAnchor()).toBeUndefined() // once
    // A click on the bubble unfolds it.
    f.toggleChildren(g.view.value.groups[0]!.id)
    expect(prefs.collapsed).toEqual([])
    expect(prefs.expanded).toEqual(['dev:ap'])
    expect(g.view.value.groups).toHaveLength(0)
  })

  it('follows the side panel only when the expansion came from it', () => {
    const { f, selectedId } = setup(10)
    f.expand('dev:ap')
    expect(selectedId.value).toBeUndefined()
    f.expand('dev:ap', true)
    expect(selectedId.value).toBe('dev:ap')
  })

  it('offers in the node menu what can be done, a bubble acting for its parent', () => {
    const { g, f, selectedId } = setup(10)
    const bubble = g.view.value.groups[0]!.id
    f.menu.value = { x: 0, y: 0, id: bubble }
    expect(f.menuCanExpand.value).toBe(true)
    expect(f.menuCanCollapse.value).toBe(true) // the automatic group can take every client
    f.menuAction('expand')
    expect(f.menu.value).toBeUndefined()
    expect(prefs.expanded).toEqual(['dev:ap'])
    f.menu.value = { x: 0, y: 0, id: 'dev:ap' }
    f.menuAction('details')
    expect(selectedId.value).toBe('dev:ap')
  })

  it('folds an area into a bubble and unfolds it from the bubble', () => {
    const { f, selectedId } = setup(1)
    f.collapseArea(7)
    f.collapseArea(7)
    expect(prefs.collapsedAreas).toEqual([7])
    selectedId.value = 'area-bubble:7'
    f.toggleChildren('area-bubble:7')
    expect(prefs.collapsedAreas).toEqual([])
    expect(selectedId.value).toBeUndefined()
  })
})
