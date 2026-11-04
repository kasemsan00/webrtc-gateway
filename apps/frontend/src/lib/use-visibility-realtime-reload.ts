import { useCallback, useEffect, useRef, useState } from 'react'

import {
  reconnectDelayMs,
  registerRealtimeStream,
  unregisterRealtimeStream,
  updateRealtimeStream,
} from './realtime-status-store'
import type { RealtimeStreamState } from './realtime-status-store'

export type RealtimeSubscribe<TEvent = unknown> = (
  onEvent: (event: TEvent) => void,
  onError?: (event: Event) => void,
  onOpen?: () => void,
) => () => void

interface UseVisibilityRealtimeReloadOptions<TEvent> {
  /**
   * Called to subscribe to a realtime event source.
   * Must return an unsubscribe function.
   */
  subscribe: RealtimeSubscribe<TEvent>
  /**
   * Called to reload data. Should be stable (e.g. wrapped in useCallback / ref).
   */
  onReload: () => void
  /**
   * Optional per-event handler (for pages that patch state from the event
   * instead of refetching). When set, events do not schedule a reload.
   */
  onEvent?: (event: TEvent) => void
  /** Stream name shown in the header connection indicator. */
  name?: string
  /**
   * Debounce delay in ms before triggering a reload after a realtime event.
   * @default 900
   */
  reloadDebounceMs?: number
  /**
   * Polling interval in ms. Set to 0 to disable polling.
   * @default 30000
   */
  pollIntervalMs?: number
}

export interface RealtimeReloadStatus {
  state: RealtimeStreamState
  lastEventAt: number | null
}

/**
 * Manages a visibility-aware realtime subscription + periodic polling.
 *
 * - Connects the SSE subscription only when the tab is visible.
 * - Disconnects when the tab is hidden.
 * - Reconnects with exponential backoff (1s → 30s) when the stream drops,
 *   and reloads once on reconnect so missed events are not lost.
 * - Schedules a debounced reload on each realtime event to avoid rapid refetches.
 * - Polls at `pollIntervalMs` when the tab is visible.
 * - Publishes the connection state to the global realtime status store so the
 *   header can show whether data is live.
 */
export function useVisibilityRealtimeReload<TEvent = unknown>({
  subscribe,
  onReload,
  onEvent,
  name = 'stream',
  reloadDebounceMs = 900,
  pollIntervalMs = 30_000,
}: UseVisibilityRealtimeReloadOptions<TEvent>): RealtimeReloadStatus {
  const onReloadRef = useRef(onReload)
  onReloadRef.current = onReload
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  const [status, setStatus] = useState<RealtimeReloadStatus>({
    state: 'connecting',
    lastEventAt: null,
  })

  const reloadTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const scheduleReload = useCallback(() => {
    if (reloadTimerRef.current) return
    reloadTimerRef.current = setTimeout(() => {
      reloadTimerRef.current = null
      onReloadRef.current()
    }, reloadDebounceMs)
  }, [reloadDebounceMs])

  useEffect(() => {
    const streamId = registerRealtimeStream(name)
    let unsubscribe: (() => void) | null = null
    let retryTimer: ReturnType<typeof setTimeout> | null = null
    let retryCount = 0
    let disposed = false

    const publish = (
      state: RealtimeStreamState,
      extra: { lastEventAt?: number; lastErrorAt?: number } = {},
    ) => {
      if (disposed) return
      updateRealtimeStream(streamId, { state, retryCount, ...extra })
      setStatus((prev) => ({
        state,
        lastEventAt: extra.lastEventAt ?? prev.lastEventAt,
      }))
    }

    const clearRetry = () => {
      if (retryTimer) {
        clearTimeout(retryTimer)
        retryTimer = null
      }
    }

    const disconnect = () => {
      clearRetry()
      if (unsubscribe) {
        unsubscribe()
        unsubscribe = null
      }
    }

    const connect = () => {
      if (disposed || document.visibilityState !== 'visible') return
      if (unsubscribe) return
      publish(retryCount > 0 ? 'reconnecting' : 'connecting')
      unsubscribe = subscribe(
        (event) => {
          const at = Date.now()
          publish('live', { lastEventAt: at })
          if (onEventRef.current) {
            onEventRef.current(event)
          } else {
            scheduleReload()
          }
        },
        () => {
          if (disposed) return
          if (unsubscribe) {
            unsubscribe()
            unsubscribe = null
          }
          retryCount += 1
          publish('reconnecting', { lastErrorAt: Date.now() })
          clearRetry()
          retryTimer = setTimeout(() => {
            retryTimer = null
            connect()
          }, reconnectDelayMs(retryCount))
        },
        () => {
          const recovered = retryCount > 0
          retryCount = 0
          publish('live')
          // Catch up on anything that happened while the stream was down.
          if (recovered) scheduleReload()
        },
      )
    }

    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible') {
        connect()
        scheduleReload()
      } else {
        disconnect()
        publish('paused')
      }
    }

    connect()

    const pollTimer =
      pollIntervalMs > 0
        ? setInterval(() => {
            if (document.visibilityState === 'visible') {
              onReloadRef.current()
            }
          }, pollIntervalMs)
        : null

    document.addEventListener('visibilitychange', handleVisibilityChange)

    return () => {
      disposed = true
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      if (pollTimer) clearInterval(pollTimer)
      disconnect()
      unregisterRealtimeStream(streamId)
      if (reloadTimerRef.current) {
        clearTimeout(reloadTimerRef.current)
        reloadTimerRef.current = null
      }
    }
  }, [subscribe, scheduleReload, pollIntervalMs, name])

  return status
}
