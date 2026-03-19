import { describe, expect, it, vi } from 'vitest'

import { extractAuthUser, initializeKeycloakRuntime } from './keycloak-runtime'
import type { KeycloakClientLike } from './keycloak-runtime'

function makeClient(
  overrides?: Partial<KeycloakClientLike>,
): KeycloakClientLike {
  return {
    authenticated: true,
    token: 'token-1',
    init: vi.fn().mockResolvedValue(true),
    login: vi.fn().mockResolvedValue(undefined),
    logout: vi.fn().mockResolvedValue(undefined),
    updateToken: vi.fn().mockResolvedValue(true),
    ...overrides,
  }
}

describe('initializeKeycloakRuntime', () => {
  it('emits ready/authenticated state on successful init', async () => {
    const client = makeClient()
    const onStateChange = vi.fn()

    const stop = await initializeKeycloakRuntime({
      client,
      onStateChange,
      onError: vi.fn(),
      refreshIntervalMs: 60_000,
    })

    expect(client.init).toHaveBeenCalledWith({
      onLoad: 'login-required',
      pkceMethod: 'S256',
      checkLoginIframe: false,
    })
    expect(onStateChange).toHaveBeenCalledWith({
      ready: true,
      authenticated: true,
      token: 'token-1',
      user: null,
    })

    stop()
  })

  it('triggers login when init reports unauthenticated', async () => {
    const client = makeClient({
      authenticated: false,
      init: vi.fn().mockResolvedValue(false),
    })

    const stop = await initializeKeycloakRuntime({
      client,
      onStateChange: vi.fn(),
      onError: vi.fn(),
      refreshIntervalMs: 60_000,
    })

    expect(client.login).toHaveBeenCalled()
    stop()
  })

  it('refreshes token when token expiry handler runs', async () => {
    const client = makeClient()
    const onStateChange = vi.fn()

    const stop = await initializeKeycloakRuntime({
      client,
      onStateChange,
      onError: vi.fn(),
      refreshIntervalMs: 60_000,
    })

    client.onTokenExpired?.()
    await Promise.resolve()

    expect(client.updateToken).toHaveBeenCalledWith(30)
    expect(onStateChange).toHaveBeenCalledTimes(2)

    stop()
  })
})

describe('extractAuthUser', () => {
  it('uses name and preferred_username when available', () => {
    expect(
      extractAuthUser({
        name: 'Alice Doe',
        preferred_username: 'alice',
        email: 'alice@example.com',
      }),
    ).toEqual({
      displayName: 'Alice Doe',
      username: 'alice',
      email: 'alice@example.com',
    })
  })

  it('falls back to unknown user and sub', () => {
    expect(
      extractAuthUser({
        sub: 'user-123',
      }),
    ).toEqual({
      displayName: 'Unknown user',
      username: 'user-123',
    })
  })

  it('returns null when token claims are missing', () => {
    expect(extractAuthUser(undefined)).toBeNull()
  })
})
