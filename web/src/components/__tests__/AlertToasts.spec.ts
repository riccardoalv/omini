import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { alertsState } from '@/lib/alerts'
import type { Alert } from '@/lib/types'

import AlertToasts from '../AlertToasts.vue'
import { plugins } from './helpers'

const alert = (
  id: number,
  severity: Alert['severity'],
  rule = 'high_cpu',
  extra: Partial<Alert> = {},
): Alert => ({
  id,
  key: `k${id}`,
  rule,
  severity,
  node_id: `dev:${id}`,
  params: { node: `dev ${id}`, pct: 97 },
  opened_at: `2026-10-08T10:0${id}:00Z`,
  updated_at: '2026-10-08T10:00:00Z',
  dismissed: false,
  ...extra,
})

const router = createRouter({
  history: createMemoryHistory(),
  routes: ['/', '/alerts', '/devices'].map((path) => ({
    path,
    component: { template: '<div />' },
  })),
})

describe('AlertToasts', () => {
  beforeEach(async () => {
    localStorage.clear()
    await router.push('/devices')
  })

  it('pops up open critical and warning alerts with a tip, most severe first', async () => {
    alertsState.list = [
      alert(1, 'warning'),
      alert(2, 'info', 'new_device'),
      alert(3, 'critical', 'device_offline'),
      alert(4, 'warning', 'disk_full', { dismissed: true }),
      alert(5, 'warning', 'disk_full', { resolved_at: '2026-10-08T11:00:00Z' }),
    ]
    const w = mount(AlertToasts, { global: { plugins: [...plugins(), router] } })
    const toasts = w.findAll('[data-test=toast]')
    expect(toasts).toHaveLength(2)
    expect(toasts[0]!.classes()).toContain('critical')
    expect(toasts[1]!.text()).toContain('High CPU on dev 1')
    expect(toasts[1]!.text()).toContain('See which process uses it')
  })

  it('closes a popup for good and sends the rest to the Alerts screen', async () => {
    alertsState.list = [1, 2, 3, 4, 5].map((i) => alert(i, 'warning'))
    const w = mount(AlertToasts, { global: { plugins: [...plugins(), router] } })
    expect(w.findAll('[data-test=toast]')).toHaveLength(3)
    expect(w.get('[data-test=toast-more]').text()).toBe('2 more alerts')
    await w.get('[data-test=toast-close]').trigger('click')
    expect(w.get('[data-test=toast-more]').text()).toBe('1 more alert')
    // Remembered: a new page load does not show it again.
    const again = mount(AlertToasts, { global: { plugins: [...plugins(), router] } })
    expect(again.findAll('[data-test=toast]').map((t) => t.text())).not.toContain(w.text())
    expect(JSON.parse(localStorage.getItem('omini.closedAlerts')!)).toHaveLength(1)
  })

  it('shows nothing on the Alerts screen itself', async () => {
    alertsState.list = [alert(1, 'critical')]
    await router.push('/alerts')
    const w = mount(AlertToasts, { global: { plugins: [...plugins(), router] } })
    expect(w.find('[data-test=toast]').exists()).toBe(false)
  })
})
