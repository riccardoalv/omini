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
