import type { ELK as ElkInstance } from 'elkjs/lib/elk-api'

import type { Point } from './types'

export interface LayoutNode {
  id: string
  width: number
  height: number
}

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
 * Computes a left-to-right layered layout (firewall/router on the left, clients
 * stacked on the right, which keeps wide networks readable). Saved
 * positions (from the user dragging nodes) take precedence.
 */
export async function layout(
  nodes: LayoutNode[],
  edges: LayoutEdge[],
  saved: Record<string, Point> = {},
): Promise<Record<string, Point>> {
  const ids = new Set(nodes.map((n) => n.id))
  const graph = await (
    await getElk()
  ).layout({
    id: 'root',
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': 'RIGHT',
      'elk.layered.spacing.nodeNodeBetweenLayers': '110',
      'elk.spacing.nodeNode': '14',
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
