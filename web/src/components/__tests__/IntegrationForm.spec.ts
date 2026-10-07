import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { Integration, IntegrationType } from '@/lib/types'

import IntegrationForm from '../IntegrationForm.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      testIntegration: vi.fn<typeof mod.api.testIntegration>(),
      createIntegration: vi.fn<typeof mod.api.createIntegration>(),
      updateIntegration: vi.fn<typeof mod.api.updateIntegration>(),
    },
  }
})

const snmp: IntegrationType = {
  type: 'snmp',
  name: 'SNMP (generic)',
  kind: 'core',
  fields: [
    { key: 'host', type: 'host', label: 'Host', required: true },
    { key: 'community', type: 'secret', label: 'Community', required: true, default: 'public' },
    { key: 'port', type: 'int', label: 'Port', default: 161 },
  ],
}

const mountForm = (props: Record<string, unknown> = {}) =>
  mount(IntegrationForm, { props: { type: snmp, ...props }, global: { plugins: plugins() } })

describe('IntegrationForm', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders inputs from the field definitions', () => {
    const w = mountForm()
    expect(w.get('input[name=host]').attributes('type')).toBe('text')
    expect(w.get('input[name=community]').attributes('type')).toBe('password')
    expect((w.get('input[name=port]').element as HTMLInputElement).value).toBe('161')
  })

  it('cannot be saved until required fields are filled', async () => {
    const w = mountForm()
    const save = w.get('button[type=submit]')
    expect(save.attributes('disabled')).toBeDefined()
    await w.get('input[name=host]').setValue('192.168.1.2')
    expect(save.attributes('disabled')).toBeUndefined()
  })

  it('tests the connection and shows the result', async () => {
    vi.mocked(api.testIntegration).mockResolvedValue({ ok: true, message: 'Connected to sw-core' })
    const w = mountForm({ prefill: { host: '192.168.1.2' } })
    await w
      .findAll('button')
      .find((b) => b.text() === 'Test connection')!
      .trigger('click')
    await flushPromises()

    expect(api.testIntegration).toHaveBeenCalledWith({
      id: undefined,
      type: 'snmp',
      config: { host: '192.168.1.2', community: 'public', port: 161 },
    })
    expect(w.get('[role=status]').text()).toBe('Connected to sw-core')
  })

  it('creates a new integration with typed values', async () => {
    const created = { id: 1 } as Integration
    vi.mocked(api.createIntegration).mockResolvedValue(created)
    const w = mountForm({ prefill: { host: '192.168.1.2' }, prefillName: 'sw-core' })
    await w.get('input[name=port]').setValue('1161')
    await w.get('form').trigger('submit')
    await flushPromises()

    expect(api.createIntegration).toHaveBeenCalledWith({
      name: 'sw-core',
      type: 'snmp',
      config: { host: '192.168.1.2', community: 'public', port: 1161 },
    })
    expect(w.emitted('saved')![0]).toEqual([created])
  })

  it('edits without showing the saved secret: blank keeps it, a new value replaces it', async () => {
    const existing = {
      id: 7,
      name: 'core switch',
      type: 'snmp',
      config: { host: '192.168.1.2', community: '********', port: 161 },
    } as unknown as Integration
    vi.mocked(api.updateIntegration).mockResolvedValue(existing)
    const w = mountForm({ existing })

    const community = w.get('input[name=community]')
    expect((community.element as HTMLInputElement).value).toBe('')
    expect(community.attributes('placeholder')).toBe('Saved — leave blank to keep')
    expect(w.get('button[type=submit]').attributes('disabled')).toBeUndefined()

    await w.get('form').trigger('submit')
    await flushPromises()
    expect(api.updateIntegration).toHaveBeenLastCalledWith(7, {
      name: 'core switch',
      config: { host: '192.168.1.2', community: '********', port: 161 },
    })

    await community.setValue('n3w-community')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(api.updateIntegration).toHaveBeenLastCalledWith(7, {
      name: 'core switch',
      config: { host: '192.168.1.2', community: 'n3w-community', port: 161 },
    })
  })

  it('lets the user reveal a secret to check it', async () => {
    const w = mountForm()
    const community = w.get('input[name=community]')
    expect(community.attributes('type')).toBe('password')
    expect((community.element as HTMLInputElement).value).toBe('public')

    await w.get('button[aria-label=Show]').trigger('click')
    expect(w.get('input[name=community]').attributes('type')).toBe('text')
    await w.get('button[aria-label=Hide]').trigger('click')
    expect(w.get('input[name=community]').attributes('type')).toBe('password')
  })
})
