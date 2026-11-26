import { describe, expect, it } from 'vitest'

import {
  resolveWSClientCapabilities,
  resolveWSClientPresenceLabel,
} from './types'
import type { WSClient } from './types'

describe('resolveWSClientPresenceLabel', () => {
  it('labels agent clients from agentOnly or ephemeral presence', () => {
    expect(resolveWSClientPresenceLabel({ agentOnly: true })).toBe('agent')
    expect(resolveWSClientPresenceLabel({ presenceMode: 'ephemeral' })).toBe(
      'agent',
    )
  })

  it('labels public clients', () => {
    expect(resolveWSClientPresenceLabel({ publicOnly: true })).toBe('public')
    expect(resolveWSClientPresenceLabel({ presenceMode: 'public' })).toBe(
      'public',
    )
  })

  it('defaults sticky/authenticated clients to mobile', () => {
    expect(resolveWSClientPresenceLabel({ presenceMode: 'sticky' })).toBe(
      'mobile',
    )
    expect(resolveWSClientPresenceLabel({})).toBe('mobile')
  })
})

describe('resolveWSClientCapabilities', () => {
  it('returns empty array when optional capability flags are omitted or false', () => {
    const legacyClient: WSClient = {
      clientId: 'c-legacy',
      connectedAt: '2026-11-25T00:00:00Z',
      trunkResolved: true,
    }
    expect(resolveWSClientCapabilities(legacyClient)).toEqual([])
    expect(
      resolveWSClientCapabilities({ multiCall: false, multiVideo: false }),
    ).toEqual([])
  })

  it('includes multi-call and multi-video badges when enabled', () => {
    expect(resolveWSClientCapabilities({ multiVideo: true })).toEqual([
      'multi-video',
    ])
    expect(
      resolveWSClientCapabilities({ multiCall: true, multiVideo: true }),
    ).toEqual(['multi-call', 'multi-video'])
  })
})
