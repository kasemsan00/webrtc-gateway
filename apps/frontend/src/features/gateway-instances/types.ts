export interface GatewayInstance {
  instanceId: string
  wsUrl: string
  expiresAt: string
  updatedAt: string
  isExpired: boolean
}

export interface GatewayDashboard {
  instanceId: string
  uptimeSeconds: number
  activeSessions: number
  totalTrunks: number
  enabledTrunks: number
  registeredTrunks: number
  publicAccounts: number
  wsClients: number
  dbConnected: boolean
}

export interface WSClient {
  sessionId: string
  connectedAt: string
}

export interface GatewayInstanceListResponse {
  items: Array<GatewayInstance>
  total: number
  page: number
  pageSize: number
}

export interface GatewayInstanceListParams {
  page?: number
  pageSize?: number
  search?: string
}
