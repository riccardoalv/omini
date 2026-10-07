import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { CatalogEntry, PluginInfo } from '@/lib/types'

import PluginStore from '../PluginStore.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      plugins: vi.fn<typeof mod.api.plugins>(),
      pluginCatalog: vi.fn<typeof mod.api.pluginCatalog>(),
      installPlugin: vi.fn<typeof mod.api.installPlugin>(),
      removePlugin: vi.fn<typeof mod.api.removePlugin>(),
    },
  }
})

const URL = 'https://github.com/riccardoalv/omini-plugin-opnsense'
const opnsenseEntry: CatalogEntry = {
  id: 'opnsense',
  name: 'OPNsense',
  description: 'OPNsense firewall through its REST API',
  url: URL,
  icon: 'opnsense',
  publisher: 'official',
  trust: 'experimental',
  installed: false,
}
const mikrotikEntry: CatalogEntry = {
  ...opnsenseEntry,
  id: 'mikrotik',
  name: 'MikroTik',
  description: 'RouterOS',
  url: 'https://github.com/x/omini-plugin-mikrotik',
}
const installed = (id: string, url?: string): PluginInfo => ({
  manifest: { id, name: id, version: '0.1.0' },
  source: url ? { url, version: 'main@1a2b3c4', installed_at: '' } : undefined,
  dev: false,
  publisher: 'official',
  trust: 'experimental',
})

const mountStore = async () => {
  const w = mount(PluginStore, { global: { plugins: plugins() } })
  await flushPromises()
  return w
}

describe('PluginStore', () => {
  beforeEach(() => {
    vi.mocked(api.pluginCatalog).mockReset().mockResolvedValue([opnsenseEntry, mikrotikEntry])
    vi.mocked(api.plugins).mockReset().mockResolvedValue([])
    vi.mocked(api.installPlugin).mockReset().mockResolvedValue(installed('opnsense', URL))
    vi.mocked(api.removePlugin).mockReset().mockResolvedValue()
  })

  it('shows the catalog as cards with trust badges and searches it', async () => {
    const w = await mountStore()
    const cards = w.findAll('[data-test=store-card]')
    expect(cards).toHaveLength(2)
    expect(cards[0]!.text()).toContain('OPNsense')
    expect(cards[0]!.text()).toContain('Official')
    expect(cards[0]!.text()).toContain('riccardoalv/omini-plugin-opnsense')

    await w.get('[data-test=store-search]').setValue('router')
    expect(w.findAll('[data-test=store-card]').map((c) => c.text())).toEqual([
      expect.stringContaining('MikroTik'),
    ])
    await w.get('[data-test=store-search]').setValue('zzz')
    expect(w.text()).toContain('No plugin matches “zzz”.')
  })

  it('installs from a card; installed plugins offer to add an integration', async () => {
    const w = await mountStore()
    vi.mocked(api.plugins).mockResolvedValue([installed('opnsense', URL)])
    await w.get('[data-test=store-install]').trigger('click')
    await flushPromises()
    expect(api.installPlugin).toHaveBeenCalledWith(URL, undefined)
    expect(w.emitted('changed')).toHaveLength(1)

    const card = w.findAll('[data-test=store-card]')[0]!
    expect(card.get('[data-test=store-installed]').text()).toContain('main@1a2b3c4')
    await card.get('[data-test=store-use]').trigger('click')
    expect(w.emitted('add')![0]).toEqual(['opnsense'])
  })

  it('filters installed and available plugins', async () => {
    vi.mocked(api.plugins).mockResolvedValue([installed('opnsense', URL)])
    const w = await mountStore()
    await w.get('[data-test=store-filter-installed]').trigger('click')
    expect(w.findAll('[data-test=store-card]')).toHaveLength(1)
    await w.get('[data-test=store-filter-available]').trigger('click')
    expect(w.findAll('[data-test=store-card]').map((c) => c.text())).toEqual([
      expect.stringContaining('MikroTik'),
    ])
  })

  it('adds any GitHub repository with the + button', async () => {
    const other = {
      ...installed('custom', 'https://github.com/me/omini-plugin-custom'),
      publisher: 'community' as const,
      trust: 'unverified' as const,
    }
    vi.mocked(api.installPlugin).mockResolvedValue(other)
    const w = await mountStore()
    expect(w.find('[data-test=store-add-form]').exists()).toBe(false)
    await w.get('[data-test=store-add]').trigger('click')
    vi.mocked(api.plugins).mockResolvedValue([other])
    await w.get('[data-test=store-url]').setValue('https://github.com/me/omini-plugin-custom')
    await w.get('[data-test=store-add-form]').trigger('submit')
    await flushPromises()

    expect(api.installPlugin).toHaveBeenCalledWith(
      'https://github.com/me/omini-plugin-custom',
      undefined,
    )
    expect(w.find('[data-test=store-add-form]').exists()).toBe(false)
    // Shown with the installed ones, marked unverified.
    const cards = w.findAll('[data-test=store-card]')
    expect(cards).toHaveLength(1)
    expect(cards[0]!.text()).toContain('Unverified')
  })

  it('shows install errors on the card', async () => {
    const { ApiError } = await import('@/lib/api')
    vi.mocked(api.installPlugin).mockRejectedValue(new ApiError(400, 'repository not found'))
    const w = await mountStore()
    await w.get('[data-test=store-install]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-test=store-card]')[0]!.get('[role=alert]').text()).toBe(
      'repository not found',
    )
  })

  it('removes after a second click', async () => {
    vi.mocked(api.plugins).mockResolvedValue([installed('opnsense', URL)])
    const w = await mountStore()
    const remove = w.get('[data-test=store-remove]')
    await remove.trigger('click')
    expect(api.removePlugin).not.toHaveBeenCalled()
    await remove.trigger('click')
    await flushPromises()
    expect(api.removePlugin).toHaveBeenCalledWith('opnsense')
  })
})
