import { afterEach, describe, expect, it, vi } from 'vitest'

import { getKeycloakRuntimeConfig, isAutoRecordEnabled } from './runtime-env'

describe('runtime-env', () => {
  afterEach(() => {
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
