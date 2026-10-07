import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { session } from '@/lib/session'

import AuthView from '../AuthView.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      setup: vi.fn<typeof mod.api.setup>(),
      login: vi.fn<typeof mod.api.login>(),
      logout: vi.fn<typeof mod.api.logout>(),
    },
  }
})

const mountView = () => mount(AuthView, { global: { plugins: plugins() } })

describe('AuthView', () => {
  beforeEach(() => {
    vi.mocked(api.setup).mockReset()
    vi.mocked(api.login).mockReset()
    Object.assign(session, {
      ready: true,
      authenticated: false,
      setupRequired: false,
      username: '',
    })
  })

  it('creates the admin account on first run', async () => {
    session.setupRequired = true
    vi.mocked(api.setup).mockResolvedValue({
      setup_required: false,
      authenticated: true,
      username: 'admin',
    })
    const w = mountView()
    expect(w.text()).toContain('Create the admin account')

    await w.get('#password').setValue('homelab123')
    await w.get('#confirm').setValue('homelab123')
    await w.get('form').trigger('submit')
    await flushPromises()

    expect(api.setup).toHaveBeenCalledWith('admin', 'homelab123')
    expect(session.authenticated).toBe(true)
  })

  it('rejects different passwords without calling the API', async () => {
    session.setupRequired = true
    const w = mountView()
    await w.get('#password').setValue('homelab123')
    await w.get('#confirm').setValue('homelab124')
    await w.get('form').trigger('submit')

    expect(api.setup).not.toHaveBeenCalled()
    expect(w.text()).toContain('Passwords do not match')
  })

  it('shows the server error on a failed login', async () => {
    const { ApiError } = await import('@/lib/api')
    vi.mocked(api.login).mockRejectedValue(new ApiError(401, 'invalid username or password'))
    const w = mountView()
    expect(w.find('#confirm').exists()).toBe(false)

    await w.get('#password').setValue('wrong')
    await w.get('form').trigger('submit')
    await flushPromises()

    expect(w.get('[role=alert]').text()).toBe('invalid username or password')
    expect(session.authenticated).toBe(false)
  })
})
