import type { ClientGroup, GraphView } from './graph'
import type { MapArea, Point, TopoEdge } from './types'

export const MIN_AREA_SIZE = 60

export interface Box {
  id: string
  x: number
  y: number
  width: number
  height: number
}

/** Flow node id of an area. */
export const areaNodeId = (id: number) => `area:${id}`

/** Area id of a flow node id, or undefined for other nodes. */
export function areaIdOf(nodeId: string): number | undefined {
  if (!nodeId.startsWith('area:')) return undefined
  const id = Number(nodeId.slice(5))
  return Number.isInteger(id) ? id : undefined
}

/** The rectangle between two corners, whatever direction it was drawn in. */
export function rectFrom(a: Point, b: Point) {
  return {
    x: Math.min(a.x, b.x),
    y: Math.min(a.y, b.y),
    width: Math.abs(a.x - b.x),
    height: Math.abs(a.y - b.y),
  }
}

/** Nodes that belong to an area: those whose center is inside it. */
export function membersOf(area: Pick<MapArea, 'x' | 'y' | 'width' | 'height'>, boxes: Box[]) {
  return boxes
    .filter((b) => {
      const cx = b.x + b.width / 2
      const cy = b.y + b.height / 2
      return cx >= area.x && cx <= area.x + area.width && cy >= area.y && cy <= area.y + area.height
    })
    .map((b) => b.id)
}

/** Space between an area's border and the nodes inside it. */
export const AREA_PADDING = 24
/** Height of an area's title, drawn above its border. */
export const AREA_TITLE = 30

type Rect = Pick<MapArea, 'x' | 'y' | 'width' | 'height'>

/**
 * The rectangle an area is drawn with: around its members on the map (so it
 * follows them when the map is laid out again), or the stored one when none
 * of them is on the map.
 */
export function fitArea(area: MapArea, boxes: Map<string, Box>): Rect {
  const members = area.members.map((id) => boxes.get(id)).filter((b): b is Box => !!b)
  if (!members.length) return { x: area.x, y: area.y, width: area.width, height: area.height }
  const left = Math.min(...members.map((b) => b.x))
  const top = Math.min(...members.map((b) => b.y))
  const right = Math.max(...members.map((b) => b.x + b.width))
  const bottom = Math.max(...members.map((b) => b.y + b.height))
  return {
    x: left - AREA_PADDING,
    y: top - AREA_PADDING,
    width: right - left + 2 * AREA_PADDING,
    height: bottom - top + 2 * AREA_PADDING,
  }
}

/**
 * New member list after nodes were dropped: a dropped node belongs to the area
 * when its center is inside the rectangle the area had before the drag.
 */
export function regroup(members: string[], rect: Rect, dropped: Box[]): string[] {
  const inside = new Set(membersOf(rect, dropped))
  const droppedIds = new Set(dropped.map((b) => b.id))
  const kept = members.filter((id) => !droppedIds.has(id) || inside.has(id))
  for (const id of inside) if (!kept.includes(id)) kept.push(id)
  return kept
}

/** The given nodes plus everything below them (apps, clients, VMs, group bubbles). */
export function withDescendants(ids: string[], edges: { source: string; target: string }[]) {
  const children = new Map<string, string[]>()
  for (const e of edges) children.set(e.source, [...(children.get(e.source) ?? []), e.target])
  const out = new Set<string>()
  const stack = [...ids]
  while (stack.length) {
    const id = stack.pop()!
    if (out.has(id)) continue
    out.add(id)
    stack.push(...(children.get(id) ?? []))
  }
  return [...out]
}

/** Node id of a collapsed area's bubble. */
export const areaBubbleId = (id: number) => `area-bubble:${id}`

/**
 * Collapses areas into one bubble each: their devices (and everything below
 * them) leave the map, links from outside go to the bubble instead.
 */
export function collapseAreas(view: GraphView, areas: MapArea[]): GraphView {
  if (!areas.length) return view
  let nodes = view.nodes
  let edges: TopoEdge[] = view.edges
  let groups = view.groups
  for (const a of areas) {
    const inside = new Set(withDescendants(a.members, edges))
    const members = nodes.filter((n) => inside.has(n.id))
    const memberGroups = groups.filter((g) => inside.has(g.id))
    if (!members.length) continue
    const bubble = areaBubbleId(a.id)
    const outsideParents = edges
      .filter((e) => inside.has(e.target) && !inside.has(e.source))
      .map((e) => e.source)
    const group: ClientGroup = {
      id: bubble,
      parentId: outsideParents[0] ?? '',
      clients: [...members, ...memberGroups.flatMap((g) => g.clients)],
      online:
        members.filter((n) => n.online).length + memberGroups.reduce((sum, g) => sum + g.online, 0),
      area: { id: a.id, name: a.name, color: a.color },
    }
    const seen = new Set<string>()
    const next: TopoEdge[] = []
    for (const e of edges) {
      const s = inside.has(e.source)
      const t = inside.has(e.target)
      if (s && t) continue
      const edge =
        s || t ? { ...e, source: s ? bubble : e.source, target: t ? bubble : e.target } : e
      if (s || t) {
        edge.id = `e:${edge.source}|${edge.target}`
        delete edge.source_port
        delete edge.target_port
        if (seen.has(edge.id)) continue
        seen.add(edge.id)
      }
      next.push(edge)
    }
    nodes = nodes.filter((n) => !inside.has(n.id))
    groups = [...groups.filter((g) => !inside.has(g.id)), group]
    edges = next
  }
  return { nodes, edges, groups }
}
