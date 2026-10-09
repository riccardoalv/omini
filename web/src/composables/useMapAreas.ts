import type { NodeDragEvent } from '@vue-flow/core'
import { computed, ref, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/lib/api'
import {
  AREA_PADDING,
  AREA_TITLE,
  areaIdOf,
  type Box,
  fitArea,
  MIN_AREA_SIZE,
  membersOf,
  rectFrom,
  withDescendants,
} from '@/lib/areas'
import type { GraphView } from '@/lib/graph'
import type { Direction } from '@/lib/layout'
import { prefs } from '@/lib/prefs'
import type { MapArea, Point } from '@/lib/types'

type Rect = { x: number; y: number; width: number; height: number }

/**
 * Map areas: drawn around their members, and edited on the map — drawn,
 * moved (with their members), resized, renamed, colored, hidden and deleted.
 * A device belongs to one area: dropping it in one takes it out of the others.
 */
export function useMapAreas(options: {
  areas: Ref<MapArea[]>
  liveAreas: Ref<MapArea[]>
  view: Ref<GraphView>
  direction: Ref<Direction>
  positions: Ref<Record<string, Point>>
  nodeBoxes: Ref<Map<string, Box>>
  /** An area's members as they are on the map (see useMapGraph). */
  onMap: (members: string[]) => string[]
  /** Keeps dragged positions (see useMapLayout). */
  keep: (moved: Record<string, Point>) => Promise<void>
  /** Folds an area into a bubble (see useFolding). */
  collapseArea: (id: number) => void
  /** Map coordinates of a point on the screen (Vue Flow's). */
  toMap: (p: Point) => Point
}) {
  const { areas, liveAreas, view, direction, positions, nodeBoxes, onMap, keep } = options
  const { t } = useI18n()

  const editingArea = ref<number>()
  const areaBusy = ref(false) // dragging or resizing
  /** Rectangles held still while something is being dragged or resized. */
  const frozen = ref<Record<number, Rect>>({})
  /** A color being picked in the area menu, shown on the area until it is applied or the menu closes. */
  const areaPreview = ref<{ id: number; color: string }>()

  /** A node in an area brings everything below it (apps, clients, VMs). */
  const areaNodes = (a: MapArea) => withDescendants(onMap(a.members), view.value.edges)

  /**
   * Areas drawn around their nodes, in both orientations. An area with none of
   * its nodes on the map keeps its stored rectangle, which only makes sense in
   * the orientation it was drawn in.
   */
  const visibleAreas = computed(() =>
    liveAreas.value
      .filter((a) => !prefs.collapsedAreas.includes(a.id))
      .filter((a) => {
        const shown = areaNodes(a).some((id) => nodeBoxes.value.has(id))
        // Members, none of them on the map (hidden, offline): not drawn. An area
        // drawn empty keeps its rectangle (in the orientation it was drawn in).
        return shown || (!a.members.length && a.direction === direction.value)
      })
      .map((a) => ({
        ...a,
        ...(frozen.value[a.id] ?? fitArea({ ...a, members: areaNodes(a) }, nodeBoxes.value)),
        ...(areaPreview.value?.id === a.id ? { color: areaPreview.value.color } : {}),
      })),
  )

  /** The area each node on the map is drawn in. */
  const areaOfNode = computed(() => {
    const out = new Map<string, number>()
    for (const a of visibleAreas.value)
      for (const id of areaNodes(a)) if (!out.has(id)) out.set(id, a.id)
    return out
  })
  /** A link entering an area: its pill stays out of the area's border (and title, top down). */
  function insetOf(source: string, target: string) {
    const a = areaOfNode.value.get(target)
    if (a === undefined || areaOfNode.value.get(source) === a) return 0
    return direction.value === 'DOWN' ? AREA_PADDING + AREA_TITLE : AREA_PADDING
  }

  function freezeAreas() {
    frozen.value = Object.fromEntries(
      visibleAreas.value.map((a) => [a.id, { x: a.x, y: a.y, width: a.width, height: a.height }]),
    )
  }

  function patchArea(id: number, patch: Partial<MapArea>) {
    areas.value = areas.value.map((a) => (a.id === id ? { ...a, ...patch } : a))
  }

  /** Saves an area with its current rectangle (used while no member is on the map). */
  async function saveArea(id: number, patch: Partial<MapArea> = {}) {
    const shown = visibleAreas.value.find((a) => a.id === id)
    const rect = shown
      ? {
          x: Math.round(shown.x),
          y: Math.round(shown.y),
          width: Math.round(shown.width),
          height: Math.round(shown.height),
        }
      : {}
    patchArea(id, { ...rect, ...patch })
    await api.updateArea(id, { ...rect, ...patch })
  }

  // Moving an area moves its members.
  let areaDrag: { id: number; start: Point; members: Record<string, Point> } | undefined

  function onDragStart(e: NodeDragEvent) {
    const id = areaIdOf(e.node.id)
    const area = visibleAreas.value.find((a) => a.id === id)
    areaBusy.value = true
    if (!area) {
      freezeAreas() // a device is dragged: areas keep their shape until it is dropped
      return
    }
    const members: Record<string, Point> = {}
    for (const m of areaNodes(area)) if (positions.value[m]) members[m] = { ...positions.value[m]! }
    areaDrag = { id: area.id, start: { x: area.x, y: area.y }, members }
    frozen.value = { [area.id]: { x: area.x, y: area.y, width: area.width, height: area.height } }
  }

  function onDrag(e: NodeDragEvent) {
    if (!areaDrag) return
    const dx = e.node.position.x - areaDrag.start.x
    const dy = e.node.position.y - areaDrag.start.y
    const next = { ...positions.value }
    for (const [m, p] of Object.entries(areaDrag.members)) next[m] = { x: p.x + dx, y: p.y + dy }
    positions.value = next
    const f = frozen.value[areaDrag.id]!
    frozen.value = { [areaDrag.id]: { ...f, x: e.node.position.x, y: e.node.position.y } }
  }

  async function onDragStop(e: NodeDragEvent) {
    if (areaDrag) {
      await finishAreaDrag()
      return
    }
    const moved = Object.fromEntries(e.nodes.map((n) => [n.id, n.position]))
    await Promise.all([keep(moved), regroupDropped(Object.keys(moved))])
  }

  async function finishAreaDrag() {
    const drag = areaDrag!
    areaDrag = undefined
    const moved = Object.fromEntries(Object.keys(drag.members).map((m) => [m, positions.value[m]!]))
    try {
      await Promise.all([keep(moved), saveArea(drag.id)])
    } finally {
      frozen.value = {}
      areaBusy.value = false
    }
  }

  /** Dropped devices join the area they were dropped in and leave the one they were dragged out of. */
  async function regroupDropped(ids: string[]) {
    const dropped = ids.map((id) => nodeBoxes.value.get(id)).filter((b) => !!b)
    const rects = frozen.value
    const changes: Promise<void>[] = []
    // A dropped device joins the innermost area it lands in (areas can sit
    // inside others) and leaves every other: it belongs to one area.
    const home = new Map<string, number>()
    for (const d of dropped) {
      const cx = d.x + d.width / 2
      const cy = d.y + d.height / 2
      let best: { id: number; size: number } | undefined
      for (const [id, r] of Object.entries(rects)) {
        if (cx < r.x || cx > r.x + r.width || cy < r.y || cy > r.y + r.height) continue
        if (!best || r.width * r.height < best.size)
          best = { id: Number(id), size: r.width * r.height }
      }
      if (best) home.set(d.id, best.id)
    }
    for (const a of liveAreas.value) {
      const rect = rects[a.id]
      if (!rect) continue
      const members = [
        // Kept unless it was dropped elsewhere (outside every area, or in another).
        ...a.members.filter((m) =>
          home.has(m) ? home.get(m) === a.id : !dropped.some((d) => d.id === m),
        ),
        ...[...home].filter(([m, id]) => id === a.id && !a.members.includes(m)).map(([m]) => m),
      ]
      if (members.join() === a.members.join()) continue
      // The last member left: the area stays where it was.
      changes.push(saveArea(a.id, members.length ? { members } : { members, ...rect }))
    }
    frozen.value = {}
    areaBusy.value = false
    await Promise.all(changes)
  }

  function resizeArea(id: number, size: { width: number; height: number }) {
    areaBusy.value = true
    const a = visibleAreas.value.find((x) => x.id === id)
    if (!a) return
    frozen.value = { [id]: { x: a.x, y: a.y, ...size } }
  }

  /** A device belongs to one area: putting it in one takes it out of the others. */
  async function claim(ids: string[], owner: number) {
    const taken = new Set(ids)
    const changes: Promise<unknown>[] = []
    for (const a of areas.value) {
      if (a.id === owner || !a.members.some((m) => taken.has(m))) continue
      const members = a.members.filter((m) => !taken.has(m))
      patchArea(a.id, { members })
      changes.push(api.updateArea(a.id, { members }))
    }
    await Promise.all(changes)
  }

  /** Resizing changes which devices are inside; the area then fits around them. */
  async function finishResize(id: number) {
    const rect = frozen.value[id]
    try {
      if (rect) {
        const members = membersOf(rect, [...nodeBoxes.value.values()])
        patchArea(id, { ...rect, members })
        frozen.value = {}
        await api.updateArea(id, { ...rect, members })
        await claim(members, id)
      }
    } finally {
      frozen.value = {}
      areaBusy.value = false
    }
  }

  async function renameArea(id: number, name: string) {
    editingArea.value = undefined
    patchArea(id, { name })
    await api.updateArea(id, { name })
  }

  async function colorArea(id: number, color: string) {
    patchArea(id, { color })
    await api.updateArea(id, { color })
  }

  /** Hides an area (or shows a hidden one again): its frame leaves the map, its devices stay. */
  async function hideArea(id: number) {
    const hidden = !areas.value.find((a) => a.id === id)?.hidden
    patchArea(id, { hidden })
    await api.updateArea(id, { hidden })
  }

  async function deleteArea(id: number) {
    areas.value = areas.value.filter((a) => a.id !== id)
    prefs.collapsedAreas = prefs.collapsedAreas.filter((x) => x !== id)
    await api.deleteArea(id)
  }

  // Drawing a new area: click "New area", then drag a rectangle on the map.
  const drawing = ref(false)

  /** A rectangle drawn on the screen becomes an area holding the devices inside it. */
  async function drawArea(start: Point, end: Point) {
    const r = rectFrom(options.toMap(start), options.toMap(end))
    const members = membersOf(r, [...nodeBoxes.value.values()])
    // A small rectangle is fine around a device (the area fits around it);
    // an empty one is probably a stray click: keep drawing.
    if (!members.length && (r.width < MIN_AREA_SIZE || r.height < MIN_AREA_SIZE)) return
    drawing.value = false
    const created = await api.createArea({
      name: t('map.areas.defaultName'),
      color: 'blue',
      direction: prefs.layoutDirection,
      x: Math.round(r.x),
      y: Math.round(r.y),
      width: Math.max(MIN_AREA_SIZE, Math.round(r.width)),
      height: Math.max(MIN_AREA_SIZE, Math.round(r.height)),
      members,
    })
    areas.value = [...areas.value, created]
    editingArea.value = created.id // name it right away
    await claim(members, created.id)
  }

  // Right click on an area title.
  const areaMenu = ref<{ x: number; y: number; id: number }>()
  const areaMenuArea = computed(() => areas.value.find((a) => a.id === areaMenu.value?.id))

  function previewArea(color?: string) {
    areaPreview.value = color && areaMenu.value ? { id: areaMenu.value.id, color } : undefined
  }
  function closeAreaMenu() {
    areaMenu.value = undefined
    areaPreview.value = undefined
  }

  function areaMenuAction(action: 'rename' | 'delete' | 'collapse' | 'hide' | { color: string }) {
    const id = areaMenu.value?.id
    areaMenu.value = undefined
    areaPreview.value = undefined
    if (id === undefined) return
    if (action === 'collapse') options.collapseArea(id)
    else if (action === 'rename') editingArea.value = id
    else if (action === 'delete') deleteArea(id)
    else if (action === 'hide') hideArea(id)
    else colorArea(id, action.color)
  }

  return {
    editingArea,
    areaBusy,
    visibleAreas,
    insetOf,
    onDragStart,
    onDrag,
    onDragStop,
    resizeArea,
    finishResize,
    renameArea,
    drawing,
    drawArea,
    areaMenu,
    areaMenuArea,
    previewArea,
    closeAreaMenu,
    areaMenuAction,
  }
}
