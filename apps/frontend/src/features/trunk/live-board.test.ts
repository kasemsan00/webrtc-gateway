import { describe, expect, it } from 'vitest'

import {
  buildTrunkLiveRows,
  callTimerSec,
  collectNonTrunkCalls,
  deriveTrunkLiveStatus,
  formatCallDuration,
  isActiveLiveRow,
  matchesLiveFilter,
  parseSipParty,
  resolveCallOperator,
  sessionCallPhase,
  summarizeLiveRows,
  toLiveCall,
} from './live-board'
import type { ActiveSession } from '@/features/active-sessions/types'
import type { Trunk } from '@/features/trunk/types'
import type { WSClient } from '@/features/ws-clients/types'

function makeTrunk(overrides?: Partial<Trunk>): Trunk {
  return {
    id: 1,
    public_id: 'uid-1',
    name: 'Primary',
    domain: 'sip.example.com',
    port: 5060,
    username: '1001',
    transport: 'udp',
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

function makeSession(overrides?: Partial<ActiveSession>): ActiveSession {
  return {
    id: 's-1',
    state: 'active',
    direction: 'outbound',
    from: 'sip:1001@sip.example.com',
    to: 'sip:0812345678@sip.example.com',
    sipCallId: '',
    authMode: 'trunk',
    trunkId: 1,
    trunkName: 'Primary',
    sipUsername: '1001',
    durationSec: 30,
    createdAt: '',
    updatedAt: '',
    translatorEnabled: false,
    ...overrides,
  }
}

function makeClient(overrides?: Partial<WSClient>): WSClient {
  return {
    clientId: 'c-1',
    connectedAt: '',
    trunkResolved: true,
    resolvedTrunkId: 1,
    ...overrides,
  }
}

describe('sessionCallPhase', () => {
  it('maps outbound setup states to dialing', () => {
    for (const state of ['new', 'connecting', 'ringing']) {
      expect(sessionCallPhase({ state, direction: 'outbound' })).toBe('dialing')
    }
  })

  it('maps inbound setup states to incoming', () => {
    expect(sessionCallPhase({ state: 'ringing', direction: 'inbound' })).toBe(
      'incoming',
    )
    expect(sessionCallPhase({ state: 'incoming', direction: '' })).toBe(
      'incoming',
    )
  })

  it('maps active and reconnecting, ignores ended', () => {
    expect(sessionCallPhase({ state: 'active', direction: 'outbound' })).toBe(
      'talking',
    )
    expect(
      sessionCallPhase({ state: 'reconnecting', direction: 'inbound' }),
    ).toBe('reconnecting')
    expect(sessionCallPhase({ state: 'ended', direction: 'outbound' })).toBe(
      null,
    )
  })
})

describe('parseSipParty', () => {
  it('extracts user part and host from a SIP URI', () => {
    expect(
      parseSipParty('sip:0812345678@pbx.example.com;transport=udp'),
    ).toEqual({ label: '0812345678', detail: 'pbx.example.com' })
  })

  it('prefers display name as detail', () => {
    expect(parseSipParty('"Alice" <sip:1001@pbx.example.com>')).toEqual({
      label: '1001',
      detail: 'Alice',
    })
  })

  it('passes plain numbers through and handles empty input', () => {
    expect(parseSipParty(' 9999 ')).toEqual({ label: '9999' })
    expect(parseSipParty('')).toEqual({ label: 'Unknown' })
    expect(parseSipParty(undefined)).toEqual({ label: 'Unknown' })
  })
})

describe('toLiveCall', () => {
  it('uses `to` as remote party for outbound calls', () => {
    expect(toLiveCall(makeSession())?.remote.label).toBe('0812345678')
  })

  it('uses `from` as remote party for inbound calls', () => {
    const call = toLiveCall(
      makeSession({
        direction: 'inbound',
        state: 'ringing',
        from: 'sip:0899999999@carrier.example',
        to: 'sip:1001@sip.example.com',
      }),
    )
    expect(call?.phase).toBe('incoming')
    expect(call?.remote.label).toBe('0899999999')
    expect(call?.local.label).toBe('1001')
  })

  it('drops ended sessions', () => {
    expect(toLiveCall(makeSession({ state: 'ended' }))).toBeNull()
  })
})

describe('deriveTrunkLiveStatus', () => {
  it('prefers talking over dialing', () => {
    expect(
      deriveTrunkLiveStatus(makeTrunk({ isRegistered: true }), [
        { phase: 'dialing' },
        { phase: 'talking' },
      ]),
    ).toBe('talking')
  })

  it('reports busy when the trunk counts calls this instance cannot see', () => {
    expect(deriveTrunkLiveStatus(makeTrunk({ activeCallCount: 1 }), [])).toBe(
      'busy',
    )
  })

  it('reports online, error, offline and disabled when idle', () => {
    expect(deriveTrunkLiveStatus(makeTrunk({ isRegistered: true }), [])).toBe(
      'online',
    )
    expect(
      deriveTrunkLiveStatus(makeTrunk({ lastError: 'i/o timeout' }), []),
    ).toBe('error')
    expect(deriveTrunkLiveStatus(makeTrunk(), [])).toBe('offline')
    expect(
      deriveTrunkLiveStatus(
        makeTrunk({ enabled: false, isRegistered: true }),
        [],
      ),
    ).toBe('disabled')
  })
})

describe('buildTrunkLiveRows', () => {
  const trunks = [
    makeTrunk({ id: 1, name: 'Idle', isRegistered: true }),
    makeTrunk({ id: 2, name: 'Dialing', isRegistered: true }),
    makeTrunk({ id: 3, name: 'Offline' }),
    makeTrunk({ id: 4, name: 'Talking', isRegistered: true }),
  ]
  const sessions = [
    makeSession({ id: 'a', trunkId: 2, state: 'ringing' }),
    makeSession({ id: 'b', trunkId: 4, state: 'active' }),
    makeSession({ id: 'c', trunkId: 0, authMode: 'public' }),
  ]
  const clients = [
    makeClient({ clientId: 'x', resolvedTrunkId: 1 }),
    makeClient({ clientId: 'y', resolvedTrunkId: 3, trunkResolved: false }),
  ]

  it('sorts by live status and attaches calls and clients', () => {
    const rows = buildTrunkLiveRows(trunks, sessions, clients)
    expect(rows.map((row) => row.trunk.name)).toEqual([
      'Talking',
      'Dialing',
      'Idle',
      'Offline',
    ])
    expect(rows[0].calls.map((call) => call.sessionId)).toEqual(['b'])
    expect(rows[2].clients.map((client) => client.clientId)).toEqual(['x'])
    expect(rows[3].clients).toEqual([])
  })

  it('keeps trunk destinations when sessions are not visible', () => {
    const [row] = buildTrunkLiveRows(
      [makeTrunk({ activeCallCount: 1, activeDestinations: [' 9999 ', ''] })],
      [],
      [],
    )
    expect(row.status).toBe('busy')
    expect(row.fallbackDestinations).toEqual(['9999'])
  })

  it('summarizes and filters rows', () => {
    const rows = buildTrunkLiveRows(trunks, sessions, clients)
    expect(summarizeLiveRows(rows)).toEqual({
      trunks: 4,
      online: 3,
      calling: 1,
      dialing: 1,
      incoming: 0,
      talking: 1,
      offline: 1,
      appsConnected: 1,
    })
    const names = (filter: Parameters<typeof matchesLiveFilter>[1]) =>
      rows
        .filter((row) => matchesLiveFilter(row, filter))
        .map((row) => row.trunk.name)
    expect(names('calling')).toEqual(['Dialing'])
    expect(names('talking')).toEqual(['Talking'])
    expect(names('offline')).toEqual(['Offline'])
    expect(names('all')).toHaveLength(4)
  })

  it('collects calls without a trunk separately', () => {
    expect(
      collectNonTrunkCalls(sessions).map((call) => call.sessionId),
    ).toEqual(['c'])
  })
})

describe('formatCallDuration', () => {
  it('formats minutes and hours', () => {
    expect(formatCallDuration(0)).toBe('00:00')
    expect(formatCallDuration(65)).toBe('01:05')
    expect(formatCallDuration(3725)).toBe('1:02:05')
    expect(formatCallDuration(-5)).toBe('00:00')
  })
})

describe('callTimerSec', () => {
  const base = {
    durationSec: 100,
    createdAt: '2026-10-27T06:00:00Z',
    answeredAt: '2026-10-27T06:00:30Z',
  }

  it('counts talk time from answeredAt plus time since fetch', () => {
    expect(callTimerSec({ ...base, phase: 'talking' }, 5_000, 0)).toBe(75)
  })

  it('falls back to call start without answeredAt', () => {
    expect(
      callTimerSec({ ...base, phase: 'talking', answeredAt: null }, 0, 0),
    ).toBe(100)
  })

  it('counts ringing time from call start', () => {
    expect(
      callTimerSec({ ...base, phase: 'dialing', answeredAt: null }, 2_000, 0),
    ).toBe(102)
  })
})

describe('resolveCallOperator', () => {
  const trunk = { username: '2001', inUseBy: null, in_use_by: null }

  it('prefers the client bound to the session', () => {
    expect(
      resolveCallOperator({ sessionId: 's-1' }, trunk, [
        makeClient({
          sessionId: 's-1',
          agentOnly: true,
          authSubject: 'nok@k2',
        }),
      ]),
    ).toEqual({ kind: 'agent', label: 'nok@k2' })
  })

  it('uses inUseBy as the label and skips UUID subjects', () => {
    expect(
      resolveCallOperator({ sessionId: 's-1' }, { ...trunk, inUseBy: 'nok' }, [
        makeClient({
          sessionId: 's-1',
          authSubject: '0f8fad5b-d9cb-469f-a165-70867728950e',
        }),
      ]),
    ).toEqual({ kind: 'app', label: 'nok' })
    expect(
      resolveCallOperator({ sessionId: 's-1' }, trunk, [
        makeClient({
          sessionId: 's-1',
          authSubject: '0f8fad5b-d9cb-469f-a165-70867728950e',
          sipUsername: '2001',
        }),
      ]),
    ).toEqual({ kind: 'app', label: '2001' })
  })

  it('falls back to inUseBy, then the trunk line', () => {
    expect(
      resolveCallOperator(
        { sessionId: 's-1' },
        { ...trunk, inUseBy: 'nok' },
        [],
      ),
    ).toEqual({ kind: 'user', label: 'nok' })
    expect(resolveCallOperator({ sessionId: 's-1' }, trunk, [])).toEqual({
      kind: 'line',
      label: '2001',
    })
  })
})

describe('isActiveLiveRow', () => {
  it('keeps only rows with live calls wide', () => {
    expect(isActiveLiveRow({ status: 'incoming' })).toBe(true)
    expect(isActiveLiveRow({ status: 'busy' })).toBe(true)
    expect(isActiveLiveRow({ status: 'online' })).toBe(false)
    expect(isActiveLiveRow({ status: 'error' })).toBe(false)
  })
})
