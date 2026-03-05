import { describe, expect, it, vi } from 'vitest'

import { initializeKeycloakRuntime } from './keycloak-runtime'
import type { KeycloakClientLike } from './keycloak-runtime'

function makeClient(overrides?: Partial<KeycloakClientLike>): KeycloakClientLike {
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
