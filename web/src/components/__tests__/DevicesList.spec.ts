import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '@/lib/api'
import type { InventoryEntry } from '@/lib/types'

import DevicesList from '../DevicesList.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      inventory: vi.fn<typeof mod.api.inventory>(),
      wake: vi.fn<typeof mod.api.wake>(),
    },
  }
})

const entry = (id: string, online: boolean, mac?: string): InventoryEntry => ({
  id,
  kind: 'client',
  label: id,
  mac,
  hidden: false,
  first_seen: '2026-10-01T00:00:00Z',
  last_seen: '2026-10-17T22:00:00Z',
  online,
})

describe('DevicesList', () => {
  it('wakes an offline device, even one that left the map', async () => {
    vi.mocked(api.inventory).mockResolvedValue([
      entry('mac:pc', false, '00:11:22:00:00:09'),
      entry('mac:tv', true, '00:11:22:00:00:0a'),
      entry('ip:192.168.1.9', false),
    ])
    vi.mocked(api.wake)
      .mockResolvedValueOnce({} as never)
      .mockRejectedValueOnce(new ApiError(404, 'no way to wake it'))
    const w = mount(DevicesList, { global: { plugins: plugins() } })
    await flushPromises()
    const buttons = w.findAll('[data-test=wake]')
    expect(buttons).toHaveLength(1) // offline, with a MAC
    await buttons[0]!.trigger('click')
    await flushPromises()
    expect(api.wake).toHaveBeenCalledWith('mac:pc')
    expect(w.get('[data-test=wake-result]').text()).toContain('Wake-on-LAN packet sent')
    await buttons[0]!.trigger('click')
    await flushPromises()
    expect(w.get('[data-test=wake-result]').text()).toContain('no way to wake it')
  })
})
