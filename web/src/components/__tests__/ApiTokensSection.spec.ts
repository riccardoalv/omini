import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { ApiToken } from '@/lib/types'

import ApiTokensSection from '../ApiTokensSection.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      apiTokens: vi.fn<typeof mod.api.apiTokens>(),
      createApiToken: vi.fn<typeof mod.api.createApiToken>(),
      deleteApiToken: vi.fn<typeof mod.api.deleteApiToken>(),
    },
  }
})

const existing: ApiToken = {
  id: 1,
  name: 'grafana',
  created_at: '2026-10-01T10:00:00Z',
  last_used_at: null,
}

describe('ApiTokensSection', () => {
  const writeText = vi.fn<(s: string) => Promise<void>>().mockResolvedValue(undefined)
  beforeEach(() => {
    vi.mocked(api.apiTokens).mockReset().mockResolvedValue([existing])
    vi.mocked(api.createApiToken).mockReset()
    vi.mocked(api.deleteApiToken).mockReset().mockResolvedValue(undefined)
    writeText.mockClear()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  })
  afterEach(() => vi.restoreAllMocks())

  it('lists the tokens and explains the read-only use and the MCP address', async () => {
    const w = mount(ApiTokensSection, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.findAll('[data-test=token]')).toHaveLength(1)
    expect(w.text()).toContain('grafana')
    expect(w.text()).toContain('never used')
    expect(w.text()).toContain('never change anything')
    expect(w.get('[data-test=mcp-url]').text()).toBe(`${window.location.origin}/mcp`)
    expect(w.get('[data-test=snippet-cli]').text()).toContain(
      'claude mcp add --transport http omini',
    )
    const json = JSON.parse(w.get('[data-test=snippet-json]').text())
    expect(json.mcpServers.omini.url).toBe(`${window.location.origin}/mcp`)
  })

  it('creates a token and shows it once, with a copy button and a warning', async () => {
    vi.mocked(api.createApiToken).mockResolvedValue({
      id: 2,
      name: 'claude',
      token: 'omini_secret',
      created_at: '2026-10-18T10:00:00Z',
    })
    const w = mount(ApiTokensSection, { global: { plugins: plugins() } })
    await flushPromises()
    vi.mocked(api.apiTokens).mockResolvedValue([
      existing,
      { id: 2, name: 'claude', created_at: '2026-10-18T10:00:00Z', last_used_at: null },
    ])
    await w.get('[data-test=token-name]').setValue('  claude ')
    await w.get('[data-test=token-form]').trigger('submit')
    await flushPromises()
    expect(api.createApiToken).toHaveBeenCalledWith('claude')
    expect(w.get('[data-test=token-value]').text()).toBe('omini_secret')
    expect(w.get('[data-test=new-token]').text()).toContain('will not show it again')
    expect(w.findAll('[data-test=token]')).toHaveLength(2)
    // The example carries the new token, ready to paste.
    expect(w.get('[data-test=snippet-cli]').text()).toContain('Bearer omini_secret')

    await w.get('[data-test=copy-token]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('omini_secret')
    expect(w.get('[data-test=copy-token]').text()).toContain('Copied')

    await w.get('[data-test=token-done]').trigger('click')
    expect(w.find('[data-test=new-token]').exists()).toBe(false)
    expect(w.text()).not.toContain('omini_secret')
  })

  it('deletes a token only once confirmed', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false)
    const w = mount(ApiTokensSection, { global: { plugins: plugins() } })
    await flushPromises()
    await w.get('[data-test=delete-token]').trigger('click')
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('grafana'))
    expect(api.deleteApiToken).not.toHaveBeenCalled()

    confirm.mockReturnValueOnce(true)
    vi.mocked(api.apiTokens).mockResolvedValue([])
    await w.get('[data-test=delete-token]').trigger('click')
    await flushPromises()
    expect(api.deleteApiToken).toHaveBeenCalledWith(1)
    expect(w.find('[data-test=no-tokens]').exists()).toBe(true)
  })
})
