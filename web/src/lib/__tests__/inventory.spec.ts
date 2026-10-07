import { describe, expect, it } from 'vitest'

import { displayName, filterInventory, isRandomMAC } from '../inventory'
import type { InventoryEntry } from '../types'

const entry = (id: string, extra: Partial<InventoryEntry>): InventoryEntry => ({
  id,
  kind: 'client',
  label: id,
  pinned: false,
  hidden: false,
  first_seen: '2026-10-01T00:00:00Z',
  last_seen: '2026-10-06T00:00:00Z',
  online: true,
  ...extra,
})

const entries = [
  entry('nas', { ip: '192.168.1.10', mac: '00:11:32:aa:00:01', vendor: 'Synology' }),
  entry('iphone', { ip: '192.168.1.101', mac: 'da:a1:19:00:00:01', online: false }),
  entry('tv', { alias: 'Living room TV', ip: '192.168.1.30', mac: '04:5d:4b:50:00:01' }),
]

const ids = (list: InventoryEntry[]) => list.map((e) => e.id)

describe('inventory', () => {
  it('searches by name, alias, IP, MAC and vendor', () => {
    const f = { status: 'all' as const, randomOnly: false }
    expect(ids(filterInventory(entries, { ...f, query: 'living' }))).toEqual(['tv'])
    expect(ids(filterInventory(entries, { ...f, query: '1.101' }))).toEqual(['iphone'])
    expect(ids(filterInventory(entries, { ...f, query: 'SYNOLOGY' }))).toEqual(['nas'])
    expect(ids(filterInventory(entries, { ...f, query: '' }))).toHaveLength(3)
  })

  it('filters by status and randomized MACs', () => {
    expect(
      ids(filterInventory(entries, { query: '', status: 'offline', randomOnly: false })),
    ).toEqual(['iphone'])
    expect(
      ids(filterInventory(entries, { query: '', status: 'online', randomOnly: false })),
    ).toEqual(['nas', 'tv'])
    expect(ids(filterInventory(entries, { query: '', status: 'all', randomOnly: true }))).toEqual([
      'iphone',
    ])
  })

  it('prefers the alias as display name', () => {
    expect(displayName(entries[2]!)).toBe('Living room TV')
    expect(isRandomMAC('da:a1:19:00:00:01')).toBe(true)
    expect(isRandomMAC('00:11:32:aa:00:01')).toBe(false)
  })
})
