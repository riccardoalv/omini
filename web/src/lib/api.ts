import type {
  AuthStatus,
  Config,
  DiscoveredHost,
  Integration,
  IntegrationType,
  InventoryEntry,
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

export interface IntegrationInput {
  name?: string
  type?: string
  config?: Config
  enabled?: boolean
}

export const api = {
  authStatus: () => request<AuthStatus>('GET', '/api/auth/status'),
  setup: (username: string, password: string) =>
    request<AuthStatus>('POST', '/api/auth/setup', { username, password }),
  login: (username: string, password: string) =>
    request<AuthStatus>('POST', '/api/auth/login', { username, password }),
  logout: () => request<AuthStatus>('POST', '/api/auth/logout'),

  health: () => request<{ status: string; version: string }>('GET', '/api/health'),

  integrationTypes: () => request<IntegrationType[]>('GET', '/api/integration-types'),
  integrations: () => request<Integration[]>('GET', '/api/integrations'),
  createIntegration: (input: IntegrationInput) =>
    request<Integration>('POST', '/api/integrations', input),
  updateIntegration: (id: number, input: IntegrationInput) =>
    request<Integration>('PUT', `/api/integrations/${id}`, input),
  deleteIntegration: (id: number) => request<void>('DELETE', `/api/integrations/${id}`),
  testIntegration: (input: { id?: number; type?: string; config: Config }) =>
    request<TestResult>('POST', '/api/integrations/test', input),

  topology: () => request<TopologyResponse>('GET', '/api/topology'),
  refresh: () => request<{ status: string }>('POST', '/api/refresh'),
  saveLayout: (positions: Record<string, Point>) =>
    request<void>('PUT', '/api/layout', { positions }),
  resetLayout: () => request<void>('DELETE', '/api/layout'),

  inventory: () => request<InventoryEntry[]>('GET', '/api/inventory'),
  updateInventory: (
    id: string,
    input: { alias?: string; pinned?: boolean; device_type?: string; icon?: string },
  ) => request<InventoryEntry>('PATCH', `/api/inventory/${encodeURIComponent(id)}`, input),
  deleteInventory: (ids: string[]) => request<void>('POST', '/api/inventory/delete', { ids }),

  webServices: (nodeId: string) =>
    request<WebService[]>('GET', `/api/nodes/${encodeURIComponent(nodeId)}/web`),

  scan: (cidr: string, community: string) =>
    request<DiscoveredHost[]>('POST', '/api/discovery/scan', { cidr, community }),
}
