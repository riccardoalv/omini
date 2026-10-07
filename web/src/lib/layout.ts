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

/** A parent's leaf children laid out as a grid, standing in for them in ELK as one node. */
interface Pack {
  id: string
  leaves: LayoutNode[]
  cols: number
  cell: { width: number; height: number }
}

const PACK_MIN = 4 // fewer leaves stay in the row
const PACK_GAP = { x: 14, y: 10 }

/**
 * Top-down maps of homelabs are very wide: a router with 15 clients or a VM
 * with 17 apps puts them all in one row. Leaf children (nothing below them)
 * of a parent are packed into a compact grid under it instead. Nodes with
 * children of their own stay in the tree.
 */
export function packLeaves(
  nodes: LayoutNode[],
  edges: LayoutEdge[],
  groupOf: Map<string, LayoutGroup>,
): { nodes: LayoutNode[]; edges: LayoutEdge[]; packs: Pack[] } {
  const ids = new Set(nodes.map((n) => n.id))
  const valid = edges.filter((e) => ids.has(e.source) && ids.has(e.target))
  const outDegree = new Map<string, number>()
  const parents = new Map<string, string[]>()
  for (const e of valid) {
    outDegree.set(e.source, (outDegree.get(e.source) ?? 0) + 1)
    parents.set(e.target, [...(parents.get(e.target) ?? []), e.source])
  }
  // Leaves with a single parent, by parent and area (a pack never crosses an area border).
  const byParent = new Map<string, LayoutNode[]>()
  for (const n of nodes) {
    const ps = parents.get(n.id)
    if (outDegree.get(n.id) || ps?.length !== 1) continue
    const key = `${ps[0]}|${groupOf.get(n.id)?.id ?? ''}`
    byParent.set(key, [...(byParent.get(key) ?? []), n])
  }
  const packs: Pack[] = []
  const packed = new Map<string, string>() // leaf → pack id
  for (const [key, leaves] of byParent) {
    if (leaves.length < PACK_MIN) continue
    const pack: Pack = {
      id: `pack:${key}`,
      leaves,
      cols: Math.ceil(Math.sqrt(leaves.length * 1.2)),
      cell: {
        width: Math.max(...leaves.map((l) => l.width)),
        height: Math.max(...leaves.map((l) => l.height)),
      },
    }
    packs.push(pack)
    for (const l of leaves) packed.set(l.id, pack.id)
    const group = groupOf.get(leaves[0]!.id)
    if (group) groupOf.set(pack.id, group)
  }
  const size = (p: Pack) => {
    const rows = Math.ceil(p.leaves.length / p.cols)
    return {
      width: p.cols * p.cell.width + (p.cols - 1) * PACK_GAP.x,
      height: rows * p.cell.height + (rows - 1) * PACK_GAP.y,
    }
  }
  const seen = new Set<string>()
  const outEdges: LayoutEdge[] = []
  for (const e of valid) {
    const target = packed.get(e.target) ?? e.target
    const key = `${e.source}|${target}`
    if (seen.has(key)) continue
    seen.add(key)
    outEdges.push(target === e.target ? e : { id: `e:${key}`, source: e.source, target })
  }
  return {
    nodes: [
      ...nodes.filter((n) => !packed.has(n.id)),
      ...packs.map((p) => ({ id: p.id, ...size(p) })),
    ],
    edges: outEdges,
    packs,
  }
}

/** Positions of packed leaves: row by row inside their grid, centered in their cell. */
function unpack(packs: Pack[], out: Record<string, Point>, saved: Record<string, Point>) {
  for (const p of packs) {
    const origin = out[p.id]
    delete out[p.id]
    if (!origin) continue
    p.leaves.forEach((l, i) => {
      const col = i % p.cols
      const row = Math.floor(i / p.cols)
      out[l.id] = saved[l.id] ?? {
        x: origin.x + col * (p.cell.width + PACK_GAP.x) + (p.cell.width - l.width) / 2,
        y: origin.y + row * (p.cell.height + PACK_GAP.y),
      }
    })
  }
}

/**
 * Computes a layered layout in the given direction. Saved positions (from the
 * user dragging nodes) take precedence. Groups become boxes laid out with the
 * rest of the graph (edges cross their border); a node belongs to one group.
 * Top down, leaf children are packed into grids (see packLeaves).
 */
