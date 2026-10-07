import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { CatalogEntry, PluginInfo } from '@/lib/types'

import PluginsSection from '../PluginsSection.vue'
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
const entry: CatalogEntry = {
  id: 'opnsense',
  name: 'OPNsense',
  description: 'OPNsense firewall',
  url: URL,
  publisher: 'official',
  trust: 'experimental',
  installed: false,
}
const opnsense: PluginInfo = {
  manifest: { id: 'opnsense', name: 'OPNsense', version: '0.1.0' },
  source: { url: URL, version: 'main@1a2b3c4', installed_at: '2026-10-06T00:00:00Z' },
  dev: false,
  publisher: 'official',
  trust: 'experimental',
}

const mountSection = () => mount(PluginsSection, { global: { plugins: plugins() } })

describe('PluginsSection', () => {
  beforeEach(() => {
    vi.mocked(api.plugins).mockReset().mockResolvedValue([])
    vi.mocked(api.pluginCatalog).mockReset().mockResolvedValue([entry])
    vi.mocked(api.installPlugin).mockReset().mockResolvedValue(opnsense)
    vi.mocked(api.removePlugin).mockReset().mockResolvedValue()
  })

  it('lists the curated plugins with their trust and installs them in one click', async () => {
    const w = mountSection()
    await flushPromises()
    const card = w.get('[data-test=catalog-entry]')
    expect(card.text()).toContain('OPNsense')
    expect(card.text()).toContain('Official')
    expect(card.text()).toContain('Experimental')

    vi.mocked(api.plugins).mockResolvedValue([opnsense])
    vi.mocked(api.pluginCatalog).mockResolvedValue([{ ...entry, installed: true }])
    await w.get('[data-test=install-catalog]').trigger('click')
    await flushPromises()

    expect(api.installPlugin).toHaveBeenCalledWith(URL, undefined)
    expect(w.get('[role=status]').text()).toBe('OPNsense 0.1.0 installed: add it in Integrations.')
    expect(w.find('[data-test=catalog-entry]').exists()).toBe(false)
    expect(w.get('[data-test=plugin]').text()).toContain('main@1a2b3c4')
  })

  it('installs from any GitHub address, optionally a version', async () => {
    const w = mountSection()
    await w.get('#p-url').setValue('https://github.com/someone/omini-plugin-x')
    await w.get('#p-version').setValue('v1.2.0')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(api.installPlugin).toHaveBeenCalledWith(
      'https://github.com/someone/omini-plugin-x',
      'v1.2.0',
    )
  })

  it('shows why an install failed', async () => {
    const { ApiError } = await import('@/lib/api')
    vi.mocked(api.installPlugin).mockRejectedValue(new ApiError(400, 'not a plugin'))
    const w = mountSection()
    await w.get('#p-url').setValue('https://github.com/a/b')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role=alert]').text()).toBe('not a plugin')
  })

  it('updates and removes installed plugins; development plugins are left alone', async () => {
    vi.mocked(api.plugins).mockResolvedValue([
      opnsense,
      {
        manifest: { id: 'dev', name: 'Dev', version: '0' },
        dev: true,
        publisher: 'community',
        trust: 'unverified',
      },
    ])
    const w = mountSection()
    await flushPromises()
    expect(w.findAll('[data-test=update]')).toHaveLength(1)

    await w.get('[data-test=update]').trigger('click')
    await flushPromises()
    expect(api.installPlugin).toHaveBeenCalledWith(URL, undefined)

    const remove = w.get('[data-test=remove]')
    await remove.trigger('click')
    expect(api.removePlugin).not.toHaveBeenCalled()
    await remove.trigger('click')
    await flushPromises()
    expect(api.removePlugin).toHaveBeenCalledWith('opnsense')
  })
})
