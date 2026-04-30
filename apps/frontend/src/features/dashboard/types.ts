export type DashboardPeriod = 'day' | 'month' | 'year'

export interface DashboardSummaryMetrics {
  periodSessions: number
  activeSessions: number
  totalTrunks: number
  enabledTrunks: number
  registeredTrunks: number
  publicAccounts: number
  sessionDirectoryNow: number
  wsClients: number
  avgDurationSec: number
  maxDurationSec: number
}

export interface DashboardSummarySeriesPoint {
  bucket: string
  count: number
}

export interface DashboardSummaryStatePoint {
  state: string
  count: number
}

export interface DashboardSummaryTrunkPoint {
  trunkKey: string
  trunkName: string
  count: number
}

export interface DashboardSummaryDirectionPoint {
  direction: string
  count: number
}

export interface DashboardSummaryResponse {
  period: DashboardPeriod
  anchorDate: string
  timezone: string
  rangeStart: string
  rangeEnd: string
  metrics: DashboardSummaryMetrics
  series: Array<DashboardSummarySeriesPoint>
  states: Array<DashboardSummaryStatePoint>
  directions: Array<DashboardSummaryDirectionPoint>
  topTrunks: Array<DashboardSummaryTrunkPoint>
}

export interface DashboardSummaryParams {
  period?: DashboardPeriod
  anchorDate?: string
}
