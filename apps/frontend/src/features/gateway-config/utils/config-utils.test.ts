import { describe, expect, it } from 'vitest'
import {
  detectValueType,
  filterConfigItems,
  flattenConfigSections,
  formatConfigValue,
  getSubsystemCounts,
  toEnvKey,
  toEnvText,
  toJsonText,
} from './config-utils'

describe('config-utils', () => {
  it('detects value types correctly', () => {
    expect(detectValueType('')).toBe('empty')
    expect(detectValueType(null)).toBe('empty')
    expect(detectValueType(undefined)).toBe('empty')
    expect(detectValueType('****')).toBe('secret')
    expect(detectValueType(true)).toBe('boolean')
    expect(detectValueType(false)).toBe('boolean')
    expect(detectValueType(8080)).toBe('number')
    expect(detectValueType(0)).toBe('number')
    expect(detectValueType('hello')).toBe('string')
    expect(detectValueType([])).toBe('other')
  })

  it('formats config values for display', () => {
    expect(formatConfigValue(null)).toBe('-')
    expect(formatConfigValue(undefined)).toBe('-')
    expect(formatConfigValue(true)).toBe('true')
    expect(formatConfigValue(false)).toBe('false')
    expect(formatConfigValue(5060)).toBe('5060')
    expect(formatConfigValue('localhost')).toBe('localhost')
  })

  it('flattens sections recursively and sorts alphabetically', () => {
    const sections = {
      turn: {
        server: 'turn.example.com',
        password: '****',
      },
      auth: {
        enable: true,
        user: {
          jwksUrl: 'https://auth.example.com/jwks',
        },
      },
      sip: {
        port: 5060,
      },
    }

    const items = flattenConfigSections(sections)
    expect(items.map((i) => i.fullKey)).toEqual([
      'auth.enable',
      'auth.user.jwksUrl',
      'sip.port',
      'turn.password',
      'turn.server',
    ])

    expect(items[0]).toEqual({
      id: 'auth.enable',
      subsystem: 'auth',
      key: 'enable',
      fullKey: 'auth.enable',
      value: true,
      type: 'boolean',
    })

    expect(items[1]).toEqual({
      id: 'auth.user.jwksUrl',
      subsystem: 'auth',
      key: 'user.jwksUrl',
      fullKey: 'auth.user.jwksUrl',
      value: 'https://auth.example.com/jwks',
      type: 'string',
    })
  })

  it('handles null and empty sections gracefully', () => {
    expect(flattenConfigSections(null)).toEqual([])
    expect(flattenConfigSections(undefined)).toEqual([])
    expect(flattenConfigSections({})).toEqual([])
  })

  it('filters items by subsystem and search text', () => {
    const items = [
      {
        id: 'sip.port',
        subsystem: 'sip',
        key: 'port',
        fullKey: 'sip.port',
        value: 5060,
        type: 'number' as const,
      },
      {
        id: 'api.port',
        subsystem: 'api',
        key: 'port',
        fullKey: 'api.port',
        value: 8080,
        type: 'number' as const,
      },
      {
        id: 'auth.enable',
        subsystem: 'auth',
        key: 'enable',
        fullKey: 'auth.enable',
        value: true,
        type: 'boolean' as const,
      },
      {
        id: 'turn.password',
        subsystem: 'turn',
        key: 'password',
        fullKey: 'turn.password',
        value: '****',
        type: 'secret' as const,
      },
    ]

    expect(filterConfigItems(items, '', 'all')).toHaveLength(4)
    expect(filterConfigItems(items, '', 'sip')).toHaveLength(1)
    expect(filterConfigItems(items, 'port', 'all')).toHaveLength(2)
    expect(filterConfigItems(items, '8080', 'all')).toHaveLength(1)
    expect(filterConfigItems(items, 'secret', 'all')).toHaveLength(1)
    expect(filterConfigItems(items, 'boolean', 'all')).toHaveLength(1)
    expect(filterConfigItems(items, 'nonexistent', 'all')).toHaveLength(0)
  })

  it('converts keys to env style', () => {
    expect(toEnvKey('sip.port')).toBe('SIP_PORT')
    expect(toEnvKey('sip.switchVideoBlackoutMs')).toBe(
      'SIP_SWITCH_VIDEO_BLACKOUT_MS',
    )
    expect(toEnvKey('auth.user.jwksUrl')).toBe('AUTH_USER_JWKS_URL')
  })

  it('formats env and json export strings', () => {
    const items = [
      {
        id: 'sip.port',
        subsystem: 'sip',
        key: 'port',
        fullKey: 'sip.port',
        value: 5060,
        type: 'number' as const,
      },
    ]

    expect(toEnvText(items)).toBe('SIP_PORT=5060')
    expect(toJsonText(items)).toBe('{\n  "sip.port": 5060\n}')
  })

  it('counts items per subsystem', () => {
    const items = [
      {
        id: 'sip.port',
        subsystem: 'sip',
        key: 'port',
        fullKey: 'sip.port',
        value: 5060,
        type: 'number' as const,
      },
      {
        id: 'sip.domain',
        subsystem: 'sip',
        key: 'domain',
        fullKey: 'sip.domain',
        value: 'example.com',
        type: 'string' as const,
      },
      {
        id: 'auth.enable',
        subsystem: 'auth',
        key: 'enable',
        fullKey: 'auth.enable',
        value: true,
        type: 'boolean' as const,
      },
    ]

    expect(getSubsystemCounts(items)).toEqual({
      sip: 2,
      auth: 1,
    })
  })
})
