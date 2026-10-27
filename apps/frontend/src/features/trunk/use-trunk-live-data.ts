import { useCallback, useEffect, useRef, useState } from 'react'

import type { ActiveSession } from '@/features/active-sessions/types'
import type { WSClient } from '@/features/ws-clients/types'
import {
  fetchActiveSessions,
  subscribeSessionEvents,
} from '@/features/active-sessions/services/active-sessions-api'
import {
  fetchWSClients,
  subscribeWSClientEvents,
} from '@/features/ws-clients/services/ws-clients-api'
import { useVisibilityRealtimeReload } from '@/lib/use-visibility-realtime-reload'

export interface TrunkLiveData {
  sessions: Array<ActiveSession>
  clients: Array<WSClient>
  /** Local clock (ms) when `sessions` was fetched; durations are relative to it. */
  sessionsFetchedAt: number
  sessionsLoaded: boolean
  sessionsError: string | null
  clientsError: string | null
}

/**
 * Live calls (GET /api/sessions + /sessions/stream) and connected apps
 * (GET /api/ws-clients + /ws-clients/stream) for the trunk live board.
 */
export function useTrunkLiveData(): TrunkLiveData {
  const [sessions, setSessions] = useState<Array<ActiveSession>>([])
  const [clients, setClients] = useState<Array<WSClient>>([])
  const [sessionsFetchedAt, setSessionsFetchedAt] = useState(() => Date.now())
  const [sessionsLoaded, setSessionsLoaded] = useState(false)
  const [sessionsError, setSessionsError] = useState<string | null>(null)
  const [clientsError, setClientsError] = useState<string | null>(null)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const loadSessions = useCallback(async () => {
    try {
      const items = await fetchActiveSessions()
      if (!mountedRef.current) return
      setSessions(Array.isArray(items) ? items : [])
      setSessionsFetchedAt(Date.now())
      setSessionsError(null)
    } catch (err) {
      if (!mountedRef.current) return
      setSessionsError(
        err instanceof Error ? err.message : 'Failed to load live calls',
      )
    } finally {
      if (mountedRef.current) setSessionsLoaded(true)
    }
  }, [])

  const loadClients = useCallback(async () => {
    try {
      const items = await fetchWSClients()
      if (!mountedRef.current) return
      setClients(Array.isArray(items) ? items : [])
      setClientsError(null)
    } catch (err) {
      if (!mountedRef.current) return
      setClientsError(
        err instanceof Error ? err.message : 'Failed to load connected apps',
      )
    }
  }, [])

  useEffect(() => {
    void loadSessions()
    void loadClients()
  }, [loadSessions, loadClients])

  useVisibilityRealtimeReload({
    subscribe: subscribeSessionEvents,
    onReload: () => void loadSessions(),
    reloadDebounceMs: 300,
    pollIntervalMs: 5_000,
  })

  useVisibilityRealtimeReload({
    subscribe: subscribeWSClientEvents,
    onReload: () => void loadClients(),
    reloadDebounceMs: 500,
    pollIntervalMs: 15_000,
  })

  return {
    sessions,
    clients,
    sessionsFetchedAt,
    sessionsLoaded,
    sessionsError,
    clientsError,
  }
}

/** Current time that ticks every `intervalMs` while `enabled`. */
export function useNow(enabled: boolean, intervalMs = 1_000) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!enabled) return
    setNow(Date.now())
    const timer = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(timer)
  }, [enabled, intervalMs])
  return now
}
