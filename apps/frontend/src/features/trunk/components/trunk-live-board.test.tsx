// @vitest-environment jsdom
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { TrunkLiveBoard } from './trunk-live-board'
import type { ActiveSession } from '@/features/active-sessions/types'
import type { Trunk } from '@/features/trunk/types'

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
    isRegistered: true,
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
    durationSec: 12,
    createdAt: '2026-10-27T06:00:00Z',
    updatedAt: '2026-10-27T06:00:12Z',
    translatorEnabled: false,
    ...overrides,
  }
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('TrunkLiveBoard transitions', () => {
  it('keeps the live call list mounted for its exit animation when the last call finishes', async () => {
    const trunks = [
      makeTrunk({ id: 1, name: 'Primary', username: '1001' }),
      makeTrunk({ id: 2, name: 'Backup', username: '1002' }),
    ]
    const session = makeSession({ id: 's-1', trunkId: 1 })
    const now = Date.now()

    const { rerender } = render(
      <TrunkLiveBoard
        trunks={trunks}
        sessions={[session]}
        clients={[]}
        filter="all"
        onFilterChange={() => {}}
        sessionsFetchedAt={now}
      />,
    )

    const liveSection = screen.getByRole('region', { name: 'Live calls' })
    const idleSection = screen.getByRole('region', {
      name: 'Trunks not in a call',
    })
    expect(within(liveSection).getByText('0812345678')).toBeTruthy()
    expect(within(idleSection).getByText('Backup')).toBeTruthy()
    expect(within(idleSection).queryByText('Primary')).toBeNull()

    // End the last active call: liveCount goes 1 -> 0.
    rerender(
      <TrunkLiveBoard
        trunks={trunks}
        sessions={[]}
        clients={[]}
        filter="all"
        onFilterChange={() => {}}
        sessionsFetchedAt={now}
      />,
    )

    // Both the returning empty placeholder and the exiting live call list
    // coexist briefly while AnimatePresence runs the exit transition.
    expect(within(liveSection).getByText('No calls right now.')).toBeTruthy()
    expect(within(liveSection).getByText('0812345678')).toBeTruthy()
    expect(within(idleSection).getByText('Primary')).toBeTruthy()

    // Once the exit animation finishes, the live call lane unmounts cleanly.
    await waitFor(
      () => {
        expect(within(liveSection).queryByText('0812345678')).toBeNull()
      },
      { timeout: 1500 },
    )
    expect(within(liveSection).getByText('No calls right now.')).toBeTruthy()
    expect(within(idleSection).getByText('Primary')).toBeTruthy()
    expect(within(idleSection).getByText('Backup')).toBeTruthy()
  })

  it('animates direct (no-trunk) calls out when they finish', async () => {
    const trunks = [makeTrunk({ id: 1, name: 'Primary', username: '1001' })]
    const directCall = makeSession({
      id: 'direct-1',
      trunkId: 0,
      authMode: 'public',
      to: 'sip:0899999999@sip.example.com',
    })
    const now = Date.now()

    const { rerender } = render(
      <TrunkLiveBoard
        trunks={trunks}
        sessions={[directCall]}
        clients={[]}
        filter="all"
        onFilterChange={() => {}}
        sessionsFetchedAt={now}
      />,
    )

    const liveSection = screen.getByRole('region', { name: 'Live calls' })
    expect(within(liveSection).getByText('Direct call')).toBeTruthy()
    expect(within(liveSection).getByText('0899999999')).toBeTruthy()

    rerender(
      <TrunkLiveBoard
        trunks={trunks}
        sessions={[]}
        clients={[]}
        filter="all"
        onFilterChange={() => {}}
        sessionsFetchedAt={now}
      />,
    )

    expect(within(liveSection).getByText('No calls right now.')).toBeTruthy()
    expect(within(liveSection).getByText('Direct call')).toBeTruthy()

    await waitFor(
      () => {
        expect(within(liveSection).queryByText('Direct call')).toBeNull()
      },
      { timeout: 1500 },
    )
  })

  it('transitions smoothly when the only trunk enters and leaves a call', async () => {
    const trunks = [makeTrunk({ id: 1, name: 'Solo', username: '1001' })]
    const session = makeSession({ id: 's-solo', trunkId: 1 })
    const now = Date.now()

    const { rerender } = render(
      <TrunkLiveBoard
        trunks={trunks}
        sessions={[]}
        clients={[]}
        filter="all"
        onFilterChange={() => {}}
        sessionsFetchedAt={now}
      />,
    )

    expect(screen.getByText('No calls right now.')).toBeTruthy()
    expect(
      within(
        screen.getByRole('region', { name: 'Trunks not in a call' }),
      ).getByText('Solo'),
    ).toBeTruthy()

    // Start call on the only trunk: idleRows goes 1 -> 0.
    rerender(
      <TrunkLiveBoard
        trunks={trunks}
        sessions={[session]}
        clients={[]}
        filter="all"
        onFilterChange={() => {}}
        sessionsFetchedAt={now}
      />,
    )

    const liveSection = screen.getByRole('region', { name: 'Live calls' })
    expect(within(liveSection).getByText('0812345678')).toBeTruthy()
    await waitFor(
      () => {
        expect(
          screen.queryByRole('region', { name: 'Trunks not in a call' }),
        ).toBeNull()
      },
      { timeout: 1500 },
    )

    // Finish call: liveCount goes 1 -> 0 and idleRows goes 0 -> 1.
    rerender(
      <TrunkLiveBoard
        trunks={trunks}
        sessions={[]}
        clients={[]}
        filter="all"
        onFilterChange={() => {}}
        sessionsFetchedAt={now}
      />,
    )

    expect(within(liveSection).getByText('No calls right now.')).toBeTruthy()
    expect(within(liveSection).getByText('0812345678')).toBeTruthy()
    expect(
      within(
        screen.getByRole('region', { name: 'Trunks not in a call' }),
      ).getByText('Solo'),
    ).toBeTruthy()

    await waitFor(
      () => {
        expect(within(liveSection).queryByText('0812345678')).toBeNull()
      },
      { timeout: 1500 },
    )
  })

  it('displays Multi-stream capability and live screen share badges on trunks and calls', () => {
    const trunks = [
      makeTrunk({ id: 1, name: 'Agent 00025', username: '00025' }),
      makeTrunk({ id: 2, name: 'Agent 9001', username: 'lab9001' }),
    ]
    const sessions = [
      makeSession({
        id: 's-share',
        trunkId: 1,
        screenShareActive: true,
        screenShareDirection: 'both',
        screenShareConfirmed: true,
      }),
    ]
    const clients = [
      {
        clientId: 'c-1',
        sessionId: 's-share',
        connectedAt: '2026-10-27T06:00:00Z',
        trunkResolved: true,
        resolvedTrunkId: 1,
        sipUsername: '00025',
        agentOnly: true,
        multiVideo: true,
      },
      {
        clientId: 'c-2',
        connectedAt: '2026-10-27T06:00:00Z',
        trunkResolved: true,
        resolvedTrunkId: 2,
        sipUsername: 'lab9001',
        agentOnly: true,
        multiVideo: true,
      },
    ]

    render(
      <TrunkLiveBoard
        trunks={trunks}
        sessions={sessions}
        clients={clients}
        filter="all"
        onFilterChange={() => {}}
        sessionsFetchedAt={Date.now()}
      />,
    )

    const liveSection = screen.getByRole('region', { name: 'Live calls' })
    const idleSection = screen.getByRole('region', {
      name: 'Trunks not in a call',
    })
    expect(within(liveSection).getByText('Dual screen')).toBeTruthy()
    expect(within(liveSection).getByText('Multi-stream')).toBeTruthy()
    expect(within(idleSection).getByText('Multi-stream')).toBeTruthy()
    expect(screen.getByText('1 screen share')).toBeTruthy()
  })
})
