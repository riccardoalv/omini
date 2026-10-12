import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import DnsNameRow from '../map/DnsNameRow.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      dnsName: vi.fn<typeof mod.api.dnsName>(),
      setDnsName: vi.fn<typeof mod.api.setDnsName>(),
    },
  }
})

const row = () =>
  mount(DnsNameRow, {
    props: { nodeId: 'mac:tv', nameOf: (id: string) => (id === 'dev:ag' ? 'AdGuard' : id) },
    global: { plugins: plugins() },
  })

describe('DnsNameRow', () => {
  beforeEach(() => {
    vi.mocked(api.dnsName).mockResolvedValue({
      name: 'living-room-tv.lan',
      label: 'living-room-tv',
      server: 'dev:ag',
      others: [
        { name: 'living-room-tv.lan', ip: '192.168.1.41' },
        { name: 'tv.old.lan', ip: '192.168.1.41' },
      ],
    })
  })

  it('shows the name, where it is, and the other names of the address', async () => {
    const w = row()
    await flushPromises()
    const text = w.get('[data-test=dns-name]').text()
    expect(text).toContain('living-room-tv.lan')
    expect(text).toContain('on AdGuard')
    expect(text).toContain('Also tv.old.lan')
    expect(text.match(/living-room-tv\.lan/g)).toHaveLength(1)
  })

  it('renames it and turns it off', async () => {
    vi.mocked(api.setDnsName).mockResolvedValue({ name: 'tv.lan', label: 'tv', custom: true })
    const w = row()
    await flushPromises()
    await w.get('[data-test=dns-edit]').trigger('click')
    await w.get('[data-test=dns-label]').setValue('tv')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(api.setDnsName).toHaveBeenCalledWith('mac:tv', 'tv')
    expect(w.text()).toContain('tv.lan')
    vi.mocked(api.setDnsName).mockResolvedValue({ off: true, label: 'tv' })
    await w.get('[data-test=dns-off]').trigger('click')
    await flushPromises()
    expect(api.setDnsName).toHaveBeenLastCalledWith('mac:tv', '-')
    expect(w.find('[data-test=dns-on]').exists()).toBe(true)
  })

  it('says why there is no name', async () => {
    vi.mocked(api.dnsName).mockResolvedValue({ label: 'tv', missing: 'no_fixed_ip' })
    const w = row()
    await flushPromises()
    expect(w.text()).toContain('Reserve its IP')
    expect(w.find('[data-test=dns-edit]').exists()).toBe(false)
  })
})
