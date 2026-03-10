import { describe, expect, it } from 'vitest'

import { isRegisterActionDisabled } from './trunk-list-page'
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
