export interface SessionEvent {
  id: number
  timestamp: string
  sessionId: string
  category: string
  name: string
  sipMethod?: string
  sipStatusCode?: number
  sipCallId?: string
  state?: string
  payloadId?: number
  data?: Record<string, unknown>
}

export interface SessionEventListResponse {
  items: Array<SessionEvent>
  total: number
  page: number
  pageSize: number
}

export interface SessionPayload {
  payloadId: number
  timestamp: string
  sessionId: string
  kind: string
  contentType?: string
  bodyText?: string
  bodyBytesB64?: string
  parsed?: Record<string, unknown>
}

export interface SessionPayloadListResponse {
  items: Array<SessionPayload>
  total: number
  page: number
  pageSize: number
}

export interface SessionDialog {
  id: number
  sessionId: string
  timestamp: string
  sipCallId?: string
  fromTag?: string
  toTag?: string
  remoteContact?: string
  cseq: number
  routeSet?: Array<string>
}

export interface SessionDialogListResponse {
  items: Array<SessionDialog>
  total: number
  page: number
  pageSize: number
}

export interface SessionStats {
  id: number
  timestamp: string
  sessionId: string
  pliSent: number
  pliResponse: number
  lastPliSentAt?: string
  lastKeyframeAt?: string
  audioRtcpRr: number
  audioRtcpSr: number
  videoRtcpRr: number
  videoRtcpSr: number
  data?: Record<string, unknown>
}

export interface SessionStatsListResponse {
  items: Array<SessionStats>
  total: number
  page: number
  pageSize: number
}

export interface SessionOverview {
  SessionId: string
  CreatedAt?: string
  UpdatedAt?: string
  EndedAt?: string
  Direction?: string
  FromUri?: string
  ToUri?: string
  SipCallId?: string
  FinalState?: string
  EndReason?: string
  AuthMode?: string
  TrunkId?: number
  TrunkName?: string
  SipUsername?: string
  RtpAudioPort?: number
  RtpVideoPort?: number
  RtcpAudioPort?: number
  RtcpVideoPort?: number
  SipOpusPt?: number
  AudioProfile?: string
  VideoProfile?: string
  VideoRejected: boolean
  LiveState?: string
  LiveDurationSec?: number
}
