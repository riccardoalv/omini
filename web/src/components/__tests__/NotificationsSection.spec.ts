import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { Notifier, NotifierType } from '@/lib/types'

import NotificationsSection from '../NotificationsSection.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      notifierTypes: vi.fn<typeof mod.api.notifierTypes>(),
      notifiers: vi.fn<typeof mod.api.notifiers>(),
      createNotifier: vi.fn<typeof mod.api.createNotifier>(),
      updateNotifier: vi.fn<typeof mod.api.updateNotifier>(),
      testNotifier: vi.fn<typeof mod.api.testNotifier>(),
    },
  }
})

const types: NotifierType[] = [
  {
    type: 'telegram',
    name: 'Telegram',
    fields: [
      { key: 'token', type: 'secret', required: true },
      { key: 'chat_id', type: 'string', required: true },
    ],
  },
  {
    type: 'webhook',
    name: 'Webhook',
    fields: [
      { key: 'url', type: 'url', required: true },
      {
        key: 'format',
        type: 'select',
        options: ['json', 'slack', 'discord', 'ntfy'],
        default: 'json',
      },
      { key: 'secret', type: 'secret' },
    ],
  },
]
const saved: Notifier = {
  id: 7,
  type: 'telegram',
  config: { token: '********', chat_id: '42' },
  min_severity: 'warning',
  notify_resolved: true,
  enabled: true,
  last_error: 'telegram: chat not found',
}

describe('NotificationsSection', () => {
  beforeEach(() => {
    vi.mocked(api.notifierTypes).mockResolvedValue(types)
    vi.mocked(api.notifiers).mockResolvedValue([saved])
    vi.mocked(api.createNotifier).mockResolvedValue({ ...saved, id: 8 })
    vi.mocked(api.updateNotifier).mockResolvedValue(saved)
    vi.mocked(api.testNotifier).mockResolvedValue({ ok: true })
  })

  it('lists channels with their last error', async () => {
    const w = mount(NotificationsSection, { global: { plugins: plugins() } })
    await flushPromises()
    expect(w.get('[data-test=channel]').text()).toContain('chat 42')
    expect(w.get('[data-test=channel-error]').text()).toBe('telegram: chat not found')
  })

  it('adds a channel after asking for its required fields, and tests it', async () => {
    const w = mount(NotificationsSection, { global: { plugins: plugins() } })
    await flushPromises()
    await w.get('[data-test=add-telegram]').trigger('click')
    await w.get('[data-test=channel-form]').trigger('submit')
    expect(w.text()).toContain('Fill in: token, chat_id')
    expect(api.createNotifier).not.toHaveBeenCalled()

    await w.get('input[name=token]').setValue('123:abc')
    await w.get('input[name=chat_id]').setValue('99')
    await w.get('[data-test=min-severity]').setValue('critical')
    await w.get('[data-test=test-channel]').trigger('click')
    await flushPromises()
    expect(api.testNotifier).toHaveBeenCalledWith({
      id: undefined,
      type: 'telegram',
      config: { token: '123:abc', chat_id: '99' },
    })
    expect(w.text()).toContain('Test message sent.')

    await w.get('[data-test=channel-form]').trigger('submit')
    await flushPromises()
    expect(api.createNotifier).toHaveBeenCalledWith({
      type: 'telegram',
      config: { token: '123:abc', chat_id: '99' },
      min_severity: 'critical',
      notify_resolved: true,
    })
  })

  it('keeps a saved secret when editing', async () => {
    const w = mount(NotificationsSection, { global: { plugins: plugins() } })
    await flushPromises()
    await w.get('[aria-label=Edit]').trigger('click')
    await w.get('[data-test=channel-form]').trigger('submit')
    await flushPromises()
    expect(api.updateNotifier).toHaveBeenCalledWith(7, {
      config: { token: '********', chat_id: '42' },
      min_severity: 'warning',
      notify_resolved: true,
    })
  })
})

describe('NotificationsSection accordions', () => {
  beforeEach(() => {
    vi.mocked(api.notifierTypes).mockResolvedValue(types)
    vi.mocked(api.notifiers).mockResolvedValue([
      saved,
      {
        ...saved,
        id: 9,
        type: 'webhook',
        config: { url: 'https://discord.com/api/webhooks/1/x', format: 'discord' },
      },
    ])
    vi.mocked(api.createNotifier).mockResolvedValue({ ...saved, id: 10 })
  })

  it('lists every way to be told, with the ones in use open', async () => {
    const w = mount(NotificationsSection, { global: { plugins: plugins() } })
    await flushPromises()
    // E-mail is not offered by this server: not listed.
    expect(w.findAll('[data-test^=kind-]').map((k) => k.attributes('data-test'))).toEqual([
      'kind-telegram',
      'kind-slack',
      'kind-discord',
      'kind-ntfy',
      'kind-webhook',
    ])
    expect(w.get('[data-test=toggle-telegram]').attributes('aria-expanded')).toBe('true')
    expect(w.get('[data-test=toggle-slack]').attributes('aria-expanded')).toBe('false')
    // The Discord webhook is under Discord, not under Webhook.
    expect(w.get('[data-test=kind-discord] [data-test=channel]').text()).toContain('discord.com')
    expect(w.get('[data-test=kind-discord]').text()).toContain('1 of 1 on')
    expect(w.get('[data-test=kind-slack]').text()).toContain('Not set up')
    // Opening one shows how to set it up.
    await w.get('[data-test=toggle-slack]').trigger('click')
    expect(w.get('[data-test=kind-slack]').text()).toContain('Incoming Webhooks')
  })

  it('saves a Discord channel as a webhook in the Discord format, without asking for it', async () => {
    const w = mount(NotificationsSection, { global: { plugins: plugins() } })
    await flushPromises()
    await w.get('[data-test=add-discord]').trigger('click')
    const form = w.get('[data-test=kind-discord] [data-test=channel-form]')
    expect(form.find('[name=format]').exists()).toBe(false)
    expect(form.find('[name=secret]').exists()).toBe(false)
    await form.get('input[name=url]').setValue('https://discord.com/api/webhooks/2/y')
    await form.trigger('submit')
    await flushPromises()
    expect(api.createNotifier).toHaveBeenCalledWith({
      type: 'webhook',
      config: { url: 'https://discord.com/api/webhooks/2/y', format: 'discord' },
      min_severity: 'warning',
      notify_resolved: true,
    })
  })
})
