import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import TrafficChart from '../TrafficChart.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return { ...mod, api: { ...mod.api, history: vi.fn<typeof mod.api.history>() } }
})

describe('TrafficChart', () => {
  beforeEach(() => {
    const now = Date.now()
    vi.mocked(api.history).mockResolvedValue([
      { at: new Date(now - 120_000).toISOString(), rx_bps: 2e6, tx_bps: 1e5 },
      { at: new Date(now - 60_000).toISOString(), rx_bps: 4e6, tx_bps: 3e5 },
    ])
  })

  it('draws the last day and its average and peak', async () => {
    const w = mount(TrafficChart, {
      props: { node: 'dev:fw', iface: 'igc0' },
      global: { plugins: plugins() },
    })
    await flushPromises()
    expect(api.history).toHaveBeenCalledWith('dev:fw', 'igc0', 24)
    // Only minutes recorded: the last hour shows them readably.
    expect(api.history).toHaveBeenLastCalledWith('dev:fw', 'igc0', 1)
    expect(w.get('[data-test=range-1]').classes()).toContain('on')
    expect(w.findAll('path.line')).toHaveLength(2)
    expect(w.get('[data-test=chart-stats]').text()).toContain('3.0 Mbps') // average down
    expect(w.get('[data-test=chart-stats]').text()).toContain('4.0 Mbps') // peak down
  })

  it("shows a port's optical module: its weakest signal and hottest temperature", async () => {
    const now = Date.now()
    vi.mocked(api.history).mockResolvedValue([
      {
        at: new Date(now - 120_000).toISOString(),
        rx_bps: 1,
        tx_bps: 1,
        rx_power_dbm: -9.5,
        temperature_c: 40,
      },
      {
        at: new Date(now - 60_000).toISOString(),
        rx_bps: 1,
        tx_bps: 1,
        rx_power_dbm: -12.25,
        temperature_c: 38,
      },
    ])
    const w = mount(TrafficChart, {
      props: { node: 'dev:sw', iface: 'SFP 1' },
      global: { plugins: plugins() },
    })
    await flushPromises()
    expect(w.get('[data-test=chart-rx-power]').text()).toContain('-12.3 dBm')
    expect(w.get('[data-test=chart-module-temp]').text()).toContain('40.0 °C')
  })

  it('shows no module for a port without one', async () => {
    const w = mount(TrafficChart, {
      props: { node: 'dev:fw', iface: 'igc0' },
      global: { plugins: plugins() },
    })
    await flushPromises()
    expect(w.find('[data-test=chart-rx-power]').exists()).toBe(false)
  })

  it('changes the range, and says when there is no data', async () => {
    const w = mount(TrafficChart, {
      props: { node: 'dev:fw', iface: 'igc0', swap: true },
      global: { plugins: plugins() },
    })
    await flushPromises()
    // Swapped: the port's sent traffic is the download.
    expect(w.get('[data-test=chart-stats]').text()).toMatch(/↓ 200 kbps/)
    vi.mocked(api.history).mockResolvedValue([])
    await w.get('[data-test=range-168]').trigger('click')
    await flushPromises()
    expect(api.history).toHaveBeenLastCalledWith('dev:fw', 'igc0', 168)
    expect(w.find('[data-test=chart-empty]').exists()).toBe(true)
  })
})
