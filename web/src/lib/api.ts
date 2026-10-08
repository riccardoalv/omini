import { noteIntegrations } from './features'
import type {
  Alert,
  FlowsResponse,
  Notifier,
  NotifierType,
  PresenceEvent,
  TrafficPoint,
  AuthStatus,
  Config,
  Capabilities,
  CollectionInfo,
  CollectionStatus,
  Integration,
  IntegrationType,
  InventoryEntry,
  MapArea,
  CatalogEntry,
  PluginIndexStatus,
  PluginInfo,
  Point,
  TestResult,
  TopologyResponse,
  WebService,
} from './types'

/** Error returned by the API, with the server's message. */
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

let onUnauthorized: (() => void) | undefined

/** Registers a callback for 401 responses (session expired → back to login). */
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (method !== 'GET') {
    headers['X-Omini-Request'] = '1' // required by the server for state-changing requests (CSRF)
  }
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }
  const res = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.status === 401 && !path.startsWith('/api/auth/')) {
    onUnauthorized?.()
  }
  if (!res.ok) {
    let message = res.statusText
    try {
      const data = await res.json()
      if (data && typeof data.error === 'string') message = data.error
    } catch {
      // not JSON
    }
    throw new ApiError(res.status, message)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

/** Integrations are named after their type; the name is not editable. */
export interface IntegrationInput {
  type?: string
  config?: Config
  enabled?: boolean
  interval_s?: number
}

/** After an integration changes, the menu learns which screens to show. */
function afterIntegrationChange<T>(result: T): T {
  void api.integrations().catch(() => undefined)
  return result
}

export const api = {
  authStatus: () => request<AuthStatus>('GET', '/api/auth/status'),
  setup: (username: string, password: string, locale?: string) =>
    request<AuthStatus>('POST', '/api/auth/setup', { username, password, locale }),
  login: (username: string, password: string) =>
    request<AuthStatus>('POST', '/api/auth/login', { username, password }),
  logout: () => request<AuthStatus>('POST', '/api/auth/logout'),
  updateMe: (input: { locale?: string }) => request<AuthStatus>('PATCH', '/api/me', input),

  health: () => request<{ status: string; version: string }>('GET', '/api/health'),

  integrationTypes: () => request<IntegrationType[]>('GET', '/api/integration-types'),
  capabilities: () =>
    request<{ capabilities: Capabilities; limited: string[] }>('GET', '/api/capabilities'),
  collection: () => request<CollectionInfo>('GET', '/api/collection'),
  setCollection: (intervalS: number) =>
    request<CollectionInfo>('PUT', '/api/collection', { interval_s: intervalS }),
  integrations: () =>
    request<Integration[]>('GET', '/api/integrations').then((list) => {
      noteIntegrations(list)
      return list
    }),
  createIntegration: (input: IntegrationInput) =>
    request<Integration>('POST', '/api/integrations', input).then(afterIntegrationChange),
  updateIntegration: (id: number, input: IntegrationInput) =>
    request<Integration>('PUT', `/api/integrations/${id}`, input).then(afterIntegrationChange),
  deleteIntegration: (id: number) =>
    request<void>('DELETE', `/api/integrations/${id}`).then(afterIntegrationChange),
  notifierTypes: () => request<NotifierType[]>('GET', '/api/notifier-types'),
  notifiers: () => request<Notifier[]>('GET', '/api/notifiers'),
  createNotifier: (input: Partial<Omit<Notifier, 'id'>>) =>
    request<Notifier>('POST', '/api/notifiers', input),
  updateNotifier: (id: number, input: Partial<Omit<Notifier, 'id' | 'type'>>) =>
    request<Notifier>('PUT', `/api/notifiers/${id}`, input),
  deleteNotifier: (id: number) => request<void>('DELETE', `/api/notifiers/${id}`),
  testNotifier: (input: { id?: number; type?: string; config?: Config }) =>
    request<{ ok: boolean; error?: string }>('POST', '/api/notifiers/test', input),
  testIntegration: (input: { id?: number; type?: string; config: Config }) =>
    request<TestResult>('POST', '/api/integrations/test', input),

  topology: () => request<TopologyResponse>('GET', '/api/topology'),
  refresh: () => request<{ status: string }>('POST', '/api/refresh'),
  saveLayout: (positions: Record<string, Point>) =>
    request<void>('PUT', '/api/layout', { positions }),
  resetLayout: () => request<void>('DELETE', '/api/layout'),
  createArea: (area: Omit<MapArea, 'id'>) => request<MapArea>('POST', '/api/areas', area),
  updateArea: (id: number, patch: Partial<Omit<MapArea, 'id' | 'direction'>>) =>
    request<MapArea>('PATCH', `/api/areas/${id}`, patch),
  deleteArea: (id: number) => request<void>('DELETE', `/api/areas/${id}`),

  inventory: () => request<InventoryEntry[]>('GET', '/api/inventory'),
  updateInventory: (
    id: string,
    input: {
      alias?: string
      pinned?: boolean
      hidden?: boolean
      device_type?: string
      icon?: string
    },
  ) => request<InventoryEntry>('PATCH', `/api/inventory/${encodeURIComponent(id)}`, input),
  deleteInventory: (ids: string[]) => request<void>('POST', '/api/inventory/delete', { ids }),

  setPortLabel: (nodeId: string, port: string, label: string) =>
    request<void>(
      'PUT',
      `/api/nodes/${encodeURIComponent(nodeId)}/ports/${encodeURIComponent(port)}`,
      { label },
    ),
  /** Scans one device with the nmap integration (takes up to a minute). */
  scanNode: (nodeId: string) =>
    request<{ ip: string; open_ports?: number[]; os?: string; banners?: string[] }>(
      'POST',
      `/api/nodes/${encodeURIComponent(nodeId)}/scan`,
    ),
  webServices: (nodeId: string) =>
    request<WebService[]>('GET', `/api/nodes/${encodeURIComponent(nodeId)}/web`),

  plugins: () => request<PluginInfo[]>('GET', '/api/plugins'),
  pluginCatalog: () => request<CatalogEntry[]>('GET', '/api/plugins/catalog'),
  pluginIndex: () => request<PluginIndexStatus>('GET', '/api/plugins/index'),
  refreshPluginIndex: () => request<PluginIndexStatus>('POST', '/api/plugins/index/refresh'),
  installPlugin: (url: string, version?: string) =>
    request<PluginInfo>('POST', '/api/plugins', { url, version }),
  removePlugin: (id: string) => request<void>('DELETE', `/api/plugins/${encodeURIComponent(id)}`),

  alerts: (resolvedHours?: number) =>
    request<Alert[]>(
      'GET',
      `/api/alerts${resolvedHours ? `?resolved_hours=${resolvedHours}` : ''}`,
    ),
  dismissAlert: (id: number, dismissed: boolean) =>
    request<void>('POST', `/api/alerts/${id}/dismiss`, { dismissed }),
  presence: (q: { node?: string; before?: number; limit?: number; first?: boolean } = {}) => {
    const params = new URLSearchParams()
    if (q.node) params.set('node', q.node)
    if (q.before) params.set('before', String(q.before))
    if (q.limit) params.set('limit', String(q.limit))
    if (q.first) params.set('first', '1')
    const qs = params.toString()
    return request<PresenceEvent[]>('GET', `/api/presence${qs ? `?${qs}` : ''}`)
  },
  flows: (q: { minutes?: number; node?: string; limit?: number } = {}) => {
    const params = new URLSearchParams()
    if (q.minutes) params.set('minutes', String(q.minutes))
    if (q.node) params.set('node', q.node)
    if (q.limit) params.set('limit', String(q.limit))
    const qs = params.toString()
    return request<FlowsResponse>('GET', `/api/flows${qs ? `?${qs}` : ''}`)
  },
  history: (node: string, iface: string, hours = 24) =>
    request<TrafficPoint[]>(
      'GET',
      `/api/history?${new URLSearchParams({ node, iface, hours: String(hours) })}`,
    ),
  runIntegration: (id: number) => request<CollectionStatus>('POST', `/api/integrations/${id}/run`),
}
