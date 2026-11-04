import { afterEach, describe, expect, it } from 'vitest'

import {
  realtimeStatusStore,
  reconnectDelayMs,
  registerRealtimeStream,
  summarizeRealtimeStreams,
  unregisterRealtimeStream,
  updateRealtimeStream,
} from './realtime-status-store'
import type { RealtimeStreamStatus } from './realtime-status-store'

function stream(state: RealtimeStreamStatus['state']): RealtimeStreamStatus {
  return {
    name: 's',
    state,
    lastEventAt: null,
    lastErrorAt: null,
    retryCount: 0,
  }
}

describe('summarizeRealtimeStreams', () => {
  it('reports none without streams', () => {
    expect(summarizeRealtimeStreams([])).toBe('none')
  })

  it('lets the worst state win so a dropped stream is visible', () => {
    expect(summarizeRealtimeStreams([stream('live'), stream('live')])).toBe(
      'live',
    )
    expect(
      summarizeRealtimeStreams([stream('live'), stream('reconnecting')]),
    ).toBe('reconnecting')
    expect(summarizeRealtimeStreams([stream('paused'), stream('live')])).toBe(
      'paused',
    )
  })
})

describe('reconnectDelayMs', () => {
  it('backs off exponentially and caps at 30s', () => {
    expect(reconnectDelayMs(1)).toBe(1000)
    expect(reconnectDelayMs(2)).toBe(2000)
    expect(reconnectDelayMs(4)).toBe(8000)
    expect(reconnectDelayMs(10)).toBe(30_000)
    expect(reconnectDelayMs(0)).toBe(1000)
  })
})

describe('realtime stream registry', () => {
  afterEach(() => {
    realtimeStatusStore.setState(() => ({ streams: {} }))
  })

  it('registers, updates and removes streams', () => {
    const id = registerRealtimeStream('sessions')
    expect(realtimeStatusStore.state.streams[id].state).toBe('connecting')

    updateRealtimeStream(id, { state: 'live', lastEventAt: 42 })
    expect(realtimeStatusStore.state.streams[id]).toMatchObject({
      name: 'sessions',
      state: 'live',
      lastEventAt: 42,
    })

    unregisterRealtimeStream(id)
    expect(realtimeStatusStore.state.streams[id]).toBeUndefined()
  })

  it('ignores updates for unknown streams', () => {
    updateRealtimeStream('missing#1', { state: 'live' })
    expect(Object.keys(realtimeStatusStore.state.streams)).toHaveLength(0)
  })
})
