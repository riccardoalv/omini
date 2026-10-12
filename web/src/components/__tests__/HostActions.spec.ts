import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { TopoNode } from '@/lib/types'

import HostActions from '../map/HostActions.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      hostActions: vi.fn<typeof mod.api.hostActions>(),
      wake: vi.fn<typeof mod.api.wake>(),
      pause: vi.fn<typeof mod.api.pause>(),
      resume: vi.fn<typeof mod.api.resume>(),
    },
  }
})

const pc = (online: boolean): TopoNode => ({
  id: 'mac:pc',
  kind: 'client',
  label: 'Desktop',
  online,
  mac: 'AA:BB:CC:00:00:09',
})
const fw = (paused: boolean): TopoNode => ({
  id: 'dev:fw',
  kind: 'device',
  label: 'fw',
  online: true,
  device: {
    key: 'fw',
    name: 'fw',
    paused: paused ? [{ mac: 'aa:bb:cc:00:00:09', ips: ['192.168.1.9'] }] : [],
  },
})
const mountIt = (node: TopoNode, nodes: TopoNode[]) =>
  mount(HostActions, { props: { node, nodes }, global: { plugins: plugins() } })

describe('HostActions', () => {
  beforeEach(() => {
    vi.mocked(api.hostActions).mockResolvedValue({
      wake: { via: 'router', router: 'dev:fw' },
      can_pause: true,
    })
    for (const f of [api.wake, api.pause, api.resume]) vi.mocked(f).mockResolvedValue({} as never)
  })

  it('wakes an offline device and pauses its internet', async () => {
    const w = mountIt(pc(false), [fw(false)])
    await flushPromises()
    await w.get('[data-test=wake]').trigger('click')
    await flushPromises()
    expect(api.wake).toHaveBeenCalledWith('mac:pc')
    expect(w.emitted('done')![0]).toEqual([expect.stringContaining('Wake-on-LAN')])
    await w.get('[data-test=pause]').trigger('click')
    await flushPromises()
    expect(api.pause).toHaveBeenCalledWith('mac:pc')
    expect(w.emitted('applied')).toHaveLength(2)
  })

  it('an online device has no wake button; a paused one resumes', async () => {
    const w = mountIt(pc(true), [fw(true)])
    await flushPromises()
    expect(w.find('[data-test=wake]').exists()).toBe(false)
    expect(w.find('[data-test=pause]').exists()).toBe(false)
    await w.get('[data-test=resume]').trigger('click')
    await flushPromises()
    expect(api.resume).toHaveBeenCalledWith('mac:pc')
  })

  it('tells what went wrong', async () => {
    vi.mocked(api.pause).mockRejectedValue(new Error('x'))
    const w = mountIt(pc(true), [fw(false)])
    await flushPromises()
    await w.get('[data-test=pause]').trigger('click')
    await flushPromises()
    expect(w.emitted('error')).toHaveLength(1)
  })
})
