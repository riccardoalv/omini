import type { TopoEdge, TopoNode } from './types'
import { networkVlans, portCarries } from './vlans'

/**
 * The networks of the map, for the VLAN/subnet filter: each VLAN and subnet
 * ("VLAN 20 · IOT", "LAN · 192.168.1.0/24") with the devices in it. In
 * exclusive mode, devices in several (the firewall, a trunk switch, an access
 * point carrying many VLANs) belong to none; what hangs below a member comes
 * along.
 */
export interface NetworkGroup {
  /** "vlan:20" | "subnet:192.168.20.0/24". */
  key: string
  name: string
  members: Set<string>
}

function parseIPv4(s: string): number | undefined {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(s.trim())
  if (!m) return undefined
  const parts = m.slice(1).map(Number)
  if (parts.some((p) => p > 255)) return undefined
  return ((parts[0]! << 24) | (parts[1]! << 16) | (parts[2]! << 8) | parts[3]!) >>> 0
}

const ntoa = (n: number) => [n >>> 24, (n >>> 16) & 255, (n >>> 8) & 255, n & 255].join('.')
const maskOf = (bits: number) => (bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0)

function isPrivate(ip: number): boolean {
  return (
    ip >>> 24 === 10 || // 10/8
    ip >>> 20 === (172 << 4) + 1 || // 172.16/12
    ip >>> 16 === (192 << 8) + 168 // 192.168/16
  )
}

export interface Subnet {
  /** "192.168.20.0/24" */
  cidr: string
  net: number
  bits: number
  /** VLAN of the interface it is on, when it is a VLAN interface. */
  vlan?: number
  /** The interface's description or name ("LAN", "IOT"). */
  label?: string
}

/** The private IPv4 network of an address with its prefix ("192.168.1.1/24"), /16 to /30. */
export function subnetOf(cidr: string): Omit<Subnet, 'vlan' | 'label'> | undefined {
  const [addr, len] = cidr.split('/')
  const ip = parseIPv4(addr ?? '')
  const bits = Number(len)
  if (ip === undefined || !Number.isInteger(bits) || bits < 16 || bits > 30 || !isPrivate(ip))
    return undefined
  const net = (ip & maskOf(bits)) >>> 0
  return { cidr: `${ntoa(net)}/${bits}`, net, bits }
}

/**
 * The networks routers and servers report on their interfaces (WAN ones
 * left out), with the VLAN each one is on.
 */
export function networkSubnets(nodes: TopoNode[]): Subnet[] {
  const out = new Map<string, Subnet>()
  for (const n of nodes) {
    const d = n.device
    if (!d) continue
    const vlanBySubnet = new Map<string, number>()
    for (const v of d.vlans ?? []) {
      const s = v.subnet ? subnetOf(v.subnet) : undefined
      if (s) vlanBySubnet.set(s.cidr, v.id)
    }
    for (const i of d.interfaces ?? []) {
      if (i.wan) continue
      for (const a of i.ips ?? []) {
        const s = subnetOf(a)
        if (!s) continue
        const cur = out.get(s.cidr)
        out.set(s.cidr, {
          ...s,
          vlan: cur?.vlan ?? i.vlan ?? vlanBySubnet.get(s.cidr),
          label: cur?.label ?? (i.description || i.name),
        })
      }
    }
    for (const [cidr, vlan] of vlanBySubnet) {
      const s = subnetOf(cidr)
      if (s && !out.has(cidr)) out.set(cidr, { ...s, vlan })
    }
  }
  return [...out.values()].sort((a, b) => a.net - b.net || a.bits - b.bits)
}

/** Every IPv4 address of a node: its own, its device's, its interfaces'. */
function addressesOf(n: TopoNode): number[] {
  const out = new Set<number>()
  const add = (s: string | undefined) => {
    const ip = s ? parseIPv4(s.split('/')[0]!) : undefined
    if (ip !== undefined) out.add(ip)
  }
  add(n.ip)
  for (const ip of n.device?.ips ?? []) add(ip)
  for (const i of n.device?.interfaces ?? []) if (!i.wan) for (const ip of i.ips ?? []) add(ip)
  return [...out]
}

/**
 * The VLANs a node is in, by what it reports itself (VLAN interfaces, port
 * membership) and by the link it hangs from (the upstream port carries the
 * VLAN, or the upstream switch saw it in that VLAN). The upstream device is
 * not counted in by its link: only its own ports say where it is.
 */
function vlansOf(vlan: number, nodes: TopoNode[], edges: TopoEdge[]): Set<string> {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const out = new Set<string>()
  for (const n of nodes) {
    const d = n.device
    if (
      d &&
      ((d.vlans ?? []).some((v) => v.id === vlan) ||
        (d.interfaces ?? []).some((i) => portCarries(i, vlan)))
    )
      out.add(n.id)
  }
  for (const e of edges) {
    const source = byId.get(e.source)
    const target = byId.get(e.target)
    if (!target) continue
    const port = e.source_port
      ? source?.device?.interfaces?.find((i) => i.name === e.source_port)
      : undefined
    const seen =
      !!target.mac &&
      (source?.device?.fdb ?? []).some(
        (f) => f.vlan === vlan && f.mac.toLowerCase() === target.mac!.toLowerCase(),
      )
    if (portCarries(port, vlan) || seen) out.add(target.id)
  }
  return out
}

