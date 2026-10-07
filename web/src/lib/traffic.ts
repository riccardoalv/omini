import type { TopoEdge, TopoNode } from './types'

/** Traffic seen from the device a link reaches: down = towards it. */
export interface Flow {
  down: number
  up: number
}

export interface LinkLabel {
  speed?: number
  flow?: Flow
  /** Where the pill goes: next to the device reached, or at the source. */
  at: 'target' | 'source'
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
 * interfaces), or a WAN node's own uplink. Undefined when unknown.
 */
export function nodeFlow(n: TopoNode, byId: Map<string, TopoNode>): Flow | undefined {
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
 * Speed and traffic shown on each link. A port shared by several links (a
 * switch behind a firewall port) gets a single pill at the port with the
 * port's speed and traffic; a direct link gets its own pill.
 */
export function linkLabels(
  edges: TopoEdge[],
  nodes: TopoNode[],
  /** Nodes with a traffic badge above them: pills go to the other end. */
  badged: ReadonlySet<string> = new Set(),
): Map<string, LinkLabel> {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const shared = new Map<string, TopoEdge[]>()
  const incoming = new Map<string, number>()
  for (const e of edges) {
    incoming.set(e.target, (incoming.get(e.target) ?? 0) + 1)
    if (e.source_port) {
      const key = `${e.source}|${e.source_port}`
      shared.set(key, [...(shared.get(key) ?? []), e])
    }
  }
  const out = new Map<string, LinkLabel>()
  for (const e of edges) {
    const source = byId.get(e.source)
    const target = byId.get(e.target)
    const group = e.source_port ? shared.get(`${e.source}|${e.source_port}`)! : [e]
    let flow: Flow | undefined
    const sr = rate(source, e.source_port)
    const tr = rate(target, e.target_port)
    if (sr) flow = { down: sr.tx_bps, up: sr.rx_bps }
    else if (tr) flow = { down: tr.rx_bps, up: tr.tx_bps }
    if (group.length > 1) {
      out.set(e.id, {
        speed: portSpeed(source, e.source_port),
        flow,
        at: 'source',
        hidden: group[0]!.id !== e.id,
      })
      continue
    }
    out.set(e.id, {
      speed: e.speed_mbps,
      flow,
      at: (incoming.get(e.target) ?? 0) > 1 || badged.has(e.target) ? 'source' : 'target',
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
