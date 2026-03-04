import type {
  GatewayDashboard,
  GatewayInstanceListParams,
  GatewayInstanceListResponse,
  WSClient,
} from '../types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'
import { appendQuery } from '@/lib/http-query'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchGatewayInstances(
  params: GatewayInstanceListParams = {},
): Promise<GatewayInstanceListResponse> {
  const url = appendQuery(`${API_BASE}/gateway/instances`, {
    page: params.page,
    pageSize: params.pageSize,
    search: params.search,
  })
  return fetchJson<GatewayInstanceListResponse>(url)
}

export async function fetchGatewayDashboard(): Promise<GatewayDashboard> {
  return fetchJson<GatewayDashboard>(`${API_BASE}/dashboard`)
}

export async function fetchWSClients(): Promise<Array<WSClient>> {
  return fetchJson<Array<WSClient>>(`${API_BASE}/ws-clients`)
}
