import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '@/lib/api'
import type { ChannelPlan, ChannelStep } from '@/lib/types'

import WifiChannelsDialog from '../map/WifiChannelsDialog.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      wifiChannelPlan: vi.fn<typeof mod.api.wifiChannelPlan>(),
      applyWifiChannels: vi.fn<typeof mod.api.applyWifiChannels>(),
      changeSettings: vi.fn<typeof mod.api.changeSettings>(),
    },
  }
})

const office: ChannelStep = {
  node_id: 'dev:office',
  label: 'office',
  radio: 'wifi0',
  from: 6,
  to: 1,
  clients: 1,
}
const attic: ChannelStep = {
  node_id: 'dev:attic',
  label: 'attic',
  radio: 'wifi0',
  from: 6,
  to: 11,
  clients: 2,
  browser: true,
}
const plan = (over: Partial<ChannelPlan> = {}): ChannelPlan => ({
  steps: [office, attic],
  skipped: [
    { node_id: 'dev:mesh', label: 'mesh', radio: 'r', channel: 6, clients: 0, skip: 'mesh' },
  ],
  ...over,
})
const mountDialog = () => mount(WifiChannelsDialog, { global: { plugins: plugins() } })

describe('WifiChannelsDialog', () => {
  beforeEach(() => {
    vi.mocked(api.applyWifiChannels).mockReset()
    vi.mocked(api.changeSettings).mockResolvedValue({ allow_destructive: false, dns_domain: '' })
  })

  it('shows the plan with clients, the browser and those left alone, then applies', async () => {
    vi.mocked(api.wifiChannelPlan).mockResolvedValue(plan())
    vi.mocked(api.applyWifiChannels).mockResolvedValue({
      results: [
        { ...office, status: 'done' },
        { ...attic, status: 'done' },
      ],
    })
    const w = mountDialog()
    await flushPromises()
    const steps = w.findAll('[data-test="wc-step"]')
    expect(steps).toHaveLength(2)
    expect(steps[0]!.text()).toContain('office (wifi0): channel 6 → 1')
    expect(steps[0]!.text()).toContain('1 client reconnects')
    expect(steps[1]!.text()).toContain('2 clients reconnect')
    expect(steps[1]!.find('[data-test="wc-browser"]').exists()).toBe(true)
    expect(steps[0]!.find('[data-test="wc-browser"]').exists()).toBe(false)
    expect(w.get('[data-test="wc-skipped"]').text()).toContain('mesh (6): a mesh unit')
    expect(w.get('[data-test="wc-apply"]').text()).toBe('Move 2 access points')

    await w.get('[data-test="wc-apply"]').trigger('click')
    await flushPromises()
    expect(api.applyWifiChannels).toHaveBeenCalledWith(false)
    expect(w.get('[data-test="wc-done"]').text()).toContain('Every access point')
    expect(w.findAll('[data-status="done"]')).toHaveLength(2)
    expect(w.emitted('applied')).toHaveLength(1)
    expect(w.find('[data-test="wc-apply"]').exists()).toBe(false)
  })

  it('shows where it stopped: those done stay', async () => {
    vi.mocked(api.wifiChannelPlan).mockResolvedValue(plan())
    vi.mocked(api.applyWifiChannels).mockRejectedValue(
      new ApiError(502, 'attic: did not answer', {
        error: 'attic: did not answer',
        results: [
          { ...office, status: 'done' },
          { ...attic, status: 'failed', error: 'did not answer' },
        ],
      }),
    )
    const w = mountDialog()
    await flushPromises()
    await w.get('[data-test="wc-apply"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-test="wc-stopped"]').text()).toContain('Stopped at attic')
    expect(w.findAll('[data-status="done"]')).toHaveLength(1)
    expect(w.get('[data-status="failed"]').element.closest('li')!.textContent).toContain(
      'Failed: did not answer',
    )
  })

  it('blocks a step that cuts Omini off unless destructive changes are allowed', async () => {
    vi.mocked(api.wifiChannelPlan).mockResolvedValue(
      plan({ steps: [{ ...office, protected: { reason: 'omini' } }] }),
    )
    const w = mountDialog()
    await flushPromises()
    expect(w.find('[data-test="wc-omini"]').exists()).toBe(true)
    expect(w.find('[data-test="wc-blocked"]').exists()).toBe(true)
    expect(w.get('[data-test="wc-apply"]').attributes('disabled')).toBeDefined()
  })

  it('asks twice for a destructive step when allowed', async () => {
    vi.mocked(api.changeSettings).mockResolvedValue({ allow_destructive: true, dns_domain: '' })
    vi.mocked(api.wifiChannelPlan).mockResolvedValue(
      plan({ steps: [{ ...office, protected: { reason: 'omini' } }] }),
    )
    vi.mocked(api.applyWifiChannels).mockResolvedValue({ results: [{ ...office, status: 'done' }] })
    const w = mountDialog()
    await flushPromises()
    await w.get('[data-test="wc-apply"]').trigger('click')
    expect(api.applyWifiChannels).not.toHaveBeenCalled()
    expect(w.find('[data-test="wc-again"]').exists()).toBe(true)
    expect(w.get('[data-test="wc-apply"]').text()).toBe('Apply anyway')
    await w.get('[data-test="wc-apply"]').trigger('click')
    await flushPromises()
    expect(api.applyWifiChannels).toHaveBeenCalledWith(true)
  })

  it('has no button when nothing can move', async () => {
    vi.mocked(api.wifiChannelPlan).mockResolvedValue(plan({ steps: [] }))
    const w = mountDialog()
    await flushPromises()
    expect(w.find('[data-test="wc-none"]').exists()).toBe(true)
    expect(w.find('[data-test="wc-apply"]').exists()).toBe(false)
  })
})
