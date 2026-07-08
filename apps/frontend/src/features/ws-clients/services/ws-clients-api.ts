// apps/frontend/src/features/ws-clients/services/ws-clients-api.ts
import type { WSClient, WSClientStreamEvent } from '../types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'
import { subscribeAuthenticatedSse } from '@/lib/sse-subscriber'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchWSClients(): Promise<Array<WSClient>> {
  return fetchJson<Array<WSClient>>(`${API_BASE}/ws-clients`)
}

export function subscribeWSClientEvents(
  onEvent: (event: WSClientStreamEvent) => void,
  onError?: (event: Event) => void,
) {
  return subscribeAuthenticatedSse<WSClientStreamEvent>({
    url: `${API_BASE}/ws-clients/stream`,
    eventName: 'ws-client',
    onEvent,
    onError,
  })
}
