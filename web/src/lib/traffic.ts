import type { Interface, TopoEdge, TopoNode } from './types'

/** Traffic seen from the device a link reaches: down = towards it. */
export interface Flow {
  down: number
  up: number
}

export interface LinkLabel {
  speed?: number
  /** Name of the port the link leaves from (or reaches): "LAN", "Uplink to the rack". */
  name?: string
  flow?: Flow
  /** Where the pill goes: the middle of the link, or of the part a shared port's links have in common. */
  at: 'middle' | 'shared'
  /** Links sharing a port show one pill for all (on the first of them). */
  hidden?: boolean
}

/** "45.2 Mbps", "1.2 Gbps", "800 kbps" */
export function formatRate(bps?: number): string {
  if (bps === undefined) return ''
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(bps >= 1e8 ? 0 : 1)} Mbps`
  if (bps >= 1e3) return `${Math.round(bps / 1e3)} kbps`
  return `${Math.round(bps)} bps`
}

const rate = (n: TopoNode | undefined, port?: string) => (port ? n?.traffic?.[port] : undefined)

/**
 * The traffic of a node: a router's or firewall's internet traffic (its WAN
 * interfaces), a WAN node's own uplink, or a Wi-Fi client's as its access
 * point measures it. Undefined when unknown.
 */
export function nodeFlow(n: TopoNode, byId: Map<string, TopoNode>): Flow | undefined {
  // A Wi-Fi client: what its access point measures (nothing while idle).
  if (n.flow)
    return n.flow.rx_bps || n.flow.tx_bps ? { down: n.flow.rx_bps, up: n.flow.tx_bps } : undefined
  if (n.kind === 'wan' && n.wan) {
    const owner = byId.get(n.id.slice(4, n.id.length - n.wan.interface.length - 1))
    const r = rate(owner, n.wan.interface)
    return r && { down: r.rx_bps, up: r.tx_bps }
  }
  const wans = (n.device?.interfaces ?? []).filter((i) => i.wan && n.traffic?.[i.name])
  if (!wans.length) return undefined
  return wans.reduce(
    (sum, i) => ({
      down: sum.down + n.traffic![i.name]!.rx_bps,
      up: sum.up + n.traffic![i.name]!.tx_bps,
    }),
    { down: 0, up: 0 },
  )
}

const portSpeed = (n: TopoNode | undefined, port?: string) =>
  port ? n?.device?.interfaces?.find((i) => i.name === port)?.speed_mbps : undefined

/**
 * The physical port behind a bridge or LAG: its fastest member that is up
 * (the link runs at its speed). Without a member list (older integrations),
 * the only physical port up at the bridge's speed, if there is exactly one.
 */
export function physicalPort(n: TopoNode, port: string): string | undefined {
  const ifaces = n.device?.interfaces ?? []
  const iface = ifaces.find((i) => i.name === port)
  if (!iface || (iface.type !== 'bridge' && iface.type !== 'lag')) return undefined
  const physical = (i: Interface) => (!i.type || i.type === 'ethernet') && i.up
  if (iface.members?.length) {
    const members = ifaces.filter((i) => iface.members!.includes(i.name) && physical(i))
    members.sort((x, y) => (y.speed_mbps ?? 0) - (x.speed_mbps ?? 0))
    return members[0]?.name
  }
  const same = ifaces.filter(
    (i) => physical(i) && !i.wan && !i.parent && i.speed_mbps === iface.speed_mbps,
  )
  return same.length === 1 ? same[0]!.name : undefined
}

/**
 * The name shown next to a link's speed: the name the user gave the port,
 * else the interface's own ("mlxen0", "Port 1"). A bridge shows the physical
 * port behind it.
 */
export function portName(n: TopoNode | undefined, port?: string): string | undefined {
  if (!n || !port) return undefined
  const label = (p: string) => n.port_labels?.[p]?.trim() || undefined
  const own = label(port)
  if (own) return own
  const phys = physicalPort(n, port)
  return phys ? (label(phys) ?? phys) : port
}

/**
 * Speed and traffic shown on each link. A port shared by several links (a
 * switch behind a firewall port) gets a single pill at the port with the
 * port's speed and traffic; a direct link gets its own pill.
 */
export function linkLabels(edges: TopoEdge[], nodes: TopoNode[]): Map<string, LinkLabel> {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const shared = new Map<string, TopoEdge[]>()
  for (const e of edges) {
    if (e.source_port) {
      const key = `${e.source}|${e.source_port}`
      shared.set(key, [...(shared.get(key) ?? []), e])
    }
  }
  const out = new Map<string, LinkLabel>()
  for (const e of edges) {
    const source = byId.get(e.source)
    const target = byId.get(e.target)
    // No pill on a Wi-Fi link (its network is a node of its own) nor on an
    // app's link (the app shows its port itself).
    if (e.kind === 'wifi' || target?.kind === 'app') continue
    const group = e.source_port ? shared.get(`${e.source}|${e.source_port}`)! : [e]
    let flow: Flow | undefined
    const sr = rate(source, e.source_port)
    const tr = rate(target, e.target_port)
    if (sr) flow = { down: sr.tx_bps, up: sr.rx_bps }
    else if (tr) flow = { down: tr.rx_bps, up: tr.tx_bps }
    if (group.length > 1) {
      out.set(e.id, {
        speed: portSpeed(source, e.source_port),
        name: portName(source, e.source_port),
        flow,
        at: 'shared',
        hidden: group[0]!.id !== e.id,
      })
      continue
    }
    out.set(e.id, {
      speed: e.speed_mbps,
      name: portName(source, e.source_port) ?? portName(target, e.target_port),
      flow,
      at: 'middle',
    })
  }
  return out
}

/**
 * Traffic of each device reached by a link of its own (not a port shared by
 * several devices): shown on the device, since the link only shows its speed.
 */
export function deviceFlows(labels: Map<string, LinkLabel>, edges: TopoEdge[]): Map<string, Flow> {
  const out = new Map<string, Flow>()
  for (const e of edges) {
    const l = labels.get(e.id)
    if (l?.flow && l.hidden === undefined) out.set(e.target, l.flow)
  }
  return out
}

/** Where a link's traffic history is: an interface of one of its ends. */
export interface LinkSeries {
  node: string
  iface: string
  /** The interface is the upstream port: what it sends is the download. */
  swap: boolean
  name: string
}

/**
 * The history that describes a link: the upstream port when it measures its
 * traffic, else the downstream device's port, else a Wi-Fi client's own.
 */
export function linkSeries(e: TopoEdge, byId: Map<string, TopoNode>): LinkSeries | undefined {
  const source = byId.get(e.source)
  const target = byId.get(e.target)
  const name = [source?.label, target?.label].filter(Boolean).join(' → ')
  if (source && e.source_port && rate(source, e.source_port))
    return { node: source.id, iface: e.source_port, swap: true, name }
  if (target && e.target_port && rate(target, e.target_port))
    return { node: target.id, iface: e.target_port, swap: false, name }
  if (target?.flow) return { node: target.id, iface: '', swap: false, name }
  if (source && e.source_port) return { node: source.id, iface: e.source_port, swap: true, name }
  return undefined
}
