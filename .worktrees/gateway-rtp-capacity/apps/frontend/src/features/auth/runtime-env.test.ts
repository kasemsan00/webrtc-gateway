import { afterEach, describe, expect, it, vi } from 'vitest'

import { getKeycloakRuntimeConfig, isAutoRecordEnabled } from './runtime-env'

describe('runtime-env', () => {
  afterEach(() => {
    delete (
      window as Window & {
        __APP_RUNTIME_ENV__?: Record<string, string>
      }
    ).__APP_RUNTIME_ENV__
    vi.unstubAllEnvs()
  })

  it('reads keycloak runtime config from VITE env keys', () => {
    vi.stubEnv('VITE_KEYCLOAK_URL', 'https://accounts.example.com/auth')
    vi.stubEnv('VITE_KEYCLOAK_REALM', 'Example-Realm')
    vi.stubEnv('VITE_KEYCLOAK_CLIENT', 'example-client')

    expect(getKeycloakRuntimeConfig()).toEqual({
      url: 'https://accounts.example.com/auth',
      realm: 'Example-Realm',
      clientId: 'example-client',
    })
  })

  it('throws when required keycloak values are missing', () => {
    vi.stubEnv('VITE_KEYCLOAK_URL', '')
    vi.stubEnv('VITE_KEYCLOAK_REALM', '')
    vi.stubEnv('VITE_KEYCLOAK_CLIENT', '')

    expect(() => getKeycloakRuntimeConfig()).toThrow(
      'VITE_KEYCLOAK_URL is not configured',
    )
  })

  it('prefers runtime env injected on window over build-time env', () => {
    vi.stubEnv('VITE_KEYCLOAK_URL', 'https://build.example.com/auth')
    vi.stubEnv('VITE_KEYCLOAK_REALM', 'build-realm')
    vi.stubEnv('VITE_KEYCLOAK_CLIENT', 'build-client')
    ;(
      window as Window & {
        __APP_RUNTIME_ENV__?: Record<string, string>
      }
    ).__APP_RUNTIME_ENV__ = {
      VITE_KEYCLOAK_URL: 'https://runtime.example.com/auth',
      VITE_KEYCLOAK_REALM: 'runtime-realm',
      VITE_KEYCLOAK_CLIENT: 'runtime-client',
    }

    expect(getKeycloakRuntimeConfig()).toEqual({
      url: 'https://runtime.example.com/auth',
      realm: 'runtime-realm',
      clientId: 'runtime-client',
    })
  })

  it('parses VITE_CONFIG_AUTORECORD as true for truthy values', () => {
    vi.stubEnv('VITE_CONFIG_AUTORECORD', '1')
    expect(isAutoRecordEnabled()).toBe(true)

    vi.stubEnv('VITE_CONFIG_AUTORECORD', 'true')
    expect(isAutoRecordEnabled()).toBe(true)

    vi.stubEnv('VITE_CONFIG_AUTORECORD', ' YES ')
    expect(isAutoRecordEnabled()).toBe(true)
  })

  it('returns false when VITE_CONFIG_AUTORECORD is not set', () => {
    vi.stubEnv('VITE_CONFIG_AUTORECORD', '')
    expect(isAutoRecordEnabled()).toBe(false)
  })
})
