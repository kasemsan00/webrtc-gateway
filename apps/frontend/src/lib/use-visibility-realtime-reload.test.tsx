// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { realtimeStatusStore } from './realtime-status-store'
import { useVisibilityRealtimeReload } from './use-visibility-realtime-reload'

type Handlers = {
  onEvent: (event: unknown) => void
  onError?: (event: Event) => void
  onOpen?: () => void
}

function createSubscribe() {
  const connections: Array<Handlers & { closed: boolean }> = []
  const subscribe = vi.fn(
    (
      onEvent: (event: unknown) => void,
      onError?: (event: Event) => void,
      onOpen?: () => void,
    ) => {
      const connection = { onEvent, onError, onOpen, closed: false }
      connections.push(connection)
      return () => {
        connection.closed = true
      }
    },
  )
  return { subscribe, connections }
}

describe('useVisibilityRealtimeReload', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    realtimeStatusStore.setState(() => ({ streams: {} }))
  })

  it('reports live, reconnects with backoff after a drop and reloads on recovery', () => {
    const { subscribe, connections } = createSubscribe()
    const onReload = vi.fn()
    const { result } = renderHook(() =>
      useVisibilityRealtimeReload({
        name: 'sessions',
        subscribe,
        onReload,
        reloadDebounceMs: 100,
        pollIntervalMs: 0,
      }),
    )

    expect(result.current.state).toBe('connecting')
    act(() => connections[0].onOpen?.())
    expect(result.current.state).toBe('live')

    // Stream drops: state flips to reconnecting and a retry is scheduled.
    act(() => connections[0].onError?.(new Event('close')))
    expect(result.current.state).toBe('reconnecting')
    expect(connections[0].closed).toBe(true)
    expect(subscribe).toHaveBeenCalledTimes(1)

    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(subscribe).toHaveBeenCalledTimes(2)

    // Recovery triggers one catch-up reload.
    act(() => connections[1].onOpen?.())
    expect(result.current.state).toBe('live')
    act(() => {
      vi.advanceTimersByTime(100)
    })
    expect(onReload).toHaveBeenCalledTimes(1)
  })

  it('debounces events into reloads, or hands them to onEvent', () => {
    const { subscribe, connections } = createSubscribe()
    const onReload = vi.fn()
    renderHook(() =>
      useVisibilityRealtimeReload({
        subscribe,
        onReload,
        reloadDebounceMs: 100,
        pollIntervalMs: 0,
      }),
    )
    act(() => {
      connections[0].onEvent({})
      connections[0].onEvent({})
      vi.advanceTimersByTime(100)
    })
    expect(onReload).toHaveBeenCalledTimes(1)

    const second = createSubscribe()
    const onEvent = vi.fn()
    const onReload2 = vi.fn()
    renderHook(() =>
      useVisibilityRealtimeReload({
        subscribe: second.subscribe,
        onReload: onReload2,
        onEvent,
        pollIntervalMs: 0,
      }),
    )
    act(() => second.connections[0].onEvent({ type: 'x' }))
    expect(onEvent).toHaveBeenCalledWith({ type: 'x' })
    act(() => {
      vi.advanceTimersByTime(2000)
    })
    expect(onReload2).not.toHaveBeenCalled()
  })

  it('publishes to the global status store and cleans up on unmount', () => {
    const { subscribe } = createSubscribe()
    const { unmount } = renderHook(() =>
      useVisibilityRealtimeReload({
        name: 'trunks',
        subscribe,
        onReload: () => {},
        pollIntervalMs: 0,
      }),
    )
    const streams = Object.values(realtimeStatusStore.state.streams)
    expect(streams).toHaveLength(1)
    expect(streams[0].name).toBe('trunks')
    unmount()
    expect(Object.keys(realtimeStatusStore.state.streams)).toHaveLength(0)
  })
})
