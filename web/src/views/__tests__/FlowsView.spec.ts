import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { api } from '@/lib/api'
import { serviceName } from '@/lib/services'
import type { FlowsResponse } from '@/lib/types'

import { plugins } from '../../components/__tests__/helpers'
import FlowsView from '../FlowsView.vue'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      flows: vi.fn<typeof mod.api.flows>(),
      topology: vi.fn<typeof mod.api.topology>(),
    },
  }
})

const flows: FlowsResponse = {
  listening: true,
  exporters: [
    {
      ip: '192.168.1.1',
      kind: 'netflow',
      flows: 120,
      last_at: new Date().toISOString(),
      version: 'v9',
    },
  ],
  conversations: [
    {
      a: '192.168.1.20',
      b: '192.168.1.50',
      a_node: 'dev:nas',
      bytes_ab: 3e9,
      bytes_ba: 1e6,
      packets: 10,
      ports: [{ proto: 6, port: 445, bytes: 3e9 }],
    },
    {
      a: '192.168.1.50',
      b: '8.8.8.8',
      bytes_ab: 2000,
      bytes_ba: 4000,
      packets: 4,
      ports: [{ proto: 17, port: 53, bytes: 6000 }],
    },
  ],
}

function setup() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'map', component: { template: '<div />' } },
      { path: '/flows', component: FlowsView },
      { path: '/integrations', component: { template: '<div />' } },
    ],
  })
}

describe('FlowsView', () => {
  beforeEach(() => {
    vi.mocked(api.flows).mockResolvedValue(flows)
    vi.mocked(api.topology).mockResolvedValue({
      topology: {
        nodes: [{ id: 'dev:nas', kind: 'device', label: 'NAS', online: true }],
        edges: [],
      },
      statuses: [],
      generated_at: '',
      layout: {},
    })
  })

  it('lists conversations with names, traffic and services, and searches them', async () => {
    const router = setup()
    await router.push('/flows')
    const w = mount(FlowsView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    const rows = w.findAll('[data-test=conversation]')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.text()).toContain('NAS')
    expect(rows[0]!.text()).toContain('SMB')
    expect(rows[1]!.text()).toContain('8.8.8.8')
    expect(w.get('[data-test=flows-exporters]').text()).toContain('192.168.1.1 (NetFlow v9')
    await w.get('input[type=search]').setValue('dns')
    expect(w.findAll('[data-test=conversation]')).toHaveLength(1)
    await w.get('[data-test=flows-range-1440]').trigger('click')
    await flushPromises()
    expect(api.flows).toHaveBeenLastCalledWith({ minutes: 1440, node: undefined })
    w.unmount()
  })

  it('explains how to turn flow collection on', async () => {
    vi.mocked(api.flows).mockResolvedValue({ listening: false, exporters: [], conversations: [] })
    const router = setup()
    await router.push('/flows')
    const w = mount(FlowsView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    expect(w.get('[data-test=flows-off]').text()).toContain('Traffic flows')
    w.unmount()
  })

  it('names common services', () => {
    expect(serviceName(6, 443)).toBe('HTTPS')
    expect(serviceName(17, 51820)).toBe('WireGuard')
    expect(serviceName(6, 12345)).toBe('TCP 12345')
    expect(serviceName(1, 0)).toBe('ICMP')
  })
})
