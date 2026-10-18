import { areaHex } from './areas'
import type { Interface, TopoNode, Vlan } from './types'

/** Whether a port carries a VLAN: it is that VLAN's interface, or a member (untagged or tagged). */
export function portCarries(iface: Interface | undefined, vlan: number): boolean {
  if (!iface) return false
  return (
    iface.vlan === vlan ||
    iface.vlans?.untagged === vlan ||
    (iface.vlans?.tagged ?? []).includes(vlan)
  )
}

/**
 * The VLANs a WAN rides on (an operator's PPPoE VLAN): a VLAN interface that
 * is a WAN or that a WAN sits on. They are not the network's.
 */
export function wanVlans(nodes: TopoNode[]): Set<number> {
  const out = new Set<number>()
  for (const n of nodes) {
    const ifaces = n.device?.interfaces ?? []
    const wanOn = new Set<string>()
    for (const i of ifaces)
      if (i.wan) {
        wanOn.add(i.name)
        if (i.parent) wanOn.add(i.parent)
      }
    for (const i of ifaces) if (i.vlan && wanOn.has(i.name)) out.add(i.vlan)
    for (const v of n.device?.vlans ?? []) if (v.interface && wanOn.has(v.interface)) out.add(v.id)
  }
  return out
}

/** Every VLAN of the network: defined on a device, on a port or seen in a MAC table (not the WAN's). */
export function networkVlans(nodes: TopoNode[]): Vlan[] {
  const wan = wanVlans(nodes)
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
  return [...out.values()]
    .filter((v) => (v.id !== 1 || v.name) && !wan.has(v.id))
    .sort((a, b) => a.id - b.id)
}

/** The colors a VLAN can have (the areas' presets). */
export const VLAN_COLORS = ['blue', 'green', 'yellow', 'red', 'purple', 'gray'] as const

/** A VLAN's color as "#rrggbb" (gray when it has none). */
export const vlanHex = (color?: string) => areaHex(color || 'gray')

/** "VLAN 20", "native 1, tagged 10, 20" or "tagged 10, 20". */
export function describeVlans(
  v: { untagged?: number; tagged?: number[] },
  t: (key: string, params: Record<string, unknown>) => string,
): string {
  if (!v.tagged?.length) return t('portVlans.access', { vlan: v.untagged })
  const tags = v.tagged.join(', ')
  return v.untagged
    ? t('portVlans.trunk', { native: v.untagged, tagged: tags })
    : t('portVlans.trunkNoNative', { tagged: tags })
}
