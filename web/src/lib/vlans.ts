import type { Interface, TopoEdge, TopoNode, Vlan } from './types'

/** Whether a port carries a VLAN: it is that VLAN's interface, or a member (untagged or tagged). */
export function portCarries(iface: Interface | undefined, vlan: number): boolean {
  if (!iface) return false
  return (
    iface.vlan === vlan ||
    iface.vlans?.untagged === vlan ||
    (iface.vlans?.tagged ?? []).includes(vlan)
  )
}

/** Every VLAN of the network: defined on a device, on a port or seen in a MAC table. */
export function networkVlans(nodes: TopoNode[]): Vlan[] {
  const out = new Map<number, Vlan>()
  const add = (v: Vlan) => {
    const cur = out.get(v.id)
    out.set(v.id, { ...v, ...cur, name: cur?.name ?? v.name, subnet: cur?.subnet ?? v.subnet })
  }
  for (const n of nodes) {
    const d = n.device
    if (!d) continue
    for (const v of d.vlans ?? []) add(v)
    for (const i of d.interfaces ?? []) {
      if (i.vlan) add({ id: i.vlan, interface: i.name })
      if (i.vlans?.untagged) add({ id: i.vlans.untagged })
      for (const t of i.vlans?.tagged ?? []) add({ id: t })
    }
    for (const f of d.fdb ?? []) if (f.vlan) add({ id: f.vlan })
  }
  // VLAN 1 is the default everywhere: listed only when named.
  return [...out.values()].filter((v) => v.id !== 1 || v.name).sort((a, b) => a.id - b.id)
}

/**
 * The nodes and links in a VLAN: devices with an interface in it, clients
 * reached through a port that carries it (or seen in it in a MAC table), and
 * links whose upstream port carries it.
 */
export function vlanMembers(
  vlan: number,
  nodes: TopoNode[],
  edges: TopoEdge[],
): { nodes: Set<string>; edges: Set<string> } {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const inNodes = new Set<string>()
  const inEdges = new Set<string>()
  const ifaceOf = (n: TopoNode | undefined, port?: string) =>
    port ? n?.device?.interfaces?.find((i) => i.name === port) : undefined
  for (const n of nodes) {
    const d = n.device
    if (!d) continue
    if (
      (d.vlans ?? []).some((v) => v.id === vlan) ||
      (d.interfaces ?? []).some((i) => portCarries(i, vlan))
    )
      inNodes.add(n.id)
  }
  for (const e of edges) {
    const source = byId.get(e.source)
    const target = byId.get(e.target)
    const port = ifaceOf(source, e.source_port)
    const seen =
      !!target?.mac &&
      (source?.device?.fdb ?? []).some(
        (f) => f.vlan === vlan && f.mac.toLowerCase() === target.mac!.toLowerCase(),
      )
    if (portCarries(port, vlan) || seen || portCarries(ifaceOf(target, e.target_port), vlan)) {
      inEdges.add(e.id)
      inNodes.add(e.source)
      inNodes.add(e.target)
    }
  }
  // Apps run on their device: they follow it.
  for (const e of edges)
    if (inNodes.has(e.source) && byId.get(e.target)?.kind === 'app') {
      inEdges.add(e.id)
      inNodes.add(e.target)
    }
  return { nodes: inNodes, edges: inEdges }
}
