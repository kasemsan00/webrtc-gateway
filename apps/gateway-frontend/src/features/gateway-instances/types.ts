export interface GatewayInstance {
  instanceId: string
  wsUrl: string
  expiresAt: string
  updatedAt: string
  isExpired: boolean
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
