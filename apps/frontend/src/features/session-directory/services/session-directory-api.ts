import type {
  SessionDirectoryListParams,
  SessionDirectoryListResponse,
} from '../types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'
import { appendQuery } from '@/lib/http-query'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchSessionDirectory(
  params: SessionDirectoryListParams = {},
): Promise<SessionDirectoryListResponse> {
  const url = appendQuery(`${API_BASE}/session-directory`, {
    page: params.page,
    pageSize: params.pageSize,
    search: params.search,
  })
  return fetchJson<SessionDirectoryListResponse>(url)
}
