import { beforeEach, describe, expect, it } from 'vitest'
import { ref, shallowRef } from 'vue'

import { prefs } from '@/lib/prefs'
import type { MapArea, TopologyResponse } from '@/lib/types'

import { useMapGraph } from '../useMapGraph'
import { area, star } from './helpers'

beforeEach(() => {
  prefs.expanded = []
  prefs.collapsed = []
  prefs.collapsedAreas = []
  prefs.hideOffline = false
  prefs.collapseThreshold = 8
})

describe('useMapGraph', () => {
  it('counts hidden devices and hidden areas, and shows them on demand', () => {
    const topo = star('dev:fw', 2)
    topo.topology.nodes[1]!.hidden = true
    const areas = ref<MapArea[]>([area(1, ['mac:0'], { hidden: true }), area(2, [])])
    const g = useMapGraph(shallowRef<TopologyResponse>(topo), areas)
    expect(g.hiddenCount.value).toBe(2) // the device and area 1
    expect(g.nodes.value.map((n) => n.id)).not.toContain('mac:0')
    expect(g.liveAreas.value.map((a) => a.id)).toEqual([2])
    g.showHidden.value = true
    expect(g.nodes.value.map((n) => n.id)).toContain('mac:0')
    expect(g.liveAreas.value.map((a) => a.id)).toEqual([1, 2])
  })

  it('folds many clients into a bubble, and unfolds everything for an export', () => {
    const g = useMapGraph(shallowRef<TopologyResponse>(star('dev:ap', 10)), ref([]))
    expect(g.view.value.groups).toHaveLength(1)
    expect(g.summary.value).toEqual({ devices: 1, clients: 10, problems: 0 })
    // A member folded into the bubble is the bubble on the map.
    expect(g.onMap(['mac:3', 'dev:ap'])).toEqual([g.view.value.groups[0]!.id, 'dev:ap'])
    g.expandAll.value = true
    expect(g.view.value.groups).toHaveLength(0)
    expect(g.onMap(['mac:3'])).toEqual(['mac:3'])
  })

  it('leaves offline devices out when asked, and orients the export its own way', () => {
    const topo = star('dev:fw', 2)
    topo.topology.nodes[2]!.online = false
    topo.statuses = [{ integration_id: 4, ok: false, collected_at: '', duration_ms: 0, devices: 0 }]
    const g = useMapGraph(shallowRef<TopologyResponse>(topo), ref([]))
    expect(g.offlineCount.value).toBe(1)
    expect(g.summary.value.problems).toBe(1)
    prefs.hideOffline = true
    expect(g.nodes.value).toHaveLength(2)
    prefs.layoutDirection = 'RIGHT'
    g.exportDirection.value = 'DOWN'
    expect(g.direction.value).toBe('DOWN')
    g.exportDirection.value = undefined
    expect(g.direction.value).toBe('RIGHT')
  })
})