export async function layout(
  allNodes: LayoutNode[],
  allEdges: LayoutEdge[],
  saved: Record<string, Point> = {},
  direction: Direction = 'RIGHT',
  groups: LayoutGroup[] = [],
): Promise<Record<string, Point>> {
  const groupOf = new Map<string, LayoutGroup>()
  const all = new Set(allNodes.map((n) => n.id))
  for (const g of groups) {
    for (const c of g.children) if (all.has(c) && !groupOf.has(c)) groupOf.set(c, g)
  }
  const { nodes, edges, packs } =
    direction === 'DOWN'
      ? packLeaves(allNodes, allEdges, groupOf)
      : { nodes: allNodes, edges: allEdges, packs: [] }
  const ids = new Set(nodes.map((n) => n.id))
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
      // Room for the traffic badge on top of a node.
      'elk.spacing.nodeNode': direction === 'RIGHT' ? '22' : '24',
      'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
      'elk.layered.nodePlacement.bk.fixedAlignment': 'BALANCED', // parents centered over children
      'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
      // Siblings keep their order (grouped by port or Wi-Fi network, see modelOrder).
      'elk.layered.crossingMinimization.forceNodeModelOrder': 'true',
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
  unpack(packs, out, saved)
  return clearSaved(out, allNodes, saved, direction)
}

const GAP = 12

/**
 * Positions the user dragged stay where they are, but the automatic layout
 * does not know them: automatic nodes that would land on a dragged one (or on
 * a node pushed before them) move along the row/column until they are clear.
 */
export function clearSaved(
  pos: Record<string, Point>,
  nodes: LayoutNode[],
  saved: Record<string, Point>,
  direction: Direction,
): Record<string, Point> {
  const size = new Map(nodes.map((n) => [n.id, n]))
  const box = (id: string, p: Point) => ({
    ...p,
    w: size.get(id)?.width ?? 0,
    h: size.get(id)?.height ?? 0,
  })
  const occupied = Object.keys(saved)
    .filter((id) => pos[id] && size.has(id))
    .map((id) => box(id, pos[id]!))
  if (!occupied.length) return pos
  const cross = direction === 'RIGHT' ? 'y' : 'x'
  const overlaps = (a: ReturnType<typeof box>, b: ReturnType<typeof box>) =>
    a.x < b.x + b.w + GAP && a.x + a.w + GAP > b.x && a.y < b.y + b.h + GAP && a.y + a.h + GAP > b.y
  const out = { ...pos }
  const auto = Object.keys(pos)
    .filter((id) => !saved[id] && size.has(id))
    .sort((a, b) => pos[a]![cross] - pos[b]![cross])
  for (const id of auto) {
    const b = box(id, out[id]!)
    for (let moved = true, guard = 0; moved && guard < 200; guard++) {
      moved = false
      for (const o of occupied) {
        if (overlaps(b, o)) {
          b[cross] = cross === 'y' ? o.y + o.h + GAP : o.x + o.w + GAP
          moved = true
        }
      }
    }
    out[id] = { x: b.x, y: b.y }
    occupied.push(b)
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

interface OrderEdge {
  source: string
  target: string
  source_port?: string
}

/**
 * The order nodes are given to the layout (which keeps it among siblings):
 * a walk from the roots where each node's children are grouped by the port or
 * Wi-Fi network they hang from ("2.4 GHz" ones together, then "5 GHz"), then
 * by name — so a link's pill sits over the devices that use it.
 */
export function modelOrder(
  ids: string[],
  edges: OrderEdge[],
  label: (id: string) => string,
): string[] {
  const known = new Set(ids)
  const children = new Map<string, OrderEdge[]>()
  const hasParent = new Set<string>()
  for (const e of edges) {
    if (!known.has(e.source) || !known.has(e.target)) continue
    children.set(e.source, [...(children.get(e.source) ?? []), e])
    hasParent.add(e.target)
  }
  const byName = (a: string, b: string) =>
    label(a).localeCompare(label(b), undefined, { numeric: true, sensitivity: 'base' }) ||
    a.localeCompare(b)
  const out: string[] = []
  const seen = new Set<string>()
  const visit = (id: string) => {
    if (seen.has(id)) return
    seen.add(id)
    out.push(id)
    const kids = [...(children.get(id) ?? [])].sort(
      (x, y) =>
        (x.source_port ?? '').localeCompare(y.source_port ?? '', undefined, { numeric: true }) ||
        byName(x.target, y.target),
    )
    for (const e of kids) visit(e.target)
  }
  for (const id of ids.filter((i) => !hasParent.has(i)).sort(byName)) visit(id)
  for (const id of ids) visit(id) // cycles or anything unreached
  return out
}
