import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'

import type { GatewayConfigResponse } from '@/features/gateway-config/types'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchGatewayConfig(): Promise<GatewayConfigResponse> {
  return fetchJson<GatewayConfigResponse>(`${API_BASE}/config`)
}
