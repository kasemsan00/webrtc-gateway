// apps/frontend/src/features/ws-clients/types.ts
export interface WSClient {
  clientId: string
  sessionId?: string
  connectedAt: string
  trunkResolved: boolean
  resolvedTrunkId?: number
  resolvedTrunkPublicId?: string
  availability?: string
  callState?: string
  authSubject?: string
  publicOnly?: boolean
}

export interface WSClientStreamEvent {
  type: string
  clientId: string
  at: string
  client?: WSClient
}
