import type { ActiveSession } from '../types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'
import { subscribeAuthenticatedSse } from '@/lib/sse-subscriber'

export interface SessionStreamEvent {
  type: string
  sessionId?: string
  at: string
}

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchActiveSessions(): Promise<Array<ActiveSession>> {
  return fetchJson<Array<ActiveSession>>(`${API_BASE}/sessions`)
}

export function subscribeSessionEvents(
  onEvent: (event: SessionStreamEvent) => void,
  onError?: (event: Event) => void,
) {
  return subscribeAuthenticatedSse<SessionStreamEvent>({
    url: `${API_BASE}/sessions/stream`,
    eventName: 'session',
    onEvent,
    onError,
  })
}