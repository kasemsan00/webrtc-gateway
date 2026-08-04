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
  clientId?: string
  sessionId?: string
  connectedAt: string
  trunkResolved?: boolean
  resolvedTrunkId?: number
  resolvedTrunkPublicId?: string
  availability?: 'idle' | 'busy' | 'unavailable' | string
  callState?: string
  authSubject?: string
  publicOnly?: boolean
  agentOnly?: boolean
  multiCall?: boolean
  activeCalls?: number
  presenceMode?: 'ephemeral' | 'sticky' | 'public' | string
  agentTrunkRefCount?: number
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
