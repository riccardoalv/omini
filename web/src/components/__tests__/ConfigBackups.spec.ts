import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { prefs } from '@/lib/prefs'
import type { Backup } from '@/lib/types'

import ConfigBackups from '../map/ConfigBackups.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      backups: vi.fn<typeof mod.api.backups>(),
      backup: vi.fn<typeof mod.api.backup>(),
    },
  }
})

const v = (id: number, by: boolean): Backup => ({
  id,
  node_id: 'dev:fw',
  integration_id: 1,
  taken_at: `2026-10-1${id}T10:00:00Z`,
  format: 'text',
  size: 40,
  by_omini: by,
})
const texts: Record<number, string> = {
  1: 'hostname fw\nlan 192.168.1.1/24\n',
  2: 'hostname fw\nlan 192.168.1.1/24\nrule pass any\n',
}
const mountIt = () =>
  mount(ConfigBackups, {
    props: { nodeId: 'dev:fw', label: 'fw' },
    global: { plugins: plugins() },
  })

describe('ConfigBackups', () => {
  beforeEach(() => {
    prefs.openSections = ['config']
    vi.mocked(api.backup).mockImplementation(async (id) => ({
      ...v(id, false),
      content: texts[id]!,
    }))
  })

  it('shows the latest change against the version before', async () => {
    vi.mocked(api.backups).mockResolvedValue([v(2, false), v(1, true)])
    const w = mountIt()
    await flushPromises()
    const diff = w.get('[data-test=backup-diff]')
    expect(diff.find('.add').text()).toBe('+ rule pass any')
    expect(w.get('[data-test=backup-to]').text()).toContain('outside Omini')
    expect(w.get('[data-test=backup-from]').text()).toContain('by Omini')
  })

  it('a device without backups has no section', async () => {
    vi.mocked(api.backups).mockResolvedValue([])
    const w = mountIt()
    await flushPromises()
    expect(w.find('[data-test=config-backups]').exists()).toBe(false)
  })

  it('one version: no differences yet', async () => {
    vi.mocked(api.backups).mockResolvedValue([v(1, false)])
    const w = mountIt()
    await flushPromises()
    expect(w.text()).toContain('One version so far')
    expect(w.find('[data-test=backup-diff]').exists()).toBe(false)
  })
})
