import { compactIPv6 } from './ipv6'
import type { InventoryEntry } from './types'

export type StatusFilter = 'all' | 'online' | 'offline'

export interface InventoryFilter {
  query: string
  status: StatusFilter
  randomOnly: boolean
}

export const displayName = (e: InventoryEntry) =>
  e.alias || e.label || e.hostname || e.ip || e.mac || e.id

/** Whether a MAC has the locally administered bit set (phones' private addresses). */
export function isRandomMAC(mac?: string): boolean {
  if (!mac || mac.length < 2) return false
  return ['2', '6', 'a', 'e'].includes(mac[1]!.toLowerCase())
}

/** Filters inventory entries by text (name, IPv4 and IPv6 address, MAC, hostname, vendor, note), status and MAC type. */
export function filterInventory(entries: InventoryEntry[], f: InventoryFilter): InventoryEntry[] {
  const q = f.query.trim().toLowerCase()
  const q6 = compactIPv6(q) // an IPv6 address typed in full finds its compact form
  return entries.filter((e) => {
    if (f.status === 'online' && !e.online) return false
    if (f.status === 'offline' && e.online) return false
    if (f.randomOnly && !isRandomMAC(e.mac)) return false
    if (!q) return true
    return [displayName(e), e.ip, ...(e.ips6 ?? []), e.mac, e.hostname, e.vendor, e.note]
      .filter(Boolean)
      .some((v) => v!.toLowerCase().includes(q) || v!.toLowerCase().includes(q6))
  })
}
