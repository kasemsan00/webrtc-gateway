import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'

export interface SwitchRequestPayload {
  sessionId: string
  queueNumber: string
  agentUsername: string
}

export interface SwitchResponsePayload {
  status: string
  sessionId: string
  queueNumber: string
  agentUsername: string
}

const API_BASE = resolveGatewayApiBaseUrl()

export async function sendSwitchRequest(
  payload: SwitchRequestPayload,
): Promise<SwitchResponsePayload> {
  return fetchJson<SwitchResponsePayload>(`${API_BASE}/switch`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
  })
}
