import type {
  ClientDiagnosticListParams,
  ClientDiagnosticListResponse,
} from '../types'
import type { SessionPayload } from '@/features/session-detail/types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'
import { appendQuery } from '@/lib/http-query'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchClientDiagnostics(
  params: ClientDiagnosticListParams = {},
): Promise<ClientDiagnosticListResponse> {
  const url = appendQuery(`${API_BASE}/client-diagnostics`, {
    page: params.page,
    pageSize: params.pageSize,
    clientTraceId: params.clientTraceId,
    authSubject: params.authSubject,
    source: params.source,
    level: params.level,
    name: params.name,
  })
  return fetchJson<ClientDiagnosticListResponse>(url)
}

export async function fetchClientDiagnosticPayload(
  payloadId: number,
): Promise<SessionPayload> {
  return fetchJson<SessionPayload>(
    `${API_BASE}/client-diagnostics/payloads/${payloadId}`,
  )
}