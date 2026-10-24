import { reactive } from 'vue'

import { api } from './api'
import { formatAgo, formatSpeed } from './format'
import { formatRate } from './traffic'
import type { Alert, Severity } from './types'

type T = (key: string, params?: Record<string, unknown>) => string

export const SEVERITIES: Severity[] = ['critical', 'warning', 'info']

const rank: Record<Severity, number> = { critical: 0, warning: 1, info: 2 }

/** Sorts alerts: most severe first, then newest. */
export function sortAlerts(list: Alert[]): Alert[] {
  return [...list].sort(
    (a, b) => rank[a.severity] - rank[b.severity] || b.opened_at.localeCompare(a.opened_at),
  )
}

/** Values of an alert ready to be shown: speeds, rates and dates formatted. */
function params(a: Alert, locale: string): Record<string, unknown> {
  const p: Record<string, unknown> = { ...a.params }
  if (typeof p.speed_mbps === 'number') p.speed = formatSpeed(p.speed_mbps)
  for (const k of ['rx_bps', 'tx_bps'] as const)
    if (typeof p[k] === 'number') p[k] = formatRate(p[k] as number)
  if (typeof p.first_seen === 'string') p.ago = formatAgo(p.first_seen, locale)
  if (typeof p.at === 'string') p.since = formatAgo(p.at, locale)
  for (const k of ['loss_pct', 'rtt_ms', 'updates', 'latest'] as const) p[k] ??= '?'
  return p
}

/** Title and detail of an alert, in the user's language. */
export function alertText(a: Alert, t: T, locale: string): { title: string; detail: string } {
  const base = `insights.rules.${a.rule}`
  const p = params(a, locale)
  let detail = `${base}.detail`
  if (a.rule === 'device_offline' && p.error) detail = `${base}.detailError`
  // DHCP gave the device another address: it answers there now.
  if ((a.rule === 'device_offline' || a.rule === 'integration_failed') && p.moved_to)
    detail = `${base}.detailMoved`
  if (a.rule === 'update_pending' && p.reboot) detail = `${base}.detailReboot`
  // The device said which version, not how many updates.
  if (a.rule === 'update_pending' && a.params.latest && a.params.updates == null)
    detail = p.reboot ? `${base}.detailVersionReboot` : `${base}.detailVersion`
  // The device did not say which version: only how many updates.
  if (a.rule === 'update_pending' && !a.params.latest) detail = `${base}.detailCount`
  if (a.rule === 'wifi_channel_conflict' && p.auto_aps) detail = `${base}.detailAuto`
  if (a.rule === 'discovery_limited' && typeof p.limits === 'string')
    p.what = p.limits
      .split(',')
      .map((k) => t(`discovery.limits.${k}`))
      .join(', ')
  return { title: t(`${base}.title`, p), detail: t(detail, p) }
}

/** What to do about an alert ('' when there is no tip). */
export function alertTip(a: Alert, t: T): string {
  if (moved(a)) return t('insights.tipMoved', a.params)
  const key = `insights.tips.${a.rule}`
  const text = t(key)
  return text === key ? '' : text
}

/** Where the device of an offline alert answers now, when DHCP gave it another address. */
export function moved(a: Alert): { integration: number; from: string; to: string } | null {
  const p = a.params
  if (a.resolved_at || (a.rule !== 'device_offline' && a.rule !== 'integration_failed')) return null
  if (typeof p.moved_to !== 'string' || typeof p.integration_id !== 'number') return null
  return { integration: p.integration_id, from: String(p.moved_from ?? ''), to: p.moved_to }
}

/** The most severe open alert of each node (dismissed ones aside). */
export function worstByNode(list: Alert[] | undefined): Map<string, Severity> {
  const out = new Map<string, Severity>()
  for (const a of list ?? []) {
    if (!a.node_id || a.dismissed || a.resolved_at) continue
    const cur = out.get(a.node_id)
    if (!cur || rank[a.severity] < rank[cur]) out.set(a.node_id, a.severity)
  }
  return out
}

/** Open alerts shared by the screens (the menu badge, the map, the panel). */
export const alertsState = reactive<{ list: Alert[]; loaded: boolean }>({ list: [], loaded: false })

/** Alerts that ask for attention: open, not dismissed, critical or warning. */
export function attentionCount(list: Alert[]): number {
  return list.filter((a) => !a.dismissed && !a.resolved_at && a.severity !== 'info').length
}

export async function refreshAlerts(): Promise<void> {
  try {
    alertsState.list = await api.alerts()
    alertsState.loaded = true
  } catch {
    // shown again on the next refresh
  }
}

/** An alert with a Resolve button: a Wi-Fi channel conflict with radios Omini can move. */
export function resolvable(a: Alert): boolean {
  return a.rule === 'wifi_channel_conflict' && !a.resolved_at && Number(a.params.movable ?? 0) > 0
}

/** Whether an alert is about a node: its own, or one of the nodes it lists (the network's access points). */
export function concerns(a: Alert, id: string | undefined): boolean {
  if (!id) return false
  const nodes = a.params.nodes
  return a.node_id === id || (Array.isArray(nodes) && nodes.includes(id))
}
