import type { GatewayConfigResponse } from '@/features/gateway-config/types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchGatewayConfig(): Promise<GatewayConfigResponse> {
  return fetchJson<GatewayConfigResponse>(`${API_BASE}/config`)
}
