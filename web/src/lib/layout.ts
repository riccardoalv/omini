import type { ELK as ElkInstance } from 'elkjs/lib/elk-api'

import type { Point } from './types'

export interface LayoutNode {
  id: string
  width: number
  height: number
}

/** RIGHT: firewall on the left, clients stacked on the right. DOWN: top-down tree. */
export type Direction = 'RIGHT' | 'DOWN'

export interface LayoutEdge {
  id: string
  source: string
  target: string
}

// ELK is large (~1.4 MB): load it on first use, in its own cacheable chunk.
let elk: Promise<ElkInstance> | undefined
function getElk(): Promise<ElkInstance> {
  elk ??= import('elkjs/lib/elk.bundled.js').then((m) => new m.default())
  return elk
}

/**
 * Computes a layered layout in the given direction. Saved positions (from the
 * user dragging nodes) take precedence.
 */
export async function layout(
  nodes: LayoutNode[],
  edges: LayoutEdge[],
  saved: Record<string, Point> = {},
  direction: Direction = 'RIGHT',
): Promise<Record<string, Point>> {
  const ids = new Set(nodes.map((n) => n.id))
  const graph = await (
    await getElk()
  ).layout({
    id: 'root',
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': direction,
      'elk.layered.spacing.nodeNodeBetweenLayers': direction === 'RIGHT' ? '110' : '80',
      'elk.spacing.nodeNode': direction === 'RIGHT' ? '14' : '24',
      'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
      'elk.layered.nodePlacement.bk.fixedAlignment': 'BALANCED', // parents centered over children
      'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
    },
    children: nodes.map((n) => ({ id: n.id, width: n.width, height: n.height })),
    edges: edges
      .filter((e) => ids.has(e.source) && ids.has(e.target))
      .map((e) => ({ id: e.id, sources: [e.source], targets: [e.target] })),
  })
  const out: Record<string, Point> = {}
  for (const c of graph.children ?? []) {
    out[c.id] = saved[c.id] ?? { x: c.x ?? 0, y: c.y ?? 0 }
  }
  return out
}

/** Saved positions are kept per direction, so switching does not mix arrangements. */
export const layoutKey = (direction: Direction, id: string) =>
  direction === 'RIGHT' ? id : `${direction}:${id}`

/** Extracts the saved positions of one direction from the server layout. */
export function positionsFor(
  saved: Record<string, Point>,
  direction: Direction,
): Record<string, Point> {
  const out: Record<string, Point> = {}
  const prefix = `${direction}:`
  for (const [key, point] of Object.entries(saved)) {
    if (direction === 'RIGHT') {
      if (!key.startsWith('DOWN:')) out[key] = point
    } else if (key.startsWith(prefix)) {
      out[key.slice(prefix.length)] = point
    }
  }
  return out
}
