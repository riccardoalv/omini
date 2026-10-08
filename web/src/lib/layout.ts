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

/** Nodes laid out together inside a box (a map area), so no other node lands among them. */
export interface LayoutGroup {
  id: string
  children: string[]
  /** Space between the box and its nodes: top, right, bottom, left. */
  padding: [number, number, number, number]
}

/** A parent's leaf children laid out as a grid, standing in for them in the layout as one node. */
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
  // Each grid takes the place of its first leaf: the layout keeps the order it
  // is given among siblings, so a grid at the end landed far from its parent.
  const byId = new Map(packs.map((p) => [p.id, p]))
  const ordered: LayoutNode[] = []
  const placed = new Set<string>()
  for (const n of nodes) {
    const packId = packed.get(n.id)
    if (!packId) ordered.push(n)
    else if (!placed.has(packId)) {
      placed.add(packId)
      ordered.push({ id: packId, ...size(byId.get(packId)!) })
    }
  }
  return { nodes: ordered, edges: outEdges, packs }
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

/** Space between columns (left to right) or rows (top down): room for a link's pill and a node's traffic badge. */
export const LAYER_GAP: Record<Direction, number> = { RIGHT: 150, DOWN: 100 }
/** Space between siblings, across: above a node sits its traffic badge (left to right). */
const SIBLING_GAP: Record<Direction, number> = { RIGHT: 22, DOWN: 24 }
/** A device's traffic badge is taller than a client's: more room above it, left to right. */
const DEVICE_BADGE_EXTRA = 6
const DEVICE_HEIGHT = 50

/**
 * A tidy tree: every level in one column (left to right) or row (top down),
 * every subtree in a band of its own across, a parent centered on its
 * children. Siblings in the same area are kept together and their band gets
 * the area's padding, so an area's box never takes in another node; a level
 * where an area starts gets the room for its border and title too. A node with
 * several parents hangs from the first; parents with nothing else below them
 * (a second WAN) stand next to that one.
 */
