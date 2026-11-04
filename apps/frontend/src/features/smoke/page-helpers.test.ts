import { describe, expect, it } from 'vitest'

import type { DashboardSummaryMetrics } from '@/features/dashboard/types'
import type { WSClient } from '@/features/ws-clients/types'
import { liveCallTimer } from '@/features/active-sessions/components/active-sessions-page'
import {
  computeSystemStatus,
  sessionsRangeSearch,
} from '@/features/dashboard/components/dashboard-page'
import { publicAccountStatus } from '@/features/public-accounts/components/public-accounts-page'
import { applyWSClientEvent } from '@/features/ws-clients/components/ws-clients-page'

const metrics: DashboardSummaryMetrics = {
  periodSessions: 10,
  activeSessions: 0,
  totalTrunks: 3,
  enabledTrunks: 2,
  registeredTrunks: 2,
  publicAccounts: 0,
  sessionDirectoryNow: 0,
  wsClients: 1,
  avgDurationSec: 30,
  maxDurationSec: 90,
}

describe('computeSystemStatus', () => {
  it('lets component health override call counts', () => {
    expect(computeSystemStatus(metrics, 'down')).toMatchObject({
      label: 'Component down',
      tone: 'danger',
    })
    expect(computeSystemStatus(metrics, 'degraded').tone).toBe('warning')
  })

  it('describes readiness from trunks and calls', () => {
    expect(computeSystemStatus(metrics, 'healthy').label).toBe('Ready')
    expect(
      computeSystemStatus({ ...metrics, activeSessions: 2 }, 'healthy').label,
    ).toBe('Handling calls')
    expect(
      computeSystemStatus({ ...metrics, registeredTrunks: 0 }, 'healthy').label,
    ).toBe('No trunk registered')
    expect(
      computeSystemStatus(
        { ...metrics, totalTrunks: 0, publicAccounts: 0 },
        'healthy',
      ).label,
    ).toBe('Not configured')
    expect(computeSystemStatus(undefined).label).toBe('Loading')
  })
})

describe('sessionsRangeSearch', () => {
  it('turns the dashboard range into Bangkok calendar dates', () => {
    expect(
      sessionsRangeSearch(
        {
          rangeStart: '2026-10-31T17:00:00Z',
          rangeEnd: '2026-11-30T17:00:00Z',
        },
        { state: 'failed' },
      ),
    ).toEqual({ from: '2026-11-01', to: '2026-11-30', state: 'failed' })
  })

  it('passes extra filters through without a summary', () => {
    expect(sessionsRangeSearch(null, { direction: 'inbound' })).toEqual({
      direction: 'inbound',
    })
  })
})

describe('liveCallTimer', () => {
  const now = Date.parse('2026-11-04T10:01:00Z')

  it('counts talk time from answer', () => {
    expect(
      liveCallTimer(
        {
          state: 'active',
          createdAt: '2026-11-04T10:00:00Z',
          answeredAt: '2026-11-04T10:00:20Z',
          durationSec: 0,
        },
        now,
        now,
      ),
    ).toEqual({ phase: 'talk', seconds: 40 })
  })

  it('counts ring time before answer', () => {
    expect(
      liveCallTimer(
        {
          state: 'ringing',
          createdAt: '2026-11-04T10:00:50Z',
          answeredAt: null,
          durationSec: 0,
        },
        now,
        now,
      ),
    ).toEqual({ phase: 'ring', seconds: 10 })
  })
})

describe('applyWSClientEvent', () => {
  const client = (id: string, callState = 'idle'): WSClient => ({
    clientId: id,
    connectedAt: '2026-11-04T10:00:00Z',
    trunkResolved: true,
    callState,
  })

  it('adds, updates and removes clients', () => {
    let list = applyWSClientEvent([], {
      type: 'connected',
      clientId: 'a',
      at: '',
      client: client('a'),
    })
    expect(list).toHaveLength(1)
    list = applyWSClientEvent(list, {
      type: 'updated',
      clientId: 'a',
      at: '',
      client: client('a', 'incall'),
    })
    expect(list[0].callState).toBe('incall')
    list = applyWSClientEvent(list, {
      type: 'disconnected',
      clientId: 'a',
      at: '',
    })
    expect(list).toHaveLength(0)
  })
})

describe('publicAccountStatus', () => {
  const now = Date.parse('2026-11-04T10:00:00Z')

  it('separates registered, failed and expired accounts', () => {
    expect(
      publicAccountStatus(
        {
          isRegistered: true,
          expiresAt: '2026-11-04T11:00:00Z',
          lastError: '',
        },
        now,
      ).label,
    ).toBe('Registered')
    expect(
      publicAccountStatus(
        {
          isRegistered: false,
          expiresAt: '2026-11-04T11:00:00Z',
          lastError: '403 Forbidden',
        },
        now,
      ).label,
    ).toBe('Failed')
    expect(
      publicAccountStatus(
        {
          isRegistered: true,
          expiresAt: '2026-11-04T09:00:00Z',
          lastError: '',
        },
        now,
      ).label,
    ).toBe('Expired')
  })
})
