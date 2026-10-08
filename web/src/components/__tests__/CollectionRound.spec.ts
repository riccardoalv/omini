import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { Integration } from '@/lib/types'

import CollectionRound from '../CollectionRound.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      collection: vi.fn<typeof mod.api.collection>(),
      setCollection: vi.fn<typeof mod.api.setCollection>(),
      runRound: vi.fn<typeof mod.api.runRound>(),
    },
  }
})

const integration = (id: number, name: string): Integration => ({
  id,
  name,
  type: name.toLowerCase(),
  config: {},
  enabled: true,
  created_at: '',
  updated_at: '',
})

describe('CollectionRound', () => {
  it('shows the order of the last round and changes the interval', async () => {
    vi.mocked(api.collection).mockResolvedValue({
      interval_s: 60,
      default_s: 60,
      round: {
        started_at: new Date().toISOString(),
        duration_ms: 41_200,
        order: [6, 7, 5, 3, 2],
        interval_s: 60,
      },
    })
    vi.mocked(api.setCollection).mockResolvedValue({ interval_s: 300, default_s: 60 })
    const list = [
      integration(2, 'Network scan'),
      integration(3, 'OPNsense'),
      integration(5, 'Horaco'),
      integration(6, 'Mercusys'),
      integration(7, 'Proxmox'),
    ]
    const w = mount(CollectionRound, {
      props: { integrations: list },
      global: { plugins: plugins() },
    })
    await flushPromises()
    expect(w.findAll('.step').map((s) => s.text())).toEqual([
      '1 Mercusys',
      '2 Proxmox',
      '3 Horaco',
      '4 OPNsense',
      '5 Network scan',
    ])
    expect(w.text()).toContain('took 41.2 s')
    const select = w.get('[data-test=round-interval]')
    expect(select.text()).toContain('1 minute · default')
    await select.setValue('300')
    await flushPromises()
    expect(api.setCollection).toHaveBeenCalledWith(300)
    // Picking the default stores "no setting".
    await select.setValue('60')
    expect(api.setCollection).toHaveBeenLastCalledWith(0)
  })
  it('runs a whole round and says when it is done', async () => {
    vi.useFakeTimers()
    const round = (started_at: string) => ({
      interval_s: 60,
      default_s: 60,
      round: { started_at, duration_ms: 9_000, order: [2], interval_s: 60 },
    })
    vi.mocked(api.collection).mockResolvedValue(round('2026-10-08T08:00:00Z'))
    vi.mocked(api.runRound).mockResolvedValue({ status: 'round scheduled' })
    const w = mount(CollectionRound, {
      props: { integrations: [integration(2, 'Network scan')] },
      global: { plugins: plugins() },
    })
    await flushPromises()
    const button = w.get('[data-test=run-round]')
    await button.trigger('click')
    await flushPromises()
    expect(api.runRound).toHaveBeenCalled()
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.text()).toContain('Collecting')

    // Still the old round: keeps waiting.
    await vi.advanceTimersByTimeAsync(2000)
    expect(button.attributes('disabled')).toBeDefined()

    vi.mocked(api.collection).mockResolvedValue(round('2026-10-08T08:01:00Z'))
    await vi.advanceTimersByTimeAsync(2000)
    expect(button.attributes('disabled')).toBeUndefined()
    expect(w.emitted('ran')).toHaveLength(1)
    vi.useRealTimers()
  })
})
