import type {
  SessionHistoryListParams,
  SessionHistoryListResponse,
} from '../types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'
import { appendQuery } from '@/lib/http-query'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchSessionHistory(
  params: SessionHistoryListParams = {},
): Promise<SessionHistoryListResponse> {
  const url = appendQuery(`${API_BASE}/sessions/history`, {
    page: params.page,
    pageSize: params.pageSize,
    search: params.search,
    direction: params.direction,
    state: params.state,
    sessionId: params.sessionId,
    createdAfter: params.createdAfter,
    createdBefore: params.createdBefore,
  })
  return fetchJson<SessionHistoryListResponse>(url)
}
