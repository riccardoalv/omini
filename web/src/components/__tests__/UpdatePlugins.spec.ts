import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '@/lib/api'
import type { PluginUpdate } from '@/lib/types'

import UpdatePlugins from '../UpdatePlugins.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      pluginUpdates: vi.fn<typeof mod.api.pluginUpdates>(),
      updateAllPlugins: vi.fn<typeof mod.api.updateAllPlugins>(),
    },
  }
})

const up = (id: string, installed: string, latest: string): PluginUpdate => ({
  id,
  name: id.toUpperCase(),
  installed,
  latest,
  update: installed !== latest,
})

const mountButton = async () => {
  const w = mount(UpdatePlugins, { global: { plugins: plugins() }, attachTo: document.body })
  await flushPromises()
  return w
}

describe('UpdatePlugins', () => {
  beforeEach(() => {
    vi.mocked(api.pluginUpdates).mockReset()
    vi.mocked(api.updateAllPlugins).mockReset()
  })

  it('is hidden when no plugin follows releases', async () => {
    vi.mocked(api.pluginUpdates).mockResolvedValue([
      { id: 'dev', name: 'Dev', installed: '1.0.0', update: false, reason: 'dev' },
      { id: 'pin', name: 'Pin', installed: 'main@1a2b3c4', update: false, reason: 'pinned' },
    ])
    const w = await mountButton()
    expect(w.find('[data-test=update-plugins]').exists()).toBe(false)
    w.unmount()
  })

  it('is disabled when the plugins are up to date', async () => {
    vi.mocked(api.pluginUpdates).mockResolvedValue([up('a', 'v1.0.0', 'v1.0.0')])
    const w = await mountButton()
    const b = w.get('[data-test=update-plugins]')
    expect(b.text()).toBe('Plugins up to date')
    expect(b.attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('is hidden when the updates cannot be read', async () => {
    vi.mocked(api.pluginUpdates).mockRejectedValue(new ApiError(502, 'GitHub'))
    const w = await mountButton()
    expect(w.find('[data-test=update-plugins]').exists()).toBe(false)
    w.unmount()
  })

  it('confirms, updates and shows each result', async () => {
    vi.mocked(api.pluginUpdates).mockResolvedValue([
      up('alpha', 'v1.0.0', 'v1.1.0'),
      up('bravo', 'v1.0.0', 'v2.0.0'),
      up('charlie', 'v3.0.0', 'v3.0.0'),
      up('delta', 'v0.1.0', 'v0.2.0'),
    ])
    let finish: (v: Awaited<ReturnType<typeof api.updateAllPlugins>>) => void = () => {}
    vi.mocked(api.updateAllPlugins).mockReturnValue(new Promise((r) => (finish = r)))
    const w = await mountButton()
    const b = w.get('[data-test=update-plugins]')
    expect(b.text()).toBe('Update 3 plugins')

    await b.trigger('click')
    const plan = document.body.querySelector('[data-test=update-plan]')!
    expect(plan.querySelectorAll('li')).toHaveLength(3)
    expect(plan.textContent).toContain('ALPHA')
    expect(plan.textContent).toContain('v1.0.0')
    expect(plan.textContent).toContain('v1.1.0')
    expect(plan.textContent).not.toContain('CHARLIE')
    expect(api.updateAllPlugins).not.toHaveBeenCalled()

    ;(document.body.querySelector('[data-test=update-confirm]') as HTMLElement).click()
    await flushPromises()
    expect(api.updateAllPlugins).toHaveBeenCalledTimes(1)
    expect(document.body.textContent).toContain('Updating… (can take a few minutes)')

    vi.mocked(api.pluginUpdates).mockResolvedValue([
      up('alpha', 'v1.1.0', 'v1.1.0'),
      up('bravo', 'v1.0.0', 'v2.0.0'),
      up('charlie', 'v3.0.0', 'v3.0.0'),
      up('delta', 'v0.2.0', 'v0.2.0'),
    ])
    finish([
      { id: 'alpha', from: 'v1.0.0', to: 'v1.1.0' },
      { id: 'bravo', from: 'v1.0.0', to: 'v2.0.0', error: 'download failed: 500' },
      { id: 'delta', from: 'v0.1.0', to: 'v0.2.0' },
    ])
    await flushPromises()
    const items = [...document.body.querySelectorAll('[data-test=update-results] li')]
    expect(items.map((li) => li.className)).toEqual(['done', 'failed', 'done'])
    expect(items[0]!.textContent).toContain('Updated')
    expect(items[1]!.textContent).toContain('Failed')
    expect(items[1]!.textContent).toContain('download failed: 500')
    expect(w.emitted('updated')).toHaveLength(1)
    // The button counts what is left.
    expect(w.get('[data-test=update-plugins]').text()).toBe('Update 1 plugin')

    ;(document.body.querySelector('[data-test=update-close]') as HTMLElement).click()
    await flushPromises()
    expect(document.body.querySelector('[data-test=update-results]')).toBeNull()
    w.unmount()
  })

  it('does not reload anything when every update failed', async () => {
    vi.mocked(api.pluginUpdates).mockResolvedValue([up('alpha', 'v1.0.0', 'v1.1.0')])
    vi.mocked(api.updateAllPlugins).mockResolvedValue([
      { id: 'alpha', from: 'v1.0.0', to: 'v1.1.0', error: 'boom' },
    ])
    const w = await mountButton()
    await w.get('[data-test=update-plugins]').trigger('click')
    ;(document.body.querySelector('[data-test=update-confirm]') as HTMLElement).click()
    await flushPromises()
    expect(document.body.textContent).toContain('boom')
    expect(w.emitted('updated')).toBeUndefined()
    w.unmount()
  })
})
