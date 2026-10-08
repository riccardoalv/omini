import { describe, expect, it } from 'vitest'

import { createAppI18n } from '@/i18n'
import { alertText, attentionCount, sortAlerts, worstByNode } from '@/lib/alerts'
import type { Alert } from '@/lib/types'

const i18n = createAppI18n('en')
const t = i18n.global.t as unknown as (k: string, p?: Record<string, unknown>) => string

const alert = (over: Partial<Alert>): Alert => ({
  id: 1,
  key: 'k',
  rule: 'high_cpu',
  severity: 'warning',
  params: {},
  opened_at: '2026-10-07T10:00:00Z',
  updated_at: '2026-10-07T10:00:00Z',
  dismissed: false,
  ...over,
})

describe('alerts', () => {
  it('words an update by what the device said', () => {
    const update = (params: Record<string, unknown>) =>
      alertText(alert({ rule: 'update_pending', params: { node: 'ap', ...params } }), t, 'en')
        .detail
    expect(update({ latest: '1.1.6', updates: 2 })).toBe('1.1.6 is available (2 updates).')
    expect(update({ latest: '1.1.6' })).toBe('1.1.6 is available.')
    expect(update({ latest: '1.1.6', reboot: true })).toBe(
      '1.1.6 is available; installing needs a reboot.',
    )
    expect(update({ updates: 7 })).toBe('7 updates available.')
  })

  it('describes each rule with its values', () => {
    expect(alertText(alert({ params: { node: 'OPNsense', pct: 97 } }), t, 'en')).toEqual({
      title: 'High CPU on OPNsense',
      detail: 'CPU at 97%.',
    })
    const slow = alertText(
      alert({
        rule: 'slow_uplink',
        params: { node: 'AP', from: 'Switch', port: 'Port 3', speed_mbps: 100 },
      }),
      t,
      'en',
    )
    expect(slow.detail).toBe('The link from Switch (Port 3) runs at 100M: below 1 Gbps.')
    const offline = alertText(
      alert({ rule: 'device_offline', params: { node: 'sw', error: 'timeout' } }),
      t,
      'en',
    )
    expect(offline.detail).toContain('timeout')
    const update = alertText(
      alert({
        rule: 'update_pending',
        params: { node: 'fw', latest: '25.7', updates: 3, reboot: true },
      }),
      t,
      'en',
    )
    expect(update.detail).toContain('reboot')
  })

  it('has a text for every rule in both languages', () => {
    const rules = [
      'device_offline',
      'integration_failed',
      'wan_down',
      'wan_degraded',
      'duplicate_ip',
      'update_pending',
      'disk_full',
      'hot_cpu',
      'high_cpu',
      'high_memory',
      'slow_uplink',
      'interface_errors',
      'weak_wifi',
      'saturated_link',
      'unmanaged_switch',
      'unknown_neighbor',
      'new_device',
    ]
    for (const locale of ['en', 'pt-BR']) {
      const { global } = createAppI18n(locale)
      for (const r of rules) {
        expect(global.te(`insights.rules.${r}.title`), `${locale} ${r}`).toBe(true)
        expect(global.te(`insights.rules.${r}.detail`), `${locale} ${r}`).toBe(true)
      }
    }
  })

  it('sorts by severity, then newest', () => {
    const list = sortAlerts([
      alert({ id: 1, severity: 'info' }),
      alert({ id: 2, severity: 'critical', opened_at: '2026-10-01T00:00:00Z' }),
      alert({ id: 3, severity: 'critical', opened_at: '2026-10-05T00:00:00Z' }),
    ])
    expect(list.map((a) => a.id)).toEqual([3, 2, 1])
  })

  it('marks each node with its worst open alert and counts what needs attention', () => {
    const list = [
      alert({ id: 1, node_id: 'a', severity: 'info' }),
      alert({ id: 2, node_id: 'a', severity: 'warning' }),
      alert({ id: 3, node_id: 'b', severity: 'critical', dismissed: true }),
      alert({ id: 4, severity: 'critical' }),
    ]
    expect([...worstByNode(list)]).toEqual([['a', 'warning']])
    expect(attentionCount(list)).toBe(2)
  })
})