/**
 * The automatic areas of a network: one per VLAN and per subnet (a subnet on
 * a VLAN is that VLAN's area), each with the nodes that are only in it.
 * Nodes in `exclude` (already in an area the user drew) are left out. None
 * when the network has a single VLAN or subnet: there is nothing to separate.
 */
export function groupNetworks(
  nodes: TopoNode[],
  edges: TopoEdge[],
  exclude: Set<string> = new Set(),
  /** inclusive: a node is in every VLAN or subnet it is in (for the filter); exclusive: only in its one (for areas). */
  mode: 'exclusive' | 'inclusive' = 'exclusive',
): NetworkGroup[] {
  const vlans = networkVlans(nodes)
  const listed = new Set(vlans.map((v) => v.id))
  const subnets = networkSubnets(nodes)
  const names = new Map<string, string>()
  const order: string[] = []
  const name = (key: string, text: string) => {
    if (!names.has(key)) {
      names.set(key, text.slice(0, 60))
      order.push(key)
    }
  }
  for (const v of vlans) {
    const s = subnets.find((x) => x.vlan === v.id)
    name(
      `vlan:${v.id}`,
      `VLAN ${v.id}` + (v.name ? ` · ${v.name}` : s?.label ? ` · ${s.label}` : ''),
    )
  }
  const subnetKey = (s: Subnet) =>
    s.vlan !== undefined && listed.has(s.vlan) ? `vlan:${s.vlan}` : `subnet:${s.cidr}`
  for (const s of subnets) {
    const key = subnetKey(s)
    if (key.startsWith('subnet:')) name(key, s.label ? `${s.label} · ${s.cidr}` : s.cidr)
  }

  const keys = new Map<string, Set<string>>()
  const add = (id: string, key: string) => {
    if (exclude.has(id)) return
    keys.set(id, (keys.get(id) ?? new Set()).add(key))
  }
  // VLAN 1 is the default: unnamed, it has no area, but a device in it and in
  // another VLAN (a trunk) is still in two.
  for (const id of [...listed, ...(listed.has(1) ? [] : [1])])
    for (const n of vlansOf(id, nodes, edges)) add(n, `vlan:${id}`)
  for (const n of nodes)
    for (const ip of addressesOf(n))
      for (const s of subnets) if ((ip & maskOf(s.bits)) >>> 0 === s.net) add(n.id, subnetKey(s))

  const members = new Map<string, Set<string>>(order.map((k) => [k, new Set()]))
  for (const [id, ks] of keys) {
    // The default VLAN with a subnet is that subnet (the plain LAN).
    if (!listed.has(1) && ks.has('vlan:1') && [...ks].some((k) => k.startsWith('subnet:')))
      ks.delete('vlan:1')
    if (mode === 'inclusive') for (const k of ks) members.get(k)?.add(id)
    else if (ks.size === 1) members.get([...ks][0]!)?.add(id)
  }
  // A node with no address or VLAN of its own (a scanned network, an
  // unmanaged switch, a Wi-Fi network) joins the area all its children are in.
  const groupOf = new Map<string, string>()
  for (const [key, set] of members) for (const id of set) groupOf.set(id, key)
  const children = new Map<string, string[]>()
  for (const e of edges) children.set(e.source, [...(children.get(e.source) ?? []), e.target])
  for (let changed = true; changed;) {
    changed = false
    for (const n of nodes) {
      if (keys.has(n.id) || groupOf.has(n.id) || exclude.has(n.id)) continue
      const below = new Set((children.get(n.id) ?? []).map((c) => groupOf.get(c) ?? ''))
      const [key] = below
      if (below.size !== 1 || !key) continue
      groupOf.set(n.id, key)
      members.get(key)!.add(n.id)
      changed = true
    }
  }
  const out = order
    .map((key) => ({ key, name: names.get(key)!, members: members.get(key)! }))
    .filter((g) => g.members.size > 0)
  return out.length >= 2 ? out : []
}

/**
 * What the VLAN/subnet filter highlights: every device in the network
 * (inclusive), and below the devices only in it (exclusive) what hangs from
 * them. A router also in other networks lights up alone: else picking the
 * modem's network lit the whole LAN under the firewall.
 */
export function networkHighlight(
  key: string | undefined,
  inclusive: NetworkGroup[],
  exclusive: NetworkGroup[],
  edges: { source: string; target: string }[],
): Set<string> | undefined {
  const g = inclusive.find((x) => x.key === key)
  if (!g) return undefined
  const own = [...(exclusive.find((x) => x.key === key)?.members ?? [])]
  const below = new Map<string, string[]>()
  for (const e of edges) below.set(e.source, [...(below.get(e.source) ?? []), e.target])
  const out = new Set(g.members)
  const stack = [...own]
  while (stack.length) {
    const id = stack.pop()!
    out.add(id)
    for (const c of below.get(id) ?? []) if (!out.has(c)) stack.push(c)
  }
  return out
}
