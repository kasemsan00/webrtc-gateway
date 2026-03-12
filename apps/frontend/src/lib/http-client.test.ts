import { afterEach, describe, expect, it, vi } from 'vitest'

import { buildAuthHeaders, fetchJson } from './http-client'
import { clearAccessToken, setAccessToken } from '@/features/auth/token-store'

describe('buildAuthHeaders', () => {
  afterEach(() => {
    clearAccessToken()
  })

  it('adds bearer token when available', () => {
    setAccessToken('token-abc')
    const headers = buildAuthHeaders()

    expect(headers.get('Authorization')).toBe('Bearer token-abc')
  })

  it('preserves explicit authorization header', () => {
    setAccessToken('token-abc')
    const headers = buildAuthHeaders({
      Authorization: 'Bearer explicit-token',
    })

    expect(headers.get('Authorization')).toBe('Bearer explicit-token')
  })
})

describe('fetchJson', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    clearAccessToken()
  })

  it('passes bearer token in request headers', async () => {
    setAccessToken('token-abc')

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    await fetchJson<{ ok: boolean }>('https://example.test/api')

    const options = fetchSpy.mock.calls[0]?.[1]
    const headers = new Headers(options?.headers)
    expect(headers.get('Authorization')).toBe('Bearer token-abc')
  })
})
