import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { plugins } from '@/components/__tests__/helpers'
import { api, ApiError } from '@/lib/api'

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
      changePassword: vi.fn<typeof mod.api.changePassword>(),
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
  it('changes the password, checking it before sending', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/settings', component: SettingsView }],
    })
    await router.push('/settings?section=account')
    const w = mount(SettingsView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    await w.get('[data-test=change-password]').trigger('click')
    const [current, next, again] = w.findAll('[data-test=password-form] input')
    await current!.setValue('old password')
    await next!.setValue('new password')
    await again!.setValue('new passw0rd')
    await w.get('[data-test=password-form]').trigger('submit')
    expect(w.text()).toContain('do not match')
    expect(api.changePassword).not.toHaveBeenCalled()

    vi.mocked(api.changePassword).mockRejectedValueOnce(new ApiError(403, 'wrong'))
    await again!.setValue('new password')
    await w.get('[data-test=password-form]').trigger('submit')
    await flushPromises()
    expect(api.changePassword).toHaveBeenCalledWith('old password', 'new password')
    expect(w.text()).toContain('current password is wrong')

    vi.mocked(api.changePassword).mockResolvedValueOnce(undefined)
    await w.get('[data-test=password-form]').trigger('submit')
    await flushPromises()
    expect(w.find('[data-test=password-form]').exists()).toBe(false)
    expect(w.find('[data-test=password-saved]').exists()).toBe(true)
  })
})
