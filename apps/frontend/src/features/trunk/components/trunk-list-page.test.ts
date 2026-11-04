import { describe, expect, it } from 'vitest'

import {
  canTestIncomingPush,
  getTrunkLeaseView,
  getTrunkLifecycleActionLabel,
  getTrunkRegistrationView,
  getTrunkStatusLabel,
  getPushContactDetailText,
  isPushContactReady,
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

describe('isPushContactReady', () => {
  it('is ready only when masked push contact fields are present', () => {
    expect(
      isPushContactReady(
        makeTrunk({
          pnAppId: 'th.or.ttrs.video.prod',
          pnType: 'apple',
          pnTokenMasked: 'D6F5DF...2A74CCF',
          pushContactReady: true,
        }),
      ),
    ).toBe(true)
  })

  it('does not treat raw missing token state as ready', () => {
    expect(
      isPushContactReady(
        makeTrunk({
          pnAppId: 'th.or.ttrs.video.prod',
          pnType: 'apple',
          pushContactReady: true,
        }),
      ),
    ).toBe(false)
  })
})

describe('getPushContactDetailText', () => {
  it('describes ready SIP PN contact', () => {
    expect(
      getPushContactDetailText(
        makeTrunk({
          pnAppId: 'app',
          pnType: 'apple',
          pnTokenMasked: 'ABCD...EF',
          pushContactReady: true,
        }),
      ),
    ).toBe('apple; ABCD...EF')
  })

  it('notes FCM when SIP PN contact is absent', () => {
    expect(getPushContactDetailText(makeTrunk({ fcmTokenReady: true }))).toBe(
      'Uses FCM (no SIP PN contact)',
    )
  })

  it('notes no push paths when nothing is bound', () => {
    expect(getPushContactDetailText(makeTrunk())).toBe('No SIP PN contact')
  })
})

describe('canTestIncomingPush', () => {
  it('enables when a stored FCM token is ready', () => {
    expect(canTestIncomingPush(makeTrunk({ fcmTokenReady: true }))).toBe(true)
  })

  it('enables when a notify user is bound', () => {
    expect(canTestIncomingPush(makeTrunk({ notifyUserBound: true }))).toBe(true)
  })

  it('enables when Apple push contact is ready', () => {
    expect(
      canTestIncomingPush(
        makeTrunk({
          pnAppId: 'th.or.ttrs.video.prod',
          pnType: 'apple',
          pnTokenMasked: 'D6F5DF...2A74CCF',
          pushContactReady: true,
        }),
      ),
    ).toBe(true)
  })

  it('disables when the trunk has no push target', () => {
    expect(canTestIncomingPush(makeTrunk())).toBe(false)
  })
})

describe('getTrunkRegistrationView', () => {
  it('shows a failed registration in red with the last error', () => {
    const view = getTrunkRegistrationView(
      makeTrunk({ isRegistered: false, lastError: '403 Forbidden' }),
    )
    expect(view).toMatchObject({ label: 'Failed', tone: 'destructive' })
    expect(view.hint).toContain('403 Forbidden')
  })

  it('distinguishes registered, idle and disabled trunks', () => {
    expect(
      getTrunkRegistrationView(makeTrunk({ isRegistered: true })).label,
    ).toBe('Registered')
    expect(getTrunkRegistrationView(makeTrunk()).label).toBe('Unregistered')
    expect(
      getTrunkRegistrationView(makeTrunk({ enabled: false, lastError: 'x' }))
        .label,
    ).toBe('Disabled')
  })
})

describe('getTrunkLeaseView', () => {
  const now = Date.parse('2026-11-04T10:00:00Z')

  it('returns null without an owner', () => {
    expect(getTrunkLeaseView(makeTrunk(), now)).toBeNull()
  })

  it('marks an expired lease', () => {
    expect(
      getTrunkLeaseView(
        makeTrunk({ leaseOwner: 'gw-a', leaseUntil: '2026-11-04T09:00:00Z' }),
        now,
      ),
    ).toEqual({ owner: 'gw-a', expired: true })
    expect(
      getTrunkLeaseView(
        makeTrunk({ leaseOwner: 'gw-a', leaseUntil: '2026-11-04T11:00:00Z' }),
        now,
      ),
    ).toEqual({ owner: 'gw-a', expired: false })
  })
})
