import type {
  SessionDialogListResponse,
  SessionEventListResponse,
  SessionPayload,
  SessionPayloadListResponse,
  SessionStatsListResponse,
} from '../types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'
import { appendQuery } from '@/lib/http-query'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchSessionEvents(
  sessionId: string,
  params: {
    page?: number
    pageSize?: number
    category?: string
    name?: string
  } = {},
): Promise<SessionEventListResponse> {
  const url = appendQuery(`${API_BASE}/sessions/${sessionId}/events`, {
    page: params.page,
    pageSize: params.pageSize,
    category: params.category,
    name: params.name,
  })
  return fetchJson<SessionEventListResponse>(url)
}

export async function fetchSessionPayloads(
  sessionId: string,
  params: { page?: number; pageSize?: number; kind?: string } = {},
): Promise<SessionPayloadListResponse> {
  const url = appendQuery(`${API_BASE}/sessions/${sessionId}/payloads`, {
    page: params.page,
    pageSize: params.pageSize,
    kind: params.kind,
  })
  return fetchJson<SessionPayloadListResponse>(url)
}

export async function fetchPayload(payloadId: number): Promise<SessionPayload> {
  return fetchJson<SessionPayload>(`${API_BASE}/payloads/${payloadId}`)
}

export async function fetchSessionDialogs(
  sessionId: string,
  params: { page?: number; pageSize?: number } = {},
): Promise<SessionDialogListResponse> {
  const url = appendQuery(`${API_BASE}/sessions/${sessionId}/dialogs`, {
    page: params.page,
    pageSize: params.pageSize,
  })
  return fetchJson<SessionDialogListResponse>(url)
}

export async function fetchSessionStats(
  sessionId: string,
  params: { page?: number; pageSize?: number } = {},
): Promise<SessionStatsListResponse> {
  const url = appendQuery(`${API_BASE}/sessions/${sessionId}/stats`, {
    page: params.page,
    pageSize: params.pageSize,
  })
  return fetchJson<SessionStatsListResponse>(url)
}
