import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'

const API_BASE = resolveGatewayApiBaseUrl()

export interface HangupSessionResponse {
  sessionId: string
  state: string
  message: string
}

export interface SendDtmfResponse {
  sessionId: string
  message: string
}

export async function hangupSession(
  sessionId: string,
): Promise<HangupSessionResponse> {
  return fetchJson<HangupSessionResponse>(
    `${API_BASE}/hangup/${encodeURIComponent(sessionId)}`,
    { method: 'POST' },
  )
}

export async function sendSessionDtmf(
  sessionId: string,
  digits: string,
): Promise<SendDtmfResponse> {
  return fetchJson<SendDtmfResponse>(
    `${API_BASE}/dtmf/${encodeURIComponent(sessionId)}`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ digits }),
    },
  )
}