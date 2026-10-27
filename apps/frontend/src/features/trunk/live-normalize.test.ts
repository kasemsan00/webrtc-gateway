import { describe, expect, it } from 'vitest'

import {
  buildTrunkLiveRows,
  collectNonTrunkCalls,
  summarizeLiveRows,
} from './live-board'
import {
  normalizeSessions,
  normalizeTrunk,
  normalizeTrunks,
  normalizeWSClients,
} from './live-normalize'

/*
 * Payloads shaped like the real gateway responses: Go `omitempty` drops empty
 * strings / zero numbers / nil slices, pointers arrive as null.
 */

// Enabled, not registered, no error -> `lastError` key is absent (the crash).
const offlineTrunkWithoutLastError = {
  id: 7,
  public_id: 'uid-7',
  publicId: 'uid-7',
  name: 'Backup',
  domain: 'pbx.example.com',
  port: 5060,
  username: '2009',
  transport: 'udp',
  enabled: true,
  isDefault: false,
  activeCallCount: 0,
  isRegistered: false,
  sipAutoRegister: false,
  pushContactReady: false,
  fcmTokenReady: false,
  notifyUserBound: false,
  createdAt: '2026-10-01T00:00:00Z',
  updatedAt: '2026-10-01T00:00:00Z',
}

const nullyTrunk = {
  id: 8,
  name: null,
  username: null,
  domain: undefined,
  lastError: null,
  inUseBy: null,
  activeCallCount: 1,
  activeDestinations: [null, ' 0891234567 ', 42, ''],
  enabled: true,
}

describe('normalizeTrunk', () => {
  it('fills every string, number and array field', () => {
    const trunk = normalizeTrunk(nullyTrunk)
    expect(trunk.name).toBe('')
    expect(trunk.username).toBe('')
    expect(trunk.domain).toBe('')
    expect(trunk.lastError).toBe('')
    expect(trunk.transport).toBe('')
    expect(trunk.inUseBy).toBeNull()
    expect(trunk.activeDestinations).toEqual(['0891234567', '42'])
    expect(trunk.port).toBe(0)
    expect(trunk.isRegistered).toBe(false)
  })

  it('accepts garbage input without throwing', () => {
    expect(() => normalizeTrunk(null)).not.toThrow()
    expect(() => normalizeTrunk('x')).not.toThrow()
    expect(normalizeTrunk(undefined).lastError).toBe('')
  })

  it('reads in_use_by or inUseBy and is idempotent', () => {
    const once = normalizeTrunk({ id: 1, in_use_by: 'nok' })
    expect(once.inUseBy).toBe('nok')
    expect(normalizeTrunk(once)).toEqual(once)
  })
})

describe('normalize lists', () => {
  it('drops null entries and handles null / missing arrays', () => {
    expect(normalizeTrunks(null)).toEqual([])
    expect(normalizeTrunks(undefined)).toEqual([])
    expect(normalizeTrunks([null, offlineTrunkWithoutLastError])).toHaveLength(
      1,
    )
    expect(normalizeSessions({ items: [] })).toEqual([])
    expect(normalizeWSClients([undefined, 3, { clientId: 'c' }])).toHaveLength(
      1,
    )
  })
})

describe('live board with real-world partial payloads', () => {
  const sessions = [
    // Missing state, direction, from, to, trunkName, sipUsername.
    { id: 's-missing', trunkId: 7, durationSec: 3, createdAt: '' },
    // Outbound call with null answeredAt and only `to`.
    {
      id: 's-ring',
      state: 'ringing',
      direction: 'outbound',
      to: 'sip:0812345678@pbx',
      trunkId: 8,
      durationSec: 2,
      answeredAt: null,
    },
    // Public call without trunkId (omitempty).
    { id: 's-public', state: 'active', direction: 'inbound', durationSec: 9 },
    null,
  ]
  const clients = [
    // No sipUsername / authSubject / sessionId / resolvedTrunkId.
    { clientId: 'c-bare', trunkResolved: false },
    {
      clientId: 'c-agent',
      trunkResolved: true,
      resolvedTrunkId: 8,
      agentOnly: true,
    },
    null,
  ]
  const trunks = [offlineTrunkWithoutLastError, nullyTrunk, null]

  it('builds rows without throwing (regression: lastError omitted)', () => {
    expect(() => buildTrunkLiveRows(trunks, sessions, clients)).not.toThrow()
    const rows = buildTrunkLiveRows(trunks, sessions, clients)
    const byId = new Map(rows.map((row) => [row.trunk.id, row]))

    expect(byId.get(7)?.status).toBe('offline')
    expect(byId.get(8)?.status).toBe('dialing')
    expect(byId.get(8)?.calls[0].remote.label).toBe('0812345678')
    // Agent bound to the trunk but not the session -> falls back to the line.
    expect(byId.get(8)?.calls[0].operator).toBeNull()
  })

  it('collects direct calls and summarises without throwing', () => {
    const other = collectNonTrunkCalls(sessions, clients)
    expect(other.map((call) => call.sessionId)).toEqual(['s-public'])
    expect(other[0].remote.label).toBe('Unknown')
    const rows = buildTrunkLiveRows(trunks, sessions, clients)
    expect(() => summarizeLiveRows(rows, other)).not.toThrow()
  })

  it('does not match clients without a session to calls without an id', () => {
    const rows = buildTrunkLiveRows(
      [{ id: 3, enabled: true, isRegistered: true, username: '2003' }],
      [{ state: 'active', direction: 'outbound', trunkId: 3, to: '9999' }],
      [{ clientId: 'c', trunkResolved: true, resolvedTrunkId: 3 }],
    )
    expect(rows[0].calls[0].operator).toEqual({ kind: 'line', label: '2003' })
  })
})
