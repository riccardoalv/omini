import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError, setUnauthorizedHandler } from '../api'

function mockFetch(status: number, body?: unknown) {
  const fn = vi.fn<typeof fetch>().mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
  vi.stubGlobal('fetch', fn)
  return fn
}

/** The URL and options of the i-th fetch call. */
function call(fn: ReturnType<typeof mockFetch>, i = 0) {
  const [url, init] = fn.mock.calls[i]!
  return {
    url: String(url),
    method: init?.method,
    headers: (init?.headers ?? {}) as Record<string, string>,
    body: init?.body ? JSON.parse(String(init.body)) : undefined,
  }
}

describe('api client', () => {
  beforeEach(() => setUnauthorizedHandler(() => {}))
  afterEach(() => vi.unstubAllGlobals())

  it('tells the menu whether the flows integration is on', async () => {
    const { features } = await import('../features')
    mockFetch(200, [{ id: 1, type: 'flows', enabled: true }])
    await api.integrations()
    expect(features.flows).toBe(true)
    mockFetch(200, [{ id: 1, type: 'flows', enabled: false }])
    await api.integrations()
    expect(features.flows).toBe(false)
  })

  it('sends the CSRF header and JSON body on state-changing requests', async () => {
    const fetch = mockFetch(201, { id: 1 })
    await api.createIntegration({ type: 'demo', config: {} })

    const c = call(fetch)
    expect(c.url).toBe('/api/integrations')
    expect(c.method).toBe('POST')
    expect(c.headers['X-Omini-Request']).toBe('1')
    expect(c.headers['Content-Type']).toBe('application/json')
    expect(c.body).toEqual({ type: 'demo', config: {} })
  })

  it('does not send the CSRF header on GET', async () => {
    const fetch = mockFetch(200, [])
    await api.integrations()
    expect(call(fetch).headers['X-Omini-Request']).toBeUndefined()
  })

  it('throws ApiError with the server message', async () => {
    mockFetch(400, { error: 'missing required fields: host' })
    await expect(api.createIntegration({})).rejects.toEqual(
      new ApiError(400, 'missing required fields: host'),
    )
  })

  it('calls the unauthorized handler on 401 outside auth endpoints', async () => {
    const handler = vi.fn<() => void>()
    setUnauthorizedHandler(handler)

    mockFetch(401, { error: 'login required' })
    await expect(api.topology()).rejects.toBeInstanceOf(ApiError)
    expect(handler).toHaveBeenCalledOnce()

    mockFetch(401, { error: 'invalid username or password' })
    await expect(api.login('a', 'b')).rejects.toBeInstanceOf(ApiError)
    expect(handler).toHaveBeenCalledOnce()
  })

  it('encodes inventory ids (they contain colons)', async () => {
    const fetch = mockFetch(200, {})
    await api.updateInventory('mac:aa:bb:cc:00:00:01', { alias: 'NAS' })
    expect(call(fetch).url).toBe('/api/inventory/mac%3Aaa%3Abb%3Acc%3A00%3A00%3A01')
  })

  it('returns undefined for 204 responses', async () => {
    mockFetch(204)
    await expect(api.deleteIntegration(3)).resolves.toBeUndefined()
  })
})
