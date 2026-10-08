import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { plugins } from '@/components/__tests__/helpers'
import { api } from '@/lib/api'

import SettingsView from '../SettingsView.vue'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      health: vi.fn<typeof mod.api.health>().mockResolvedValue({ status: 'ok', version: '0.4.0' }),
      notifierTypes: vi.fn<typeof mod.api.notifierTypes>().mockResolvedValue([]),
      notifiers: vi.fn<typeof mod.api.notifiers>().mockResolvedValue([]),
      plugins: vi.fn<typeof mod.api.plugins>().mockResolvedValue([]),
    },
  }
})

describe('SettingsView', () => {
  it('shows one section at a time from the menu, kept in the address', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/settings', component: SettingsView }],
    })
    await router.push('/settings')
    const w = mount(SettingsView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    expect(w.find('#s-theme').exists()).toBe(true)
    expect(w.find('[data-test=notifications]').exists()).toBe(false)

    await w.get('[data-test=section-notifications]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.section).toBe('notifications')
    expect(w.find('[data-test=notifications]').exists()).toBe(true)
    expect(w.find('#s-theme').exists()).toBe(false)
    // No plugin list here: the store lives in Integrations.
    expect(w.text()).not.toContain('Open the store')
    expect(api.plugins).not.toHaveBeenCalled()

    await w.get('[data-test=section-about]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Version 0.4.0')
  })
})
