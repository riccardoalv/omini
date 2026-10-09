import { flushPromises } from '@vue/test-utils'
import type { NodeDragEvent } from '@vue-flow/core'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, ref, shallowRef } from 'vue'

import { api } from '@/lib/api'
import type { Box } from '@/lib/areas'
import { prefs } from '@/lib/prefs'
import { NODE_SIZES } from '@/lib/sizes'
import type { MapArea, Point, TopologyResponse } from '@/lib/types'

import { useMapAreas } from '../useMapAreas'
import { useMapGraph } from '../useMapGraph'
import { area, star, withSetup } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      updateArea: vi.fn<typeof mod.api.updateArea>(),
      createArea: vi.fn<typeof mod.api.createArea>(),
      deleteArea: vi.fn<typeof mod.api.deleteArea>(),
    },
  }
})

beforeEach(() => {
  vi.clearAllMocks()
  prefs.expanded = []
  prefs.collapsed = []
  prefs.collapsedAreas = []
  prefs.layoutDirection = 'RIGHT'
})

function setup(areaList: MapArea[]) {
  const areas = ref(areaList)
  const g = useMapGraph(shallowRef<TopologyResponse>(star('dev:fw', 2)), areas)
  const positions = ref<Record<string, Point>>({
    'dev:fw': { x: 0, y: 0 },
    'mac:0': { x: 300, y: 0 },
    'mac:1': { x: 300, y: 60 },
  })
  const nodeBoxes = computed(
    () =>
      new Map<string, Box>(
        g.view.value.nodes
          .filter((n) => positions.value[n.id])
          .map((n) => [n.id, { id: n.id, ...positions.value[n.id]!, ...NODE_SIZES[n.kind]! }]),
      ),
  )
  const keep = vi.fn<(moved: Record<string, Point>) => Promise<void>>(async (moved) => {
    positions.value = { ...positions.value, ...moved }
  })
  const collapseArea = vi.fn<(id: number) => void>()
  const { result } = withSetup(() =>
    useMapAreas({
      areas,
      liveAreas: g.liveAreas,
      view: g.view,
      direction: g.direction,
      positions,
      nodeBoxes,
      onMap: g.onMap,
      keep,
      collapseArea,
      toMap: (p) => p,
    }),
  )
  return { a: result, areas, positions, keep, collapseArea }
}

const drag = (id: string, position: Point) =>
  ({ node: { id, position }, nodes: [{ id, position }] }) as unknown as NodeDragEvent

describe('useMapAreas', () => {
  it('draws an area around its members, and an empty one where it was drawn', () => {
    const { a } = setup([
      area(1, ['mac:0']),
      area(2, [], { x: 900, y: 900 }),
      area(3, [], { direction: 'DOWN' }),
    ])
    const [one, two] = a.visibleAreas.value
    expect(a.visibleAreas.value).toHaveLength(2) // area 3 was drawn top down
    expect(one!.x).toBeLessThan(300)
    expect(one!.x + one!.width).toBeGreaterThan(500)
    expect(two).toMatchObject({ x: 900, y: 900 })
    // A link entering area 1 keeps its pill out of the border.
    expect(a.insetOf('dev:fw', 'mac:0')).toBeGreaterThan(0)
    expect(a.insetOf('dev:fw', 'mac:1')).toBe(0)
  })

  it('moves a dropped device from one area to the one it lands in', async () => {
    const { a, keep } = setup([area(1, ['mac:0']), area(2, [], { x: 800, y: 0, width: 400 })])
    a.onDragStart(drag('mac:0', { x: 300, y: 0 }))
    expect(a.areaBusy.value).toBe(true)
    await a.onDragStop(drag('mac:0', { x: 850.4, y: 20 }))
    expect(keep).toHaveBeenCalledWith({ 'mac:0': { x: 850.4, y: 20 } })
    const calls = vi.mocked(api.updateArea).mock.calls
    // The last member left area 1: it stays where it was.
    expect(calls.find(([id]) => id === 1)![1]).toMatchObject({ members: [], width: 248 })
    expect(calls.find(([id]) => id === 2)![1]).toMatchObject({ members: ['mac:0'] })
    expect(a.areaBusy.value).toBe(false)
  })

  it('moves an area with its members', async () => {
    const { a, positions, keep } = setup([area(1, ['mac:0'])])
    const start = a.visibleAreas.value[0]!
    a.onDragStart(drag('area:1', { x: start.x, y: start.y }))
    a.onDrag(drag('area:1', { x: start.x + 100, y: start.y + 50 }))
    expect(positions.value['mac:0']).toEqual({ x: 400, y: 50 })
    expect(a.visibleAreas.value[0]).toMatchObject({ x: start.x + 100, y: start.y + 50 })
    await a.onDragStop(drag('area:1', { x: start.x + 100, y: start.y + 50 }))
    expect(keep).toHaveBeenCalledWith({ 'mac:0': { x: 400, y: 50 } })
    expect(api.updateArea).toHaveBeenCalledWith(1, expect.objectContaining({ x: start.x + 100 }))
    expect(a.areaBusy.value).toBe(false)
  })

  it('creates an area from a rectangle, taking its devices from other areas', async () => {
    vi.mocked(api.createArea).mockImplementation(async (x) => ({ ...x, id: 9 }))
    const { a, areas } = setup([area(1, ['mac:0', 'mac:1'])])
    a.drawing.value = true
    // A stray click draws nothing: still drawing.
    await a.drawArea({ x: 2000, y: 2000 }, { x: 2005, y: 2005 })
    expect(api.createArea).not.toHaveBeenCalled()
    expect(a.drawing.value).toBe(true)
    await a.drawArea({ x: 290, y: 50 }, { x: 520, y: 110 })
    expect(a.drawing.value).toBe(false)
    expect(api.createArea).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'New area', members: ['mac:1'] }),
    )
    expect(a.editingArea.value).toBe(9) // named right away
    expect(areas.value.find((x) => x.id === 1)!.members).toEqual(['mac:0'])
    expect(api.updateArea).toHaveBeenCalledWith(1, { members: ['mac:0'] })
  })

  it('previews a color from the menu and applies it', async () => {
    const { a, collapseArea } = setup([area(1, ['mac:0'])])
    a.areaMenu.value = { x: 0, y: 0, id: 1 }
    a.previewArea('#ff0000')
    expect(a.visibleAreas.value[0]!.color).toBe('#ff0000')
    a.areaMenuAction({ color: '#ff0000' })
    await flushPromises()
    expect(a.areaMenu.value).toBeUndefined()
    expect(api.updateArea).toHaveBeenCalledWith(1, { color: '#ff0000' })
    a.areaMenu.value = { x: 0, y: 0, id: 1 }
    a.areaMenuAction('collapse')
    expect(collapseArea).toHaveBeenCalledWith(1)
  })
})
