import type { ActiveSession } from '../types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'

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
  const stream = new EventSource(`${API_BASE}/sessions/stream`)

  const handleSessionEvent = (message: MessageEvent<string>) => {
    try {
      const parsed = JSON.parse(message.data) as SessionStreamEvent
      onEvent(parsed)
    } catch {
      // Ignore malformed event payloads.
    }
  }

  stream.addEventListener('session', (event) => {
    handleSessionEvent(event as MessageEvent<string>)
  })

  stream.onerror = (event) => {
    if (onError) {
      onError(event)
    }
  }

  return () => {
    stream.close()
  }
}
