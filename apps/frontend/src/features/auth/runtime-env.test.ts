import { afterEach, describe, expect, it, vi } from 'vitest'

import { isAutoRecordEnabled } from './runtime-env'

describe('runtime-env', () => {
  afterEach(() => {
    delete (
      window as Window & {
        __APP_RUNTIME_ENV__?: Record<string, string>
      }
    ).__APP_RUNTIME_ENV__
    vi.unstubAllEnvs()
  })

  it('does not require Keycloak env values', () => {
    vi.stubEnv('VITE_KEYCLOAK_URL', '')
    vi.stubEnv('VITE_KEYCLOAK_REALM', '')
    vi.stubEnv('VITE_KEYCLOAK_CLIENT', '')
    vi.stubEnv('VITE_CONFIG_AUTORECORD', '')

    expect(() => isAutoRecordEnabled()).not.toThrow()
    expect(isAutoRecordEnabled()).toBe(false)
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