function treeLayout(
  nodes: LayoutNode[],
  edges: LayoutEdge[],
  groupOf: Map<string, LayoutGroup>,
  direction: Direction,
): Record<string, Point> {
  const right = direction === 'RIGHT'
  const main = (n: LayoutNode) => (right ? n.width : n.height)
  const cross = (n: LayoutNode) => (right ? n.height : n.width)
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const order = new Map(nodes.map((n, i) => [n.id, i]))

  const parents = new Map<string, string[]>()
  const outgoing = new Map<string, string[]>()
  for (const e of edges) {
    if (!byId.has(e.source) || !byId.has(e.target) || e.source === e.target) continue
    parents.set(e.target, [...(parents.get(e.target) ?? []), e.source])
    outgoing.set(e.source, [...(outgoing.get(e.source) ?? []), e.target])
  }
  // A node without links in an area stands with the area's other nodes (as a
  // sibling of the first that hangs from something), not apart, which would
  // stretch the area's box over everything between.
  for (const n of nodes) {
    const g = groupOf.get(n.id)
    if (!g || parents.has(n.id) || outgoing.has(n.id)) continue
    const mate = nodes.find((m) => m.id !== n.id && groupOf.get(m.id) === g && parents.has(m.id))
    if (!mate) continue
    const p = parents.get(mate.id)![0]!
    parents.set(n.id, [p])
    outgoing.set(p, [...(outgoing.get(p) ?? []), n.id])
  }
  // Parents with nothing but this child and no parent of their own stand next
  // to the child's first parent instead of starting a tree of their own.
  const companions = new Map<string, string[]>()
  const companion = new Set<string>()
  for (const ps of parents.values()) {
    for (const p of ps.slice(1)) {
      if (parents.has(p) || (outgoing.get(p) ?? []).length !== 1 || companion.has(p)) continue
      companions.set(ps[0]!, [...(companions.get(ps[0]!) ?? []), p])
      companion.add(p)
    }
  }

  // The tree: a walk from the roots in model order, each node under its first parent.
  const children = new Map<string, string[]>()
  const depth = new Map<string, number>()
  const roots: string[] = []
  const visit = (root: string) => {
    if (depth.has(root) || companion.has(root)) return
    roots.push(root)
    depth.set(root, 0)
    const queue = [root]
    while (queue.length) {
      const id = queue.shift()!
      const kids = (outgoing.get(id) ?? [])
        .filter((c) => !depth.has(c) && !companion.has(c))
        .filter((c, i, all) => all.indexOf(c) === i)
        .sort((a, b) => order.get(a)! - order.get(b)!)
      for (const c of kids) depth.set(c, depth.get(id)! + 1)
      children.set(id, kids)
      queue.push(...kids)
    }
  }
  for (const n of nodes)
    if (!parents.has(n.id) || parents.get(n.id)!.every((p) => companion.has(p))) visit(n.id)
  for (const n of nodes) visit(n.id) // cycles
  for (const [p, cs] of companions) for (const c of cs) depth.set(c, depth.get(p) ?? 0)

  // Siblings of one area next to each other, where the first of them was.
  const parentOf = new Map<string, string>()
  for (const [p, cs] of children) for (const c of cs) parentOf.set(c, p)
  const enters = (id: string) => {
    const g = groupOf.get(id)
    const p = parentOf.get(id)
    return g && (!p || groupOf.get(p) !== g) ? g : undefined
  }
  for (const [p, cs] of children) {
    const runs: string[][] = []
    const runOf = new Map<LayoutGroup, string[]>()
    for (const c of cs) {
      const g = enters(c)
      if (g && runOf.has(g)) runOf.get(g)!.push(c)
      else {
        const run = [c]
        if (g) runOf.set(g, run)
        runs.push(run)
      }
    }
    children.set(p, runs.flat())
  }

  // Columns (rows): each level as wide as its widest node, plus the gap; a
  // level where an area starts also leaves room for its border (and title).
  const levels = Math.max(0, ...depth.values()) + 1
  const size = Array.from({ length: levels }, () => 0)
  const extra = Array.from({ length: levels }, () => 0)
  for (const [id, d] of depth) {
    size[d] = Math.max(size[d]!, main(byId.get(id)!))
    const g = enters(id)
    if (g) extra[d] = Math.max(extra[d]!, right ? g.padding[3] : g.padding[0])
  }
  const at = Array.from({ length: levels }, () => 0)
  for (let d = 1; d < levels; d++)
    at[d] = at[d - 1]! + size[d - 1]! + LAYER_GAP[direction] + extra[d]!

  // Across: each subtree in its own band; a run of siblings entering an area
  // gets the area's padding (and title, left to right) around it.
  const gapBefore = (n: LayoutNode) =>
    SIBLING_GAP[direction] + (right && n.height >= DEVICE_HEIGHT ? DEVICE_BADGE_EXTRA : 0)
  const center = new Map<string, number>()
  const place = (id: string, start: number): number => {
    const mates = [id, ...(companions.get(id) ?? [])].map((m) => byId.get(m)!)
    const own =
      mates.reduce((s, m) => s + cross(m), 0) + mates.slice(1).reduce((s, m) => s + gapBefore(m), 0)
    const kids = children.get(id) ?? []
    // Children, run by run.
    let cursor = 0
    const kidCenters: number[] = []
    const layoutKids = (offset: number) => {
      cursor = 0
      kidCenters.length = 0
      let i = 0
      while (i < kids.length) {
        const g = enters(kids[i]!)
        let j = i + 1
        while (g && j < kids.length && enters(kids[j]!) === g) j++
        const [before, after] = g
          ? right
            ? [g.padding[0], g.padding[2]]
            : [g.padding[3], g.padding[1]]
          : [0, 0]
        if (cursor > 0) cursor += gapBefore(byId.get(kids[i]!)!)
        cursor += before
        for (let k = i; k < j; k++) {
          if (k > i) cursor += gapBefore(byId.get(kids[k]!)!)
          cursor += place(kids[k]!, offset + cursor)
          kidCenters.push(center.get(kids[k]!)!)
        }
        cursor += after
        i = j
      }
    }
    layoutKids(start)
    const span = cursor
    if (own > span) {
      // A parent wider than its children: they are centered under it.
      layoutKids(start + (own - span) / 2)
      center.set(id, start + own / 2)
      return own
    }
    const c = kids.length
      ? (kidCenters[0]! + kidCenters[kidCenters.length - 1]!) / 2
      : start + own / 2
    // Centered on its children, but never out of its band.
    center.set(id, Math.min(Math.max(c, start + own / 2), start + span - own / 2))
    return Math.max(own, span)
  }
  let cursor = 0
  for (const r of roots) {
    if (cursor > 0) cursor += gapBefore(byId.get(r)!) + SIBLING_GAP[direction]
    const g = enters(r)
    const [before, after] = g
      ? right
        ? [g.padding[0], g.padding[2]]
        : [g.padding[3], g.padding[1]]
      : [0, 0]
    cursor += before
    cursor += place(r, cursor)
    cursor += after
  }

  const out: Record<string, Point> = {}
  const put = (id: string, c: number) => {
    const n = byId.get(id)!
    const m = at[depth.get(id) ?? 0]!
    const x = c - cross(n) / 2
    out[id] = right ? { x: m, y: x } : { x, y: m }
  }
  for (const [id, c] of center) {
    const mates = [id, ...(companions.get(id) ?? [])]
    if (mates.length === 1) {
      put(id, c)
      continue
    }
    // The parent and its companions, stacked around the band's center.
    const total =
      mates.reduce((s, m) => s + cross(byId.get(m)!), 0) +
      mates.slice(1).reduce((s, m) => s + gapBefore(byId.get(m)!), 0)
    let p = c - total / 2
    mates.forEach((m, i) => {
      if (i > 0) p += gapBefore(byId.get(m)!)
      put(m, p + cross(byId.get(m)!) / 2)
      p += cross(byId.get(m)!)
    })
  }
  return out
}

/**
 * Lays the map out as a tree in the given direction (see treeLayout). Saved
 * positions (from the user dragging nodes) take precedence. Groups (areas)
 * keep their nodes together; a node belongs to one group. Top down, leaf
 * children are packed into grids (see packLeaves).
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
  const tree = treeLayout(nodes, edges, groupOf, direction)
  // A node in an area is placed with its area (its band), not where it was
  // dragged once: a few dragged members stretched the area over the map.
  // Only an area moved as a whole (every member dragged with it) keeps them.
  const keep = new Set<LayoutGroup>()
  for (const g of groups) if (g.children.every((c) => !all.has(c) || saved[c])) keep.add(g)
  const free: Record<string, Point> = {}
  for (const [id, p] of Object.entries(saved)) {
    const g = groupOf.get(id)
    if (!g || keep.has(g)) free[id] = p
  }
  const out: Record<string, Point> = {}
  for (const [id, p] of Object.entries(tree)) out[id] = id.startsWith('pack:') ? p : (free[id] ?? p)
  unpack(packs, out, free)
  return clearSaved(out, allNodes, free, direction)
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
