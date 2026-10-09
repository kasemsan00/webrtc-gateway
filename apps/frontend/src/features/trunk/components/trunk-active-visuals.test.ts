import { describe, expect, it } from 'vitest'

import {
  formatActiveDestinationLabel,
  isTrunkOnCall,
  trunkTableRowClassName,
} from './trunk-active-visuals'
import type { Trunk } from '@/features/trunk/types'

function makeTrunk(overrides?: Partial<Trunk>): Trunk {
  return {
    id: 1,
    public_id: 'uid-1',
    name: 'Primary',
    domain: 'sip.example.com',
    port: 5060,
    username: '1001',
    transport: 'tcp',
    enabled: true,
    isDefault: false,
    activeCallCount: 0,
    leaseOwner: '',
    leaseUntil: '',
    lastRegisteredAt: '',
    isRegistered: false,
    lastError: '',
    createdAt: '',
    updatedAt: '',
    ...overrides,
  }
}

describe('isTrunkOnCall', () => {
  it('is false when activeCallCount is zero', () => {
    expect(isTrunkOnCall(makeTrunk({ activeCallCount: 0 }))).toBe(false)
  })

  it('is true when activeCallCount is positive', () => {
    expect(isTrunkOnCall(makeTrunk({ activeCallCount: 2 }))).toBe(true)
  })
})

describe('formatActiveDestinationLabel', () => {
  it('returns null when destinations are empty', () => {
    expect(formatActiveDestinationLabel(undefined)).toBeNull()
    expect(formatActiveDestinationLabel([])).toBeNull()
  })

  it('joins trimmed destinations', () => {
    expect(formatActiveDestinationLabel(['9999', ' 8888 '])).toBe('9999, 8888')
  })
})

describe('trunkTableRowClassName', () => {
  it('returns undefined when trunk is idle', () => {
    expect(trunkTableRowClassName(makeTrunk())).toBeUndefined()
  })

  it('returns highlight classes when trunk is on call', () => {
    expect(trunkTableRowClassName(makeTrunk({ activeCallCount: 1 }))).toBe(
      'border-l-2 border-cyan-400/80 bg-cyan-500/5',
    )
  })
})
