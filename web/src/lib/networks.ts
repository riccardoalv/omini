import type { MapArea, TopoEdge, TopoNode } from './types'
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
  /** The VLAN, for a VLAN's group. */
  vlan?: number
  /** Its address range, when known ("192.168.20.0/24"). */
  cidr?: string
  /** What the router calls it ("IOT"), or the VLAN's name. */
  label?: string
}

/** What a network is, besides its members. */
type NetworkInfo = Omit<NetworkGroup, 'members'>

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
 * left out) and routers in their networks (VLAN and gateway), with the VLAN
 * each one is on.
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
    for (const nw of d.networks ?? []) {
      const s = nw.gateway ? subnetOf(nw.gateway) : undefined
      if (!s) continue
      const cur = out.get(s.cidr)
      out.set(s.cidr, {
        ...s,
        vlan: cur?.vlan ?? nw.vlan,
        label: cur?.label ?? (nw.name || nw.interface),
      })
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
 * The VLAN of each Wi-Fi client: its SSID's, as the access point it hangs
 * from (or the controller of its site) reports it; an SSID without a VLAN is
 * on the untagged LAN (VLAN 1).
 */
function ssidVlans(nodes: TopoNode[], edges: TopoEdge[]): Map<string, number> {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const parentOf = new Map<string, string>()
  for (const e of edges)
    if (e.kind !== 'vpn' && !parentOf.has(e.target)) parentOf.set(e.target, e.source)
  const vlanOn = (n: TopoNode | undefined, ssid: string) =>
    n?.device?.wifi_networks?.find((w) => w.ssid === ssid)
  const anywhere = new Map<string, number>()
  for (const n of nodes)
    for (const w of n.device?.wifi_networks ?? [])
      if (!anywhere.has(w.ssid) || w.vlan !== undefined) anywhere.set(w.ssid, w.vlan ?? 1)
  const out = new Map<string, number>()
  for (const n of nodes) {
    if (!n.ssid || n.device) continue
    let found: { vlan?: number } | undefined
    // Up the tree (an SSID node may sit between the client and its access point).
    for (let id = parentOf.get(n.id) ?? n.parent_id, i = 0; id && !found && i < 3; i++) {
      found = vlanOn(byId.get(id), n.ssid)
      id = parentOf.get(id)
    }
    const vlan = found ? (found.vlan ?? 1) : anywhere.get(n.ssid)
    if (vlan !== undefined) out.set(n.id, vlan)
  }
  return out
}

/**
 * The automatic areas of a network: one per VLAN and per subnet (a subnet on
 * a VLAN is that VLAN's area), each with the nodes that are only in it.
 * Nodes in `exclude` (already in an area the user drew) are left out. None
 * when the network has a single VLAN or subnet: there is nothing to separate.
 */
function networkKeys(nodes: TopoNode[], edges: TopoEdge[], exclude: Set<string>) {
  const vlans = networkVlans(nodes)
  const listed = new Set(vlans.map((v) => v.id))
  const subnets = networkSubnets(nodes)
  const info = new Map<string, NetworkInfo>()
  const name = (key: string, text: string, rest: Omit<NetworkInfo, 'key' | 'name'>) => {
    if (!info.has(key)) info.set(key, { key, name: text.slice(0, 60), ...rest })
  }
  for (const v of vlans) {
    const s = subnets.find((x) => x.vlan === v.id)
    const label = v.name || s?.label
    name(`vlan:${v.id}`, `VLAN ${v.id}` + (label ? ` · ${label}` : ''), {
      vlan: v.id,
      cidr: s?.cidr ?? (v.subnet ? subnetOf(v.subnet)?.cidr : undefined),
      label,
    })
  }
  const subnetKey = (s: Subnet) =>
    s.vlan !== undefined && listed.has(s.vlan) ? `vlan:${s.vlan}` : `subnet:${s.cidr}`
  for (const s of subnets) {
    const key = subnetKey(s)
    if (key.startsWith('subnet:'))
      name(key, s.label ? `${s.label} · ${s.cidr}` : s.cidr, { cidr: s.cidr, label: s.label })
  }

  const keys = new Map<string, Set<string>>()
  /** The networks of a node's own addresses. */
  const byAddress = new Map<string, Set<string>>()
  const add = (to: Map<string, Set<string>>, id: string, key: string) => {
    if (exclude.has(id)) return
    to.set(id, (to.get(id) ?? new Set()).add(key))
  }
  // VLAN 1 is the default: unnamed, it has no area, but a device in it and in
  // another VLAN (a trunk) is still in two.
  for (const id of [...listed, ...(listed.has(1) ? [] : [1])])
    for (const n of vlansOf(id, nodes, edges)) add(keys, n, `vlan:${id}`)
  for (const [n, vlan] of ssidVlans(nodes, edges))
    if (listed.has(vlan) || vlan === 1) add(keys, n, `vlan:${vlan}`)
  for (const n of nodes)
    for (const ip of addressesOf(n))
      for (const s of subnets)
        if ((ip & maskOf(s.bits)) >>> 0 === s.net) {
          add(keys, n.id, subnetKey(s))
          add(byAddress, n.id, subnetKey(s))
        }
  for (const ks of keys.values())
    // The default VLAN with a subnet is that subnet (the plain LAN).
    if (!listed.has(1) && ks.has('vlan:1') && [...ks].some((k) => k.startsWith('subnet:')))
      ks.delete('vlan:1')
  return { info, keys, byAddress }
}

/**
 * Nodes with no address or VLAN of their own (a scanned network, an unmanaged
 * switch, a Wi-Fi network) join the network all their children are in.
 */
function joinByChildren(
  nodes: TopoNode[],
  edges: TopoEdge[],
  keys: Map<string, Set<string>>,
  groupOf: Map<string, string>,
  exclude: Set<string>,
) {
  const children = new Map<string, string[]>()
  for (const e of edges) children.set(e.source, [...(children.get(e.source) ?? []), e.target])
  for (let changed = true; changed;) {
    changed = false
    for (const n of nodes) {
      if (keys.get(n.id)?.size || groupOf.has(n.id) || exclude.has(n.id)) continue
      const below = new Set((children.get(n.id) ?? []).map((c) => groupOf.get(c) ?? ''))
      const [key] = below
      if (below.size !== 1 || !key) continue
      groupOf.set(n.id, key)
      changed = true
    }
  }
}

export function groupNetworks(
  nodes: TopoNode[],
  edges: TopoEdge[],
  exclude: Set<string> = new Set(),
  /** inclusive: a node is in every VLAN or subnet it is in (for the filter); exclusive: only in its one (for areas). */
  mode: 'exclusive' | 'inclusive' = 'exclusive',
): NetworkGroup[] {
  const { info, keys, byAddress } = networkKeys(nodes, edges, exclude)
  const members = new Map<string, Set<string>>([...info.keys()].map((k) => [k, new Set()]))
  if (mode === 'exclusive')
    // A client's own address says which network it is in, whatever the link
    // above it carries: a client under a trunk is not in every VLAN of it.
    for (const n of nodes) {
      const mine = byAddress.get(n.id)
      if (!n.device && mine?.size) keys.set(n.id, new Set(mine))
    }
  // Only the networks that are listed (an unnamed default VLAN is not one).
  const known = new Map(
    [...keys].map(([id, ks]) => [id, new Set([...ks].filter((k) => info.has(k)))]),
  )
  for (const [id, ks] of known) {
    if (mode === 'inclusive') for (const k of ks) members.get(k)!.add(id)
    else if (ks.size === 1) members.get([...ks][0]!)!.add(id)
  }
  const groupOf = new Map<string, string>()
  for (const [key, set] of members) for (const id of set) groupOf.set(id, key)
  const before = new Set(groupOf.keys())
  joinByChildren(nodes, edges, known, groupOf, exclude)
  for (const [id, key] of groupOf) if (!before.has(id)) members.get(key)!.add(id)
  const out = [...info.values()]
    .map((g) => ({ ...g, members: members.get(g.key)! }))
    .filter((g) => g.members.size > 0)
  return out.length >= 2 ? out : []
}

/** A stable negative id for a network's area (the areas the user draws have positive ones). */
export function networkAreaId(key: string): number {
  let h = 5381
  for (let i = 0; i < key.length; i++) h = ((h * 33) ^ key.charCodeAt(i)) >>> 0
  return -(h % 2_000_000_000) - 1
}

/**
 * Every VLAN and subnet as areas on the map, strict like the areas the user
 * draws (an area holds its devices and what hangs below them, nothing else):
 *
 * - a device in several networks (the router, a trunk switch) stays outside
 *   them all, and so does one with something of another network below it;
 * - a network gets one area per branch it is in (VLAN 30 under two switches
 *   is two areas with the same name and color), never one box across the map;
 * - devices in an area the user drew are left to it, and `drop` (clients
 *   folded into a bubble with clients of other networks) to none.
 */
export function networkAreas(
  nodes: TopoNode[],
  edges: TopoEdge[],
  userAreas: MapArea[],
  direction: MapArea['direction'],
  vlanColors: Map<number, string> = new Map(),
  drop: ReadonlySet<string> = new Set(),
): MapArea[] {
  const taken = new Set(userAreas.flatMap((a) => a.members))
  const all = groupNetworks(nodes, edges, new Set(), 'inclusive')
  const groups = groupNetworks(nodes, edges, taken)
  const keyOf = new Map<string, string>()
  for (const g of groups) for (const m of g.members) if (!drop.has(m)) keyOf.set(m, g.key)

  // The tree the map is laid out on: each node under its first parent (VPN tunnels aside).
  const parentOf = new Map<string, string>()
  const children = new Map<string, string[]>()
  for (const e of edges) {
    if (e.kind === 'vpn' || e.source === e.target || parentOf.has(e.target)) continue
    parentOf.set(e.target, e.source)
    children.set(e.source, [...(children.get(e.source) ?? []), e.target])
  }
  // The networks of what hangs below a node.
  const below = new Map<string, Set<string>>()
  const keysBelow = (id: string, seen = new Set<string>()): Set<string> => {
    const known = below.get(id)
    if (known) return known
    const out = new Set<string>()
    seen.add(id)
    for (const c of children.get(id) ?? []) {
      if (seen.has(c)) continue
      const k = keyOf.get(c)
      if (k) out.add(k)
      for (const x of keysBelow(c, seen)) out.add(x)
    }
    below.set(id, out)
    return out
  }
  const kept = new Map<string, string>()
  for (const [id, key] of keyOf) if ([...keysBelow(id)].every((k) => k === key)) kept.set(id, key)

  // One area per branch: members grouped by where they enter the network.
  const areas = new Map<string, { key: string; members: string[] }>()
  for (const [id, key] of kept) {
    let top = id
    const seen = new Set([id])
    for (
      let p = parentOf.get(top);
      p && kept.get(p) === key && !seen.has(p);
      p = parentOf.get(top)
    ) {
      seen.add(p)
      top = p
    }
    const branch = `${key}@${parentOf.get(top) ?? top}`
    if (!areas.has(branch)) areas.set(branch, { key, members: [] })
    areas.get(branch)!.members.push(id)
  }
  const info = new Map(groups.map((g) => [g.key, g]))
  return [...areas]
    .map(([branch, { key, members }]) => {
      const g = info.get(key)!
      return {
        id: networkAreaId(branch),
        name: g.name,
        color: networkColor(
          g,
          all.findIndex((x) => x.key === key),
          vlanColors,
        ),
        direction,
        x: 0,
        y: 0,
        width: 0,
        height: 0,
        members,
        auto: true,
      }
    })
    .sort((x, y) => x.name.localeCompare(y.name) || x.id - y.id)
}

/** Colors for networks with none of their own (a VLAN's color comes from the catalog). */
export const NETWORK_PALETTE = [
  '#3b82f6',
  '#22c55e',
  '#f59e0b',
  '#ef4444',
  '#a855f7',
  '#06b6d4',
  '#ec4899',
  '#84cc16',
  '#f97316',
  '#6366f1',
  '#14b8a6',
  '#eab308',
] as const

/** A network's color: its VLAN's color in the catalog, else one from the palette by its place. */
export function networkColor(
  g: Pick<NetworkGroup, 'vlan'>,
  index: number,
  vlanColors: Map<number, string> = new Map(),
): string {
  const own = g.vlan !== undefined ? vlanColors.get(g.vlan) : undefined
  return own ?? NETWORK_PALETTE[index % NETWORK_PALETTE.length]!
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

/** A suggested gateway for a new VLAN: 192.168.<id>.1/24 (10.x for ids over 254), else the next free /24. */
export function suggestGateway(id: number, used: string[]): string {
  const taken = new Set(used.map((c) => subnetOf(c.includes('/') ? c : `${c}/24`)?.cidr))
  const candidates = [
    id >= 1 && id <= 254 ? `192.168.${id}.1/24` : '',
    `10.${Math.floor(id / 256)}.${id % 256}.1/24`,
    ...Array.from({ length: 254 }, (_, i) => `10.${id % 256}.${i + 1}.1/24`),
  ].filter(Boolean)
  return candidates.find((g) => !taken.has(subnetOf(g)?.cidr)) ?? candidates[0]!
}

/** The DHCP range suggested for a gateway: .100 to .250 of a /24, the upper half of anything else. */
export function suggestRange(gateway: string): { from: string; to: string } | undefined {
  const s = subnetOf(gateway)
  if (!s) return undefined
  const size = 2 ** (32 - s.bits)
  const from = size >= 256 ? 100 : Math.floor(size / 2)
  const to = size >= 256 ? 250 : size - 2
  return { from: ntoa(s.net + from), to: ntoa(s.net + to) }
}

/** Whether an address is inside a gateway's network (and not its first or last address). */
export function inNetwork(ip: string, gateway: string): boolean {
  const s = subnetOf(gateway)
  const v = parseIPv4(ip)
  if (!s || v === undefined) return false
  const size = 2 ** (32 - s.bits)
  return v > s.net && v < s.net + size - 1
}
