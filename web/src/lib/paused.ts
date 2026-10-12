import type { PausedDevice, TopoNode } from './types'

/** The MACs whose internet a router pauses, from every router's list. */
export function pausedMacs(nodes: TopoNode[]): Set<string> {
  const out = new Set<string>()
  for (const r of nodes) for (const p of r.device?.paused ?? []) out.add(p.mac.toLowerCase())
  return out
}

/** How a router pauses this device: Omini's pause, or a rule set by hand. */
export function pauseOf(node: TopoNode, nodes: TopoNode[]): PausedDevice | undefined {
  const mac = node.mac?.toLowerCase()
  if (!mac) return undefined
  for (const r of nodes)
    for (const p of r.device?.paused ?? []) if (p.mac.toLowerCase() === mac) return p
  return undefined
}

/** Whether a router pauses this device's internet. */
export function pausedOn(node: TopoNode, nodes: TopoNode[]): boolean {
  return !!node.mac && pausedMacs(nodes).has(node.mac.toLowerCase())
}
