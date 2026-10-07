import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { PluginInfo } from '@/lib/types'

import PluginsSection from '../PluginsSection.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      plugins: vi.fn<typeof mod.api.plugins>(),
      installPlugin: vi.fn<typeof mod.api.installPlugin>(),
      removePlugin: vi.fn<typeof mod.api.removePlugin>(),
    },
  }
})

const opnsense: PluginInfo = {
  manifest: { id: 'opnsense', name: 'OPNsense', version: '0.1.0' },
  source: {
    url: 'https://github.com/riccardoalv/omini-plugin-opnsense',
    version: 'v0.1.0',
    installed_at: '2026-10-06T00:00:00Z',
  },
  dev: false,
}

const mountSection = () => mount(PluginsSection, { global: { plugins: plugins() } })

describe('PluginsSection', () => {
  beforeEach(() => {
    vi.mocked(api.plugins).mockReset().mockResolvedValue([])
    vi.mocked(api.installPlugin).mockReset()
    vi.mocked(api.removePlugin).mockReset().mockResolvedValue()
  })

  it('installs a plugin from its GitHub address', async () => {
    vi.mocked(api.installPlugin).mockResolvedValue(opnsense)
    const w = mountSection()
    await flushPromises()
    expect(w.text()).toContain('No plugins installed.')

    vi.mocked(api.plugins).mockResolvedValue([opnsense])
    await w.get('#p-url').setValue(' https://github.com/riccardoalv/omini-plugin-opnsense ')
    await w.get('form').trigger('submit')
    await flushPromises()

    expect(api.installPlugin).toHaveBeenCalledWith(
      'https://github.com/riccardoalv/omini-plugin-opnsense',
      undefined,
    )
    expect(w.get('[role=status]').text()).toBe('OPNsense 0.1.0 installed: add it in Integrations.')
    expect(w.findAll('[data-test=plugin]')).toHaveLength(1)
  })

  it('shows why an install failed', async () => {
    const { ApiError } = await import('@/lib/api')
    vi.mocked(api.installPlugin).mockRejectedValue(new ApiError(400, 'no release yet'))
    const w = mountSection()
    await w.get('#p-url').setValue('https://github.com/a/b')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role=alert]').text()).toBe('no release yet')
  })

  it('removes after a second click; development plugins cannot be removed', async () => {
    vi.mocked(api.plugins).mockResolvedValue([
      opnsense,
      { manifest: { id: 'dev', name: 'Dev', version: '0' }, dev: true },
    ])
    const w = mountSection()
    await flushPromises()
    const buttons = w.findAll('[data-test=remove]')
    expect(buttons).toHaveLength(1)
    await buttons[0]!.trigger('click')
    expect(api.removePlugin).not.toHaveBeenCalled()
    await buttons[0]!.trigger('click')
    await flushPromises()
    expect(api.removePlugin).toHaveBeenCalledWith('opnsense')
  })
})
