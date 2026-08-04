import { describe, expect, it } from 'vitest'

import { resolveWSClientPresenceLabel } from './types'

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
