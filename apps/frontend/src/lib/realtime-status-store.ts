import { Store } from '@tanstack/store'

/**
 * Connection state of one realtime (SSE) stream a page is using.
 *
 * - `connecting`: first connect attempt in flight
 * - `live`: stream is open and delivering events
 * - `reconnecting`: stream dropped; a retry is scheduled
 * - `paused`: tab is hidden, stream intentionally closed
 */
export type RealtimeStreamState =
  | 'connecting'
  | 'live'
  | 'reconnecting'
  | 'paused'

export interface RealtimeStreamStatus {
  name: string
  state: RealtimeStreamState
  lastEventAt: number | null
  lastErrorAt: number | null
  retryCount: number
}

interface RealtimeStatusState {
  streams: Record<string, RealtimeStreamStatus>
}

export const realtimeStatusStore = new Store<RealtimeStatusState>({
  streams: {},
})

let nextId = 0

export function registerRealtimeStream(name: string): string {
  nextId += 1
  const id = `${name}#${nextId}`
  realtimeStatusStore.setState((prev) => ({
    streams: {
      ...prev.streams,
      [id]: {
        name,
        state: 'connecting',
        lastEventAt: null,
        lastErrorAt: null,
        retryCount: 0,
      },
    },
  }))
  return id
}

export function updateRealtimeStream(
  id: string,
  patch: Partial<Omit<RealtimeStreamStatus, 'name'>>,
) {
  realtimeStatusStore.setState((prev) => {
    const current = prev.streams[id] as RealtimeStreamStatus | undefined
    if (!current) return prev
    return { streams: { ...prev.streams, [id]: { ...current, ...patch } } }
  })
}

export function unregisterRealtimeStream(id: string) {
  realtimeStatusStore.setState((prev) => {
    if (!(id in prev.streams)) return prev
    const next = { ...prev.streams }
    delete next[id]
    return { streams: next }
  })
}

export type RealtimeSummaryState = RealtimeStreamState | 'none'

/** Worst state wins so a dropped stream is never hidden by a healthy one. */
export function summarizeRealtimeStreams(
  streams: Array<RealtimeStreamStatus>,
): RealtimeSummaryState {
  if (streams.length === 0) return 'none'
  const order: Array<RealtimeStreamState> = [
    'reconnecting',
    'connecting',
    'paused',
    'live',
  ]
  for (const state of order) {
    if (streams.some((stream) => stream.state === state)) return state
  }
  return 'none'
}

/** Delay before reconnect attempt `retry` (1-based): 1s, 2s, 4s … capped at 30s. */
export function reconnectDelayMs(retry: number): number {
  const attempt = Math.max(1, retry)
  return Math.min(30_000, 1000 * 2 ** (attempt - 1))
}
