import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import ScanCapabilities from '../ScanCapabilities.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return { ...mod, api: { ...mod.api, capabilities: vi.fn<typeof mod.api.capabilities>() } }
})

describe('ScanCapabilities', () => {
  it('says what works here and how to fix the rest', async () => {
    vi.mocked(api.capabilities).mockResolvedValue({
      capabilities: {
        container: true,
        host_network: false,
        unprivileged_ping: false,
        raw_sockets: true,
        multicast: false,
      },
      limited: ['host_network', 'multicast'],
    })
    const w = mount(ScanCapabilities, {
      props: { type: 'network' },
      global: { plugins: plugins() },
    })
    await flushPromises()
    expect(w.get('[data-test=cap-host_network]').text()).toContain('network_mode: host')
    expect(w.get('[data-test=cap-host_network]').classes()).not.toContain('ok')
    // Root pings with raw sockets.
    expect(w.get('[data-test=cap-ping]').classes()).toContain('ok')
    // Raw sockets are nmap's concern, not the scan's.
    expect(w.find('[data-test=cap-raw_sockets]').exists()).toBe(false)
  })
})
