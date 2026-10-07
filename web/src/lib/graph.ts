import type { TopoEdge, TopoNode } from './types'

/** A collapsed set of clients attached to the same parent. */
export interface ClientGroup {
  id: string
  parentId: string
  clients: TopoNode[]
  online: number
}

export interface GraphView {
  nodes: TopoNode[]
  edges: TopoEdge[]
  groups: ClientGroup[]
}

export const groupId = (parentId: string) => `grp:${parentId}`

/** Infrastructure is never collapsed into a client group. */
const infrastructure = new Set([
  'firewall',
  'router',
  'switch',
  'ap',
  'server',
  'nas',
  'hypervisor',
  'virtual_machine',
])

/** Pinned devices, homelab software and infrastructure always stay on the map. */
export const alwaysVisible = (n: TopoNode) =>
  !!n.pinned || !!n.product || infrastructure.has(n.type ?? '')

/**
 * Collapses clients into a group bubble when a parent (AP, switch, segment)
 * has more than `threshold` of them, unless the user expanded that parent.
 * Pinned clients, homelab software and infrastructure always stay visible.
 */
export function collapseClients(
  nodes: TopoNode[],
  edges: TopoEdge[],
  threshold: number,
  expanded: ReadonlySet<string>,
): GraphView {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const children = new Map<string, TopoNode[]>()
  for (const e of edges) {
    const target = byId.get(e.target)
    if (target?.kind !== 'client') continue
    const list = children.get(e.source) ?? []
    list.push(target)
    children.set(e.source, list)
  }

  const hidden = new Set<string>()
  const groups: ClientGroup[] = []
  for (const [parentId, clients] of children) {
    if (clients.length <= threshold || expanded.has(parentId)) continue
    const collapsible = clients.filter((c) => !alwaysVisible(c))
    if (collapsible.length < 2) continue
    for (const c of collapsible) hidden.add(c.id)
    groups.push({
      id: groupId(parentId),
      parentId,
      clients: collapsible,
      online: collapsible.filter((c) => c.online).length,
    })
  }

  return {
    nodes: nodes.filter((n) => !hidden.has(n.id)),
    edges: [
      ...edges.filter((e) => !hidden.has(e.target) && !hidden.has(e.source)),
      ...groups.map((g) => ({
        id: `e:${g.parentId}|${g.id}`,
        source: g.parentId,
        target: g.id,
        kind: 'inferred' as const,
      })),
    ],
    groups,
  }
}

export interface EdgeLook {
  width: number
  dashed: boolean
  dotted: boolean
  /** An infrastructure link negotiated below 1 Gbps: likely a bad cable or port. */
  slow: boolean
}

/** Visual style of a link: thicker for faster links, dashed when inferred, dotted for Wi-Fi. */
export function edgeLook(edge: TopoEdge, source?: TopoNode, target?: TopoNode): EdgeLook {
  const speed = edge.speed_mbps ?? 0
  const infra = source?.kind !== 'client' && target?.kind !== 'client' && target?.kind !== undefined
  let width = 1.5
  if (speed >= 10000) width = 4
  else if (speed >= 2500) width = 3
  else if (speed >= 1000) width = 2
  return {
    width,
    dashed: edge.kind === 'inferred',
    dotted: edge.kind === 'wifi',
    slow: infra && speed > 0 && speed < 1000,
  }
}

/** Direct children of a node (clients, segments, downstream devices). */
export function childrenOf(id: string, nodes: TopoNode[], edges: TopoEdge[]): TopoNode[] {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  return edges
    .filter((e) => e.source === id)
    .map((e) => byId.get(e.target))
    .filter((n): n is TopoNode => n !== undefined)
}

/** The node at the other end of a device port, if any. */
export function linkOnPort(
  id: string,
  port: string,
  nodes: TopoNode[],
  edges: TopoEdge[],
): TopoNode | undefined {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  for (const e of edges) {
    if (e.source === id && e.source_port === port) return byId.get(e.target)
    if (e.target === id && e.target_port === port) return byId.get(e.source)
  }
  return undefined
}
