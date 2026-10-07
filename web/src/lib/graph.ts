import type { TopoEdge, TopoNode } from './types'

/** A collapsed set of clients attached to the same parent. */
export interface ClientGroup {
  id: string
  parentId: string
  clients: TopoNode[]
  online: number
  /** A collapsed map area (its devices in one bubble). */
  area?: { id: number; name: string; color: string }
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
  !!n.pinned || (n.kind !== 'app' && (!!n.product || infrastructure.has(n.type ?? '')))

/**
 * Collapses clients into a group bubble when a parent (AP, switch, segment)
 * has more than `threshold` of them, unless the user expanded that parent.
 * Pinned clients, homelab software and infrastructure always stay visible.
 * Parents in `forced` (collapsed by the user) group all their clients except pinned ones.
 */
export function collapseClients(
  nodes: TopoNode[],
  edges: TopoEdge[],
  threshold: number,
  expanded: ReadonlySet<string>,
  forced: ReadonlySet<string> = new Set(),
): GraphView {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const children = new Map<string, TopoNode[]>()
  for (const e of edges) {
    const target = byId.get(e.target)
    if (target?.kind !== 'client' && target?.kind !== 'app') continue
    const list = children.get(e.source) ?? []
    list.push(target)
    children.set(e.source, list)
  }

  const hidden = new Set<string>()
  const groups: ClientGroup[] = []
  for (const [parentId, clients] of children) {
    // The user can collapse every client of a node (context menu), whatever the count.
    const force = forced.has(parentId)
    if (!force && (clients.length <= threshold || expanded.has(parentId))) continue
    const collapsible = clients.filter((c) => (force ? !c.pinned : !alwaysVisible(c)))
    if (collapsible.length < 2) continue
    for (const c of collapsible) hidden.add(c.id)
    groups.push({
      id: groupId(parentId),
      parentId,
      clients: collapsible,
      online: collapsible.filter((c) => c.online).length,
    })
  }

  // A hidden node takes everything below it (a collapsed Proxmox hides its
  // VMs and their apps), including the groups those had.
  const below = new Map<string, string[]>()
  for (const e of edges) below.set(e.source, [...(below.get(e.source) ?? []), e.target])
  const stack = [...hidden]
  while (stack.length) {
    for (const child of below.get(stack.pop()!) ?? []) {
      if (!hidden.has(child)) {
        hidden.add(child)
        stack.push(child)
      }
    }
  }
  const shown = groups.filter((g) => !hidden.has(g.parentId))

  return {
    nodes: nodes.filter((n) => !hidden.has(n.id)),
    edges: [
      ...edges.filter((e) => !hidden.has(e.target) && !hidden.has(e.source)),
      ...shown.map((g) => ({
        id: `e:${g.parentId}|${g.id}`,
        source: g.parentId,
        target: g.id,
        kind: 'inferred' as const,
      })),
    ],
    groups: shown,
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

/** Number of children of a node that can be grouped (clients and apps). */
export function clientCount(id: string, nodes: TopoNode[], edges: TopoEdge[]): number {
  return childrenOf(id, nodes, edges).filter((n) => n.kind === 'client' || n.kind === 'app').length
}

/** Removes offline nodes and the links that lead to them. */
/** Removes the devices the user hid, and everything below them. */
export function withoutHidden(nodes: TopoNode[], edges: TopoEdge[]) {
  const hidden = new Set(nodes.filter((n) => n.hidden).map((n) => n.id))
  const count = hidden.size
  const below = new Map<string, string[]>()
  for (const e of edges) below.set(e.source, [...(below.get(e.source) ?? []), e.target])
  const stack = [...hidden]
  while (stack.length) {
    for (const child of below.get(stack.pop()!) ?? []) {
      if (!hidden.has(child)) {
        hidden.add(child)
        stack.push(child)
      }
    }
  }
  return {
    nodes: nodes.filter((n) => !hidden.has(n.id)),
    edges: edges.filter((e) => !hidden.has(e.source) && !hidden.has(e.target)),
    hidden: count,
  }
}

export function withoutOffline(nodes: TopoNode[], edges: TopoEdge[]) {
  const offline = new Set(nodes.filter((n) => !n.online).map((n) => n.id))
  return {
    nodes: nodes.filter((n) => !offline.has(n.id)),
    edges: edges.filter((e) => !offline.has(e.source) && !offline.has(e.target)),
    hidden: offline.size,
  }
}

/** Id of a Wi-Fi network's mini node: "wifi:<access point>:<network>". */
export const isWifiNetwork = (id: string) => id.startsWith('wifi:')

/**
 * Puts each Wi-Fi network of an access point between it and its clients, as a
 * mini node ("IOT · 2.4 GHz"): AP → network → clients. Only visual (not in
 * the inventory, not clickable); the clients keep the AP as their parent.
 */
export function withWifiNetworks(
  nodes: TopoNode[],
  edges: TopoEdge[],
): { nodes: TopoNode[]; edges: TopoEdge[] } {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const networks = new Map<string, TopoNode>()
  const out: TopoEdge[] = []
  for (const e of edges) {
    if (e.kind !== 'wifi' || !e.source_port) {
      out.push(e)
      continue
    }
    const id = `wifi:${e.source}:${e.source_port}`
    if (!networks.has(id)) {
      networks.set(id, {
        id,
        kind: 'ssid',
        label: e.source_port,
        online: true,
        parent_id: e.source,
        band: byId.get(e.target)?.band,
      })
      out.push({ id: `e:${id}`, source: e.source, target: id, kind: 'wifi' })
    }
    out.push({ ...e, id: `${e.id}:net`, source: id, source_port: undefined })
  }
  return { nodes: [...nodes, ...networks.values()], edges: out }
}
