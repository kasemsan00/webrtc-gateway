import type {
  DashboardSummaryParams,
  DashboardSummaryResponse,
} from '@/features/dashboard/types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'
import { appendQuery } from '@/lib/http-query'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchDashboardSummary(
  params: DashboardSummaryParams,
): Promise<DashboardSummaryResponse> {
  const url = appendQuery(`${API_BASE}/dashboard/summary`, {
    period: params.period,
    anchorDate: params.anchorDate,
  })

  return fetchJson<DashboardSummaryResponse>(url)
}
