export interface SessionDirectoryEntry {
  sessionId: string
  ownerInstanceId: string
  wsUrl: string
  expiresAt: string
  updatedAt: string
  isExpired: boolean
}

export interface SessionDirectoryListResponse {
  items: Array<SessionDirectoryEntry>
  total: number
  page: number
  pageSize: number
}

export interface SessionDirectoryListParams {
  page?: number
  pageSize?: number
  search?: string
}
