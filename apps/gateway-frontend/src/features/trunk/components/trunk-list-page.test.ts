import { describe, expect, it } from 'vitest'

import {
  getTrunkLifecycleActionLabel,
  getTrunkStatusLabel,
  isRegisterActionDisabled,
} from './trunk-list-page'
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

describe('isRegisterActionDisabled', () => {
  it('disables when already registered', () => {
    expect(isRegisterActionDisabled(makeTrunk({ isRegistered: true }))).toBe(
      true,
    )
  })

  it('disables when trunk is not enabled', () => {
    expect(isRegisterActionDisabled(makeTrunk({ enabled: false }))).toBe(true)
  })

  it('enables when trunk is unregistered and enabled', () => {
    expect(
      isRegisterActionDisabled(
        makeTrunk({ enabled: true, isRegistered: false }),
      ),
    ).toBe(false)
  })
})

describe('trunk soft-delete labels', () => {
  it('returns Enabled status when trunk is enabled', () => {
    expect(getTrunkStatusLabel(true)).toBe('Enabled')
  })

  it('returns Deleted status when trunk is disabled', () => {
    expect(getTrunkStatusLabel(false)).toBe('Deleted')
  })

  it('returns Disable action when trunk is enabled', () => {
    expect(getTrunkLifecycleActionLabel(true)).toBe('Disable')
  })

  it('returns Restore action when trunk is disabled', () => {
    expect(getTrunkLifecycleActionLabel(false)).toBe('Restore')
  })
})
