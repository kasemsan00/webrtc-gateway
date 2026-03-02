export interface SessionHistory {
  sessionId: string
  createdAt: string
  updatedAt: string
  endedAt: string
  direction: string
  fromUri: string
  toUri: string
  sipCallId: string
  finalState: string
  endReason: string
}

export interface SessionHistoryListResponse {
  items: Array<SessionHistory>
  total: number
  page: number
  pageSize: number
}

export interface SessionHistoryListParams {
  page?: number
  pageSize?: number
  search?: string
  direction?: string
  state?: string
  sessionId?: string
  createdAfter?: string
  createdBefore?: string
}
