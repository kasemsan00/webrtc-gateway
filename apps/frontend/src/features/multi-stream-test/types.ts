export type GatewayWsEndpoint = '/ws-agent' | '/ws'

export type ConnectionStatus =
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'registered'

export type CallPhase =
  | 'idle'
  | 'dialing'
  | 'ringing'
  | 'incoming'
  | 'active'
  | 'ended'

export type LocalScreenStatus =
  | 'idle'
  | 'negotiating'
  | 'waiting_peer'
  | 'active'
  | 'stopping'

export type RemoteScreenStatus = 'idle' | 'negotiating' | 'active' | 'closed'

export type PendingStreamAction = 'add_with_sdp' | 'add_reuse' | 'remove' | null

export type BannerTone = 'info' | 'success' | 'warning' | 'error'

export interface StatusBanner {
  tone: BannerTone
  message: string
  reason?: string
}

export interface ConnectionFormConfig {
  endpoint: GatewayWsEndpoint
  wsUrl: string
  accessToken: string
  sipDomain: string
  sipUsername: string
  sipPassword: string
  sipPort: string
  multiVideo: boolean
  multiCall: boolean
}

export interface IncomingCallInfo {
  sessionId: string
  from: string
  to: string
  hasVideo: boolean
}

export interface PendingRenegotiation {
  renegotiationId: string
  sessionId: string
  reason: string
  role?: string
  mediaDirection?: string
}

export interface PeerMediaStatus {
  audioReceiving: boolean
  videoReceiving: boolean
}

export interface DiagnosticLogEntry {
  id: number
  timestamp: string
  direction: 'IN' | 'OUT' | 'SYS'
  type: string
  summary: string
}

export interface OutboundAgentRegisterMessage {
  type: 'agent_register'
  sipDomain: string
  sipUsername: string
  sipPassword: string
  sipPort?: number
  multiCall?: boolean
  multiVideo?: boolean
}

export interface OutboundClientStateMessage {
  type: 'client_state'
  availability?: 'idle' | 'busy' | 'unavailable'
  callState?: string
  sessionId?: string
  multiCall?: boolean
  multiVideo?: boolean
}

export interface OutboundTrunkResolveMessage {
  type: 'trunk_resolve'
  sipDomain: string
  sipUsername: string
  sipPassword: string
  sipPort?: number
}

export interface OutboundOfferMessage {
  type: 'offer'
  sdp: string
  sessionId?: string
}

export interface OutboundIceMessage {
  type: 'ice'
  sessionId: string
  candidate: RTCIceCandidateInit
}

export interface OutboundCallMessage {
  type: 'call'
  sessionId: string
  destination: string
  from?: string
  sipDomain?: string
  sipUsername?: string
  sipPassword?: string
  sipPort?: number
}

export interface OutboundAcceptMessage {
  type: 'accept'
  sessionId: string
}

export interface OutboundRejectMessage {
  type: 'reject'
  sessionId: string
  reason?: string
}

export interface OutboundHangupMessage {
  type: 'hangup'
  sessionId: string
  reason?: string
}

export interface OutboundStreamUpdateMessage {
  type: 'stream_update'
  sessionId: string
  role: 'screen'
  action: 'add' | 'remove'
  sdp?: string
}

export interface OutboundRenegotiateAnswerMessage {
  type: 'renegotiate_answer'
  sessionId: string
  renegotiationId: string
  status: 'ok' | 'failed'
  sdp?: string
  reason?: string
}

export type OutboundWsMessage =
  | OutboundAgentRegisterMessage
  | OutboundClientStateMessage
  | OutboundTrunkResolveMessage
  | OutboundOfferMessage
  | OutboundIceMessage
  | OutboundCallMessage
  | OutboundAcceptMessage
  | OutboundRejectMessage
  | OutboundHangupMessage
  | OutboundStreamUpdateMessage
  | OutboundRenegotiateAnswerMessage

export interface InboundWsMessage {
  type: string
  sessionId?: string
  sdp?: string
  state?: string
  from?: string
  to?: string
  hasVideo?: string
  endReason?: string
  reason?: string
  error?: string
  operation?: string
  operationSessionId?: string
  trunkId?: number
  trunkPublicId?: string
  role?: string
  action?: string
  status?: string
  active?: boolean
  renegotiationId?: string
  mediaDirection?: string
  requiresAnswer?: boolean
  kind?: string
  direction?: string
  held?: boolean
}

export interface MultiVideoSdpValidation {
  valid: boolean
  videoCount: number
  audioCount: number
  videoMids: Array<string>
  error?: string
}

export interface MultiStreamState {
  connectionStatus: ConnectionStatus
  multiVideoEnabled: boolean
  multiCallEnabled: boolean
  trunkId: number | null
  trunkPublicId: string | null
  lastError: string | null

  callPhase: CallPhase
  sessionId: string | null
  incomingCall: IncomingCallInfo | null
  endReason: string | null
  peerMedia: PeerMediaStatus

  localScreenStatus: LocalScreenStatus
  localScreenTransceiverNegotiated: boolean
  localScreenConfirmedByPeer: boolean
  pendingStreamAction: PendingStreamAction
  localScreenBanner: StatusBanner | null

  remoteScreenStatus: RemoteScreenStatus
  remoteScreenTransceiverNegotiated: boolean
  remoteScreenReason: string | null
  pendingRenegotiation: PendingRenegotiation | null
}
