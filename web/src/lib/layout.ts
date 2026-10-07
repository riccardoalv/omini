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

/** Nodes laid out together inside a box (a map area), so no other node lands among them. */
export interface LayoutGroup {
  id: string
  children: string[]
  /** Space between the box and its nodes: top, right, bottom, left. */
  padding: [number, number, number, number]
}

/**
 * Computes a layered layout in the given direction. Saved positions (from the
 * user dragging nodes) take precedence. Groups become boxes laid out with the
 * rest of the graph (edges cross their border); a node belongs to one group.
 */
export async function layout(
  nodes: LayoutNode[],
  edges: LayoutEdge[],
  saved: Record<string, Point> = {},
  direction: Direction = 'RIGHT',
  groups: LayoutGroup[] = [],
): Promise<Record<string, Point>> {
  const ids = new Set(nodes.map((n) => n.id))
  const groupOf = new Map<string, LayoutGroup>()
  for (const g of groups) {
    for (const c of g.children) if (ids.has(c) && !groupOf.has(c)) groupOf.set(c, g)
  }
  const box = (n: LayoutNode) => ({ id: n.id, width: n.width, height: n.height })
  const boxes = groups
    .map((g) => ({
      id: `layout-group:${g.id}`,
      layoutOptions: {
        'elk.padding': `[top=${g.padding[0]},left=${g.padding[3]},bottom=${g.padding[2]},right=${g.padding[1]}]`,
      },
      children: nodes.filter((n) => groupOf.get(n.id) === g).map(box),
    }))
    .filter((g) => g.children.length > 0)
  const graph = await (
    await getElk()
  ).layout({
    id: 'root',
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': direction,
      'elk.hierarchyHandling': 'INCLUDE_CHILDREN', // one layout across the area boxes
      'elk.layered.spacing.nodeNodeBetweenLayers': direction === 'RIGHT' ? '110' : '80',
      'elk.spacing.nodeNode': direction === 'RIGHT' ? '14' : '24',
      'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
      'elk.layered.nodePlacement.bk.fixedAlignment': 'BALANCED', // parents centered over children
      'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
    },
    children: [...nodes.filter((n) => !groupOf.has(n.id)).map(box), ...boxes],
    edges: edges
      .filter((e) => ids.has(e.source) && ids.has(e.target))
      .map((e) => ({ id: e.id, sources: [e.source], targets: [e.target] })),
  })
  const out: Record<string, Point> = {}
  for (const c of graph.children ?? []) {
    if (c.children?.length) {
      // Positions inside a box are relative to it.
      for (const cc of c.children) {
        out[cc.id] = saved[cc.id] ?? { x: (c.x ?? 0) + (cc.x ?? 0), y: (c.y ?? 0) + (cc.y ?? 0) }
      }
    } else if (!c.id.startsWith('layout-group:')) {
      out[c.id] = saved[c.id] ?? { x: c.x ?? 0, y: c.y ?? 0 }
    }
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

/**
 * Shifts a fresh layout so the anchor (the node the user expanded or
 * collapsed) stays exactly where it is on screen. The rest of the map is laid
 * out again around it, so new nodes never overlap old ones. Positions the user
 * saved (`fixed`) are absolute and never shifted.
 */
export function alignOn(
  current: Record<string, Point>,
  fresh: Record<string, Point>,
  anchor: string | undefined,
  fixed: Set<string> = new Set(),
): Record<string, Point> {
  const a = anchor ? current[anchor] : undefined
  const f = anchor ? fresh[anchor] : undefined
  if (!a || !f || fixed.has(anchor!)) return fresh
  const dx = a.x - f.x
  const dy = a.y - f.y
  const out: Record<string, Point> = {}
  for (const [id, p] of Object.entries(fresh)) {
    out[id] = fixed.has(id) ? p : { x: p.x + dx, y: p.y + dy }
  }
  return out
}
