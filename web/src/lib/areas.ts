import type { MapArea, Point } from './types'

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
