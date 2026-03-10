import { useCallback, useEffect, useRef } from 'react'

interface UseVisibilityRealtimeReloadOptions {
  /**
   * Called to subscribe to a realtime event source.
   * Must return an unsubscribe function.
   */
  subscribe: (onEvent: () => void, onError?: (event: Event) => void) => () => void
  /**
   * Called to reload data. Should be stable (e.g. wrapped in useCallback / ref).
   */
  onReload: () => void
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

/**
 * Manages a visibility-aware realtime subscription + periodic polling.
 *
 * - Connects the SSE subscription only when the tab is visible.
 * - Disconnects when the tab is hidden.
 * - Schedules a debounced reload on each realtime event to avoid rapid refetches.
 * - Polls at `pollIntervalMs` when the tab is visible.
 * - Triggers an immediate reload when the tab becomes visible again.
 */
export function useVisibilityRealtimeReload({
  subscribe,
  onReload,
  reloadDebounceMs = 900,
  pollIntervalMs = 30_000,
}: UseVisibilityRealtimeReloadOptions): void {
  const onReloadRef = useRef(onReload)
  onReloadRef.current = onReload

  const reloadTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const scheduleReload = useCallback(() => {
    if (reloadTimerRef.current) return
    reloadTimerRef.current = setTimeout(() => {
      reloadTimerRef.current = null
      onReloadRef.current()
    }, reloadDebounceMs)
  }, [reloadDebounceMs])

  useEffect(() => {
    let unsubscribe: (() => void) | null = null

    const connect = () => {
      if (document.visibilityState !== 'visible') return
      if (unsubscribe) return
      unsubscribe = subscribe(scheduleReload)
    }

    const disconnect = () => {
      if (unsubscribe) {
        unsubscribe()
        unsubscribe = null
      }
    }

    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible') {
        connect()
        scheduleReload()
      } else {
        disconnect()
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
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      if (pollTimer) clearInterval(pollTimer)
      disconnect()
      if (reloadTimerRef.current) {
        clearTimeout(reloadTimerRef.current)
        reloadTimerRef.current = null
      }
    }
  }, [subscribe, scheduleReload, pollIntervalMs])
}
