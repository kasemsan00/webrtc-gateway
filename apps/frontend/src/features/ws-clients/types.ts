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
  agentOnly?: boolean
  multiCall?: boolean
  activeCalls?: number
  presenceMode?: 'ephemeral' | 'sticky' | 'public' | string
  agentTrunkRefCount?: number
}

export interface WSClientStreamEvent {
  type: string
  clientId: string
  at: string
  client?: WSClient
}

export type WSClientPresenceLabel = 'agent' | 'mobile' | 'public'

export function resolveWSClientPresenceLabel(
  client: Pick<WSClient, 'agentOnly' | 'publicOnly' | 'presenceMode'>,
): WSClientPresenceLabel {
  if (client.agentOnly || client.presenceMode === 'ephemeral') return 'agent'
  if (client.publicOnly || client.presenceMode === 'public') return 'public'
  return 'mobile'
}
