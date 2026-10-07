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

/** Filters inventory entries by text (name, IP, MAC, hostname, vendor), status and MAC type. */
export function filterInventory(entries: InventoryEntry[], f: InventoryFilter): InventoryEntry[] {
  const q = f.query.trim().toLowerCase()
  return entries.filter((e) => {
    if (f.status === 'online' && !e.online) return false
    if (f.status === 'offline' && e.online) return false
    if (f.randomOnly && !isRandomMAC(e.mac)) return false
    if (!q) return true
    return [displayName(e), e.ip, e.mac, e.hostname, e.vendor]
      .filter(Boolean)
      .some((v) => v!.toLowerCase().includes(q))
  })
}
