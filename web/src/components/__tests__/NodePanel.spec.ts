import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { TopoNode } from '@/lib/types'

import NodePanel from '../map/NodePanel.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return { ...mod, api: { ...mod.api, webServices: vi.fn<typeof mod.api.webServices>() } }
})

const firewall: TopoNode = {
  id: 'dev:00:e0:4c:68:00:02',
  kind: 'device',
  label: 'opnsense',
  role: 'firewall',
  online: true,
  ip: '192.168.1.1',
}

const mountPanel = (node: TopoNode) =>
  mount(NodePanel, {
    props: { node, nodes: [node], edges: [], expandedParent: false },
    global: { plugins: plugins(), stubs: { RouterLink: true } },
  })

describe('NodePanel web interface', () => {
  beforeEach(() => vi.mocked(api.webServices).mockReset())

  it('offers to open the detected web interface in a new tab', async () => {
    vi.mocked(api.webServices).mockResolvedValue([
      { url: 'https://192.168.1.1/', port: 443, title: 'OPNsense' },
      { url: 'http://192.168.1.1:8080/', port: 8080, title: 'Status page' },
    ])
    const w = mountPanel(firewall)
    await flushPromises()

    expect(api.webServices).toHaveBeenCalledWith(firewall.id)
    const links = w.findAll('[data-test=open-web]')
    expect(links).toHaveLength(2)
    expect(links[0]!.attributes()).toMatchObject({
      href: 'https://192.168.1.1/',
      target: '_blank',
      rel: 'noopener noreferrer',
    })
    expect(links[0]!.text()).toContain('Open web interface')
    expect(links[1]!.text()).toContain('Status page')
  })

  it('shows nothing when there is no web interface', async () => {
    vi.mocked(api.webServices).mockResolvedValue([])
    const w = mountPanel(firewall)
    await flushPromises()
    expect(w.find('[data-test=open-web]').exists()).toBe(false)
    expect(w.text()).not.toContain('Looking for a web interface')
  })

  it('does not probe nodes without an IP', async () => {
    mountPanel({ ...firewall, id: 'seg:x', kind: 'segment', ip: undefined })
    await flushPromises()
    expect(api.webServices).not.toHaveBeenCalled()
  })
})
